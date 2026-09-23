//go:build !pro

package httpapi

import "github.com/go-chi/chi/v5"

// Self-hosted installations have no hosted billing or checkout routes.
func (s *Server) mountBilling(chi.Router)       {}
func (s *Server) mountPublicBilling(chi.Router) {}
