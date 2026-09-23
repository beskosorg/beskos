// Package httpx holds the tiny, generic JSON-response helpers shared by httpapi and ee. It
// carries no business logic and no license-sensitive code, only the wire format both sides must
// agree on (law §4.4's stable error-code contract).
package httpx

import (
	"encoding/json"
	"net/http"
	"strings"
)

// WriteJSON writes v as the JSON response body with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteErr returns an error with a STABLE code (the frontend translates it via i18n — law §4.4)
// plus an English fallback message derived from the code. The code is the source of truth for
// the translation; the message is only a backup and is always English (law §4.6).
func WriteErr(w http.ResponseWriter, status int, code string) {
	WriteJSON(w, status, map[string]string{"error_code": code, "error": humanizeCode(code)})
}

// Decode reads the request body as JSON into v, or writes a 400 and returns false.
func Decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		WriteErr(w, http.StatusBadRequest, "request.invalid_json")
		return false
	}
	return true
}

// humanizeCode turns "auth.invalid_credentials" into "Invalid credentials" (English fallback).
func humanizeCode(code string) string {
	s := code
	if i := strings.LastIndex(code, "."); i >= 0 {
		s = code[i+1:]
	}
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return "Error"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
