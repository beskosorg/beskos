package provisioning

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

type fakeOwnerStore struct {
	tenants        map[string]db.Tenant
	domains        map[string]db.Domain
	mailUsers      map[string]db.MailUser
	deletedTenants []uuid.UUID
	failCreateUser bool
}

func newFakeOwnerStore() *fakeOwnerStore {
	return &fakeOwnerStore{tenants: map[string]db.Tenant{}, domains: map[string]db.Domain{}, mailUsers: map[string]db.MailUser{}}
}

func (s *fakeOwnerStore) CreateTenant(_ context.Context, arg db.CreateTenantParams) (db.Tenant, error) {
	t := db.Tenant{ID: uuid.New(), Name: arg.Name, Plan: arg.Plan}
	s.tenants[t.ID.String()] = t
	return t, nil
}

func (s *fakeOwnerStore) DeleteTenant(_ context.Context, id uuid.UUID) error {
	s.deletedTenants = append(s.deletedTenants, id)
	delete(s.tenants, id.String())
	return nil
}

func (s *fakeOwnerStore) GetDomainByName(_ context.Context, domain string) (db.Domain, error) {
	if d, ok := s.domains[domain]; ok {
		return d, nil
	}
	return db.Domain{}, errors.New("not found")
}

func (s *fakeOwnerStore) CreateDomain(_ context.Context, arg db.CreateDomainParams) (db.Domain, error) {
	d := db.Domain{ID: uuid.New(), TenantID: arg.TenantID, Domain: arg.Domain}
	s.domains[arg.Domain] = d
	return d, nil
}

func (s *fakeOwnerStore) UpdateDomainProvision(_ context.Context, arg db.UpdateDomainProvisionParams) (db.Domain, error) {
	for k, d := range s.domains {
		if d.ID == arg.ID {
			d.ProvisionState = arg.ProvisionState
			d.StalwartID = arg.StalwartID
			s.domains[k] = d
			return d, nil
		}
	}
	return db.Domain{}, errors.New("domain not found")
}

func (s *fakeOwnerStore) GetMailUserByEmail(_ context.Context, email string) (db.MailUser, error) {
	if u, ok := s.mailUsers[email]; ok {
		return u, nil
	}
	return db.MailUser{}, errors.New("not found")
}

func (s *fakeOwnerStore) CreateMailUser(_ context.Context, arg db.CreateMailUserParams) (db.MailUser, error) {
	if s.failCreateUser {
		return db.MailUser{}, errors.New("boom")
	}
	u := db.MailUser{ID: uuid.New(), TenantID: arg.TenantID, Email: arg.Email, Role: arg.Role}
	s.mailUsers[arg.Email] = u
	return u, nil
}

func validOwnerInput() OwnerInput {
	return OwnerInput{OrgName: "Acme", Domain: "acme.example", Login: "admin", Name: "Jane Doe", Password: "correct-horse-battery"}
}

func TestValidateOwnerInput(t *testing.T) {
	if err := ValidateOwnerInput(validOwnerInput()); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	cases := []OwnerInput{
		{Domain: "acme.example", Login: "admin", Password: "correct-horse-battery"},         // no org name
		{OrgName: "Acme", Domain: "not a domain", Login: "admin", Password: "12345678901"},  // bad domain
		{OrgName: "Acme", Domain: "acme.example", Login: "-admin", Password: "12345678901"}, // bad login
		{OrgName: "Acme", Domain: "acme.example", Login: "admin", Password: "short"},        // weak password
	}
	for i, in := range cases {
		if err := ValidateOwnerInput(in); err == nil {
			t.Errorf("case %d: expected an error, got none", i)
		}
	}
}

func TestCreateOwnerHappyPath(t *testing.T) {
	store := newFakeOwnerStore()
	sw := &fakeMailserver{}
	res, err := CreateOwner(context.Background(), store, sw, "self_hosted", true, validOwnerInput())
	if err != nil {
		t.Fatalf("CreateOwner: %v", err)
	}
	if res.Email != "admin@acme.example" {
		t.Errorf("email = %q, want admin@acme.example", res.Email)
	}
	if res.Owner.Role != "owner" {
		t.Errorf("role = %q, want owner", res.Owner.Role)
	}
	if sw.createDomainCalls != 1 || sw.createAcctCalls != 1 {
		t.Errorf("expected exactly one domain and one account created in Stalwart, got %d/%d", sw.createDomainCalls, sw.createAcctCalls)
	}
	if !sw.autoDKIM {
		t.Error("autoDKIM=true was not passed through to the Stalwart client")
	}
	if len(store.deletedTenants) != 0 {
		t.Error("a successful creation must not roll back the tenant")
	}
}

func TestCreateOwnerRejectsTakenDomain(t *testing.T) {
	store := newFakeOwnerStore()
	store.domains["acme.example"] = db.Domain{Domain: "acme.example"}
	if _, err := CreateOwner(context.Background(), store, &fakeMailserver{}, "self_hosted", true, validOwnerInput()); err == nil {
		t.Fatal("expected an error for an already-registered domain")
	}
	if len(store.tenants) != 0 {
		t.Error("no tenant should have been created before the domain check failed")
	}
}

func TestCreateOwnerRollsBackOnFailure(t *testing.T) {
	store := newFakeOwnerStore()
	store.failCreateUser = true
	if _, err := CreateOwner(context.Background(), store, &fakeMailserver{}, "self_hosted", true, validOwnerInput()); err == nil {
		t.Fatal("expected an error when creating the mail user fails")
	}
	if len(store.deletedTenants) != 1 {
		t.Fatalf("expected the tenant to be rolled back, deletedTenants = %v", store.deletedTenants)
	}
}
