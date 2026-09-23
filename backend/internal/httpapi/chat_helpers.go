package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/beskosorg/beskos/backend/internal/chat"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// Slack-style chat. REST for data + WebSocket (Redis hub) for real time. Everything filters by tenant.

// ---- DTOs (flatten pgtype into clean JSON for the frontend to consume) ----

type reactionDTO struct {
	Emoji string   `json:"emoji"`
	Count int      `json:"count"`
	Mine  bool     `json:"mine"`
	Users []string `json:"users"`
}

type attachmentDTO struct {
	FileID string `json:"file_id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
}

type messageDTO struct {
	ID          string          `json:"id"`
	ChannelID   string          `json:"channel_id"`
	UserID      string          `json:"user_id"`
	Parent      string          `json:"parent_id"`
	Body        string          `json:"body"`
	Author      string          `json:"author"`
	AuthorEmail string          `json:"author_email"`
	Deleted     bool            `json:"deleted"`
	EditedAt    string          `json:"edited_at"`
	CreatedAt   string          `json:"created_at"`
	BotName     string          `json:"bot_name"`
	ReplyCount  int64           `json:"reply_count"`
	Reactions   []reactionDTO   `json:"reactions"`
	Attachments []attachmentDTO `json:"attachments"`
	Saved       bool            `json:"saved"`
}

// markSaved sets the Saved field on messages based on what the user saved in that channel.
func (s *Server) markSaved(ctx context.Context, userID, channelID uuid.UUID, msgs []messageDTO) []messageDTO {
	ids, _ := s.q.ListSavedIDsInChannel(ctx, db.ListSavedIDsInChannelParams{UserID: userID, ChannelID: channelID})
	if len(ids) == 0 {
		return msgs
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id.String()] = true
	}
	for i := range msgs {
		if set[msgs[i].ID] {
			msgs[i].Saved = true
		}
	}
	return msgs
}

// rawMsg is the common intermediate representation between the channel list and the thread reply list.
type rawMsg struct {
	ID, ChannelID, UserID   uuid.UUID
	ParentID                pgtype.UUID
	Body                    string
	EditedAt, DeletedAt     pgtype.Timestamptz
	CreatedAt               time.Time
	AuthorEmail, AuthorName string
	ReplyCount              int64
	BotName                 string
}

// RFC3339Nano (not RFC3339): preserves sub-second precision. This is the timestamp the pagination
// uses as a cursor (?before=) — at second resolution, messages from the same second tie and the
// previous page comes back empty.
func iso(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// pgUUID wraps a uuid for pgx's nullable fields.
func pgUUID(u uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: u, Valid: true} }

func uuidStr(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	return uuid.UUID(u.Bytes).String()
}

// ---- authorization / recipients helpers ----

// memberOr403 ensures the user is a member of the channel; returns the membership.
func (s *Server) memberOr403(w http.ResponseWriter, r *http.Request, channelID uuid.UUID) (db.ChannelMember, bool) {
	mu := mailUserFrom(r.Context())
	m, err := s.q.GetMembership(r.Context(), db.GetMembershipParams{ChannelID: channelID, UserID: mu.ID})
	if err != nil {
		writeErr(w, http.StatusForbidden, "chat.not_member")
		return db.ChannelMember{}, false
	}
	return m, true
}

func (s *Server) channelRecipients(ctx context.Context, channelID uuid.UUID) []string {
	ids, _ := s.q.ListMemberUserIDs(ctx, channelID)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func (s *Server) publish(ctx context.Context, evType string, channelID uuid.UUID, recipients []string, payload any) {
	if s.hub == nil {
		return
	}
	raw, _ := json.Marshal(payload)
	s.hub.Publish(ctx, chat.Event{Type: evType, ChannelID: channelID.String(), Recipients: recipients, Payload: raw})
}

// ---- Channels ----

// generalChannelName is the STORED name of the default channel. It stays in English (law §4.6):
// the frontend shows the translation as long as the name is untouched — if someone renames it,
// the chosen name wins.
const generalChannelName = "General"

// ensureGeneral ensures the tenant has the default channel and that whoever is requesting the
// list is a member of it. Doing this at listing time (and not only at provisioning) also covers
// tenants and users that already existed. It's two indexed reads in the normal case, no write.
func (s *Server) ensureGeneral(ctx context.Context, tenant, user uuid.UUID) {
	ch, err := s.q.GetGeneralChannel(ctx, tenant)
	if err != nil {
		created, cerr := s.q.CreateGeneralChannel(ctx, db.CreateGeneralChannelParams{
			TenantID: tenant, Name: generalChannelName, CreatedBy: user,
		})
		if cerr != nil {
			// Race with another request: the partial index let only one through. Read what's there.
			if ch, err = s.q.GetGeneralChannel(ctx, tenant); err != nil {
				log.Printf("chat: ensure general channel: %v", cerr)
				return
			}
		} else {
			ch = created
		}
	}
	if _, err := s.q.GetMembership(ctx, db.GetMembershipParams{ChannelID: ch.ID, UserID: user}); err == nil {
		return // already a member
	}
	if err := s.q.AddChannelMember(ctx, db.AddChannelMemberParams{
		ChannelID: ch.ID, TenantID: tenant, UserID: user, Role: "member",
	}); err != nil {
		log.Printf("chat: join general channel: %v", err)
	}
}

func (s *Server) dmDisplayName(ctx context.Context, channelID, me uuid.UUID) string {
	name, _ := s.dmOther(ctx, channelID, me)
	return name
}

// dmOther returns the display name and id of the "other" party in a DM. In a group DM (3+), it
// joins the other names ("Bob, Carol") and doesn't return a presence id (there's no single other).
func (s *Server) dmOther(ctx context.Context, channelID, me uuid.UUID) (string, string) {
	members, _ := s.q.ListChannelMembers(ctx, channelID)
	names := make([]string, 0, len(members))
	otherID := ""
	for _, m := range members {
		if m.UserID == me {
			continue
		}
		n := m.Name
		if n == "" {
			n = m.Email
		}
		names = append(names, n)
		otherID = m.UserID.String()
	}
	switch len(names) {
	case 0:
		return "DM", ""
	case 1:
		return names[0], otherID
	default:
		return strings.Join(names, ", "), ""
	}
}

// dmKeyN is the canonical key of a DM (pair or group): sorted ids joined together. The same set of
// people always resolves to the same key → reuses the channel instead of duplicating it.
func dmKeyN(ids []uuid.UUID) string {
	ss := make([]string, len(ids))
	for i, id := range ids {
		ss[i] = id.String()
	}
	sort.Strings(ss)
	return strings.Join(ss, ":")
}

// managerOr403 ensures the user is a manager of the channel (owner or manager) — the permission
// OF the channel, independent of the organization role. Returns the membership.
func (s *Server) managerOr403(w http.ResponseWriter, r *http.Request, channelID uuid.UUID) (db.ChannelMember, bool) {
	m, ok := s.memberOr403(w, r, channelID)
	if !ok {
		return db.ChannelMember{}, false
	}
	if m.Role != "owner" && m.Role != "manager" {
		writeErr(w, http.StatusForbidden, "chat.not_manager")
		return db.ChannelMember{}, false
	}
	return m, true
}

func dmKey(a, b uuid.UUID) string {
	ids := []string{a.String(), b.String()}
	sort.Strings(ids)
	return strings.Join(ids, ":")
}

// buildMessages transforms rows + reactions + attachments into DTOs in chronological order.
// reverse=true when rows come in desc order (channel list); false when they already come asc (thread).
func buildMessages(rows []rawMsg, reactions []db.ListReactionsByChannelRow, atts []db.ListAttachmentsByChannelRow, me uuid.UUID, reverse bool) []messageDTO {
	type key struct{ msg, emoji string }
	grouped := map[key]*reactionDTO{}
	order := map[string][]string{}
	for _, r := range reactions {
		mid := r.MessageID.String()
		k := key{mid, r.Emoji}
		rd := grouped[k]
		if rd == nil {
			rd = &reactionDTO{Emoji: r.Emoji}
			grouped[k] = rd
			order[mid] = append(order[mid], r.Emoji)
		}
		rd.Count++
		rd.Users = append(rd.Users, r.UserID.String())
		if r.UserID == me {
			rd.Mine = true
		}
	}
	attByMsg := map[string][]attachmentDTO{}
	for _, a := range atts {
		mid := a.MessageID.String()
		attByMsg[mid] = append(attByMsg[mid], attachmentDTO{FileID: a.FileID.String(), Name: a.Name, Size: a.Size})
	}
	toDTO := func(m rawMsg) messageDTO {
		mid := m.ID.String()
		var rs []reactionDTO
		for _, emoji := range order[mid] {
			rs = append(rs, *grouped[key{mid, emoji}])
		}
		author := m.AuthorName
		if author == "" {
			author = m.AuthorEmail
		}
		return messageDTO{
			ID: mid, ChannelID: m.ChannelID.String(), UserID: m.UserID.String(),
			Parent: uuidStr(m.ParentID), Body: m.Body, Author: author, AuthorEmail: m.AuthorEmail,
			Deleted: m.DeletedAt.Valid, EditedAt: tsStr(m.EditedAt), CreatedAt: iso(m.CreatedAt),
			ReplyCount: m.ReplyCount, Reactions: rs, Attachments: attByMsg[mid], BotName: m.BotName,
		}
	}
	out := make([]messageDTO, 0, len(rows))
	if reverse {
		for i := len(rows) - 1; i >= 0; i-- {
			out = append(out, toDTO(rows[i]))
		}
	} else {
		for i := range rows {
			out = append(out, toDTO(rows[i]))
		}
	}
	return out
}

func tsStr(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return iso(t.Time)
}

// hasMentionToken reports whether the body contains @channel/@here as a token (preceded by
// start/whitespace and followed by end/whitespace/punctuation) — avoids matching "@channel.com" or
// "email@here.io".
func hasMentionToken(body, tok string) bool {
	at := "@" + tok
	low := strings.ToLower(body)
	for i := 0; ; {
		j := strings.Index(low[i:], at)
		if j < 0 {
			return false
		}
		p := i + j
		startOK := p == 0 || low[p-1] == ' ' || low[p-1] == '\n' || low[p-1] == '\t'
		end := p + len(at)
		endOK := end == len(low) || strings.ContainsRune(" \n\t.,!?;:", rune(low[end]))
		if startOK && endOK {
			return true
		}
		i = p + 1
	}
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// handleDeleteChannel deletes a channel — OWNER only. The cascade removes members and messages.
// canArchiveChannel: the channel OWNER or an organization admin/owner. The organization admin might
// not be a member of the (private) channel — hence the elevation check comes before the membership one.
func (s *Server) canArchiveChannel(r *http.Request, channelID uuid.UUID) bool {
	mu := mailUserFrom(r.Context())
	if mu.Role == "owner" || mu.Role == "admin" || mu.Role == "internal_admin" {
		return true
	}
	m, err := s.q.GetMembership(r.Context(), db.GetMembershipParams{ChannelID: channelID, UserID: mu.ID})
	return err == nil && m.Role == "owner"
}

// ---- Message search ----

// resolveColleague finds a tenant user by email/name (exact match, then contains). uuid.Nil if not found.
func (s *Server) resolveColleague(ctx context.Context, tenant uuid.UUID, who string) uuid.UUID {
	who = strings.ToLower(strings.TrimSpace(who))
	if who == "" {
		return uuid.Nil
	}
	users, _ := s.q.ListMailUsersByTenant(ctx, tenant)
	for _, u := range users {
		if strings.ToLower(u.Email) == who || strings.ToLower(u.Name) == who {
			return u.ID
		}
	}
	for _, u := range users {
		if strings.Contains(strings.ToLower(u.Name), who) || strings.Contains(strings.ToLower(u.Email), who) {
			return u.ID
		}
	}
	return uuid.Nil
}
