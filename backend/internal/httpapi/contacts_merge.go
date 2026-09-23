package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Merging duplicates + contact photo/avatar.

func orDefault(a, b string) string {
	if strings.TrimSpace(a) == "" {
		return b
	}
	return a
}

// unionStr joins two lists without duplicating (preserves order: a first).
func unionStr(a, b []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, v := range append(append([]string{}, a...), b...) {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// mergeCustom merges two JSON objects of custom fields; on conflict, "keep" wins.
func mergeCustom(keep, merge json.RawMessage) json.RawMessage {
	km, mm := map[string]string{}, map[string]string{}
	_ = json.Unmarshal(keep, &km)
	_ = json.Unmarshal(merge, &mm)
	for k, v := range mm {
		if _, ok := km[k]; !ok {
			km[k] = v
		}
	}
	if len(km) == 0 {
		return json.RawMessage("{}")
	}
	b, err := json.Marshal(km)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}

// handleMergeContacts merges the `merge_id` contact into `id` (keep): fills in keep's blanks, unions
// lists and custom fields, reassigns cards/notes/groups, and deletes the duplicate. Both must belong
// to the owner.
func (s *Server) handleMergeContacts(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	keepID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.invalid_id")
		return
	}
	var req struct {
		MergeID string `json:"merge_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	mergeID, err := uuid.Parse(req.MergeID)
	if err != nil || mergeID == keepID {
		writeErr(w, http.StatusBadRequest, "contacts.invalid_merge")
		return
	}
	keep, err1 := s.q.GetContact(r.Context(), db.GetContactParams{ID: keepID, OwnerUserID: mu.ID, TenantID: mu.TenantID})
	merge, err2 := s.q.GetContact(r.Context(), db.GetContactParams{ID: mergeID, OwnerUserID: mu.ID, TenantID: mu.TenantID})
	// Only the owner can merge (editing ≠ viewing): requires their own owner_user_id on both.
	if err1 != nil || err2 != nil || keep.OwnerUserID != mu.ID || merge.OwnerUserID != mu.ID {
		writeErr(w, http.StatusNotFound, "contacts.not_owner")
		return
	}
	companyID := keep.CompanyID
	if !companyID.Valid {
		companyID = merge.CompanyID
	}
	notes := keep.Notes
	if strings.TrimSpace(merge.Notes) != "" && !strings.Contains(notes, strings.TrimSpace(merge.Notes)) {
		notes = strings.TrimSpace(notes + "\n" + merge.Notes)
	}
	if _, err := s.q.UpdateContact(r.Context(), db.UpdateContactParams{
		ID: keepID, OwnerUserID: mu.ID, Kind: keep.Kind, Name: keep.Name,
		Email: orDefault(keep.Email, merge.Email), Phone: orDefault(keep.Phone, merge.Phone),
		Company: orDefault(keep.Company, merge.Company), Domain: orDefault(keep.Domain, merge.Domain),
		Notes: notes, Tags: unionStr(keep.Tags, merge.Tags),
		Title: orDefault(keep.Title, merge.Title), Mobile: orDefault(keep.Mobile, merge.Mobile),
		IsWhatsapp: keep.IsWhatsapp || merge.IsWhatsapp, AllowPhone: keep.AllowPhone, AllowEmail: keep.AllowEmail,
		CompanyID:    companyID,
		ExtraEmails:  unionStr(keep.ExtraEmails, merge.ExtraEmails),
		ExtraPhones:  unionStr(keep.ExtraPhones, merge.ExtraPhones),
		CustomFields: mergeCustom(keep.CustomFields, merge.CustomFields),
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "contacts.merge_failed")
		return
	}
	// Reassigns the duplicate's references to the kept contact, then deletes the duplicate.
	_ = s.q.MergeReassignCards(r.Context(), db.MergeReassignCardsParams{Keep: pgUUID(keepID), Merge: pgUUID(mergeID), Tenant: mu.TenantID})
	_ = s.q.MergeReassignNotes(r.Context(), db.MergeReassignNotesParams{Keep: keepID, Merge: mergeID, Tenant: mu.TenantID})
	_ = s.q.MergeReassignGroups(r.Context(), db.MergeReassignGroupsParams{Keep: keepID, Merge: mergeID})
	_ = s.q.ClearContactGroups(r.Context(), mergeID)
	if err := s.q.DeleteContact(r.Context(), db.DeleteContactParams{ID: mergeID, OwnerUserID: mu.ID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "contacts.merge_failed")
		return
	}
	out, _ := s.q.GetContact(r.Context(), db.GetContactParams{ID: keepID, OwnerUserID: mu.ID, TenantID: mu.TenantID})
	writeJSON(w, http.StatusOK, out)
}

// handleSetContactAvatar links (or unlinks) the contact's photo to a file already uploaded by the
// owner via /api/files. Reuses storage and the /thumb endpoint — no new image pipeline.
func (s *Server) handleSetContactAvatar(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.invalid_id")
		return
	}
	var req struct {
		FileID string `json:"file_id"` // empty = remove the photo
	}
	if !decode(w, r, &req) {
		return
	}
	var avatar pgtype.UUID
	if req.FileID != "" {
		fid, perr := uuid.Parse(req.FileID)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, "contacts.invalid_id")
			return
		}
		if _, ferr := s.q.GetStorageFile(r.Context(), db.GetStorageFileParams{ID: fid, OwnerUserID: ownerPg(mu)}); ferr != nil {
			writeErr(w, http.StatusNotFound, "storage.not_found") // the file must belong to the owner
			return
		}
		avatar = pgtype.UUID{Bytes: fid, Valid: true}
	}
	c, err := s.q.SetContactAvatar(r.Context(), db.SetContactAvatarParams{ID: id, OwnerUserID: mu.ID, AvatarFileID: avatar})
	if err != nil {
		writeErr(w, http.StatusNotFound, "contacts.not_owner")
		return
	}
	writeJSON(w, http.StatusOK, c)
}
