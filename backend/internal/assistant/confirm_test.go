package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/beskosorg/beskos/backend/internal/ai"
	"github.com/beskosorg/beskos/backend/internal/mailbox"
	"github.com/beskosorg/beskos/backend/internal/office"
)

// recordingBox records what it was told to send, so it can be asserted that NOTHING went out.
type recordingBox struct {
	mu    sync.Mutex
	sent  []mailbox.Draft
	saved []mailbox.Draft
}

func (b *recordingBox) SearchEmails(context.Context, string, int) ([]mailbox.EmailHeader, error) {
	return nil, nil
}
func (b *recordingBox) ReadEmail(context.Context, string) (mailbox.EmailBody, error) {
	return mailbox.EmailBody{}, nil
}
func (b *recordingBox) CreateDraft(_ context.Context, d mailbox.Draft) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.saved = append(b.saved, d)
	return "draft-1", nil
}
func (b *recordingBox) SendEmail(_ context.Context, d mailbox.Draft) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sent = append(b.sent, d)
	return "sent-1", nil
}

// scriptedConfirmer always answers the same way and keeps track of what it was asked.
type scriptedConfirmer struct {
	approve bool
	err     error
	asked   []ConfirmRequest
}

func (c *scriptedConfirmer) Ask(_ context.Context, _ Identity, req ConfirmRequest) (bool, error) {
	c.asked = append(c.asked, req)
	return c.approve, c.err
}

func sendCall(args string) ai.Response {
	return ai.Response{ToolCalls: []ai.ToolCall{{ID: "s1", Name: ToolMailSend, Arguments: args}}, FinishReason: "tool_calls"}
}

const sendArgs = `{"to":["ana@example.com"],"subject":"Invoice","text":"Please find the invoice attached."}`

// INVARIANT (§6.3): nothing goes out without approval. If declined, the tool does NOT run — and
// the model receives the refusal as an observation, so it can tell the user instead of blindly retrying.
func TestSendRefusedWithoutApproval(t *testing.T) {
	box := &recordingBox{}
	conf := &scriptedConfirmer{approve: false}
	audit := &memAudit{}
	eng := NewEngine(&fakeProvider{script: []ai.Response{sendCall(sendArgs), textResp("I did not send it")}},
		mailRegistry(box), audit, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "send the invoice to Ana")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(box.sent) != 0 {
		t.Fatalf("MAIL WENT OUT WITHOUT APPROVAL: %+v", box.sent)
	}
	if len(run.Steps) != 1 || run.Steps[0].ErrorCode != "assistant.confirm_declined" {
		t.Errorf("step = %+v, want the refusal recorded as an i18n code", run.Steps)
	}
	// The refusal stays in the audit trail: "why was this e-mail NOT sent?" also needs an
	// answer (§6.4).
	if len(audit.entries) != 1 || audit.entries[0].Error == "" {
		t.Errorf("audit = %+v, want the refusal recorded", audit.entries)
	}
}

// FAIL-CLOSED: with no Confirmer wired in, an outbound tool is refused — not executed. A missing
// security dependency has to close the door, not open it.
func TestSendRefusedWithoutConfirmerAtAll(t *testing.T) {
	box := &recordingBox{}
	eng := NewEngine(&fakeProvider{script: []ai.Response{sendCall(sendArgs), textResp("ok")}},
		mailRegistry(box), &memAudit{}, 0) // no WithConfirmer
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "send it")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(box.sent) != 0 {
		t.Fatalf("MAIL WENT OUT WITH NO CONFIRMATION MECHANISM: %+v", box.sent)
	}
	if run.Steps[0].ErrorCode != "assistant.confirm_unavailable" {
		t.Errorf("code = %q, want assistant.confirm_unavailable", run.Steps[0].ErrorCode)
	}
}

