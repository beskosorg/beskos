package httpapi

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Kanban (our layer). Boards are shared per tenant. Every query filters by tenant.

var defaultColumns = []struct {
	Name, Color string
}{
	{"A fazer", "#6B6863"}, {"Em andamento", "#3B6FB0"},
	{"Aguardando", "#C0692F"}, {"Concluído", "#2E7D5B"},
}

// seedDefaultColumns creates the default columns of a newly created board.
func (s *Server) seedDefaultColumns(ctx context.Context, board db.Board, tenantID uuid.UUID) error {
	for i, c := range defaultColumns {
		if _, err := s.q.CreateColumn(ctx, db.CreateColumnParams{
			BoardID: board.ID, TenantID: tenantID, Name: c.Name, Color: c.Color, Position: int32(i),
		}); err != nil {
			return err
		}
	}
	return nil
}

// ensureDefaultBoard creates an initial board with columns if the tenant doesn't have any yet.
func (s *Server) ensureDefaultBoard(ctx context.Context, tenantID, userID uuid.UUID) (db.Board, error) {
	boards, err := s.q.ListBoards(ctx, db.ListBoardsParams{TenantID: tenantID, UserID: pgtype.UUID{Bytes: userID, Valid: true}})
	if err != nil {
		return db.Board{}, err
	}
	if len(boards) > 0 {
		return boards[0], nil
	}
	board, err := s.createBoard(ctx, db.CreateBoardParams{TenantID: tenantID, Name: "Quadro"}) // no owner = legacy board, visible to the whole org
	if err != nil {
		return db.Board{}, err
	}
	if err := s.seedDefaultColumns(ctx, board, tenantID); err != nil {
		return db.Board{}, err
	}
	return board, nil
}

// handleCreateBoard creates a new board already with the default columns.
func (s *Server) handleCreateBoard(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "boards.invalid")
		return
	}
	board, err := s.createBoard(r.Context(), db.CreateBoardParams{
		TenantID: mu.TenantID, Name: name, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true},
	})
	// Collaborative by default: the new board is born visible/editable by the whole org (a grant
	// the manager can remove to restrict it). Without this, only the owner would see it.
	if err == nil {
		_, _ = s.q.InsertBoardGrant(r.Context(), db.InsertBoardGrantParams{
			TenantID: mu.TenantID, BoardID: board.ID, SubjectType: "org", Role: "editor",
		})
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "boards.create_failed")
		return
	}
	if err := s.seedDefaultColumns(r.Context(), board, mu.TenantID); err != nil {
		writeErr(w, http.StatusInternalServerError, "boards.create_failed")
		return
	}
	writeJSON(w, http.StatusCreated, board)
}

func (s *Server) handleUpdateBoard(w http.ResponseWriter, r *http.Request) {
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
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "boards.invalid")
		return
	}
	board, err := s.q.UpdateBoard(r.Context(), db.UpdateBoardParams{ID: id, TenantID: mu.TenantID, Name: strings.TrimSpace(req.Name)})
	if err != nil {
		writeErr(w, http.StatusNotFound, "boards.not_found")
		return
	}
	writeJSON(w, http.StatusOK, board)
}

// handleDeleteBoard deletes a board (columns and cards cascade away). It never leaves the tenant
// with no board at all — the last one can't be deleted.
func (s *Server) handleDeleteBoard(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if !s.requireBoardRole(w, r, id, boardRoleManager) {
		return
	}
	n, err := s.q.CountBoards(r.Context(), mu.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "boards.failed")
		return
	}
	if n <= 1 {
		writeErr(w, http.StatusBadRequest, "boards.last_board")
		return
	}
	if err := s.q.DeleteBoard(r.Context(), db.DeleteBoardParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "boards.delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleListBoards(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	if _, err := s.ensureDefaultBoard(r.Context(), mu.TenantID, mu.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "boards.failed")
		return
	}
	boards, err := s.q.ListBoards(r.Context(), db.ListBoardsParams{TenantID: mu.TenantID, UserID: pgtype.UUID{Bytes: mu.ID, Valid: true}})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "boards.failed")
		return
	}
	writeJSON(w, http.StatusOK, boards)
}

