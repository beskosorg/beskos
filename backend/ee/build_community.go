//go:build !pro

package ee

import (
	"github.com/go-chi/chi/v5"

	"github.com/beskosorg/beskos/backend/internal/edition"
)

// Register is the Community build's version: a default build (no -tags pro) never links any
// Enterprise code, and this is the only symbol that stands in for it.
func Register(r chi.Router, caps edition.Capabilities, deps Deps) {}
