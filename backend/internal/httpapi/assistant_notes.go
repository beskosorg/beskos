package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/assistant"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/wiki"
)

// Adapters that connect the assistant's note tools to the database.
//
// THE PERMISSION IS RE-CHECKED HERE, on every call, from the Identity -- which comes from the
// session and never from the model's arguments (law 5.1). A note is private until shared, so a tool
// that trusted a note_id handed to it would be a way to read someone else's notes by guessing an id.
//
// The model receives PLAIN TEXT, never blocks and never HTML: it has no business knowing the editor
// format, and text is what it can actually reason about.

const assistantNoteLimit = 8

// assistantNotesSearch looks through the notes this person may see. The search is textual (ILIKE) on
// purpose: notes have no embeddings, and pretending to rank by meaning would be a lie about how the
// results were chosen.
func (s *Server) assistantNotesSearch(ctx context.Context, id assistant.Identity, query string, limit int) ([]assistant.NoteHit, error) {
	if limit <= 0 || limit > 50 {
		limit = assistantNoteLimit
	}
	rows, err := s.q.ListNotes(ctx, db.ListNotesParams{UserID: id.UserID, TenantID: id.TenantID, Lim: 500})
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	hits := make([]assistant.NoteHit, 0, limit)
	for _, n := range rows {
		if len(hits) >= limit {
			break
		}
		title := noteDisplayTitle(n.Title, n.MeetingTitle)
		if needle != "" && !strings.Contains(strings.ToLower(title+" "+n.BodyText), needle) {
			continue
		}
		hits = append(hits, assistant.NoteHit{
			NoteID: n.ID.String(), Title: title,
			Snippet: firstLine(n.BodyText, 240),
			Link:    "/mail/notes/" + n.ID.String(),
		})
	}
	return hits, nil
}

func (s *Server) assistantNotesRead(ctx context.Context, id assistant.Identity, noteID string) (assistant.NoteData, error) {
	nid, err := uuid.Parse(noteID)
	if err != nil {
		return assistant.NoteData{}, fmt.Errorf("invalid note id")
	}
	if rank, err := s.q.NoteRole(ctx, db.NoteRoleParams{UserID: id.UserID, NoteID: nid, TenantID: id.TenantID}); err != nil || rank <= 0 {
		// Same answer for "does not exist" and "not yours": anything else confirms it exists.
		return assistant.NoteData{}, fmt.Errorf("note not found")
	}
	n, err := s.q.GetNote(ctx, db.GetNoteParams{UserID: id.UserID, NoteID: nid, TenantID: id.TenantID})
	if err != nil {
		return assistant.NoteData{}, fmt.Errorf("note not found")
	}
	out := assistant.NoteData{
		NoteID: n.ID.String(), Title: noteDisplayTitle(n.Title, n.MeetingTitle),
		Text: n.BodyText, Link: "/mail/notes/" + n.ID.String(),
	}
	if n.MeetingTitle != nil {
		out.Meeting = *n.MeetingTitle
	}
	return out, nil
}

func (s *Server) assistantNotesCreate(ctx context.Context, id assistant.Identity, in assistant.NoteNew) (assistant.NoteData, error) {
	blocks := assistantBlocks(in.Content)
	text, _ := wiki.Derive(blocks)
	n, err := s.q.CreateNote(ctx, db.CreateNoteParams{
		TenantID: id.TenantID, OwnerUserID: id.UserID,
		Title: strings.TrimSpace(in.Title), BodyJson: blocks, BodyText: text,
	})
	if err != nil {
		return assistant.NoteData{}, err
	}
	return assistant.NoteData{NoteID: n.ID.String(), Title: n.Title, Text: n.BodyText, Link: "/mail/notes/" + n.ID.String()}, nil
}

func (s *Server) assistantNotesEdit(ctx context.Context, id assistant.Identity, in assistant.NoteEdit) (assistant.NoteData, error) {
	nid, err := uuid.Parse(in.NoteID)
	if err != nil {
		return assistant.NoteData{}, fmt.Errorf("invalid note id")
	}
	// Editor rank, not viewer: being allowed to read a shared note is not being allowed to rewrite it.
	if rank, err := s.q.NoteRole(ctx, db.NoteRoleParams{UserID: id.UserID, NoteID: nid, TenantID: id.TenantID}); err != nil || rank < noteRoleEditor {
		return assistant.NoteData{}, fmt.Errorf("note not found")
	}
	blocks := assistantBlocks(in.Content)
	text, _ := wiki.Derive(blocks)
	n, err := s.q.UpdateNote(ctx, db.UpdateNoteParams{
		NoteID: nid, TenantID: id.TenantID, Title: strings.TrimSpace(in.Title),
		BodyJson: blocks, BodyText: text, UpdatedBy: pgtype.UUID{Bytes: id.UserID, Valid: true},
	})
	if err != nil {
		return assistant.NoteData{}, err
	}
	return assistant.NoteData{NoteID: n.ID.String(), Title: n.Title, Text: n.BodyText, Link: "/mail/notes/" + n.ID.String()}, nil
}

// assistantBlocks turns the model's plain text into the editor's block array -- one paragraph per
// line, the same shape the Meet panel writes. The model never sees this format.
func assistantBlocks(text string) json.RawMessage {
	type inline struct {
		Type   string         `json:"type"`
		Text   string         `json:"text"`
		Styles map[string]any `json:"styles"`
	}
	type block struct {
		Type    string   `json:"type"`
		Content []inline `json:"content"`
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	blocks := make([]block, 0, len(lines))
	for _, l := range lines {
		b := block{Type: "paragraph", Content: []inline{}}
		if l != "" {
			b.Content = append(b.Content, inline{Type: "text", Text: l, Styles: map[string]any{}})
		}
		blocks = append(blocks, b)
	}
	raw, err := json.Marshal(blocks)
	if err != nil {
		return json.RawMessage("[]")
	}
	return raw
}

// noteDisplayTitle is what a note is called when it has no title of its own.
func noteDisplayTitle(title string, meeting *string) string {
	if t := strings.TrimSpace(title); t != "" {
		return t
	}
	if meeting != nil && strings.TrimSpace(*meeting) != "" {
		return strings.TrimSpace(*meeting)
	}
	return ""
}
