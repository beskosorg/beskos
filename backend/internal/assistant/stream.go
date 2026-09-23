package assistant

import (
	"context"

	"github.com/beskosorg/beskos/backend/internal/ai"
)

// Event types emitted during a run. This is what the conversation shows live.
const (
	// EventDelta is a chunk of text from the final answer.
	EventDelta = "delta"
	// EventStep is a COMPLETED tool call — the action card (§3.3). Never a sentence saying what it
	// did: the record of the actual call, with the result or the error code.
	EventStep = "step"
	// EventConfirm asks for human approval for an action that goes outside or destroys something
	// (§6.3). The run STOPS here until there's a response — it's deliberately inconvenient, because
	// it's the only point where a human sees what's actually about to happen.
	EventConfirm = "confirm"
	// EventDone closes the run and carries the complete Run.
	EventDone = "done"
)

// Event is what the engine emits while it works.
type Event struct {
	Kind    string          `json:"kind"`
	Text    string          `json:"text,omitempty"`
	Step    *Step           `json:"step,omitempty"`
	Run     *Run            `json:"run,omitempty"`
	Confirm *ConfirmRequest `json:"confirm,omitempty"`
}

// complete performs one step of the loop. It uses streaming when the provider supports it AND
// there's someone listening; otherwise it falls back to Complete. The response is the same on
// both paths — the loop doesn't know which one ran, and that's what keeps streaming from becoming
// a second engine to maintain in parallel.
func (e *Engine) complete(ctx context.Context, req ai.Request, emit func(Event)) (ai.Response, error) {
	sp, ok := e.provider.(ai.StreamingProvider)
	if !ok || emit == nil {
		return e.provider.Complete(ctx, req)
	}
	return sp.Stream(ctx, req, func(chunk string) {
		emit(Event{Kind: EventDelta, Text: chunk})
	})
}

// historyMessages translates the persisted history into the model's thread.
//
// Two things are left out on purpose: empty messages (they say nothing and spend context) and
// anything that isn't user or assistant. In particular, TOOL RESULTS from previous turns don't go
// back to the model: they're third-party data from a moment that's already passed, and reinjecting
// them on every turn would give an email read yesterday a second chance to control the model
// (§6.1). What the assistant concluded from them is in its own reply.
func historyMessages(history []Turn) []ai.Message {
	out := make([]ai.Message, 0, len(history))
	for _, t := range history {
		if t.Content == "" {
			continue
		}
		switch t.Role {
		case string(ai.RoleUser):
			out = append(out, ai.Message{Role: ai.RoleUser, Content: t.Content})
		case string(ai.RoleAssistant):
			out = append(out, ai.Message{Role: ai.RoleAssistant, Content: t.Content})
		}
	}
	return out
}
