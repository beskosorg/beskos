package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Time off: a day off (single day) and vacation (a range) are ALL-DAY events with their own
// kind. Being all-day, booking blocks them automatically (BookingBusy includes all-day). During
// vacation, the user can turn on the mailbox's automatic reply — done via JMAP VacationResponse
// with THEIR OWN credential (the same one from brokering; never an admin credential).

type timeOffReq struct {
	Kind         string `json:"kind"`      // dayoff | vacation
	Start        string `json:"start"`     // YYYY-MM-DD
	End          string `json:"end"`       // YYYY-MM-DD (vacation; ignored for dayoff)
	StartMin     *int   `json:"start_min"` // partial dayoff: minutes since 00:00 (nil = full day)
	EndMin       *int   `json:"end_min"`
	AutoReply    bool   `json:"auto_reply"`
	ReplySubject string `json:"reply_subject"`
	ReplyBody    string `json:"reply_body"`
	// on_conflict: "" asks (returns the conflicts), "cancel" cancels the events, "keep" keeps them.
	OnConflict string `json:"on_conflict"`
}

// POST /api/timeoff — creates the absence in the user's own calendar.
func (s *Server) handleCreateTimeOff(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req timeOffReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "timeoff.invalid_body")
		return
	}
	if req.Kind != "dayoff" && req.Kind != "vacation" {
		writeErr(w, http.StatusBadRequest, "timeoff.invalid_kind")
		return
	}
	start, err := time.ParseInLocation("2006-01-02", req.Start, time.Local)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "timeoff.invalid_date")
		return
	}
	end := start
	if req.Kind == "vacation" && req.End != "" {
		end, err = time.ParseInLocation("2006-01-02", req.End, time.Local)
		if err != nil || end.Before(start) {
			writeErr(w, http.StatusBadRequest, "timeoff.invalid_date")
			return
		}
	}
	// Partial time off: an hour range within the day (half day, appointment, etc.).
	allDay := true
	evStart, evEnd := start, end.Add(24*time.Hour-time.Second)
	if req.Kind == "dayoff" && req.StartMin != nil && req.EndMin != nil {
		sm, em := *req.StartMin, *req.EndMin
		if sm < 0 || em > 1440 || sm >= em {
			writeErr(w, http.StatusBadRequest, "timeoff.invalid_date")
			return
		}
		allDay = false
		evStart = start.Add(time.Duration(sm) * time.Minute)
		evEnd = start.Add(time.Duration(em) * time.Minute)
	}

	// Conflicts with the calendar: own events and invites in the range. Without the user's
	// decision, return the list and create NOTHING — the UI asks whether to cancel.
	conflicts, _ := s.q.TimeOffConflicts(r.Context(), db.TimeOffConflictsParams{
		OwnerID: pgUUID(mu.ID), TenantID: mu.TenantID, InviteeID: mu.ID, FromTs: evStart, ToTs: evEnd,
	})
	if len(conflicts) > 0 && req.OnConflict == "" {
		writeJSON(w, http.StatusOK, map[string]any{"status": "conflicts", "conflicts": conflicts})
		return
	}
	if len(conflicts) > 0 && req.OnConflict == "cancel" {
		s.cancelForTimeOff(r, mu, conflicts)
	}

	// Fallback title in English (law 4.6); the UI shows the label translated by kind.
	title, color := "Day off", "#9AA0A6"
	if req.Kind == "vacation" {
		title, color = "Vacation", "#53C08B"
	}
	ev, err := s.q.CreateEvent(r.Context(), db.CreateEventParams{
		TenantID: mu.TenantID, OwnerUserID: pgUUID(mu.ID), Title: title, Description: "",
		Location: "", Color: color, StartsAt: evStart, EndsAt: evEnd,
		SourceEmailID: "", Tags: []string{}, MeetingRoom: "", RemindMinutes: 0, AllDay: allDay,
		Recur: "", RecurUntil: pgtype.Timestamptz{}, CalendarID: pgtype.UUID{},
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "timeoff.create_failed")
		return
	}
	_ = s.q.SetEventKind(r.Context(), db.SetEventKindParams{ID: ev.ID, Kind: req.Kind, TenantID: mu.TenantID})

	out := map[string]any{"status": "ok", "event_id": ev.ID}
	// Vacation auto-reply — best-effort: the absence is still created even if JMAP fails, and the
	// customer is told to try again in settings.
	if req.Kind == "vacation" && req.AutoReply {
		if err := s.setVacationResponse(r, mu, start, end.Add(24*time.Hour-time.Second), req.ReplySubject, req.ReplyBody); err != nil {
			log.Printf("timeoff: vacation response for %s: %v", mu.Email, err)
			out["auto_reply"] = "failed"
		} else {
			out["auto_reply"] = "ok"
		}
	}
	writeJSON(w, http.StatusCreated, out)
}

