package httpapi

// What happens to mail addressed to somebody who is gone.
//
// Suspending stops a person reading their mailbox; it does NOT stop mail arriving in it. For a
// deleted mailbox that is the wrong outcome twice over: messages pile up for a person nobody will
// ever hand them to, and the organisation believes the address is dead while it quietly is not.
//
// So deletion also takes the ADDRESS away from the account -- by renaming it -- and then either
// leaves the address unclaimed, so senders are told the recipient does not exist, or gives it to a
// named successor as an alias. Nothing is destroyed either way: the account keeps every message it
// had, under a name nobody writes to, until the purge.
//
// FORWARDING IS OPT-IN AND NAMED. It never happens by default, because redirecting a person's mail
// to a colleague without saying so is the thing European regulators have repeatedly penalised. The
// admin has to name the successor, and the successor has to be a live mailbox of the same tenant.

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// parkedLocal is the name a deleted account is parked under. It cannot collide with a real mailbox
// (nobody is allowed an address with a UUID fragment in it) and it says what it is at a glance to
// whoever is reading the account list in Stalwart at 2am.
func parkedLocal(m db.MailUser) string {
	return "deleted-" + strings.SplitN(m.ID.String(), "-", 2)[0]
}

// localPart is the name the mailbox answers to today, which is what restoring has to put back.
func localPart(email string) string { return strings.SplitN(email, "@", 2)[0] }

// releaseAddress takes the address away from a deleted mailbox and, if a successor was named, hands
// it to them. Called after the account has already been suspended.
func (s *Server) releaseAddress(ctx context.Context, tenant uuid.UUID, m db.MailUser, successor string) error {
	if s.sw == nil || m.StalwartAccountID == nil {
		return nil // no mail server wired up (dev), or a mailbox not yet provisioned
	}
	if err := s.sw.RenameAccount(ctx, *m.StalwartAccountID, parkedLocal(m)); err != nil {
		return fmt.Errorf("park the address: %w", err)
	}
	if successor == "" {
		return nil // unclaimed: senders get "no such user", which is the honest answer
	}
	succ, err := s.q.GetMailUserByEmailInTenant(ctx, db.GetMailUserByEmailInTenantParams{
		Email: successor, TenantID: tenant,
	})
	if err != nil {
		return fmt.Errorf("find the successor: %w", err)
	}
	dom, err := s.q.GetDomainByName(ctx, domainOf(m.Email))
	if err != nil {
		return fmt.Errorf("find the domain: %w", err)
	}
	if _, err := s.q.CreateEmailAlias(ctx, db.CreateEmailAliasParams{
		TenantID: tenant, DomainID: dom.ID, MailUserID: succ.ID, Address: m.Email,
	}); err != nil {
		return fmt.Errorf("give the address to the successor: %w", err)
	}
	return s.syncAliasesToStalwart(ctx, tenant, succ)
}

// reclaimAddress undoes releaseAddress: the successor stops receiving the address and the mailbox
// answers to its own name again.
func (s *Server) reclaimAddress(ctx context.Context, tenant uuid.UUID, m db.MailUser) error {
	if s.sw == nil || m.StalwartAccountID == nil {
		return nil
	}
	// Take the alias away FIRST. Renaming the mailbox back while the successor still holds the
	// address would leave two claims on one address, and which one wins is not ours to decide.
	if alias, err := s.q.GetEmailAliasByAddress(ctx, db.GetEmailAliasByAddressParams{
		TenantID: tenant, Address: m.Email,
	}); err == nil {
		if err := s.q.DeleteEmailAlias(ctx, db.DeleteEmailAliasParams{ID: alias.ID, TenantID: tenant}); err != nil {
			return fmt.Errorf("take the address back from the successor: %w", err)
		}
		if owner, err := s.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{
			ID: alias.MailUserID, TenantID: tenant,
		}); err == nil {
			if err := s.syncAliasesToStalwart(ctx, tenant, owner); err != nil {
				return fmt.Errorf("resync the successor's aliases: %w", err)
			}
		}
	}
	if err := s.sw.RenameAccount(ctx, *m.StalwartAccountID, localPart(m.Email)); err != nil {
		return fmt.Errorf("restore the address: %w", err)
	}
	return nil
}
