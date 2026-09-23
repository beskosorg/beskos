package httpapi

import (
	"encoding/json"
	"net/http"
)

// sseWriter streams Server-Sent Events. Each event is a `data: <json>` line, followed by a blank
// line — the serialized JSON never contains raw line breaks, so one line per event is enough.
type sseWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

// newSSE writes the headers and returns the streamer. It fails when the connection doesn't
// support flush — without flush, everything would stay buffered and arrive all at once at the
// end, which is the opposite of streaming.
func newSSE(w http.ResponseWriter) (*sseWriter, bool) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Tells nginx not to buffer the response; without this the proxy would defeat the streaming.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	f.Flush()
	return &sseWriter{w: w, f: f}, true
}

// send emits an event. A value that fails to serialize is silently dropped: killing the
// connection midway over one event would be worse than losing that event.
func (s *sseWriter) send(v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		return
	}
	if _, err := s.w.Write(append(append([]byte("data: "), payload...), '\n', '\n')); err != nil {
		return
	}
	s.f.Flush()
}
