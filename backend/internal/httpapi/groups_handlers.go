package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// User groups. Each group has a private chat channel created alongside it — and the
// group↔channel association is kept in sync: joining the group joins the channel, leaving leaves it.
// Managing requires members.manage (RBAC); listing requires members.view.

type groupDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ChannelID string `json:"channel_id"`
	Members   int64  `json:"members"`
}

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListUserGroups(r.Context(), mu.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "groups.failed")
		return
	}
	out := make([]groupDTO, 0, len(rows))
	for _, g := range rows {
		out = append(out, groupDTO{ID: g.ID.String(), Name: g.Name, ChannelID: uuidStr(g.ChannelID), Members: g.Members})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateGroup creates the group and its corresponding private chat channel.
func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req struct {
		Name    string   `json:"name"`
		Members []string `json:"members"` // user ids (optional)
	}
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "groups.invalid")
		return
	}
	g, err := s.q.CreateUserGroup(r.Context(), db.CreateUserGroupParams{TenantID: mu.TenantID, Name: name, CreatedBy: mu.ID})
	if err != nil {
		writeErr(w, http.StatusConflict, "groups.name_taken")
		return
	}
	// The group's private channel. If this fails, the group is still valid (just without a
	// channel) — we don't lose the group because of the chat.
	ch, cerr := s.q.CreateChannel(r.Context(), db.CreateChannelParams{
		TenantID: mu.TenantID, Kind: "private", Name: name, Topic: "", DmKey: "", CreatedBy: mu.ID,
	})
	if cerr == nil {
		_ = s.q.SetUserGroupChannel(r.Context(), db.SetUserGroupChannelParams{ID: g.ID, ChannelID: pgUUID(ch.ID)})
		g.ChannelID = pgUUID(ch.ID)
	}
	// The creator joins the group; the rest, if they came in the request.
	s.addMember(r.Context(), g, mu.TenantID, mu.ID)
	for _, id := range req.Members {
		if uid, perr := uuid.Parse(id); perr == nil {
			s.addMember(r.Context(), g, mu.TenantID, uid)
		}
	}
	writeJSON(w, http.StatusCreated, groupDTO{ID: g.ID.String(), Name: g.Name, ChannelID: uuidStr(g.ChannelID)})
}

// addMember puts the user in the group AND in the group's channel (the two go together).
func (s *Server) addMember(ctx context.Context, g db.UserGroup, tenantID, userID uuid.UUID) {
	_ = s.q.AddUserToGroup(ctx, db.AddUserToGroupParams{GroupID: g.ID, UserID: userID, TenantID: tenantID})
	if g.ChannelID.Valid {
		_ = s.q.AddChannelMember(ctx, db.AddChannelMemberParams{
			ChannelID: uuid.UUID(g.ChannelID.Bytes), TenantID: tenantID, UserID: userID, Role: "member",
		})
	}
}

func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "groups.invalid_id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "groups.invalid")
		return
	}
	g, err := s.q.UpdateUserGroup(r.Context(), db.UpdateUserGroupParams{ID: id, TenantID: mu.TenantID, Name: name})
	if err != nil {
		writeErr(w, http.StatusNotFound, "groups.not_found")
		return
	}
	// The channel follows the group's name.
	if g.ChannelID.Valid {
		_, _ = s.q.UpdateChannelName(r.Context(), db.UpdateChannelNameParams{ID: uuid.UUID(g.ChannelID.Bytes), TenantID: mu.TenantID, Name: name})
	}
	writeJSON(w, http.StatusOK, groupDTO{ID: g.ID.String(), Name: g.Name, ChannelID: uuidStr(g.ChannelID)})
}

