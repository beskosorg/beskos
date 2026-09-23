package mailbox

import (
	"reflect"
	"testing"
)

func TestSplitAddr(t *testing.T) {
	cases := []struct {
		raw      string
		wantName string
		wantMail string
	}{
		{"jane@example.com", "", "jane@example.com"},
		{"Jane Doe <jane@example.com>", "Jane Doe", "jane@example.com"},
		{`"Jane Doe" <jane@example.com>`, "Jane Doe", "jane@example.com"},
		{"  jane@example.com  ", "", "jane@example.com"},
		{"<jane@example.com>", "", "jane@example.com"},
	}
	for _, c := range cases {
		name, email := splitAddr(c.raw)
		if name != c.wantName || email != c.wantMail {
			t.Errorf("splitAddr(%q) = (%q, %q), want (%q, %q)", c.raw, name, email, c.wantName, c.wantMail)
		}
	}
}

func TestFromAddr(t *testing.T) {
	got := fromAddr("jane@example.com", "Jane Doe")
	want := map[string]any{"email": "jane@example.com", "name": "Jane Doe"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fromAddr with a name = %+v, want %+v", got, want)
	}

	got = fromAddr("jane@example.com", "  ")
	want = map[string]any{"email": "jane@example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fromAddr with a blank name = %+v, want %+v (no name key)", got, want)
	}
}

func TestAddrList(t *testing.T) {
	got := addrList([]string{"Jane Doe <jane@example.com>", "bob@example.com"})
	want := []map[string]any{
		{"email": "jane@example.com", "name": "Jane Doe"},
		{"email": "bob@example.com"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("addrList = %+v, want %+v", got, want)
	}
}
