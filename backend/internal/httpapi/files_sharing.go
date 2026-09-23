package httpapi

import (
	"net/http"
	"strings"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type shareReq struct {
	Principal string `json:"principal"`
	Level     string `json:"level"` // owner | editor | leitor | link
}

func (s *Server) handleShareFile(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	var req shareReq
	if !decode(w, r, &req) {
		return
	}
	valid := map[string]bool{"owner": true, "editor": true, "leitor": true, "link": true}
	if req.Principal == "" || !valid[req.Level] {
		writeErr(w, http.StatusBadRequest, "storage.invalid_share")
		return
	}
	// CreateShare is an upsert; to know whether this is a NEW share (and therefore whether someone
	// needs notifying), we check beforehand.
	existed := false
	if cur, lerr := s.q.ListSharesByFile(r.Context(), db.ListSharesByFileParams{FileID: f.ID, TenantID: f.TenantID}); lerr == nil {
		for _, c := range cur {
			if strings.EqualFold(c.Principal, req.Principal) {
				existed = true
				break
			}
		}
	}
	sh, err := s.q.CreateShare(r.Context(), db.CreateShareParams{
		FileID: f.ID, TenantID: f.TenantID, Principal: req.Principal, Level: req.Level,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.share_failed")
		return
	}
	// Inside the organization: an in-app notification (feeds the Files badge). Outside: an email with
	// the public link, the only entry point for someone without an account.
	if target, terr := s.q.GetMailUserByEmail(r.Context(), req.Principal); terr != nil || target.TenantID != f.TenantID {
		// Only notifies someone who did NOT already have access: changing the permission of someone
		// already notified doesn't justify a second "a file was shared with you".
		if strings.Contains(req.Principal, "@") && !existed {
			link, sent := s.shareWithOutsider(r, f, mailUserFrom(r.Context()), req.Principal)
			writeJSON(w, http.StatusCreated, map[string]any{"id": sh.ID, "external": true, "link": link, "emailed": sent})
			return
		}
	} else {
		mu := mailUserFrom(r.Context())
		s.notifyUser(r.Context(), f.TenantID, target.ID, "file_share",
			"New shared file", mu.Email+" shared \""+f.Name+"\" with you", fileLink(f.ID), uuid.Nil,
			map[string]string{"actor": mu.Email, "file": f.Name})
	}
	writeJSON(w, http.StatusCreated, sh)
}

func (s *Server) handleListShares(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	shares, err := s.q.ListSharesByFile(r.Context(), db.ListSharesByFileParams{FileID: f.ID, TenantID: f.TenantID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.list_failed")
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(shares))
}

func (s *Server) handleDeleteShare(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "shareId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	if err := s.q.DeleteShare(r.Context(), db.DeleteShareParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.share_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}
