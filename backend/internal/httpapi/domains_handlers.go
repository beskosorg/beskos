package httpapi

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/provisioning"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

type createDomainReq struct {
	Domain string `json:"domain"`
}

// handleCreateDomain registers a domain in the 'pending' state and responds immediately.
// The worker (state machine §7) advances the provisioning asynchronously.
func (s *Server) handleCreateDomain(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	var req createDomainReq
	if !decode(w, r, &req) {
		return
	}
	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
	if req.Domain == "" {
		writeErr(w, http.StatusBadRequest, "domains.name_required")
		return
	}
	d, err := s.q.CreateDomain(r.Context(), db.CreateDomainParams{
		TenantID: u.TenantID, Domain: req.Domain,
	})
	if err != nil {
		writeErr(w, http.StatusConflict, "domains.already_exists")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: u.TenantID, Valid: true}, Actor: u.Email, Action: "domain.create", Target: req.Domain,
	})
	writeJSON(w, http.StatusCreated, d)
}

// handleGetDomain returns the domain (state + DNS records for the dns_pending screen),
// checking tenant ownership first (brief §2/§10).
func (s *Server) handleGetDomain(w http.ResponseWriter, r *http.Request) {
	d, ok := s.ownedDomain(w, r)
	if !ok {
		return
	}
	zone := s.zoneOf(d)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": d.ID, "domain": d.Domain, "provision_state": d.ProvisionState,
		"dkim_status": d.DkimStatus, "dns_status": d.DnsStatus,
		"dns_records": orEmpty(provisioning.ParseZone(zone)), // never null: a new domain doesn't have a zone yet
	})
}

type createMailboxReq struct {
	Login    string `json:"login"`
	Name     string `json:"name"`
	Password string `json:"password"`
	QuotaGB  int64  `json:"quota_gb"`
}

// handleCreateMailbox creates a mailbox on the domain (only when 'active'). Checks ownership
// and translates it into the JMAP call to Stalwart (brief §2).
func (s *Server) handleCreateMailbox(w http.ResponseWriter, r *http.Request) {
	d, ok := s.ownedDomain(w, r)
	if !ok {
		return
	}
	if d.ProvisionState != provisioning.StateActive {
		writeErr(w, http.StatusConflict, "domains.not_active")
		return
	}
	// Quota: tenant's number of mailboxes vs. the plan's limit.
	plan := s.tenantPlan(r, d.TenantID)
	if count, _ := s.q.CountMailboxesByTenant(r.Context(), d.TenantID); int(count) >= plan.MaxMailboxes {
		writeErr(w, http.StatusPaymentRequired, "quota.mailboxes")
		return
	}
	var req createMailboxReq
	if !decode(w, r, &req) {
		return
	}
	if req.Login == "" || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "mailbox.missing_fields")
		return
	}
	quota := req.QuotaGB
	if quota <= 0 {
		quota = 5
	}
	acctID, err := s.sw.CreateAccount(r.Context(), stalwart.AccountInput{
		Login: req.Login, DomainID: strval(d.StalwartID), DisplayName: req.Name,
		Password: req.Password, QuotaBytes: quota << 30,
	})
	if err != nil {
		log.Printf("creating the mailbox in Stalwart failed: %v", err)
		if errors.Is(err, stalwart.ErrWeakPassword) {
			writeErr(w, http.StatusBadRequest, "mailbox.password_weak")
			return
		}
		writeErr(w, http.StatusBadGateway, "mailbox.stalwart_failed")
		return
	}
	email := req.Login + "@" + d.Domain
	mu, err := s.q.CreateMailUser(r.Context(), db.CreateMailUserParams{
		TenantID: d.TenantID, Email: email, Name: req.Name, Role: "user",
		StalwartAccountID: &acctID, QuotaBytes: quota << 30,
	})
	if err != nil {
		writeErr(w, http.StatusConflict, "mailbox.already_exists")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: d.TenantID, Valid: true}, Actor: mailUserFrom(r.Context()).Email,
		Action: "mailbox.create", Target: email,
	})
	s.syncSeats(d.TenantID) // seats in Stripe follow along (prorated for the period)
	writeJSON(w, http.StatusCreated, mu)
}

// handleDeleteDomain removes a domain: refuses if it still has mailboxes on it (delete users
// first — removal is never cascaded onto people's data), deletes it in Stalwart, removes the
// identity in SES (best-effort), and finally the row. Order designed for resumability: if
// Stalwart fails, nothing is lost; if the row fails, repeating the call is safe (everything is
// idempotent).
func (s *Server) handleDeleteDomain(w http.ResponseWriter, r *http.Request) {
	d, ok := s.ownedDomain(w, r)
	if !ok {
		return
	}
	u := mailUserFrom(r.Context())
	// A domain cannot go while mailboxes on it still hold data -- including the ones already
	// deleted, whose messages survive until the recovery window closes. Saying WHICH matters: one
	// case asks the admin to do something, the other asks them to wait, and "has mailboxes" told
	// somebody with an empty domain to go and delete mailboxes that were already gone (law 2.9).
	st, err := s.q.DomainMailboxState(r.Context(), db.DomainMailboxStateParams{
		TenantID: d.TenantID, Domain: &d.Domain,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "domains.check_failed")
		return
	}
	switch domainRemovalBlock(st) {
	case "domains.has_mailboxes":
		writeErr(w, http.StatusConflict, "domains.has_mailboxes")
		return
	case "domains.mailboxes_in_recovery":
		writeJSON(w, http.StatusConflict, map[string]any{
			"error_code": "domains.mailboxes_in_recovery",
			"error":      "Mailboxes deleted from this domain can still be restored",
			"free_after": st.OldestDeleted.Add(RecoveryWindow),
			"mailboxes":  st.Deleted,
		})
		return
	}
	if id := strval(d.StalwartID); id != "" {
		if err := s.sw.DeleteDomain(r.Context(), id); err != nil {
			writeErr(w, http.StatusBadGateway, "domains.stalwart_failed")
			return
		}
	}
	s.removeRelayIdentity(r.Context(), d.Domain)
	if err := s.q.DeleteDomainRow(r.Context(), db.DeleteDomainRowParams{ID: d.ID, TenantID: d.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "domains.delete_failed")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: d.TenantID, Valid: true}, Actor: u.Email, Action: "domain.delete", Target: d.Domain,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ownedDomain loads the domain from the route and ensures it belongs to the user's tenant.
func (s *Server) ownedDomain(w http.ResponseWriter, r *http.Request) (db.Domain, bool) {
	u := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "domains.invalid_id")
		return db.Domain{}, false
	}
	d, err := s.q.GetDomainByID(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "domains.not_found")
		return db.Domain{}, false
	}
	if d.TenantID != u.TenantID {
		// Doesn't leak the existence of another tenant's resource.
		writeErr(w, http.StatusNotFound, "domains.not_found")
		return db.Domain{}, false
	}
	return d, true
}

func strval(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// domainRemovalBlock says what stands between a domain and its removal, or "" if nothing does.
//
// The two answers need opposite advice. Live mailboxes are somebody's to delete; mailboxes already
// deleted are nobody's to hurry, and the only honest thing to say is when they stop counting. One
// number cannot tell them apart, and telling an admin with an empty domain to go and delete
// mailboxes that were already deleted is how "not yet" gets mistaken for "never" (law 2.9).
func domainRemovalBlock(st db.DomainMailboxStateRow) string {
	switch {
	case st.Total > st.Deleted:
		return "domains.has_mailboxes"
	case st.Deleted > 0:
		return "domains.mailboxes_in_recovery"
	default:
		return ""
	}
}
