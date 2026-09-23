// Package stalwart is our typed client for Stalwart v0.16.4.
//
// The REST Management API was REMOVED in 0.16 — there is no OpenAPI spec to generate code from.
// All administration goes through JMAP, capability "urn:stalwart:jmap", with methods in the "x:"
// namespace.
//
// License constraint: we only ever talk to Stalwart over the network. Nothing here links,
// embeds, or modifies the Stalwart binary.
package stalwart

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Capabilities used in every administrative request.
var adminUsing = []string{"urn:ietf:params:jmap:core", "urn:stalwart:jmap"}

// Client speaks JMAP with a Stalwart. The credential is the GLOBAL recovery admin
// (STALWART_RECOVERY_ADMIN). Only the Go backend loads it — never the frontend (brief §2).
type Client struct {
	baseURL  string // e.g.: http://stalwart:8080
	adminID  string // basic-auth user, e.g.: "admin"
	adminPwd string
	http     *http.Client
}

func New(baseURL, adminUser, adminPwd string) *Client {
	return &Client{
		baseURL:  baseURL,
		adminID:  adminUser,
		adminPwd: adminPwd,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// BaseURL returns Stalwart's base URL (e.g.: http://stalwart:8080). Used by the control plane's
// JMAP proxy (the native app has no same-origin reverse proxy; see jmap_proxy.go).
func (c *Client) BaseURL() string { return c.baseURL }

// --- JMAP envelope -----------------------------------------------------------

type methodCall [3]any // [ "x:Object/fn", args, callId ]

type jmapRequest struct {
	Using       []string     `json:"using"`
	MethodCalls []methodCall `json:"methodCalls"`
}

type jmapResponse struct {
	MethodResponses []json.RawMessage `json:"methodResponses"`
	SessionState    string            `json:"sessionState"`
}

// APIError is a request-level JMAP error (e.g.: notRequest, forbidden during bootstrap).
type APIError struct {
	Type   string `json:"type"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("stalwart jmap error: status=%d type=%s detail=%s", e.Status, e.Type, e.Detail)
}

// call executes a single method call and returns the raw payload of that method's response.
// Idempotency (brief §10): the caller uses deterministic creationIds/updates; retry is safe for
// set operations as long as the existence check precedes the create in the control plane.
func (c *Client) call(ctx context.Context, method string, args any) (json.RawMessage, error) {
	body, err := json.Marshal(jmapRequest{
		Using:       adminUsing,
		MethodCalls: []methodCall{{method, args, "c0"}},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/jmap/", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.adminID, c.adminPwd)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stalwart request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Request-level errors come as a JMAP error object (status != 200).
	if resp.StatusCode != http.StatusOK {
		var apiErr APIError
		if json.Unmarshal(raw, &apiErr) == nil && apiErr.Type != "" {
			return nil, &apiErr
		}
		return nil, fmt.Errorf("stalwart http %d: %s", resp.StatusCode, string(raw))
	}

	var jr jmapResponse
	if err := json.Unmarshal(raw, &jr); err != nil {
		return nil, fmt.Errorf("decode jmap response: %w (body=%s)", err, string(raw))
	}
	if len(jr.MethodResponses) == 0 {
		return nil, fmt.Errorf("stalwart: response has no methodResponses")
	}

	// methodResponses[0] = [ method, payload, callId ]. We extract the payload (index 1).
	var triple []json.RawMessage
	if err := json.Unmarshal(jr.MethodResponses[0], &triple); err != nil || len(triple) < 2 {
		return nil, fmt.Errorf("stalwart: malformed methodResponse: %s", string(jr.MethodResponses[0]))
	}
	// Detects a method-level error (e.g.: {"type":"...","description":"..."} under "error").
	var maybeName string
	_ = json.Unmarshal(triple[0], &maybeName)
	if maybeName == "error" {
		me := &MethodError{Raw: string(triple[1])}
		_ = json.Unmarshal(triple[1], me)
		return nil, me
	}
	return triple[1], nil
}

// MethodError is a method-level JMAP error (e.g.: forbidden in bootstrap mode, invalidPatch).
type MethodError struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Raw         string `json:"-"`
}

func (e *MethodError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("stalwart method error: %s (%s)", e.Type, e.Description)
	}
	return "stalwart method error: " + e.Raw
}

// setResult is the standard response of an x:Object/set.
type setResult struct {
	Created map[string]struct {
		ID string `json:"id"`
	} `json:"created"`
	Updated    map[string]json.RawMessage `json:"updated"`
	Destroyed  []string                   `json:"destroyed"`
	NotCreated map[string]json.RawMessage `json:"notCreated"`
	NotUpdated map[string]json.RawMessage `json:"notUpdated"`
}

func firstCreatedID(r *setResult) (string, error) {
	for _, v := range r.Created {
		return v.ID, nil
	}
	if len(r.NotCreated) > 0 {
		b, _ := json.Marshal(r.NotCreated)
		err := fmt.Errorf("stalwart notCreated: %s", string(b))
		if isWeakSecret(r.NotCreated) {
			return "", fmt.Errorf("%w: %v", ErrWeakPassword, err)
		}
		return "", err
	}
	return "", fmt.Errorf("stalwart: no object created in the response")
}

// ErrWeakPassword: Stalwart has a strength meter (zxcvbn) and refuses the password before
// creating the mailbox. This is an error from WHOEVER MADE THE REQUEST, not from the mail
// server — the caller should respond with 400 and explanatory text, instead of the generic 502
// that just said "Bad gateway".
var ErrWeakPassword = errors.New("stalwart: password too weak")

// isWeakSecret recognizes the password refusal in the set response (invalidProperties on `secret`).
func isWeakSecret(m map[string]json.RawMessage) bool {
	for _, raw := range m {
		var e struct {
			Type        string   `json:"type"`
			Description string   `json:"description"`
			Properties  []string `json:"properties"`
		}
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		if e.Type != "invalidProperties" {
			continue
		}
		for _, p := range e.Properties {
			// "secret" on creation, "credentials/0/secret" on password change.
			if p == "secret" || strings.HasSuffix(p, "/secret") {
				return true
			}
		}
	}
	return false
}
