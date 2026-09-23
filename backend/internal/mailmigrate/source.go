// Package mailmigrate copies a mailbox out of the provider the customer is leaving and into theirs
// here. The source speaks IMAP -- which every provider worth leaving still offers -- and the
// destination speaks JMAP through internal/mailbox, because that end is ours.
//
// WHY THIS EXISTS AT ALL. Until it did, the documentation asked the customer to move their own mail
// with a desktop client. For a ten-person company that is a week of somebody's time and a real
// chance of losing a folder, so the honest answer to "can we move" was no -- and everything else
// built here sat behind that closed door. The technique is thirty years old; the reason it had to
// be built is that nothing else we offer matters to somebody who cannot get their mail in.
//
// WHY A LIBRARY (law 2.5). IMAP is a protocol with literals, continuation lines, modified UTF-7
// folder names and one dialect per vendor. github.com/emersion/go-imap is the maintained Go
// implementation and adds no transitive dependency of its own. Writing our own parser would be a
// month of finding out how Gmail, Exchange and Zimbra each disagree with the RFC -- in production,
// on customer data.
package mailmigrate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// Credentials is the account being left behind.
type Credentials struct {
	Host     string
	Port     int
	Username string
	Password string
	// StartTLS uses port 143 with an upgrade instead of implicit TLS on 993. Plain, unencrypted IMAP
	// is not an option: we would be handing somebody's whole correspondence to the network, and a
	// provider that only offers it is a provider whose customer has a bigger problem than migrating.
	StartTLS bool
}

// ErrAuth is a credential the server rejected. It is separated from every other failure because the
// two need opposite handling: this one must stop and ask the customer, and retrying it forever is
// how an account gets locked out. See law 2.9 -- "not yet" has to be distinguishable from "never".
var ErrAuth = errors.New("mailmigrate: the source server rejected the credentials")

// Source is a live, logged-in connection to the old provider.
type Source struct {
	c *imapclient.Client
}

