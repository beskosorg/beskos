package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ShareResult is what sharing returns: the file and who gained access.
type ShareResult struct {
	File       string   `json:"file"`
	SharedWith []string `json:"shared_with"`
}

// DocsShare grants a Drive file to colleagues in the SAME tenant, by email. Errors if the file
// does not belong to the user. Everything is resolved server-side — the model only supplies the
// file id (from docs.create/drive.search) and the emails.
type DocsShare func(ctx context.Context, id Identity, fileID string, emails []string, role string) (ShareResult, error)

type docsShareArgs struct {
	FileID string   `json:"file_id"`
	Emails []string `json:"emails"`
	Role   string   `json:"role"`
}

func docsShareTools(share DocsShare) []Tool {
	return []Tool{
		{
			Name: ToolDocsShare,
			Description: "Share a file from the user's Drive with colleagues in the same organisation, " +
				"by email — they get access to open and download it. Use the file_id returned by " +
				ToolDocsCreate + " or " + ToolDriveSearch + ". role is \"reader\" (default) or " +
				"\"editor\". This grants another person access, so the user approves it first with the " +
				"file and the people in front of them. External addresses cannot be granted access this " +
				"way; only colleagues in the organisation.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"file_id": {"type": "string", "description": "Id of the file, from docs.create or drive.search."},
					"emails": {"type": "array", "items": {"type": "string"}, "description": "Colleague emails to share with."},
					"role": {"type": "string", "enum": ["reader", "editor"], "description": "Access level. Default reader."}
				},
				"required": ["file_id", "emails"]
			}`),
			Writes: true,
			// Sharing grants access to ANOTHER person: it leaves the owner's sphere → confirmation (§6.3).
			Confirm: true,
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a docsShareArgs
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				a.FileID = strings.TrimSpace(a.FileID)
				if a.FileID == "" {
					return Result{}, fmt.Errorf("file_id is required")
				}
				emails := cleanAddrs(a.Emails)
				if len(emails) == 0 {
					return Result{}, fmt.Errorf("at least one colleague email is required")
				}
				role := "reader"
				if strings.EqualFold(a.Role, "editor") {
					role = "editor"
				}
				res, err := share(ctx, id, a.FileID, emails, role)
				if err != nil {
					return Result{}, fmt.Errorf("share failed: %w", err)
				}
				return Result{
					Summary: fmt.Sprintf("%s shared with %s", res.File, strings.Join(res.SharedWith, ", ")),
					Data:    res,
				}, nil
			},
		},
	}
}
