package httpapi

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Import/export of events in iCalendar (RFC 5545) — the universal calendar format (Google, Apple,
// Outlook). Covers the essentials: VEVENT with date/time, all-day, title, location, and description.

const icsWide = 100 * 365 * 24 * time.Hour // "everything" window to export the entire calendar

func (s *Server) handleExportEvents(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	now := time.Now()
	events, err := s.q.ListEventsRange(r.Context(), db.ListEventsRangeParams{
		OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, EndsAt: now.Add(-icsWide), StartsAt: now.Add(icsWide),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "events.failed")
		return
	}
	var b bytes.Buffer
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Beskos//Calendar//EN\r\nCALSCALE:GREGORIAN\r\n")
	stamp := now.UTC().Format("20060102T150405Z")
	for _, e := range events {
		writeVEvent(&b, e, stamp)
	}
	b.WriteString("END:VCALENDAR\r\n")
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="calendar.ics"`)
	_, _ = w.Write(b.Bytes())
}

func icsEsc(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	return s
}

func writeVEvent(b *bytes.Buffer, e db.Event, stamp string) {
	b.WriteString("BEGIN:VEVENT\r\n")
	fmt.Fprintf(b, "UID:%s@mailin\r\n", e.ID)
	fmt.Fprintf(b, "DTSTAMP:%s\r\n", stamp)
	if e.AllDay {
		// All-day: DATE with no time. DTEND is EXCLUSIVE in iCal → end + 1 day.
		fmt.Fprintf(b, "DTSTART;VALUE=DATE:%s\r\n", e.StartsAt.UTC().Format("20060102"))
		fmt.Fprintf(b, "DTEND;VALUE=DATE:%s\r\n", e.EndsAt.UTC().AddDate(0, 0, 1).Format("20060102"))
	} else {
		fmt.Fprintf(b, "DTSTART:%s\r\n", e.StartsAt.UTC().Format("20060102T150405Z"))
		fmt.Fprintf(b, "DTEND:%s\r\n", e.EndsAt.UTC().Format("20060102T150405Z"))
	}
	fmt.Fprintf(b, "SUMMARY:%s\r\n", icsEsc(e.Title))
	if e.Location != "" {
		fmt.Fprintf(b, "LOCATION:%s\r\n", icsEsc(e.Location))
	}
	if e.Description != "" {
		fmt.Fprintf(b, "DESCRIPTION:%s\r\n", icsEsc(e.Description))
	}
	b.WriteString("END:VEVENT\r\n")
}

type icsEvent struct {
	Title, Location, Description string
	Start, End                   time.Time
	AllDay                       bool
	ok                           bool
}

func (s *Server) handleImportEvents(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "events.import_no_file")
		return
	}
	defer file.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(file); err != nil {
		writeErr(w, http.StatusBadRequest, "events.import_failed")
		return
	}
	imported := 0
	for _, ev := range parseICS(buf.Bytes()) {
		if !ev.ok || ev.Title == "" {
			continue
		}
		if ev.End.Before(ev.Start) || ev.End.Equal(ev.Start) {
			ev.End = ev.Start.Add(time.Hour)
		}
		if _, err := s.q.CreateEvent(r.Context(), db.CreateEventParams{
			TenantID: mu.TenantID, OwnerUserID: pgtype.UUID{Bytes: mu.ID, Valid: true},
			Title: ev.Title, Description: ev.Description, Location: ev.Location, Color: "#116466",
			StartsAt: ev.Start, EndsAt: ev.End, Tags: []string{}, RemindMinutes: 10, AllDay: ev.AllDay,
		}); err != nil {
			continue
		}
		imported++
	}
	writeJSON(w, http.StatusOK, map[string]int{"imported": imported})
}

// parseICS extracts the VEVENTs from an iCalendar file. Unfolds lines (continuation starts with a
// space/tab), splits property/params/value, and handles dates with/without time.
func parseICS(data []byte) []icsEvent {
	var out []icsEvent
	var cur *icsEvent
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var logical string
	handle := func(line string) {
		if line == "" {
			return
		}
		up := strings.ToUpper(line)
		switch {
		case up == "BEGIN:VEVENT":
			cur = &icsEvent{ok: true}
		case up == "END:VEVENT":
			if cur != nil {
				out = append(out, *cur)
				cur = nil
			}
		case cur != nil:
			applyICSLine(cur, line)
		}
	}
	for sc.Scan() {
		raw := strings.TrimRight(sc.Text(), "\r")
		if len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\t') {
			logical += raw[1:]
			continue
		}
		handle(logical)
		logical = raw
	}
	handle(logical)
	return out
}

func applyICSLine(e *icsEvent, line string) {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return
	}
	nameParams, value := line[:colon], icsUnesc(line[colon+1:])
	parts := strings.Split(nameParams, ";")
	prop := strings.ToUpper(parts[0])
	isDate := false
	for _, p := range parts[1:] {
		if strings.EqualFold(p, "VALUE=DATE") {
			isDate = true
		}
	}
	switch prop {
	case "SUMMARY":
		e.Title = value
	case "LOCATION":
		e.Location = value
	case "DESCRIPTION":
		e.Description = value
	case "DTSTART":
		if t, allDay, ok := parseICSTime(value, isDate); ok {
			e.Start = t
			e.AllDay = allDay
		}
	case "DTEND":
		if t, _, ok := parseICSTime(value, isDate); ok {
			// All-day DTEND is EXCLUSIVE → step back 1 day for our inclusive end.
			if isDate {
				t = t.AddDate(0, 0, -1)
			}
			e.End = t
		}
	}
}

// parseICSTime reads the common formats: 20300310T140000Z (UTC), 20300310T140000 (floating→UTC), and
// 20300310 (DATE, all-day). Ignores TZID (treats it as UTC) — enough for the essentials.
func parseICSTime(v string, isDate bool) (time.Time, bool, bool) {
	if isDate || (len(v) == 8 && !strings.Contains(v, "T")) {
		if t, err := time.Parse("20060102", v); err == nil {
			return t.UTC(), true, true
		}
		return time.Time{}, false, false
	}
	for _, layout := range []string{"20060102T150405Z", "20060102T150405"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC(), false, true
		}
	}
	return time.Time{}, false, false
}

func icsUnesc(s string) string {
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\N", "\n")
	s = strings.ReplaceAll(s, "\\,", ",")
	s = strings.ReplaceAll(s, "\\;", ";")
	s = strings.ReplaceAll(s, "\\\\", "\\")
	return s
}
