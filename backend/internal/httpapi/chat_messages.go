package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// ---- Messages ----

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	// Jump to a message: ?around=<id> returns a WINDOW around it (older + newer), so search/pin
	// opens already in context. Chronological order; has_more = there are older ones before the window.
	if around := r.URL.Query().Get("around"); around != "" {
		aid, perr := uuid.Parse(around)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, "chat.invalid")
			return
		}
		pivot, perr := s.q.GetMessage(r.Context(), db.GetMessageParams{ID: aid, TenantID: mu.TenantID})
		if perr != nil || pivot.ChannelID != id {
			writeErr(w, http.StatusNotFound, "chat.not_found")
			return
		}
		const half = 25
		older, _ := s.q.ListMessagesBeforeByChannel(r.Context(), db.ListMessagesBeforeByChannelParams{ChannelID: id, CreatedAt: pivot.CreatedAt.Add(time.Microsecond), Limit: half}) // includes the pivot, DESC
		newer, _ := s.q.ListMessagesAfterByChannel(r.Context(), db.ListMessagesAfterByChannelParams{ChannelID: id, CreatedAt: pivot.CreatedAt, Limit: half})                         // ASC
		raw := make([]rawMsg, 0, len(older)+len(newer))
		for i := len(older) - 1; i >= 0; i-- { // DESC → ASC (oldest first)
			m := older[i]
			raw = append(raw, rawMsg{ID: m.ID, ChannelID: m.ChannelID, UserID: m.UserID, ParentID: m.ParentID, Body: m.Body, EditedAt: m.EditedAt, DeletedAt: m.DeletedAt, CreatedAt: m.CreatedAt, AuthorEmail: m.AuthorEmail, AuthorName: m.AuthorName, ReplyCount: m.ReplyCount, BotName: m.BotName})
		}
		for _, m := range newer {
			raw = append(raw, rawMsg{ID: m.ID, ChannelID: m.ChannelID, UserID: m.UserID, ParentID: m.ParentID, Body: m.Body, EditedAt: m.EditedAt, DeletedAt: m.DeletedAt, CreatedAt: m.CreatedAt, AuthorEmail: m.AuthorEmail, AuthorName: m.AuthorName, ReplyCount: m.ReplyCount, BotName: m.BotName})
		}
		reactions, _ := s.q.ListReactionsByChannel(r.Context(), id)
		atts, _ := s.q.ListAttachmentsByChannel(r.Context(), id)
		writeJSON(w, http.StatusOK, map[string]any{
			"messages": s.markSaved(r.Context(), mu.ID, id, buildMessages(raw, reactions, atts, mu.ID, false)), // raw is already chronological
			"has_more": len(older) == half, "focus": around,
		})
		return
	}
	// Pagination: ?limit (default 50, cap 100) and ?before=<RFC3339 of the already-loaded top> to
	// bring in the PREVIOUS messages. Without `before`, it's the most recent page (and marks the
	// channel as read).
	limit := int32(50)
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, e := strconv.Atoi(l); e == nil && n > 0 && n <= 100 {
			limit = int32(n)
		}
	}
	before := r.URL.Query().Get("before")
	raw := make([]rawMsg, 0, limit)
	if before != "" {
		ts, perr := time.Parse(time.RFC3339Nano, before)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, "chat.invalid")
			return
		}
		rows, err := s.q.ListMessagesBeforeByChannel(r.Context(), db.ListMessagesBeforeByChannelParams{ChannelID: id, CreatedAt: ts, Limit: limit})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "chat.failed")
			return
		}
		for _, m := range rows {
			raw = append(raw, rawMsg{ID: m.ID, ChannelID: m.ChannelID, UserID: m.UserID, ParentID: m.ParentID, Body: m.Body, EditedAt: m.EditedAt, DeletedAt: m.DeletedAt, CreatedAt: m.CreatedAt, AuthorEmail: m.AuthorEmail, AuthorName: m.AuthorName, ReplyCount: m.ReplyCount, BotName: m.BotName})
		}
	} else {
		rows, err := s.q.ListMessagesByChannel(r.Context(), db.ListMessagesByChannelParams{ChannelID: id, Limit: limit})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "chat.failed")
			return
		}
		for _, m := range rows {
			raw = append(raw, rawMsg{ID: m.ID, ChannelID: m.ChannelID, UserID: m.UserID, ParentID: m.ParentID, Body: m.Body, EditedAt: m.EditedAt, DeletedAt: m.DeletedAt, CreatedAt: m.CreatedAt, AuthorEmail: m.AuthorEmail, AuthorName: m.AuthorName, ReplyCount: m.ReplyCount, BotName: m.BotName})
		}
	}
	hasMore := len(raw) == int(limit) // there may be older ones
	reactions, _ := s.q.ListReactionsByChannel(r.Context(), id)
	atts, _ := s.q.ListAttachmentsByChannel(r.Context(), id)
	msgs := s.markSaved(r.Context(), mu.ID, id, buildMessages(raw, reactions, atts, mu.ID, true))
	if before == "" {
		_ = s.q.UpdateLastRead(r.Context(), db.UpdateLastReadParams{ChannelID: id, UserID: mu.ID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs, "has_more": hasMore})
}

