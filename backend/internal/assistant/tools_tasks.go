package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// NewTask is a task to create on a Kanban board.
type NewTask struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	// Due is an ISO date (YYYY-MM-DD). Empty = no due date.
	Due string `json:"due"`
	// Board is the board NAME. Empty uses the tenant's first one — the model doesn't know board
	// ids, and forcing it to guess one would be asking it to make one up.
	Board string `json:"board"`
	// Assignee is the NAME or EMAIL of the responsible colleague (e.g. "Bob"). Empty = unassigned.
	Assignee string `json:"assignee"`
}

// TaskRef is the created task, as the action card shows it.
type TaskRef struct {
	ID       string `json:"id"`
	Board    string `json:"board"`
	Number   string `json:"number"`
	Assignee string `json:"assignee"` // resolved name of the assignee ("" = none)
}

// TaskCreate creates the task using the SAME queries as the equivalent handler (§5: no new SQL).
type TaskCreate func(ctx context.Context, id Identity, t NewTask) (TaskRef, error)

func taskTools(create TaskCreate) []Tool {
	return []Tool{
		{
			Name: ToolTasksCreate,
			Description: "Create a task on one of the user's Kanban boards. The task is internal to " +
				"the organisation: nothing is sent to anyone outside it.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"title": {"type": "string", "description": "Short title of the task."},
					"description": {"type": "string"},
					"due": {"type": "string", "description": "Due date as YYYY-MM-DD."},
					"board": {"type": "string", "description": "Board name. Leave empty for the default board."}
				},
				"required": ["title"]
			}`),
			Writes: true,
			// No Confirm: a task doesn't leave the organisation or destroy anything, and is
			// reversible. Confirmation is for what leaves or deletes (§6.3) — requiring it here
			// would spend the confirmer's attention on what doesn't matter, and that attention is
			// what protects everything else.
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var t NewTask
				if err := decodeArgs(raw, &t); err != nil {
					return Result{}, err
				}
				t.Title = strings.TrimSpace(t.Title)
				if t.Title == "" {
					return Result{}, fmt.Errorf("title is required")
				}
				ref, err := create(ctx, id, t)
				if err != nil {
					return Result{}, fmt.Errorf("task creation failed: %w", err)
				}
				summary := t.Title
				if ref.Number != "" {
					summary = ref.Number + " " + t.Title
				}
				return Result{Summary: summary, Data: ref}, nil
			},
		},
	}
}
