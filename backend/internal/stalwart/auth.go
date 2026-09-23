package stalwart

import (
	"context"
	"net/http"
)

// VerifyUser confirms an end user's (mailbox) credential by making a JMAP session request with
// Basic auth. Used to authenticate the webmail against OUR API without storing the user's
// password — Basic auth is the only viable path on this Stalwart version. Returns true on HTTP 200.
func (c *Client) VerifyUser(ctx context.Context, email, password string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/jmap/session", nil)
	if err != nil {
		return false, err
	}
	req.SetBasicAuth(email, password)
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}
