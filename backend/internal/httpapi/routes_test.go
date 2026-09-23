package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/edition"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

// Registered routes. Writing the handler isn't enough — if nobody wires it to the router, the app
// responds 404 and nobody notices until a client tries (that's what happened with /api/aliases:
// handlers, queries, frontend and migration ready, no route at all). This test now catches it.
func TestRoutesRegistered(t *testing.T) {
	// Minimal Stalwart client: the router sets up the JMAP proxy and reads its base URL from it.
	srv := NewServer(nil, stalwart.New("http://stalwart.invalid", "", ""), nil, nil, nil,
		billing.ManualProvider{}, billing.Settings{Currency: "EUR", MinPaidUsers: 1}, edition.CommunityCapabilities())
	router, ok := srv.Router().(chi.Routes)
	if !ok {
		t.Fatal("the router doesn't expose the routes")
	}
	got := map[string]bool{}
	_ = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+strings.TrimSuffix(route, "/")] = true
		return nil
	})

	want := []string{
		// Public probes: this is what the status page measures. Outside /api, the proxy can't
		// reach them and the measurement would be the static file server's instead.
		"GET /api/healthz", "GET /api/readyz",
		// Email aliases (the case that motivated the test).
		"GET /api/aliases", "POST /api/aliases", "DELETE /api/aliases/{id}", "GET /api/me/addresses",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing route: %s", w)
		}
	}
}
