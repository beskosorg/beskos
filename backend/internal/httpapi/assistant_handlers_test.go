package httpapi

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// Drive search is shared between the UI and the assistant's drive.search tool. If wildcard
// escaping gets lost, a user searching for "50%" would start receiving everything — and the
// assistant would start "seeing" files the screen doesn't show.
func TestLikePatternEscapesWildcards(t *testing.T) {
	cases := map[string]string{
		"relatorio": `%relatorio%`,
		"50%":       `%50\%%`,
		"a_b":       `%a\_b%`,
		`c\d`:       `%c\\d%`,
		"":          `%%`,
	}
	for in, want := range cases {
		if got := likePattern(in); got != want {
			t.Errorf("likePattern(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUUIDText(t *testing.T) {
	id := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	got := uuidText(pgtype.UUID{Bytes: id, Valid: true})
	if got != id.String() {
		t.Errorf("uuidText = %q, want %q", got, id)
	}
	// A null column returns an empty string instead of a zero UUID, which would look like a real id.
	if got := uuidText(pgtype.UUID{}); got != "" {
		t.Errorf("uuidText of null UUID = %q, want empty", got)
	}
}

// The title comes from the user's own sentence, not from the model — and has to fit in the list
// without breaking it.
func TestConversationTitle(t *testing.T) {
	if got := conversationTitle("  procura a fatura\ndo mês  "); got != "procura a fatura do mês" {
		t.Errorf("conversationTitle = %q", got)
	}
	// Cut by RUNES, not by bytes: cutting mid-way through an accented character would produce garbage.
	long := strings.Repeat("á", 200)
	got := conversationTitle(long)
	if r := []rune(got); len(r) != 81 || r[80] != '…' {
		t.Errorf("long title = %d runes (%q…), want 80 + ellipsis", len(r), string(r[:5]))
	}
}
