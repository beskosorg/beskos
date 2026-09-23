package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/edition"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

// Without the Agents capability, none of the four write endpoints reach ee -- each answers
// 403 before ever touching the database or the authenticated user in context (a Community server
// has neither wired up here).
func TestAssistantAgentGatesRefuseWithoutCapability(t *testing.T) {
	srv := NewServer(nil, stalwart.New("http://stalwart.invalid", "", ""), nil, nil, nil,
		billing.ManualProvider{}, billing.Settings{}, edition.CommunityCapabilities())

	cases := []struct {
		name string
		gate func(http.ResponseWriter, *http.Request)
	}{
		{"create", srv.handleAssistantAgentCreateGate},
		{"update", srv.handleAssistantAgentUpdateGate},
		{"delete", srv.handleAssistantAgentDeleteGate},
		{"toggle", srv.handleAssistantAgentToggleGate},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/assistant/agents", nil)
			c.gate(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
			}
			if got := w.Body.String(); !strings.Contains(got, "assistant.agents_edition_required") {
				t.Errorf("body = %q, want it to contain assistant.agents_edition_required", got)
			}
		})
	}
}
