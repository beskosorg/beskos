package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/assistant"
)

// OpenAI-compatible proxy so the OnlyOffice AI plugin can use DeepSeek WITHOUT the real key ever
// leaving the server. The plugin runs in the Document Server's iframe (another origin) and
// authenticates with a stable user token (Bearer) — which here turns into identity, quota, and
// forwarding with the real key. Only within the authenticated editor; the quota is the user's own
// (the same 6h window as the assistant's). See docs/AI-ASSISTANT.md (§9.2).

// aiProxyHTTP has a generous timeout: an AI completion takes longer than a normal request.
var aiProxyHTTP = &http.Client{Timeout: 120 * time.Second}

// EnableAIProxy stores the DeepSeek endpoint/key/model that the proxy forwards to. The key stays
// ONLY here, on the server. Without a key, the proxy reports itself as disabled.
func (s *Server) EnableAIProxy(baseURL, apiKey, model string) {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	if model == "" {
		model = "deepseek-chat"
	}
	s.aiProxyBase = strings.TrimRight(baseURL, "/")
	s.aiProxyKey = apiKey
	s.aiProxyModel = model
}

func (s *Server) aiProxyReady(w http.ResponseWriter) bool {
	if s.office == nil || s.aiProxyKey == "" {
		writeErr(w, http.StatusServiceUnavailable, "assistant.disabled")
		return false
	}
	return true
}

// handleAIEditorKey returns to the AUTHENTICATED editor the config for the AI plugin to point at
// our proxy: OpenAI-compatible base URL + "API key" (stable user token) + model. The DeepSeek key
// never appears here.
func (s *Server) handleAIEditorKey(w http.ResponseWriter, r *http.Request) {
	if !s.aiProxyReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	tok := s.office.mintAIToken(mu.ID, mu.TenantID, time.Now().Add(365*24*time.Hour))
	writeJSON(w, http.StatusOK, map[string]any{
		"base_url": s.publicURL + "/api/ai/openai/v1",
		"api_key":  tok,
		"model":    s.aiProxyModel,
	})
}

// aiCORS handles the plugin's cross-origin requests (Document Server iframe). Answers the
// preflight and sets the headers. Returns true if it already responded (OPTIONS) and the handler
// should stop.
func (s *Server) aiCORS(w http.ResponseWriter, r *http.Request) bool {
	if s.office != nil && s.office.publicURL != "" {
		w.Header().Set("Access-Control-Allow-Origin", s.office.publicURL)
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	w.Header().Set("Access-Control-Max-Age", "600")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

// aiAuth validates the Bearer (the user's AI token) and returns the Identity. No cookie: the DS
// iframe doesn't have one.
func (s *Server) aiAuth(r *http.Request) (assistant.Identity, bool) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	userID, tenantID, ok := s.office.parseAIToken(tok)
	if !ok {
		return assistant.Identity{}, false
	}
	return assistant.Identity{UserID: userID, TenantID: tenantID}, true
}

// handleAIModels responds with the model list (the plugin requests /v1/models). Only our own
// model, so the user can't pick another one the proxy doesn't serve.
func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	if s.aiCORS(w, r) || !s.aiProxyReady(w) {
		return
	}
	if _, ok := s.aiAuth(r); !ok {
		aiError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   []map[string]any{{"id": s.aiProxyModel, "object": "model", "owned_by": "beskos"}},
	})
}

// handleAIModelsEditor responds to /v1/models for the Document Server's ai-proxy (only our own
// model). The model list isn't sensitive; the proxy just needs to be enabled.
func (s *Server) handleAIModelsEditor(w http.ResponseWriter, r *http.Request) {
	if s.aiCORS(w, r) || !s.aiProxyReady(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   []map[string]any{{"id": s.aiProxyModel, "object": "model", "owned_by": "beskos"}},
	})
}

