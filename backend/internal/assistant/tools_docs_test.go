package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/beskosorg/beskos/backend/internal/ai"
	"github.com/beskosorg/beskos/backend/internal/office"
)

// docsRegistry builds a catalog with only docs.create, wired to a creator that records the plan.
func docsRegistry(capture *office.Plan) *Registry {
	return BuildRegistry(Deps{
		Docs: func(_ context.Context, _ Identity, name string, plan office.Plan) (DriveHit, error) {
			*capture = plan
			return DriveHit{ID: "f1", Name: name, Kind: "file", Size: 123}, nil
		},
	})
}

func docsCall(args string) ai.Response {
	return ai.Response{ToolCalls: []ai.ToolCall{{ID: "d1", Name: ToolDocsCreate, Arguments: args}}, FinishReason: "tool_calls"}
}

// docs.create builds the file server-side from a PLAN — the model describes the content, never
// the bytes. The plan that reaches the creator has to be exactly what the model asked for.
func TestDocsCreatePassesThePlanToTheServer(t *testing.T) {
	var got office.Plan
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		docsCall(`{"kind":"document","name":"Report","title":"Q1","blocks":[{"style":"heading","text":"Summary"},{"style":"paragraph","text":"body"}]}`),
		textResp("created the document"),
	}}, docsRegistry(&got), &memAudit{}, 0)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "make a report")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.Kind != office.KindDocument || got.Title != "Q1" {
		t.Errorf("the plan didn't arrive as requested: %+v", got)
	}
	if len(got.Blocks) != 2 || got.Blocks[0].Text != "Summary" {
		t.Errorf("the blocks didn't arrive: %+v", got.Blocks)
	}
	if len(run.Steps) != 1 || run.Steps[0].Summary != "Report" {
		t.Errorf("the action card doesn't show the file: %+v", run.Steps)
	}
}

// An empty plan is not saved: it is almost always the model failing the schema, and reporting it
// gives it a chance to correct itself instead of leaving an empty file in Drive.
func TestDocsCreateRefusesEmptyPlan(t *testing.T) {
	var got office.Plan
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		docsCall(`{"kind":"presentation","name":"Empty"}`),
		textResp("couldn't do it"),
	}}, docsRegistry(&got), &memAudit{}, 0)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "make a presentation")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if run.Steps[0].ErrorCode == "" {
		t.Error("an empty plan should fail the tool, not save an empty file")
	}
	if got.Kind != "" {
		t.Error("the creator was called with an empty plan")
	}
}

// An unknown kind is refused in the tool, before touching storage.
func TestDocsCreateRefusesUnknownKind(t *testing.T) {
	var got office.Plan
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		docsCall(`{"kind":"pdf","name":"x","blocks":[{"text":"a"}]}`),
		textResp("can't do that"),
	}}, docsRegistry(&got), &memAudit{}, 0)
	ag, _ := Builtin(AgentGeneral)

	run, _ := eng.Run(context.Background(), testIdentity(), ag, "make a pdf")
	if run.Steps[0].ErrorCode == "" {
		t.Error("an unknown kind should fail the tool")
	}
}

// docs.create does NOT ask for confirmation: the file stays in the user's own Drive and doesn't
// leave the organisation. Sharing does leave, and that's a different tool (§6.3).
func TestDocsCreateDoesNotAskForConfirmation(t *testing.T) {
	var got office.Plan
	conf := &scriptedConfirmer{approve: false}
	eng := NewEngine(&fakeProvider{script: []ai.Response{
		docsCall(`{"kind":"document","name":"x","blocks":[{"text":"a"}]}`),
		textResp("done"),
	}}, docsRegistry(&got), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	run, _ := eng.Run(context.Background(), testIdentity(), ag, "make a document")
	if len(conf.asked) != 0 {
		t.Error("docs.create asked for confirmation — but it doesn't leave the organisation")
	}
	if run.Steps[0].ErrorCode != "" {
		t.Errorf("the tool failed: %+v", run.Steps[0])
	}
}

// The Docs agent can create files; the Mail agent cannot. Same per-agent tool isolation,
// now exercised with the new tool.
func TestDocsCreateIsScopedByAgent(t *testing.T) {
	docs, _ := Builtin(AgentDocs)
	if !allows(docs, ToolDocsCreate) {
		t.Error("the Docs agent cannot create files")
	}
	inbox, _ := Builtin(AgentInbox)
	if allows(inbox, ToolDocsCreate) {
		t.Error("the Mail agent can create files — it should be out of its scope")
	}
}

// The docs.create schema declares neither tenant_id nor user_id: identity comes from the
// request, as everywhere else (§6.2). Reinforces the general test with the new tool.
func TestDocsCreateSchemaHasNoTenant(t *testing.T) {
	reg := docsRegistry(&office.Plan{})
	tool, _ := reg.Get(ToolDocsCreate)
	schema := strings.ToLower(string(tool.Parameters))
	for _, f := range []string{"tenant", "user_id", "owner"} {
		if strings.Contains(schema, `"`+f+`"`) {
			t.Errorf("the docs.create schema declares %q", f)
		}
	}
	// And it is valid JSON Schema.
	var v any
	if err := json.Unmarshal(tool.Parameters, &v); err != nil {
		t.Errorf("the schema is not valid JSON: %v", err)
	}
}
