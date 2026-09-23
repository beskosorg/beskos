package httpapi

import (
	"bytes"
	"strings"
	"testing"
)

// rebuild is a shortcut for the tests: it runs the rebuild and returns the final content.
func rebuild(t *testing.T, old, body string, totalSize, bs int64, changed ...int64) string {
	t.Helper()
	set := map[int64]bool{}
	for _, i := range changed {
		set[i] = true
	}
	var out bytes.Buffer
	err := rebuildFromDelta(&out, strings.NewReader(old), strings.NewReader(body),
		int64(len(old)), totalSize, bs, set)
	if err != nil {
		t.Fatalf("rebuildFromDelta: %v", err)
	}
	return out.String()
}

func TestRebuildFromDelta(t *testing.T) {
	// 4-byte blocks. "AAAABBBBCCCC" with the middle block swapped for "XXXX".
	if got, want := rebuild(t, "AAAABBBBCCCC", "XXXX", 12, 4, 1), "AAAAXXXXCCCC"; got != want {
		t.Errorf("middle block: %q, want %q", got, want)
	}
	// No block changed ⇒ identical content (empty body).
	if got, want := rebuild(t, "AAAABBBB", "", 8, 4), "AAAABBBB"; got != want {
		t.Errorf("no changes: %q, want %q", got, want)
	}
	// Several blocks changed, in index order.
	if got, want := rebuild(t, "AAAABBBBCCCC", "XXXXZZZZ", 12, 4, 0, 2), "XXXXBBBBZZZZ"; got != want {
		t.Errorf("two blocks: %q, want %q", got, want)
	}
}

func TestRebuildFromDeltaFileGrew(t *testing.T) {
	// File grew: the new block (index 2) can only come from the body.
	got := rebuild(t, "AAAABBBB", "CCCC", 12, 4, 2)
	if want := "AAAABBBBCCCC"; got != want {
		t.Errorf("grew: %q, want %q", got, want)
	}
}

func TestRebuildFromDeltaFileShrank(t *testing.T) {
	// File shrank: stops at totalSize and ignores the rest of the old content.
	if got, want := rebuild(t, "AAAABBBBCCCC", "", 8, 4), "AAAABBBB"; got != want {
		t.Errorf("shrank: %q, want %q", got, want)
	}
	// Last partial block coming from the body.
	if got, want := rebuild(t, "AAAABBBB", "XX", 6, 4, 1), "AAAAXX"; got != want {
		t.Errorf("partial block: %q, want %q", got, want)
	}
}

func TestRebuildFromDeltaRejectsBadInput(t *testing.T) {
	set := map[int64]bool{}
	var out bytes.Buffer
	// Block NOT marked as changed but missing from the old content (the client got it wrong):
	// it has to refuse instead of writing a truncated/garbage file.
	err := rebuildFromDelta(&out, strings.NewReader("AAAA"), strings.NewReader(""), 4, 8, 4, set)
	if err == nil {
		t.Fatal("expected an error when a block is missing from the old content")
	}

	// Body shorter than the blocks marked as changed.
	out.Reset()
	err = rebuildFromDelta(&out, strings.NewReader("AAAA"), strings.NewReader("X"), 4, 4, 4,
		map[int64]bool{0: true})
	if err == nil {
		t.Fatal("expected an error when the body is too short")
	}
}

func TestParseBlockList(t *testing.T) {
	got, err := parseBlockList("0, 3,7")
	if err != nil {
		t.Fatalf("parseBlockList: %v", err)
	}
	for _, i := range []int64{0, 3, 7} {
		if !got[i] {
			t.Errorf("missing block %d", i)
		}
	}
	if got[1] {
		t.Error("block 1 shouldn't be marked")
	}
	if empty, err := parseBlockList(""); err != nil || len(empty) != 0 {
		t.Errorf("empty list should give an empty set, got %v (%v)", empty, err)
	}
}
