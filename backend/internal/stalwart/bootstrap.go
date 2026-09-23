package stalwart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// A fresh server comes up in bootstrap mode: any x:* method (except x:Bootstrap) returns forbidden.
// Bootstrap has to be completed ONCE and the process restarted (DIVERGENCES §D3).
// We model this as an idempotent control-plane setup step (not to be confused with provisioning a tenant).

type BootstrapInput struct {
	ServerHostname string // e.g.: mail.beskos.app
	DefaultDomain  string // e.g.: beskos.app
}

// BootstrapResult carries the generated permanent admin (secret shown only once).
type BootstrapResult struct {
	Username string `json:"username"`
	Secret   string `json:"secret"`
}

// Bootstrap completes the initial setup. After calling it, restarting the Stalwart process is MANDATORY.
func (c *Client) Bootstrap(ctx context.Context, in BootstrapInput) (*BootstrapResult, error) {
	args := map[string]any{
		"update": map[string]any{
			"singleton": map[string]any{
				"serverHostname":        in.ServerHostname,
				"defaultDomain":         in.DefaultDomain,
				"requestTlsCertificate": false, // avoids ACME in dev; production turns this on
				"generateDkimKeys":      true,
			},
		},
	}
	raw, err := c.call(ctx, "x:Bootstrap/set", args)
	if err != nil {
		return nil, err
	}
	var r struct {
		Updated map[string]json.RawMessage `json:"updated"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("decode Bootstrap/set: %w", err)
	}
	upd, ok := r.Updated["singleton"]
	if !ok {
		return nil, errors.New("stalwart: bootstrap returned no updated singleton")
	}
	var res BootstrapResult
	if err := json.Unmarshal(upd, &res); err != nil {
		return nil, fmt.Errorf("decode bootstrap result: %w", err)
	}
	return &res, nil
}

// IsBootstrapped attempts a lightweight administrative read; if the server is still in bootstrap
// mode the call fails with forbidden. Used by the control plane to decide whether it needs to bootstrap.
func (c *Client) IsBootstrapped(ctx context.Context) (bool, error) {
	_, err := c.call(ctx, "x:Domain/query", map[string]any{})
	if err == nil {
		return true, nil
	}
	// Bootstrap mode responds with forbidden — it can come as a request-level error (APIError) or
	// a method-level one.
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Type != "" {
		return false, nil
	}
	var methErr *MethodError
	if errors.As(err, &methErr) && methErr.Type == "forbidden" {
		return false, nil
	}
	return false, err
}
