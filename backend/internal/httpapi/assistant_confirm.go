package httpapi

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/assistant"
)

// assistantConfirmWait is how long an action waits for a decision. Once the deadline passes, it is
// REJECTED — never approved by default. A send that went out because no one answered would be
// exactly the failure mode the confirmation exists to prevent.
const assistantConfirmWait = 5 * time.Minute

// assistantConfirms holds actions waiting for a decision.
//
// In-memory on purpose: execution today is synchronous and lives inside a single HTTP request, so
// it dies with the server anyway. Durable waiting — a task that stays "awaiting confirmation" even
// across a restart — only makes sense once execution moves to a background worker.
type assistantConfirms struct {
	mu      sync.Mutex
	pending map[uuid.UUID]pendingConfirm
}

type pendingConfirm struct {
	tenant uuid.UUID
	user   uuid.UUID
	// reply has buffer 1: whoever decides never blocks, even if the request has already given up.
	reply chan bool
}

func newAssistantConfirms() *assistantConfirms {
	return &assistantConfirms{pending: make(map[uuid.UUID]pendingConfirm)}
}

// Ask registers the action and waits for the decision. Implements assistant.Confirmer.
//
// Three ways to end, and two of them are "no": an explicit decision, the user closed the tab
// (context cancelled), or the deadline ran out. Only the first can approve.
func (c *assistantConfirms) Ask(ctx context.Context, id assistant.Identity, req assistant.ConfirmRequest) (bool, error) {
	reply := make(chan bool, 1)
	c.mu.Lock()
	c.pending[req.ID] = pendingConfirm{tenant: id.TenantID, user: id.UserID, reply: reply}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, req.ID)
		c.mu.Unlock()
	}()

	timer := time.NewTimer(assistantConfirmWait)
	defer timer.Stop()
	select {
	case ok := <-reply:
		return ok, nil
	case <-ctx.Done():
		return false, ctx.Err()
	case <-timer.C:
		return false, nil
	}
}

// decide delivers the decision. Returns false when the action doesn't exist (already decided,
// expired, or belongs to someone else) — and doesn't distinguish between those cases: telling
// someone who guessed an id "it exists but it's not yours" would already be too much information.
func (c *assistantConfirms) decide(id uuid.UUID, tenant, user uuid.UUID, approve bool) bool {
	c.mu.Lock()
	p, ok := c.pending[id]
	c.mu.Unlock()
	if !ok || p.tenant != tenant || p.user != user {
		return false
	}
	select {
	case p.reply <- approve:
		return true
	default:
		// A decision was already on its way. Approving twice doesn't send twice.
		return false
	}
}

type assistantConfirmReq struct {
	Approve bool `json:"approve"`
}

// handleAssistantConfirm approves or rejects ONE action (§6.3). One per request: there's no bulk
// approval, and it's deliberate — every outgoing thing is seen and approved on its own.
func (s *Server) handleAssistantConfirm(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "assistant.confirm_unknown")
		return
	}
	var req assistantConfirmReq
	if !decode(w, r, &req) {
		return
	}
	// Two possible waits, and the order barely matters since an id only exists in one of them: the
	// SYNCHRONOUS execution (in-memory, in this process) and a background TASK (in the database,
	// polled from the other side by the worker).
	if !s.confirms.decide(id, mu.TenantID, mu.ID, req.Approve) &&
		!s.decideDurablePendingAction(r, id, req.Approve) {
		writeErr(w, http.StatusNotFound, "assistant.confirm_unknown")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "approved": req.Approve})
}
