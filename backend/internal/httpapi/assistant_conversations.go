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

// assistantConvListLimit is how many conversations the list shows. Enough for recent history;
// searching the history is not supported.
const assistantConvListLimit = 50

// assistantMsgLimit caps how much of a conversation is returned at once.
const assistantMsgLimit = 200

type assistantConvJSON struct {
	ID        uuid.UUID `json:"id"`
	Agent     string    `json:"agent"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
}

type assistantMsgJSON struct {
	ID   uuid.UUID `json:"id"`
	Role string    `json:"role"`
	// Agent is who answered. Empty for user messages and for ones recorded before agent
	// attribution was tracked.
	Agent     string          `json:"agent,omitempty"`
	Content   string          `json:"content"`
	Steps     json.RawMessage `json:"steps"`
	Stopped   string          `json:"stopped,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// handleAssistantConversations lists the history of the user THEMSELVES. The conversation is
// personal and isn't shared even within the same organization (§3.5) — hence the filter by (tenant, user).
func (s *Server) handleAssistantConversations(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListAssistantConversations(r.Context(), db.ListAssistantConversationsParams{
		TenantID: mu.TenantID, MailUserID: mu.ID, Lim: assistantConvListLimit,
	})
	if err != nil {
		log.Printf("assistant: list conversations: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	items := make([]assistantConvJSON, 0, len(rows))
	for _, c := range rows {
		items = append(items, assistantConvJSON{ID: c.ID, Agent: c.Agent, Title: c.Title, UpdatedAt: c.UpdatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleAssistantConversation returns a thread with its messages and action cards. Reopening a
// conversation has to show the same thing that was seen live, including the record of calls (§3.3).
func (s *Server) handleAssistantConversation(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "assistant.unknown_conversation")
		return
	}
	conv, err := s.q.GetAssistantConversation(r.Context(), db.GetAssistantConversationParams{
		ID: id, TenantID: mu.TenantID, MailUserID: mu.ID,
	})
	if err != nil {
		// "Doesn't exist" and "isn't yours" respond the same way: distinguishing them would tell
		// anyone guessing an id that it exists on another account.
		writeErr(w, http.StatusNotFound, "assistant.unknown_conversation")
		return
	}
	rows, err := s.q.ListAssistantMessages(r.Context(), db.ListAssistantMessagesParams{
		ConversationID: conv.ID, TenantID: mu.TenantID, MailUserID: mu.ID, Lim: assistantMsgLimit,
	})
	if err != nil {
		log.Printf("assistant: list messages: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	msgs := make([]assistantMsgJSON, 0, len(rows))
	for _, m := range rows {
		steps := m.Steps
		if len(steps) == 0 || !json.Valid(steps) {
			steps = json.RawMessage("[]")
		}
		msgs = append(msgs, assistantMsgJSON{
			ID: m.ID, Role: m.Role, Agent: m.Agent, Content: m.Content, Steps: steps,
			Stopped: m.Stopped, CreatedAt: m.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": conv.ID, "agent": conv.Agent, "pinned_agent": conv.PinnedAgent,
		"title": conv.Title, "messages": msgs,
	})
}

// handleAssistantConversationDelete deletes a thread. The messages go with it (ON DELETE CASCADE);
// the audit record does NOT — it keeps the conversation reference as NULL, because "why did this
// happen?" has to keep having an answer after the conversation is gone (§6.4).
func (s *Server) handleAssistantConversationDelete(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "assistant.unknown_conversation")
		return
	}
	if err := s.q.DeleteAssistantConversation(r.Context(), db.DeleteAssistantConversationParams{
		ID: id, TenantID: mu.TenantID, MailUserID: mu.ID,
	}); err != nil {
		log.Printf("assistant: delete conversation: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
