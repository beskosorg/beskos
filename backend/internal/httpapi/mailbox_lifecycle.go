package httpapi

// The life and death of a mailbox: suspend, delete with a way back, restore.
//
// A mailbox is not a row. It is somebody's correspondence, most of it written by people who never
// agreed to anything with us, and some of it the only record a company has of what it promised a
// customer. So there is no operation here that destroys anything: the worker does that, later, out
// of sight, and only after the window in which somebody can change their mind.
//
// See migration 00134 for the funnel and why it exists, and DIVERGENCES for how suspension works
// against a Stalwart that has no notion of a disabled account.

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// RecoveryWindow is how long a deleted mailbox can be brought back, and therefore how long its
// address stays reserved.
//
// Thirty days, where Google uses twenty. The extra ten are not generosity: the people who discover
// that a mailbox mattered are the ones who were away when it was deleted, and a month covers a
// holiday. It is also the number written in the retention notes we publish, so changing it here
// means changing it there -- see docs/data-retention.md.
const RecoveryWindow = 30 * 24 * time.Hour

type mailboxStatusReq struct {
	Status string `json:"status"` // active | suspended
}

// handleSetMailboxStatus suspends a mailbox or brings it back.
//
// Suspending removes the account's credentials in Stalwart, so IMAP, SMTP, JMAP and our own panel
// all stop accepting the password at once -- while mail carries on being delivered and nothing is
// lost. Reactivating needs a new password, because the old one no longer exists; that is Stalwart's
// shape, and it is the right default anyway for an account that was suspended.
func (s *Server) handleSetMailboxStatus(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	target, ok := s.ownedMailbox(w, r)
	if !ok {
		return
	}
	var req mailboxStatusReq
	if !decode(w, r, &req) {
		return
	}
	switch req.Status {
	case "active", "suspended":
	default:
		writeErr(w, http.StatusBadRequest, "mailbox.invalid_status")
		return
	}
	// Deleting is its own route, with its own permission and its own consequences. Letting this one
	// walk a mailbox out of 'deleted' would skip the restore path and leave the Stalwart account
	// still without credentials -- reachable in our database, and unusable in reality.
	if target.Status == "deleted" {
		writeErr(w, http.StatusConflict, "mailbox.is_deleted")
		return
	}
	if target.ID == u.ID && req.Status == "suspended" {
		writeErr(w, http.StatusBadRequest, "mailbox.cannot_suspend_self")
		return
	}

	if req.Status == "suspended" {
		if err := s.sw.SuspendAccount(r.Context(), strval(target.StalwartAccountID)); err != nil {
			log.Printf("mailbox suspend %s: %v", target.Email, err)
			writeErr(w, http.StatusBadGateway, "mailbox.suspend_failed")
			return
		}
	}
	// Reactivating is deliberately NOT done here. There is no password to restore, so the way back
	// is the existing "reset password" route: it writes a new secret, which is exactly what recreates
	// the credential. Doing it silently here would hand an account back with a password nobody knows.
	updated, err := s.q.SetMailUserStatus(r.Context(), db.SetMailUserStatusParams{
		ID: target.ID, TenantID: u.TenantID, Status: req.Status,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "mailbox.status_failed")
		return
	}
	writeJSON(w, http.StatusOK, mailboxLifecycleView(updated))
}

type deleteMailboxReq struct {
	// Where mail addressed to this person should go from now on. Empty means it is rejected at the
	// door, which is the default on purpose: quietly redirecting somebody's mail to their former
	// manager is precisely what European regulators have penalised employers for.
	Successor string `json:"successor_email"`
	// The admin states they understand this ends in destruction. Not a dialog box moved to the API:
	// it is the record that somebody was told, which is the part that matters afterwards.
	Confirm bool `json:"confirm"`
}

