package mailbox

import (
	"strings"
	"testing"
)

func TestWireAddrDisplay(t *testing.T) {
	cases := []struct {
		addr wireAddr
		want string
	}{
		{wireAddr{Name: "Jane Doe", Email: "jane@example.com"}, "Jane Doe <jane@example.com>"},
		{wireAddr{Name: "Jane Doe"}, "Jane Doe"},
		{wireAddr{Email: "jane@example.com"}, "jane@example.com"},
		{wireAddr{}, ""},
	}
	for _, c := range cases {
		if got := c.addr.display(); got != c.want {
			t.Errorf("display(%+v) = %q, want %q", c.addr, got, c.want)
		}
	}
}

func TestWireEmailHeader(t *testing.T) {
	e := wireEmail{
		ID:            "e1",
		ThreadID:      "t1",
		Subject:       "Hello",
		Preview:       "hi there",
		ReceivedAt:    "2026-01-02T15:04:05Z",
		HasAttachment: true,
		From:          []wireAddr{{Name: "Jane Doe", Email: "jane@example.com"}},
		To:            []wireAddr{{Email: "bob@example.com"}, {Name: "Cara"}},
	}
	h := e.header()
	if h.ID != "e1" || h.ThreadID != "t1" || h.Subject != "Hello" || h.Preview != "hi there" || !h.HasAttach {
		t.Errorf("header() basic fields = %+v", h)
	}
	if h.From != "Jane Doe <jane@example.com>" {
		t.Errorf("header().From = %q", h.From)
	}
	wantTo := []string{"bob@example.com", "Cara"}
	if len(h.To) != len(wantTo) || h.To[0] != wantTo[0] || h.To[1] != wantTo[1] {
		t.Errorf("header().To = %v, want %v", h.To, wantTo)
	}
	if h.ReceivedAt.Year() != 2026 {
		t.Errorf("header().ReceivedAt not parsed: %v", h.ReceivedAt)
	}
}

func TestWireEmailHeaderInvalidDateDoesNotPanic(t *testing.T) {
	e := wireEmail{ReceivedAt: "not-a-date"}
	h := e.header()
	if !h.ReceivedAt.IsZero() {
		t.Errorf("an unparsable receivedAt should yield the zero time, got %v", h.ReceivedAt)
	}
}

func TestWireEmailTextJoinsParts(t *testing.T) {
	e := wireEmail{
		TextBody: []wirePart{{PartID: "1"}, {PartID: "2"}, {PartID: "missing"}},
		BodyValues: wireBodyMap{
			"1": {Value: "first part"},
			"2": {Value: "second part"},
		},
	}
	got := e.text()
	want := "first part\nsecond part"
	if got != want {
		t.Errorf("text() = %q, want %q", got, want)
	}
}

func TestWireEmailTextTruncatesLongBody(t *testing.T) {
	long := strings.Repeat("a", bodyCharLimit+500)
	e := wireEmail{
		TextBody:   []wirePart{{PartID: "1"}},
		BodyValues: wireBodyMap{"1": {Value: long}},
	}
	got := e.text()
	if len(got) <= bodyCharLimit {
		t.Fatalf("text() = %d chars, expected it to exceed bodyCharLimit before truncation markup", len(got))
	}
	if !strings.HasSuffix(got, "[...truncated]") {
		t.Errorf("truncated text does not end with the truncation marker: %q", got[len(got)-30:])
	}
	if !strings.HasPrefix(got, strings.Repeat("a", 100)) {
		t.Error("truncated text lost its original content prefix")
	}
}
