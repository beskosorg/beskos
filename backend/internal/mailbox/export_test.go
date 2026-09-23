package mailbox

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestWriteMboxMessageSeparatorLine(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	m := exportMsg{
		ID:         "m1",
		ReceivedAt: time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC),
		From: []struct {
			Email string `json:"email"`
		}{{Email: "jane@example.com"}},
	}
	if err := writeMboxMessage(w, m, []byte("Subject: hi\r\n\r\nbody")); err != nil {
		t.Fatalf("writeMboxMessage: %v", err)
	}
	w.Flush()
	out := buf.String()
	if !strings.HasPrefix(out, "From jane@example.com ") {
		t.Fatalf("mbox message does not start with the From separator line: %q", out[:min(60, len(out))])
	}
}

func TestWriteMboxMessageEscapesEmbeddedFromLines(t *testing.T) {
	// A quoted email whose body itself contains a line starting with "From " must be escaped
	// with a leading ">", or an mbox reader would split it into two messages -- the exact bug
	// the function's own comment describes.
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	m := exportMsg{ID: "m1", ReceivedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	raw := "Subject: fwd\r\n\r\n> From me@example.com Mon Jan 1\r\nFrom the whole team, thanks"
	if err := writeMboxMessage(w, m, []byte(raw)); err != nil {
		t.Fatalf("writeMboxMessage: %v", err)
	}
	w.Flush()
	lines := strings.Split(buf.String(), "\n")
	for i, line := range lines {
		if i == 0 {
			continue // the message's own "From <sender> <date>" separator line is expected
		}
		if strings.HasPrefix(line, "From ") {
			t.Errorf("body line %q was not escaped with a leading '>'", line)
		}
	}
	if !strings.Contains(buf.String(), ">From the whole team, thanks") {
		t.Error("the embedded 'From ' body line was not escaped")
	}
}

func TestWriteMboxMessageFallsBackToMailerDaemon(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	m := exportMsg{ID: "m1"} // no From address, no ReceivedAt
	if err := writeMboxMessage(w, m, []byte("body")); err != nil {
		t.Fatalf("writeMboxMessage: %v", err)
	}
	w.Flush()
	if !strings.HasPrefix(buf.String(), "From MAILER-DAEMON ") {
		t.Errorf("missing sender should fall back to MAILER-DAEMON, got %q", buf.String()[:40])
	}
}
