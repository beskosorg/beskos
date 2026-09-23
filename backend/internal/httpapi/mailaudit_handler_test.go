package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// What people actually paste into the box. Every one of these that we reject is a person who tried
// the tool once, got "invalid domain", and never came back -- so the normalisation is not a nicety.
func TestMailAuditAcceptsWhatPeoplePaste(t *testing.T) {
	s := &Server{}
	for _, in := range []string{
		"beskos.com",
		"BESKOS.COM",
		"  beskos.com  ",
		"ana@beskos.com",          // an address
		"https://beskos.com",      // a URL
		"https://beskos.com/mail", // a URL with a path
		"beskos.com.",             // trailing dot
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/public/mail-audit?domain="+url.QueryEscape(in), nil)
		s.handleMailAudit(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("%q -> %d, want 200", in, w.Code)
			continue
		}
		var rep struct {
			Domain string `json:"domain"`
			Total  int    `json:"total"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if rep.Domain != "beskos.com" {
			t.Errorf("%q normalised to %q, want beskos.com", in, rep.Domain)
		}
		if rep.Total == 0 {
			t.Errorf("%q returned no checks", in)
		}
	}
}

// The endpoint reads DNS for whatever it is given, so what it accepts is the whole of its exposure.
// None of these should reach a resolver.
func TestMailAuditRejectsWhatIsNotADomain(t *testing.T) {
	s := &Server{}
	for _, in := range []string{
		"", "localhost", "127.0.0.1", "10.0.0.1",
		"file:///etc/passwd", "beskos", "-beskos.com", "beskos..com",
		"http://[::1]", "a b.com",
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/public/mail-audit?domain="+url.QueryEscape(in), nil)
		s.handleMailAudit(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%q -> %d, want 400", in, w.Code)
		}
	}
}

// The landing calls this from a different origin, so the browser needs both the header on the
// response and an answer to the preflight. It shipped without either: curl does not enforce CORS,
// so every test I ran passed while the page itself could not read a single reply.
func TestMailAuditIsCallableFromAnotherOrigin(t *testing.T) {
	s := &Server{}
	h := publicCORS(http.HandlerFunc(s.handleMailAudit))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/public/mail-audit?domain=beskos.com", nil)
	r.Header.Set("Origin", "https://beskos.com")
	h.ServeHTTP(w, r)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, want *", got)
	}
	// Credentials must NOT be allowed alongside a wildcard: a browser rejects that pairing, and
	// wanting it here would mean this endpoint had become something that reads a session.
	if w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Error("a public endpoint must not allow credentials")
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodOptions, "/api/public/mail-audit", nil)
	r.Header.Set("Origin", "https://beskos.com")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Errorf("preflight -> %d, want 204", w.Code)
	}
}