// handleDeleteMailbox starts the deletion. Nothing is destroyed here.
//
// The account is suspended immediately (nobody can read it any more), the row stays so the address
// cannot be handed to somebody else, and the purge worker finishes the job when the recovery window
// closes. Between the two, an admin can restore it, and anybody entitled to the data can still be
// given an export -- which is the "delete OR RETURN" that Article 28(3)(g) requires us to offer.
func (s *Server) handleDeleteMailbox(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	target, ok := s.ownedMailbox(w, r)
	if !ok {
		return
	}
	var req deleteMailboxReq
	if !decode(w, r, &req) {
		return
	}
	if !req.Confirm {
		writeErr(w, http.StatusBadRequest, "mailbox.confirm_required")
		return
	}
	if target.ID == u.ID {
		writeErr(w, http.StatusBadRequest, "mailbox.cannot_delete_self")
		return
	}
	if target.Status == "deleted" {
		writeErr(w, http.StatusConflict, "mailbox.already_deleted")
		return
	}

	successor := strings.ToLower(strings.TrimSpace(req.Successor))
	if successor != "" {
		// The successor has to be a live mailbox of this same tenant. Anything else and we would be
		// forwarding a person's mail out of the organisation on the strength of a typed string.
		succ, err := s.q.GetMailUserByEmailInTenant(r.Context(), db.GetMailUserByEmailInTenantParams{
			Email: successor, TenantID: u.TenantID,
		})
		if err != nil || succ.Status != "active" {
			writeErr(w, http.StatusBadRequest, "mailbox.successor_invalid")
			return
		}
	}

	// Suspend first. If the purge never happens, or the row is restored, the worst case is a mailbox
	// nobody can log into -- not one an ex-employee can still read after being told they cannot.
	if err := s.sw.SuspendAccount(r.Context(), strval(target.StalwartAccountID)); err != nil {
		log.Printf("mailbox delete %s: suspend failed: %v", target.Email, err)
		writeErr(w, http.StatusBadGateway, "mailbox.suspend_failed")
		return
	}
	// Take the address away from the account: parked under a name nobody writes to, and handed to
	// the successor if one was named. Until this happens the mailbox is unreadable but still
	// COLLECTING -- which is the outcome nobody wants and nobody would notice.
	if err := s.releaseAddress(r.Context(), u.TenantID, target, successor); err != nil {
		log.Printf("mailbox delete %s: %v", target.Email, err)
		writeErr(w, http.StatusBadGateway, "mailbox.release_failed")
		return
	}
	updated, err := s.q.SoftDeleteMailUser(r.Context(), db.SoftDeleteMailUserParams{
		ID: target.ID, TenantID: u.TenantID, SuccessorEmail: successor,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "mailbox.delete_failed")
		return
	}
	log.Printf("mailbox %s scheduled for deletion by %s (successor=%q, purge after %s)",
		target.Email, u.Email, successor, RecoveryWindow)
	writeJSON(w, http.StatusOK, mailboxLifecycleView(updated))
}

// handleRestoreMailbox brings a deleted mailbox back before the window closes.
//
// It comes back SUSPENDED, not active: the credentials are gone and only a password reset recreates
// them. Marking it active here would show a mailbox that looks usable to everyone in the panel and
// refuses every login.
func (s *Server) handleRestoreMailbox(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	target, ok := s.ownedMailbox(w, r)
	if !ok {
		return
	}
	if target.Status != "deleted" {
		writeErr(w, http.StatusConflict, "mailbox.not_deleted")
		return
	}
	// Give the address back before anything else: while the successor still holds it, the mailbox
	// we are restoring would answer to a name and the mail would go somewhere else.
	if err := s.reclaimAddress(r.Context(), u.TenantID, target); err != nil {
		log.Printf("mailbox restore %s: %v", target.Email, err)
		writeErr(w, http.StatusBadGateway, "mailbox.reclaim_failed")
		return
	}
	restored, err := s.q.SetMailUserStatus(r.Context(), db.SetMailUserStatusParams{
		ID: target.ID, TenantID: u.TenantID, Status: "suspended",
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "mailbox.restore_failed")
		return
	}
	log.Printf("mailbox %s restored by %s (suspended; needs a new password to be usable)", target.Email, u.Email)
	writeJSON(w, http.StatusOK, mailboxLifecycleView(restored))
}

// ownedMailbox resolves the mailbox in the URL and proves it belongs to the caller's tenant before
// anything else happens (law 5.1/5.2).
func (s *Server) ownedMailbox(w http.ResponseWriter, r *http.Request) (db.MailUser, bool) {
	u := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "mailbox.invalid_id")
		return db.MailUser{}, false
	}
	m, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: id, TenantID: u.TenantID})
	if err != nil {
		// 404 rather than 403: a mailbox of another tenant must not be distinguishable from one that
		// does not exist, or the id becomes a way to enumerate other people's staff.
		writeErr(w, http.StatusNotFound, "mailbox.not_found")
		return db.MailUser{}, false
	}
	return m, true
}

// mailboxLifecycleView is what the panel needs to render the state and the clock.
func mailboxLifecycleView(m db.MailUser) map[string]any {
	out := map[string]any{
		"id": m.ID, "email": m.Email, "name": m.Name, "status": m.Status,
		"successor_email": m.SuccessorEmail,
	}
	if m.DeletedAt.Valid {
		out["deleted_at"] = m.DeletedAt.Time
		out["purge_after"] = m.DeletedAt.Time.Add(RecoveryWindow)
	}
	return out
}
