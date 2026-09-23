package httpapi

import (
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Link unfurl: fetches metadata (Open Graph / <title>) from a URL for the chat preview. It's a
// server-to-server fetch of a user-controlled URL → SSRF is the central risk. Defenses:
//   - http/https only;
//   - DNS resolution is checked at DIAL time (blocks private/loopback/link-local IPs, avoiding
//     rebinding);
//   - short timeout, body cap, few redirects (each revalidated by the same dialer);
//   - in-memory cache with TTL so we don't redo the fetch or hammer the target.

type unfurlData struct {
	// Kind tells the client how to draw it: "" is a plain web page, "task" is one of ours and gets
	// the task card instead of a generic link preview.
	Kind        string `json:"kind,omitempty"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Image       string `json:"image"`
	Site        string `json:"site"`
}

type unfurlEntry struct {
	data unfurlData
	ok   bool
	exp  time.Time
}

var (
	unfurlMu    sync.Mutex
	unfurlCache = map[string]unfurlEntry{}
)

const (
	unfurlTTL     = time.Hour
	unfurlMaxBody = 512 << 10 // 512 KiB is enough for the <head>
)

// isBlockedIP rejects addresses that aren't publicly routable (SSRF).
func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast()
}

// safeDialContext resolves the host and only dials if ALL the IPs are public — checking at dial
// time (and not before) closes off DNS rebinding.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("unfurl: resolve %q: %v", host, err)
	}
	for _, ip := range ips {
		if isBlockedIP(ip.IP) {
			return nil, fmt.Errorf("unfurl: blocked address %s", ip.IP)
		}
	}
	d := &net.Dialer{Timeout: 4 * time.Second}
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}

var unfurlClient = &http.Client{
	Timeout: 6 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("unfurl: too many redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return fmt.Errorf("unfurl: bad redirect scheme")
		}
		return nil // safeDialContext revalidates the IP on every hop
	},
	Transport: &http.Transport{DialContext: safeDialContext, DisableKeepAlives: true},
}

var (
	metaRe  = regexp.MustCompile(`(?is)<meta[^>]+>`)
	titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	attrRe  = regexp.MustCompile(`(?is)(property|name)\s*=\s*["']([^"']+)["']`)
	contRe  = regexp.MustCompile(`(?is)content\s*=\s*["']([^"']*)["']`)
)

func metaContent(tag string) (kind, content string) {
	k := attrRe.FindStringSubmatch(tag)
	c := contRe.FindStringSubmatch(tag)
	if k == nil || c == nil {
		return "", ""
	}
	return strings.ToLower(k[2]), html.UnescapeString(c[1])
}

func (s *Server) handleUnfurl(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeErr(w, http.StatusBadRequest, "chat.invalid")
		return
	}
	// OUR OWN LINK: resolved from the database, for THIS viewer, and never cached. The cache is keyed
	// by URL alone, so one person with access would fill it and the next person without access would
	// be served their answer. A task title is not public inside the tenant.
	if s.ownHost(u) {
		if data, ok := s.internalUnfurl(r.Context(), u); ok {
			writeJSON(w, http.StatusOK, map[string]any{"unfurl": data})
			return
		}
		// No access, no such task, or a Beskos link we do not preview: answer "nothing to show".
		// Fetching our own SPA instead would cost a round trip to read a page with no metadata.
		writeJSON(w, http.StatusOK, map[string]any{"unfurl": nil})
		return
	}

	unfurlMu.Lock()
	if e, ok := unfurlCache[raw]; ok && time.Now().Before(e.exp) {
		unfurlMu.Unlock()
		if !e.ok {
			writeJSON(w, http.StatusOK, map[string]any{"unfurl": nil})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"unfurl": e.data})
		return
	}
	unfurlMu.Unlock()

	data, ok := s.fetchUnfurl(r.Context(), u)
	unfurlMu.Lock()
	unfurlCache[raw] = unfurlEntry{data: data, ok: ok, exp: time.Now().Add(unfurlTTL)}
	if len(unfurlCache) > 5000 { // simple pruning
		for k, e := range unfurlCache {
			if time.Now().After(e.exp) {
				delete(unfurlCache, k)
			}
		}
	}
	unfurlMu.Unlock()
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"unfurl": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unfurl": data})
}

func (s *Server) fetchUnfurl(ctx context.Context, u *url.URL) (unfurlData, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return unfurlData{}, false
	}
	req.Header.Set("User-Agent", "BeskosBot/1.0 (+link preview)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := unfurlClient.Do(req)
	if err != nil {
		return unfurlData{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		return unfurlData{}, false
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, unfurlMaxBody))
	head := string(body)
	if i := strings.Index(strings.ToLower(head), "</head>"); i >= 0 {
		head = head[:i] // only the <head> matters for OG/title
	}
	d := unfurlData{URL: u.String(), Site: u.Host}
	for _, tag := range metaRe.FindAllString(head, -1) {
		kind, content := metaContent(tag)
		if content == "" {
			continue
		}
		switch kind {
		case "og:title", "twitter:title":
			if d.Title == "" {
				d.Title = content
			}
		case "og:description", "twitter:description", "description":
			if d.Description == "" {
				d.Description = content
			}
		case "og:image", "twitter:image", "twitter:image:src":
			if d.Image == "" {
				d.Image = content
			}
		case "og:site_name":
			d.Site = content
		}
	}
	if d.Title == "" {
		if m := titleRe.FindStringSubmatch(head); m != nil {
			d.Title = strings.TrimSpace(html.UnescapeString(m[1]))
		}
	}
	// Relative image → absolute; rejects unusual schemes.
	if d.Image != "" {
		if iu, e := u.Parse(d.Image); e == nil && (iu.Scheme == "http" || iu.Scheme == "https") {
			d.Image = iu.String()
		} else {
			d.Image = ""
		}
	}
	if d.Title == "" && d.Description == "" {
		return unfurlData{}, false // nothing useful
	}
	return d, true
}
