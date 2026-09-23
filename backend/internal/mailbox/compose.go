package mailbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Draft is an unsent message, as proposed by the assistant. No attachments: the assistant writes
// text, and an attachment it picked on its own would be content going out that nobody had reviewed.
type Draft struct {
	To      []string `json:"to"`
	Cc      []string `json:"cc,omitempty"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	// InReplyTo, when set, links the draft to the thread of an existing message.
	InReplyTo string `json:"in_reply_to,omitempty"`
}

// CreateDraft saves a draft in the drafts folder and returns its JMAP id. It does NOT send: that
// separation is what makes `mail.draft` a tool without confirmation and `mail.send` one with it.
func (u *UserClient) CreateDraft(ctx context.Context, d Draft) (string, error) {
	acc, err := u.accountID(ctx)
	if err != nil {
		return "", err
	}
	drafts, err := u.mailboxByRole(ctx, acc, "drafts")
	if err != nil {
		return "", err
	}
	_, from, name, err := u.primaryIdentity(ctx, acc)
	if err != nil {
		return "", err
	}
	return u.createEmail(ctx, acc, drafts, d, fromAddr(from, name))
}

// SendEmail saves the draft and submits it. Returns the id of the created draft, so the log
// knows what the action was about.
//
// Sending is always save-then-submit because that is how JMAP works (EmailSubmission points
// at an Email that already exists) — and it has an advantage: if the submission fails, the text
// is not lost, it stays in the drafts.
func (u *UserClient) SendEmail(ctx context.Context, d Draft) (string, error) {
	acc, err := u.accountID(ctx)
	if err != nil {
		return "", err
	}
	drafts, err := u.mailboxByRole(ctx, acc, "drafts")
	if err != nil {
		return "", err
	}
	identityID, from, name, err := u.primaryIdentity(ctx, acc)
	if err != nil {
		return "", err
	}
	emailID, err := u.createEmail(ctx, acc, drafts, d, fromAddr(from, name))
	if err != nil {
		return "", err
	}
	rcpts := append(append([]string{}, d.To...), d.Cc...)
	if err := u.SubmitEmail(ctx, emailID, identityID, from, rcpts); err != nil {
		return emailID, err
	}
	return emailID, nil
}

// createEmail creates the Email/set with the body in plain text. from is the "From:" of the
// authenticated account — without it the e-mail would go out with an empty sender ("—").
func (u *UserClient) createEmail(ctx context.Context, acc, mailboxID string, d Draft, from map[string]any) (string, error) {
	if len(d.To) == 0 {
		return "", fmt.Errorf("jmap user: draft has no recipient")
	}
	email := map[string]any{
		"mailboxIds": map[string]any{mailboxID: true},
		"keywords":   map[string]any{"$draft": true},
		"from":       []any{from},
		"to":         addrList(d.To),
		"subject":    d.Subject,
		"bodyValues": map[string]any{"body": map[string]any{"value": d.Text}},
		"textBody":   []any{map[string]any{"partId": "body", "type": "text/plain"}},
	}
	if len(d.Cc) > 0 {
		email["cc"] = addrList(d.Cc)
	}
	if d.InReplyTo != "" {
		email["inReplyTo"] = []string{d.InReplyTo}
	}

	resp, err := u.jmap(ctx, []any{
		[]any{"Email/set", map[string]any{
			"accountId": acc,
			"create":    map[string]any{"d": email},
		}, "s"},
	})
	if err != nil {
		return "", err
	}
	if len(resp) < 1 {
		return "", fmt.Errorf("jmap user: no create response")
	}
	var got struct {
		Created map[string]struct {
			ID string `json:"id"`
		} `json:"created"`
		NotCreated map[string]json.RawMessage `json:"notCreated"`
	}
	if err := json.Unmarshal(resp[0], &got); err != nil {
		return "", err
	}
	if len(got.NotCreated) > 0 {
		return "", fmt.Errorf("jmap user: draft rejected: %s", resp[0])
	}
	created, ok := got.Created["d"]
	if !ok || created.ID == "" {
		return "", fmt.Errorf("jmap user: draft created without id")
	}
	return created.ID, nil
}

// mailboxByRole resolves a folder's id by its JMAP role (inbox, drafts, sent, trash).
func (u *UserClient) mailboxByRole(ctx context.Context, acc, role string) (string, error) {
	resp, err := u.jmap(ctx, []any{
		[]any{"Mailbox/get", map[string]any{"accountId": acc, "ids": nil, "properties": []string{"role"}}, "m"},
	})
	if err != nil {
		return "", err
	}
	if len(resp) < 1 {
		return "", fmt.Errorf("jmap user: no mailbox response")
	}
	var list struct {
		List []struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"list"`
	}
	if err := json.Unmarshal(resp[0], &list); err != nil {
		return "", err
	}
	for _, m := range list.List {
		if m.Role == role {
			return m.ID, nil
		}
	}
	return "", fmt.Errorf("jmap user: no %s mailbox", role)
}

// primaryIdentity returns the account's sending identity: id, address and "From:" name. The assistant
// NEVER picks the sender: it sends as the authenticated person and nothing else (§6.2). The name is
// needed so the "From:" does not go out empty on the e-mail — without it, the recipient just saw "—".
func (u *UserClient) primaryIdentity(ctx context.Context, acc string) (id, email, name string, err error) {
	raw, err := u.do(ctx, "/jmap/", map[string]any{
		"using": []string{"urn:ietf:params:jmap:core", "urn:ietf:params:jmap:submission"},
		"methodCalls": []any{
			[]any{"Identity/get", map[string]any{"accountId": acc}, "i"},
		},
	})
	if err != nil {
		return "", "", "", err
	}
	var responses [][]json.RawMessage
	if err := json.Unmarshal(raw["methodResponses"], &responses); err != nil {
		return "", "", "", err
	}
	if len(responses) == 0 || len(responses[0]) < 2 {
		return "", "", "", fmt.Errorf("jmap user: no identity response")
	}
	var got struct {
		List []struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"list"`
	}
	if err := json.Unmarshal(responses[0][1], &got); err != nil {
		return "", "", "", err
	}
	if len(got.List) == 0 {
		return "", "", "", fmt.Errorf("jmap user: account has no send identity")
	}
	return got.List[0].ID, got.List[0].Email, got.List[0].Name, nil
}

// fromAddr builds the "From:" address in JMAP form. Empty name → address only (better than "—").
func fromAddr(email, name string) map[string]any {
	a := map[string]any{"email": email}
	if strings.TrimSpace(name) != "" {
		a["name"] = name
	}
	return a
}

// addrList translates text addresses into JMAP form. Accepts "Name <a@b>" and "a@b".
func addrList(in []string) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, raw := range in {
		name, email := splitAddr(raw)
		a := map[string]any{"email": email}
		if name != "" {
			a["name"] = name
		}
		out = append(out, a)
	}
	return out
}

func splitAddr(raw string) (name, email string) {
	s := strings.TrimSpace(raw)
	open := strings.LastIndex(s, "<")
	close := strings.LastIndex(s, ">")
	if open >= 0 && close > open {
		return strings.TrimSpace(strings.Trim(s[:open], ` "`)), strings.TrimSpace(s[open+1 : close])
	}
	return "", s
}