// handleAIChatCompletions is the heart of the proxy: authenticates, checks the quota, forces our
// own model, forwards to DeepSeek with the real key, and counts the tokens spent against the
// user's quota.
func (s *Server) handleAIChatCompletions(w http.ResponseWriter, r *http.Request) {
	if s.aiCORS(w, r) || !s.aiProxyReady(w) {
		return
	}
	id, ok := s.aiAuth(r)
	if !ok {
		aiError(w, http.StatusUnauthorized, "invalid api key")
		return
	}
	// The user's own quota (same window as the assistant's). Exhausted → refuse, same as in chat.
	if s.assistantQuotaStatus(r.Context(), id, time.Now()).Exceeded() {
		aiError(w, http.StatusTooManyRequests, "AI quota exceeded for this period")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		aiError(w, http.StatusBadRequest, "invalid request")
		return
	}
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		aiError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// Forces our own model (the user doesn't get to pick another) and, in streaming mode, requests
	// usage at the end so we can count the tokens.
	req["model"] = s.aiProxyModel
	stream, _ := req["stream"].(bool)
	if stream {
		req["stream_options"] = map[string]any{"include_usage": true}
	}
	out, _ := json.Marshal(req)

	fReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.aiProxyBase+"/chat/completions", bytes.NewReader(out))
	if err != nil {
		aiError(w, http.StatusInternalServerError, "proxy error")
		return
	}
	fReq.Header.Set("Content-Type", "application/json")
	fReq.Header.Set("Authorization", "Bearer "+s.aiProxyKey)
	if stream {
		fReq.Header.Set("Accept", "text/event-stream")
	}
	resp, err := aiProxyHTTP.Do(fReq)
	if err != nil {
		aiError(w, http.StatusBadGateway, "AI provider unreachable")
		return
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)

	// Records the spend against the quota after the response goes through. An error counting it
	// doesn't affect the user — but the actual spend must still count (hence context.WithoutCancel:
	// it survives the connection closing).
	tokens := pipeAndCountTokens(w, resp.Body, stream)
	if tokens > 0 {
		s.recordAssistantUsage(context.WithoutCancel(r.Context()), id, tokens)
	}
}

