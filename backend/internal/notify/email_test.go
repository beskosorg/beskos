package notify

import "testing"

// The sender has two forms: the From: header carries the name ("Beskos <noreply@...>"), the SMTP
// envelope only accepts the bare address. Mixing them up gave `501 5.1.8 Bad sender's system
// address` in production.
func TestFromAddressIsBare(t *testing.T) {
	cases := map[string]string{
		"Beskos <noreply@beskos.com>": "noreply@beskos.com",
		"noreply@beskos.com":          "noreply@beskos.com",
		"  <a@b.pt>":                  "a@b.pt",
	}
	for from, want := range cases {
		e := NewEmail("mail.example.com", "465", from, "u", "p", false)
		//nolint:staticcheck // SA5011: see the note below, this is one finding split across both lines
		if e == nil {
			t.Fatalf("NewEmail returned nil for %q", from)
		}
		//nolint:staticcheck // SA5011 false positive: t.Fatalf above calls runtime.Goexit, so this never runs with e nil
		if e.fromAddr != want {
			t.Errorf("from %q -> envelope %q, want %q", from, e.fromAddr, want)
		}
		if e.from != from {
			t.Errorf("the From: header must stay intact: %q", e.from)
		}
	}
}
