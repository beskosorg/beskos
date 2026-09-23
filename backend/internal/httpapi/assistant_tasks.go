package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// assistantTaskListLimit is how many tasks the tab shows.
const assistantTaskListLimit = 50

type assistantTaskJSON struct {
	ID             uuid.UUID       `json:"id"`
	ConversationID uuid.UUID       `json:"conversation_id"`
	Agent          string          `json:"agent"`
	Message        string          `json:"message"`
	Status         string          `json:"status"`
	Steps          json.RawMessage `json:"steps"`
	Answer         string          `json:"answer,omitempty"`
	ErrorCode      string          `json:"error_code,omitempty"`
	Stopped        string          `json:"stopped,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

func taskJSON(t db.AssistantTask) assistantTaskJSON {
	steps := t.Steps
	if len(steps) == 0 || !json.Valid(steps) {
		steps = json.RawMessage("[]")
	}
	return assistantTaskJSON{
		ID: t.ID, ConversationID: t.ConversationID, Agent: t.Agent, Message: t.Message,
		Status: t.Status, Steps: steps, Answer: t.Answer, ErrorCode: t.ErrorCode,
		Stopped: t.Stopped, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

// handleAssistantTasks lists the tasks belonging to the user THEMSELVES, with progress. It's what
// feeds the Tasks tab and the bubble indicator (§3.6).
func (s *Server) handleAssistantTasks(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListAssistantTasks(r.Context(), db.ListAssistantTasksParams{
		TenantID: mu.TenantID, MailUserID: mu.ID, Lim: assistantTaskListLimit,
	})
	if err != nil {
		log.Printf("assistant: list tasks: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	items := make([]assistantTaskJSON, 0, len(rows))
	for _, t := range rows {
		items = append(items, taskJSON(t))
	}

	// Actions waiting for approval go along with it: a stopped task has to say what it's waiting
	// for, otherwise it's just "waiting" without saying for what (§6.3).
	pending, err := s.q.ListAssistantPendingActions(r.Context(), db.ListAssistantPendingActionsParams{
		TenantID: mu.TenantID, MailUserID: mu.ID,
	})
	if err != nil {
		log.Printf("assistant: list pending actions: %v", err)
	}
	waiting := make([]map[string]any, 0, len(pending))
	for _, p := range pending {
		args := p.Args
		if len(args) == 0 || !json.Valid(args) {
			args = json.RawMessage("{}")
		}
		waiting = append(waiting, map[string]any{
			"id": p.ID, "task_id": p.TaskID, "tool": p.Tool, "args": args,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "pending": waiting})
}

// handleAssistantTaskCancel requests the cancellation. It's a REQUEST, not an immediate effect:
// the task may be mid-call, and the worker sees the flag on the next step. What it has already
// done stays recorded (§7.5) — a cancelled task isn't a task without history.
func (s *Server) handleAssistantTaskCancel(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "assistant.unknown_task")
		return
	}
	if err := s.q.RequestAssistantTaskCancel(r.Context(), db.RequestAssistantTaskCancelParams{
		ID: id, TenantID: mu.TenantID, MailUserID: mu.ID,
	}); err != nil {
		log.Printf("assistant: cancel task: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelling"})
}

// startAssistantTask enqueues the request instead of running it inside the HTTP request. The
// conversation immediately returns a task card; the work continues in the worker (§3.6).
func (s *Server) startAssistantTask(w http.ResponseWriter, r *http.Request, turn assistantTurnState, msg string) {
	mu := mailUserFrom(r.Context())
	row, err := s.q.CreateAssistantTask(r.Context(), db.CreateAssistantTaskParams{
		TenantID: mu.TenantID, MailUserID: mu.ID, ConversationID: turn.conv.ID,
		Agent: turn.agent.Key, Message: msg,
	})
	if err != nil {
		log.Printf("assistant: create task: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"conversation_id": turn.conv.ID,
		"agent":           turn.agent.Key,
		"task":            taskJSON(row),
	})
}

// decideDurablePendingAction delivers the decision to a TASK action (the kind that survives
// restarts). Returns false when there's no pending action with this id for this user.
func (s *Server) decideDurablePendingAction(r *http.Request, id uuid.UUID, approve bool) bool {
	mu := mailUserFrom(r.Context())
	decision := "rejected"
	if approve {
		decision = "approved"
	}
	// The query filters by user AND requires `decision = ''`: the approval belongs to whoever owns
	// the action, and it's single-use — approving twice doesn't send it twice.
	if _, err := s.q.DecideAssistantPendingAction(r.Context(), db.DecideAssistantPendingActionParams{
		ID: id, TenantID: mu.TenantID, MailUserID: mu.ID, Decision: decision,
	}); err != nil {
		return false
	}
	return true
}
