package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beskosorg/beskos/backend/internal/office"
)

// FileContent is what a file read returns. Version and Hash identify the exact version that was
// read: the next write validates against them (§5.1).
type FileContent struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"` // document | spreadsheet | presentation | other
	Text    string `json:"text"`
	Version int32  `json:"version"`
	Hash    string `json:"-"`
	// Editable is false for binaries (images, PDF): those aren't edited blindly, only replaced by
	// a new upload (§5.1). The model is told, and the edit tool refuses them regardless.
	Editable bool `json:"editable"`
}

// DocsRead reads the text content of a Drive file and its current version.
type DocsRead func(ctx context.Context, id Identity, fileID string) (FileContent, error)

// DocsEdit writes a NEW version of a file, from a plan, but only if the current version is still
// expectVersion. Returns the new version.
type DocsEdit func(ctx context.Context, id Identity, fileID string, plan office.Plan, expectVersion int32) (DriveHit, error)

func docsEditTools(read DocsRead, edit DocsEdit) []Tool {
	return []Tool{
		{
			Name: ToolDocsRead,
			Description: "Read a file from the user's Drive: its text content and current version. " +
				"You MUST read a file with this before editing it with " + ToolDocsEdit + ".",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {"file_id": {"type": "string", "description": "File id from drive.search."}},
				"required": ["file_id"]
			}`),
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a struct {
					FileID string `json:"file_id"`
				}
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				if strings.TrimSpace(a.FileID) == "" {
					return Result{}, fmt.Errorf("file_id is required")
				}
				fc, err := read(ctx, id, a.FileID)
				if err != nil {
					return Result{}, fmt.Errorf("read failed: %w", err)
				}
				// Records the read WITHIN THIS RUN: this is what docs.edit will require (§5.1, rule 1).
				MarkRead(ctx, fc.ID, ReadMark{Version: fc.Version, Hash: fc.Hash})
				return Result{Summary: fc.Name, Data: fc}, nil
			},
		},
		{
			Name: ToolDocsEdit,
			Description: "Replace the content of an existing Drive file with a new version, built " +
				"from a plan. You must have read the file with " + ToolDocsRead + " in this same " +
				"turn first. Each edit is a new version; the previous one is kept and can be restored.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"file_id": {"type": "string"},
					"kind": {"type": "string", "enum": ["document", "spreadsheet", "presentation"]},
					"title": {"type": "string"},
					"summary": {"type": "string", "description": "One line describing what you changed."},
					"blocks": {"type": "array", "items": {"type": "object", "properties": {"style": {"type": "string"}, "text": {"type": "string"}}, "required": ["text"]}},
					"sheets": {"type": "array", "items": {"type": "object", "properties": {"name": {"type": "string"}, "rows": {"type": "array", "items": {"type": "array", "items": {"type": "string"}}}}, "required": ["rows"]}},
					"slides": {"type": "array", "items": {"type": "object", "properties": {"title": {"type": "string"}, "bullets": {"type": "array", "items": {"type": "string"}}}, "required": ["title"]}}
				},
				"required": ["file_id", "kind"]
			}`),
			Writes: true,
			// No Confirm: the file is the user's own and doesn't leave the organisation. The
			// guard protecting this isn't confirmation — it's the new (reversible) version and
			// the history (§5.1, rule 2). A mistake costs a click to restore, not the file.
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a struct {
					FileID  string         `json:"file_id"`
					Kind    string         `json:"kind"`
					Title   string         `json:"title"`
					Summary string         `json:"summary"`
					Blocks  []office.Block `json:"blocks"`
					Sheets  []office.Sheet `json:"sheets"`
					Slides  []office.Slide `json:"slides"`
				}
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				if strings.TrimSpace(a.FileID) == "" {
					return Result{}, fmt.Errorf("file_id is required")
				}
				if _, ok := office.Ext(a.Kind); !ok {
					return Result{}, fmt.Errorf("kind must be document, spreadsheet or presentation")
				}
				// RULE 1 (§5.1): read before writing. Without a read in this run, the model would
				// write over content it never saw. This is an invariant, not advice.
				mark, seen := ReadMarkFor(ctx, a.FileID)
				if !seen {
					return Result{}, fmt.Errorf("you must read this file with %s before editing it", ToolDocsRead)
				}
				plan := office.Plan{
					Kind: a.Kind, Title: strings.TrimSpace(a.Title),
					Blocks: a.Blocks, Sheets: a.Sheets, Slides: a.Slides,
				}
				if emptyPlan(plan) {
					return Result{}, fmt.Errorf("the new content is empty")
				}
				// RULE 3 (§5.1): edit on top of the version that was read. The write carries the
				// read version; if the file changed in the meantime, the server refuses and no
				// one's work is lost.
				hit, err := edit(ctx, id, a.FileID, plan, mark.Version)
				if err != nil {
					return Result{}, fmt.Errorf("edit failed: %w", err)
				}
				// The read mark now points to the NEW version: a second edit in the same turn is
				// legitimate and must not trip the concurrency guard because of its own write.
				MarkRead(ctx, a.FileID, ReadMark{Version: hit.Version})
				// RULE 5 (§5.1): say what changed. The model's summary is what the card shows;
				// without it, "document updated" doesn't let anyone judge whether it went well.
				summary := strings.TrimSpace(a.Summary)
				if summary == "" {
					summary = "updated " + hit.Name
				}
				return Result{
					Summary: summary,
					Data:    map[string]any{"file_id": hit.ID, "name": hit.Name, "version": hit.Version},
				}, nil
			},
		},
	}
}
