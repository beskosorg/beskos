package httpapi

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/assistant"
	"github.com/beskosorg/beskos/backend/internal/db"
)

// assistantMemoryInject loads the user's facts to inject into a run (§8.3).
//
// Marks them as used: this feeds the proposal to remove whatever never gets used (§8.4). With few
// facts they all go in; filtering by recent use for when there are many dozens is tuning work for
// later — today memory is small by nature and fits whole.
func (s *Server) assistantMemoryInject(ctx context.Context, id assistant.Identity) []assistant.Fact {
	rows, err := s.q.ListAssistantMemory(ctx, db.ListAssistantMemoryParams{
		TenantID: id.TenantID, MailUserID: id.UserID,
	})
	if err != nil {
		log.Printf("assistant: load memory: %v", err)
		return nil
	}
	facts := make([]assistant.Fact, 0, len(rows))
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		facts = append(facts, assistant.Fact{Text: r.Fact, Category: r.Category})
		ids = append(ids, r.ID)
	}
	if len(ids) > 0 {
		if err := s.q.TouchAssistantMemoryUsed(ctx, db.TouchAssistantMemoryUsedParams{
			MailUserID: id.UserID, Ids: ids,
		}); err != nil {
			log.Printf("assistant: touch memory: %v", err)
		}
	}
	return facts
}

// assistantExtractMemory runs the extraction pass over the USER'S MESSAGE and saves new facts.
// After each turn, cheap and separate (§8.2).
//
// The hard rule from §6.5 is baked into the signature: it receives `userMessage`, a string, and
// nothing else. There's no way for a tool result or the assistant's response to get in. Only the
// user writes to memory.
//
// Returns the saved facts and the refusals, so the handler can announce them to the user (§8.2):
// when it saves something, the conversation shows it; when it refuses, it says so.
func (s *Server) assistantExtractMemory(ctx context.Context, id assistant.Identity, userMessage string, sourceMsg pgtype.UUID) ([]assistant.Fact, []assistant.Refusal) {
	if s.ai == nil {
		return nil, nil
	}
	facts, refusals := assistant.ExtractMemory(ctx, s.ai, userMessage)
	for _, f := range facts {
		if _, err := s.q.CreateAssistantMemory(ctx, db.CreateAssistantMemoryParams{
			TenantID: id.TenantID, MailUserID: id.UserID,
			Fact: f.Text, Category: f.Category, SourceMessageID: sourceMsg,
		}); err != nil {
			log.Printf("assistant: save memory: %v", err)
		}
	}
	return facts, refusals
}

// --- Management screen (§3.5, §8.4) ---

type memoryJSON struct {
	ID         uuid.UUID  `json:"id"`
	Fact       string     `json:"fact"`
	Category   string     `json:"category"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// handleAssistantMemory lists everything the application knows about the user. Without this it's
// a store of personal data that grows without anyone seeing it — and GDPR grants the right to see
// and delete it (§3.5).
func (s *Server) handleAssistantMemory(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListAssistantMemory(r.Context(), db.ListAssistantMemoryParams{
		TenantID: mu.TenantID, MailUserID: mu.ID,
	})
	if err != nil {
		log.Printf("assistant: list memory: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	items := make([]memoryJSON, 0, len(rows))
	for _, r := range rows {
		m := memoryJSON{ID: r.ID, Fact: r.Fact, Category: r.Category, CreatedAt: r.CreatedAt}
		if r.LastUsedAt.Valid {
			t := r.LastUsedAt.Time
			m.LastUsedAt = &t
		}
		items = append(items, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type memoryWriteReq struct {
	Fact     string `json:"fact"`
	Category string `json:"category"`
}

// handleAssistantMemoryCreate lets the user add a fact BY HAND. It's the purest form of the §6.5
// rule: only the user writes to memory, and here they write it directly. Forbidden categories are
// still refused — not even an explicit request gets a password saved (§6.5).
func (s *Server) handleAssistantMemoryCreate(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req memoryWriteReq
	if !decode(w, r, &req) {
		return
	}
	fact := strings.TrimSpace(req.Fact)
	if fact == "" {
		writeErr(w, http.StatusBadRequest, "assistant.memory.empty")
		return
	}
	if code, bad := assistant.CheckForbidden(fact); bad {
		writeErr(w, http.StatusUnprocessableEntity, code)
		return
	}
	row, err := s.q.CreateAssistantMemory(r.Context(), db.CreateAssistantMemoryParams{
		TenantID: mu.TenantID, MailUserID: mu.ID, Fact: fact,
		Category: assistant.NormalizeCategory(req.Category),
	})
	if err != nil {
		log.Printf("assistant: create memory: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusCreated, memoryJSON{ID: row.ID, Fact: row.Fact, Category: row.Category, CreatedAt: row.CreatedAt})
}

func (s *Server) handleAssistantMemoryDelete(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "assistant.memory.unknown")
		return
	}
	if err := s.q.DeleteAssistantMemory(r.Context(), db.DeleteAssistantMemoryParams{
		ID: id, TenantID: mu.TenantID, MailUserID: mu.ID,
	}); err != nil {
		log.Printf("assistant: delete memory: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAssistantMemoryClear deletes everything at once (§8.4). The user's right.
func (s *Server) handleAssistantMemoryClear(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	if err := s.q.DeleteAllAssistantMemory(r.Context(), db.DeleteAllAssistantMemoryParams{
		TenantID: mu.TenantID, MailUserID: mu.ID,
	}); err != nil {
		log.Printf("assistant: clear memory: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