// cancelForTimeOff resolves the conflicts when the user chose "cancel": the user's OWN events are
// deleted (soft-delete) and every participant is notified — internal guests via notification
// (+push) and external ones via e-mail. For events owned by OTHERS, it declines the invite and
// notifies the owner.
func (s *Server) cancelForTimeOff(r *http.Request, mu db.MailUser, conflicts []db.TimeOffConflictsRow) {
	ctx := r.Context()
	actor := mu.Name
	if actor == "" {
		actor = mu.Email
	}
	for _, c := range conflicts {
		when := c.StartsAt.UTC().Format(time.RFC3339)
		if !c.Own {
			// Someone else's invite: decline it and notify the organizer.
			_ = s.q.SetInviteStatus(ctx, db.SetInviteStatusParams{EventID: c.ID, UserID: mu.ID, Status: "declined"})
			if ev, err := s.q.GetEventInTenant(ctx, db.GetEventInTenantParams{ID: c.ID, TenantID: mu.TenantID}); err == nil && ev.OwnerUserID.Valid {
				s.notifyUser(ctx, mu.TenantID, uuid.UUID(ev.OwnerUserID.Bytes), "event_rsvp",
					actor+" declined: "+c.Title, actor+" declined — "+when, eventLink(ev.ID, ev.StartsAt), uuid.Nil,
					map[string]string{"actor": actor, "title": c.Title, "status": "declined", "when": when})
			}
			continue
		}
		// Own event: guests first (the list disappears with the logical cascade delete).
		invites, _ := s.q.ListEventInvites(ctx, c.ID)
		externals, _ := s.q.ListEventExternals(ctx, c.ID)
		if err := s.q.DeleteEvent(ctx, db.DeleteEventParams{ID: c.ID, TenantID: mu.TenantID, OwnerUserID: pgUUID(mu.ID)}); err != nil {
			continue
		}
		for _, inv := range invites {
			s.notifyUser(ctx, mu.TenantID, inv.UserID, "event_cancelled",
				"Cancelled: "+c.Title, actor+" cancelled the event — "+when, "/mail/cal", uuid.Nil,
				map[string]string{"actor": actor, "title": c.Title, "when": when})
		}
		if s.email != nil {
			body := fmt.Sprintf("%s cancelou o evento: %s\r\nEstava marcado para: %s\r\n", actor, c.Title, c.StartsAt.Format("02/01/2006 15:04"))
			for _, ext := range externals {
				go func(to, subject, body string) {
					if err := s.email.Send(to, subject, body); err != nil {
						log.Printf("timeoff cancel mail to %s: %v", to, err)
					}
				}(ext, "Evento cancelado: "+c.Title, body)
			}
		}
	}
}

// setVacationResponse turns on the auto-reply in Stalwart via JMAP, authenticated with the OWN
// mailbox's credential (H11 brokering — never an admin credential). from/until bound the period:
// Stalwart enables and disables it on its own on those dates.
func (s *Server) setVacationResponse(r *http.Request, mu db.MailUser, from, until time.Time, subject, body string) error {
	cred, ok := s.decryptCred(mu.JmapCred)
	if !ok {
		return fmt.Errorf("no mailbox credential")
	}
	ctx := r.Context()
	// 1) Session: find the primary mail accountId.
	sreq, err := http.NewRequestWithContext(ctx, http.MethodGet, s.sw.BaseURL()+"/jmap/session", nil)
	if err != nil {
		return err
	}
	sreq.Header.Set("Authorization", "Basic "+cred)
	resp, err := http.DefaultClient.Do(sreq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jmap session: HTTP %d", resp.StatusCode)
	}
	var sess struct {
		PrimaryAccounts map[string]string `json:"primaryAccounts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		return err
	}
	accountID := sess.PrimaryAccounts["urn:ietf:params:jmap:mail"]
	if accountID == "" {
		return fmt.Errorf("jmap session: no mail account")
	}
	// 2) VacationResponse/set no singleton.
	if subject == "" {
		subject = "Out of office"
	}
	payload := map[string]any{
		"using": []string{"urn:ietf:params:jmap:core", "urn:ietf:params:jmap:vacationresponse"},
		"methodCalls": []any{[]any{"VacationResponse/set", map[string]any{
			"accountId": accountID,
			"update": map[string]any{"singleton": map[string]any{
				"isEnabled": true,
				"fromDate":  from.UTC().Format(time.RFC3339),
				"toDate":    until.UTC().Format(time.RFC3339),
				"subject":   subject,
				"textBody":  body,
			}},
		}, "0"}},
	}
	buf, _ := json.Marshal(payload)
	vreq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.sw.BaseURL()+"/jmap/", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	vreq.Header.Set("Authorization", "Basic "+cred)
	vreq.Header.Set("Content-Type", "application/json")
	vresp, err := http.DefaultClient.Do(vreq)
	if err != nil {
		return err
	}
	defer vresp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(vresp.Body, 64<<10))
	if vresp.StatusCode != http.StatusOK {
		return fmt.Errorf("jmap set: HTTP %d: %s", vresp.StatusCode, raw)
	}
	// A method failure comes back in the body (notUpdated/error) with HTTP 200 — check for it.
	if bytes.Contains(raw, []byte("notUpdated")) || bytes.Contains(raw, []byte(`"error"`)) {
		return fmt.Errorf("jmap set rejected: %s", raw)
	}
	return nil
}
