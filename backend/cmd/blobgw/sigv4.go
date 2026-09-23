package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// AWS SigV4 signature verification.
//
// WHY VERIFY, if the service is only reachable on the internal network: because whoever talks to
// it reads all of the mail. "It is on a private network" is an assumption about tomorrow's
// infrastructure, not a property of the service. Stalwart already signs — we just have to check.
//
// What is checked: the access key, the date (against replays), and the HMAC over the canonical
// request. The body is checked separately, in stream — see checker.

const algorithm = "AWS4-HMAC-SHA256"

// maxSkew is the clock acceptance window. Fifteen minutes is what AWS uses.
const maxSkew = 15 * time.Minute

type credential struct {
	access string
	secret string
	region string
}

func (c credential) check(r *http.Request) error {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, algorithm+" ") {
		return errf("missing %s authorization", algorithm)
	}
	parts := map[string]string{}
	for _, p := range strings.Split(strings.TrimPrefix(auth, algorithm+" "), ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(p), "=")
		if ok {
			parts[key] = value
		}
	}

	rawCredential := parts["Credential"]
	signature := parts["Signature"]
	signed := parts["SignedHeaders"]
	if rawCredential == "" || signature == "" || signed == "" {
		return errf("incomplete authorization")
	}

	fields := strings.Split(rawCredential, "/")
	if len(fields) != 5 {
		return errf("malformed credential")
	}
	if !hmac.Equal([]byte(fields[0]), []byte(c.access)) {
		return errf("unknown access key")
	}
	shortDate, region, service := fields[1], fields[2], fields[3]

	when := r.Header.Get("X-Amz-Date")
	if when == "" {
		return errf("missing X-Amz-Date")
	}
	t, err := time.Parse("20060102T150405Z", when)
	if err != nil {
		return errf("invalid X-Amz-Date: %w", err)
	}
	// Without this window, a captured signed request would work forever.
	if d := time.Since(t); d > maxSkew || d < -maxSkew {
		return errf("request outside the %s window (skew %s)", maxSkew, d.Round(time.Second))
	}

	bodyHash := r.Header.Get("X-Amz-Content-Sha256")
	if bodyHash == "" {
		return errf("missing X-Amz-Content-Sha256")
	}
	if strings.HasPrefix(bodyHash, "STREAMING-") {
		return errf("streaming-signed body is not supported")
	}

	canonical := canonicalRequest(r, strings.Split(signed, ";"), bodyHash)
	scope := strings.Join([]string{shortDate, region, service, "aws4_request"}, "/")
	toSign := strings.Join([]string{
		algorithm, when, scope, hexSHA256([]byte(canonical)),
	}, "\n")

	want := hex.EncodeToString(hmacSHA256(signingKey(c.secret, shortDate, region, service), toSign))
	if !hmac.Equal([]byte(want), []byte(signature)) {
		return errf("signature does not match")
	}
	return nil
}

// canonicalRequest builds the canonical form of the request, as the spec requires.
func canonicalRequest(r *http.Request, signed []string, bodyHash string) string {
	sort.Strings(signed)

	var headers strings.Builder
	for _, name := range signed {
		headers.WriteString(name)
		headers.WriteByte(':')
		headers.WriteString(headerValue(r, name))
		headers.WriteByte('\n')
	}

	return strings.Join([]string{
		r.Method,
		canonicalPath(r.URL),
		canonicalQuery(r.URL),
		headers.String(),
		strings.Join(signed, ";"),
		bodyHash,
	}, "\n")
}

// headerValue: Host does not come in Go's header map, it comes in its own field.
func headerValue(r *http.Request, name string) string {
	if name == "host" {
		return strings.TrimSpace(r.Host)
	}
	values := r.Header.Values(http.CanonicalHeaderKey(name))
	for i, v := range values {
		values[i] = strings.Join(strings.Fields(v), " ")
	}
	return strings.Join(values, ",")
}

// canonicalPath uses the path exactly as it came in the request, without decoding: the client
// signed what it wrote, and normalizing here would give different signatures for the same request.
func canonicalPath(u *url.URL) string {
	if p := u.EscapedPath(); p != "" {
		return p
	}
	return "/"
}

func canonicalQuery(u *url.URL) string {
	values := u.Query()
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		vs := values[k]
		sort.Strings(vs)
		for _, v := range vs {
			if b.Len() > 0 {
				b.WriteByte('&')
			}
			b.WriteString(escape(k))
			b.WriteByte('=')
			b.WriteString(escape(v))
		}
	}
	return b.String()
}

// escape follows AWS's encoding, which differs from url.QueryEscape in two spots: the space is
// %20 (not +) and the tilde is not escaped.
func escape(s string) string {
	e := url.QueryEscape(s)
	e = strings.ReplaceAll(e, "+", "%20")
	return strings.ReplaceAll(e, "%7E", "~")
}

func signingKey(secret, data, region, service string) []byte {
	k := hmacSHA256([]byte("AWS4"+secret), data)
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, service)
	return hmacSHA256(k, "aws4_request")
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

func hexSHA256(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
