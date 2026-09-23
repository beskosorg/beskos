package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Directory sync — mirror of subfolders. The daemon needs the user's folder tree to
// place each file in the right spot and to create missing folders on upload. Scope: folders owned
// by the user themselves (law §5).

type syncFolder struct {
	ID       uuid.UUID  `json:"id"`
	ParentID *uuid.UUID `json:"parent_id"` // null = root
	Name     string     `json:"name"`
}

func toSyncFolder(f db.StorageFolder) syncFolder {
	sf := syncFolder{ID: f.ID, Name: f.Name}
	if f.ParentID.Valid {
		pid := uuid.UUID(f.ParentID.Bytes)
		sf.ParentID = &pid
	}
	return sf
}

// handleListSyncFolders: GET /api/sync/folders — the owner's entire folder tree (the daemon builds
// the paths in memory).
func (s *Server) handleListSyncFolders(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListAllFoldersByOwner(r.Context(), mu.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "files.folders_failed")
		return
	}
	out := make([]syncFolder, 0, len(rows))
	for _, f := range rows {
		out = append(out, toSyncFolder(f))
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": out})
}

// handleSyncTrashFile: POST /api/sync/files/{id}/trash — the file was deleted in the local folder.
// It goes to the TRASH (soft delete, recoverable), never a hard delete: a local mistake doesn't
// destroy the original. The user empties the trash in the webmail if they really want to delete it.
func (s *Server) handleSyncTrashFile(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "files.invalid_id")
		return
	}
	rec, err := s.q.TrashFile(r.Context(), db.TrashFileParams{ID: id, OwnerUserID: ownerPg(mu)})
	if err != nil {
		writeErr(w, http.StatusNotFound, "files.not_found")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// handleSyncMoveFile: POST /api/sync/files/{id}/move?folder_id=<uuid|empty>&name=<name> — the file
// was moved/renamed in the local folder. Empty `folder_id` = root.
func (s *Server) handleSyncMoveFile(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "files.invalid_id")
		return
	}
	name := sanitizeUploadName(r.URL.Query().Get("name"))
	if name == "" {
		writeErr(w, http.StatusBadRequest, "files.invalid_name")
		return
	}
	var folder pgtype.UUID
	if fid := r.URL.Query().Get("folder_id"); fid != "" {
		pid, perr := uuid.Parse(fid)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, "files.invalid_id")
			return
		}
		// The destination folder must belong to the user themselves.
		if _, gerr := s.q.GetFolder(r.Context(), db.GetFolderParams{ID: pid, OwnerUserID: mu.ID}); gerr != nil {
			writeErr(w, http.StatusNotFound, "files.folder_not_found")
			return
		}
		folder = pgtype.UUID{Bytes: pid, Valid: true}
	}
	rec, err := s.q.MoveRenameStorageFile(r.Context(), db.MoveRenameStorageFileParams{
		ID: id, OwnerUserID: ownerPg(mu), FolderID: folder, Name: name,
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, "files.not_found")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// handleCreateSyncFolder: POST /api/sync/folders?name=<name>&parent_id=<uuid> — creates a folder.
// The daemon only calls this for segments that don't exist yet (it reuses the tree it already read).
func (s *Server) handleCreateSyncFolder(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	name := sanitizeUploadName(r.URL.Query().Get("name"))
	if name == "" {
		writeErr(w, http.StatusBadRequest, "files.invalid_name")
		return
	}
	var parent pgtype.UUID
	if pid := r.URL.Query().Get("parent_id"); pid != "" {
		id, err := uuid.Parse(pid)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "files.invalid_id")
			return
		}
		// The parent folder MUST belong to the user themselves — otherwise content could be hung
		// off another tenant's tree.
		if _, err := s.q.GetFolder(r.Context(), db.GetFolderParams{ID: id, OwnerUserID: mu.ID}); err != nil {
			writeErr(w, http.StatusNotFound, "files.folder_not_found")
			return
		}
		parent = pgtype.UUID{Bytes: id, Valid: true}
	}
	f, err := s.q.CreateFolder(r.Context(), db.CreateFolderParams{
		TenantID: mu.TenantID, OwnerUserID: mu.ID, ParentID: parent, Name: name,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "files.folder_create_failed")
		return
	}
	writeJSON(w, http.StatusCreated, toSyncFolder(f))
}
