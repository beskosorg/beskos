package httpapi

// The pure decisions of the mailbox funnel: what a parked account is called, what a restore has to
// put back, and what the panel is told about the clock. The parts that talk to Postgres and to
// Stalwart are exercised in production against a disposable domain; these are the ones that can be
// got wrong quietly.

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

func TestParkedNameCannotCollideWithARealAddress(t *testing.T) {
	m := db.MailUser{ID: uuid.MustParse("7bff5596-e784-49ed-9b87-39353ab455e6")}
	got := parkedLocal(m)
	if got != "deleted-7bff5596" {
		t.Fatalf("parkedLocal = %q, want deleted-7bff5596", got)
	}
	// The point of the name is that nobody could ever have been given it: a real local part is not
	// allowed to look like this, so parking cannot land on somebody else's address.
	if !strings.HasPrefix(got, "deleted-") {
		t.Error("a parked account must be recognisable as one")
	}
}

func TestParkedNamesDifferPerMailbox(t *testing.T) {
	// Two people deleted from the same domain must not be parked under the same name -- the second
	// rename would collide and the second mailbox would keep its address, still collecting mail.
	a := parkedLocal(db.MailUser{ID: uuid.MustParse("7bff5596-e784-49ed-9b87-39353ab455e6")})
	b := parkedLocal(db.MailUser{ID: uuid.MustParse("39f38bef-7bfb-4957-a5bf-792bc47c47c1")})
	if a == b {
		t.Fatalf("two mailboxes parked under the same name: %q", a)
	}
}

func TestLocalPartIsWhatRestoreGivesBack(t *testing.T) {
	for _, c := range []struct{ email, want string }{
		{"ana@acme.com", "ana"},
		{"ana.silva+vendas@acme.com", "ana.silva+vendas"},
		{"postmaster@sub.acme.com", "postmaster"},
	} {
		if got := localPart(c.email); got != c.want {
			t.Errorf("localPart(%q) = %q, want %q", c.email, got, c.want)
		}
	}
}

func TestViewShowsTheClockOnlyWhenDeleted(t *testing.T) {
	live := mailboxLifecycleView(db.MailUser{Email: "ana@acme.com", Status: "suspended"})
	if _, ok := live["purge_after"]; ok {
		t.Error("a mailbox that was never deleted must not show a purge date")
	}

	when := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	gone := mailboxLifecycleView(db.MailUser{
		Email: "ana@acme.com", Status: "deleted",
		DeletedAt: pgtype.Timestamptz{Time: when, Valid: true},
	})
	got, ok := gone["purge_after"].(time.Time)
	if !ok {
		t.Fatalf("a deleted mailbox must say when it stops being recoverable, got %v", gone["purge_after"])
	}
	if want := when.Add(RecoveryWindow); !got.Equal(want) {
		t.Errorf("purge_after = %s, want %s", got, want)
	}
}

// The window is written in two places that must agree: here, and the retention notes we publish to
// customers. A change made in one and not the other is a promise broken on paper only.
func TestRecoveryWindowIsThirtyDays(t *testing.T) {
	if RecoveryWindow != 30*24*time.Hour {
		t.Fatalf("RecoveryWindow = %s; docs/data-retention.md says 30 days", RecoveryWindow)
	}
}

// Removing a domain has two different obstacles and they need opposite advice. Exercised here
// rather than in production because reaching the second state means deleting the mailbox of the
// admin doing the deleting, which the product refuses -- correctly.
func TestDomainRemovalSaysWhichObstacleItHit(t *testing.T) {
	for _, c := range []struct {
		name        string
		total, gone int64
		want        string
	}{
		{"empty domain", 0, 0, ""},
		{"someone still uses it", 3, 0, "domains.has_mailboxes"},
		{"some live, some deleted", 3, 2, "domains.has_mailboxes"},
		{"only deleted, still recoverable", 2, 2, "domains.mailboxes_in_recovery"},
	} {
		got := domainRemovalBlock(db.DomainMailboxStateRow{Total: c.total, Deleted: c.gone})
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
