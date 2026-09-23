package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beskosorg/beskos/backend/internal/office"
)

// DocsCreate writes an Office file to the user's Drive and returns what was created.
//
// Receives the PLAN, not the bytes: the server is what builds the file (decision §12.1, point
// 2), and the model describes the content. That separation is what guarantees a valid Office
// file comes out, instead of text with the wrong extension.
type DocsCreate func(ctx context.Context, id Identity, name string, plan office.Plan) (DriveHit, error)

type docsCreateArgs struct {
	Kind   string         `json:"kind"`
	Name   string         `json:"name"`
	Title  string         `json:"title"`
	Blocks []office.Block `json:"blocks"`
	Sheets []office.Sheet `json:"sheets"`
	Slides []office.Slide `json:"slides"`
}

func docsTools(create DocsCreate) []Tool {
	return []Tool{
		{
			Name: ToolDocsCreate,
			Description: "Create a Word document, an Excel spreadsheet or a PowerPoint presentation " +
				"in the user's Drive, from a plan you write. Use `blocks` for a document, `sheets` " +
				"for a spreadsheet, `slides` for a presentation. The file is built on the server, so " +
				"you describe the content and never the file format.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"kind": {"type": "string", "enum": ["document", "spreadsheet", "presentation"]},
					"name": {"type": "string", "description": "File name, without extension."},
					"title": {"type": "string", "description": "Title shown inside the file."},
					"blocks": {
						"type": "array",
						"description": "Document content, in order.",
						"items": {
							"type": "object",
							"properties": {
								"style": {"type": "string", "enum": ["heading", "paragraph", "bullet"]},
								"text": {"type": "string"}
							},
							"required": ["text"]
						}
					},
					"sheets": {
						"type": "array",
						"description": "Spreadsheet tabs. The first row is usually the header.",
						"items": {
							"type": "object",
							"properties": {
								"name": {"type": "string"},
								"rows": {"type": "array", "items": {"type": "array", "items": {"type": "string"}}}
							},
							"required": ["rows"]
						}
					},
					"slides": {
						"type": "array",
						"description": "Presentation slides, in order.",
						"items": {
							"type": "object",
							"properties": {
								"title": {"type": "string"},
								"bullets": {"type": "array", "items": {"type": "string"}}
							},
							"required": ["title"]
						}
					}
				},
				"required": ["kind", "name"]
			}`),
			Writes: true,
			// No Confirm: the file stays in the user's own Drive and doesn't leave the
			// organisation. Sharing is what goes outside — and that's a different tool, with
			// confirmation (§6.3).
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a docsCreateArgs
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				if _, ok := office.Ext(a.Kind); !ok {
					return Result{}, fmt.Errorf("kind must be document, spreadsheet or presentation")
				}
				name := strings.TrimSpace(a.Name)
				if name == "" {
					return Result{}, fmt.Errorf("name is required")
				}
				plan := office.Plan{
					Kind: a.Kind, Title: strings.TrimSpace(a.Title),
					Blocks: a.Blocks, Sheets: a.Sheets, Slides: a.Slides,
				}
				if emptyPlan(plan) {
					// An empty file is almost always the model missing the schema, not the
					// user's actual request. Saying so gives it a chance to correct itself;
					// saving the empty file wouldn't.
					return Result{}, fmt.Errorf("the plan has no content: fill blocks, sheets or slides")
				}
				hit, err := create(ctx, id, name, plan)
				if err != nil {
					return Result{}, fmt.Errorf("document creation failed: %w", err)
				}
				return Result{
					Summary: hit.Name,
					Data:    map[string]any{"file_id": hit.ID, "name": hit.Name, "size": hit.Size},
				}, nil
			},
		},
	}
}

func emptyPlan(p office.Plan) bool {
	return len(p.Blocks) == 0 && len(p.Sheets) == 0 && len(p.Slides) == 0 && p.Title == ""
}
