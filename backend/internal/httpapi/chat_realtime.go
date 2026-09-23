package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ---- Attachments (download authorized by channel membership) ----

func (s *Server) handleChatAttachment(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	fid, err := uuid.Parse(chi.URLParam(r, "fileId"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "chat.invalid_id")
		return
	}
	att, err := s.q.GetChatAttachment(r.Context(), db.GetChatAttachmentParams{FileID: fid, UserID: mu.ID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "chat.not_found")
		return
	}
	obj, err := s.storage.Get(r.Context(), att.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer obj.Close()
	if r.URL.Query().Get("inline") == "1" {
		ct := mime.TypeByExtension("." + extOf(att.Name))
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", att.Name))
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", att.Name))
	}
	_, _ = io.Copy(w, obj)
}

// ---- Presence (online/away) ----

// handleChatPresence returns which tenant members are online right now.
func (s *Server) handleChatPresence(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	if s.hub == nil {
		writeJSON(w, http.StatusOK, map[string]any{"online": []string{}})
		return
	}
	users, _ := s.q.ListMailUsersByTenant(r.Context(), mu.TenantID)
	ids := make([]uuid.UUID, len(users))
	presByID := make(map[string]string, len(users))
	for i, u := range users {
		ids[i] = u.ID
		presByID[u.ID.String()] = u.Presence
	}
	online := s.hub.OnlineAmong(r.Context(), ids)
	// Rich presence only for whoever is REALLY online (offline always wins).
	presence := make(map[string]string, len(online))
	for _, id := range online {
		p := presByID[id]
		if p == "" {
			p = "active"
		}
		presence[id] = p
	}
	writeJSON(w, http.StatusOK, map[string]any{"online": online, "presence": presence})
}

func (s *Server) handleChatSearch(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	// Slack-style filters: from:<name/email>, before:/after:<YYYY-MM-DD>. The rest is the term (FTS).
	p := db.SearchMessagesParams{UserID: mu.ID, TenantID: mu.TenantID, Lim: 40}
	var termParts []string
	for _, tok := range strings.Fields(q) {
		low := strings.ToLower(tok)
		switch {
		case strings.HasPrefix(low, "from:") && len(tok) > 5:
			if uid := s.resolveColleague(r.Context(), mu.TenantID, tok[5:]); uid != uuid.Nil {
				p.HasFrom, p.FromUser = true, uid
			}
		case strings.HasPrefix(low, "before:") && len(tok) > 7:
			if t, err := time.Parse("2006-01-02", tok[7:]); err == nil {
				p.HasBefore, p.BeforeTs = true, t
			}
		case strings.HasPrefix(low, "after:") && len(tok) > 6:
			if t, err := time.Parse("2006-01-02", tok[6:]); err == nil {
				p.HasAfter, p.AfterTs = true, t
			}
		default:
			termParts = append(termParts, tok)
		}
	}
	p.Term = strings.Join(termParts, " ")
	if len(p.Term) < 2 && !p.HasFrom && !p.HasBefore && !p.HasAfter {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}})
		return
	}
	rows, err := s.q.SearchMessages(r.Context(), p)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "chat.failed")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		author := m.AuthorName
		if author == "" {
			author = m.AuthorEmail
		}
		name := m.ChannelName
		if m.ChannelKind == "dm" {
			name = s.dmDisplayName(r.Context(), m.ChannelID, mu.ID)
		}
		out = append(out, map[string]any{
			"id": m.ID, "channel_id": m.ChannelID, "channel_name": name, "channel_kind": m.ChannelKind,
			"author": author, "body": m.Body, "created_at": iso(m.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}

// ---- WebSocket ----

func (s *Server) handleChatWS(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeErr(w, http.StatusServiceUnavailable, "chat.realtime_disabled")
		return
	}
	mu := mailUserFrom(r.Context())
	// Origin check: the default accepts only same-origin (web), and the allow-list adds the native
	// app (Tauri). Without this (InsecureSkipVerify) any page could open the WS carrying the
	// victim's ambient cookie — CSWSH (audit M).
	opts := &websocket.AcceptOptions{OriginPatterns: originHostPatterns(s.corsOrigins)}
	ws, err := websocket.Accept(w, r, opts)
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ctx := r.Context()
	// The client only ever sends "typing": {type:"typing", channel_id}. Relay to channel members.
	onClient := func(data []byte) {
		var in struct {
			Type      string `json:"type"`
			ChannelID string `json:"channel_id"`
		}
		if json.Unmarshal(data, &in) != nil || in.Type != "typing" {
			return
		}
		cid, perr := uuid.Parse(in.ChannelID)
		if perr != nil {
			return
		}
		if _, merr := s.q.GetMembership(ctx, db.GetMembershipParams{ChannelID: cid, UserID: mu.ID}); merr != nil {
			return
		}
		author := mu.Name
		if author == "" {
			author = mu.Email
		}
		// EVERYONE EXCEPT WHOEVER IS TYPING. Seeing "so-and-so is typing…" with our own name while
		// we're typing isn't just noise: it looks like there's another person with the same name in
		// the channel. This also applies across devices — if I'm typing on the laptop, the phone
		// shouldn't announce me to myself.
		//
		// Filtered HERE and not in the UI because it's the only event that doesn't matter to the
		// person themselves: the rest (new message, edit) have to reach all of their devices to
		// stay in sync. That's why channelRecipients keeps including everyone.
		self := mu.ID.String()
		all := s.channelRecipients(ctx, cid)
		others := make([]string, 0, len(all))
		for _, id := range all {
			if id != self {
				others = append(others, id)
			}
		}
		if len(others) == 0 {
			return // channel with only myself (personal notes): no one to announce to
		}
		s.publish(ctx, "typing", cid, others,
			map[string]any{"user_id": self, "author": author})
	}
	s.hub.Serve(ctx, mu.ID, ws, onClient)
}
