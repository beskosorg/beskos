package httpapi

import (
	"net/http"

	"github.com/beskosorg/beskos/backend/internal/edition"
	"github.com/beskosorg/beskos/backend/internal/version"
)

// editionResponse embeds Capabilities so every existing field (branding, agents, ...) stays at
// the top level exactly as before -- adding the build-version fields alongside them, not nesting
// the response under a new key that would break an existing reader expecting e.g. `data.branding`.
type editionResponse struct {
	edition.Capabilities
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"build_date"`
}

// handleGetEdition tells the frontend which capabilities this instance has unlocked, so it can
// gate Pro-only UI without guessing from feature flags, plus which build is actually running --
// otherwise there is no way for an operator to tell a release binary's version apart from any
// other build of the same commit. Unauthenticated on purpose: the signup and login pages need it
// too.
func (s *Server) handleGetEdition(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, editionResponse{
		Capabilities: s.edition,
		Version:      version.Version,
		Commit:       version.Commit,
		Date:         version.Date,
	})
}
