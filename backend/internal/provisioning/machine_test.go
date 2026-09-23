package provisioning

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

// --- Fakes -------------------------------------------------------------------

type fakeMailserver struct {
	createDomainCalls int
	autoDKIM          bool
	createAcctCalls   int
	zone              string
	failCreateAccount bool
}

func (f *fakeMailserver) CreateDomain(_ context.Context, _, _ string, autoDKIM bool) (string, error) {
	f.autoDKIM = autoDKIM
	f.createDomainCalls++
	return "sw-dom-1", nil
}
func (f *fakeMailserver) GetDomainDNSZone(context.Context, string) (string, error) {
	return f.zone, nil
}
func (f *fakeMailserver) CreateAccount(context.Context, stalwart.AccountInput) (string, error) {
	f.createAcctCalls++
	if f.failCreateAccount {
		return "", errors.New("boom")
	}
	return "sw-acct-1", nil
}

type fakeStore struct {
	domain    db.Domain
	mailUsers map[string]db.MailUser
	updates   int
}

func (s *fakeStore) UpdateDomainProvision(_ context.Context, arg db.UpdateDomainProvisionParams) (db.Domain, error) {
	s.updates++
	s.domain.ProvisionState = arg.ProvisionState
	s.domain.StalwartID = arg.StalwartID
	s.domain.DkimStatus = arg.DkimStatus
	s.domain.DnsStatus = arg.DnsStatus
	s.domain.DnsZone = arg.DnsZone
	return s.domain, nil
}
func (s *fakeStore) CreateMailUser(_ context.Context, arg db.CreateMailUserParams) (db.MailUser, error) {
	u := db.MailUser{Email: arg.Email, StalwartAccountID: arg.StalwartAccountID}
	if s.mailUsers == nil {
		s.mailUsers = map[string]db.MailUser{}
	}
	s.mailUsers[arg.Email] = u
	return u, nil
}
func (s *fakeStore) GetMailUserByEmail(_ context.Context, email string) (db.MailUser, error) {
	if u, ok := s.mailUsers[email]; ok {
		return u, nil
	}
	return db.MailUser{}, errors.New("not found")
}

func newDomain(state string) db.Domain {
	return db.Domain{ID: uuid.New(), TenantID: uuid.New(), Domain: "cliente.com.br", ProvisionState: state}
}

// runToActive runs the machine until a terminal state or until it stops progressing.
func runToActive(t *testing.T, m *Machine, store *fakeStore) db.Domain {
	t.Helper()
	for i := 0; i < 20; i++ {
		changed, err := m.AdvanceOnce(context.Background(), store.domain)
		if err != nil {
			t.Fatalf("AdvanceOnce error in state %s: %v", store.domain.ProvisionState, err)
		}
		if !changed {
			break
		}
	}
	return store.domain
}

// --- Tests ------------------------------------------------------------------

func TestHappyPath(t *testing.T) {
	sw := &fakeMailserver{zone: "sel._domainkey.cliente.com.br. IN TXT \"v=DKIM1; k=ed25519; p=abc\""}
	store := &fakeStore{domain: newDomain(StatePending)}
	m := NewMachine(sw, AlwaysVerified{}, store)

	final := runToActive(t, m, store)
	if !sw.autoDKIM {
		t.Fatal("self-hosted domains must use Stalwart DKIM")
	}
	if final.ProvisionState != StateActive {
		t.Fatalf("expected active, got %s", final.ProvisionState)
	}
	if sw.createDomainCalls != 1 {
		t.Errorf("CreateDomain should be called 1x, got %d", sw.createDomainCalls)
	}
	if _, ok := store.mailUsers["postmaster@cliente.com.br"]; !ok {
		t.Errorf("postmaster mailbox was not created")
	}
}

func TestWaitsForDKIM(t *testing.T) {
	// Zone without DKIM: should not go past domain_created.
	sw := &fakeMailserver{zone: "cliente.com.br. IN MX 10 mail.example.org."}
	store := &fakeStore{domain: newDomain(StatePending)}
	m := NewMachine(sw, AlwaysVerified{}, store)

	runToActive(t, m, store)
	if store.domain.ProvisionState != StateDomainCreated {
		t.Fatalf("without DKIM it should be stuck at domain_created, got %s", store.domain.ProvisionState)
	}
}

type notYetDNS struct{ calls int }

func (n *notYetDNS) Verify(context.Context, string, string) (bool, error) {
	n.calls++
	return n.calls >= 3, nil // publishes only on the 3rd check
}

func TestWaitsForDNSThenVerifies(t *testing.T) {
	sw := &fakeMailserver{zone: "sel._domainkey.cliente.com.br. IN TXT \"v=DKIM1; p=x\""}
	store := &fakeStore{domain: newDomain(StateDNSPending)}
	dns := &notYetDNS{}
	m := NewMachine(sw, dns, store)

	// First calls don't advance (DNS not published).
	for i := 0; i < 2; i++ {
		changed, _ := m.AdvanceOnce(context.Background(), store.domain)
		if changed {
			t.Fatalf("should not advance before DNS is published")
		}
	}
	// Now it publishes and completes.
	final := runToActive(t, m, store)
	if final.ProvisionState != StateActive {
		t.Fatalf("expected active after DNS, got %s", final.ProvisionState)
	}
}

func TestIdempotentDomainCreate(t *testing.T) {
	// Resumed from pending with stalwart_id already set: doesn't recreate the domain.
	sw := &fakeMailserver{zone: "sel._domainkey.cliente.com.br. TXT DKIM1"}
	existing := "sw-dom-existing"
	d := newDomain(StatePending)
	d.StalwartID = &existing
	store := &fakeStore{domain: d}
	m := NewMachine(sw, AlwaysVerified{}, store)

	if _, err := m.AdvanceOnce(context.Background(), store.domain); err != nil {
		t.Fatal(err)
	}
	if sw.createDomainCalls != 0 {
		t.Errorf("CreateDomain should not be called with an existing stalwart_id")
	}
}

func TestIdempotentActivate(t *testing.T) {
	// Resumed at dns_verified with the mailbox already existing: doesn't recreate the account in Stalwart.
	sw := &fakeMailserver{}
	store := &fakeStore{
		domain:    newDomain(StateDNSVerified),
		mailUsers: map[string]db.MailUser{"postmaster@cliente.com.br": {Email: "postmaster@cliente.com.br"}},
	}
	m := NewMachine(sw, AlwaysVerified{}, store)

	changed, err := m.AdvanceOnce(context.Background(), store.domain)
	if err != nil || !changed {
		t.Fatalf("should activate, changed=%v err=%v", changed, err)
	}
	if sw.createAcctCalls != 0 {
		t.Errorf("CreateAccount should not be called when the mailbox already exists")
	}
	if store.domain.ProvisionState != StateActive {
		t.Errorf("expected active, got %s", store.domain.ProvisionState)
	}
}

// --- Relay SES ---------------------------------------------------------------
