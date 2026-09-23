package mailbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// EmailHeader is the header of a message returned by a search. No body: whoever wants
// the content requests it separately (Read), so a listing does not drag megabytes along.
type EmailHeader struct {
	ID         string    `json:"id"`
	ThreadID   string    `json:"thread_id"`
	From       string    `json:"from"`
	To         []string  `json:"to"`
	Subject    string    `json:"subject"`
	Preview    string    `json:"preview"`
	ReceivedAt time.Time `json:"received_at"`
	HasAttach  bool      `json:"has_attachment"`
}

// EmailBody is the content of a message. Text is the body in plain text; the HTML is not returned
// on purpose, because what consumes this is a language model and markup only adds noise
// (and injection surface — §6.1).
type EmailBody struct {
	EmailHeader
	Text        string   `json:"text"`
	Attachments []string `json:"attachments"`
}

// bodyCharLimit caps the returned body. An e-mail with a 10 MB text attachment would blow
// past the model's context window, and the bill along with it.
const bodyCharLimit = 20000

// SearchEmails searches messages by free text across the whole mailbox (JMAP Email/query with the
// "text" filter, which Stalwart applies to sender, subject and body). Most recent first.
//
// An empty query returns the account's most recent messages, which is the useful behavior for
// "what came in today?".
func (u *UserClient) SearchEmails(ctx context.Context, query string, limit int) ([]EmailHeader, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	acc, err := u.accountID(ctx)
	if err != nil {
		return nil, err
	}
	filter := map[string]any{}
	if q := strings.TrimSpace(query); q != "" {
		filter["text"] = q
	}
	resp, err := u.jmap(ctx, []any{
		[]any{"Email/query", map[string]any{
			"accountId": acc, "filter": filter,
			"sort":  []any{map[string]any{"property": "receivedAt", "isAscending": false}},
			"limit": limit,
		}, "q"},
		[]any{"Email/get", map[string]any{
			"accountId": acc, "#ids": map[string]any{"resultOf": "q", "name": "Email/query", "path": "/ids"},
			"properties": []string{"id", "threadId", "from", "to", "subject", "preview", "receivedAt", "hasAttachment"},
		}, "g"},
	})
	if err != nil {
		return nil, err
	}
	if len(resp) < 2 {
		return nil, fmt.Errorf("jmap user: incomplete search response")
	}
	var got struct {
		List []wireEmail `json:"list"`
	}
	if err := json.Unmarshal(resp[1], &got); err != nil {
		return nil, err
	}
	out := make([]EmailHeader, 0, len(got.List))
	for _, e := range got.List {
		out = append(out, e.header())
	}
	return out, nil
}

// ReadEmail returns the header and the text body of a message. The id comes from an earlier
// search; if it doesn't exist (or belongs to another account), Stalwart returns an empty list
// and this raises a not-found error — never another mailbox's content.
func (u *UserClient) ReadEmail(ctx context.Context, id string) (EmailBody, error) {
	if strings.TrimSpace(id) == "" {
		return EmailBody{}, fmt.Errorf("jmap user: empty email id")
	}
	acc, err := u.accountID(ctx)
	if err != nil {
		return EmailBody{}, err
	}
	resp, err := u.jmap(ctx, []any{
		[]any{"Email/get", map[string]any{
			"accountId": acc, "ids": []string{id},
			"properties": []string{"id", "threadId", "from", "to", "subject", "preview", "receivedAt",
				"hasAttachment", "textBody", "bodyValues", "attachments"},
			"fetchTextBodyValues": true,
			"maxBodyValueBytes":   bodyCharLimit,
		}, "g"},
	})
	if err != nil {
		return EmailBody{}, err
	}
	if len(resp) < 1 {
		return EmailBody{}, fmt.Errorf("jmap user: incomplete read response")
	}
	var got struct {
		List []wireEmail `json:"list"`
	}
	if err := json.Unmarshal(resp[0], &got); err != nil {
		return EmailBody{}, err
	}
	if len(got.List) == 0 {
		return EmailBody{}, fmt.Errorf("jmap user: email %s not found", id)
	}
	e := got.List[0]
	body := EmailBody{EmailHeader: e.header(), Text: e.text()}
	for _, a := range e.Attachments {
		name := a.Name
		if name == "" {
			name = a.Type
		}
		if name != "" {
			body.Attachments = append(body.Attachments, name)
		}
	}
	return body, nil
}

// wireEmail is the JMAP form of an Email, with only the fields we requested.
type wireEmail struct {
	ID            string      `json:"id"`
	ThreadID      string      `json:"threadId"`
	Subject       string      `json:"subject"`
	Preview       string      `json:"preview"`
	ReceivedAt    string      `json:"receivedAt"`
	HasAttachment bool        `json:"hasAttachment"`
	From          []wireAddr  `json:"from"`
	To            []wireAddr  `json:"to"`
	TextBody      []wirePart  `json:"textBody"`
	BodyValues    wireBodyMap `json:"bodyValues"`
	Attachments   []wirePart  `json:"attachments"`
}

type wireAddr struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type wirePart struct {
	PartID string `json:"partId"`
	Name   string `json:"name"`
	Type   string `json:"type"`
}

type wireBodyMap map[string]struct {
	Value       string `json:"value"`
	IsTruncated bool   `json:"isTruncated"`
}

func (e wireEmail) header() EmailHeader {
	h := EmailHeader{
		ID: e.ID, ThreadID: e.ThreadID, Subject: e.Subject,
		Preview: e.Preview, HasAttach: e.HasAttachment,
	}
	h.ReceivedAt, _ = time.Parse(time.RFC3339, e.ReceivedAt)
	if len(e.From) > 0 {
		h.From = e.From[0].display()
	}
	for _, a := range e.To {
		h.To = append(h.To, a.display())
	}
	return h
}

func (a wireAddr) display() string {
	if a.Name != "" && a.Email != "" {
		return a.Name + " <" + a.Email + ">"
	}
	if a.Name != "" {
		return a.Name
	}
	return a.Email
}

// text joins the body's text parts, resolving each partId in the bodyValues map.
func (e wireEmail) text() string {
	var b strings.Builder
	for _, p := range e.TextBody {
		v, ok := e.BodyValues[p.PartID]
		if !ok {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(v.Value)
	}
	s := b.String()
	if len(s) > bodyCharLimit {
		s = s[:bodyCharLimit] + "\n[...truncated]"
	}
	return s
}
