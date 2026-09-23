package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/assistant"
	"github.com/beskosorg/beskos/backend/internal/db"
)

// assistantMaxBody caps the request size. The user's message is a sentence, not a file.
const assistantMaxBody = 64 << 10

// assistantHistoryTurns is how much of the thread goes back to the model. Resending the whole
// thread on every turn is the fastest way to run up a bill (§9): past a certain point it should be
// summarized instead of resent (§9.3), and that is not implemented yet. Until then, a fixed
// window is the honest behavior.
const assistantHistoryTurns = 20

type assistantChatReq struct {
	Message string `json:"message"`
	// Agent, when set, PINS the agent for the rest of the thread — it's the user's manual choice
	// (§3.2). Empty leaves routing to decide on every turn.
	Agent string `json:"agent"`
	// Conversation empty opens a new conversation.
	Conversation string `json:"conversation_id"`
	// Background sends the request to the worker instead of running it here (§3.6). It's an
	// EXPLICIT choice made by the caller: guessing what counts as "long" would just be guessing,
	// and a background task changes what the person sees next.
	Background bool `json:"background"`
}

// handleAssistantChat runs a full turn and streams it over SSE: text chunks, action cards, and the
// final result (§7.3).
//
// Errors fall into two moments, and that's what explains the response format: before the stream
// opens they're normal HTTP errors, with error_code for the frontend to translate (law 4.4); once
// it's open there's no way to change the status code anymore, so they travel as an `error` event
// carrying the same key.
func (s *Server) handleAssistantChat(w http.ResponseWriter, r *http.Request) {
	if s.ai == nil || s.agent == nil {
		writeErr(w, http.StatusServiceUnavailable, "ai.disabled")
		return
	}
	mu := mailUserFrom(r.Context())
	// Master switch: the admin disabled the assistant for the organization (disabled the general
	// agent). Reject at the door, before spending quota or opening the stream.
	if s.assistantDisabled(r.Context(), mu.TenantID) {
		writeErr(w, http.StatusServiceUnavailable, "assistant.disabled")
		return
	}
	// Its own limiter: an agentic run spends several requests against the model, so it can't share
	// the bucket used for single-step AI calls.
	if !s.agentLimiter.allow("assistant:"+mu.ID.String(), time.Now()) {
		writeErr(w, http.StatusTooManyRequests, "assistant.rate_limited")
		return
	}
	// Commercial quota for the 6h window (§9.2). Distinct from the step cap (§9.1), which still
	// applies within each run. Once exhausted, the request is rejected with the renewal time shown.
	qid := assistant.Identity{TenantID: mu.TenantID, UserID: mu.ID}
	if s.assistantQuotaStatus(r.Context(), qid, time.Now()).Exceeded() {
		writeErr(w, http.StatusTooManyRequests, "assistant.quota_exceeded")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, assistantMaxBody)

	var req assistantChatReq
	if !decode(w, r, &req) {
		return
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		writeErr(w, http.StatusBadRequest, "assistant.message_required")
		return
	}
	id := assistant.Identity{TenantID: mu.TenantID, UserID: mu.ID, Email: mu.Email, Language: mu.Language}

	turn, code := s.assistantTurn(r.Context(), id, req, msg)
	if code != "" {
		writeErr(w, http.StatusBadRequest, code)
		return
	}
	// Apps disabled by the organization: the AI loses those apps' tools, and the agent that depends
	// on them (customer request). Applies to the stream AND to background tasks. An agent without
	// its app falls back to the general one, already with its tools filtered.
	if disabled := s.disabledAppsEffective(r.Context(), mu.TenantID); len(disabled) > 0 {
		if !assistant.AgentAvailable(turn.agent.Key, disabled) {
			if g, ok := assistant.Builtin(assistant.AgentGeneral); ok {
				turn.agent = g
			}
		}
		turn.agent.Tools = assistant.AllowedTools(turn.agent.Tools, disabled)
	}
	// Fine-grained cap: tools the organization disabled one by one. Applies to ALL agents and is
	// applied AFTER the app filter — it's the final word on what the AI can do.
	turn.agent.Tools = assistant.WithoutTools(turn.agent.Tools, s.disabledAssistantTools(r.Context(), mu.TenantID))
	if req.Background {
		// The conversation immediately returns a task card; the work continues on the server. No
		// SSE: there's nothing to stream, progress is visible in the Tasks tab.
		s.startAssistantTask(w, r, turn, msg)
		return
	}
	conv, ag := turn.conv, turn.agent

	sse, ok := newSSE(w)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	// The conversation goes first: without this, a new conversation would only get an id at the
	// end, and a user who closes the panel midway would lose the thread that's already saved.
	// The agent that's going to respond goes in the first event: the routing choice is ALWAYS
	// visible (§3.2). Silent routing would leave the person unable to tell why the assistant
	// sometimes can do something and sometimes can't.
	sse.send(map[string]any{
		"kind": "conversation", "conversation_id": conv.ID, "title": conv.Title,
		"agent": ag.Key, "pinned_agent": conv.PinnedAgent,
	})

	run, err := s.agent.RunStream(r.Context(), id, ag,
		assistant.RunOptions{
			Message: msg, History: turn.history, ConversationID: conv.ID,
			Memory: s.assistantMemoryInject(r.Context(), id), // lasting facts, as CONTEXT (§8.3)
		},
		func(ev assistant.Event) { sse.send(ev) })
	if err != nil {
		log.Printf("assistant: run failed for user %s: %v", mu.ID, err)
		sse.send(map[string]any{"kind": "error", "error_code": "assistant.failed"})
		return
	}

	// The response gets saved even if the user closed the tab midway: the work has ALREADY been
	// done (and paid for), and reopening the conversation has to show what happened.
	s.saveAssistantAnswer(r.Context(), id, conv.ID, run)
	s.recordAssistantUsage(r.Context(), id, run.Usage.TotalTokens) // 6h window quota (§9.2)
	sse.send(assistant.Event{Kind: assistant.EventDone, Run: &run})

	// Memory extraction happens AFTER the turn, only over the user's message (§8.2). It isn't
	// silent: what it saved and what it refused goes out in an event, for the panel to show (§6.5).
	facts, refusals := s.assistantExtractMemory(context.WithoutCancel(r.Context()), id, msg, turn.userMsgID)
	if len(facts) > 0 || len(refusals) > 0 {
		sse.send(map[string]any{"kind": "memory", "saved": facts, "refused": refusals})
	}
}

