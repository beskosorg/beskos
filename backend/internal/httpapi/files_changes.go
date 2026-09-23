package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

var errBadCursor = errors.New("invalid cursor")

// Directory sync feed: an incremental feed of changes to the current user's own files, consumed
// by the desktop sync daemon (written in Rust). The daemon keeps the opaque `cursor` and comes
// back with ?since=<cursor>. Contract:
//   - `hash` is the content ETag (content-addressed blob): equal means the file is unchanged.
//   - `deleted=true` is a tombstone (moved to trash): the client removes it locally.
//   - Purging (the hard delete when trash is emptied) does not appear in the feed; the client
//     reconciles via periodic full scans (the feed speeds up the common case, it does not
//     replace reconciliation).
// Scope: files owned by the authenticated user, within their tenant.

const fileChangesMaxLimit = 500

type fileChange struct {
	ID        uuid.UUID  `json:"id"`
	FolderID  *uuid.UUID `json:"folder_id"` // null = root
	Name      string     `json:"name"`
	Hash      string     `json:"hash"` // content ETag
	Size      int64      `json:"size"`
	Version   int32      `json:"version"`
	Deleted   bool       `json:"deleted"` // tombstone (in trash)
	UpdatedAt time.Time  `json:"updated_at"`
	// false = shared with me (I'm not the owner): the client treats it as READ-ONLY.
	Owned bool `json:"owned"`
}

func (s *Server) handleListFileChanges(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	sinceTS, sinceID, err := parseChangeCursor(r.URL.Query().Get("since"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "files.bad_cursor")
		return
	}
	limit := fileChangesMaxLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, e := strconv.Atoi(v); e == nil && n > 0 && n < limit {
			limit = n
		}
	}

	rows, err := s.q.ListFileChanges(r.Context(), db.ListFileChangesParams{
		TenantID:    mu.TenantID,
		OwnerUserID: ownerPg(mu),
		Principal:   mu.Email, // shares are by email: also brings in what was shared with me
		UpdatedAt:   sinceTS,
		ID:          sinceID,
		Limit:       int32(limit),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "files.changes_failed")
		return
	}

	changes := make([]fileChange, 0, len(rows))
	for _, row := range rows {
		c := fileChange{ID: row.ID, Name: row.Name, Hash: row.Hash, Size: row.Size, Version: row.Version, UpdatedAt: row.UpdatedAt, Owned: row.Owned}
		if row.FolderID.Valid {
			fid := uuid.UUID(row.FolderID.Bytes)
			c.FolderID = &fid
		}
		c.Deleted = row.DeletedAt.Valid
		changes = append(changes, c)
	}

	// Cursor = that of the last row; with no news, echoes the received cursor so the client doesn't rewind.
	cursor := r.URL.Query().Get("since")
	if n := len(changes); n > 0 {
		cursor = encodeChangeCursor(changes[n-1].UpdatedAt, changes[n-1].ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"changes":  changes,
		"cursor":   cursor,
		"has_more": len(rows) == limit,
	})
}

// The cursor is opaque to the client: "<updated_at RFC3339Nano>|<id>". Empty = start (epoch + null id),
// which makes the first page bring everything via (updated_at > epoch).
func parseChangeCursor(s string) (time.Time, uuid.UUID, error) {
	if s == "" {
		return time.Time{}, uuid.Nil, nil
	}
	ts, idStr, ok := strings.Cut(s, "|")
	if !ok {
		return time.Time{}, uuid.Nil, errBadCursor
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, uuid.Nil, errBadCursor
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return time.Time{}, uuid.Nil, errBadCursor
	}
	return t, id, nil
}

func encodeChangeCursor(t time.Time, id uuid.UUID) string {
	return t.UTC().Format(time.RFC3339Nano) + "|" + id.String()
}
