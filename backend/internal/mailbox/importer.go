package mailbox

// Putting a message that came from somewhere else INTO the mailbox.
//
// This is the destination half of the migration. The source half speaks IMAP to whoever the customer
// is leaving (see internal/mailmigrate); this half speaks JMAP to us, over the same user credential
// every other server-side job already uses. Two protocols would have been one too many: we own this
// end, and JMAP already has the exact call for it.
//
// WHY Email/import AND NOT Email/set. Email/set builds a message from parts we describe, so it
// writes OUR idea of the message: re-encoded headers, a new Message-ID, our own MIME structure. A
// migration that does that hands the customer a mailbox where every message is subtly not the one
// they received -- signatures break, threading breaks, and a message produced in court is no longer
// the message that arrived. Email/import takes the bytes as they are and stores them untouched.
//
// RECEIVED DATE AND FLAGS TRAVEL WITH THE MESSAGE. A migration that lands 40,000 messages all dated
// today has destroyed the only ordering the mailbox had, and one that loses read/unread hands the
// customer 40,000 unread emails on their first morning. Both are the kind of damage that gets a
// migration blamed for the product.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ImportedMessage is one message on its way in: the raw RFC822 bytes exactly as the old server held
// them, plus the two pieces of context that are NOT in those bytes.
type ImportedMessage struct {
	Raw []byte
	// ReceivedAt is the old server's internal date, not the Date: header. The header is written by
	// the sender and can say anything; the internal date is when the mailbox actually got it, which
	// is what every "sort by date" in every client is really sorting on.
	ReceivedAt time.Time
	// Keywords are JMAP keywords ($seen, $flagged, $answered, $draft), already translated from the
	// source's IMAP flags by the caller.
	Keywords []string
}

// ErrAlreadyImported reports a message the account already holds. It is a normal outcome, not a
// failure: a migration that is resumed, retried, or run twice must land on the same mailbox rather
// than a doubled one, so the caller counts these and carries on.
var ErrAlreadyImported = fmt.Errorf("mailbox: message already imported")

// Import uploads one message and files it in mailboxID, keeping its original bytes, date and flags.
//
// Returns ErrAlreadyImported if the account already has it. Every other error is real.
func (u *UserClient) Import(ctx context.Context, acc, mailboxID string, m ImportedMessage) error {
	blobID, err := u.upload(ctx, acc, m.Raw)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	keywords := map[string]any{}
	for _, k := range m.Keywords {
		keywords[k] = true
	}
	received := m.ReceivedAt
	if received.IsZero() {
		// No internal date from the source. Better an obviously wrong old date than today's, which
		// would silently sort a ten-year-old message to the top of the inbox.
		received = time.Unix(0, 0)
	}
	resp, err := u.jmap(ctx, []any{
		[]any{"Email/import", map[string]any{
			"accountId": acc,
			"emails": map[string]any{
				"m": map[string]any{
					"blobId":     blobID,
					"mailboxIds": map[string]any{mailboxID: true},
					"keywords":   keywords,
					"receivedAt": received.UTC().Format("2006-01-02T15:04:05Z"),
				},
			},
		}, "i"},
	})
	if err != nil {
		return err
	}
	if len(resp) == 0 {
		return fmt.Errorf("jmap user: no import response")
	}
	var res struct {
		Created    map[string]json.RawMessage `json:"created"`
		NotCreated map[string]struct {
			Type string `json:"type"`
		} `json:"notCreated"`
	}
	if err := json.Unmarshal(resp[0], &res); err != nil {
		return err
	}
	if len(res.Created) > 0 {
		return nil
	}
	if e, ok := res.NotCreated["m"]; ok {
		if e.Type == "alreadyExists" {
			return ErrAlreadyImported
		}
		return fmt.Errorf("jmap user: import rejected (%s)", e.Type)
	}
	return fmt.Errorf("jmap user: import returned neither created nor an error")
}

// upload puts the raw message in the blob store and returns its id. The blob is temporary until
// something references it, so an upload whose import then fails costs nothing but time.
func (u *UserClient) upload(ctx context.Context, acc string, raw []byte) (string, error) {
	url := fmt.Sprintf("%s/jmap/upload/%s/", strings.TrimRight(u.base, "/"), acc)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Basic "+u.basic)
	req.Header.Set("Content-Type", "message/rfc822")
	// Own client rather than u.http: a 40 MB message over a slow link legitimately outlives the 20s
	// timeout that suits every other call on this connection, and a migration that dies on the one
	// big attachment in the mailbox is a migration the customer does not trust.
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("upload: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		BlobID string `json:"blobId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.BlobID == "" {
		return "", fmt.Errorf("upload: server returned no blobId")
	}
	return out.BlobID, nil
}

// AccountID exposes the primary mail account of the credential. The migration resolves it once and
// carries it through thousands of messages instead of asking per message.
func (u *UserClient) AccountID(ctx context.Context) (string, error) { return u.accountID(ctx) }

// MailboxRef is one folder as it exists in the destination account.
type MailboxRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	ParentID string `json:"parentId"`
}

// Mailboxes lists every folder in the account.
func (u *UserClient) Mailboxes(ctx context.Context, acc string) ([]MailboxRef, error) {
	resp, err := u.jmap(ctx, []any{
		[]any{"Mailbox/get", map[string]any{
			"accountId": acc, "ids": nil,
			"properties": []string{"id", "name", "role", "parentId"},
		}, "m"},
	})
	if err != nil {
		return nil, err
	}
	if len(resp) == 0 {
		return nil, fmt.Errorf("jmap user: no mailbox response")
	}
	var list struct {
		List []MailboxRef `json:"list"`
	}
	if err := json.Unmarshal(resp[0], &list); err != nil {
		return nil, err
	}
	return list.List, nil
}

// CreateMailbox creates a folder and returns its id. parentID empty means top level.
func (u *UserClient) CreateMailbox(ctx context.Context, acc, name, parentID string) (string, error) {
	create := map[string]any{"name": name}
	if parentID != "" {
		create["parentId"] = parentID
	}
	resp, err := u.jmap(ctx, []any{
		[]any{"Mailbox/set", map[string]any{
			"accountId": acc,
			"create":    map[string]any{"m": create},
		}, "s"},
	})
	if err != nil {
		return "", err
	}
	if len(resp) == 0 {
		return "", fmt.Errorf("jmap user: no mailbox/set response")
	}
	var res struct {
		Created map[string]struct {
			ID string `json:"id"`
		} `json:"created"`
		NotCreated map[string]struct {
			Type string `json:"type"`
		} `json:"notCreated"`
	}
	if err := json.Unmarshal(resp[0], &res); err != nil {
		return "", err
	}
	if c, ok := res.Created["m"]; ok && c.ID != "" {
		return c.ID, nil
	}
	if e, ok := res.NotCreated["m"]; ok {
		return "", fmt.Errorf("jmap user: mailbox %q rejected (%s)", name, e.Type)
	}
	return "", fmt.Errorf("jmap user: mailbox %q not created", name)
}