// handleGetBoard returns the board with its columns and cards grouped by column.
func (s *Server) handleGetBoard(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if !s.requireBoardRole(w, r, id, boardRoleViewer) {
		return
	}
	board, err := s.q.GetBoard(r.Context(), db.GetBoardParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "boards.not_found")
		return
	}
	cols, _ := s.q.ListColumns(r.Context(), board.ID)
	cards, _ := s.q.ListCardsByBoard(r.Context(), board.ID)
	byCol := map[uuid.UUID][]db.ListCardsByBoardRow{}
	for _, c := range cards {
		byCol[c.ColumnID] = append(byCol[c.ColumnID], c)
	}
	out := make([]map[string]any, len(cols))
	for i, col := range cols {
		cs := byCol[col.ID]
		if cs == nil {
			cs = []db.ListCardsByBoardRow{} // never null in JSON (the frontend does cards.length)
		}
		out[i] = map[string]any{"column": col, "cards": cs}
	}
	// Additional assignees + dependencies, grouped by card (the frontend indexes by card_id).
	assignees, _ := s.q.ListBoardAssignees(r.Context(), board.ID)
	if assignees == nil {
		assignees = []db.ListBoardAssigneesRow{}
	}
	deps, _ := s.q.ListBoardDeps(r.Context(), board.ID)
	if deps == nil {
		deps = []db.ListBoardDepsRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"board": board, "columns": out, "assignees": assignees, "deps": deps})
}

