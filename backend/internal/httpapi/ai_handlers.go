package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/beskosorg/beskos/backend/internal/ai"
	"github.com/beskosorg/beskos/backend/internal/assistant"
)

// AI endpoints for email. The email content arrives in the request body (the frontend
// fetches it via JMAP directly). The AI key stays only here in the backend (law §3.5).
// Default language pt-BR (mail_users doesn't store a language preference yet).

// aiMaxBody limits the body of each AI request — the input goes to the paid API and is billable,
// so without a cap a user could trigger unlimited cost with huge bodies (audit M — cost abuse).
const aiMaxBody = 256 << 10 // 256 KiB

// localeFromRequest reads the PAGE's language (X-Locale header, e.g. "pt-BR", "en") so the AI
// generates content in that language. Sanitizes it (only letters and "-", 2-10 chars) — the value
// goes into a prompt, so it must never be able to inject instructions. Empty/invalid → pt-BR.
func localeFromRequest(r *http.Request) string {
	v := strings.TrimSpace(r.Header.Get("X-Locale"))
	if v == "" || len(v) > 10 {
		return "pt-BR"
	}
	for _, c := range v {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && c != '-' {
			return "pt-BR"
		}
	}
	return v
}

func (s *Server) assistant(w http.ResponseWriter, r *http.Request) (*ai.Assistant, bool) {
	if s.ai == nil {
		writeErr(w, http.StatusServiceUnavailable, "ai.disabled")
		return nil, false
	}
	// Rate limit per user (cost/abuse) + body cap. The limiter counts per mail_user.
	mu := mailUserFrom(r.Context())
	if !s.aiLimiter.allow("ai:"+mu.ID.String(), time.Now()) {
		writeErr(w, http.StatusTooManyRequests, "ai.rate_limited")
		return nil, false
	}
	// Token quota for the 6h window (the SAME one chat uses): these one-shot endpoints spend paid
	// tokens and must count against the limit — otherwise they'd be a way around it. Gated on
	// entry, with a per-call debit via SetUsageHook.
	id := assistant.Identity{TenantID: mu.TenantID, UserID: mu.ID, Email: mu.Email}
	if s.assistantQuotaStatus(r.Context(), id, time.Now()).Exceeded() {
		writeErr(w, http.StatusTooManyRequests, "assistant.quota_exceeded")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, aiMaxBody)
	// Language of the GENERATED content (briefing, summaries, tasks…) = the PAGE's language, sent
	// by the frontend in X-Locale. This way the summary follows the user's UI, not a fixed
	// language. (The chat is separate: it mirrors the language the person writes in — see
	// assistant/engine.go.)
	a := ai.NewAssistant(s.ai, localeFromRequest(r))
	ctx := r.Context()
	a.SetUsageHook(func(tokens int) { s.recordAssistantUsage(ctx, id, tokens) })
	return a, true
}

type aiEmailReq struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// handleAIPlanTask expands a short idea into a title + task description (Kanban).
func (s *Server) handleAIPlanTask(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assistant(w, r)
	if !ok {
		return
	}
	var req struct {
		Idea string `json:"idea"`
	}
	if !decode(w, r, &req) {
		return
	}
	draft, err := a.PlanTask(r.Context(), req.Idea)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ai.failed")
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

func (s *Server) handleAISummarize(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assistant(w, r)
	if !ok {
		return
	}
	var req aiEmailReq
	if !decode(w, r, &req) {
		return
	}
	out, err := a.SummarizeEmail(r.Context(), req.Subject, req.Body)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ai.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"summary": out})
}

type aiDraftReq struct {
	Original    string `json:"original"`
	Instruction string `json:"instruction"`
	Tone        string `json:"tone"` // neutro | formal | curto e direto
}

func (s *Server) handleAIDraft(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assistant(w, r)
	if !ok {
		return
	}
	var req aiDraftReq
	if !decode(w, r, &req) {
		return
	}
	out, err := a.DraftReply(r.Context(), req.Original, req.Instruction, ai.Tone(req.Tone))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ai.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"draft": out})
}

// handleAIWikiWrite generates wiki page content (Markdown) for the BlockNote editor: continue
// writing, improve, summarize, or a free-form instruction. Session + rate limit like the other
// AI routes.
func (s *Server) handleAIWikiWrite(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assistant(w, r)
	if !ok {
		return
	}
	var req struct {
		Instruction string `json:"instruction"`
		Context     string `json:"context"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Instruction) == "" {
		writeErr(w, http.StatusBadRequest, "ai.instruction_required")
		return
	}
	out, err := a.WikiWrite(r.Context(), req.Instruction, req.Context)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ai.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": out})
}

func (s *Server) handleAIExtractTasks(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assistant(w, r)
	if !ok {
		return
	}
	var req aiEmailReq
	if !decode(w, r, &req) {
		return
	}
	// The roster goes with the request so the model can answer with a real e-mail instead of echoing
	// whatever name appeared in the text. THIS tenant only: suggesting someone from another
	// organisation would reveal that they exist.
	mu := mailUserFrom(r.Context())
	people := []ai.Person{}
	if rows, err := s.q.ListColleagues(r.Context(), mu.TenantID); err == nil {
		for _, c := range rows {
			people = append(people, ai.Person{Name: c.Name, Email: c.Email})
		}
	}
	tasks, err := a.ExtractTasks(r.Context(), req.Subject, req.Body, people)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ai.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

type aiBriefingReq struct {
	Events   []string `json:"events"`
	Tasks    []string `json:"tasks"`
	Emails   []string `json:"emails"`
	Date     string   `json:"date"`
	Period   string   `json:"period"`
	Tomorrow []string `json:"tomorrow"`
}

func (s *Server) handleAIBriefing(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assistant(w, r)
	if !ok {
		return
	}
	var req aiBriefingReq
	if !decode(w, r, &req) {
		return
	}
	mu := mailUserFrom(r.Context())
	out, err := a.DailyBriefing(r.Context(), ai.BriefingInput{
		UserName: mu.Name, Date: req.Date, Period: req.Period,
		Events: req.Events, Tasks: req.Tasks, Emails: req.Emails, Tomorrow: req.Tomorrow,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "ai.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"briefing": out})
}
