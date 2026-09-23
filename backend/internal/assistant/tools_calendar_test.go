package assistant

import (
	"context"
	"testing"

	"github.com/beskosorg/beskos/backend/internal/ai"
)

// recordingCal records the events it was asked to create, so tests can assert what was (or
// wasn't) created.
type recordingCal struct{ created []NewEvent }

func (c *recordingCal) create(_ context.Context, _ Identity, ev NewEvent) (EventRef, error) {
	c.created = append(c.created, ev)
	return EventRef{ID: "ev-1", Title: ev.Title}, nil
}

func calRegistry(cal *recordingCal) *Registry { return BuildRegistry(Deps{Calendar: cal.create}) }

func calCall(args string) ai.Response {
	return ai.Response{ToolCalls: []ai.ToolCall{{ID: "c1", Name: ToolCalendarCreate, Arguments: args}}, FinishReason: "tool_calls"}
}

// A SOLO event stays on the owner's own calendar: it doesn't leave the organisation and doesn't
// ask for confirmation — requiring it here would spend the confirmer's attention on what doesn't
// matter.
func TestCalendarSoloEventNeedsNoConfirmation(t *testing.T) {
	cal := &recordingCal{}
	conf := &scriptedConfirmer{approve: false} // would refuse, if asked
	args := `{"title":"Foco","starts_at":"2026-07-22T10:00:00Z"}`
	eng := NewEngine(&fakeProvider{script: []ai.Response{calCall(args), textResp("criado")}},
		calRegistry(cal), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "cria um bloco de foco")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(conf.asked) != 0 {
		t.Errorf("solo event asked for confirmation (%d) — it shouldn't have", len(conf.asked))
	}
	if len(cal.created) != 1 {
		t.Fatalf("events created = %d, want 1", len(cal.created))
	}
	if run.Steps[0].ErrorCode != "" {
		t.Errorf("step with error: %+v", run.Steps[0])
	}
}

// INVARIANT (§6.3): inviting notifies ANOTHER person, so it asks for confirmation. If declined,
// the event is NOT created — no one gets invited without approval.
func TestCalendarWithGuestsRequiresConfirmation(t *testing.T) {
	cal := &recordingCal{}
	conf := &scriptedConfirmer{approve: false}
	args := `{"title":"Reunião","starts_at":"2026-07-22T10:00:00Z","invitees":["bob@acme.pt"]}`
	eng := NewEngine(&fakeProvider{script: []ai.Response{calCall(args), textResp("não criei")}},
		calRegistry(cal), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "marca reunião com o bob")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(conf.asked) != 1 {
		t.Errorf("invites sent to confirmation = %d, want 1", len(conf.asked))
	}
	if len(cal.created) != 0 {
		t.Fatalf("INVITED WITHOUT APPROVAL: %+v", cal.created)
	}
	if run.Steps[0].ErrorCode != "assistant.confirm_declined" {
		t.Errorf("step = %+v, want the refusal recorded", run.Steps[0])
	}
}

// Approved, it creates the event WITH the guests that were shown.
func TestCalendarWithGuestsApprovedCreatesWithInvitees(t *testing.T) {
	cal := &recordingCal{}
	conf := &scriptedConfirmer{approve: true}
	args := `{"title":"Reunião","starts_at":"2026-07-22T10:00:00Z","invitees":["bob@acme.pt"]}`
	eng := NewEngine(&fakeProvider{script: []ai.Response{calCall(args), textResp("criado")}},
		calRegistry(cal), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	if _, err := eng.Run(context.Background(), testIdentity(), ag, "marca reunião com o bob"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(conf.asked) != 1 {
		t.Errorf("confirmations = %d, want 1", len(conf.asked))
	}
	if len(cal.created) != 1 || len(cal.created[0].Invitees) != 1 || cal.created[0].Invitees[0] != "bob@acme.pt" {
		t.Fatalf("event created without the right guests: %+v", cal.created)
	}
}
