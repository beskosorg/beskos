//go:build pro

package ee

import (
	"net/http"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/httpx"
	"github.com/beskosorg/beskos/backend/internal/reqauth"
)

// Custom organization branding (logo, primary color, dark-theme logo variant). Reading branding
// stays a Community feature (routes_public.go's /public/branding, and the authenticated
// GET /branding) so a Pro tenant's look reaches every viewer and the login screen with no key on
// the reading end; only SETTING it is Enterprise.

type updateBrandingReq struct {
	Color         string `json:"color"`
	Logo          string `json:"logo"`
	LogoWhiteDark bool   `json:"logo_white_dark"`
}

const maxLogoBytes = 512 * 1024 // ~512 KB data URI -- enough for a logo, keeps the row from bloating

// UpdateBranding validates and saves a tenant's branding. Mounted by Register only when
// caps.Branding is true; downgrading a license does not erase what was already saved, it only
// stops further changes (the read path keeps serving the last saved value).
func UpdateBranding(w http.ResponseWriter, r *http.Request, deps Deps) {
	mu := reqauth.MailUserFrom(r.Context())
	var req updateBrandingReq
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.Color != "" && !isHexColor(req.Color) {
		httpx.WriteErr(w, http.StatusBadRequest, "branding.invalid_color")
		return
	}
	if len(req.Logo) > maxLogoBytes {
		httpx.WriteErr(w, http.StatusRequestEntityTooLarge, "branding.logo_too_large")
		return
	}
	if req.Logo != "" && !isDataImage(req.Logo) {
		httpx.WriteErr(w, http.StatusBadRequest, "branding.invalid_logo")
		return
	}
	if err := deps.Queries.UpdateTenantBranding(r.Context(), db.UpdateTenantBrandingParams{
		ID: mu.TenantID, BrandColor: req.Color, BrandLogo: req.Logo, BrandLogoWhiteDark: req.LogoWhiteDark,
	}); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "branding.save_failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func isHexColor(s string) bool {
	if (len(s) != 7 && len(s) != 4) || s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func isDataImage(s string) bool {
	return len(s) > 11 && s[:11] == "data:image/"
}
