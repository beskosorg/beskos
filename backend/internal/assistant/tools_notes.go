package assistant

import (
	"context"
	"encoding/json"
	"fmt"
)

// Notes tools, built on the same shape as the wiki's: reading needs no approval, writing does.
//
// PERMISSION IS NOT DECIDED HERE. Every function below reaches an adapter that re-checks NoteRole
// for the caller, exactly as the HTTP handlers do -- the model never carries authority, it only
// asks. A note is private until shared, so a search that ignored this would be a way to read other
// people's notes by asking politely.
//
// CREATING A NOTE DOES NOT LEAVE THE ORGANISATION and reaches nobody else, so it does not ask for
// confirmation -- the same call the person could have made by clicking "new note". EDITING does:
// it overwrites text somebody wrote, possibly not the person talking to the assistant, and that is
// not undoable by looking at it afterwards.

// NoteHit is one note found by a search, trimmed for the model to cite.
type NoteHit struct {
	NoteID  string `json:"note_id"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	// Link is the in-app path, so the model can offer something the person can open.
	Link string `json:"link"`
}

// NoteData is a whole note handed to the model: plain text, never blocks and never HTML.
type NoteData struct {
	NoteID string `json:"note_id"`
	Title  string `json:"title"`
	Text   string `json:"text"`
	Link   string `json:"link"`
	// Meeting names the meeting this note belongs to, when it does. Empty for a personal note.
	Meeting string `json:"meeting,omitempty"`
}

// NoteNew and NoteEdit are the write requests. Content is plain text: the adapter turns it into the
// block format, so the model never has to know what BlockNote is.
type NoteNew struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type NoteEdit struct {
	NoteID  string `json:"note_id"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type (
	NotesSearch func(ctx context.Context, id Identity, query string, limit int) ([]NoteHit, error)
	NotesRead   func(ctx context.Context, id Identity, noteID string) (NoteData, error)
	NotesCreate func(ctx context.Context, id Identity, in NoteNew) (NoteData, error)
	NotesEdit   func(ctx context.Context, id Identity, in NoteEdit) (NoteData, error)
)

// notesTools builds the note tools from whichever functions are wired (nil = tool absent).
func notesTools(search NotesSearch, read NotesRead, create NotesCreate, edit NotesEdit) []Tool {
	var tools []Tool
	if search != nil {
		tools = append(tools, Tool{
			Name: ToolNotesSearch,
			Description: "Search the user's notes -- their own and the ones shared with them, " +
				"including meeting notes. Returns title, an excerpt and the note id. Use it to answer " +
				"from what was written down; cite the note.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {"type": "string", "description": "What to look for."},
					"limit": {"type": "integer", "description": "Max notes to return (default 8)."}
				},
				"required": ["query"]
			}`),
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a struct {
					Query string `json:"query"`
					Limit int    `json:"limit"`
				}
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				hits, err := search(ctx, id, a.Query, a.Limit)
				if err != nil {
					return Result{}, err
				}
				return Result{
					Summary: fmt.Sprintf("Searched notes for %q: %d result(s)", a.Query, len(hits)),
					Data:    hits,
				}, nil
			},
		})
	}
	if read != nil {
		tools = append(tools, Tool{
			Name:        ToolNotesRead,
			Description: "Read one note in full (plain text) by note_id. Only notes the user may see.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {"note_id": {"type": "string", "description": "The note id."}},
				"required": ["note_id"]
			}`),
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a struct {
					NoteID string `json:"note_id"`
				}
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				n, err := read(ctx, id, a.NoteID)
				if err != nil {
					return Result{}, err
				}
				return Result{Summary: "Read the note " + n.Title, Data: n}, nil
			},
		})
	}
	if create != nil {
		tools = append(tools, Tool{
			Name: ToolNotesCreate,
			Description: "Create a personal note for the user. It is PRIVATE: nobody else sees it " +
				"until the user shares it. Use it to write down what was asked to be kept.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"title": {"type": "string", "description": "Short title."},
					"content": {"type": "string", "description": "The body, plain text; blank lines separate paragraphs."}
				},
				"required": ["title", "content"]
			}`),
			Writes: true,
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a NoteNew
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				n, err := create(ctx, id, a)
				if err != nil {
					return Result{}, err
				}
				return Result{Summary: "Created the note " + n.Title, Data: n}, nil
			},
		})
	}
	if edit != nil {
		tools = append(tools, Tool{
			Name: ToolNotesEdit,
			Description: "Replace the title and body of an existing note. The whole body is " +
				"overwritten, so read the note first and send back the full text you want it to have.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"note_id": {"type": "string", "description": "The note id."},
					"title": {"type": "string", "description": "New title."},
					"content": {"type": "string", "description": "The COMPLETE new body, plain text."}
				},
				"required": ["note_id", "content"]
			}`),
			Writes: true,
			// Overwrites text the user -- or a colleague, on a shared note -- wrote. Approved with the
			// exact content in view, because "I rewrote your note" is not something you notice later.
			Confirm: true,
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a NoteEdit
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				n, err := edit(ctx, id, a)
				if err != nil {
					return Result{}, err
				}
				return Result{Summary: "Edited the note " + n.Title, Data: n}, nil
			},
		})
	}
	return tools
}
