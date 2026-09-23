//go:build pro

package ee

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/assistant"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/httpx"
	"github.com/beskosorg/beskos/backend/internal/reqauth"
)

// Organization-wide AI agents: creating, changing, deleting, or toggling one. Reading the list an
// organization already has stays Community (httpapi's handleAssistantAgents) -- see
// assistant_agents.go for why.

// Limits for a customer-created profile. They exist so that an agent doesn't turn into a place
// where someone pastes a 50 KB prompt that then gets paid for on every turn.
const (
	agentNameMax         = 60
	agentPurposeMax      = 200
	agentInstructionsMax = 4000
)

type agentJSON struct {
	Key          string   `json:"key"`
	Name         string   `json:"name,omitempty"`
	Purpose      string   `json:"purpose,omitempty"`
	Builtin      bool     `json:"builtin"`
	Enabled      bool     `json:"enabled"`
	Tools        []string `json:"tools"`
	Instructions string   `json:"instructions,omitempty"`
}

type agentWriteReq struct {
	Name         string   `json:"name"`
	Purpose      string   `json:"purpose"`
	Instructions string   `json:"instructions"`
	Tools        []string `json:"tools"`
	// DuplicateOf, when set, starts from a BUILT-IN agent. The copy is independent: it doesn't
	// receive our updates, and that's disclosed at duplication time (§3.2).
	DuplicateOf string `json:"duplicate_of"`
}

// validate normalizes and rejects whatever can't be saved. Rejecting tools is the important part:
// a profile can never have more than the cap, and that's decided HERE, not in the UI.
func (a *agentWriteReq) validate() string {
	a.Name = strings.TrimSpace(a.Name)
	a.Purpose = strings.TrimSpace(a.Purpose)
	if a.Name == "" || len([]rune(a.Name)) > agentNameMax {
		return "assistant.agent_name_invalid"
	}
	if len([]rune(a.Purpose)) > agentPurposeMax || len([]rune(a.Instructions)) > agentInstructionsMax {
		return "assistant.agent_too_long"
	}
	// A tool outside the cap is REJECTED, not silently dropped: whoever asked for it has to know
	// they didn't get it. Filtering silently would produce an agent different from the one they
	// thought they were creating.
	max := assistant.MaxTools()
	for _, w := range a.Tools {
		found := false
		for _, m := range max {
			if w == m {
				found = true
				break
			}
		}
		if !found {
			return "assistant.agent_tool_not_allowed"
		}
	}
	if a.Tools == nil {
		a.Tools = []string{}
	}
	return ""
}

// CreateAgent creates a customer profile, from scratch or by duplicating a built-in one.
func CreateAgent(w http.ResponseWriter, r *http.Request, deps Deps) {
	mu := reqauth.MailUserFrom(r.Context())
	var req agentWriteReq
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.DuplicateOf != "" {
		src, ok := assistant.Builtin(req.DuplicateOf)
		if !ok {
			// Only BUILT-IN agents can be duplicated: duplicating a customer one would be copying a
			// copy, and the spec only promises starting from a base that works.
			httpx.WriteErr(w, http.StatusBadRequest, "assistant.unknown_agent")
			return
		}
		if req.Instructions == "" {
			req.Instructions = src.Instructions
		}
		if len(req.Tools) == 0 {
			req.Tools = src.Tools
		}
		if req.Purpose == "" {
			req.Purpose = src.Purpose
		}
	}
	if code := req.validate(); code != "" {
		httpx.WriteErr(w, http.StatusBadRequest, code)
		return
	}
	row, err := deps.Queries.CreateClientAgent(r.Context(), db.CreateClientAgentParams{
		TenantID: mu.TenantID, Name: req.Name, Purpose: req.Purpose,
		Instructions: req.Instructions, Tools: req.Tools,
	})
	if err != nil {
		httpx.WriteErr(w, http.StatusConflict, agentWriteError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, agentJSON{
		Key: row.ID.String(), Name: row.Name, Purpose: row.Purpose,
		Instructions: row.Instructions, Tools: assistant.CapTools(row.Tools), Enabled: true,
	})
}

// UpdateAgent changes a customer profile. Built-in agents never go through here: the route
// requires a UUID, and built-in keys aren't UUIDs -- the immutability doesn't depend on an `if`.
func UpdateAgent(w http.ResponseWriter, r *http.Request, deps Deps) {
	mu := reqauth.MailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "assistant.unknown_agent")
		return
	}
	var req agentWriteReq
	if !httpx.Decode(w, r, &req) {
		return
	}
	if code := req.validate(); code != "" {
		httpx.WriteErr(w, http.StatusBadRequest, code)
		return
	}
	row, err := deps.Queries.UpdateClientAgent(r.Context(), db.UpdateClientAgentParams{
		ID: id, TenantID: mu.TenantID, Name: req.Name, Purpose: req.Purpose,
		Instructions: req.Instructions, Tools: req.Tools,
	})
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, agentWriteError(err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, agentJSON{
		Key: row.ID.String(), Name: row.Name, Purpose: row.Purpose,
		Instructions: row.Instructions, Tools: assistant.CapTools(row.Tools), Enabled: true,
	})
}

// DeleteAgent removes a customer profile.
func DeleteAgent(w http.ResponseWriter, r *http.Request, deps Deps) {
	mu := reqauth.MailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "assistant.unknown_agent")
		return
	}
	if err := deps.Queries.DeleteClientAgent(r.Context(), db.DeleteClientAgentParams{ID: id, TenantID: mu.TenantID}); err != nil {
		log.Printf("assistant: delete agent: %v", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type agentToggleReq struct {
	Enabled bool `json:"enabled"`
}

// ToggleAgent enables or disables a BUILT-IN agent for the organisation.
//
// It controls AVAILABILITY, never behavior: there's no endpoint that changes the instructions or
// tools of a built-in agent, because those live in code (§3.2, decision 3).
func ToggleAgent(w http.ResponseWriter, r *http.Request, deps Deps) {
	mu := reqauth.MailUserFrom(r.Context())
	key := chi.URLParam(r, "key")
	if _, ok := assistant.Builtin(key); !ok {
		httpx.WriteErr(w, http.StatusBadRequest, "assistant.unknown_agent")
		return
	}
	// Disabling the general agent is allowed: it's the assistant's master switch. While it's
	// disabled, chat rejects at the door (assistant.disabled) — the other agents end up with no way
	// in, which is the intended behavior for "assistant disabled for the organisation".
	var req agentToggleReq
	if !httpx.Decode(w, r, &req) {
		return
	}
	if err := deps.Queries.SetBuiltinAgentEnabled(r.Context(), db.SetBuiltinAgentEnabledParams{
		TenantID: mu.TenantID, AgentKey: key, Enabled: req.Enabled,
	}); err != nil {
		log.Printf("assistant: toggle agent: %v", err)
		httpx.WriteErr(w, http.StatusInternalServerError, "assistant.failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"key": key, "enabled": req.Enabled})
}

// agentWriteError translates the database error into an i18n key. A duplicate name is the common
// case and deserves its own message; everything else stays generic, so as not to leak database
// details.
func agentWriteError(err error) string {
	if strings.Contains(err.Error(), "idx_assistant_agents_name") {
		return "assistant.agent_name_taken"
	}
	log.Printf("assistant: agent write: %v", fmt.Errorf("%w", err))
	return "assistant.failed"
}
