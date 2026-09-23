package httpapi

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Copy/duplication, folder download as zip, and batch actions on files.

// handleCopyFile duplicates a file owned by the user. Storage is content-addressed (hash), so
// the copy REUSES the same blob — it doesn't recopy bytes, doesn't change disk usage. New id, same folder.
// The name comes from the frontend (which knows the locale for the "(copy)" suffix); empty = same name.
func (s *Server) handleCopyFile(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	mu := mailUserFrom(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // optional body
	name := f.Name
	if n := strings.TrimSpace(req.Name); n != "" {
		name = n
	}
	prevMax, _ := s.q.MaxVersionByName(r.Context(), db.MaxVersionByNameParams{TenantID: mu.TenantID, Name: name})
	rec, err := s.q.CreateStorageFile(r.Context(), db.CreateStorageFileParams{
		TenantID: mu.TenantID, OwnerUserID: ownerPg(mu),
		Name: name, BlobKey: f.BlobKey, Hash: f.Hash, Size: f.Size, Version: prevMax + 1, FolderID: f.FolderID,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.record_failed")
		return
	}
	_ = s.q.InsertAudit(r.Context(), db.InsertAuditParams{TenantID: pgtype.UUID{Bytes: mu.TenantID, Valid: true}, Actor: mu.Email, Action: "file.copy", Target: name})
	writeJSON(w, http.StatusCreated, rec)
}

// handleZipFolder streams all files of a folder (recursively) as a .zip. It builds the owner's
// folder tree in memory and does a BFS from the root, preserving relative paths.
func (s *Server) handleZipFolder(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	root, err := s.q.GetFolder(r.Context(), db.GetFolderParams{ID: id, OwnerUserID: mu.ID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	all, _ := s.q.ListAllFoldersByOwner(r.Context(), mu.ID)
	childrenOf := map[string][]db.StorageFolder{}
	for _, fo := range all {
		p := ""
		if fo.ParentID.Valid {
			p = uuid.UUID(fo.ParentID.Bytes).String()
		}
		childrenOf[p] = append(childrenOf[p], fo)
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", zipSafe(root.Name)+".zip"))
	zw := zip.NewWriter(w)
	defer zw.Close()

	type node struct{ id, rel string }
	queue := []node{{root.ID.String(), ""}}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		fid, _ := uuid.Parse(n.id)
		files, _ := s.q.ListFilesInFolder(r.Context(), db.ListFilesInFolderParams{
			OwnerUserID: ownerPg(mu), FolderID: pgtype.UUID{Bytes: fid, Valid: true},
		})
		for _, file := range files {
			obj, gerr := s.storage.Get(r.Context(), file.BlobKey)
			if gerr != nil {
				continue // missing blob: skip the file instead of breaking the zip
			}
			if fw, cerr := zw.Create(path.Join(n.rel, file.Name)); cerr == nil {
				_, _ = io.Copy(fw, obj)
			}
			_ = obj.Close()
		}
		for _, c := range childrenOf[n.id] {
			queue = append(queue, node{c.ID.String(), path.Join(n.rel, c.Name)})
		}
	}
}

// zipSafe cleans the folder name to turn it into a file name (no path separators).
func zipSafe(name string) string {
	name = strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(strings.TrimSpace(name))
	if name == "" {
		return "folder"
	}
	return name
}

// handleBatchFiles applies an action to several of the owner's files at once: trash | restore | move.
// Each operation is filtered by owner_user_id (multi-tenant), so ids from outside simply don't match.
func (s *Server) handleBatchFiles(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		IDs      []string `json:"ids"`
		Action   string   `json:"action"`
		FolderID string   `json:"folder_id"` // for "move" ("" = root)
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.IDs) == 0 {
		writeErr(w, http.StatusBadRequest, "storage.batch_empty")
		return
	}
	if req.Action != "trash" && req.Action != "restore" && req.Action != "move" {
		writeErr(w, http.StatusBadRequest, "storage.bad_action")
		return
	}
	var dest pgtype.UUID
	if req.Action == "move" && req.FolderID != "" {
		fid, err := uuid.Parse(req.FolderID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "storage.invalid_id")
			return
		}
		if _, err := s.q.GetFolder(r.Context(), db.GetFolderParams{ID: fid, OwnerUserID: mu.ID}); err != nil {
			writeErr(w, http.StatusNotFound, "storage.not_found") // destination folder isn't owned by this user
			return
		}
		dest = pgtype.UUID{Bytes: fid, Valid: true}
	}
	done := 0
	for _, raw := range req.IDs {
		fid, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		switch req.Action {
		case "trash":
			if _, err := s.q.TrashFile(r.Context(), db.TrashFileParams{ID: fid, OwnerUserID: ownerPg(mu)}); err == nil {
				done++
			}
		case "restore":
			if _, err := s.q.RestoreFile(r.Context(), db.RestoreFileParams{ID: fid, OwnerUserID: ownerPg(mu)}); err == nil {
				done++
			}
		case "move":
			if _, err := s.q.MoveStorageFile(r.Context(), db.MoveStorageFileParams{ID: fid, OwnerUserID: ownerPg(mu), FolderID: dest}); err == nil {
				done++
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"done": done})
}
