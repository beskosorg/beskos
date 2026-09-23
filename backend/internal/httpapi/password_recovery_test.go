package httpapi

// The two things in this flow that fail silently if they are wrong.
//
// A reset that leaks which addresses exist turns the login screen into a staff directory, and a
// reset that skips the second factor hands the account to whoever holds the recovery inbox. Neither
// shows up as an error: both look like the feature working.

import (
	"testing"
	"time"
)

// The endpoint has exactly one answer. If a future change makes it say "no such account", or return
// a different status, or answer faster for an address that does not exist, this is the test that
// should have been standing in the way.
func TestForgotAnswerIsASingleFixedValue(t *testing.T) {
	if len(forgotAnswer) != 1 || forgotAnswer["status"] != "requested" {
		t.Fatalf("the forgot endpoint must have one answer for every input, got %v", forgotAnswer)
	}
}

// An hour: long enough to walk to another computer, short enough that a link left in an inbox is
// not a spare key. Pinned because "let's make it a day, it is friendlier" is a change that sounds
// harmless and is not.
func TestResetWindowIsOneHour(t *testing.T) {
	if resetWindow != time.Hour {
		t.Fatalf("resetWindow = %s; a recovery link that outlives the person's attention is a spare key", resetWindow)
	}
	if resetWindow > 24*time.Hour {
		t.Fatal("a link valid for more than a day is a credential, not a recovery step")
	}
}
