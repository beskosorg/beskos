//go:build pro

package ee

import (
	"github.com/go-chi/chi/v5"

	"github.com/beskosorg/beskos/backend/internal/edition"
)

// Register wires the Enterprise routes for whatever caps grants. No feature has been moved
// behind this seam yet -- see docs/editions.md for what Pro is meant to include.
func Register(r chi.Router, caps edition.Capabilities, deps Deps) {}
