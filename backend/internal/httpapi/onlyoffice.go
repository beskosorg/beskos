package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Integration with the OnlyOffice Document Server for office editing (docx/xlsx/pptx).
// The Document Server (a network-only container, with a startup branding patch) loads a config
// that WE SIGN with JWT; it then fetches the file and returns the saved version via a callback,
// both authenticated by our own HMAC token tied to file+user+tenant+permission.

type onlyoffice struct {
	publicURL    string // Document Server URL that the BROWSER uses (api.js + editor)
	internalURL  string // DS URL as reached by OUR API (downloading the edited file in the callback)
	filesHostURL string // OUR API's URL as reached by the Document Server (download/callback)
	jwtSecret    []byte // secret shared with the DS (signs the config; verifies the callback)
	tokenSecret  []byte // internal secret that signs the download/callback URLs
	sourceURL    string // source of the modified Document Server, shown in "About" (AGPL §13)
	brandURL     string // site the header logo links to
}

// SetOfficeBranding sets the addresses that go along with the Beskos brand in the editor: the
// modified Document Server source (required in production, see config) and the logo's site.
func (s *Server) SetOfficeBranding(sourceURL, brandURL string) {
	if s.office == nil {
		return
	}
	s.office.sourceURL = strings.TrimRight(sourceURL, "/")
	s.office.brandURL = strings.TrimRight(brandURL, "/")
}

// EnableOnlyOffice turns on office editing. Without URLs/secrets, it stays off. internalURL is
// optional (default = publicURL) and is used when the DS generates URLs with the browser's host.
func (s *Server) EnableOnlyOffice(publicURL, internalURL, filesHostURL, jwtSecret, tokenSecret string) {
	if publicURL == "" || filesHostURL == "" || jwtSecret == "" || tokenSecret == "" {
		return
	}
	if internalURL == "" {
		internalURL = publicURL
	}
	s.office = &onlyoffice{
		publicURL:    strings.TrimRight(publicURL, "/"),
		internalURL:  strings.TrimRight(internalURL, "/"),
		filesHostURL: strings.TrimRight(filesHostURL, "/"),
		jwtSecret:    []byte(jwtSecret),
		tokenSecret:  []byte(tokenSecret),
	}
}

// officeChrome builds the editor's presentation parameters — the same in the authenticated editor
// and the public viewer, so the product has a single look.
//
// Tabs go on the LIGHT bar with an underline on the active one. The Document Server default
// (`tabStyle: fill` + `tabBackground: header`) pins them to the dark strip with the active one
// filled in, which is what made the editor look heavier than the one in their official screenshots.
// These are free parameters (standard branding); they don't depend on the patch.
// The palette is ours: the startup patch adds the Beskos colors to the editor's CSS, so the theme
// requested here is still the standard light/dark one — it's the CSS that swaps ONLYOFFICE's blue
// for petrol. If the patch fails, the editor stays blue but still works.
func officeChrome(dark bool, plugins bool) map[string]any {
	uiTheme := "theme-light"
	if dark {
		uiTheme = "theme-dark"
	}
	return map[string]any{
		"uiTheme": uiTheme,
		// Two-level header: logo + quick access (save/print/undo) and title on top, tabs below.
		// `compactToolbar: false` keeps the ribbon expanded on open — otherwise the editor starts
		// with only the tabs showing and looks "non-editable" until one is clicked.
		"compactHeader":  false,
		"compactToolbar": false,
		"toolbarNoTabs":  false,
		"hideRightMenu":  true,
		"feedback":       false,
		"help":           false,
		"chat":           false,
		"plugins":        plugins,
		"features": map[string]any{
			"tabStyle":      "line",
			"tabBackground": "toolbar",
		},
	}
}

// reachable rewrites a URL coming from the DS (browser host) to the internal host reachable
// by the API, when they differ.
func (o *onlyoffice) reachable(u string) string {
	if o.internalURL != o.publicURL && strings.HasPrefix(u, o.publicURL) {
		return o.internalURL + strings.TrimPrefix(u, o.publicURL)
	}
	return u
}

// fetchURLAllowed validates the edited file's URL that the callback tells OUR API to fetch. The
// host must be exactly the internal Document Server's — otherwise the callback (whose HMAC token
// leaks in logs/Referer) turns into an SSRF: pointing `url` at 169.254.169.254/minio/stalwart and
// storing the response as the file's version (audit H1). Returns the rewritten URL (internal host)
// and whether it is allowed.
func (o *onlyoffice) fetchURLAllowed(raw string) (string, bool) {
	rewritten := o.reachable(raw)
	pu, err := url.Parse(rewritten)
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
		return "", false
	}
	iu, err := url.Parse(o.internalURL)
	if err != nil || !strings.EqualFold(pu.Host, iu.Host) {
		return "", false
	}
	return rewritten, true
}

// --- HMAC token for the download/callback URLs (file+user+tenant+permission) ---

type docClaims struct {
	FileID   uuid.UUID
	UserID   uuid.UUID
	TenantID uuid.UUID
	CanWrite bool
	Exp      time.Time
}

