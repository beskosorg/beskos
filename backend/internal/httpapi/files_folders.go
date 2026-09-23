package httpapi

import (
	"net/http"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// --- Folders ---

type folderReq struct {
	Name     string `json:"name"`
	ParentID string `json:"parent_id"`
}

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req folderReq
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "storage.folder_name_required")
		return
	}
	var parent pgtype.UUID
	if req.ParentID != "" {
		if id, err := uuid.Parse(req.ParentID); err == nil {
			parent = pgtype.UUID{Bytes: id, Valid: true}
		}
	}
	f, err := s.q.CreateFolder(r.Context(), db.CreateFolderParams{TenantID: mu.TenantID, OwnerUserID: mu.ID, ParentID: parent, Name: req.Name})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.folder_failed")
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (s *Server) handleRenameFolder(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	var req folderReq
	if !decode(w, r, &req) || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "storage.folder_name_required")
		return
	}
	f, err := s.q.RenameFolder(r.Context(), db.RenameFolderParams{ID: id, OwnerUserID: mu.ID, Name: req.Name})
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

type moveFolderReq struct {
	ParentID *string `json:"parent_id"` // null/"" = root
}

// handleMoveFolder changes a folder's parent. Blocks cycles: won't allow moving into itself or
// into a descendant (otherwise the subtree disappears from navigation and becomes a loop in the DB).
func (s *Server) handleMoveFolder(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	var req moveFolderReq
	if !decode(w, r, &req) {
		return
	}
	var parent pgtype.UUID
	if req.ParentID != nil && *req.ParentID != "" {
		pid, err := uuid.Parse(*req.ParentID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "storage.invalid_id")
			return
		}
		if pid == id {
			writeErr(w, http.StatusBadRequest, "storage.folder_cycle")
			return
		}
		// Walks up from the proposed parent to the root; if it crosses the folder itself, it's a
		// descendant -> cycle.
		cur := pgtype.UUID{Bytes: pid, Valid: true}
		for cur.Valid {
			f, err := s.q.GetFolder(r.Context(), db.GetFolderParams{ID: cur.Bytes, OwnerUserID: mu.ID})
			if err != nil {
				writeErr(w, http.StatusNotFound, "storage.not_found")
				return
			}
			if f.ID == id {
				writeErr(w, http.StatusBadRequest, "storage.folder_cycle")
				return
			}
			cur = f.ParentID
		}
		parent = pgtype.UUID{Bytes: pid, Valid: true}
	}
	f, err := s.q.MoveFolder(r.Context(), db.MoveFolderParams{ID: id, OwnerUserID: mu.ID, ParentID: parent})
	if err != nil {
		writeErr(w, http.StatusNotFound, "storage.not_found")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "storage.invalid_id")
		return
	}
	if err := s.q.DeleteFolder(r.Context(), db.DeleteFolderParams{ID: id, OwnerUserID: mu.ID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.folder_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
