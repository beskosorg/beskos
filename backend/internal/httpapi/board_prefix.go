package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Board prefixes: the "QUA" in the task key "QUA-12".
//
// Derived from the board name and unique per tenant. The uniqueness matters because the number
// after the dash restarts at 1 on every board: two boards sharing a prefix both produce "QUA-1",
// for different tasks, and a key pasted into a conversation stops identifying anything.
//
// The guarantee lives in the database (unique index, migration 00132), not here. The SELECT inside
// CreateBoard picks the first free candidate, which is right almost always and racy in principle --
// two requests can both read the same prefix as free before either writes. This retries when that
// happens, which is the only honest way to use a "check then write" against a real constraint.

// uniqueViolation reports whether the error is Postgres refusing a duplicate key.
func uniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// Attempts before giving up. Each retry re-runs the query, which re-reads what is taken -- so a
// second attempt already knows about the prefix that beat it. Losing three in a row means an
// unusual amount of concurrent board creation in one tenant, and a real error is better than a
// loop.
const prefixAttempts = 3

// createBoard inserts a board, retrying when another request took the prefix first.
func (s *Server) createBoard(ctx context.Context, arg db.CreateBoardParams) (db.Board, error) {
	var err error
	for i := 0; i < prefixAttempts; i++ {
		var b db.Board
		b, err = s.q.CreateBoard(ctx, arg)
		if err == nil {
			return b, nil
		}
		if !uniqueViolation(err) {
			return db.Board{}, err
		}
	}
	return db.Board{}, err
}

// handleUpdateBoardPrefix changes the prefix of a board's task keys (QUA-12). Only the shape is
// imposed: uppercase, no spaces, 2 to 6 characters -- the rest is the board manager's decision.
// Existing cards start showing the new prefix: the key is derived (prefix + number), not stored, so
// renaming rewrites no history and breaks no links (a link carries the id).
func (s *Server) handleUpdateBoardPrefix(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if !s.requireBoardRole(w, r, id, boardRoleManager) {
		return
	}
	var req struct {
		Prefix string `json:"prefix"`
	}
	if !decode(w, r, &req) {
		return
	}
	p := strings.ToUpper(strings.TrimSpace(req.Prefix))
	p = nonAlnum.ReplaceAllString(p, "")
	if len(p) < 2 || len(p) > 6 {
		writeErr(w, http.StatusBadRequest, "boards.invalid_prefix")
		return
	}
	b, err := s.q.UpdateBoardPrefix(r.Context(), db.UpdateBoardPrefixParams{ID: id, TenantID: mu.TenantID, Prefix: p})
	if err != nil {
		// Taken by another board of the same tenant. Told apart from "no such board" on purpose: the
		// person typed a valid prefix and needs to know it is in use, not that their board vanished.
		// The index (migration 00132) is what refuses it; checking first would only widen the race.
		if uniqueViolation(err) {
			writeErr(w, http.StatusConflict, "boards.prefix_taken")
			return
		}
		writeErr(w, http.StatusNotFound, "boards.not_found")
		return
	}
	writeJSON(w, http.StatusOK, b)
}
