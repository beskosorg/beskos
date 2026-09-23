package httpapi

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Preview for links that point back at Beskos itself -- today, a task.
//
// WHY NOT THE NORMAL PATH: the external unfurl fetches the page and reads its Open Graph tags. Our
// app is a single-page bundle; every task URL serves the same HTML, with no title and no board.
// Fetching it would spend a round trip to learn nothing. The data is one query away instead.
//
// PERMISSION IS THE WHOLE POINT. A task title is not public inside the tenant: boards carry grants.
// The preview is resolved per viewer and returns nothing at all when the viewer has no access --
// not an error, which would confirm the task exists to someone who should not know that.

// /mail/kanban/<board>/<card> -- the shape the app links to, and the one the task notifications use.
var taskPath = regexp.MustCompile(`^/mail/kanban/([0-9a-fA-F-]{36})/([0-9a-fA-F-]{36})/?$`)

// /mail/notes/<note> -- a note pasted into a conversation.
var notePath = regexp.MustCompile(`^/mail/notes/([0-9a-fA-F-]{36})/?$`)

// ownHost reports whether the URL points at this deployment, so it is resolved from the database
// instead of fetched over the network.
func (s *Server) ownHost(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return false
	}
	for _, origin := range append([]string{s.publicURL}, s.corsOrigins...) {
		if origin == "" {
			continue
		}
		if o, err := url.Parse(origin); err == nil && strings.ToLower(o.Hostname()) == host {
			return true
		}
	}
	return false
}

// internalUnfurl resolves a Beskos link for the viewer in ctx. The second result is false whenever
// there is nothing to show -- unknown shape, missing row, or no permission -- and the caller must
// treat all three the same, so the answer never reveals which one it was.
func (s *Server) internalUnfurl(ctx context.Context, u *url.URL) (unfurlData, bool) {
	if m := notePath.FindStringSubmatch(u.Path); m != nil {
		return s.noteUnfurl(ctx, u, m[1])
	}
	m := taskPath.FindStringSubmatch(u.Path)
	if m == nil {
		return unfurlData{}, false
	}
	boardID, err1 := uuid.Parse(m[1])
	cardID, err2 := uuid.Parse(m[2])
	if err1 != nil || err2 != nil {
		return unfurlData{}, false
	}

	mu := mailUserFrom(ctx)
	if mu.ID == uuid.Nil {
		return unfurlData{}, false
	}
	// Viewer's own permission on the board, the same one the Kanban itself enforces.
	if s.boardRole(ctx, mu, boardID) <= 0 {
		return unfurlData{}, false
	}

	card, err := s.q.GetCard(ctx, db.GetCardParams{ID: cardID, TenantID: mu.TenantID})
	if err != nil {
		return unfurlData{}, false
	}
	// The card has to belong to the board we just authorised. Without this, a link pairing a board
	// the viewer can see with a card from a board they cannot would leak that card's title -- the
	// permission check would pass on the first id and the lookup would answer about the second.
	if card.BoardID != boardID {
		return unfurlData{}, false
	}

	board, err := s.q.GetBoard(ctx, db.GetBoardParams{ID: boardID, TenantID: mu.TenantID})
	if err != nil {
		return unfurlData{}, false
	}

	return unfurlData{
		Kind:        "task",
		URL:         u.String(),
		Title:       card.Title,
		Description: taskSummary(card),
		Site:        board.Name,
	}, true
}

// taskSummary is the one line under the title: who it is on and when it is due. Both are optional,
// and an empty line is better than a line of labels with nothing after them.
func taskSummary(card db.Card) string {
	var parts []string
	if a := strings.TrimSpace(card.Assignee); a != "" {
		parts = append(parts, a)
	}
	if d := strings.TrimSpace(card.Due); d != "" {
		parts = append(parts, d)
	}
	if l := strings.TrimSpace(card.Label); l != "" {
		parts = append(parts, l)
	}
	if len(parts) == 0 {
		// Falls back to the description, trimmed: a task with no assignee and no date still has to
		// say something, or the card is a title floating on its own.
		return firstLine(card.Description, 140)
	}
	return strings.Join(parts, " · ")
}

func firstLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > max {
		return strings.TrimSpace(s[:max]) + "…"
	}
	return s
}

// noteUnfurl previews a note pasted into a conversation.
//
// A note is private until it is shared, so the preview is resolved FOR THE READER: pasting a link
// into a channel does not hand the note to the channel. Everyone who may already see it gets the
// card; everyone else gets a plain link and no hint that there is anything behind it. That is the
// point -- an unfurl that leaked the title would turn "share the link" into "share the note".
func (s *Server) noteUnfurl(ctx context.Context, u *url.URL, raw string) (unfurlData, bool) {
	noteID, err := uuid.Parse(raw)
	if err != nil {
		return unfurlData{}, false
	}
	mu := mailUserFrom(ctx)
	if mu.ID == uuid.Nil {
		return unfurlData{}, false
	}
	rank, err := s.q.NoteRole(ctx, db.NoteRoleParams{UserID: mu.ID, NoteID: noteID, TenantID: mu.TenantID})
	if err != nil || rank <= 0 {
		return unfurlData{}, false
	}
	n, err := s.q.GetNote(ctx, db.GetNoteParams{UserID: mu.ID, NoteID: noteID, TenantID: mu.TenantID})
	if err != nil {
		return unfurlData{}, false
	}
	// A note need not have a title, and a card showing only its author says nothing about what was
	// shared. Falls back to the meeting's name, then to the first line of the note itself -- which is
	// what someone would have written as a title anyway. Empty stays empty and the client names it.
	title := strings.TrimSpace(n.Title)
	if title == "" && n.MeetingTitle != nil {
		title = strings.TrimSpace(*n.MeetingTitle)
	}
	if title == "" {
		title = firstLine(n.BodyText, 80)
	}
	site := ""
	if n.OwnerName != nil {
		site = *n.OwnerName
	}
	return unfurlData{
		Kind:        "note",
		URL:         u.String(),
		Title:       title,
		Description: firstLine(n.BodyText, 160),
		Site:        site,
	}, true
}