// assistantDisabled reports whether the organization disabled the assistant entirely. The general
// agent IS the assistant: disabling it disables the feature for everyone in the organization. A
// transient error reading the database doesn't disable anything — it degrades to "enabled".
func (s *Server) assistantDisabled(ctx context.Context, tenant uuid.UUID) bool {
	rows, err := s.q.ListDisabledBuiltinAgents(ctx, tenant)
	if err != nil {
		return false
	}
	for _, k := range rows {
		if k == assistant.AgentGeneral {
			return true
		}
	}
	return false
}

// assistantTurnState is what a turn needs to know before the loop starts.
type assistantTurnState struct {
	conv      db.AssistantConversation
	history   []assistant.Turn
	agent     assistant.Agent
	userMsgID pgtype.UUID // this turn's user message — the source of a memory fact (§6.5)
}

// assistantTurn resolves the conversation, PICKS THE AGENT, and saves the user's message.
//
// The ordering here is a security rule, not tidiness: the agent is decided BEFORE any tool runs,
// once per turn, and only from the user's message (§3.2). Switching agents means switching
// privileges; if the decision could happen after the loop reads an email, that email could trigger
// it.
func (s *Server) assistantTurn(ctx context.Context, id assistant.Identity, req assistantChatReq, msg string) (assistantTurnState, string) {
	var st assistantTurnState

	// 1. The conversation and its thread.
	if req.Conversation != "" {
		parsed, err := uuid.Parse(req.Conversation)
		if err != nil {
			return st, "assistant.unknown_conversation"
		}
		st.conv, err = s.q.GetAssistantConversation(ctx, db.GetAssistantConversationParams{
			ID: parsed, TenantID: id.TenantID, MailUserID: id.UserID,
		})
		if err != nil {
			// "Doesn't exist" is not distinguished from "isn't yours": that difference would tell
			// anyone guessing an id that it exists on another account.
			return st, "assistant.unknown_conversation"
		}
		msgs, err := s.q.ListAssistantMessages(ctx, db.ListAssistantMessagesParams{
			ConversationID: st.conv.ID, TenantID: id.TenantID, MailUserID: id.UserID, Lim: assistantHistoryTurns,
		})
		if err != nil {
			log.Printf("assistant: load history: %v", err)
			return st, "assistant.failed"
		}
		for _, m := range msgs {
			st.history = append(st.history, assistant.Turn{Role: m.Role, Content: m.Content})
		}
	}

	// 2. The agent.
	key, code := s.pickAssistantAgent(ctx, id.TenantID, req, st.conv, msg)
	if code != "" {
		return st, code
	}
	ag, ok := s.assistantAgentByKey(ctx, id.TenantID, key)
	if !ok {
		return st, "assistant.unknown_agent"
	}
	st.agent = ag

	// 3. The new conversation, already with the chosen agent; or the pinning, when the user chose.
	if st.conv.ID == (uuid.UUID{}) {
		created, err := s.q.CreateAssistantConversation(ctx, db.CreateAssistantConversationParams{
			TenantID: id.TenantID, MailUserID: id.UserID, Agent: ag.Key, Title: conversationTitle(msg),
		})
		if err != nil {
			log.Printf("assistant: create conversation: %v", err)
			return st, "assistant.failed"
		}
		st.conv = created
	}
	if req.Agent != "" && st.conv.PinnedAgent != req.Agent {
		if err := s.q.PinAssistantConversationAgent(ctx, db.PinAssistantConversationAgentParams{
			ID: st.conv.ID, TenantID: id.TenantID, MailUserID: id.UserID, PinnedAgent: req.Agent,
		}); err != nil {
			log.Printf("assistant: pin agent: %v", err)
		} else {
			st.conv.PinnedAgent = req.Agent
		}
	}

	// 4. The user's message.
	userMsg, err := s.q.AppendAssistantMessage(ctx, db.AppendAssistantMessageParams{
		TenantID: id.TenantID, ConversationID: st.conv.ID, Role: "user", Content: msg,
		Steps: json.RawMessage("[]"),
	})
	if err != nil {
		log.Printf("assistant: append user message: %v", err)
		return st, "assistant.failed"
	}
	st.userMsgID = pgtype.UUID{Bytes: userMsg.ID, Valid: true}
	return st, ""
}