// Dial connects and logs in. The caller closes it.
func Dial(ctx context.Context, cr Credentials) (*Source, error) {
	port := cr.Port
	if port == 0 {
		port = 993
		if cr.StartTLS {
			port = 143
		}
	}
	addr := fmt.Sprintf("%s:%d", cr.Host, port)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A host that accepts the connection and then says nothing is the failure mode of a firewalled
	// or wrong port, and it is the one that would otherwise hang a worker slot forever.
	opts := &imapclient.Options{Dialer: &net.Dialer{Timeout: 20 * time.Second}}

	var c *imapclient.Client
	var err error
	if cr.StartTLS {
		c, err = imapclient.DialStartTLS(addr, opts)
	} else {
		c, err = imapclient.DialTLS(addr, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", addr, err)
	}
	if err := c.Login(cr.Username, cr.Password).Wait(); err != nil {
		c.Close()
		if isAuthFailure(err) {
			return nil, fmt.Errorf("%w: %v", ErrAuth, err)
		}
		return nil, fmt.Errorf("login as %s: %w", cr.Username, err)
	}
	return &Source{c: c}, nil
}

// Close logs out and drops the connection. Errors are not worth reporting: the copy already
// happened or already failed, and neither outcome changes because a logout was rude.
func (s *Source) Close() {
	if s == nil || s.c == nil {
		return
	}
	_ = s.c.Logout().Wait()
	_ = s.c.Close()
}

// Folder is one folder on the old server, with the numbers needed to copy it and to resume.
type Folder struct {
	Name  string   // as the server names it, e.g. "INBOX" or "[Gmail]/Sent Mail"
	Delim string   // the server's hierarchy separator: "/" for most, "." for Courier and friends
	Attrs []string // LIST attributes, including \Sent, \Trash and the rest of RFC 6154
	// Messages, UIDValidity and UIDNext come from STATUS. UIDValidity is what makes resuming safe:
	// if the server changes it, every UID we recorded now means a different message.
	Messages    uint32
	UIDValidity uint32
	UIDNext     uint32
}

// Folders lists everything the account holds, with its message count.
//
// A folder that cannot be selected (\Noselect -- a pure container like Gmail's "[Gmail]") is left
// out: there is nothing in it to copy, and asking for its STATUS is an error on some servers.
func (s *Source) Folders(ctx context.Context) ([]Folder, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	list, err := s.c.List("", "*", &imap.ListOptions{
		ReturnStatus: &imap.StatusOptions{NumMessages: true, UIDNext: true, UIDValidity: true},
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	out := make([]Folder, 0, len(list))
	for _, m := range list {
		f := Folder{Name: m.Mailbox, Delim: string(m.Delim)}
		skip := false
		for _, a := range m.Attrs {
			if a == imap.MailboxAttrNoSelect || a == imap.MailboxAttrNonExistent {
				skip = true
			}
			f.Attrs = append(f.Attrs, string(a))
		}
		if skip {
			continue
		}
		if m.Status != nil {
			if m.Status.NumMessages != nil {
				f.Messages = *m.Status.NumMessages
			}
			f.UIDValidity = m.Status.UIDValidity
			f.UIDNext = uint32(m.Status.UIDNext)
		} else if st, serr := s.Status(ctx, m.Mailbox); serr == nil {
			// The server does not support LIST-STATUS. Ask folder by folder -- slower, but the
			// alternative is a migration that reports every folder as empty and looks finished.
			f.Messages, f.UIDValidity, f.UIDNext = st.Messages, st.UIDValidity, st.UIDNext
		}
		out = append(out, f)
	}
	return out, nil
}

// FolderStatus is the countable part of a folder.
type FolderStatus struct {
	Messages    uint32
	UIDValidity uint32
	UIDNext     uint32
}

// Status asks for one folder's counters without selecting it.
func (s *Source) Status(ctx context.Context, name string) (FolderStatus, error) {
	if err := ctx.Err(); err != nil {
		return FolderStatus{}, err
	}
	d, err := s.c.Status(name, &imap.StatusOptions{NumMessages: true, UIDNext: true, UIDValidity: true}).Wait()
	if err != nil {
		return FolderStatus{}, fmt.Errorf("status %q: %w", name, err)
	}
	st := FolderStatus{UIDValidity: d.UIDValidity, UIDNext: uint32(d.UIDNext)}
	if d.NumMessages != nil {
		st.Messages = *d.NumMessages
	}
	return st, nil
}

// Select opens a folder read-only and returns its UIDVALIDITY.
//
// READ-ONLY IS NOT A DETAIL. A normal SELECT lets the server clear \Recent and, on some, marks
// messages seen as we fetch them. The customer is still working in that account while we copy; a
// migration that comes back having marked four thousand emails as read has damaged the mailbox it
// was supposed to preserve.
func (s *Source) Select(ctx context.Context, name string) (FolderStatus, error) {
	if err := ctx.Err(); err != nil {
		return FolderStatus{}, err
	}
	d, err := s.c.Select(name, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return FolderStatus{}, fmt.Errorf("select %q: %w", name, err)
	}
	return FolderStatus{Messages: d.NumMessages, UIDValidity: d.UIDValidity, UIDNext: uint32(d.UIDNext)}, nil
}

// Message is one message as the old server holds it.
type Message struct {
	UID          uint32
	Raw          []byte
	InternalDate time.Time
	Flags        []string
}

// FetchAfter returns up to limit messages with a UID greater than afterUID, oldest first, from the
// currently selected folder.
//
// ASCENDING UID IS THE WHOLE RESUME STRATEGY. UIDs only ever go up within one UIDVALIDITY, so
// "highest UID copied" is a complete record of progress in a single number -- no list of ids to
// keep, no set to compare. A job killed mid-folder resumes at the next message and copies nothing
// twice.
func (s *Source) FetchAfter(ctx context.Context, afterUID uint32, limit int) ([]Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var set imap.UIDSet
	set.AddRange(imap.UID(afterUID+1), 0) // 0 = "*", everything from here to the end
	cmd := s.c.Fetch(set, &imap.FetchOptions{
		UID:          true,
		Flags:        true,
		InternalDate: true,
		// Peek, for the same reason Select is read-only: fetching must not mark anything read.
		BodySection: []*imap.FetchItemBodySection{{Peek: true}},
	})
	defer cmd.Close()

	out := make([]Message, 0, limit)
	for len(out) < limit {
		data := cmd.Next()
		if data == nil {
			break
		}
		m, err := readMessage(data)
		if err != nil {
			return nil, err
		}
		if m.UID > afterUID {
			out = append(out, m)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	// Closing before the server finished sending is deliberate: asking for one page of a folder with
	// 40,000 messages must not download all 40,000. Close drains the rest of the response.
	if err := cmd.Close(); err != nil && len(out) == 0 {
		return nil, fmt.Errorf("fetch after uid %d: %w", afterUID, err)
	}
	return out, nil
}

// readMessage collects one FETCH response into a Message.
func readMessage(data *imapclient.FetchMessageData) (Message, error) {
	var m Message
	for {
		item := data.Next()
		if item == nil {
			return m, nil
		}
		switch v := item.(type) {
		case imapclient.FetchItemDataUID:
			m.UID = uint32(v.UID)
		case imapclient.FetchItemDataInternalDate:
			m.InternalDate = v.Time
		case imapclient.FetchItemDataFlags:
			for _, f := range v.Flags {
				m.Flags = append(m.Flags, string(f))
			}
		case imapclient.FetchItemDataBodySection:
			raw, err := io.ReadAll(v.Literal)
			if err != nil {
				return m, fmt.Errorf("read message body: %w", err)
			}
			m.Raw = raw
		}
	}
}

// isAuthFailure tells a rejected password from a broken connection. The server may say so in a
// response code (the modern way) or only in the text (everything older), so both are checked.
func isAuthFailure(err error) bool {
	var ie *imap.Error
	if errors.As(err, &ie) {
		switch ie.Code {
		case imap.ResponseCodeAuthenticationFailed, imap.ResponseCodeAuthorizationFailed:
			return true
		}
		if ie.Type == imap.StatusResponseTypeNo {
			t := strings.ToLower(ie.Text)
			for _, hint := range []string{"authentication", "invalid credentials", "login failed",
				"password", "not authorized", "auth"} {
				if strings.Contains(t, hint) {
					return true
				}
			}
		}
	}
	return false
}
