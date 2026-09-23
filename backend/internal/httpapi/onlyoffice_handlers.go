package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

const docTokenTTL = 10 * time.Hour

// aiPluginGUID is the Document Server's native AI plugin (autostart in the authenticated editor).
const aiPluginGUID = "asc.{9DC93CDB-B576-4F0C-B55E-FCC9C48DD007}"

var errStorageDisabled = errors.New("storage disabled")

// handleEditSession opens an office editing session: returns the Document Server's api.js and
// the JWT-signed config the editor consumes, with the mode (view or edit) resolved from the
// caller's write access.
func (s *Server) handleEditSession(w http.ResponseWriter, r *http.Request) {
	if s.office == nil {
		writeErr(w, http.StatusServiceUnavailable, "files.editing_disabled")
		return
	}
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	f, canWrite, ok := s.resolveDocAccess(w, r, mu)
	if !ok {
		return
	}
	// Office editing is gated by plan: a plan without OnlyOffice edit downgrades to view-only.
	if canWrite && !s.tenantPlanByID(r.Context(), mu.TenantID).OnlyOfficeEdit {
		canWrite = false
	}
	ext := extOf(f.Name)
	if !officeExts[ext] {
		writeErr(w, http.StatusUnprocessableEntity, "files.not_editable")
		return
	}
	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = "pt-BR"
	}
	// Editor UI theme matching Beskos (light/dark). Free params (standard branding).
	darkTheme := r.URL.Query().Get("theme") == "dark"
	token := s.office.mintToken(docClaims{
		FileID: f.ID, UserID: mu.ID, TenantID: mu.TenantID, CanWrite: canWrite,
		Exp: time.Now().Add(docTokenTTL),
	})
	base := s.office.filesHostURL
	// key: unique per version (alphanumeric) — changes on every new version so the DS reloads.
	key := strings.ReplaceAll(uuid.NewSHA1(f.ID, []byte(strconv.Itoa(int(f.Version)))).String(), "-", "")

	// Strips the chrome down to integrate with Beskos. The header branding is ours (applyBranding),
	// which requires the branding patch on the Document Server; attribution to Ascensio System SIA
	// stays in "About". See onlyoffice_branding.go.
	customization := officeChrome(darkTheme, true)
	// Close button IN the editor's header (avoids needing our own bar on top). Only shows up if
	// the client registers the onRequestClose event — it decides where to go back to. No `text`:
	// the Document Server uses its own string, already translated via `lang` (law §4.6).
	customization["close"] = map[string]any{"visible": true}
	s.office.applyBranding(customization)

	config := map[string]any{
		"document": map[string]any{
			"fileType": ext,
			"key":      key,
			"title":    f.Name,
			"url":      base + "/doc/files/" + f.ID.String() + "/download?token=" + token,
			"permissions": map[string]any{
				"edit": canWrite, "download": true, "print": true,
			},
		},
		"documentType": documentType(ext),
		"editorConfig": map[string]any{
			"mode":        map[bool]string{true: "edit", false: "view"}[canWrite],
			"lang":        lang,
			"user":        map[string]any{"id": mu.ID.String(), "name": mu.Name},
			"callbackUrl": base + "/doc/callback/" + f.ID.String() + "?token=" + token,
			// Plugins ENABLED in the authenticated editor (the AI one is used here; the public one has no plugins).
			"plugins": map[string]any{
				"autostart": []string{aiPluginGUID},
			},
			"customization": customization,
		},
	}
	signed, err := s.office.jwtSign(config)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "files.editor_config_failed")
		return
	}
	config["token"] = signed

	writeJSON(w, http.StatusOK, map[string]any{
		"apiJs":    s.office.publicURL + "/web-apps/apps/api/documents/api.js",
		"config":   config,
		"canWrite": canWrite,
		"name":     f.Name,
	})
}

// resolveDocAccess decides whether the user can open the file and whether they can write. File
// owner can write; shared as "editor" can write; "reader" opens for viewing only; no link → 403.
// Always isolated by tenant (law §5.1) and checking ownership/sharing (law §5.2).
func (s *Server) resolveDocAccess(w http.ResponseWriter, r *http.Request, mu db.MailUser) (db.StorageFile, bool, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return db.StorageFile{}, false, false
	}
	f, err := s.q.GetStorageFileInTenant(r.Context(), db.GetStorageFileInTenantParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return db.StorageFile{}, false, false
	}
	if f.OwnerUserID.Valid && f.OwnerUserID.Bytes == mu.ID {
		return f, true, true // owner can write
	}
	shares, _ := s.q.ListSharesByFile(r.Context(), db.ListSharesByFileParams{FileID: id, TenantID: mu.TenantID})
	if canWrite, has := shareAccess(shares, mu.Email); has {
		return f, canWrite, true
	}
	writeErr(w, http.StatusForbidden, "files.no_access")
	return db.StorageFile{}, false, false
}

// orgSharePrincipal is the sentinel "principal" for an ORGANIZATION-LEVEL share: any authenticated
// tenant member gets the granted access (reader/editor). It's not an email — the leading "@"
// guarantees it never collides with a real address.
const orgSharePrincipal = "@org"

