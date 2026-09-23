package httpx

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteErrIncludesStableCodeAndFallback(t *testing.T) {
	w := httptest.NewRecorder()
	WriteErr(w, 403, "branding.edition_required")
	if w.Code != 403 {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error_code"] != "branding.edition_required" {
		t.Errorf("error_code = %q", body["error_code"])
	}
	if body["error"] != "Edition required" {
		t.Errorf("error fallback = %q, want %q", body["error"], "Edition required")
	}
}

func TestDecodeRejectsInvalidJSON(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	var v map[string]any
	if Decode(w, r, &v) {
		t.Fatal("Decode should have rejected invalid JSON")
	}
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
