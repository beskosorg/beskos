package httpapi

import (
	"net/http"

	"github.com/beskosorg/beskos/backend/ee"
)

// Organization branding (primary color + logo). Read by any tenant user (for the app's top bar);
// write only by admin/owner (RBAC org.branding) AND only with the Pro Branding capability --
// see handleUpdateBrandingGate and ee/branding.go. At login, resolved by email.

type brandingResp struct {
	Name          string `json:"name"`
	Color         string `json:"color"`
	Logo          string `json:"logo"`            // data URI, or empty
	LogoWhiteDark bool   `json:"logo_white_dark"` // forces the logo to white in dark theme
}

// handleGetBranding returns the authenticated user's tenant branding.
func (s *Server) handleGetBranding(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	t, err := s.q.GetTenant(r.Context(), mu.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "branding.load_failed")
		return
	}
	writeJSON(w, http.StatusOK, brandingResp{Name: t.Name, Color: t.BrandColor, Logo: t.BrandLogo, LogoWhiteDark: t.BrandLogoWhiteDark})
}

// handleUpdateBrandingGate enforces the Pro capability before handing off to ee.UpdateBranding,
// which holds the actual validation and persistence (Enterprise code, ee/branding.go). RBAC
// (org.branding) is checked first by the route's own middleware, same as every other admin route;
// this only adds the edition check on top.
func (s *Server) handleUpdateBrandingGate(w http.ResponseWriter, r *http.Request) {
	if !s.edition.Branding {
		writeErr(w, http.StatusForbidden, "branding.edition_required")
		return
	}
	ee.UpdateBranding(w, r, ee.Deps{Queries: s.q})
}

// handleResolveBranding returns the tenant's branding for an email, for the login screen before any
// session exists. An unknown email returns the empty (default) branding, never revealing whether
// the account exists.
func (s *Server) handleResolveBranding(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	if email == "" {
		writeJSON(w, http.StatusOK, brandingResp{})
		return
	}
	b, err := s.q.GetTenantBrandingByEmail(r.Context(), email)
	if err != nil {
		writeJSON(w, http.StatusOK, brandingResp{}) // unknown -> default, without leaking existence
		return
	}
	writeJSON(w, http.StatusOK, brandingResp{Name: b.Name, Color: b.BrandColor, Logo: b.BrandLogo})
}