// shareAccess computes a user's access to a file from the shares: the BEST of the direct share
// (by email) and the organization share. Editor beats reader. (canWrite, has).
func shareAccess(shares []db.StorageShare, email string) (canWrite, has bool) {
	for _, sh := range shares {
		if sh.Principal == email || sh.Principal == orgSharePrincipal {
			has = true
			if sh.Level == "editor" {
				canWrite = true
			}
		}
	}
	return
}

// docAuth validates the query's HMAC token and loads the file (isolated by tenant, law §5.1).
func (s *Server) docAuth(w http.ResponseWriter, r *http.Request) (docClaims, db.StorageFile, bool) {
	var cl docClaims
	if s.office == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return cl, db.StorageFile{}, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return cl, db.StorageFile{}, false
	}
	cl, ok := s.office.parseToken(r.URL.Query().Get("token"))
	if !ok || cl.FileID != id {
		w.WriteHeader(http.StatusForbidden)
		return cl, db.StorageFile{}, false
	}
	f, err := s.q.GetStorageFileInTenant(r.Context(), db.GetStorageFileInTenantParams{ID: id, TenantID: cl.TenantID})
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return cl, db.StorageFile{}, false
	}
	return cl, f, true
}

// handleDocDownload: the Document Server fetches the file's content here.
func (s *Server) handleDocDownload(w http.ResponseWriter, r *http.Request) {
	_, f, ok := s.docAuth(w, r)
	if !ok {
		return
	}
	if !s.storageReady(w) {
		return
	}
	obj, err := s.storage.Get(r.Context(), f.BlobKey)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	defer obj.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, obj)
}

// ooCallback is the body the Document Server sends when saving.
type ooCallback struct {
	Status int    `json:"status"` // 2/6 = ready to save
	URL    string `json:"url"`    // where to download the edited file from
	Token  string `json:"token"`  // JWT from the body (when JWT is enabled)
}

// handleDocCallback: the Document Server notifies that the document was saved; we fetch the
// edited file and write it as a NEW version in MinIO. A view-only session never triggers a save.
func (s *Server) handleDocCallback(w http.ResponseWriter, r *http.Request) {
	cl, f, ok := s.docAuth(w, r)
	if !ok {
		return
	}
	var cb ooCallback
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&cb); err != nil {
		writeJSON(w, http.StatusOK, map[string]int{"error": 1})
		return
	}
	// JWT from the body is MANDATORY: it's the server-to-server proof that the callback came from
	// the Document Server (signed with the shared secret). It can't be optional — otherwise all it
	// would take is omitting the token and the query's HMAC (which leaks in logs/Referer) would be
	// the only barrier (audit H2).
	if _, valid := s.office.jwtVerify(cb.Token); !valid {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	// Only status 2 (MustSave) and 6 (ForceSave) require saving; and only with write permission.
	if (cb.Status == 2 || cb.Status == 6) && cl.CanWrite && cb.URL != "" {
		if err := s.saveEditedFile(r, f, cb.URL); err != nil {
			log.Printf("onlyoffice save failed (file=%s url=%s): %v", f.ID, cb.URL, err)
			writeJSON(w, http.StatusOK, map[string]int{"error": 1})
			return
		}
		log.Printf("onlyoffice saved new version (file=%s)", f.ID)
	}
	writeJSON(w, http.StatusOK, map[string]int{"error": 0})
}

// saveEditedFile downloads the edited file from the Document Server and saves it as a new version.
func (s *Server) saveEditedFile(r *http.Request, f db.StorageFile, url string) error {
	if s.storage == nil {
		return errStorageDisabled
	}
	target, ok := s.office.fetchURLAllowed(url)
	if !ok {
		return fmt.Errorf("onlyoffice: refused non-DocumentServer callback url %q", url)
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	// Not following redirects (a 3xx could jump to an internal host) and with a timeout.
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("onlyoffice: redirect on callback url blocked")
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, s.maxUploadLimit()))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	blobKey := f.TenantID.String() + "/" + hash
	if exists, _ := s.storage.Exists(r.Context(), blobKey); !exists {
		if err := s.storage.Put(r.Context(), blobKey, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
			return err
		}
	}
	// Content identical to the current one (same hash) → nothing changed; doesn't create a version
	// or bump it. Avoids clutter from identical "versions" when the Document Server fires callbacks
	// without a real edit.
	if hash == f.Hash {
		return nil
	}
	// History: snapshots the CURRENT version before overwriting, so it can be listed/restored.
	_ = s.q.SnapshotFileVersion(r.Context(), db.SnapshotFileVersionParams{
		FileID: f.ID, TenantID: f.TenantID, BlobKey: f.BlobKey, Hash: f.Hash, Size: f.Size, Version: f.Version,
	})
	_, err = s.q.UpdateStorageFileContent(r.Context(), db.UpdateStorageFileContentParams{
		ID: f.ID, TenantID: f.TenantID, BlobKey: blobKey, Hash: hash, Size: int64(len(data)),
	})
	return err
}
