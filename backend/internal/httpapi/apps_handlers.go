package httpapi

import (
	"net/http"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// toggleableApps are the rail apps that the admin can disable for the organization. Settings and
// Administration aren't rail apps (they can't be disabled). Keep this list aligned with the
// frontend rail and with assistant.AppTools.
var toggleableApps = []string{"today", "mail", "chat", "kanban", "cal", "files", "docs", "contacts", "meet", "notes", "wiki"}

// handleGetApps returns the toggleable apps and the ones disabled for the organization. Any
// user can read this (the rail filters based on it).
func (s *Server) handleGetApps(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	disabled := s.disabledAppsEffective(r.Context(), u.TenantID) // org admin + plan exclusions
	writeJSON(w, http.StatusOK, map[string]any{"apps": toggleableApps, "disabled": orEmpty(disabled)})
}

// handleSetApps saves the organization's DISABLED apps (admin/owner only, gated at the route).
// Ignores ids that aren't toggleable — Mail, for instance, can never be disabled.
func (s *Server) handleSetApps(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Disabled []string `json:"disabled"`
	}
	if !decode(w, r, &req) {
		return
	}
	valid := map[string]bool{}
	for _, a := range toggleableApps {
		valid[a] = true
	}
	seen := map[string]bool{}
	clean := []string{}
	for _, a := range req.Disabled {
		if valid[a] && !seen[a] {
			clean = append(clean, a)
			seen[a] = true
		}
	}
	u := mailUserFrom(r.Context())
	if err := s.q.SetTenantDisabledApps(r.Context(), db.SetTenantDisabledAppsParams{ID: u.TenantID, DisabledApps: clean}); err != nil {
		writeErr(w, http.StatusInternalServerError, "apps.save_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"disabled": clean})
}