func (o *onlyoffice) mintToken(cl docClaims) string {
	write := "0"
	if cl.CanWrite {
		write = "1"
	}
	payload := strings.Join([]string{cl.FileID.String(), cl.UserID.String(), cl.TenantID.String(), write, strconv.FormatInt(cl.Exp.Unix(), 10)}, "|")
	enc := base64.RawURLEncoding
	mac := hmac.New(sha256.New, o.tokenSecret)
	mac.Write([]byte(payload))
	return enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString(mac.Sum(nil))
}

func (o *onlyoffice) parseToken(tok string) (docClaims, bool) {
	var cl docClaims
	parts := strings.SplitN(tok, ".", 2)
	if len(parts) != 2 {
		return cl, false
	}
	enc := base64.RawURLEncoding
	payload, err := enc.DecodeString(parts[0])
	if err != nil {
		return cl, false
	}
	sig, err := enc.DecodeString(parts[1])
	if err != nil {
		return cl, false
	}
	mac := hmac.New(sha256.New, o.tokenSecret)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return cl, false
	}
	f := strings.Split(string(payload), "|")
	if len(f) != 5 {
		return cl, false
	}
	if cl.FileID, err = uuid.Parse(f[0]); err != nil {
		return cl, false
	}
	if cl.UserID, err = uuid.Parse(f[1]); err != nil {
		return cl, false
	}
	if cl.TenantID, err = uuid.Parse(f[2]); err != nil {
		return cl, false
	}
	cl.CanWrite = f[3] == "1"
	exp, err := strconv.ParseInt(f[4], 10, 64)
	if err != nil {
		return cl, false
	}
	cl.Exp = time.Unix(exp, 0)
	if time.Now().After(cl.Exp) {
		return cl, false
	}
	return cl, true
}

// mintAIToken signs a STABLE token for the user, for the OnlyOffice AI plugin to use as an
// "API key". The plugin runs in the Document Server's iframe (a different origin) and doesn't have
// our cookie, so authorization travels in the Bearer header. It carries user+tenant and a long
// validity; it only serves the AI proxy, which is limited by the user's own quota — if the token
// leaks, the abuse is theirs and it's capped by the quota.
func (o *onlyoffice) mintAIToken(userID, tenantID uuid.UUID, exp time.Time) string {
	payload := "ai|" + userID.String() + "|" + tenantID.String() + "|" + strconv.FormatInt(exp.Unix(), 10)
	enc := base64.RawURLEncoding
	mac := hmac.New(sha256.New, o.tokenSecret)
	mac.Write([]byte(payload))
	return enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString(mac.Sum(nil))
}

// parseAIToken validates the AI token and returns user+tenant. ok=false if the signature is invalid or it expired.
func (o *onlyoffice) parseAIToken(tok string) (userID, tenantID uuid.UUID, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(tok), ".", 2)
	if len(parts) != 2 {
		return uuid.Nil, uuid.Nil, false
	}
	enc := base64.RawURLEncoding
	payload, err := enc.DecodeString(parts[0])
	if err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	sig, err := enc.DecodeString(parts[1])
	if err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	mac := hmac.New(sha256.New, o.tokenSecret)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return uuid.Nil, uuid.Nil, false
	}
	f := strings.Split(string(payload), "|")
	if len(f) != 4 || f[0] != "ai" {
		return uuid.Nil, uuid.Nil, false
	}
	if userID, err = uuid.Parse(f[1]); err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	if tenantID, err = uuid.Parse(f[2]); err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	exp, err := strconv.ParseInt(f[3], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return uuid.Nil, uuid.Nil, false
	}
	return userID, tenantID, true
}

// --- JWT HS256 (signs the config and verifies the Document Server's callback) ---

func (o *onlyoffice) jwtSign(payload map[string]any) (string, error) {
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signing := header + "." + enc.EncodeToString(body)
	mac := hmac.New(sha256.New, o.jwtSecret)
	mac.Write([]byte(signing))
	return signing + "." + enc.EncodeToString(mac.Sum(nil)), nil
}

// jwtVerify checks the HS256 signature and returns the decoded payload (for the callback).
func (o *onlyoffice) jwtVerify(tok string) (map[string]any, bool) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return nil, false
	}
	enc := base64.RawURLEncoding
	mac := hmac.New(sha256.New, o.jwtSecret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	sig, err := enc.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, false
	}
	raw, err := enc.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

// --- type mapping ---

var officeExts = map[string]bool{
	"docx": true, "doc": true, "odt": true, "rtf": true, "txt": true,
	"xlsx": true, "xls": true, "ods": true, "csv": true,
	"pptx": true, "ppt": true, "odp": true,
}

// documentType is OnlyOffice's: "word" | "cell" | "slide".
func documentType(ext string) string {
	switch ext {
	case "xlsx", "xls", "ods", "csv":
		return "cell"
	case "pptx", "ppt", "odp":
		return "slide"
	default:
		return "word"
	}
}

func extOf(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return strings.ToLower(name[i+1:])
	}
	return ""
}
