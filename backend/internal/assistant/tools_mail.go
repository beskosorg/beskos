package assistant

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/beskosorg/beskos/backend/internal/mailbox"
)

// MailBox is the minimum the mail tools need. *mailbox.UserClient satisfies it; tests pass a
// double. The engine never builds one itself: it neither knows how to decrypt the JMAP
// credential nor should it.
type MailBox interface {
	SearchEmails(ctx context.Context, query string, limit int) ([]mailbox.EmailHeader, error)
	ReadEmail(ctx context.Context, id string) (mailbox.EmailBody, error)
	// CreateDraft saves without sending; SendEmail sends. They are distinct methods because the
	// difference between the two IS the human-confirmation boundary (§6.3), and a flag on a single
	// method would make that boundary invisible in the signature.
	CreateDraft(ctx context.Context, d mailbox.Draft) (string, error)
	SendEmail(ctx context.Context, d mailbox.Draft) (string, error)
}

// MailAccess resolves the user's mailbox from the Identity — and ONLY from the Identity. Resolving
// by identity, rather than taking an already-built client, is what lets background tasks run
// without an HTTP request, and what guarantees the opened mailbox is always the authenticated
// user's.
type MailAccess func(ctx context.Context, id Identity) (MailBox, error)

// mailSearchArgs — note there is no tenant_id or user_id, and there never will be (§6.2).
type mailSearchArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type mailReadArgs struct {
	MessageID string `json:"message_id"`
}

func mailTools(access MailAccess) []Tool {
	return []Tool{
		{
			Name: ToolMailSearch,
			Description: "Search the signed-in user's mailbox by free text. Matches sender, " +
				"subject and body. Returns message headers, newest first, without the body: use " +
				ToolMailRead + " to read one. Leave query empty for the most recent messages.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {"type": "string", "description": "Free-text search terms. Empty for most recent."},
					"limit": {"type": "integer", "description": "How many messages to return (1-50, default 20)."}
				}
			}`),
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a mailSearchArgs
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				box, err := access(ctx, id)
				if err != nil {
					return Result{}, fmt.Errorf("mailbox unavailable: %w", err)
				}
				hits, err := box.SearchEmails(ctx, a.Query, a.Limit)
				if err != nil {
					return Result{}, fmt.Errorf("mail search failed: %w", err)
				}
				return Result{
					Summary: fmt.Sprintf("%d messages", len(hits)),
					Data:    hits,
				}, nil
			},
		},
		{
			Name: ToolMailRead,
			Description: "Read one message from the signed-in user's mailbox: headers plus the " +
				"plain-text body. The message_id comes from a previous " + ToolMailSearch + " call.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"message_id": {"type": "string", "description": "Message id returned by ` + ToolMailSearch + `."}
				},
				"required": ["message_id"]
			}`),
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a mailReadArgs
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				if a.MessageID == "" {
					return Result{}, fmt.Errorf("message_id is required")
				}
				box, err := access(ctx, id)
				if err != nil {
					return Result{}, fmt.Errorf("mailbox unavailable: %w", err)
				}
				msg, err := box.ReadEmail(ctx, a.MessageID)
				if err != nil {
					return Result{}, fmt.Errorf("mail read failed: %w", err)
				}
				subject := msg.Subject
				if subject == "" {
					subject = "(no subject)"
				}
				return Result{Summary: subject, Data: msg}, nil
			},
		},
	}
}
