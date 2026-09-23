package httpapi

import (
	"net/url"
	"strings"
	"testing"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// The permission checks live in internalUnfurl and need a database to exercise. What is unit-tested
// here is everything that decides WHETHER we even look: the path shape and the host. Those are the
// parts that, if wrong, either send a task link out to a stranger's web server or make a Beskos link
// fall through to the SSRF fetcher and preview our own login page.

func TestTaskPathAcceptsOnlyTheRealShape(t *testing.T) {
	const board = "11111111-1111-1111-1111-111111111111"
	const card = "22222222-2222-2222-2222-222222222222"

	ok := []string{
		"/mail/kanban/" + board + "/" + card,
		"/mail/kanban/" + board + "/" + card + "/", // trailing slash: same link
	}
	for _, p := range ok {
		if taskPath.FindStringSubmatch(p) == nil {
			t.Errorf("%q should match", p)
		}
	}

	bad := []string{
		"/mail/kanban/" + board,                        // board only: no task to show
		"/mail/kanban/" + board + "/" + card + "/edit", // deeper route, not a task link
		"/mail/wiki/" + board + "/" + card,             // another app
		"/mail/kanban/nope/" + card,                    // not a uuid
		"/",
		"",
	}
	for _, p := range bad {
		if taskPath.FindStringSubmatch(p) != nil {
			t.Errorf("%q should NOT match", p)
		}
	}
}

func TestOwnHostMatchesTheDeploymentAndNothingElse(t *testing.T) {
	s := &Server{
		publicURL:   "https://app.beskos.com",
		corsOrigins: []string{"tauri://localhost", "https://desktop.beskos.com"},
	}
	cases := []struct {
		raw  string
		want bool
	}{
		{"https://app.beskos.com/mail/kanban/a/b", true},
		{"http://app.beskos.com/mail", true},        // scheme does not decide, the host does
		{"https://APP.BESKOS.COM/mail", true},       // hosts are case-insensitive
		{"https://app.beskos.com:8443/mail", true},  // a port is not a different deployment
		{"https://desktop.beskos.com/mail", true},   // listed as an app origin
		{"https://evil.com/mail/kanban/a/b", false}, // the giveaway: someone else's host
		{"https://beskos.com.evil.com/x", false},    // suffix trick
		{"https://meet.google.com/naf", false},
	}
	for _, c := range cases {
		u, err := url.Parse(c.raw)
		if err != nil {
			t.Fatalf("parse %q: %v", c.raw, err)
		}
		if got := s.ownHost(u); got != c.want {
			t.Errorf("ownHost(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}

// Without publicURL and origins configured, nothing is "ours" -- a misconfigured deployment must not
// start treating arbitrary hosts as internal.
func TestOwnHostIsFalseWhenNothingIsConfigured(t *testing.T) {
	s := &Server{}
	u, _ := url.Parse("https://app.beskos.com/mail")
	if s.ownHost(u) {
		t.Error("with no publicURL and no origins, no host is ours")
	}
}

func TestTaskSummaryPrefersWhoAndWhen(t *testing.T) {
	got := taskSummary(db.Card{Assignee: "Ana", Due: "2026-08-04", Label: "urgent", Description: "ignored"})
	if got != "Ana · 2026-08-04 · urgent" {
		t.Errorf("summary = %q", got)
	}
}

func TestTaskSummaryFallsBackToTheDescription(t *testing.T) {
	got := taskSummary(db.Card{Description: "first line\nsecond line"})
	if got != "first line" {
		t.Errorf("summary = %q, want the first line only", got)
	}
}

func TestTaskSummaryTrimsALongDescription(t *testing.T) {
	got := taskSummary(db.Card{Description: strings.Repeat("x", 300)})
	if len([]rune(got)) > 141 {
		t.Errorf("summary is %d runes, want it trimmed", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a trimmed summary should say so, got %q", got[len(got)-10:])
	}
}

func TestNotePathAcceptsOnlyTheRealShape(t *testing.T) {
	const note = "33333333-3333-3333-3333-333333333333"
	if notePath.FindStringSubmatch("/mail/notes/"+note) == nil {
		t.Error("the plain note link should match")
	}
	if notePath.FindStringSubmatch("/mail/notes/"+note+"/") == nil {
		t.Error("a trailing slash is the same link")
	}
	for _, p := range []string{
		"/mail/notes",                   // the list, not a note
		"/mail/notes/" + note + "/edit", // a deeper route
		"/mail/notes/nope",              // not a uuid
		"/mail/kanban/" + note,          // another app
	} {
		if notePath.FindStringSubmatch(p) != nil {
			t.Errorf("%q should NOT match", p)
		}
	}
}

// The two internal shapes must not overlap: a note link resolved as a task (or the reverse) would
// run the wrong permission check, which is the only mistake here that matters.
func TestNoteAndTaskPathsDoNotOverlap(t *testing.T) {
	const a = "11111111-1111-1111-1111-111111111111"
	const b = "22222222-2222-2222-2222-222222222222"
	if taskPath.MatchString("/mail/notes/" + a) {
		t.Error("a note link must not match the task shape")
	}
	if notePath.MatchString("/mail/kanban/" + a + "/" + b) {
		t.Error("a task link must not match the note shape")
	}
}
