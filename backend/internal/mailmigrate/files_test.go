package mailmigrate

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// The oldest bug in mail archiving: a body line that begins "From " is escaped with ">" on the way
// in, and a reader that does not undo it returns a message different from the one that arrived.
// A reader that also treats it as a separator invents a message nobody ever sent.
func TestReadMboxDoesNotSplitOnQuotedFrom(t *testing.T) {
	box := "From alice@example.com Mon Jan  1 10:00:00 2020\r\n" +
		"From: alice@example.com\r\n" +
		"Subject: one\r\n" +
		"\r\n" +
		">From here on it is body text, not a new message.\r\n" +
		"Still the same message.\r\n" +
		"\r\n" +
		"From bob@example.com Tue Jan  2 11:00:00 2020\r\n" +
		"From: bob@example.com\r\n" +
		"Subject: two\r\n" +
		"\r\n" +
		"Second body.\r\n"

	var got []Message
	n, err := ReadMessages(strings.NewReader(box), KindMbox, 0, func(m Message) error {
		got = append(got, m)
		return nil
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if n != 2 || len(got) != 2 {
		t.Fatalf("got %d messages (n=%d), want 2 -- a quoted From split one in two", len(got), n)
	}
	if !bytes.Contains(got[0].Raw, []byte("\nFrom here on it is body text")) {
		t.Fatalf("the escaping was not undone; body was:\n%s", got[0].Raw)
	}
	if !bytes.Contains(got[0].Raw, []byte("Subject: one")) || !bytes.Contains(got[1].Raw, []byte("Subject: two")) {
		t.Fatalf("messages came apart at the wrong place")
	}
	// The separator line itself is not part of the message: it was written by the archiver.
	if bytes.Contains(got[1].Raw, []byte("From bob@example.com Tue")) {
		t.Fatalf("the mbox separator leaked into the message")
	}
}

// Resuming: a job that already imported the first two messages must hand over only the third, and
// must not re-import what it has.
func TestReadMboxSkipsWhatWasAlreadyImported(t *testing.T) {
	var box strings.Builder
	for _, s := range []string{"one", "two", "three"} {
		box.WriteString("From a@b.c Mon Jan  1 10:00:00 2020\r\nSubject: " + s + "\r\n\r\nbody\r\n\r\n")
	}
	var got []string
	n, err := ReadMessages(strings.NewReader(box.String()), KindMbox, 2, func(m Message) error {
		got = append(got, string(m.Raw))
		return nil
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if n != 3 {
		t.Fatalf("counted %d, want 3 -- the watermark must still advance over skipped messages", n)
	}
	if len(got) != 1 || !strings.Contains(got[0], "three") {
		t.Fatalf("got %v, want only the third message", got)
	}
}

// Thunderbird records read/unread outside the message. Losing it hands somebody forty thousand
// unread emails on their first morning.
func TestMozillaStatusBecomesSeen(t *testing.T) {
	box := "From a@b.c Mon Jan  1 10:00:00 2020\r\nX-Mozilla-Status: 0001\r\nSubject: read\r\n\r\nbody\r\n" +
		"\r\nFrom a@b.c Mon Jan  1 10:00:00 2020\r\nX-Mozilla-Status: 0000\r\nSubject: unread\r\n\r\nbody\r\n"
	var got []Message
	if _, err := ReadMessages(strings.NewReader(box), KindMbox, 0, func(m Message) error {
		got = append(got, m)
		return nil
	}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2", len(got))
	}
	if len(Keywords(got[0].Flags)) != 1 || Keywords(got[0].Flags)[0] != "$seen" {
		t.Fatalf("first message should be seen, flags=%v", got[0].Flags)
	}
	if len(got[1].Flags) != 0 {
		t.Fatalf("second message should not be seen, flags=%v", got[1].Flags)
	}
}

func TestReadEml(t *testing.T) {
	var got []Message
	n, err := ReadMessages(strings.NewReader("Subject: alone\r\n\r\nbody\r\n"), KindEml, 0, func(m Message) error {
		got = append(got, m)
		return nil
	})
	if err != nil || n != 1 || len(got) != 1 {
		t.Fatalf("n=%d got=%d err=%v", n, len(got), err)
	}
	if !strings.Contains(string(got[0].Raw), "Subject: alone") {
		t.Fatalf("body mangled: %q", got[0].Raw)
	}
}

// Outlook's own store is a MAPI database, not mail. Refusing it clearly, with a named reason the
// panel can translate, beats importing a mangled approximation of somebody's correspondence.
func TestDetectFile(t *testing.T) {
	cases := []struct {
		name string
		kind FileKind
		code string
	}{
		{"Local Folders", KindMbox, ""}, // Thunderbird: no extension at all
		{"Inbox.mbox", KindMbox, ""},
		{"saved.eml", KindEml, ""},
		{"profile.zip", KindZip, ""},
		{"archive.pst", "", "migration.file_pst"},
		{"archive.OST", "", "migration.file_pst"},
		{"mac-outlook.olm", "", "migration.file_olm"},
		{"one.msg", "", "migration.file_msg"},
		{"Inbox.msf", "", "migration.file_msf"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, err := DetectFile(tc.name)
			if tc.code == "" {
				if err != nil || kind != tc.kind {
					t.Fatalf("got (%q, %v), want %q", kind, err, tc.kind)
				}
				return
			}
			var ue ErrUnsupportedFile
			if !errorsAs(err, &ue) || ue.Code != tc.code {
				t.Fatalf("got %v, want code %q", err, tc.code)
			}
		})
	}
}

// A Thunderbird profile zips up as a tree with an index file beside every mailbox. The indexes hold
// no mail, and the ".sbd" directories are scaffolding the customer never named.
func TestZipEntriesDropsIndexesAndScaffolding(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range []string{
		"Mail/Local Folders/Inbox",
		"Mail/Local Folders/Inbox.msf",
		"Mail/Local Folders/Archives.sbd/2019",
		"__MACOSX/._Inbox",
		"Mail/Local Folders/notes.pst",
	} {
		f, _ := w.Create(name)
		_, _ = f.Write([]byte("From a@b.c Mon Jan  1 10:00:00 2020\r\nSubject: x\r\n\r\nbody\r\n"))
	}
	_ = w.Close()

	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	got := ZipEntries(r)
	want := map[string]bool{"Mail/Local Folders/Inbox": true, "Mail/Local Folders/Archives/2019": true}
	if len(got) != len(want) {
		t.Fatalf("got %d entries %v, want %v", len(got), names(got), want)
	}
	for _, e := range got {
		if !want[e.Name] {
			t.Fatalf("unexpected entry %q (all: %v)", e.Name, names(got))
		}
	}
}

func names(es []FileEntry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Name)
	}
	return out
}

// errorsAs keeps the test readable without importing errors for one call.
func errorsAs(err error, target *ErrUnsupportedFile) bool {
	if ue, ok := err.(ErrUnsupportedFile); ok {
		*target = ue
		return true
	}
	return false
}