// A Confirmer error (timeout, lost connection) is NOT an approval.
func TestConfirmerErrorIsNotApproval(t *testing.T) {
	box := &recordingBox{}
	conf := &scriptedConfirmer{approve: true, err: context.DeadlineExceeded}
	eng := NewEngine(&fakeProvider{script: []ai.Response{sendCall(sendArgs), textResp("ok")}},
		mailRegistry(box), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	if _, err := eng.Run(context.Background(), testIdentity(), ag, "send it"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(box.sent) != 0 {
		t.Fatalf("A CONFIRMATION ERROR LET MAIL OUT: %+v", box.sent)
	}
}

// Once approved, the tool runs — and runs with the EXACT arguments that were shown. A preview
// that diverged from what actually runs would grant trust it hadn't earned.
func TestApprovedSendRunsTheExactContentShown(t *testing.T) {
	box := &recordingBox{}
	conf := &scriptedConfirmer{approve: true}
	eng := NewEngine(&fakeProvider{script: []ai.Response{sendCall(sendArgs), textResp("sent")}},
		mailRegistry(box), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	var shown *ConfirmRequest
	_, err := eng.RunStream(context.Background(), testIdentity(), ag, RunOptions{Message: "send it"},
		func(ev Event) {
			if ev.Kind == EventConfirm {
				shown = ev.Confirm
			}
		})
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	if len(box.sent) != 1 {
		t.Fatalf("sent count = %d, want 1", len(box.sent))
	}
	if box.sent[0].To[0] != "ana@example.com" || box.sent[0].Text != "Please find the invoice attached." {
		t.Errorf("sent something else: %+v", box.sent[0])
	}
	if shown == nil {
		t.Fatal("the user never saw a confirmation card")
	}
	// The bytes shown are the bytes executed, not a second formatted version.
	var got, want map[string]any
	_ = json.Unmarshal(shown.Args, &got)
	_ = json.Unmarshal([]byte(sendArgs), &want)
	if got["text"] != want["text"] || got["subject"] != want["subject"] {
		t.Errorf("what was shown (%v) diverges from what ran (%v)", got, want)
	}
	if len(conf.asked) != 1 {
		t.Errorf("confirmation requests = %d, want exactly 1 (no batch approval)", len(conf.asked))
	}
}

// Every outgoing action is approved INDIVIDUALLY: two sends in one turn are two questions.
func TestEachOutgoingActionIsApprovedSeparately(t *testing.T) {
	box := &recordingBox{}
	conf := &scriptedConfirmer{approve: true}
	two := ai.Response{ToolCalls: []ai.ToolCall{
		{ID: "s1", Name: ToolMailSend, Arguments: sendArgs},
		{ID: "s2", Name: ToolMailSend, Arguments: `{"to":["bruno@example.com"],"text":"Hello"}`},
	}, FinishReason: "tool_calls"}
	eng := NewEngine(&fakeProvider{script: []ai.Response{two, textResp("done")}},
		mailRegistry(box), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	if _, err := eng.Run(context.Background(), testIdentity(), ag, "send to both"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(conf.asked) != 2 {
		t.Errorf("questions = %d, want 2 — there is no batch approval (§6.3)", len(conf.asked))
	}
}

// `mail.draft` saves without leaving the organization, so it does NOT ask for confirmation.
// Requiring it here would spend the confirmer's attention on what doesn't matter — and that
// attention is exactly what protects sending.
func TestDraftDoesNotAskForConfirmation(t *testing.T) {
	box := &recordingBox{}
	conf := &scriptedConfirmer{approve: false}
	draft := ai.Response{ToolCalls: []ai.ToolCall{{ID: "d1", Name: ToolMailDraft, Arguments: sendArgs}}, FinishReason: "tool_calls"}
	eng := NewEngine(&fakeProvider{script: []ai.Response{draft, textResp("draft ready")}},
		mailRegistry(box), &memAudit{}, 0).WithConfirmer(conf)
	ag, _ := Builtin(AgentGeneral)

	run, err := eng.Run(context.Background(), testIdentity(), ag, "write a draft")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(conf.asked) != 0 {
		t.Errorf("a draft doesn't leave the organization and should not ask for confirmation")
	}
	if len(box.saved) != 1 {
		t.Fatalf("saved drafts = %d, want 1", len(box.saved))
	}
	if len(box.sent) != 0 {
		t.Fatal("mail.draft SENT — the separation between saving and sending is what A2 rests on")
	}
	if run.Steps[0].ErrorCode != "" {
		t.Errorf("step with error: %+v", run.Steps[0])
	}
}

// The catalog has to keep the right marking: whatever leaves the organization carries Confirm,
// nothing else does. This test is what keeps a new tool from getting in without the marker.
func TestOutgoingToolsAreMarkedConfirm(t *testing.T) {
	reg := BuildRegistry(Deps{
		Mail:  func(context.Context, Identity) (MailBox, error) { return &recordingBox{}, nil },
		Drive: func(context.Context, Identity, string, int) ([]DriveHit, error) { return nil, nil },
		Docs:  func(context.Context, Identity, string, office.Plan) (DriveHit, error) { return DriveHit{}, nil },
		DocsShare: func(context.Context, Identity, string, []string, string) (ShareResult, error) {
			return ShareResult{}, nil
		},
		Tasks: func(context.Context, Identity, NewTask) (TaskRef, error) { return TaskRef{}, nil },
	})
	// Leaves the organization or destroys something → confirmation is mandatory. Sharing grants
	// another person access.
	mustConfirm := map[string]bool{ToolMailSend: true, ToolDocsShare: true}
	for _, name := range reg.Names() {
		tool, _ := reg.Get(name)
		if want := mustConfirm[name]; tool.Confirm != want {
			t.Errorf("%s: Confirm = %v, want %v", name, tool.Confirm, want)
		}
		// A tool that writes but doesn't leave the organization still has to be marked as a write.
		if strings.HasSuffix(name, ".search") || strings.HasSuffix(name, ".read") {
			if tool.Writes {
				t.Errorf("%s is read-only but is marked as a write", name)
			}
		}
	}
}

// WithConfirmer must not touch the source engine: the worker and the server share it, and each
// needs its own — one durable, the other in-memory. If it mutated, wiring one in would disconnect the other.
func TestWithConfirmerDoesNotMutateTheOriginal(t *testing.T) {
	base := NewEngine(&fakeProvider{script: []ai.Response{textResp("ok")}}, mailRegistry(&recordingBox{}), nil, 0)
	if base.confirm != nil {
		t.Fatal("the base engine already comes with a Confirmer")
	}
	a := base.WithConfirmer(&scriptedConfirmer{approve: true})
	b := base.WithConfirmer(&scriptedConfirmer{approve: false})

	if base.confirm != nil {
		t.Error("WithConfirmer changed the source engine")
	}
	if a.confirm == b.confirm {
		t.Error("the two copies share the same Confirmer")
	}
	// And the copy keeps everything else: a lost step cap here would be a silent security
	// failure (§9.1).
	if a.maxSteps != base.maxSteps || a.registry != base.registry || a.provider != base.provider {
		t.Error("the copy lost configuration from the source engine")
	}
}
