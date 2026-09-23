package ai

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func modelsServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The incident this exists for: the endpoint stopped offering the configured model and every
// request failed with a 403 for hours. The listing still answered, without that name in it.
func TestCheckModelReportsARetiredModel(t *testing.T) {
	srv := modelsServer(t, 200, `{"data":[{"id":"deepseek/deepseek-v4-flash-0731"},{"id":"deepseek/deepseek-v4.1-flash"}]}`)
	c := NewClient(ClientOptions{Name: "tensorx", APIKey: "k", BaseURL: srv.URL, Model: "deepseek/deepseek-v4-flash"})
	err := c.CheckModel(context.Background())
	if err == nil {
		t.Fatal("a model the endpoint no longer lists must be reported at startup")
	}
	if !strings.Contains(err.Error(), "deepseek/deepseek-v4-flash") || !strings.Contains(err.Error(), "AI_MODEL") {
		t.Errorf("the report must name the model and the variable to change, got: %v", err)
	}
}

func TestCheckModelAcceptsAListedModel(t *testing.T) {
	srv := modelsServer(t, 200, `{"data":[{"id":"a"},{"id":"m"}]}`)
	c := NewClient(ClientOptions{APIKey: "k", BaseURL: srv.URL, Model: "m"})
	if err := c.CheckModel(context.Background()); err != nil {
		t.Errorf("a listed model is fine: %v", err)
	}
}

// Not every OpenAI-compatible server implements the listing. "Cannot tell" must not be reported
// as "wrong", or a working installation would log a false alarm on every boot.
func TestCheckModelCannotTellIsNotAnError(t *testing.T) {
	for name, srv := range map[string]*httptest.Server{
		"no listing":    modelsServer(t, 404, `not found`),
		"empty listing": modelsServer(t, 200, `{"data":[]}`),
		"not json":      modelsServer(t, 200, `<html>`),
	} {
		c := NewClient(ClientOptions{APIKey: "k", BaseURL: srv.URL, Model: "m"})
		if err := c.CheckModel(context.Background()); err != nil {
			t.Errorf("%s: should be treated as unknown, got: %v", name, err)
		}
	}
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prev); log.SetFlags(flags) })
	return &buf
}

// Every failure is written down with what the endpoint said, and a run of them escalates once and
// announces recovery (law 2.8). Before this the only trace was "POST /api/ai/briefing -> 502".
func TestHealthRecordsEscalatesAndRecovers(t *testing.T) {
	buf := captureLog(t)
	var h health
	ctx := context.Background()
	boom := errors.New("tensorx HTTP 403: key not allowed to access model")

	for i := 0; i < failThreshold; i++ {
		h.record(ctx, "tensorx", "m", boom)
	}
	out := buf.String()
	if strings.Count(out, "request failed") != failThreshold {
		t.Errorf("each failure must be logged with its cause:\n%s", out)
	}
	if !strings.Contains(out, "key not allowed to access model") {
		t.Errorf("the endpoint's own words must reach the log:\n%s", out)
	}
	if strings.Count(out, "ESCALATION") != 1 {
		t.Errorf("the threshold must escalate exactly once:\n%s", out)
	}

	h.record(ctx, "tensorx", "m", boom) // past the threshold: still logged, not re-escalated
	if strings.Count(buf.String(), "ESCALATION") != 1 {
		t.Error("escalation must not repeat on every further failure")
	}

	h.record(ctx, "tensorx", "m", nil)
	if !strings.Contains(buf.String(), "RECOVERED") {
		t.Error("recovery after an escalation must be announced")
	}
}

// A request the caller abandoned is not the provider failing, and must not push the count toward
// an outage report.
func TestHealthIgnoresCancelledRequests(t *testing.T) {
	buf := captureLog(t)
	var h health
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := 0; i < failThreshold*2; i++ {
		h.record(ctx, "p", "m", context.Canceled)
	}
	if buf.Len() != 0 || h.consecutive.Load() != 0 {
		t.Errorf("cancelled requests were counted: n=%d log=%q", h.consecutive.Load(), buf.String())
	}
}
