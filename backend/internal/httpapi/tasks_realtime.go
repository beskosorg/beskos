package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/chat"
	"github.com/beskosorg/beskos/backend/internal/db"
)

// Board real-time + archive card/board + edit comment.
//
// Real-time reuses the same hub (Redis pub/sub) and WebSocket as chat — the same path meeting
// notes already use. A "board" event is published to every user in the tenant after each
// mutation; the frontend, on receiving it, revalidates the board. It's coarse invalidation (not
// incremental): a board changes far less often than a busy channel, so this is enough.

func (s *Server) publishBoard(ctx context.Context, tenantID, boardID uuid.UUID) {
	if s.hub == nil {
		return
	}
	people, err := s.q.ListColleagues(ctx, tenantID)
	if err != nil {
		return
	}
	rcpt := make([]string, 0, len(people))
	for _, p := range people {
		rcpt = append(rcpt, p.ID.String())
	}
	if len(rcpt) == 0 {
		return
	}
	payload, _ := json.Marshal(map[string]string{"board_id": boardID.String()})
	s.hub.Publish(ctx, chat.Event{Type: "board", ChannelID: boardID.String(), Recipients: rcpt, Payload: payload})
}

// handleArchiveCard archives/restores a card. Archived means off the board (but recoverable).
func (s *Server) handleArchiveCard(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "cards.invalid_id")
		return
	}
	if !s.requireCardEditor(w, r, id) {
		return
	}
	var req struct {
		Archived bool `json:"archived"`
	}
	if !decode(w, r, &req) {
		return
	}
	card, err := s.q.SetCardArchived(r.Context(), db.SetCardArchivedParams{ID: id, TenantID: mu.TenantID, Archived: req.Archived})
	if err != nil {
		writeErr(w, http.StatusNotFound, "cards.not_found")
		return
	}
	act := "archived"
	if !req.Archived {
		act = "unarchived"
	}
	s.logCard(r.Context(), mu.TenantID, card.ID, mu.ID, act, "")
	s.publishBoard(r.Context(), mu.TenantID, card.BoardID)
	writeJSON(w, http.StatusOK, card)
}

// handleListArchivedCards lists a board's archived cards (for restoring).
func (s *Server) handleListArchivedCards(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	boardID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	cards, err := s.q.ListArchivedCards(r.Context(), db.ListArchivedCardsParams{BoardID: boardID, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cards.failed")
		return
	}
	if cards == nil {
		cards = []db.Card{}
	}
	writeJSON(w, http.StatusOK, cards)
}

// handleSearchCards searches tasks by key (QUA-12), number, or title — only among the ones the
// person can see. Powers chat's `/task`: referencing a task without leaving the conversation.
func (s *Server) handleSearchCards(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"cards": []db.SearchCardsRow{}})
		return
	}
	cards, err := s.q.SearchCards(r.Context(), db.SearchCardsParams{
		TenantID: mu.TenantID, Q: q, UserID: pgtype.UUID{Bytes: mu.ID, Valid: true},
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cards.failed")
		return
	}
	if cards == nil {
		cards = []db.SearchCardsRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": cards})
}

// handleArchiveBoard archives/restores an entire board.
func (s *Server) handleArchiveBoard(w http.ResponseWriter, r *http.Request) {
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
		Archived bool `json:"archived"`
	}
	if !decode(w, r, &req) {
		return
	}
	board, err := s.q.SetBoardArchived(r.Context(), db.SetBoardArchivedParams{ID: id, TenantID: mu.TenantID, Archived: req.Archived})
	if err != nil {
		writeErr(w, http.StatusNotFound, "boards.not_found")
		return
	}
	writeJSON(w, http.StatusOK, board)
}

// handleListArchivedBoards lists the tenant's archived boards.
func (s *Server) handleListArchivedBoards(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	boards, err := s.q.ListArchivedBoards(r.Context(), mu.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "boards.failed")
		return
	}
	if boards == nil {
		boards = []db.Board{}
	}
	writeJSON(w, http.StatusOK, boards)
}

// handlePatchCardComment edits a comment's body (author only).
func (s *Server) handlePatchCardComment(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	cid, err := uuid.Parse(chi.URLParam(r, "cid"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "comments.invalid_id")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Body == "" {
		writeErr(w, http.StatusBadRequest, "comments.empty")
		return
	}
	c, err := s.q.EditCardComment(r.Context(), db.EditCardCommentParams{ID: cid, AuthorID: mu.ID, Body: req.Body})
	if err != nil {
		writeErr(w, http.StatusNotFound, "comments.not_found") // not the author or doesn't exist
		return
	}
	writeJSON(w, http.StatusOK, c)
}
