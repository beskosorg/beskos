package httpapi

import (
	"encoding/base64"
	"errors"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/auth"
	"github.com/beskosorg/beskos/backend/internal/db"
)

// User profile (avatar menu): name, job title, phone, photo, and self-service password change.
// PERSONAL security (MFA) uses the existing /mfa/* endpoints.

const maxAvatarBytes = 150 << 10 // a ~128px data-URL fits comfortably; above this it's the wrong upload

func (s *Server) handleGetMyProfile(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	fresh, err := s.q.GetUserByID(r.Context(), u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "profile.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"email": fresh.Email, "name": fresh.Name, "job_title": fresh.JobTitle,
		"phone": fresh.Phone, "avatar": fresh.Avatar,
		// The address outside this domain that can be used to get back in. Empty is normal for an
		// employee -- their way back is an administrator. See password_recovery.go.
		"recovery_email": fresh.RecoveryEmail,
	})
}

func (s *Server) handleUpdateMyProfile(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	var req struct {
		Name     string `json:"name"`
		JobTitle string `json:"job_title"`
		Phone    string `json:"phone"`
		Avatar   string `json:"avatar"`
		// Where a reset link may be sent. It has to be OUTSIDE this domain: an address inside it is
		// the mailbox the person is locked out of, which is the circularity this whole flow exists
		// to break.
		RecoveryEmail string `json:"recovery_email"`
	}
	if !decode(w, r, &req) {
		return
	}
	rec := strings.ToLower(strings.TrimSpace(req.RecoveryEmail))
	if rec != "" {
		if !strings.Contains(rec, "@") || strings.HasSuffix(rec, "@"+domainOf(u.Email)) {
			writeErr(w, http.StatusBadRequest, "profile.recovery_email_invalid")
			return
		}
	}
	if err := s.q.SetRecoveryEmail(r.Context(), db.SetRecoveryEmailParams{ID: u.ID, RecoveryEmail: rec}); err != nil {
		writeErr(w, http.StatusInternalServerError, "profile.failed")
		return
	}
	if len(req.Avatar) > maxAvatarBytes {
		writeErr(w, http.StatusBadRequest, "profile.avatar_too_large")
		return
	}
	// Photo: either empty (remove) or an image data-URL — never HTML/an external URL (XSS/tracking).
	if req.Avatar != "" && !strings.HasPrefix(req.Avatar, "data:image/") {
		writeErr(w, http.StatusBadRequest, "profile.avatar_invalid")
		return
	}
	if err := s.q.UpdateMyProfile(r.Context(), db.UpdateMyProfileParams{
		ID: u.ID, Name: strings.TrimSpace(req.Name), JobTitle: strings.TrimSpace(req.JobTitle),
		Phone: strings.TrimSpace(req.Phone), Avatar: req.Avatar,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "profile.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleChangeMyPassword changes the user's own password with STEP-UP (current password required).
// With a mailbox, changes it on Stalwart AND rewrites jmap_cred (otherwise the JMAP proxy would be
// left with the old credential); drops the user's other sessions (the current one stays).
func (s *Server) handleChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.New) < 10 {
		writeErr(w, http.StatusBadRequest, "profile.password_weak")
		return
	}
	fresh, err := s.q.GetUserByID(r.Context(), u.ID)
	if err != nil || !s.verifyUserPassword(r.Context(), fresh, req.Current) {
		writeErr(w, http.StatusForbidden, "auth.invalid_credentials")
		return
	}
	if fresh.StalwartAccountID != nil {
		if err := s.sw.SetAccountPassword(r.Context(), *fresh.StalwartAccountID, req.New); err != nil {
			log.Printf("profile: Stalwart refused: %v", err)
			if errors.Is(err, stalwart.ErrWeakPassword) {
				writeErr(w, http.StatusBadRequest, "mailbox.password_weak")
				return
			}
			writeErr(w, http.StatusBadGateway, "mailbox.stalwart_failed")
			return
		}
		// JMAP credential follows the new password (brokering H11).
		basic := base64.StdEncoding.EncodeToString([]byte(fresh.Email + ":" + req.New))
		_ = s.q.SetJmapCred(r.Context(), db.SetJmapCredParams{ID: u.ID, JmapCred: s.encTOTP(basic)})
	}
	if hash, herr := auth.HashPassword(req.New); herr == nil {
		_ = s.q.UpdateUserPassword(r.Context(), db.UpdateUserPasswordParams{ID: u.ID, PasswordHash: hash})
	}
	_ = s.q.DeleteUserWebmailSessionsExcept(r.Context(), db.DeleteUserWebmailSessionsExceptParams{
		MailUserID: u.ID, KeepToken: sessionToken(r),
	})
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email,
		Action: "user.password_change_self", Target: "",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