// pickAssistantAgent decides who responds, in this order:
//
//  1. the user's MANUAL choice on this request — pins the agent for the rest of the thread;
//  2. the agent already pinned on the conversation;
//  3. automatic routing, based on the user's message.
//
// The manual choice comes first on purpose: the user always outranks the model.
func (s *Server) pickAssistantAgent(ctx context.Context, tenant uuid.UUID, req assistantChatReq, conv db.AssistantConversation, msg string) (string, string) {
	available := s.assistantAgentsFor(ctx, tenant)
	has := func(key string) bool {
		for _, a := range available {
			if a.Key == key {
				return true
			}
		}
		return false
	}
	if req.Agent != "" {
		if !has(req.Agent) {
			return "", "assistant.unknown_agent"
		}
		return req.Agent, ""
	}
	if conv.PinnedAgent != "" && has(conv.PinnedAgent) {
		return conv.PinnedAgent, ""
	}
	// The pinned agent no longer exists (deleted, or disabled by the admin). Falls back to routing
	// instead of failing the request: the person's conversation can't be held hostage by a profile
	// that's gone.
	choices := make([]assistant.AgentChoice, 0, len(available))
	for _, a := range available {
		choices = append(choices, assistant.AgentChoice{Key: a.Key, Purpose: a.Purpose})
	}
	return s.agentRouter.Pick(ctx, msg, choices), ""
}

// saveAssistantAnswer persists the response and bumps the conversation up the list. Failing here
// doesn't spoil the turn the user just saw — but it goes into the log, because the thread ended up
// incomplete.
func (s *Server) saveAssistantAnswer(ctx context.Context, id assistant.Identity, conv uuid.UUID, run assistant.Run) {
	// No cancellation: if the tab closed, the write still has to happen.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	steps, err := json.Marshal(run.Steps)
	if err != nil {
		steps = json.RawMessage("[]")
	}
	if _, err := s.q.AppendAssistantMessage(saveCtx, db.AppendAssistantMessageParams{
		TenantID: id.TenantID, ConversationID: conv, Role: "assistant", Content: run.Answer,
		Steps: steps, Stopped: run.Stopped, RunID: pgtype.UUID{Bytes: run.ID, Valid: true},
		// Who responded gets recorded: reopening the conversation shows the same choice that was
		// seen live.
		Agent: run.Agent,
	}); err != nil {
		log.Printf("assistant: append answer: %v", err)
	}
	if err := s.q.TouchAssistantConversation(saveCtx, db.TouchAssistantConversationParams{
		ID: conv, TenantID: id.TenantID, MailUserID: id.UserID,
	}); err != nil {
		log.Printf("assistant: touch conversation: %v", err)
	}
}

// conversationTitle derives the title from the user's first sentence. A title isn't requested from
// the model: it would cost a call per conversation to say less than the sentence itself.
func conversationTitle(msg string) string {
	title := strings.TrimSpace(strings.ReplaceAll(msg, "\n", " "))
	const max = 80
	if len([]rune(title)) > max {
		title = strings.TrimSpace(string([]rune(title)[:max])) + "…"
	}
	return title
}