type createCardReq struct {
	ColumnID    string `json:"column_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Label       string `json:"label"`
	AssigneeID  string `json:"assignee_id"`
	Due         string `json:"due"`
	Priority    string `json:"priority"`
	SourceEmail string `json:"source_email_id"`
	ContactID   string `json:"contact_id"`
	StartDate   string `json:"start_date"`
}

// normalizePriority: accepts only the fixed scale; anything else (including empty) becomes
// 'normal'. Keeps the database CHECK always satisfied without relying on the client.
func normalizePriority(p string) string {
	switch p {
	case "low", "high", "urgent":
		return p
	default:
		return "normal"
	}
}

// contactOrZero: absent = keeps the link; "" = clears it.
func contactOrZero(s *Server, r *http.Request, mu db.MailUser, raw *string) pgtype.UUID {
	if raw == nil {
		return pgtype.UUID{}
	}
	return s.resolveContact(r.Context(), mu, *raw)
}

// resolveContact validates the task/ticket → contact link: it only accepts a contact the user
// CAN SEE (GetContact applies group-based visibility). An invalid or invisible id → no link,
// never a silent leak.
func (s *Server) resolveContact(ctx context.Context, mu db.MailUser, raw string) pgtype.UUID {
	if raw == "" {
		return pgtype.UUID{}
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return pgtype.UUID{}
	}
	if _, err := s.q.GetContact(ctx, db.GetContactParams{ID: id, OwnerUserID: mu.ID, TenantID: mu.TenantID}); err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func (s *Server) handleCreateCard(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	boardID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if !s.requireBoardRole(w, r, boardID, boardRoleEditor) {
		return
	}
	if _, err := s.q.GetBoard(r.Context(), db.GetBoardParams{ID: boardID, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "boards.not_found")
		return
	}
	var req createCardReq
	if !decode(w, r, &req) {
		return
	}
	colID, err := uuid.Parse(req.ColumnID)
	if err != nil || req.Title == "" {
		writeErr(w, http.StatusBadRequest, "cards.invalid")
		return
	}
	// Number per board (QUA-12): the board's sequence is the source of truth, never MAX+1.
	seq, err := s.q.NextCardNumber(r.Context(), boardID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cards.create_failed")
		return
	}
	card, err := s.q.CreateCard(r.Context(), db.CreateCardParams{
		TenantID: mu.TenantID, BoardID: boardID, ColumnID: colID, Title: req.Title,
		Description: req.Description, Label: req.Label,
		Due: req.Due, Priority: normalizePriority(req.Priority), SourceEmailID: req.SourceEmail, Position: 0,
		AssigneeID: s.resolveUser(r.Context(), mu, req.AssigneeID),
		CreatedBy:  pgtype.UUID{Bytes: mu.ID, Valid: true},
		ContactID:  s.resolveContact(r.Context(), mu, req.ContactID),
		Number:     seq.CardSeq, StartDate: req.StartDate,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cards.create_failed")
		return
	}
	// Notifies whoever is going to do the task (never the creator — that was them already).
	s.notifyTask(r.Context(), mu, card, taskWatchers(card), "New task", card.Title)
	s.logCard(r.Context(), mu.TenantID, card.ID, mu.ID, "created", "")
	s.publishBoard(r.Context(), mu.TenantID, card.BoardID)
	writeJSON(w, http.StatusCreated, card)
}

type moveCardReq struct {
	ColumnID string `json:"column_id"`
	Position int32  `json:"position"`
}

func (s *Server) handleMoveCard(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "cards.invalid_id")
		return
	}
	if !s.requireCardEditor(w, r, id) {
		return
	}
	var req moveCardReq
	if !decode(w, r, &req) {
		return
	}
	colID, err := uuid.Parse(req.ColumnID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "cards.invalid")
		return
	}
	// Changing column? Used for the WIP limit and to fire automations only on ENTERING the column.
	cur, _ := s.q.GetCard(r.Context(), db.GetCardParams{ID: id, TenantID: mu.TenantID})
	entering := cur.ColumnID != colID
	if entering {
		if col, cerr := s.q.GetColumn(r.Context(), db.GetColumnParams{ID: colID, TenantID: mu.TenantID}); cerr == nil && col.WipLimit > 0 {
			if n, _ := s.q.CountCardsInColumn(r.Context(), colID); n >= int64(col.WipLimit) {
				writeErr(w, http.StatusConflict, "cards.wip_full")
				return
			}
		}
	}
	card, err := s.q.MoveCard(r.Context(), db.MoveCardParams{
		ID: id, TenantID: mu.TenantID, ColumnID: colID, Position: req.Position,
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, "cards.not_found")
		return
	}
	if entering {
		s.applyAutomations(r.Context(), mu.TenantID, colID, card.ID)
	}
	s.logCard(r.Context(), mu.TenantID, card.ID, mu.ID, "moved", req.ColumnID)
	s.publishBoard(r.Context(), mu.TenantID, card.BoardID)
	writeJSON(w, http.StatusOK, card)
}

type reorderColumnReq struct {
	CardIDs []string `json:"card_ids"` // final order of the column's cards (includes ones that came from elsewhere)
}

// handleReorderColumn sets the order (and column) of the cards from the received array — it
// persists the vertical drag-and-drop position, which previously always saved 0.
func (s *Server) handleReorderColumn(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	colID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "cards.invalid")
		return
	}
	var req reorderColumnReq
	if !decode(w, r, &req) {
		return
	}
	ids := make([]uuid.UUID, 0, len(req.CardIDs))
	for _, s := range req.CardIDs {
		if id, e := uuid.Parse(s); e == nil {
			ids = append(ids, id)
		}
	}
	// WIP limit: the final list can't exceed the column's limit (drag-and-drop goes through here).
	if col, cerr := s.q.GetColumn(r.Context(), db.GetColumnParams{ID: colID, TenantID: mu.TenantID}); cerr == nil && col.WipLimit > 0 && len(ids) > int(col.WipLimit) {
		writeErr(w, http.StatusConflict, "cards.wip_full")
		return
	}
	// Cards ENTERING the column now (current column != destination) → fire automations after reordering.
	var entered []uuid.UUID
	for _, cid := range ids {
		if c, e := s.q.GetCard(r.Context(), db.GetCardParams{ID: cid, TenantID: mu.TenantID}); e == nil && c.ColumnID != colID {
			entered = append(entered, cid)
		}
	}
	if err := s.q.ReorderColumn(r.Context(), db.ReorderColumnParams{
		TenantID: mu.TenantID, ColumnID: colID, CardIds: ids,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "cards.reorder_failed")
		return
	}
	for _, cid := range entered {
		s.applyAutomations(r.Context(), mu.TenantID, colID, cid)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type reorderColumnsReq struct {
	ColumnIDs []string `json:"column_ids"`
}

// handleReorderColumns applies the board's new horizontal column order (column drag-and-drop).
func (s *Server) handleReorderColumns(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	boardID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if _, err := s.q.GetBoard(r.Context(), db.GetBoardParams{ID: boardID, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "boards.not_found")
		return
	}
	var req reorderColumnsReq
	if !decode(w, r, &req) {
		return
	}
	ids := make([]uuid.UUID, 0, len(req.ColumnIDs))
	for _, s := range req.ColumnIDs {
		if id, e := uuid.Parse(s); e == nil {
			ids = append(ids, id)
		}
	}
	if err := s.q.ReorderColumns(r.Context(), db.ReorderColumnsParams{
		TenantID: mu.TenantID, BoardID: boardID, ColumnIds: ids,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "columns.reorder_failed")
		return
	}
	s.publishBoard(r.Context(), mu.TenantID, boardID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteCard(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "cards.invalid_id")
		return
	}
	if !s.requireCardEditor(w, r, id) {
		return
	}
	// Captures the board before deleting, to notify the board in real time.
	card, _ := s.q.GetCard(r.Context(), db.GetCardParams{ID: id, TenantID: mu.TenantID})
	if err := s.q.DeleteCard(r.Context(), db.DeleteCardParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "cards.delete_failed")
		return
	}
	if card.BoardID != uuid.Nil {
		s.publishBoard(r.Context(), mu.TenantID, card.BoardID)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type updateCardReq struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Label       string `json:"label"`
	AssigneeID  string `json:"assignee_id"`
	Due         string `json:"due"`
	Priority    string `json:"priority"`
	StartDate   string `json:"start_date"`
	Color       string `json:"color"` // card tint ('' = default); contrast guaranteed in the frontend
	// Pointer on purpose: absent = keeps the contact linked; "" = clears it.
	ContactID *string `json:"contact_id"`
}

// handleUpdateCard edits a card's fields (title, description, label, assignee, due date).
func (s *Server) handleUpdateCard(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "cards.invalid_id")
		return
	}
	if !s.requireCardEditor(w, r, id) {
		return
	}
	var req updateCardReq
	if !decode(w, r, &req) {
		return
	}
	if req.Title == "" {
		writeErr(w, http.StatusBadRequest, "cards.invalid")
		return
	}
	card, err := s.q.UpdateCard(r.Context(), db.UpdateCardParams{
		ID: id, TenantID: mu.TenantID, Title: req.Title, Description: req.Description,
		Label: req.Label, Due: req.Due, Priority: normalizePriority(req.Priority),
		AssigneeID: s.resolveUser(r.Context(), mu, req.AssigneeID), StartDate: req.StartDate, Color: req.Color,
		SetContact: req.ContactID != nil,
		ContactID:  contactOrZero(s, r, mu, req.ContactID),
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, "cards.not_found")
		return
	}
	s.logCard(r.Context(), mu.TenantID, card.ID, mu.ID, "updated", "")
	s.publishBoard(r.Context(), mu.TenantID, card.BoardID)
	writeJSON(w, http.StatusOK, card)
}

type columnReq struct {
	Name     string `json:"name"`
	Color    string `json:"color"`
	WipLimit int32  `json:"wip_limit"`
}

// handleCreateColumn creates a column at the end of the board (position = current column count).
func (s *Server) handleCreateColumn(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	boardID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "boards.invalid_id")
		return
	}
	if !s.requireBoardRole(w, r, boardID, boardRoleEditor) {
		return
	}
	if _, err := s.q.GetBoard(r.Context(), db.GetBoardParams{ID: boardID, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "boards.not_found")
		return
	}
	var req columnReq
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "columns.invalid")
		return
	}
	if req.Color == "" {
		req.Color = "#6B6863"
	}
	existing, _ := s.q.ListColumns(r.Context(), boardID)
	col, err := s.q.CreateColumn(r.Context(), db.CreateColumnParams{
		BoardID: boardID, TenantID: mu.TenantID, Name: req.Name, Color: req.Color, Position: int32(len(existing)),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "columns.create_failed")
		return
	}
	writeJSON(w, http.StatusCreated, col)
}

// handleUpdateColumn renames / changes the color of a column.
func (s *Server) handleUpdateColumn(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "columns.invalid_id")
		return
	}
	var req columnReq
	if !decode(w, r, &req) {
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "columns.invalid")
		return
	}
	if req.Color == "" {
		req.Color = "#6B6863"
	}
	col, err := s.q.UpdateColumn(r.Context(), db.UpdateColumnParams{WipLimit: req.WipLimit,
		ID: id, TenantID: mu.TenantID, Name: req.Name, Color: req.Color,
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, "columns.not_found")
		return
	}
	writeJSON(w, http.StatusOK, col)
}

// handleDeleteColumn deletes a column (its cards cascade away via ON DELETE CASCADE).
func (s *Server) handleDeleteColumn(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "columns.invalid_id")
		return
	}
	if err := s.q.DeleteColumn(r.Context(), db.DeleteColumnParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "columns.delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

var nonAlnum = regexp.MustCompile(`[^A-Z0-9]`)
