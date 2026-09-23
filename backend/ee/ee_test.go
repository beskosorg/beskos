package ee

import (
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/beskosorg/beskos/backend/internal/edition"
)

// Register must never panic, in either build: it runs on every server start, before anyone
// knows whether a license key is even configured.
func TestRegisterDoesNotPanic(t *testing.T) {
	Register(chi.NewRouter(), edition.CommunityCapabilities(), Deps{})
}
