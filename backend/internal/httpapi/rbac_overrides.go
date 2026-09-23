package httpapi

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/rbac"
)

// Per-member permission exceptions (granular RBAC). The role remains the starting point; this
// resolves the cases no factory role covers. The rules that matter:
//
//   - deny beats allow (rbac.Effective), so a contradiction always falls on the safe side;
//   - whoever edits can only grant/deny what THEY THEMSELVES have — otherwise an admin could give
//     themselves billing access through a proxy (privilege escalation);
//   - and only acts on someone of a lower rank (rbac.CanManage), the same rule as changing a role.

// memberOverrides reads a user's exceptions, already split into allow/deny.
func (s *Server) memberOverrides(ctx context.Context, tenant, user uuid.UUID) (allow, deny []rbac.Permission) {
	rows, err := s.q.ListMemberPermissions(ctx, db.ListMemberPermissionsParams{TenantID: tenant, UserID: user})
	if err != nil {
		return nil, nil // no exceptions (or a read failure): falls back to the role, which is the safe behavior
	}
	for _, r := range rows {
		if r.Allow {
			allow = append(allow, rbac.Permission(r.Permission))
		} else {
			deny = append(deny, rbac.Permission(r.Permission))
		}
	}
	return allow, deny
}

// effectivePerms returns a user's effective permissions (role ± exceptions).
func (s *Server) effectivePerms(ctx context.Context, tenant, user uuid.UUID, role string) []rbac.Permission {
	allow, deny := s.memberOverrides(ctx, tenant, user)
	return rbac.Effective(rbac.Role(role), allow, deny)
}

// handleGetMemberPermissions returns what comes from the role and what was adjusted by hand, so the
// UI can show the difference instead of a list of checkboxes with no context.
func (s *Server) handleGetMemberPermissions(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "rbac.invalid_id")
		return
	}
	target, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "rbac.not_found")
		return
	}
	allow, deny := s.memberOverrides(r.Context(), mu.TenantID, target.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"role":      target.Role,
		"all":       rbac.AllPermissions(),
		"from_role": rbac.Permissions(rbac.Role(target.Role)),
		"effective": rbac.Effective(rbac.Role(target.Role), allow, deny),
		"allow":     orEmpty(allow),
		"deny":      orEmpty(deny),
		// What I can touch: you can't grant what you don't have.
		"editable": s.effectivePerms(r.Context(), mu.TenantID, mu.ID, mu.Role),
	})
}

// handleSetMemberPermissions saves the exceptions (replaces any existing ones).
func (s *Server) handleSetMemberPermissions(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "rbac.invalid_id")
		return
	}
	target, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "rbac.not_found")
		return
	}
	// Same hierarchy as changing a role: you don't adjust someone at the same level or above.
	if !rbac.CanManage(rbac.Role(mu.Role), rbac.Role(target.Role)) {
		writeErr(w, http.StatusForbidden, "rbac.forbidden")
		return
	}
	var req struct {
		Allow []string `json:"allow"`
		Deny  []string `json:"deny"`
	}
	if !decode(w, r, &req) {
		return
	}
	mine := map[rbac.Permission]bool{}
	for _, p := range s.effectivePerms(r.Context(), mu.TenantID, mu.ID, mu.Role) {
		mine[p] = true
	}
	known := map[rbac.Permission]bool{}
	for _, p := range rbac.AllPermissions() {
		known[p] = true
	}
	type entry struct {
		perm  rbac.Permission
		allow bool
	}
	var entries []entry
	for _, list := range []struct {
		vals  []string
		allow bool
	}{{req.Allow, true}, {req.Deny, false}} {
		for _, raw := range list.vals {
			p := rbac.Permission(raw)
			if !known[p] {
				writeErr(w, http.StatusBadRequest, "rbac.unknown_permission")
				return
			}
			// Applies to both granting AND denying: whoever doesn't have the permission doesn't govern it either.
			if !mine[p] {
				writeErr(w, http.StatusForbidden, "rbac.cannot_grant")
				return
			}
			entries = append(entries, entry{p, list.allow})
		}
	}
	if err := s.q.DeleteMemberPermissions(r.Context(), db.DeleteMemberPermissionsParams{TenantID: mu.TenantID, UserID: target.ID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "rbac.failed")
		return
	}
	for _, e := range entries {
		if err := s.q.InsertMemberPermission(r.Context(), db.InsertMemberPermissionParams{
			TenantID: mu.TenantID, UserID: target.ID, Permission: string(e.perm), Allow: e.allow,
			CreatedBy: pgUUID(mu.ID),
		}); err != nil {
			writeErr(w, http.StatusInternalServerError, "rbac.failed")
			return
		}
	}
	allow, deny := s.memberOverrides(r.Context(), mu.TenantID, target.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"effective": rbac.Effective(rbac.Role(target.Role), allow, deny),
		"allow":     orEmpty(allow), "deny": orEmpty(deny),
	})
}
