package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/auth"
	"github.com/beskosorg/beskos/backend/internal/db"
)

// linkExpiry converts the requested days into a deadline: absent (nil) = default fileLinkTTL;
// 0 = no expiration (NULL); N = N days. Capped at 365 so it doesn't become "forever" by mistake.
func linkExpiry(days *int) pgtype.Timestamptz {
	d := int(fileLinkTTL / (24 * time.Hour))
	if days != nil {
		d = *days
	}
	if d <= 0 {
		return pgtype.Timestamptz{} // NULL: no expiration
	}
	if d > 365 {
		d = 365
	}
	return pgtype.Timestamptz{Time: time.Now().Add(time.Duration(d) * 24 * time.Hour), Valid: true}
}

// fileLinkTTL is how long a public download link (for large attachments) stays valid. Once it
// passes, the token stops resolving and a new one is generated on the next send.
const fileLinkTTL = 30 * 24 * time.Hour

// Public download links for large attachments (>10MB) sent by email. The composer uploads the
// file to Files, requests a link, and inserts it into the body — like Gmail. The link carries a
// secret token: whoever has it can download, without logging in. Same-org recipients also get a
// share in Files (handleShareFile).

// newLinkToken generates an unpredictable token (32 bytes → base64url), the link's authorization.
func newLinkToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// handleCreateFileLink returns the public download token for a file (owned by the caller).
// Idempotent: reuses the existing link instead of creating a new one on every send.
func (s *Server) handleCreateFileLink(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	var req struct {
		ExpiresDays   *int   `json:"expires_days"`   // nil=30d · 0=no expiration · N=N days
		Password      string `json:"password"`       // ""=no password
		Permission    string `json:"permission"`     // "view" (default) | "edit"
		AllowDownload *bool  `json:"allow_download"` // nil=true (default) · the download toggle
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // optional body: no body keeps the defaults
	expires := linkExpiry(req.ExpiresDays)
	pwHash := ""
	if req.Password != "" {
		h, herr := auth.HashPassword(req.Password)
		if herr != nil {
			writeErr(w, http.StatusInternalServerError, "storage.link_failed")
			return
		}
		pwHash = h
	}
	perm := "view"
	if req.Permission == "edit" {
		perm = "edit"
	}
	allowDl := true
	if req.AllowDownload != nil {
		allowDl = *req.AllowDownload
	}
	// Already exists? Reconfigure it (expiration/password/permission/download) while keeping the
	// same token.
	if existing, err := s.q.GetFileLinkByFile(r.Context(), f.ID); err == nil {
		if _, uerr := s.q.UpdateFileLink(r.Context(), db.UpdateFileLinkParams{
			FileID: f.ID, ExpiresAt: expires, PasswordHash: pwHash, TenantID: f.TenantID,
			Permission: perm, AllowDownload: allowDl,
		}); uerr != nil {
			writeErr(w, http.StatusInternalServerError, "storage.link_failed")
			return
		}
		writeJSON(w, http.StatusOK, linkDTO(existing.Token, expires, pwHash != "", perm, allowDl))
		return
	}
	mu := mailUserFrom(r.Context())
	token, err := newLinkToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.link_failed")
		return
	}
	link, err := s.q.CreateFileLink(r.Context(), db.CreateFileLinkParams{
		TenantID: f.TenantID, FileID: f.ID, Token: token, CreatedBy: mu.ID,
		ExpiresAt: expires, PasswordHash: pwHash, Permission: perm, AllowDownload: allowDl,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.link_failed")
		return
	}
	writeJSON(w, http.StatusCreated, linkDTO(link.Token, expires, pwHash != "", perm, allowDl))
}

// linkDTO builds the link response (token + expiration + password + permission + download).
func linkDTO(token string, expires pgtype.Timestamptz, hasPassword bool, permission string, allowDownload bool) map[string]any {
	out := map[string]any{"token": token, "has_password": hasPassword, "permission": permission, "allow_download": allowDownload}
	if expires.Valid {
		out["expires_at"] = expires.Time
	}
	return out
}

// handleGetFileLink returns the ACTIVE public link token for a file (or an empty token if there
// isn't one), for the UI to show/copy/revoke. Owner only. Always 200 (token "" = no link) to keep
// the client simple.
func (s *Server) handleGetFileLink(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	link, err := s.q.GetFileLinkByFile(r.Context(), f.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"token": ""})
		return
	}
	writeJSON(w, http.StatusOK, linkDTO(link.Token, link.ExpiresAt, link.PasswordHash != "", link.Permission, link.AllowDownload))
}

