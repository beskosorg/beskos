package assistant

import (
	"context"
	"encoding/json"
	"fmt"
)

// DriveHit is a Drive search result — file or folder.
type DriveHit struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"` // "file" | "folder"
	Size    int64  `json:"size,omitempty"`
	Folder  string `json:"folder_id,omitempty"`
	Version int32  `json:"version,omitempty"` // current version, filled in by docs.edit
}

// DriveSearch searches the authenticated user's Drive. Implemented by whoever already has the
// existing queries: the engine never talks to the database (§5 — tools sit on top of what already
// exists, never on new SQL).
type DriveSearch func(ctx context.Context, id Identity, query string, limit int) ([]DriveHit, error)

type driveSearchArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func driveTools(search DriveSearch) []Tool {
	return []Tool{
		{
			Name: ToolDriveSearch,
			Description: "Search the signed-in user's Drive for files and folders by name. " +
				"Returns names and ids, not file contents.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {"type": "string", "description": "Text to match against file and folder names."},
					"limit": {"type": "integer", "description": "How many results to return (1-50, default 20)."}
				},
				"required": ["query"]
			}`),
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a driveSearchArgs
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				if a.Query == "" {
					return Result{}, fmt.Errorf("query is required")
				}
				hits, err := search(ctx, id, a.Query, a.Limit)
				if err != nil {
					return Result{}, fmt.Errorf("drive search failed: %w", err)
				}
				return Result{Summary: fmt.Sprintf("%d results", len(hits)), Data: hits}, nil
			},
		},
	}
}

// Deps are the wiring to the deployment's capabilities. A nil field leaves that area's tools
// OUT of the catalog — the assistant says it cannot, instead of failing mid-run.
type Deps struct {
	Mail          MailAccess
	Drive         DriveSearch
	Docs          DocsCreate
	DocsRead      DocsRead
	DocsEdit      DocsEdit
	DocsShare     DocsShare
	Tasks         TaskCreate
	Calendar      EventCreate
	Contacts      ContactSearch
	ContactCreate ContactCreate
	ContactShare  ContactShare
	// Wiki (knowledge base, §7.5): read + write-with-confirmation. WikiSearch can be left nil
	// (no embeddings configured) without disabling the rest.
	WikiSearch     WikiSearch
	WikiRead       WikiRead
	WikiCreatePage WikiCreatePage
	WikiEditPage   WikiEditPage

	// Notes: private by default, so every one of these re-checks the caller's access in the adapter.
	NotesSearch NotesSearch
	NotesRead   NotesRead
	NotesCreate NotesCreate
	NotesEdit   NotesEdit
}

// BuildRegistry builds the catalog from whatever the deployment has wired up.
func BuildRegistry(d Deps) *Registry {
	var tools []Tool
	if d.Mail != nil {
		tools = append(tools, mailTools(d.Mail)...)
		tools = append(tools, mailWriteTools(d.Mail)...)
	}
	if d.Drive != nil {
		tools = append(tools, driveTools(d.Drive)...)
	}
	if d.Docs != nil {
		tools = append(tools, docsTools(d.Docs)...)
	}
	if d.DocsRead != nil && d.DocsEdit != nil {
		tools = append(tools, docsEditTools(d.DocsRead, d.DocsEdit)...)
	}
	if d.DocsShare != nil {
		tools = append(tools, docsShareTools(d.DocsShare)...)
	}
	if d.Tasks != nil {
		tools = append(tools, taskTools(d.Tasks)...)
	}
	if d.Calendar != nil {
		tools = append(tools, calendarTools(d.Calendar)...)
	}
	if d.Contacts != nil {
		tools = append(tools, contactTools(d.Contacts)...)
	}
	if d.ContactCreate != nil {
		tools = append(tools, contactCreateTools(d.ContactCreate)...)
	}
	if d.ContactShare != nil {
		tools = append(tools, contactShareTools(d.ContactShare)...)
	}
	if d.WikiRead != nil {
		tools = append(tools, wikiTools(d.WikiSearch, d.WikiRead, d.WikiCreatePage, d.WikiEditPage)...)
	}
	if d.NotesRead != nil {
		tools = append(tools, notesTools(d.NotesSearch, d.NotesRead, d.NotesCreate, d.NotesEdit)...)
	}
	return NewRegistry(tools...)
}
