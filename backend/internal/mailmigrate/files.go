package mailmigrate

// Reading mail out of a file the customer uploaded, rather than off a server.
//
// WHAT THIS IS FOR. Thunderbird keeps "Local Folders" as mbox files, and for anyone who used POP3
// that is the ONLY copy -- it was downloaded and removed from the server years ago. IMAP cannot
// reach it, no provider holds it, and before this the honest answer was that the mail could not be
// moved. The same applies to an archive exported before an account was closed, or a mailbox handed
// over on a memory stick when somebody left.
//
// FORMATS, AND WHY THESE. mbox because it is what Thunderbird actually stores and what every
// exporter emits; .eml because a single saved message is one message; .zip because a Thunderbird
// profile is a tree of mbox files and nobody uploads forty of them one at a time.

import (
	"archive/zip"
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"path"
	"strings"
	"time"
)

// ErrUnsupportedFile is a file we cannot read. It carries a translation key so the panel can say
// which format was handed over and what to do instead.
type ErrUnsupportedFile struct{ Code string }

func (e ErrUnsupportedFile) Error() string { return "mailmigrate: unsupported file (" + e.Code + ")" }

// FileKind is how an upload will be read.
type FileKind string

const (
	KindMbox FileKind = "mbox"
	KindEml  FileKind = "eml"
	KindZip  FileKind = "zip"
)

// DetectFile decides how to read an upload from its name.
//
// BY NAME AND NOT BY CONTENT, for the formats we accept. A Thunderbird "Local Folders" file has no
// extension at all and begins with "From " like any mbox, so sniffing buys nothing that the default
// does not already give. What sniffing WOULD buy is a wrong guess on a truncated file.
func DetectFile(name string) (FileKind, error) {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return KindZip, nil
	case strings.HasSuffix(lower, ".eml"), strings.HasSuffix(lower, ".msg.eml"):
		return KindEml, nil
	case strings.HasSuffix(lower, ".pst"), strings.HasSuffix(lower, ".ost"):
		// Outlook's own store. It is not mail in a readable sense: it is a MAPI database, and the
		// messages inside it have no RFC822 form until Outlook itself builds one. Refusing clearly,
		// with the way round on screen, beats importing a mangled approximation of somebody's
		// correspondence.
		return "", ErrUnsupportedFile{Code: "migration.file_pst"}
	case strings.HasSuffix(lower, ".olm"):
		return "", ErrUnsupportedFile{Code: "migration.file_olm"}
	case strings.HasSuffix(lower, ".msg"):
		// A single Outlook message, also MAPI rather than RFC822.
		return "", ErrUnsupportedFile{Code: "migration.file_msg"}
	case strings.HasSuffix(lower, ".msf"):
		// Thunderbird's index beside each mbox. It holds no messages, and somebody who uploads it
		// has picked the wrong file out of the same directory.
		return "", ErrUnsupportedFile{Code: "migration.file_msf"}
	default:
		// Thunderbird's Local Folders have no extension, and .mbox is the common export suffix.
		return KindMbox, nil
	}
}

// FileEntry is one mailbox inside an upload: the whole file for an mbox, or one member of a zip.
type FileEntry struct {
	// Name as it will appear to the customer and as the destination folder is named.
	Name string
	Kind FileKind
}

// ZipEntries lists the mailboxes inside a zip.
//
// Thunderbird's profile has "Mail/Local Folders/Sent" beside "Sent.msf" and "Foo.sbd/Bar". The
// index files and the directory scaffolding are dropped; what is left is named by its path so two
// folders called "Sent" in different branches do not collide.
func ZipEntries(r *zip.Reader) []FileEntry {
	out := make([]FileEntry, 0, len(r.File))
	for _, f := range r.File {
		if f.FileInfo().IsDir() || f.UncompressedSize64 == 0 {
			continue
		}
		name := f.Name
		base := path.Base(name)
		if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "__MACOSX") {
			continue
		}
		kind, err := DetectFile(base)
		if err != nil {
			continue // a .msf index or an Outlook file riding along; not mail we can read
		}
		if kind == KindZip {
			continue // a zip inside a zip is somebody's backup of a backup; one level is enough
		}
		out = append(out, FileEntry{Name: cleanEntryName(name), Kind: kind})
	}
	return out
}

