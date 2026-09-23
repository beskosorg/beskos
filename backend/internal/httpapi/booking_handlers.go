package httpapi

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/notify"
)

// Booking: a public Calendly-style page per collaborator. The owner configures weekly windows
// plus required fields; outsiders book at /b/<org>/<collaborator>. Each booking creates the
// event (with a Meet room), emails whoever booked, and notifies the collaborator.

// bookingWindow is a weekly availability window, in minutes since 00:00 local server time.
// weekday follows time.Weekday/JS: 0 = Sunday.
type bookingWindow struct {
	Weekday int `json:"weekday"`
	Start   int `json:"start"`
	End     int `json:"end"`
}

var bookingEmailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]{2,}$`)

func bookingSlugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case strings.ContainsRune("áàâãä", r):
			b.WriteRune('a')
			dash = false
		case strings.ContainsRune("éèêë", r):
			b.WriteRune('e')
			dash = false
		case strings.ContainsRune("íìîï", r):
			b.WriteRune('i')
			dash = false
		case strings.ContainsRune("óòôõö", r):
			b.WriteRune('o')
			dash = false
		case strings.ContainsRune("úùûü", r):
			b.WriteRune('u')
			dash = false
		case r == 'ç':
			b.WriteRune('c')
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteRune('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func parseBookingWindows(raw []byte) []bookingWindow {
	var ws []bookingWindow
	if err := json.Unmarshal(raw, &ws); err != nil {
		return nil
	}
	out := ws[:0]
	for _, w := range ws {
		if w.Weekday >= 0 && w.Weekday <= 6 && w.Start >= 0 && w.End <= 1440 && w.Start < w.End {
			out = append(out, w)
		}
	}
	return out
}

// bookingOut is the configuration as the owner sees it (includes the public URL).
func (s *Server) bookingOut(p db.BookingPage) map[string]any {
	url := ""
	if s.publicURL != "" {
		url = s.publicURL + "/b/" + p.OrgSlug + "/" + p.UserSlug
	}
	return map[string]any{
		"enabled": p.Enabled, "title": p.Title, "duration_min": p.DurationMin,
		"phone_field": p.PhoneField, "company_field": p.CompanyField,
		"windows": parseBookingWindows(p.Windows), "url": url,
	}
}

// GET /api/booking/page — my configuration (defaults if it doesn't exist yet).
func (s *Server) handleGetBookingPage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	p, err := s.q.GetBookingPageByUser(r.Context(), mu.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": false, "title": "", "duration_min": 30,
			"phone_field": "optional", "company_field": "optional",
			"windows": []bookingWindow{}, "url": "",
		})
		return
	}
	writeJSON(w, http.StatusOK, s.bookingOut(p))
}

type saveBookingPageReq struct {
	Enabled      bool            `json:"enabled"`
	Title        string          `json:"title"`
	DurationMin  int             `json:"duration_min"`
	PhoneField   string          `json:"phone_field"`
	CompanyField string          `json:"company_field"`
	Windows      []bookingWindow `json:"windows"`
}

// PUT /api/booking/page — saves the configuration; slugs are generated on the first save and
// stay stable (the public URL doesn't change on rename).
func (s *Server) handleSaveBookingPage(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	var req saveBookingPageReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "booking.invalid_body")
		return
	}
	if req.DurationMin < 10 || req.DurationMin > 240 {
		req.DurationMin = 30
	}
	valid := map[string]bool{"off": true, "optional": true, "required": true}
	if !valid[req.PhoneField] {
		req.PhoneField = "optional"
	}
	if !valid[req.CompanyField] {
		req.CompanyField = "optional"
	}
	clean := make([]bookingWindow, 0, len(req.Windows))
	for _, win := range req.Windows {
		if win.Weekday >= 0 && win.Weekday <= 6 && win.Start >= 0 && win.End <= 1440 && win.Start < win.End {
			clean = append(clean, win)
		}
	}
	wsJSON, _ := json.Marshal(clean)

	// Slugs: keep the existing ones; otherwise derive from the tenant name and the collaborator's name.
	orgSlug, userSlug := "", ""
	if prev, err := s.q.GetBookingPageByUser(r.Context(), mu.ID); err == nil {
		orgSlug, userSlug = prev.OrgSlug, prev.UserSlug
	} else {
		ten, terr := s.q.GetTenant(r.Context(), mu.TenantID)
		if terr != nil {
			writeErr(w, http.StatusInternalServerError, "booking.save_failed")
			return
		}
		orgSlug = bookingSlugify(ten.Name)
		userSlug = bookingSlugify(mu.Name)
		if userSlug == "" {
			userSlug = bookingSlugify(strings.SplitN(mu.Email, "@", 2)[0])
		}
		if orgSlug == "" {
			orgSlug = "org-" + mu.TenantID.String()[:8]
		}
		if userSlug == "" {
			userSlug = "u-" + mu.ID.String()[:8]
		}
	}
	// Collision of the (org, user) pair between same-named tenants: append a suffix and retry.
	base := userSlug
	var page db.BookingPage
	var err error
	for i := 0; i < 5; i++ {
		page, err = s.q.UpsertBookingPage(r.Context(), db.UpsertBookingPageParams{
			TenantID: mu.TenantID, UserID: mu.ID, OrgSlug: orgSlug, UserSlug: userSlug,
			Enabled: req.Enabled, Title: strings.TrimSpace(req.Title), DurationMin: int32(req.DurationMin),
			PhoneField: req.PhoneField, CompanyField: req.CompanyField, Windows: wsJSON,
		})
		if err == nil {
			break
		}
		userSlug = fmt.Sprintf("%s-%s", base, uuid.NewString()[:4])
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "booking.save_failed")
		return
	}
	writeJSON(w, http.StatusOK, s.bookingOut(page))
}

// -- Public (no session; rate limited) --

func (s *Server) publicBookingPage(w http.ResponseWriter, r *http.Request) (db.BookingPage, db.MailUser, bool) {
	page, err := s.q.GetBookingPageBySlugs(r.Context(), db.GetBookingPageBySlugsParams{
		OrgSlug: chi.URLParam(r, "org"), UserSlug: chi.URLParam(r, "user"),
	})
	if err != nil || !page.Enabled {
		writeErr(w, http.StatusNotFound, "booking.not_found")
		return db.BookingPage{}, db.MailUser{}, false
	}
	owner, err := s.q.GetMailUserInTenant(r.Context(), db.GetMailUserInTenantParams{ID: page.UserID, TenantID: page.TenantID})
	if err != nil {
		writeErr(w, http.StatusNotFound, "booking.not_found")
		return db.BookingPage{}, db.MailUser{}, false
	}
	return page, owner, true
}

// GET /api/public/booking/{org}/{user} — data to build the public page.
func (s *Server) handlePublicBookingInfo(w http.ResponseWriter, r *http.Request) {
	page, owner, ok := s.publicBookingPage(w, r)
	if !ok {
		return
	}
	ten, _ := s.q.GetTenant(r.Context(), page.TenantID)
	days := map[int]bool{}
	for _, win := range parseBookingWindows(page.Windows) {
		days[win.Weekday] = true
	}
	weekdays := make([]int, 0, 7)
	for d := 0; d < 7; d++ {
		if days[d] {
			weekdays = append(weekdays, d)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"org": ten.Name, "name": owner.Name, "avatar": owner.Avatar, "title": page.Title,
		"duration_min": page.DurationMin, "phone_field": page.PhoneField, "company_field": page.CompanyField,
		"weekdays": weekdays,
		// Organization branding: the public page wears the customer's colors/logo.
		"brand": map[string]any{"color": ten.BrandColor, "logo": ten.BrandLogo, "logo_white_dark": ten.BrandLogoWhiteDark},
	})
}

// bookingSlotsFor computes the free start times of a day, INTEGRATED with the collaborator's full
// calendar: own events (timed and all-day), non-declined invites, and expanded recurring events.
// Step = duration; minimum 30-minute lead time.
func (s *Server) bookingSlotsFor(r *http.Request, page db.BookingPage, day time.Time) []time.Time {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	dayEnd := dayStart.Add(24 * time.Hour)
	type block struct{ s, e time.Time }
	var blocks []block
	if rows, err := s.q.BookingBusy(r.Context(), db.BookingBusyParams{
		OwnerID: pgUUID(page.UserID), TenantID: page.TenantID, InviteeID: page.UserID,
		FromTs: dayStart, ToTs: dayEnd,
	}); err == nil {
		for _, b := range rows {
			blocks = append(blocks, block{b.StartsAt, b.EndsAt})
		}
	}
	// Recurring events: the master might have started months earlier — expand the occurrences for the day.
	if masters, err := s.q.ListRecurringByOwner(r.Context(), pgUUID(page.UserID)); err == nil {
		for _, m := range masters {
			if m.TenantID != page.TenantID {
				continue
			}
			for _, occ := range expandRecurrence(m, dayStart, dayEnd) {
				blocks = append(blocks, block{occ.StartsAt, occ.EndsAt})
			}
		}
	}
	dur := time.Duration(page.DurationMin) * time.Minute
	minStart := time.Now().Add(30 * time.Minute)
	var out []time.Time
	for _, win := range parseBookingWindows(page.Windows) {
		if win.Weekday != int(dayStart.Weekday()) {
			continue
		}
		for m := win.Start; m+int(page.DurationMin) <= win.End; m += int(page.DurationMin) {
			st := dayStart.Add(time.Duration(m) * time.Minute)
			en := st.Add(dur)
			if st.Before(minStart) {
				continue
			}
			free := true
			for _, b := range blocks {
				if st.Before(b.e) && en.After(b.s) {
					free = false
					break
				}
			}
			if free {
				out = append(out, st)
			}
		}
	}
	return out
}

// GET /api/public/booking/{org}/{user}/slots?date=YYYY-MM-DD
func (s *Server) handlePublicBookingSlots(w http.ResponseWriter, r *http.Request) {
	page, _, ok := s.publicBookingPage(w, r)
	if !ok {
		return
	}
	day, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("date"), time.Local)
	if err != nil || day.Before(time.Now().AddDate(0, 0, -1)) || day.After(time.Now().AddDate(0, 2, 0)) {
		writeErr(w, http.StatusBadRequest, "booking.invalid_date")
		return
	}
	slots := s.bookingSlotsFor(r, page, day)
	iso := make([]string, 0, len(slots))
	for _, t := range slots {
		iso = append(iso, t.Format(time.RFC3339))
	}
	writeJSON(w, http.StatusOK, map[string]any{"slots": iso})
}

type publicBookReq struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Company  string `json:"company"`
	Note     string `json:"note"`
	StartsAt string `json:"starts_at"`
}

// POST /api/public/booking/{org}/{user}/book — creates the booking: event + Meet room + email to
// whoever booked + notification to the collaborator. Revalidates the slot on the server (the client
// never decides).
func (s *Server) handlePublicBookingCreate(w http.ResponseWriter, r *http.Request) {
	page, owner, ok := s.publicBookingPage(w, r)
	if !ok {
		return
	}
	var req publicBookReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "booking.invalid_body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Phone = strings.TrimSpace(req.Phone)
	req.Company = strings.TrimSpace(req.Company)
	if req.Name == "" || len(req.Name) > 120 {
		writeErr(w, http.StatusBadRequest, "booking.name_required")
		return
	}
	if !bookingEmailRe.MatchString(req.Email) || len(req.Email) > 254 {
		writeErr(w, http.StatusBadRequest, "booking.email_invalid")
		return
	}
	if page.PhoneField == "required" && req.Phone == "" {
		writeErr(w, http.StatusBadRequest, "booking.phone_required")
		return
	}
	if page.CompanyField == "required" && req.Company == "" {
		writeErr(w, http.StatusBadRequest, "booking.company_required")
		return
	}
	starts, err := time.Parse(time.RFC3339, req.StartsAt)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "booking.invalid_date")
		return
	}
	starts = starts.In(time.Local)
	// The requested slot must be in the list computed RIGHT NOW (window + free + lead time).
	okSlot := false
	for _, t := range s.bookingSlotsFor(r, page, starts) {
		if t.Equal(starts) {
			okSlot = true
			break
		}
	}
	if !okSlot {
		writeErr(w, http.StatusConflict, "booking.slot_taken")
		return
	}
	ends := starts.Add(time.Duration(page.DurationMin) * time.Minute)

	ctx := r.Context()
	bk, err := s.q.CreateBooking(ctx, db.CreateBookingParams{
		TenantID: page.TenantID, PageID: page.ID, UserID: page.UserID,
		Name: req.Name, Email: req.Email, Phone: req.Phone, Company: req.Company,
		Note: strings.TrimSpace(req.Note), StartsAt: starts, EndsAt: ends,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "booking.create_failed")
		return
	}
	title, desc := notify.BookingEvent(owner.Language, page.Title, req.Name, req.Email, req.Phone, req.Company, strings.TrimSpace(req.Note))
	ev, err := s.q.CreateEvent(ctx, db.CreateEventParams{
		TenantID: page.TenantID, OwnerUserID: pgUUID(owner.ID), Title: title, Description: desc,
		Location: "", Color: "#8B7CF6", StartsAt: starts, EndsAt: ends, SourceEmailID: "",
		Tags: []string{}, MeetingRoom: "", RemindMinutes: 10, AllDay: false, Recur: "", CalendarID: pgtype.UUID{},
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "booking.create_failed")
		return
	}
	_ = s.q.SetEventBooking(ctx, db.SetEventBookingParams{ID: ev.ID, BookingID: pgUUID(bk.ID)})
	room := s.attachMeetToEvent(ctx, owner, ev.ID, title, starts, ends)
	_ = s.q.AddEventExternal(ctx, db.AddEventExternalParams{EventID: ev.ID, TenantID: page.TenantID, Email: req.Email})

	// Confirmation email to whoever booked (best-effort, asynchronous).
	if s.email != nil {
		who := owner.Name
		if who == "" {
			who = owner.Email
		}
		meetURL := ""
		if room != "" && s.publicURL != "" {
			meetURL = s.publicURL + "/j/" + room
		}
		subject, body := notify.BookingConfirmed(owner.Language, title, who, starts.Format("02/01/2006 15:04"), int(page.DurationMin), meetURL)
		go func(to, subject, body string) {
			if err := s.email.Send(to, subject, body); err != nil {
				log.Printf("booking confirmation to %s: %v", to, err)
			}
		}(req.Email, subject, body)
	}
	// In-app + push notification to the collaborator (the frontend translates by kind+args).
	s.notifyUser(ctx, page.TenantID, owner.ID, "booking_new",
		"New booking: "+req.Name, req.Name+" ("+req.Email+") booked "+starts.Format("02/01/2006 15:04"),
		eventLink(ev.ID, starts), uuid.Nil, map[string]string{"name": req.Name, "email": req.Email, "when": starts.Format(time.RFC3339)})

	out := map[string]any{"status": "ok", "starts_at": starts.Format(time.RFC3339), "duration_min": page.DurationMin}
	if room != "" && s.publicURL != "" {
		out["meet_url"] = s.publicURL + "/j/" + room
	}
	writeJSON(w, http.StatusCreated, out)
}
