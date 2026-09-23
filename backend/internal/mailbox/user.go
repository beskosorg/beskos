// Package mailbox speaks JMAP as an END USER (not the admin), using the mailbox's Basic credential
// (the same one the broker keeps encrypted). Used by server-side jobs that need to touch the
// user's mailbox without them being present — e.g. returning a "snoozed" e-mail to the Inbox.
package mailbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type UserClient struct {
	base  string // e.g.: http://stalwart:8080
	basic string // base64(email:password) — the decrypted jmap_cred value
	http  *http.Client
}

func NewUserClient(base, basic string) *UserClient {
	return &UserClient{base: base, basic: basic, http: &http.Client{Timeout: 20 * time.Second}}
}

func (u *UserClient) do(ctx context.Context, path string, body any) (map[string]json.RawMessage, error) {
	var reader *bytes.Reader
	method := http.MethodGet
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
		method = http.MethodPost
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.base+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Basic "+u.basic)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := u.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jmap user: status %d", resp.StatusCode)
	}
	var out map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (u *UserClient) accountID(ctx context.Context) (string, error) {
	raw, err := u.do(ctx, "/jmap/session", nil)
	if err != nil {
		return "", err
	}
	var pa map[string]string
	if err := json.Unmarshal(raw["primaryAccounts"], &pa); err != nil {
		return "", err
	}
	if id := pa["urn:ietf:params:jmap:mail"]; id != "" {
		return id, nil
	}
	return "", fmt.Errorf("jmap user: no mail account")
}

// inboxID resolves the account's Inbox id (role inbox).
func (u *UserClient) inboxID(ctx context.Context, acc string) (string, error) {
	resp, err := u.jmap(ctx, []any{
		[]any{"Mailbox/get", map[string]any{"accountId": acc, "ids": nil, "properties": []string{"role"}}, "m"},
	})
	if err != nil {
		return "", err
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
		if m.Role == "inbox" {
			return m.ID, nil
		}
	}
	return "", fmt.Errorf("jmap user: no inbox")
}

// jmap makes a /jmap/ call and returns the arguments of each methodResponse (in order).
func (u *UserClient) jmap(ctx context.Context, calls []any) ([]json.RawMessage, error) {
	raw, err := u.do(ctx, "/jmap/", map[string]any{
		"using":       []string{"urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail"},
		"methodCalls": calls,
	})
	if err != nil {
		return nil, err
	}
	var responses [][]json.RawMessage
	if err := json.Unmarshal(raw["methodResponses"], &responses); err != nil {
		return nil, err
	}
	out := make([]json.RawMessage, 0, len(responses))
	for _, m := range responses {
		if len(m) >= 2 {
			out = append(out, m[1]) // the arguments object
		}
	}
	return out, nil
}

// InboxMsg is the minimum needed for a new-e-mail notification.
type InboxMsg struct {
	From       string
	Subject    string
	ReceivedAt time.Time
}

// InboxRecent returns the Inbox's most recent messages (newest first). The worker
// filters for the ones that arrived after the last check.
func (u *UserClient) InboxRecent(ctx context.Context, limit int) ([]InboxMsg, error) {
	acc, err := u.accountID(ctx)
	if err != nil {
		return nil, err
	}
	inbox, err := u.inboxID(ctx, acc)
	if err != nil {
		return nil, err
	}
	resp, err := u.jmap(ctx, []any{
		[]any{"Email/query", map[string]any{
			"accountId": acc, "filter": map[string]any{"inMailbox": inbox},
			"sort": []any{map[string]any{"property": "receivedAt", "isAscending": false}}, "limit": limit,
		}, "q"},
		[]any{"Email/get", map[string]any{
			"accountId": acc, "#ids": map[string]any{"resultOf": "q", "name": "Email/query", "path": "/ids"},
			"properties": []string{"from", "subject", "receivedAt"},
		}, "g"},
	})
	if err != nil || len(resp) < 2 {
		return nil, err
	}
	var got struct {
		List []struct {
			Subject    string `json:"subject"`
			ReceivedAt string `json:"receivedAt"`
			From       []struct {
				Name  string `json:"name"`
				Email string `json:"email"`
			} `json:"from"`
		} `json:"list"`
	}
	if err := json.Unmarshal(resp[1], &got); err != nil {
		return nil, err
	}
	out := make([]InboxMsg, 0, len(got.List))
	for _, e := range got.List {
		when, _ := time.Parse(time.RFC3339, e.ReceivedAt)
		from := ""
		if len(e.From) > 0 {
			from = e.From[0].Name
			if from == "" {
				from = e.From[0].Email
			}
		}
		out = append(out, InboxMsg{From: from, Subject: e.Subject, ReceivedAt: when})
	}
	return out, nil
}

