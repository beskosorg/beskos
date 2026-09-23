package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync/atomic"
)

// Whether the provider is actually answering, as opposed to merely configured.
//
// Written after an incident where every AI request in production failed for hours with nothing
// in the logs but `POST /api/ai/briefing -> 502`. The endpoint had stopped offering the model
// name in AI_MODEL, answered each request with a 403 that said exactly that, and the handlers
// turned it into a translatable error code for the browser and dropped the text. The cause was
// found by repeating the request by hand. Two things here make that unnecessary.

// failThreshold is how many consecutive failures turn individual errors into an outage report.
// Low on purpose: one failure is a hiccup, five in a row across users is the assistant being down.
const failThreshold = 5

// health counts consecutive failed requests. One client serves the whole process, so the count
// is across users -- which is the point: it measures the provider, not a single request.
type health struct {
	consecutive atomic.Int64
}

// record logs a failed request and escalates past the threshold (law 2.8). A request cancelled by
// its caller -- a closed tab, a client timeout -- is not the provider failing and is not counted.
func (h *health) record(ctx context.Context, name, model string, err error) {
	if err == nil {
		if n := h.consecutive.Swap(0); n >= failThreshold {
			log.Printf("ai: %s RECOVERED after %d consecutive failures", name, n)
		}
		return
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return
	}
	n := h.consecutive.Add(1)
	log.Printf("ai: %s model=%s request failed: %v", name, model, err)
	if n == failThreshold {
		log.Printf("ai: %s ESCALATION -- %d consecutive failures; the assistant is down for everyone until this is fixed", name, n)
	}
}

// CheckModel asks the endpoint whether it offers the configured model.
//
// A model name is the part of this configuration that goes stale, and it cannot be caught by
// reading the configuration: it is only wrong once the vendor retires it. Asking at startup turns
// a failure that surfaces hours later as a misleading permission error into one line at boot.
//
// GET {base}/models is part of the same OpenAI-compatible protocol, but not every server
// implements it. When the listing is unavailable the answer is "cannot tell", not "wrong": it
// returns nil, and only a listing that succeeds and omits the model is reported.
func (d *Client) CheckModel(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL+"/models", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+d.apiKey)
	resp, err := d.http.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if json.Unmarshal(body, &list) != nil || len(list.Data) == 0 {
		return nil
	}
	for _, m := range list.Data {
		if m.ID == d.model {
			return nil
		}
	}
	return fmt.Errorf("model %q is not offered by %s, which lists %d other models -- set AI_MODEL to one of them",
		d.model, d.Name(), len(list.Data))
}
