package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/auth"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/rbac"
)

const inviteTTL = 7 * 24 * time.Hour

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	members, err := s.q.ListMembers(r.Context(), u.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "members.failed")
		return
	}
	writeJSON(w, http.StatusOK, members)
}

type inviteReq struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// handleCreateInvite creates an invite. Security: the role must be assignable; only an OWNER can
// invite as owner (prevents an admin from promoting someone — including themselves — to owner).
func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	tid := u.TenantID
	var req inviteReq
	if !decode(w, r, &req) {
		return
	}
	role := rbac.Role(req.Role)
	if req.Email == "" || !rbac.IsAssignableRole(role) {
		writeErr(w, http.StatusBadRequest, "invites.invalid")
		return
	}
	if role == rbac.RoleOwner && rbac.Role(u.Role) != rbac.RoleOwner {
		writeErr(w, http.StatusForbidden, "invites.owner_only")
		return
	}
	token, err := auth.NewSessionToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "invites.failed")
		return
	}
	inv, err := s.q.CreateInvite(r.Context(), db.CreateInviteParams{
		TenantID: tid, Email: req.Email, Role: req.Role, Token: token,
		InvitedBy: u.Email, ExpiresAt: time.Now().Add(inviteTTL),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "invites.failed")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "invite.create", Target: req.Email})
	// Sending the invite email is an external hook; we return the token for the flow.
	writeJSON(w, http.StatusCreated, map[string]any{"id": inv.ID, "email": inv.Email, "role": inv.Role, "token": inv.Token})
}

func (s *Server) handleListInvites(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	invs, err := s.q.ListPendingInvites(r.Context(), u.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "invites.failed")
		return
	}
	writeJSON(w, http.StatusOK, invs)
}

func (s *Server) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	tid := u.TenantID
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invites.invalid_id")
		return
	}
	if err := s.q.RevokeInvite(r.Context(), db.RevokeInviteParams{ID: id, TenantID: tid}); err != nil {
		writeErr(w, http.StatusInternalServerError, "invites.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// handleGetInvite (public) validates an invite token for the acceptance screen.
func (s *Server) handleGetInvite(w http.ResponseWriter, r *http.Request) {
	inv, err := s.q.GetValidInvite(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "invites.not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"email": inv.Email, "role": inv.Role})
}

type acceptInviteReq struct {
	Token    string `json:"token"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

// handleAcceptInvite (public) creates the panel user from a valid invite.
func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	var req acceptInviteReq
	if !decode(w, r, &req) {
		return
	}
	if req.Password == "" {
		writeErr(w, http.StatusBadRequest, "auth.missing_fields")
		return
	}
	// Consumes the invite ATOMICALLY (single use, no race): marks it accepted and returns it only
	// if it was still valid. Only whoever gets the row proceeds to create the account.
	inv, err := s.q.ConsumeInvite(r.Context(), req.Token)
	if err != nil {
		writeErr(w, http.StatusNotFound, "invites.not_found")
		return
	}
	// Revalidates the invite's role (fail-closed in case the database has something unexpected).
	if !rbac.IsAssignableRole(rbac.Role(inv.Role)) {
		writeErr(w, http.StatusBadRequest, "invites.invalid")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "auth.password_hash_failed")
		return
	}
	u, err := s.q.CreateAccount(r.Context(), db.CreateAccountParams{
		TenantID: inv.TenantID, Email: inv.Email, PasswordHash: hash, Role: inv.Role,
	})
	if err != nil {
		writeErr(w, http.StatusConflict, "auth.email_taken")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: inv.TenantID, Valid: true}, Actor: inv.Email, Action: "invite.accept", Target: inv.Role})
	token, ok := s.issueSession(w, r.Context(), u)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user_id": u.ID, "role": u.Role, "token": token})
}

type changeRoleReq struct {
	Role string `json:"role"`
}

// handleChangeRole changes a member's role. Security: the role must be assignable; touching an
// owner (promoting or demoting) requires being an owner; the organization must never be left without an owner.
func (s *Server) handleChangeRole(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "members.invalid_id")
		return
	}
	var req changeRoleReq
	if !decode(w, r, &req) {
		return
	}
	newRole := rbac.Role(req.Role)
	if !rbac.IsAssignableRole(newRole) {
		writeErr(w, http.StatusBadRequest, "members.invalid_role")
		return
	}
	target, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: id, TenantID: u.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "members.not_found")
		return
	}
	involvesOwner := newRole == rbac.RoleOwner || rbac.Role(target.Role) == rbac.RoleOwner
	if involvesOwner && rbac.Role(u.Role) != rbac.RoleOwner {
		writeErr(w, http.StatusForbidden, "members.owner_only")
		return
	}
	// Never remove the last owner (demoting the only owner).
	if rbac.Role(target.Role) == rbac.RoleOwner && newRole != rbac.RoleOwner {
		if n, _ := s.q.CountOwners(r.Context(), u.TenantID); n <= 1 {
			writeErr(w, http.StatusConflict, "members.last_owner")
			return
		}
	}
	updated, err := s.q.UpdateUserRole(r.Context(), db.UpdateUserRoleParams{ID: id, TenantID: u.TenantID, Role: req.Role})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "members.failed")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "member.role_change", Target: updated.Email + "→" + req.Role})
	writeJSON(w, http.StatusOK, updated)
}

// handleRemoveMember removes a member. Doesn't allow removing the last owner.
func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "members.invalid_id")
		return
	}
	target, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: id, TenantID: u.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "members.not_found")
		return
	}
	if rbac.Role(target.Role) == rbac.RoleOwner {
		if n, _ := s.q.CountOwners(r.Context(), u.TenantID); n <= 1 {
			writeErr(w, http.StatusConflict, "members.last_owner")
			return
		}
	}
	if err := s.q.DeleteUserInTenant(r.Context(), db.DeleteUserInTenantParams{ID: id, TenantID: u.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "members.failed")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "member.remove", Target: target.Email})
	s.syncSeats(u.TenantID) // Stripe seats follow along (prorated credit)
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}
