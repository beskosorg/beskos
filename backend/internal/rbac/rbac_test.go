package rbac

import "testing"

// Owner, admin, and internal_admin have everything: whoever administers the organization does not
// run into permission walls. What separates them is RANK, not the set of permissions.
func TestOwnerAdminAndInternalHaveEverything(t *testing.T) {
	for _, role := range []Role{RoleOwner, RoleAdmin, RoleInternalAdmin} {
		for _, p := range AllPermissions() {
			if !Has(role, p) {
				t.Errorf("%s should have %s", role, p)
			}
		}
	}
}

func TestPermissionMatrix(t *testing.T) {
	cases := []struct {
		role Role
		perm Permission
		want bool
	}{
		{RoleAdmin, PermDomainsManage, true},
		{RoleAdmin, PermMembersManage, true},
		{RoleAdmin, PermBillingManage, true}, // admin has everything
		{RoleAdmin, PermTenantDelete, true},
		{RoleMember, PermDomainsView, true},
		{RoleMember, PermDomainsManage, false}, // member is read-only
		{RoleMember, PermMembersManage, false},
		{RoleBilling, PermBillingManage, true},
		{RoleBilling, PermDomainsManage, false},
		{RoleBilling, PermMembersManage, false},
	}
	for _, c := range cases {
		if got := Has(c.role, c.perm); got != c.want {
			t.Errorf("Has(%s,%s)=%v, want %v", c.role, c.perm, got, c.want)
		}
	}
}

func TestUnknownRoleFailsClosed(t *testing.T) {
	if Has(Role("hacker"), PermDomainsView) {
		t.Error("unknown role must not have any permission")
	}
	if len(Permissions(Role(""))) != 0 {
		t.Error("empty role must have no permissions")
	}
}

func TestOnlyAdminsCanDeleteTenant(t *testing.T) {
	for _, r := range []Role{RoleMember, RoleBilling} {
		if Has(r, PermTenantDelete) {
			t.Errorf("%s must not be able to delete a tenant", r)
		}
	}
}

// Admin has all permissions, but is still below owner in the hierarchy: the protection over who
// can act ON whom is the rank, and that hasn't changed.
func TestAdminStillCannotActOnOwner(t *testing.T) {
	if CanManage(RoleAdmin, RoleOwner) {
		t.Error("admin must not be able to manage an owner")
	}
	if !CanManage(RoleAdmin, RoleMember) {
		t.Error("admin must be able to manage a member")
	}
}

func TestInternalAdminNotClientAssignable(t *testing.T) {
	if IsAssignableRole(RoleInternalAdmin) {
		t.Error("internal_admin must not be assignable by customers (privilege escalation)")
	}
	for _, r := range []Role{RoleOwner, RoleAdmin, RoleMember, RoleBilling} {
		if !IsAssignableRole(r) {
			t.Errorf("%s should be assignable", r)
		}
	}
}

// Per-member exceptions are a security decision: what matters to test is the precedence order
// (revoke wins), what stays untouched, and that a made-up permission doesn't sneak in by mistake.
func TestEffectiveOverrides(t *testing.T) {
	has := func(list []Permission, p Permission) bool {
		for _, x := range list {
			if x == p {
				return true
			}
		}
		return false
	}

	// Grant on top of the role: an admin doesn't delete domains, but can be given the permission.
	got := Effective(RoleAdmin, []Permission{PermDomainsDelete}, nil)
	if !has(got, PermDomainsDelete) {
		t.Error("granting should add domains.delete to admin")
	}
	if !has(got, PermMembersManage) {
		t.Error("granting must not remove what the role already gave")
	}

	// Revoke from the role: admin without member management.
	got = Effective(RoleAdmin, nil, []Permission{PermMembersManage})
	if has(got, PermMembersManage) {
		t.Error("revoking should remove members.manage from admin")
	}

	// Contradiction: revoke always wins (fail-closed).
	got = Effective(RoleAdmin, []Permission{PermBillingManage}, []Permission{PermBillingManage})
	if has(got, PermBillingManage) {
		t.Error("with both grant and revoke at once, it must end up revoked")
	}

	// Unknown permission doesn't make it into the effective list.
	got = Effective(RoleMember, []Permission{"inventada.qualquer"}, nil)
	for _, p := range got {
		if p == "inventada.qualquer" {
			t.Error("unknown permission should not be granted")
		}
	}

	// Owner with a revoke exception still loses the permission (nobody is above the rule).
	if HasEffective(RoleOwner, nil, []Permission{PermTenantDelete}, PermTenantDelete) {
		t.Error("revoking must also apply to the owner")
	}
	// And the fast path agrees with the full one.
	if HasEffective(RoleMember, []Permission{PermUsageView}, nil, PermUsageView) != has(Effective(RoleMember, []Permission{PermUsageView}, nil), PermUsageView) {
		t.Error("HasEffective and Effective must give the same answer")
	}
}