// cleanEntryName turns a path inside the archive into the folder name the customer will see.
// Thunderbird's ".sbd" suffix marks a directory holding subfolders and means nothing here.
func cleanEntryName(name string) string {
	parts := make([]string, 0, 4)
	for _, p := range strings.Split(name, "/") {
		p = strings.TrimSpace(strings.TrimSuffix(p, ".sbd"))
		if p == "" || p == "." {
			continue
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return name
	}
	return strings.Join(parts, "/")
}

// mboxFrom marks the start of a message. Only a line beginning exactly like this separates
// messages; anything else that looks similar is body text.
var mboxFrom = []byte("From ")

// ReadMessages walks a mailbox stream and calls fn for each message, in order, one-based.
//
// skip messages are counted and thrown away without being handed over -- that is how a resumed job
// picks up where it stopped without importing anything twice and without a byte offset to trust.
//
// THE ESCAPED "From " LINE IS THE WHOLE FORMAT. mbox separates messages with a line starting
// "From ", so any body line that would start that way was written with a ">" in front of it. Not
// undoing that returns a message subtly different from the one that arrived -- and doing it wrong
// splits one message into two, inventing mail nobody sent. It is the oldest bug in mail archiving.
func ReadMessages(r io.Reader, kind FileKind, skip int64, fn func(Message) error) (int64, error) {
	if kind == KindEml {
		raw, err := io.ReadAll(io.LimitReader(r, maxMessageBytes))
		if err != nil {
			return 0, err
		}
		if skip > 0 || len(raw) == 0 {
			return 1, nil // already imported on an earlier pass, or an empty file
		}
		return 1, fn(messageFrom(raw, 1))
	}

	br := bufio.NewReaderSize(r, 256*1024)
	var seen int64
	var buf []byte
	started := false

	deliver := func() error {
		if !started {
			return nil
		}
		seen++
		if seen <= skip || len(buf) == 0 {
			buf = buf[:0]
			return nil
		}
		msg := messageFrom(buf, seen)
		buf = nil // handed over; the next message gets a fresh slice
		return fn(msg)
	}

	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			switch {
			case hasPrefix(line, mboxFrom) && atMessageStart(buf, started):
				if derr := deliver(); derr != nil {
					return seen, derr
				}
				started = true
			case started:
				buf = append(buf, unescapeFrom(line)...)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if derr := deliver(); derr != nil {
					return seen, derr
				}
				return seen, nil
			}
			return seen, err
		}
		if len(buf) > maxMessageBytes {
			return seen, fmt.Errorf("a message in this file exceeds %d bytes", maxMessageBytes)
		}
	}
}

// atMessageStart guards against a "From " line in the middle of a message being read as a
// separator. Inside a message the previous line must have been blank; before the first one there is
// nothing to check.
func atMessageStart(buf []byte, started bool) bool {
	if !started {
		return true
	}
	n := len(buf)
	if n == 0 {
		return true
	}
	if n >= 2 && buf[n-1] == '\n' && buf[n-2] == '\n' {
		return true
	}
	return n >= 3 && buf[n-1] == '\n' && buf[n-2] == '\r' && buf[n-3] == '\n'
}

func hasPrefix(line, prefix []byte) bool {
	return len(line) >= len(prefix) && string(line[:len(prefix)]) == string(prefix)
}

// unescapeFrom removes the ">" that the writer added in front of a body line beginning "From ".
// Successive ">" are stripped one level, which is the same convention every mbox writer uses.
func unescapeFrom(line []byte) []byte {
	i := 0
	for i < len(line) && line[i] == '>' {
		i++
	}
	if i == 0 || !hasPrefix(line[i:], mboxFrom) {
		return line
	}
	return line[1:]
}

// messageFrom wraps raw bytes as a message, taking the received date from the headers.
//
// The mbox "From " line carries a date too, but it is the local time of whatever wrote the file,
// with no time zone worth trusting. The Date: header is what the mail itself says.
func messageFrom(raw []byte, ordinal int64) Message {
	m := Message{UID: uint32(ordinal), Raw: raw}
	if hdr, err := mail.ReadMessage(strings.NewReader(string(raw))); err == nil {
		if when, derr := hdr.Header.Date(); derr == nil {
			m.InternalDate = when
		}
		// Thunderbird records read/unread in an X-Mozilla-Status bitfield rather than in the
		// message. Bit 0 is "read"; without it every imported message arrives unread and somebody
		// gets forty thousand bold lines on their first morning.
		if st := hdr.Header.Get("X-Mozilla-Status"); st != "" && mozillaRead(st) {
			m.Flags = append(m.Flags, `\Seen`)
		}
	}
	if m.InternalDate.IsZero() {
		m.InternalDate = time.Unix(0, 0)
	}
	return m
}

// mozillaRead reads bit 0 of Thunderbird's four-digit hexadecimal status field.
func mozillaRead(status string) bool {
	var v uint64
	if _, err := fmt.Sscanf(strings.TrimSpace(status), "%x", &v); err != nil {
		return false
	}
	return v&0x0001 != 0
}

// importFolderName is where an uploaded mailbox lands.
//
// Under its own name, taken from the upload's tree, and never merged into the Inbox. An import that
// scatters ten years of archived mail through the folders somebody uses every day is an import they
// cannot undo, and the first thing they ask is how to separate it again.
func importFolderName(entry string) string {
	name := cleanEntryName(entry)
	// Drop a trailing extension so "Inbox.mbox" becomes "Inbox"; Thunderbird's own files have none.
	if i := strings.LastIndex(name, "."); i > 0 {
		if suffix := strings.ToLower(name[i:]); suffix == ".mbox" || suffix == ".eml" || suffix == ".zip" {
			name = name[:i]
		}
	}
	// Thunderbird's own containers add nothing: everything under them is already the customer's.
	for _, prefix := range []string{"Mail/Local Folders/", "Local Folders/", "Mail/"} {
		name = strings.TrimPrefix(name, prefix)
	}
	if name == "" {
		return "Imported"
	}
	return name
}
