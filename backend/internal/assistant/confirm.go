package assistant

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// ConfirmRequest is an action awaiting human approval. Args are the EXACT arguments that will run
// if the person approves — the same bytes the handler receives, not a second version formatted
// for display. A preview that diverged from what actually runs would be worse than no confirmation
// at all: it would grant trust it hadn't earned.
type ConfirmRequest struct {
	ID   uuid.UUID       `json:"id"`
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

// Confirmer asks the human and returns the decision. It blocks until there's a response, the
// context is canceled, or the deadline expires.
//
// An error (timeout, lost connection) is NOT an approval: implementations must return false. It's
// the same rule the engine applies when there is no Confirmer at all — when in doubt, nothing goes out.
type Confirmer interface {
	Ask(ctx context.Context, id Identity, req ConfirmRequest) (bool, error)
}

// WithConfirmer returns a COPY of the engine with this confirmer wired in.
//
// A copy, not an in-place mutation, because the engine is shared: the worker needs a durable
// Confirmer (one that writes to the database) while the server uses its own, in-memory one. If
// this mutated the receiver, wiring one in would swap out the other's — and the confirmation for a
// background task would end up delivered to a process that isn't listening anymore.
//
// Without a Confirmer, the engine REFUSES every tool marked with Confirm instead of letting it
// run. This is deliberate: a missing security dependency has to close the door, not open it.
func (e *Engine) WithConfirmer(c Confirmer) *Engine {
	cp := *e
	cp.confirm = c
	return &cp
}

// confirmGate decides whether an outbound tool is allowed to run. Returns the i18n code of the
// refusal, or empty when it can proceed.
func (e *Engine) confirmGate(ctx context.Context, id Identity, tool Tool, args json.RawMessage, emit func(Event)) string {
	needs := tool.Confirm
	if tool.ConfirmWhen != nil {
		needs = tool.ConfirmWhen(args)
	}
	if !needs {
		return ""
	}
	if e.confirm == nil {
		return "assistant.confirm_unavailable"
	}
	req := ConfirmRequest{ID: uuid.New(), Tool: tool.Name, Args: args}
	if emit != nil {
		// The person sees the card BEFORE the wait starts — otherwise the panel would just sit
		// there without saying why.
		emit(Event{Kind: EventConfirm, Confirm: &req})
	}
	ok, err := e.confirm.Ask(ctx, id, req)
	if err != nil || !ok {
		return "assistant.confirm_declined"
	}
	return ""
}
