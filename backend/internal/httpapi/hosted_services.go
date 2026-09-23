package httpapi

import (
	"context"
	"github.com/go-chi/chi/v5"
)

// Self-hosted deployments have no operator-owned external services.
type hostedServices struct{}

func newHostedServices() hostedServices                       { return hostedServices{} }
func (s *Server) mountHostedPublic(chi.Router)                {}
func (s *Server) removeRelayIdentity(context.Context, string) {}
