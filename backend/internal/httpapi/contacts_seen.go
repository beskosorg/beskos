package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/mail"
	"strings"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// recordRecipientContacts creates a contact (person) for each address a message is sent to that
// isn't yet in the user's address book — so it shows up next time in the "To" autocomplete.
// Idempotent: an address already known is skipped. Failures don't interrupt anything (this is a
// side effect of sending, not the send itself). Scoped by tenant+owner, like everything else (law 5.1).
func (s *Server) recordRecipientContacts(ctx context.Context, tenantID, userID uuid.UUID, addrs []string) {
	// Names and emails already known, to avoid duplicates. The key is the lowercased email.
	known := map[string]bool{}
	rows, err := s.q.ListContacts(ctx, db.ListContactsParams{OwnerUserID: userID, TenantID: tenantID})
	if err != nil {
		return
	}
	for _, c := range rows {
		if e := strings.ToLower(strings.TrimSpace(c.Email)); e != "" {
			known[e] = true
		}
	}
	for _, raw := range addrs {
		name, email := parseRecipient(raw)
		email = strings.ToLower(email)
		if email == "" || known[email] {
			continue
		}
		if name == "" {
			name = email // no name in the address: use the email itself
		}
		if _, err := s.q.CreateContact(ctx, db.CreateContactParams{
			TenantID: tenantID, OwnerUserID: userID, Kind: "person", Name: name, Email: email,
			Tags: []string{}, ExtraEmails: []string{}, ExtraPhones: []string{},
			AllowPhone: true, AllowEmail: true,
			// custom_fields is jsonb NOT NULL: without this the driver sends NULL and the INSERT
			// fails silently (the loop does `continue` and no contact gets saved).
			CustomFields: json.RawMessage("{}"),
		}); err != nil {
			continue
		}
		known[email] = true // don't repeat within the same batch (To and Cc can overlap)
	}
}

// parseRecipient extracts name and email from "Name <a@b>" or "a@b". Without a valid email, returns empty.
func parseRecipient(raw string) (name, email string) {
	if a, err := mail.ParseAddress(strings.TrimSpace(raw)); err == nil {
		return strings.TrimSpace(a.Name), strings.TrimSpace(a.Address)
	}
	s := strings.TrimSpace(raw)
	if strings.Contains(s, "@") && !strings.ContainsAny(s, " <>") {
		return "", s
	}
	return "", ""
}

type seenRecipientsReq struct {
	Recipients []string `json:"recipients"`
}

// handleSeenRecipients records as contacts the recipients of an email just sent from the
// webmail (manual sending is done by the client via JMAP, which is why the client notifies us).
// Sending via the assistant records it server-side (see assistantMailAccess).
func (s *Server) handleSeenRecipients(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req seenRecipientsReq
	if !decode(w, r, &req) {
		return
	}
	s.recordRecipientContacts(r.Context(), mu.TenantID, mu.ID, req.Recipients)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
