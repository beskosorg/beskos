package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/beskosorg/beskos/backend/internal/ai"
)

// Tool names. Constants because they appear in three places (catalog, agent permission list,
// audit log) and a typo between them would go unnoticed.
const (
	ToolMailSearch     = "mail.search"
	ToolMailRead       = "mail.read"
	ToolMailDraft      = "mail.draft"
	ToolMailSend       = "mail.send"
	ToolDriveSearch    = "drive.search"
	ToolDocsCreate     = "docs.create"
	ToolDocsRead       = "docs.read"
	ToolDocsEdit       = "docs.edit"
	ToolDocsShare      = "docs.share"
	ToolTasksCreate    = "tasks.create"
	ToolCalendarCreate = "calendar.create"
	ToolContactsSearch = "contacts.search"
	ToolContactsCreate = "contacts.create"
	ToolContactsShare  = "contacts.share"
	ToolWikiSearch     = "wiki.search"
	ToolWikiRead       = "wiki.read"
	ToolWikiCreatePage = "wiki.create_page"
	ToolWikiEditPage   = "wiki.edit_page"

	ToolNotesSearch = "notes.search"
	ToolNotesRead   = "notes.read"
	ToolNotesCreate = "notes.create"
	ToolNotesEdit   = "notes.edit"
)

// Result is what a tool returns.
type Result struct {
	// Summary is the line the conversation card shows: the record of the actual call, not a
	// sentence from the model claiming what it did (§3.3).
	Summary string
	// Data goes back to the model, always wrapped as untrusted content (§6.1).
	Data any
	// Choices, when present, are quick replies the frontend shows as BUTTONS (e.g. which of
	// several "Bob" contacts). Clicking sends Value as the next message — it saves the user from
	// typing the answer. It's extra to the conversation; it doesn't replace the model's text
	// question.
	Choices []Choice
}

// Choice is a quick-reply button: Label is what is shown, Value is what is sent when clicked.
type Choice struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Handler executes the tool. Receives the Identity of the request — it does NOT read it from
// args, which come from the model.
type Handler func(ctx context.Context, id Identity, args json.RawMessage) (Result, error)

// Tool is a typed function the model can invoke. It's the only way to touch data.
type Tool struct {
	Name        string
	Description string
	// Parameters is a JSON Schema of type "object". NEVER declares tenant_id or user_id: a
	// parameter that accepted a tenant would be a tenant-crossing bug waiting to happen (§6.2).
	// A test guarantees this.
	Parameters json.RawMessage
	// Writes marks write tools.
	Writes bool
	// Confirm marks whatever GOES OUTSIDE or DESTROYS: send, share, invite, delete, webhooks
	// (§6.3). A tool like this never runs without human approval with the exact content in view,
	// and approval is per-item — there is no bulk approval. It's the defense that doesn't depend
	// on the model behaving well, and that's why it's what backs the broad-scope decision
	// (§12.1, decision 1).
	Confirm bool
	// ConfirmWhen, when set, decides confirmation PER CALL from the args — for tools where only
	// SOME uses go outside. E.g.: creating a solo event is internal (no confirmation), but
	// creating an event WITH guests notifies another person (inviting → §6.3, confirms).
	// When set, it takes precedence over Confirm. When in doubt (unreadable args) returns true:
	// fail-closed.
	ConfirmWhen func(args json.RawMessage) bool
	Handler     Handler
}

// Registry is the catalog of tools available in the installation. It's not the same as an
// agent's permission list: the catalog is what exists, the list is what each agent may use.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry builds the catalog. Duplicate names are a programming error and panic at
// initialization — better to fail at startup than dispatch the wrong tool in production.
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(tools))}
	for _, t := range tools {
		if _, dup := r.tools[t.Name]; dup {
			panic("assistant: duplicate tool " + t.Name)
		}
		r.tools[t.Name] = t
	}
	return r
}

// Get returns a tool from the catalog.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Names returns the catalog's names, in order — useful for tests and for the audit screen.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.tools))
	for n := range r.tools {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Schemas translates to the provider's format the tools THIS agent may use. A tool requested by
// the agent but absent from the catalog is silently ignored: it means the installation doesn't
// have it configured (e.g. Drive without storage), not that there was an attack.
func (r *Registry) Schemas(allowed []string) []ai.Tool {
	out := make([]ai.Tool, 0, len(allowed))
	for _, name := range allowed {
		t, ok := r.tools[name]
		if !ok {
			continue
		}
		out = append(out, ai.Tool{Name: t.Name, Description: t.Description, Parameters: t.Parameters})
	}
	return out
}

// allows reports whether the agent has this tool in its permission list. The check runs on the
// server on every call, not in the prompt (§6.1, defense 2).
func allows(ag Agent, tool string) bool {
	for _, t := range ag.Tools {
		if t == tool {
			return true
		}
	}
	return false
}

// decodeArgs deserializes the model's arguments into the type the tool expects. Empty arguments
// are treated as an empty object: some models omit them when the schema has no required fields.
func decodeArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}
