package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Device tokens: authentication for headless clients (the sync daemon). Security properties:
//   - the raw token (32 random bytes, base64url, "mlnd_" prefix) is shown ONCE, at creation;
//   - only its SHA-256 (hex) is stored — a database leak doesn't expose usable tokens;
//   - narrow scope: the token only works on the /api/sync/* group (read changes + download),
//     never the rest of the API nor the session flow;
//   - revocable per device, with an optional expiry.
// Managing tokens (create/list/revoke) sits behind the webmail session — users provision their
// own devices.

const deviceTokenPrefix = "mlnd_"

// hashDeviceToken returns the SHA-256 in hex of the raw token — the lookup key stored in the database.
func hashDeviceToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func newDeviceToken() (raw, prefix string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	raw = deviceTokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	// Short, non-sensitive prefix, just so the user can recognize the token in the list.
	prefix = raw[:len(deviceTokenPrefix)+6]
	return raw, prefix, nil
}

func (s *Server) handleCreateDeviceToken(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		Name      string `json:"name"`
		ExpiresIn int    `json:"expires_in_days"` // 0 = never expires
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Name) == 0 || len(req.Name) > 80 {
		writeErr(w, http.StatusBadRequest, "devices.invalid_name")
		return
	}
	raw, prefix, err := newDeviceToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "devices.token_gen_failed")
		return
	}
	var expires pgtype.Timestamptz
	if req.ExpiresIn > 0 {
		expires = pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, req.ExpiresIn), Valid: true}
	}
	row, err := s.q.CreateDeviceToken(r.Context(), db.CreateDeviceTokenParams{
		TenantID: mu.TenantID, MailUserID: mu.ID, Name: req.Name,
		TokenHash: hashDeviceToken(raw), TokenPrefix: prefix, ExpiresAt: expires,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "devices.create_failed")
		return
	}
	// The raw token is only ever returned HERE (never again). The client stores it; we only keep the hash.
	writeJSON(w, http.StatusOK, map[string]any{
		"id": row.ID, "name": row.Name, "token": raw, "prefix": row.TokenPrefix, "created_at": row.CreatedAt,
	})
}

func (s *Server) handleListDeviceTokens(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListDeviceTokens(r.Context(), mu.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "devices.list_failed")
		return
	}
	if rows == nil {
		rows = []db.ListDeviceTokensRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": rows})
}

func (s *Server) handleRevokeDeviceToken(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "devices.invalid_id")
		return
	}
	if err := s.q.RevokeDeviceToken(r.Context(), db.RevokeDeviceTokenParams{ID: id, MailUserID: mu.ID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "devices.revoke_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// requireDeviceToken authenticates the daemon via the device token (Bearer or ?access_token=) and
// injects the mail_user into the context — sync handlers reuse mailUserFrom without knowing the source.
func (s *Server) requireDeviceToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := sessionToken(r)
		if raw == "" {
			writeErr(w, http.StatusUnauthorized, "auth.no_token")
			return
		}
		row, err := s.q.GetDeviceTokenUser(r.Context(), hashDeviceToken(raw))
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "auth.invalid_token")
			return
		}
		// Records "last seen" for audit purposes; a failure here doesn't block the request.
		_ = s.q.TouchDeviceToken(r.Context(), row.TokenID)
		ctx := withMailUser(r.Context(), row.MailUser)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
