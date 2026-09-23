package mailmigrate

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// Law 2.9: "not yet" has to be distinguishable from "never". A rejected password will still be
// rejected on the hundredth attempt, and retrying it is how an account gets locked out; a refused
// connection is worth another try in a minute. Getting this backwards is the difference between a
// migration that recovers and one that quietly hammers Google until it stops answering.
func TestClassify(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		code      string
		permanent bool
	}{
		{"rejected password", fmt.Errorf("login: %w", ErrAuth), "migration.auth_failed", true},
		{"no destination credential", errPermanent{code: "migration.destination_locked", err: errors.New("x")},
			"migration.destination_locked", true},
		{"server refused the connection", errors.New("dial tcp: connection refused"),
			"migration.source_unreachable", false},
		{"cancelled", context.Canceled, "migration.interrupted", false},
		{"timed out", context.DeadlineExceeded, "migration.interrupted", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, permanent := classify(tc.err)
			if code != tc.code || permanent != tc.permanent {
				t.Fatalf("got (%q, %v), want (%q, %v)", code, permanent, tc.code, tc.permanent)
			}
		})
	}
}

// The error carried to the customer is a key, never a sentence: whatever a foreign IMAP server says
// is in its own language and format (law 4.4).
func TestClassifyAlwaysReturnsAKey(t *testing.T) {
	code, _ := classify(errors.New("NO [ALERT] Nie mozna zalogowac"))
	if code == "" || code[:10] != "migration." {
		t.Fatalf("got %q, want a migration.* key", code)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abcdef", 3); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := truncate("ab", 5); got != "ab" {
		t.Fatalf("got %q", got)
	}
}