// handleThreadReplies returns the parent + the replies of a thread.
func (s *Server) handleThreadReplies(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	parent, err := s.q.GetMessage(r.Context(), db.GetMessageParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	if _, ok := s.memberOr403(w, r, parent.ChannelID); !ok {
		return
	}
	replies, _ := s.q.ListThreadReplies(r.Context(), pgtype.UUID{Bytes: id, Valid: true})
	raw := make([]rawMsg, len(replies))
	for i, m := range replies {
		raw[i] = rawMsg{
			ID: m.ID, ChannelID: m.ChannelID, UserID: m.UserID, ParentID: m.ParentID, Body: m.Body,
			EditedAt: m.EditedAt, DeletedAt: m.DeletedAt, CreatedAt: m.CreatedAt,
			AuthorEmail: m.AuthorEmail, AuthorName: m.AuthorName,
		}
	}
	reactions, _ := s.q.ListReactionsByChannel(r.Context(), parent.ChannelID)
	atts, _ := s.q.ListAttachmentsByChannel(r.Context(), parent.ChannelID)
	writeJSON(w, http.StatusOK, map[string]any{"replies": buildMessages(raw, reactions, atts, mu.ID, false)})
}

type sendMessageReq struct {
	Body        string          `json:"body"`
	ParentID    string          `json:"parent_id"`
	Mentions    []string        `json:"mentions"` // mentioned user ids
	Attachments []attachmentDTO `json:"attachments"`
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	if _, ok := s.memberOr403(w, r, id); !ok {
		return
	}
	// An archived channel is read-only — otherwise "archiving" wouldn't mean anything.
	if ch, err := s.q.GetChannel(r.Context(), db.GetChannelParams{ID: id, TenantID: mu.TenantID}); err == nil && ch.ArchivedAt.Valid {
		writeErr(w, http.StatusForbidden, "chat.channel_archived")
		return
	}
	var req sendMessageReq
	if !decode(w, r, &req) {
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" && len(req.Attachments) == 0 {
		writeErr(w, http.StatusBadRequest, "chat.invalid")
		return
	}
	var parent pgtype.UUID
	if p, perr := uuid.Parse(req.ParentID); perr == nil {
		parent = pgtype.UUID{Bytes: p, Valid: true}
	}
	msg, err := s.q.CreateMessage(r.Context(), db.CreateMessageParams{
		TenantID: mu.TenantID, ChannelID: id, UserID: mu.ID, ParentID: parent, Body: body,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.send_failed")
		return
	}
	// Attachments: links each file (already uploaded to storage) to the message.
	out := make([]attachmentDTO, 0, len(req.Attachments))
	for _, a := range req.Attachments {
		fid, perr := uuid.Parse(a.FileID)
		if perr != nil {
			continue
		}
		_ = s.q.AddMessageAttachment(r.Context(), db.AddMessageAttachmentParams{MessageID: msg.ID, FileID: fid, Name: a.Name, Size: a.Size})
		out = append(out, attachmentDTO{FileID: a.FileID, Name: a.Name, Size: a.Size})
	}
	author := mu.Name
	if author == "" {
		author = mu.Email
	}
	dto := messageDTO{
		ID: msg.ID.String(), ChannelID: id.String(), UserID: mu.ID.String(), Parent: uuidStr(parent),
		Body: body, Author: author, AuthorEmail: mu.Email, CreatedAt: iso(msg.CreatedAt),
		Reactions: []reactionDTO{}, Attachments: out,
	}
	s.publish(r.Context(), "message", id, s.channelRecipients(r.Context(), id), dto)

	preview := trim(body, 120)
	if preview == "" && len(out) > 0 {
		preview = "📎 " + out[0].Name
	}
	s.fanoutMessage(r.Context(), id, mu.TenantID, mu.ID, author, body, preview, req.Mentions)
	writeJSON(w, http.StatusCreated, dto)
}

// fanoutMessage fires the notifications for a new message: personal mentions, @channel/@here, and
// the plain "message" notification (DM always; channel "all"). Dedup per person; never the author
// nor anyone who muted it.
func (s *Server) fanoutMessage(ctx context.Context, channelID, tenantID, authorID uuid.UUID, author, body, preview string, mentions []string) {
	notified := map[uuid.UUID]bool{authorID: true}

	// 1) Personal mentions. The target MUST be a channel member (otherwise it would notify any
	// UUID, even from another tenant — audit M).
	for _, uidStr := range mentions {
		tid, perr := uuid.Parse(uidStr)
		if perr != nil || notified[tid] {
			continue
		}
		m, merr := s.q.GetMembership(ctx, db.GetMembershipParams{ChannelID: channelID, UserID: tid})
		if merr != nil || m.NotifPref == "none" {
			continue
		}
		s.notifyUser(ctx, tenantID, tid, "chat_mention", author+" mentioned you", preview, chatLink(channelID), uuid.Nil,
			map[string]string{"actor": author})
		notified[tid] = true
	}

	meta, _ := s.q.GetChannelMeta(ctx, channelID)
	prefs, _ := s.q.ListChannelMemberPrefs(ctx, channelID)

	// 2) @channel (all members) and @here (only those online). Respects mute; doesn't apply in DMs.
	atChannel := hasMentionToken(body, "channel")
	atHere := hasMentionToken(body, "here")
	if meta.Kind != "dm" && (atChannel || atHere) {
		online := map[string]bool{}
		if atHere && s.hub != nil {
			memberIDs, _ := s.q.ListMemberUserIDs(ctx, channelID)
			for _, o := range s.hub.OnlineAmong(ctx, memberIDs) {
				online[o] = true
			}
		}
		label := "@here"
		if atChannel {
			label = "@channel"
		}
		for _, p := range prefs {
			if notified[p.UserID] || p.NotifPref == "none" {
				continue
			}
			if !atChannel && !online[p.UserID.String()] {
				continue // @here alone: only those online
			}
			s.notifyUser(ctx, tenantID, p.UserID, "chat_mention", author+" called "+label+" in #"+meta.Name, preview, chatLink(channelID), uuid.Nil,
				map[string]string{"actor": author, "at": label, "channel": meta.Name})
			notified[p.UserID] = true
		}
	}

	// 3) Plain message: DM always notifies the other side; channel notifies whoever chose "all".
	isDM := meta.Kind == "dm"
	for _, p := range prefs {
		if notified[p.UserID] || p.NotifPref == "none" {
			continue
		}
		if !isDM && p.NotifPref != "all" {
			continue
		}
		title := author
		nkind := "chat_dm"
		if !isDM {
			title = author + " in #" + meta.Name
			nkind = "chat_message"
		}
		s.notifyPushOnly(ctx, p.UserID, nkind, title, preview, chatLink(channelID))
	}
}

type forwardReq struct {
	ChannelID string `json:"channel_id"`
}

// handleForwardMessage forwards a message to another channel, creating a new message that quotes
// the original. Requires membership of the SOURCE (to read) and of the DESTINATION (to post).
func (s *Server) handleForwardMessage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	mid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	src, err := s.q.GetMessage(r.Context(), db.GetMessageParams{ID: mid, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	if _, ok := s.memberOr403(w, r, src.ChannelID); !ok {
		return
	}
	var req forwardReq
	if !decode(w, r, &req) {
		return
	}
	target, err := uuid.Parse(req.ChannelID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid")
		return
	}
	if _, merr := s.q.GetMembership(r.Context(), db.GetMembershipParams{ChannelID: target, UserID: mu.ID}); merr != nil {
		writeErr(w, http.StatusForbidden, "chat.not_member")
		return
	}
	srcAuthor := "?"
	if u, uerr := s.q.GetUserByID(r.Context(), src.UserID); uerr == nil {
		srcAuthor = u.Name
		if srcAuthor == "" {
			srcAuthor = u.Email
		}
	}
	quoted := "> " + strings.ReplaceAll(strings.TrimSpace(src.Body), "\n", "\n> ")
	fwdBody := "↪ **Forwarded from " + srcAuthor + "**\n" + quoted
	msg, err := s.q.CreateMessage(r.Context(), db.CreateMessageParams{TenantID: mu.TenantID, ChannelID: target, UserID: mu.ID, Body: fwdBody})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.send_failed")
		return
	}
	author := mu.Name
	if author == "" {
		author = mu.Email
	}
	dto := messageDTO{
		ID: msg.ID.String(), ChannelID: target.String(), UserID: mu.ID.String(),
		Body: fwdBody, Author: author, AuthorEmail: mu.Email, CreatedAt: iso(msg.CreatedAt),
		Reactions: []reactionDTO{}, Attachments: []attachmentDTO{},
	}
	s.publish(r.Context(), "message", target, s.channelRecipients(r.Context(), target), dto)
	s.fanoutMessage(r.Context(), target, mu.TenantID, mu.ID, author, fwdBody, trim(fwdBody, 120), nil)
	writeJSON(w, http.StatusCreated, dto)
}

// handleSaveMessage / handleUnsaveMessage: save/unsave a message (the user's saved items).
func (s *Server) handleSaveMessage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	mid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	msg, err := s.q.GetMessage(r.Context(), db.GetMessageParams{ID: mid, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	if _, ok := s.memberOr403(w, r, msg.ChannelID); !ok {
		return
	}
	_ = s.q.SaveMessage(r.Context(), db.SaveMessageParams{UserID: mu.ID, MessageID: mid, TenantID: mu.TenantID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (s *Server) handleUnsaveMessage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	mid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	_ = s.q.UnsaveMessage(r.Context(), db.UnsaveMessageParams{UserID: mu.ID, MessageID: mid})
	writeJSON(w, http.StatusOK, map[string]string{"status": "unsaved"})
}

// handleListSaved lists the user's saved messages ("Saved" panel).
func (s *Server) handleListSaved(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListSavedMessages(r.Context(), mu.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		out = append(out, map[string]any{
			"id": m.ID, "channel_id": m.ChannelID, "channel_name": m.ChannelName, "channel_kind": m.ChannelKind,
			"author": authorName(m.AuthorName, m.AuthorEmail), "body": m.Body,
			"created_at": iso(m.CreatedAt), "saved_at": iso(m.SavedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": out})
}

func (s *Server) handleEditMessage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeErr(w, http.StatusBadRequest, "chat.invalid")
		return
	}
	msg, err := s.q.EditMessage(r.Context(), db.EditMessageParams{ID: id, UserID: mu.ID, Body: strings.TrimSpace(req.Body)})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	s.publish(r.Context(), "message_edit", msg.ChannelID, s.channelRecipients(r.Context(), msg.ChannelID),
		map[string]any{"id": msg.ID.String(), "body": msg.Body, "edited_at": tsStr(msg.EditedAt)})
	writeJSON(w, http.StatusOK, map[string]any{"id": msg.ID.String(), "body": msg.Body, "edited_at": tsStr(msg.EditedAt)})
}

func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	msg, err := s.q.GetMessage(r.Context(), db.GetMessageParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	// The author deletes their own; a channel manager/owner moderates (deletes others').
	if msg.UserID == mu.ID {
		if err := s.q.DeleteMessage(r.Context(), db.DeleteMessageParams{ID: id, UserID: mu.ID}); err != nil {
			writeErr(w, http.StatusInternalServerError, "chat.failed")
			return
		}
	} else {
		if _, ok := s.managerOr403(w, r, msg.ChannelID); !ok {
			return
		}
		if err := s.q.DeleteMessageAny(r.Context(), db.DeleteMessageAnyParams{ID: id, TenantID: mu.TenantID}); err != nil {
			writeErr(w, http.StatusInternalServerError, "chat.failed")
			return
		}
	}
	s.publish(r.Context(), "message_delete", msg.ChannelID, s.channelRecipients(r.Context(), msg.ChannelID),
		map[string]any{"id": id.String()})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---- Reactions ----

func (s *Server) handleReact(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	var req struct {
		Emoji  string `json:"emoji"`
		Remove bool   `json:"remove"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Emoji == "" {
		writeErr(w, http.StatusBadRequest, "chat.invalid")
		return
	}
	msg, err := s.q.GetMessage(r.Context(), db.GetMessageParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	// Only a channel member reacts: all its siblings (list/send/pin/search) check memberOr403 —
	// without this, a non-member could react to a message in a private channel and the reaction
	// would be broadcast to the members (audit M — channel authorization break).
	if _, ok := s.memberOr403(w, r, msg.ChannelID); !ok {
		return
	}
	if req.Remove {
		_ = s.q.RemoveReaction(r.Context(), db.RemoveReactionParams{MessageID: id, UserID: mu.ID, Emoji: req.Emoji})
	} else {
		_ = s.q.AddReaction(r.Context(), db.AddReactionParams{MessageID: id, UserID: mu.ID, Emoji: req.Emoji})
	}
	s.publish(r.Context(), "reaction", msg.ChannelID, s.channelRecipients(r.Context(), msg.ChannelID),
		map[string]any{"message_id": id.String(), "emoji": req.Emoji, "user_id": mu.ID.String(), "remove": req.Remove})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
