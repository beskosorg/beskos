package provisioning

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/auth"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/rbac"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

var (
	OwnerDomainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	OwnerLoginRe  = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`)
)

// OwnerInput describes the organization and the owner mailbox to create for it.
type OwnerInput struct {
	OrgName  string
	Domain   string
	Login    string
	Name     string
	Password string
}

// OwnerResult is what CreateOwner actually created.
type OwnerResult struct {
	Tenant db.Tenant
	Owner  db.MailUser
	Email  string
}

// OwnerStore is the subset of db.Queries CreateOwner needs.
type OwnerStore interface {
	CreateTenant(ctx context.Context, arg db.CreateTenantParams) (db.Tenant, error)
	DeleteTenant(ctx context.Context, id uuid.UUID) error
	GetDomainByName(ctx context.Context, domain string) (db.Domain, error)
	CreateDomain(ctx context.Context, arg db.CreateDomainParams) (db.Domain, error)
	UpdateDomainProvision(ctx context.Context, arg db.UpdateDomainProvisionParams) (db.Domain, error)
	GetMailUserByEmail(ctx context.Context, email string) (db.MailUser, error)
	CreateMailUser(ctx context.Context, arg db.CreateMailUserParams) (db.MailUser, error)
}

// ValidateOwnerInput checks the organization/domain/login/password shape shared by every path
// that can create the first account of an organization (hosted signup, the self-hosted bootstrap
// CLI). It does not check availability (domain/email already taken) -- that needs the database.
func ValidateOwnerInput(in OwnerInput) error {
	switch {
	case strings.TrimSpace(in.OrgName) == "":
		return errors.New("organization name is required")
	case !OwnerDomainRe.MatchString(in.Domain):
		return fmt.Errorf("invalid domain: %q", in.Domain)
	case !OwnerLoginRe.MatchString(in.Login):
		return fmt.Errorf("invalid mailbox login: %q", in.Login)
	case in.Password != "" && len(in.Password) < 10:
		return errors.New("password must be at least 10 characters")
	}
	return nil
}

// CreateOwner creates the organization, its first domain, and the owner's mailbox in a single
// unit: on any failure past tenant creation, the tenant (and everything cascading from it) is
// rolled back, so a half-created organization never locks a domain or an email address for good.
//
// planID selects the tenant's billing plan (the self-hosted build's catalog has exactly one).
// autoDKIM matches the Mailserver.CreateDomain parameter: true lets Stalwart sign its own DKIM,
// false expects the caller to bring its own (e.g. a hosted SES relay in white-label mode).
// Callers still own anything outside this core: issuing a session, setting the JMAP proxy
// credential (login already does this on first sign-in), billing profiles, welcome e-mails.
func CreateOwner(ctx context.Context, store OwnerStore, sw Mailserver, planID string, autoDKIM bool, in OwnerInput) (OwnerResult, error) {
	if err := ValidateOwnerInput(in); err != nil {
		return OwnerResult{}, err
	}
	email := in.Login + "@" + in.Domain
	if _, err := store.GetDomainByName(ctx, in.Domain); err == nil {
		return OwnerResult{}, fmt.Errorf("domain %q is already registered", in.Domain)
	}
	if _, err := store.GetMailUserByEmail(ctx, email); err == nil {
		return OwnerResult{}, fmt.Errorf("email %q is already registered", email)
	}

	tenant, err := store.CreateTenant(ctx, db.CreateTenantParams{Name: in.OrgName, Plan: planID})
	if err != nil {
		return OwnerResult{}, fmt.Errorf("create tenant: %w", err)
	}
	rollback := func(step string, cause error) error {
		log.Printf("provisioning: create owner: %s: %v (rolling back tenant %s)", step, cause, tenant.ID)
		if delErr := store.DeleteTenant(context.WithoutCancel(ctx), tenant.ID); delErr != nil {
			log.Printf("provisioning: create owner: rollback tenant %s failed: %v", tenant.ID, delErr)
		}
		return fmt.Errorf("%s: %w", step, cause)
	}

	dom, err := store.CreateDomain(ctx, db.CreateDomainParams{TenantID: tenant.ID, Domain: in.Domain})
	if err != nil {
		return OwnerResult{}, rollback("create domain", err)
	}
	swDomainID, err := sw.CreateDomain(ctx, in.Domain, "tenant_id="+tenant.ID.String(), autoDKIM)
	if err != nil {
		return OwnerResult{}, rollback("create domain in Stalwart", err)
	}
	if _, err := store.UpdateDomainProvision(ctx, db.UpdateDomainProvisionParams{
		ID: dom.ID, ProvisionState: StateDomainCreated, StalwartID: &swDomainID,
		DkimStatus: dom.DkimStatus, DnsStatus: dom.DnsStatus, DnsZone: dom.DnsZone,
	}); err != nil {
		return OwnerResult{}, rollback("save domain state", err)
	}

	acctID, err := sw.CreateAccount(ctx, stalwart.AccountInput{
		Login: in.Login, DomainID: swDomainID, DisplayName: in.Name, Password: in.Password, QuotaBytes: 5 << 30,
	})
	if err != nil {
		return OwnerResult{}, rollback("create mailbox in Stalwart", err)
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return OwnerResult{}, rollback("hash password", err)
	}
	owner, err := store.CreateMailUser(ctx, db.CreateMailUserParams{
		TenantID: tenant.ID, Email: email, Name: in.Name, Role: string(rbac.RoleOwner),
		StalwartAccountID: &acctID, QuotaBytes: 5 << 30, PasswordHash: hash,
	})
	if err != nil {
		return OwnerResult{}, rollback("create user", err)
	}
	return OwnerResult{Tenant: tenant, Owner: owner, Email: email}, nil
}
