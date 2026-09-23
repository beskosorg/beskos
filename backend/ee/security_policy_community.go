//go:build !pro

package ee

import (
	"net/http"

	"github.com/beskosorg/beskos/backend/internal/httpx"
)

// SetSecurityPolicy is the Community build's version: this build links none of the Enterprise
// security-policy logic (see security_policy_pro.go), so it always answers as if no license could
// ever grant this capability. httpapi's own edition check (handleSetSecurityPolicyGate) already
// refuses this request before reaching here in a real deployment; this is the defense for anyone
// who wires a route to it directly.
func SetSecurityPolicy(w http.ResponseWriter, _ *http.Request, _ Deps) {
	httpx.WriteErr(w, http.StatusForbidden, "security.edition_required")
}