// handleRevokeFileLink revokes (deletes) a file's public link — owner only.
func (s *Server) handleRevokeFileLink(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	if err := s.q.DeleteFileLinkByFile(r.Context(), db.DeleteFileLinkByFileParams{FileID: f.ID, TenantID: f.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.link_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// handleListSharedWithMe lists the files other people in the tenant have shared with me.
func (s *Server) handleListSharedWithMe(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	files, err := s.q.ListFilesSharedWithMe(r.Context(), db.ListFilesSharedWithMeParams{
		TenantID: mu.TenantID, Principal: mu.Email,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.list_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": orEmpty(files)})
}

// handleShareWithRecipients shares a file with an email's recipients who are from the SAME
// organisation — that way the large attachment shows up in "Shared with me" in their Files and
// triggers the notification. Externals are skipped (they have no account): for them, only the
// public link applies.
func (s *Server) handleShareWithRecipients(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	var req struct {
		Emails []string `json:"emails"`
	}
	if !decode(w, r, &req) {
		return
	}
	mu := mailUserFrom(r.Context())
	shared, external, notEmailed := 0, 0, 0
	for _, email := range req.Emails {
		if email == "" || email == mu.Email {
			continue
		}
		target, terr := s.q.GetMailUserByEmail(r.Context(), email)
		if terr != nil || target.TenantID != f.TenantID {
			// From outside the organisation: no app to receive a notification in — the link goes by
			// email instead.
			if link, sent := s.shareWithOutsider(r, f, mu, email); link != "" {
				shared++
				external++
				if !sent {
					notEmailed++
				}
			}
			continue
		}
		if _, err := s.q.CreateShare(r.Context(), db.CreateShareParams{
			FileID: f.ID, TenantID: f.TenantID, Principal: email, Level: "leitor",
		}); err != nil {
			continue
		}
		s.notifyUser(r.Context(), f.TenantID, target.ID, "file_share",
			"New shared file", mu.Email+" shared \""+f.Name+"\" with you", fileLink(f.ID), uuid.Nil,
			map[string]string{"actor": mu.Email, "file": f.Name})
		shared++
	}
	// `external`/`not_emailed`: the UI has to be able to say "the link was created but the email
	// didn't go out" instead of reporting the share as fully done.
	writeJSON(w, http.StatusOK, map[string]int{"shared": shared, "external": external, "not_emailed": notEmailed})
}

// handleShareWithGroup shares the file with ALL members of an organisation group/team — it expands
// into per-email shares (the shares model is per-principal). Dedup: a member already shared with
// doesn't become a duplicate row. The group has to be in the same tenant (never trust the
// request's id).
func (s *Server) handleShareWithGroup(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	var req struct {
		GroupID string `json:"group_id"`
		Level   string `json:"level"`
	}
	if !decode(w, r, &req) {
		return
	}
	gid, err := uuid.Parse(req.GroupID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "groups.invalid_id")
		return
	}
	level := "leitor"
	if req.Level == "editor" {
		level = "editor"
	}
	mu := mailUserFrom(r.Context())
	if _, err := s.q.GetUserGroup(r.Context(), db.GetUserGroupParams{ID: gid, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "groups.not_found")
		return
	}
	// Dedup: emails already shared with don't repeat (there's no unique constraint on
	// storage_shares).
	existing := map[string]bool{}
	if shares, err := s.q.ListSharesByFile(r.Context(), db.ListSharesByFileParams{FileID: f.ID, TenantID: f.TenantID}); err == nil {
		for _, sh := range shares {
			existing[sh.Principal] = true
		}
	}
	members, _ := s.q.ListGroupMembers(r.Context(), gid)
	shared := 0
	for _, m := range members {
		if m.Email == "" || m.Email == mu.Email || existing[m.Email] {
			continue
		}
		if _, err := s.q.CreateShare(r.Context(), db.CreateShareParams{
			FileID: f.ID, TenantID: f.TenantID, Principal: m.Email, Level: level,
		}); err != nil {
			continue
		}
		existing[m.Email] = true
		s.notifyUser(r.Context(), f.TenantID, m.ID, "file_share",
			"New shared file", mu.Email+" shared \""+f.Name+"\" with you", fileLink(f.ID), uuid.Nil,
			map[string]string{"actor": mu.Email, "file": f.Name})
		shared++
	}
	writeJSON(w, http.StatusOK, map[string]int{"shared": shared})
}

// handleMyGroups lists the groups/teams the current user belongs to (for sharing files with the
// team). No special RBAC: it's just the user's own membership.
func (s *Server) handleMyGroups(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	groups, _ := s.q.ListGroupsForUser(r.Context(), mu.ID)
	writeJSON(w, http.StatusOK, orEmpty(groups))
}

// handlePublicFileDownload serves the file by token, with NO session. The token is the
// authorization — there is deliberately no owner/tenant check. Forces a download (never inline:
// third-party content).
func (s *Server) handlePublicFileDownload(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	token := chi.URLParam(r, "token")
	if token == "" {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	f, err := s.q.GetFileByLinkToken(r.Context(), token)
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	// Password-protected link: requires the correct ?pw= (the download page collects it and resends
	// it).
	if f.PasswordHash != "" {
		pw := r.URL.Query().Get("pw")
		if pw == "" || !auth.CheckPassword(f.PasswordHash, pw) {
			writeErr(w, http.StatusUnauthorized, "storage.link_password")
			return
		}
	}
	// Download disabled on the link (view-only): the raw route also refuses it, not just the
	// viewer's button.
	if !f.AllowDownload {
		writeErr(w, http.StatusForbidden, "storage.download_disabled")
		return
	}
	obj, err := s.storage.Get(r.Context(), f.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer obj.Close()
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.Name))
	_, _ = io.Copy(w, obj)
}
