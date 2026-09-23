package mailmigrate

import "testing"

// The Gmail trap, which is the reason folders.go exists at all. "[Gmail]/All Mail" holds a copy of
// every message in the account; a migration that treats it as an ordinary folder lands each message
// twice and doubles the customer's storage on day one.
func TestMapSkipsGmailAllMail(t *testing.T) {
	got := Map(Folder{Name: "[Gmail]/All Mail", Delim: "/", Attrs: []string{`\All`, `\HasNoChildren`}})
	if got.Skip != SkipVirtual {
		t.Fatalf("All Mail must be skipped, got %+v", got)
	}
}

// Same trap without the special-use attribute, for a server that publishes none.
func TestMapSkipsAllMailByName(t *testing.T) {
	got := Map(Folder{Name: "[Gmail]/All Mail", Delim: "/"})
	if got.Skip != SkipVirtual {
		t.Fatalf("All Mail must be skipped by name too, got %+v", got)
	}
}

func TestMapRoles(t *testing.T) {
	cases := []struct {
		name  string
		f     Folder
		role  string
		skip  SkipReason
		local string
	}{
		{"inbox", Folder{Name: "INBOX"}, "inbox", "", ""},
		{"sent by attribute", Folder{Name: "[Gmail]/Sent Mail", Delim: "/", Attrs: []string{`\Sent`}}, "sent", "", ""},
		{"sent by name", Folder{Name: "Sent Items"}, "sent", "", ""},
		{"drafts", Folder{Name: "Drafts"}, "drafts", "", ""},
		{"archive", Folder{Name: "Archive"}, "archive", "", ""},
		{"trash", Folder{Name: "Trash"}, "", SkipTrash, ""},
		{"junk by attribute", Folder{Name: "Ongewenste e-mail", Attrs: []string{`\Junk`}}, "", SkipJunk, ""},
		// A German "Gesendet" with no attribute is NOT guessed at: it is copied under its own name,
		// which is untidy but correct. Guessing across languages files somebody's mail in the wrong
		// place, which is not.
		{"unknown language keeps its name", Folder{Name: "Gesendet"}, "", "", "Gesendet"},
		{"nested keeps its path", Folder{Name: "Clients/2024/Invoices", Delim: "/"}, "", "", "Clients/2024/Invoices"},
		// The vendor container is dropped: nobody wants a folder named after the provider they left.
		{"gmail container dropped", Folder{Name: "[Gmail]/Work", Delim: "/"}, "", "", "Work"},
		// Courier and friends separate with a dot.
		{"dot delimiter", Folder{Name: "INBOX.Projects.Alpha", Delim: "."}, "", "", "INBOX/Projects/Alpha"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Map(tc.f)
			if got.Role != tc.role || got.Skip != tc.skip || got.Name != tc.local {
				t.Fatalf("got %+v, want role=%q skip=%q name=%q", got, tc.role, tc.skip, tc.local)
			}
		})
	}
}

// Read/unread has to survive the move. Landing forty thousand messages as unread is the kind of
// damage that gets a migration blamed for the product.
func TestKeywords(t *testing.T) {
	got := Keywords([]string{`\Seen`, `\Flagged`, `\Answered`, `\Draft`, `\Recent`, `\Deleted`})
	want := map[string]bool{"$seen": true, "$flagged": true, "$answered": true, "$draft": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want exactly %v", got, want)
	}
	for _, k := range got {
		if !want[k] {
			t.Fatalf("unexpected keyword %q in %v", k, got)
		}
	}
}

// \Deleted means "marked for removal, not yet removed" -- a state JMAP has no word for. Importing it
// as anything either resurrects mail the customer deleted or hides mail they did not.
func TestKeywordsDropsDeleted(t *testing.T) {
	for _, k := range Keywords([]string{`\Deleted`}) {
		t.Fatalf("\\Deleted must not become a keyword, got %q", k)
	}
}
