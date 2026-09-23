//go:build !pro

package ee

import (
	"net/http"

	"github.com/beskosorg/beskos/backend/internal/httpx"
)

// CreateAgent, UpdateAgent, DeleteAgent, and ToggleAgent are the Community build's versions: this
// build links none of the Enterprise agent-management logic (see agents_pro.go), so each always
// answers as if no license could ever grant this capability. httpapi's own edition check
// (handleAssistantAgent*Gate) already refuses these requests before reaching here in a real
// deployment; this is the defense for anyone who wires a route to one of these directly.

func CreateAgent(w http.ResponseWriter, _ *http.Request, _ Deps) { refuseAgentCapability(w) }
func UpdateAgent(w http.ResponseWriter, _ *http.Request, _ Deps) { refuseAgentCapability(w) }
func DeleteAgent(w http.ResponseWriter, _ *http.Request, _ Deps) { refuseAgentCapability(w) }
func ToggleAgent(w http.ResponseWriter, _ *http.Request, _ Deps) { refuseAgentCapability(w) }

func refuseAgentCapability(w http.ResponseWriter) {
	httpx.WriteErr(w, http.StatusForbidden, "assistant.agents_edition_required")
}