// SubmitEmail sends an already-existing draft (EmailSubmission/set) and, on success, MOVES the
// e-mail to the Sent folder. Used by the server-side outbox and by the assistant.
//
// It used to use onSuccessDestroyEmail, which deleted the e-mail — result: the sent message
// vanished (Stalwart does not keep a Sent copy on its own). It now uses onSuccessUpdateEmail to
// move it from Drafts to Sent (removes $draft, sets $seen). With no Sent folder resolved, it falls
// back to destroying it as before — better to lose the copy than leave the draft in the mailbox
// forever.
// Errors if Stalwart refuses the submission (notCreated) — the worker marks the outbox as 'failed'.
func (u *UserClient) SubmitEmail(ctx context.Context, emailID, identityID, from string, rcpts []string) error {
	acc, err := u.accountID(ctx)
	if err != nil {
		return err
	}
	rcptTo := make([]map[string]any, 0, len(rcpts))
	for _, r := range rcpts {
		rcptTo = append(rcptTo, map[string]any{"email": r})
	}
	submit := map[string]any{
		"accountId": acc,
		"create": map[string]any{
			"sub": map[string]any{
				"emailId":    emailID,
				"identityId": identityID,
				"envelope": map[string]any{
					"mailFrom": map[string]any{"email": from},
					"rcptTo":   rcptTo,
				},
			},
		},
	}
	// Keeps the copy in Sent. If the folder does not exist, destroys it (old behavior).
	if sent, err := u.mailboxByRole(ctx, acc, "sent"); err == nil && sent != "" {
		submit["onSuccessUpdateEmail"] = map[string]any{
			"#sub": map[string]any{
				"mailboxIds":      map[string]any{sent: true}, // leaves Drafts, enters Sent
				"keywords/$draft": nil,                        // no longer a draft
				"keywords/$seen":  true,                       // sent by me → read
			},
		}
	} else {
		submit["onSuccessDestroyEmail"] = []string{"#sub"}
	}
	raw, err := u.do(ctx, "/jmap/", map[string]any{
		"using":       []string{"urn:ietf:params:jmap:core", "urn:ietf:params:jmap:mail", "urn:ietf:params:jmap:submission"},
		"methodCalls": []any{[]any{"EmailSubmission/set", submit, "s"}},
	})
	if err != nil {
		return err
	}
	var responses [][]json.RawMessage
	if err := json.Unmarshal(raw["methodResponses"], &responses); err != nil {
		return err
	}
	if len(responses) == 0 || len(responses[0]) < 2 {
		return fmt.Errorf("jmap user: no submission response")
	}
	var res struct {
		NotCreated map[string]json.RawMessage `json:"notCreated"`
	}
	if err := json.Unmarshal(responses[0][1], &res); err != nil {
		return err
	}
	if len(res.NotCreated) > 0 {
		return fmt.Errorf("jmap user: submission rejected: %s", responses[0][1])
	}
	return nil
}

// MoveToInbox returns the message to the Inbox (mailboxIds = {inbox: true}). Idempotent: if the
// e-mail no longer exists, Email/set just ignores it (notUpdated), with no error for the worker.
func (u *UserClient) MoveToInbox(ctx context.Context, emailID string) error {
	acc, err := u.accountID(ctx)
	if err != nil {
		return err
	}
	inbox, err := u.inboxID(ctx, acc)
	if err != nil {
		return err
	}
	_, err = u.jmap(ctx, []any{
		[]any{"Email/set", map[string]any{
			"accountId": acc,
			"update":    map[string]any{emailID: map[string]any{"mailboxIds": map[string]any{inbox: true}}},
		}, "s"},
	})
	return err
}
