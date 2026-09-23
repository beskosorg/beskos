package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Task notifications: who gets notified of what, and why.
//
// Rules (decided with the customer, 2026-07-15):
//   - whoever @mentions someone → the mentioned person is notified;
//   - whoever replies to a comment → the author of the replied-to comment is notified;
//   - whoever commented before on the task → is notified of the following comments (it's their
//     conversation);
//   - whoever CREATED the task → notified when it changes or gets a comment;
//   - whoever IS GOING TO DO the task (assignee) → notified of changes and comments.
//
// Two rules that outrank the list: (1) NEVER notify whoever performed the action — getting an
// e-mail about what you just wrote yourself is noise that teaches people to ignore the rest;
// (2) a person gets ONE notification per event, even if it matches five of the reasons above.

type notifyKind string

const (
	kindTaskComment  notifyKind = "task_comment"
	kindTaskMention  notifyKind = "task_mention"
	kindTaskReply    notifyKind = "task_reply"
	kindTaskAssigned notifyKind = "task_assigned"
	kindTaskChanged  notifyKind = "task_changed"
)

// notifyTarget is a person + the reason they're being notified. The strongest reason wins: being
// mentioned is more relevant than "I also commented here".
type notifyTarget struct {
	userID uuid.UUID
	kind   notifyKind
}

// rank orders the reasons: the higher, the more specific.
func (k notifyKind) rank() int {
	switch k {
	case kindTaskMention:
		return 4
	case kindTaskReply:
		return 3
	case kindTaskAssigned:
		return 2
	default:
		return 1
	}
}

// audience merges the reasons and returns one notification per person, with the strongest
// reason, already excluding the actor.
func audience(actor uuid.UUID, targets []notifyTarget) []notifyTarget {
	best := map[uuid.UUID]notifyKind{}
	for _, t := range targets {
		if t.userID == uuid.Nil || t.userID == actor {
			continue // never notify whoever performed the action
		}
		if cur, ok := best[t.userID]; !ok || t.kind.rank() > cur.rank() {
			best[t.userID] = t.kind
		}
	}
	out := make([]notifyTarget, 0, len(best))
	for id, k := range best {
		out = append(out, notifyTarget{userID: id, kind: k})
	}
	return out
}

// resolveUser only accepts an assignee from the SAME tenant. An invalid or foreign id → no
// assignee, never an e-mail to the wrong user.
func (s *Server) resolveUser(ctx context.Context, mu db.MailUser, raw string) pgtype.UUID {
	if raw == "" {
		return pgtype.UUID{}
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return pgtype.UUID{}
	}
	if _, err := s.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: id, TenantID: mu.TenantID}); err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

// taskWatchers are a task's fixed stakeholders: whoever created it and whoever is going to do it.
func taskWatchers(card db.Card) []notifyTarget {
	var out []notifyTarget
	if card.CreatedBy.Valid {
		out = append(out, notifyTarget{userID: uuid.UUID(card.CreatedBy.Bytes), kind: kindTaskChanged})
	}
	if card.AssigneeID.Valid {
		out = append(out, notifyTarget{userID: uuid.UUID(card.AssigneeID.Bytes), kind: kindTaskAssigned})
	}
	return out
}

// notifyTask records the bell notification and sends e-mail to whoever hasn't turned it off.
// Best-effort on purpose: an e-mail failing can't take down the comment the person just wrote.
func (s *Server) notifyTask(ctx context.Context, actor db.MailUser, card db.Card, targets []notifyTarget, title, body string) {
	// Same address the UI uses and that the "copy link" button produces. With ?card= the board
	// would open but not the task — the e-mail link has to land on the task.
	link := fmt.Sprintf("/mail/kanban/%s/%s", card.BoardID, card.ID)
	who := actor.Name
	if who == "" {
		who = actor.Email
	}
	// Structured data for the frontend to translate via server.notif.<kind> (docs/engineering-standards.md §4.6). The
	// `body` (comment/task name) is user content and stays shown raw as a fallback.
	args := notifArgs(map[string]string{"actor": who, "card": card.Title})
	for _, t := range audience(actor.ID, targets) {
		u, err := s.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: t.userID, TenantID: actor.TenantID})
		if err != nil {
			continue // from another tenant or deleted: doesn't exist for us
		}
		if _, err := s.q.CreateNotification(ctx, db.CreateNotificationParams{
			TenantID: actor.TenantID, UserID: u.ID, Kind: string(t.kind),
			Title: title, Body: body, Link: link, MeetingID: pgtype.UUID{}, Args: args,
		}); err != nil {
			slog.Error("task notification failed", "kind", t.kind, "user", u.ID, "err", err)
		}
		if s.email == nil || !u.NotifyEmail || u.Email == "" {
			continue
		}
		subject, text := emailFor(t.kind, actor, card, title, body)
		if err := s.email.Send(u.Email, subject, text); err != nil {
			// Doesn't fail the request: the bell notification is already there, and the e-mail is extra.
			slog.Error("notification email failed", "to", u.Email, "kind", t.kind, "err", err)
		}
	}
}

// emailFor builds the text. The subject states the REASON — "mentioned you" and "commented" call
// for different attention, and whoever reads the inbox decides from the subject.
func emailFor(k notifyKind, actor db.MailUser, card db.Card, title, body string) (string, string) {
	who := actor.Name
	if who == "" {
		who = actor.Email
	}
	var reason string
	switch k {
	case kindTaskMention:
		reason = who + " mentioned you in a task"
	case kindTaskReply:
		reason = who + " replied to your comment"
	case kindTaskAssigned:
		reason = "Task assigned to you"
	case kindTaskChanged:
		reason = who + " changed one of your tasks"
	default:
		reason = who + " commented on a task"
	}
	text := strings.TrimSpace(fmt.Sprintf("%s\n\nTask: %s\n\n%s\n\n---\n%s",
		reason, card.Title, body, "Beskos"))
	return fmt.Sprintf("[%s] %s", card.Title, title), text
}

// parseMentions extracts @mentions from the text and returns the ids of the COLLEAGUES that
// match. It matches by e-mail (@ana@acme.com) or by local-part (@ana) — never by name, which
// repeats and changes.
func (s *Server) parseMentions(ctx context.Context, tenantID uuid.UUID, body string) []uuid.UUID {
	raw := mentionRe.FindAllStringSubmatch(body, -1)
	if len(raw) == 0 {
		return nil
	}
	people, err := s.q.ListColleagues(ctx, tenantID)
	if err != nil {
		return nil
	}
	var out []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, m := range raw {
		needle := strings.ToLower(m[1])
		for _, p := range people {
			local := strings.ToLower(strings.SplitN(p.Email, "@", 2)[0])
			if strings.ToLower(p.Email) == needle || local == needle {
				if !seen[p.ID] {
					seen[p.ID] = true
					out = append(out, p.ID)
				}
			}
		}
	}
	return out
}

// @ana or @ana@acme.com — the local-part can have a dot, hyphen, and underscore.
var mentionRe = regexp.MustCompile(`@([A-Za-z0-9._%+-]+(?:@[A-Za-z0-9.-]+\.[A-Za-z]{2,})?)`)