// pipeAndCountTokens copies the DeepSeek response to the client and returns the total tokens. In
// streaming mode, it reads line by line and picks up the "usage" from the last chunk (requested
// with include_usage); for plain JSON, it deserializes and reads usage.total_tokens.
func pipeAndCountTokens(w http.ResponseWriter, body io.Reader, stream bool) int {
	flusher, _ := w.(http.Flusher)
	if !stream {
		data, _ := io.ReadAll(io.LimitReader(body, 8<<20))
		_, _ = w.Write(data)
		var parsed struct {
			Usage struct {
				TotalTokens int `json:"total_tokens"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(data, &parsed)
		return parsed.Usage.TotalTokens
	}
	tokens := 0
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		_, _ = w.Write(line)
		_, _ = w.Write([]byte("\n"))
		if flusher != nil {
			flusher.Flush()
		}
		trimmed := strings.TrimSpace(strings.TrimPrefix(string(line), "data:"))
		if trimmed == "" || trimmed == "[DONE]" {
			continue
		}
		var chunk struct {
			Usage *struct {
				TotalTokens int `json:"total_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(trimmed), &chunk) == nil && chunk.Usage != nil {
			tokens = chunk.Usage.TotalTokens
		}
	}
	return tokens
}

// handleAIEnvelopeProxy is the proxy for the SERVER MODE of the OnlyOffice AI plugin
// (aiSettings.proxy). The plugin sends an envelope {target, method, headers, data} with the
// EDITOR's JWT as the Bearer token. From here it validates the JWT (proves the session), derives
// the user (editorConfig.user.id) -> tenant, checks the quota, and forwards the inner request
// (data) to DeepSeek with OUR OWN key — ignoring the envelope's target/headers (the DeepSeek key
// never comes from the browser). This keeps the config on the server, with no key pasted into the
// client, and the quota charged is the user's own.
func (s *Server) handleAIEnvelopeProxy(w http.ResponseWriter, r *http.Request) {
	if s.aiCORS(w, r) || !s.aiProxyReady(w) {
		return
	}
	// Identity from the editor's JWT (signed by us; the secret is shared with the DS).
	jwt := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	payload, ok := s.office.jwtVerify(jwt)
	if !ok {
		aiError(w, http.StatusUnauthorized, "invalid editor token")
		return
	}
	userID := userIDFromEditorJWT(payload)
	if userID == uuid.Nil {
		aiError(w, http.StatusUnauthorized, "no user in token")
		return
	}
	mu, err := s.q.GetUserByID(r.Context(), userID)
	if err != nil {
		aiError(w, http.StatusUnauthorized, "unknown user")
		return
	}
	id := assistant.Identity{UserID: mu.ID, TenantID: mu.TenantID}
	if s.assistantQuotaStatus(r.Context(), id, time.Now()).Exceeded() {
		aiError(w, http.StatusTooManyRequests, "AI quota exceeded for this period")
		return
	}

	// The body may arrive as an ENVELOPE {target, headers, data} (the OpenAI request goes in `data`)
	// OR already as the raw OpenAI request — depends on how the Document Server forwards it. Accepts
	// both. The envelope's `target`/`headers` are ignored on purpose: we don't follow wherever
	// they're told to, nor use an outside key.
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	var req map[string]any
	if json.Unmarshal(raw, &env) == nil && len(env.Data) > 0 {
		req = decodeInnerRequest(env.Data)
	}
	if req == nil { // no `data`: the body already IS the OpenAI request
		req = decodeInnerRequest(raw)
	}
	if req == nil || req["messages"] == nil {
		aiError(w, http.StatusBadRequest, "invalid request")
		return
	}
	req["model"] = s.aiProxyModel
	stream, _ := req["stream"].(bool)
	if stream {
		req["stream_options"] = map[string]any{"include_usage": true}
	}
	out, _ := json.Marshal(req)

	fReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.aiProxyBase+"/chat/completions", bytes.NewReader(out))
	if err != nil {
		aiError(w, http.StatusInternalServerError, "proxy error")
		return
	}
	fReq.Header.Set("Content-Type", "application/json")
	fReq.Header.Set("Authorization", "Bearer "+s.aiProxyKey)
	if stream {
		fReq.Header.Set("Accept", "text/event-stream")
	}
	resp, err := aiProxyHTTP.Do(fReq)
	if err != nil {
		aiError(w, http.StatusBadGateway, "AI provider unreachable")
		return
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	tokens := pipeAndCountTokens(w, resp.Body, stream)
	if tokens > 0 {
		s.recordAssistantUsage(context.WithoutCancel(r.Context()), id, tokens)
	}
}

// userIDFromEditorJWT extracts the user id from the JWT the Document Server gives the plugin. The
// format isn't documented (a gap in the docs), so it tries the plausible spots: editorConfig.user.id,
// user.id, top-level userId. The first one that's a valid UUID wins.
func userIDFromEditorJWT(payload map[string]any) uuid.UUID {
	candidates := []string{}
	// The JWT the Document Server gives to the ai-proxy carries the user at payload.user.id
	// (confirmed live). We also keep the other plausible spots for robustness.
	if p, ok := payload["payload"].(map[string]any); ok {
		if usr, ok := p["user"].(map[string]any); ok {
			if s, _ := usr["id"].(string); s != "" {
				candidates = append(candidates, s)
			}
		}
	}
	if ec, ok := payload["editorConfig"].(map[string]any); ok {
		if usr, ok := ec["user"].(map[string]any); ok {
			if s, _ := usr["id"].(string); s != "" {
				candidates = append(candidates, s)
			}
		}
	}
	if usr, ok := payload["user"].(map[string]any); ok {
		if s, _ := usr["id"].(string); s != "" {
			candidates = append(candidates, s)
		}
	}
	for _, k := range []string{"userId", "userid", "uid", "sub"} {
		if s, _ := payload[k].(string); s != "" {
			candidates = append(candidates, s)
		}
	}
	for _, s := range candidates {
		if uid, err := uuid.Parse(s); err == nil {
			return uid
		}
	}
	return uuid.Nil
}

// decodeInnerRequest reads the OpenAI request from inside the envelope. The `data` field may
// arrive as a JSON object or as a string containing JSON (the plugin's fetch body) — handles both.
func decodeInnerRequest(data json.RawMessage) map[string]any {
	var asStr string
	if json.Unmarshal(data, &asStr) == nil {
		data = json.RawMessage(asStr)
	}
	var req map[string]any
	if json.Unmarshal(data, &req) != nil {
		return nil
	}
	return req
}

// aiError responds in OpenAI's error format, which is what the plugin knows how to read.
func aiError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": msg, "type": "invalid_request_error"}})
}
