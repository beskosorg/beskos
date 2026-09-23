// Package rbac defines the panel's access control: FIXED roles mapped to granular
// permissions, checked on the backend (law §5.2). Per-tenant customizable roles are for later.
//
// Security: the source of truth is the authenticated user's saved role (never from the request).
// The check is an explicit allow-list — when in doubt, deny.
package rbac

// Permission is a granular capability. Handlers require a specific permission.
type Permission string

const (
	PermDomainsView   Permission = "domains.view"
	PermDomainsManage Permission = "domains.manage" // add/edit domain
	PermDomainsDelete Permission = "domains.delete" // remove domain (destructive, separate)
	PermMailboxManage Permission = "mailboxes.manage"
	// Destroying a mailbox is separate from running one, the same way removing a domain is: the
	// person who creates accounts and resets passwords every day has no need to destroy anybody's
	// correspondence, and should not be one misclick away from it.
	PermMailboxDelete Permission = "mailboxes.delete"
	PermMembersView   Permission = "members.view"
	PermMembersInvite Permission = "members.invite" // invite/create account
	PermMembersManage Permission = "members.manage" // change role, remove, permissions
	PermGroupsManage  Permission = "groups.manage"  // user groups
	PermBillingView   Permission = "billing.view"   // view plan and invoices
	PermBillingManage Permission = "billing.manage" // change plan, portal, cancel
	PermUsageView     Permission = "usage.view"
	PermTenantDelete  Permission = "tenant.delete"
	// org.settings used to be the catch-all bucket for anything "organization-level" — branding,
	// apps, security policy, announcements, assistant, and the wiki home. Split apart so one of
	// those can be entrusted to someone without handing over the rest, per the request for
	// finer-grained permissions.
	PermOrgBranding     Permission = "org.branding"      // brand/appearance and visible apps
	PermOrgSecurity     Permission = "org.security"      // security policy (MFA, sessions)
	PermAnnouncements   Permission = "org.announcements" // internal announcements
	PermAssistantManage Permission = "assistant.manage"  // assistant agents and governance
	PermWikiManage      Permission = "wiki.manage"       // wiki home (organization content)
)

// Role is a fixed role. The hierarchy is by permission set, not implicit inheritance.
type Role string

const (
	RoleOwner         Role = "owner"          // organization owner — everything
	RoleAdmin         Role = "admin"          // administers the organization — all permissions
	RoleMember        Role = "member"         // read-only
	RoleBilling       Role = "billing"        // billing + usage
	RoleInternalAdmin Role = "internal_admin" // our team (support) — everything
)

// rolePermissions is the authoritative matrix. Adding a role/permission means editing here.
var rolePermissions = map[Role]map[Permission]bool{
	RoleOwner:         allPerms(),
	RoleInternalAdmin: allPerms(),
	// Admin has EVERYTHING, by product decision: an organization admin should not run into
	// permission walls. The hierarchy still matters on another axis — rank stops an admin from
	// touching an owner (CanManage), and a permission can still be revoked from one specific
	// person via per-member exceptions. What went away was the ceiling by role.
	RoleAdmin: allPerms(),
	RoleMember: set(
		PermDomainsView, PermUsageView,
	),
	RoleBilling: set(
		PermBillingView, PermBillingManage, PermUsageView, PermMembersView,
	),
}

// AllPermissions lists all known permissions (used by owner/internal and in tests).
func AllPermissions() []Permission {
	return []Permission{
		PermDomainsView, PermDomainsManage, PermDomainsDelete, PermMailboxManage, PermMailboxDelete,
		PermMembersView, PermMembersInvite, PermMembersManage, PermGroupsManage,
		PermBillingView, PermBillingManage, PermUsageView, PermTenantDelete,
		PermOrgBranding, PermOrgSecurity, PermAnnouncements, PermAssistantManage, PermWikiManage,
	}
}

// AssignableRoles are the roles an admin can assign in an invite/edit.
// internal_admin is NOT assignable by customers (only provisioned internally).
func AssignableRoles() []Role { return []Role{RoleOwner, RoleAdmin, RoleMember, RoleBilling} }

func IsAssignableRole(r Role) bool {
	for _, x := range AssignableRoles() {
		if x == r {
			return true
		}
	}
	return false
}

// roleRank defines the hierarchy for administrative operations BETWEEN users (password reset,
// removal, role change). Unknown role = 0. The permission check (Has) says whether the actor
// CAN manage members; the rank says WHO it can act on — never someone of rank >= its own.
var roleRank = map[Role]int{
	RoleInternalAdmin: 100,
	RoleOwner:         90,
	RoleAdmin:         50,
	RoleBilling:       20,
	RoleMember:        10,
}

// Rank returns the role's hierarchical level (0 if unknown).
func Rank(role Role) int { return roleRank[role] }

// CanManage answers whether the actor can perform an administrative action on the target.
// Fail-closed rule: the actor's rank must be STRICTLY greater than the target's. This way an
// admin does not reset an owner's password, nor another admin's (law §5.2).
func CanManage(actor, target Role) bool {
	return roleRank[actor] > roleRank[target]
}

// Has answers whether the role has the permission. Unknown role -> deny (fail-closed).
func Has(role Role, perm Permission) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	return perms[perm]
}

// Permissions returns the role's permissions (for /me, for the UI to gate buttons).
func Permissions(role Role) []Permission {
	var out []Permission
	for _, p := range AllPermissions() {
		if Has(role, p) {
			out = append(out, p)
		}
	}
	return out
}

// Effective returns someone's permissions: the role's, plus those granted to the person, minus
// those revoked. This is the authoritative answer — handlers and /me go through here, never Has
// directly. Revoking always wins over granting: in a contradiction, the most restrictive decision
// stands.
func Effective(role Role, allow, deny []Permission) []Permission {
	granted := map[Permission]bool{}
	for _, p := range AllPermissions() {
		granted[p] = Has(role, p)
	}
	for _, p := range allow {
		if _, known := granted[p]; known {
			granted[p] = true
		}
	}
	for _, p := range deny {
		granted[p] = false
	}
	var out []Permission
	for _, p := range AllPermissions() {
		if granted[p] {
			out = append(out, p)
		}
	}
	return out
}

// HasEffective is Effective for a single permission (the handlers' hot path).
func HasEffective(role Role, allow, deny []Permission, perm Permission) bool {
	for _, p := range deny {
		if p == perm {
			return false
		}
	}
	for _, p := range allow {
		if p == perm {
			return true
		}
	}
	return Has(role, perm)
}

func set(perms ...Permission) map[Permission]bool {
	m := make(map[Permission]bool, len(perms))
	for _, p := range perms {
		m[p] = true
	}
	return m
}

func allPerms() map[Permission]bool {
	m := map[Permission]bool{}
	for _, p := range AllPermissions() {
		m[p] = true
	}
	return m
}
