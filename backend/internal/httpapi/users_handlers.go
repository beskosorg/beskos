package httpapi

import (
	"encoding/base64"
	"errors"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/auth"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/rbac"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

// "User" = e-mail mailbox + panel account with a role (RBAC). Users and Members are the same
// thing; what tells them apart is the role. Creating a user provisions the mailbox AND the
// account together.

type createUserReq struct {
	DomainID string   `json:"domain_id"`
	Login    string   `json:"login"`
	Name     string   `json:"name"`
	Password string   `json:"password"`
	Role     string   `json:"role"`
	QuotaGB  int64    `json:"quota_gb"`
	Groups   []string `json:"groups"` // the user can join several groups at once
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	actor := mailUserFrom(r.Context())
	var req createUserReq
	if !decode(w, r, &req) {
		return
	}
	role := rbac.Role(req.Role)
	if req.Login == "" || req.Password == "" || !rbac.IsAssignableRole(role) {
		writeErr(w, http.StatusBadRequest, "users.invalid")
		return
	}
	// Anti-escalation: only an owner can create another owner.
	if role == rbac.RoleOwner && rbac.Role(actor.Role) != rbac.RoleOwner {
		writeErr(w, http.StatusForbidden, "invites.owner_only")
		return
	}
	domID, err := uuid.Parse(req.DomainID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "domains.invalid_id")
		return
	}
	d, err := s.q.GetDomainByID(r.Context(), domID)
	if err != nil || d.TenantID != actor.TenantID {
		writeErr(w, http.StatusNotFound, "domains.not_found")
		return
	}
	if d.ProvisionState != "active" {
		writeErr(w, http.StatusConflict, "domains.not_active")
		return
	}
	// Mailbox quota.
	plan := s.tenantPlan(r, d.TenantID)
	if count, _ := s.q.CountMailboxesByTenant(r.Context(), d.TenantID); plan.MailboxesExceeded(int(count)) {
		writeErr(w, http.StatusPaymentRequired, "quota.mailboxes")
		return
	}
	quota := req.QuotaGB
	if quota <= 0 {
		quota = 5
	}
	email := req.Login + "@" + d.Domain

	// Mailbox in Stalwart + the user (unified identity: a single record with mailbox, password, and role).
	acctID, err := s.sw.CreateAccount(r.Context(), stalwart.AccountInput{
		Login: req.Login, DomainID: strval(d.StalwartID), DisplayName: req.Name, Password: req.Password, QuotaBytes: quota << 30,
	})
	if err != nil {
		log.Printf("mailbox operation in Stalwart failed: %v", err)
		if errors.Is(err, stalwart.ErrWeakPassword) {
			writeErr(w, http.StatusBadRequest, "mailbox.password_weak")
			return
		}
		writeErr(w, http.StatusBadGateway, "mailbox.stalwart_failed")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "auth.password_hash_failed")
		return
	}
	pu, err := s.q.CreateMailUser(r.Context(), db.CreateMailUserParams{
		TenantID: d.TenantID, Email: email, Name: req.Name, Role: req.Role,
		StalwartAccountID: &acctID, QuotaBytes: quota << 30, PasswordHash: hash,
	})
	if err != nil {
		writeErr(w, http.StatusConflict, "mailbox.already_exists")
		return
	}
	// Groups chosen at creation time (also joins the corresponding chat channels).
	if len(req.Groups) > 0 {
		s.syncUserGroups(r.Context(), d.TenantID, pu.ID, req.Groups)
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: actor.TenantID, Valid: true}, Actor: actor.Email, Action: "user.create", Target: email})
	s.syncSeats(actor.TenantID) // seats in Stripe follow along (prorated within the period)
	writeJSON(w, http.StatusCreated, map[string]any{"id": pu.ID, "email": email, "role": req.Role})
}

type resetUserPwReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleResetUserPassword changes the user's password on BOTH ends (mailbox in Stalwart + panel).
func (s *Server) handleResetUserPassword(w http.ResponseWriter, r *http.Request) {
	actor := mailUserFrom(r.Context())
	var req resetUserPwReq
	if !decode(w, r, &req) {
		return
	}
	if req.Password == "" {
		writeErr(w, http.StatusBadRequest, "mailbox.missing_fields")
		return
	}
	mu, err := s.q.GetMailUserByEmail(r.Context(), req.Email)
	if err != nil || mu.TenantID != actor.TenantID || mu.StalwartAccountID == nil {
		writeErr(w, http.StatusNotFound, "mailbox.not_found")
		return
	}
	// Anti-escalation (law §5.2): the actor can only reset the password of someone of lower rank.
	// Without this, an admin could reset the owner's password and take over the account + mailbox
	// (via IMAP/JMAP, without MFA).
	if mu.ID != actor.ID && !rbac.CanManage(rbac.Role(actor.Role), rbac.Role(mu.Role)) {
		writeErr(w, http.StatusForbidden, "rbac.forbidden")
		return
	}
	if err := s.sw.SetAccountPassword(r.Context(), *mu.StalwartAccountID, req.Password); err != nil {
		log.Printf("mailbox operation in Stalwart failed: %v", err)
		writeErr(w, http.StatusBadGateway, "mailbox.stalwart_failed")
		return
	}
	// Also updates the panel account (same identity), if it exists.
	if hash, err := auth.HashPassword(req.Password); err == nil {
		_ = s.q.UpdateUserPasswordByEmail(r.Context(), db.UpdateUserPasswordByEmailParams{
			Email: req.Email, TenantID: actor.TenantID, PasswordHash: hash,
		})
	}
	// OWN password: the stored JMAP credential (Basic with the old password) would stop working
	// and the webmail would start returning 401 until the next login. Rewrite it with the new one.
	if mu.ID == actor.ID {
		basic := base64.StdEncoding.EncodeToString([]byte(req.Email + ":" + req.Password))
		_ = s.q.SetJmapCred(r.Context(), db.SetJmapCredParams{ID: mu.ID, JmapCred: s.encTOTP(basic)})
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: actor.TenantID, Valid: true}, Actor: actor.Email, Action: "user.password_reset", Target: req.Email})
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
