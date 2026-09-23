package httpapi

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

// Email aliases: an extra address that delivers to someone else's mailbox (contact@company.com →
// Ana's mailbox). The alias is NOT a member of the organization — it doesn't join chat, or groups,
// or the people list. It's just mail, and that's why it lives in its own table instead of a row in
// mail_users: the separation lives in the structure, not in a filter a new query could forget.
//
// In Stalwart, aliases are a field on the owner's account (x:Account, DIVERGENCES §D4). Our table
// is the source of truth for the list: every change rewrites the whole set on the account, which
// makes the operation idempotent — and turns reconciliation into a plain "write it back again".

// syncAliasesToStalwart rewrites the set of aliases for a mailbox in Stalwart based on our
// database. Call it after creating or deleting.
func (s *Server) syncAliasesToStalwart(ctx context.Context, tenant uuid.UUID, mu db.MailUser) error {
	if s.sw == nil || mu.StalwartAccountID == nil {
		return nil // no mail server connected (dev) or mailbox not yet provisioned
	}
	rows, err := s.q.ListAliasesForStalwart(ctx, db.ListAliasesForStalwartParams{TenantID: tenant, UserID: mu.ID})
	if err != nil {
		return err
	}
	aliases := make([]stalwart.Alias, 0, len(rows))
	for _, a := range rows {
		if a.StalwartDomainID == "" {
			continue // domain not yet provisioned there: nothing to attach the alias to
		}
		aliases = append(aliases, stalwart.Alias{Local: a.Local, DomainID: a.StalwartDomainID})
	}
	return s.sw.SetAccountAliases(ctx, *mu.StalwartAccountID, aliases)
}

// handleListAliases lists the tenant's aliases (panel).
func (s *Server) handleListAliases(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListEmailAliases(r.Context(), mu.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "aliases.failed")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, a := range rows {
		out = append(out, map[string]any{
			"id": a.ID, "address": a.Address, "domain": a.Domain,
			"owner_id": a.MailUserID, "owner_email": a.OwnerEmail, "owner_name": a.OwnerName,
			"created_at": iso(a.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"aliases": out})
}

type createAliasReq struct {
	Local    string `json:"local"`     // part before the @
	DomainID string `json:"domain_id"` // tenant's domain
	OwnerID  string `json:"owner_id"`  // mailbox that receives it
}

// handleCreateAlias creates the alias and provisions it in Stalwart.
func (s *Server) handleCreateAlias(w http.ResponseWriter, r *http.Request) {
	actor := mailUserFrom(r.Context())
	var req createAliasReq
	if !decode(w, r, &req) {
		return
	}
	local := strings.ToLower(strings.TrimSpace(req.Local))
	if local == "" || strings.ContainsAny(local, "@ ") {
		writeErr(w, http.StatusBadRequest, "aliases.invalid_local")
		return
	}
	domID, err := uuid.Parse(req.DomainID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "aliases.invalid_domain")
		return
	}
	dom, err := s.q.GetDomainByID(r.Context(), domID)
	// Tenant ownership is checked here and not only in the query: GetDomainByID is by id alone.
	if err != nil || dom.TenantID != actor.TenantID {
		writeErr(w, http.StatusNotFound, "aliases.invalid_domain")
		return
	}
	ownerID, err := uuid.Parse(req.OwnerID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "aliases.invalid_owner")
		return
	}
	owner, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: ownerID, TenantID: actor.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "aliases.invalid_owner")
		return
	}
	address := local + "@" + dom.Domain
	// An alias can't steal the address of an existing mailbox: mail would stop reaching its
	// rightful owner, and Stalwart would accept the creation without saying anything.
	if _, err := s.q.GetMailUserByEmail(r.Context(), address); err == nil {
		writeErr(w, http.StatusConflict, "aliases.address_taken")
		return
	}
	alias, err := s.q.CreateEmailAlias(r.Context(), db.CreateEmailAliasParams{
		TenantID: actor.TenantID, DomainID: domID, MailUserID: owner.ID, Address: address,
	})
	if err != nil {
		writeErr(w, http.StatusConflict, "aliases.address_taken")
		return
	}
	if err := s.syncAliasesToStalwart(r.Context(), actor.TenantID, owner); err != nil {
		// The reason MUST end up in the log: without it, "provision_failed" is a dead end in
		// production — there's no way to know whether Stalwart rejected the format, the address,
		// or the account.
		log.Printf("alias %s for %s: Stalwart provisioning failed: %v", address, owner.Email, err)
		// Leaves no garbage behind: if the mail server rejected it, the alias doesn't keep existing
		// only on our side.
		_ = s.q.DeleteEmailAlias(r.Context(), db.DeleteEmailAliasParams{ID: alias.ID, TenantID: actor.TenantID})
		writeErr(w, http.StatusBadGateway, "aliases.provision_failed")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgUUID(actor.TenantID), Actor: actor.Email, Action: "alias.create", Target: address,
	})
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": alias.ID, "address": alias.Address, "owner_id": owner.ID, "owner_email": owner.Email,
	})
}

// handleDeleteAlias removes the alias and writes the list back to Stalwart.
func (s *Server) handleDeleteAlias(w http.ResponseWriter, r *http.Request) {
	actor := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "aliases.not_found")
		return
	}
	alias, err := s.q.GetEmailAlias(r.Context(), db.GetEmailAliasParams{ID: id, TenantID: actor.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "aliases.not_found")
		return
	}
	if err := s.q.DeleteEmailAlias(r.Context(), db.DeleteEmailAliasParams{ID: id, TenantID: actor.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "aliases.failed")
		return
	}
	if owner, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: alias.MailUserID, TenantID: actor.TenantID}); err == nil {
		_ = s.syncAliasesToStalwart(r.Context(), actor.TenantID, owner)
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{
		TenantID: pgUUID(actor.TenantID), Actor: actor.Email, Action: "alias.delete", Target: alias.Address,
	})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// handleMyAddresses returns the addresses of the authenticated user: themselves and the aliases
// that deliver to their mailbox. This is what the composer shows in the "From:" field.
func (s *Server) handleMyAddresses(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	aliases, _ := s.q.ListAliasesForUser(r.Context(), db.ListAliasesForUserParams{TenantID: mu.TenantID, UserID: mu.ID})
	writeJSON(w, http.StatusOK, map[string]any{
		"primary": mu.Email,
		"aliases": orEmpty(aliases),
	})
}
