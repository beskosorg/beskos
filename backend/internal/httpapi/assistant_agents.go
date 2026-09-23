package httpapi

import (
	"context"
	"log"
	"net/http"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/ee"
	"github.com/beskosorg/beskos/backend/internal/assistant"
	"github.com/beskosorg/beskos/backend/internal/db"
)

// assistantAgentsFor returns the agents AVAILABLE to this organisation: the built-in ones that
// haven't been disabled, plus the ones created by the customer. This -- and everything else in
// this file -- stays Community: chat needs to resolve an agent regardless of edition, and an
// organization that once had Pro keeps whatever it already configured after the license lapses
// (see ee/agents_pro.go for what actually requires the Agents capability: creating, changing,
// deleting, or toggling an agent).
//
// A failure reading the database doesn't leave the person without an assistant: it returns at
// least the general agent. Without this, a transient database error would take away the whole
// feature instead of degrading it.
func (s *Server) assistantAgentsFor(ctx context.Context, tenant uuid.UUID) []assistant.Agent {
	general, _ := assistant.Builtin(assistant.AgentGeneral)
	out := make([]assistant.Agent, 0, 6)

	disabled := map[string]bool{}
	if rows, err := s.q.ListDisabledBuiltinAgents(ctx, tenant); err == nil {
		for _, k := range rows {
			disabled[k] = true
		}
	} else {
		log.Printf("assistant: read builtin settings: %v", err)
	}
	for _, key := range assistant.BuiltinKeys() {
		// Disabling the general agent disables the assistant entirely (it IS the "assistant"): chat
		// rejects at the door with assistant.disabled. Here we just remove it from the list of
		// available agents like any other — the decision to block everything lives in the chat
		// handler, not here.
		if disabled[key] {
			continue
		}
		if ag, ok := assistant.Builtin(key); ok {
			out = append(out, ag)
		}
	}

	rows, err := s.q.ListClientAgents(ctx, tenant)
	if err != nil {
		log.Printf("assistant: list client agents: %v", err)
		if len(out) == 0 {
			out = append(out, general)
		}
		return out
	}
	for _, r := range rows {
		out = append(out, clientAgent(r))
	}
	return out
}

// clientAgent translates the row into the profile the engine uses. Tools are filtered against the
// CAP again, on read: if one of our releases removes a tool from the catalog, a profile saved
// months ago can't keep granting it.
func clientAgent(r db.AssistantAgent) assistant.Agent {
	return assistant.Agent{
		Key:          r.ID.String(),
		Name:         r.Name,
		Purpose:      r.Purpose,
		Instructions: r.Instructions,
		Tools:        assistant.CapTools(r.Tools),
	}
}

// assistantAgentByKey resolves an agent by the key that came in the request — built-in or
// customer-created. Only returns what's available to this organisation: a disabled agent is as
// good as nonexistent.
func (s *Server) assistantAgentByKey(ctx context.Context, tenant uuid.UUID, key string) (assistant.Agent, bool) {
	for _, ag := range s.assistantAgentsFor(ctx, tenant) {
		if ag.Key == key {
			return ag, true
		}
	}
	return assistant.Agent{}, false
}

// --- Management API (read side; see ee/agents_pro.go for the write side) ---

type agentJSON struct {
	Key     string   `json:"key"`
	Name    string   `json:"name,omitempty"`
	Purpose string   `json:"purpose,omitempty"`
	Builtin bool     `json:"builtin"`
	Enabled bool     `json:"enabled"`
	Tools   []string `json:"tools"`
	// Instructions only goes out for the customer's own agents: the built-in ones' instructions are
	// ours and aren't editable, nor of interest to the admin (§3.2).
	Instructions string `json:"instructions,omitempty"`
}

// handleAssistantAgents lists the organisation's agents — the built-in ones (with their
// enabled/disabled state) and the customer's own.
//
// The built-in ones carry neither a name nor a description: that text belongs to the UI and comes
// from i18n by key (law 4.1). The customer's own carry them, because the name was written by a
// person and isn't translated.
func (s *Server) handleAssistantAgents(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	disabled := map[string]bool{}
	if rows, err := s.q.ListDisabledBuiltinAgents(r.Context(), mu.TenantID); err == nil {
		for _, k := range rows {
			disabled[k] = true
		}
	}
	// Apps disabled by the organization also remove the agent that depends on them (e.g. no Files,
	// no Documents agent; no Mail, no Inbox agent) — not just from the tools, but from the picker
	// too.
	disabledApps := s.disabledAppsEffective(r.Context(), mu.TenantID)
	blockedTools := s.disabledAssistantTools(r.Context(), mu.TenantID)

	items := make([]agentJSON, 0, 6)
	for _, key := range assistant.BuiltinKeys() {
		ag, ok := assistant.Builtin(key)
		if !ok || !assistant.AgentAvailable(key, disabledApps) {
			continue
		}
		items = append(items, agentJSON{
			Key: ag.Key, Builtin: true, Tools: assistant.WithoutTools(assistant.AllowedTools(ag.Tools, disabledApps), blockedTools),
			Enabled: !disabled[key],
		})
	}
	rows, err := s.q.ListClientAgents(r.Context(), mu.TenantID)
	if err != nil {
		log.Printf("assistant: list client agents: %v", err)
		writeErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	for _, c := range rows {
		items = append(items, agentJSON{
			Key: c.ID.String(), Name: c.Name, Purpose: c.Purpose,
			Instructions: c.Instructions, Tools: assistant.CapTools(c.Tools), Enabled: true,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "max_tools": assistant.MaxTools()})
}

// handleAssistantAgentCreateGate, ...UpdateGate, ...DeleteGate, ...ToggleGate enforce the Pro
// Agents capability before handing off to ee -- see ee/agents_pro.go for the actual logic and
// ee/README.md for why the gate lives here rather than inside ee.Register.
func (s *Server) handleAssistantAgentCreateGate(w http.ResponseWriter, r *http.Request) {
	if !s.edition.Agents {
		writeErr(w, http.StatusForbidden, "assistant.agents_edition_required")
		return
	}
	ee.CreateAgent(w, r, ee.Deps{Queries: s.q})
}

func (s *Server) handleAssistantAgentUpdateGate(w http.ResponseWriter, r *http.Request) {
	if !s.edition.Agents {
		writeErr(w, http.StatusForbidden, "assistant.agents_edition_required")
		return
	}
	ee.UpdateAgent(w, r, ee.Deps{Queries: s.q})
}

func (s *Server) handleAssistantAgentDeleteGate(w http.ResponseWriter, r *http.Request) {
	if !s.edition.Agents {
		writeErr(w, http.StatusForbidden, "assistant.agents_edition_required")
		return
	}
	ee.DeleteAgent(w, r, ee.Deps{Queries: s.q})
}

func (s *Server) handleAssistantAgentToggleGate(w http.ResponseWriter, r *http.Request) {
	if !s.edition.Agents {
		writeErr(w, http.StatusForbidden, "assistant.agents_edition_required")
		return
	}
	ee.ToggleAgent(w, r, ee.Deps{Queries: s.q})
}
