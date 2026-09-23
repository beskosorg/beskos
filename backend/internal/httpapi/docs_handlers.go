package httpapi

// The Documents app: what somebody was working on, across every folder.
//
// A VIEW, NOT A STORE. Every document listed here is a file in Drive, with the same owner, the same
// sharing, the same version history and the same trash. Nothing is created here that is not created
// there. The whole app exists because "create a document" and "find a file" are different
// intentions: a file browser is organised by where things were put, and this is organised by what
// somebody had open on Tuesday -- which is the question people actually arrive with.
//
// Deliberately WITHOUT a template gallery. Google's screen is mostly templates because Google has
// hundreds of them; a gallery holding one blank card advertises an empty shelf.

import (
	"net/http"
	"strconv"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// recentDocsCap is the most this ever returns. Enough that scrolling finds last month's contract,
// small enough that the response stays a list rather than an export.
const recentDocsCap = 200

func (s *Server) handleListRecentDocuments(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	limit := 60
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= recentDocsCap {
		limit = n
	}
	rows, err := s.q.ListRecentDocuments(r.Context(), db.ListRecentDocumentsParams{
		TenantID: mu.TenantID, UserID: ownerPg(mu), Email: mu.Email, Lim: int32(limit),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "docs.list_failed")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, f := range rows {
		out = append(out, map[string]any{
			"id": f.ID, "name": f.Name, "size": f.Size, "version": f.Version,
			"updated_at": f.UpdatedAt, "folder_id": f.FolderID, "starred": f.Starred,
			// `owned` is what the UI needs to decide whether to offer renaming and deleting; a
			// document somebody shared with me is mine to edit, not mine to throw away.
			"owned": f.Owned,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
