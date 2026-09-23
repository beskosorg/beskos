package mailmigrate

// Deciding what a folder on the old server becomes here, and what is not copied at all.
//
// This file is small and it is where a migration is won or lost. Copying bytes is solved; deciding
// that "[Gmail]/All Mail" must NOT be copied is the difference between a customer arriving with
// their mailbox and a customer arriving with two of everything.

import "strings"

// SkipReason says why a folder is not copied. Empty means it is.
//
// A SKIPPED FOLDER IS REPORTED, NEVER SILENT. The customer counts their folders after a migration,
// and one that vanished with no explanation is indistinguishable from data loss. Each reason below
// has a translated line in the panel.
type SkipReason string

const (
	// SkipVirtual is a folder that is not a folder: a saved search the server presents as one. Its
	// messages already live somewhere real, so copying it copies them a second time.
	//
	// GMAIL IS THE REASON THIS CONSTANT EXISTS. "[Gmail]/All Mail" contains every message in the
	// account, including everything in the inbox and everything in Sent. A migration that copies the
	// folder list literally lands each message twice -- once from its own folder and once from All
	// Mail -- and doubles both the customer's storage and their bill. "Starred" and "Important" are
	// the same trap in miniature.
	SkipVirtual SkipReason = "virtual"
	// SkipTrash is the deleted mail. Copying it means paying to store, and carrying into a new
	// system, exactly the mail somebody already decided to throw away.
	SkipTrash SkipReason = "trash"
	// SkipJunk is the spam folder, for the same reason, plus one: spam that arrives pre-filed in a
	// new mailbox teaches the new filter nothing and looks like our filter let it in.
	SkipJunk SkipReason = "junk"
)

// virtualFolders are the names, lowercased and with the vendor prefix stripped, that servers present
// as folders while holding no messages of their own.
//
// English names only, and Gmail localises all of these. That is not a gap: Gmail also publishes the
// RFC 6154 attributes, which Map checks first and which say the same thing in every language. This
// list is the belt to that pair of braces, for a server that offers neither.
var virtualFolders = map[string]bool{
	"all mail":  true, // Gmail: every message in the account, in one place
	"starred":   true,
	"important": true,
	"chats":     true, // Gmail: not mail at all, and not fetchable over IMAP anyway
}

// Target is where one source folder lands, or why it does not.
type Target struct {
	// Name is the folder name to use here, with the hierarchy separated by "/". Empty when Skip is
	// set, and empty for a folder mapped by Role instead.
	Name string
	// Role maps the folder onto one this account already has: "inbox", "sent", "drafts", "archive".
	// Mapping by role rather than by name is what makes a German customer's "Gesendet" land in the
	// Sent folder their new client already knows about, instead of beside it.
	Role string
	Skip SkipReason
}

// Map decides what happens to one source folder.
func Map(f Folder) Target {
	attrs := make(map[string]bool, len(f.Attrs))
	for _, a := range f.Attrs {
		attrs[strings.ToLower(a)] = true
	}
	// RFC 6154 special-use attributes first: they are the server telling us what the folder is, in
	// any language, and they beat guessing from a name.
	switch {
	case attrs[`\all`]:
		return Target{Skip: SkipVirtual}
	case attrs[`\trash`]:
		return Target{Skip: SkipTrash}
	case attrs[`\junk`]:
		return Target{Skip: SkipJunk}
	case attrs[`\sent`]:
		return Target{Role: "sent"}
	case attrs[`\drafts`]:
		return Target{Role: "drafts"}
	case attrs[`\archive`]:
		return Target{Role: "archive"}
	case attrs[`\flagged`], attrs[`\important`]:
		return Target{Skip: SkipVirtual}
	}

	leaf, path := splitFolder(f.Name, f.Delim)
	lower := strings.ToLower(leaf)
	if virtualFolders[lower] {
		return Target{Skip: SkipVirtual}
	}
	// Name-based fallbacks, for servers that publish no special-use attributes at all. English only
	// and deliberately so: a folder we fail to recognise is copied under its own name, which is
	// correct-but-untidy, whereas guessing wrong across languages files somebody's mail in the wrong
	// place. The attributes above already cover every server that says what it means.
	switch lower {
	case "inbox":
		return Target{Role: "inbox"}
	case "sent", "sent items", "sent mail", "sent messages":
		return Target{Role: "sent"}
	case "drafts", "draft":
		return Target{Role: "drafts"}
	case "archive", "archives":
		return Target{Role: "archive"}
	case "trash", "deleted items", "deleted messages":
		return Target{Skip: SkipTrash}
	case "spam", "junk", "junk e-mail", "junk email", "bulk mail":
		return Target{Skip: SkipJunk}
	}
	return Target{Name: strings.Join(append(path, leaf), "/")}
}

// splitFolder breaks a server path into its parts and drops the vendor container that holds no mail.
//
// Gmail nests everything under a literal "[Gmail]" (or "[Google Mail]" in some locales) that exists
// only to group things. Carrying it across would give the customer a folder named after the provider
// they just left, at the top of the mailbox they just paid to move.
func splitFolder(name, delim string) (leaf string, path []string) {
	parts := []string{name}
	if delim != "" && delim != "\x00" {
		parts = strings.Split(name, delim)
	}
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "[") && strings.HasSuffix(p, "]") {
			continue // "[Gmail]", "[Google Mail]"
		}
		cleaned = append(cleaned, p)
	}
	if len(cleaned) == 0 {
		return name, nil
	}
	return cleaned[len(cleaned)-1], cleaned[:len(cleaned)-1]
}

// Keywords translates the old server's IMAP flags into JMAP keywords.
//
// \Deleted is dropped on purpose. In IMAP it means "marked for removal, not yet removed" -- a state
// JMAP has no word for. Importing it as anything would either resurrect mail the customer deleted or
// hide mail they did not.
func Keywords(flags []string) []string {
	out := make([]string, 0, len(flags))
	for _, f := range flags {
		switch strings.ToLower(f) {
		case `\seen`:
			out = append(out, "$seen")
		case `\answered`:
			out = append(out, "$answered")
		case `\flagged`:
			out = append(out, "$flagged")
		case `\draft`:
			out = append(out, "$draft")
		}
	}
	return out
}