// handleDeleteGroup deletes the group. The chat channel REMAINS (with its history) — deleting the
// conversation along with it would be silent data loss.
func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "groups.invalid_id")
		return
	}
	if err := s.q.DeleteUserGroup(r.Context(), db.DeleteUserGroupParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "groups.delete_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleListGroupMembers(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "groups.invalid_id")
		return
	}
	if _, err := s.q.GetUserGroup(r.Context(), db.GetUserGroupParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "groups.not_found")
		return
	}
	members, _ := s.q.ListGroupMembers(r.Context(), id)
	writeJSON(w, http.StatusOK, orEmpty(members))
}

func (s *Server) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "groups.invalid_id")
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	uid, err := uuid.Parse(req.UserID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "groups.invalid")
		return
	}
	g, err := s.q.GetUserGroup(r.Context(), db.GetUserGroupParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "groups.not_found")
		return
	}
	// The target must belong to the SAME tenant (never trust the id coming from the request).
	if _, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: uid, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "members.not_found")
		return
	}
	s.addMember(r.Context(), g, mu.TenantID, uid)
	writeJSON(w, http.StatusOK, map[string]string{"status": "added"})
}

// handleUserGroups lists the groups of ONE user (the same user can be in several).
func (s *Server) handleUserGroups(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "members.invalid_id")
		return
	}
	if _, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "members.not_found")
		return
	}
	groups, _ := s.q.ListGroupsForUser(r.Context(), id)
	writeJSON(w, http.StatusOK, orEmpty(groups))
}

// handleSetUserGroups sets in one go WHICH groups the user is in (the frontend sends the full
// list). It computes the difference and also syncs the chat channels.
func (s *Server) handleSetUserGroups(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "members.invalid_id")
		return
	}
	if _, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: id, TenantID: mu.TenantID}); err != nil {
		writeErr(w, http.StatusNotFound, "members.not_found")
		return
	}
	var req struct {
		GroupIDs []string `json:"group_ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.syncUserGroups(r.Context(), mu.TenantID, id, req.GroupIDs)
	groups, _ := s.q.ListGroupsForUser(r.Context(), id)
	writeJSON(w, http.StatusOK, orEmpty(groups))
}

// syncUserGroups puts the user in exactly the requested groups: joins the missing ones, leaves the
// extra ones — and each group's channel follows along. Groups from another tenant are ignored.
func (s *Server) syncUserGroups(ctx context.Context, tenantID, userID uuid.UUID, want []string) {
	target := map[uuid.UUID]bool{}
	for _, raw := range want {
		gid, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		g, err := s.q.GetUserGroup(ctx, db.GetUserGroupParams{ID: gid, TenantID: tenantID})
		if err != nil { // not from the tenant → ignore (never trust the id from the request)
			continue
		}
		target[gid] = true
		s.addMember(ctx, g, tenantID, userID)
	}
	current, _ := s.q.ListGroupsForUser(ctx, userID)
	for _, c := range current {
		if target[c.ID] {
			continue
		}
		g, err := s.q.GetUserGroup(ctx, db.GetUserGroupParams{ID: c.ID, TenantID: tenantID})
		if err != nil {
			continue
		}
		_ = s.q.RemoveUserFromGroup(ctx, db.RemoveUserFromGroupParams{GroupID: c.ID, UserID: userID})
		if g.ChannelID.Valid {
			_ = s.q.RemoveChannelMember(ctx, db.RemoveChannelMemberParams{ChannelID: uuid.UUID(g.ChannelID.Bytes), UserID: userID})
		}
	}
}

func (s *Server) handleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	uid, uerr := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil || uerr != nil {
		writeErr(w, http.StatusBadRequest, "groups.invalid_id")
		return
	}
	g, err := s.q.GetUserGroup(r.Context(), db.GetUserGroupParams{ID: id, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "groups.not_found")
		return
	}
	_ = s.q.RemoveUserFromGroup(r.Context(), db.RemoveUserFromGroupParams{GroupID: id, UserID: uid})
	if g.ChannelID.Valid {
		_ = s.q.RemoveChannelMember(r.Context(), db.RemoveChannelMemberParams{ChannelID: uuid.UUID(g.ChannelID.Bytes), UserID: uid})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}
