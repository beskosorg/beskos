package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// NewEvent is an event to create on the AUTHENTICATED user's calendar. No owner or tenant in the
// fields: they come from the Identity, never from the model's args (§6.2).
type NewEvent struct {
	Title       string
	Description string
	Location    string
	StartsAt    time.Time
	EndsAt      time.Time
	// CreateMeet attaches a video-call room (Meet) to the event and returns its path.
	CreateMeet bool
	// Invitees are e-mail addresses of COLLEAGUES to invite (RSVP). The handler resolves each one
	// to a user in the SAME tenant; external addresses cannot be invited on the calendar (they
	// are notified by e-mail instead). Inviting notifies another person: the tool confirms when
	// this list isn't empty.
	Invitees []string
}

// EventRef is the created event, as the action card shows it.
type EventRef struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	StartsAt string `json:"starts_at"`
	MeetURL  string `json:"meet_url,omitempty"`
}

// EventCreate creates the event with the SAME queries as the interface's handler (§5: no new SQL).
type EventCreate func(ctx context.Context, id Identity, ev NewEvent) (EventRef, error)

// calendarEventArgs are the fields the model fills in. Times are RFC3339 — the system prompt
// gives it the present time to resolve relative dates ("tomorrow at 10am").
type calendarEventArgs struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Location    string   `json:"location"`
	StartsAt    string   `json:"starts_at"`
	EndsAt      string   `json:"ends_at"`
	CreateMeet  bool     `json:"create_meet"`
	Invitees    []string `json:"invitees"`
}

// hasInvitees reports whether these args ask to invite someone. Decides the tool's confirmation
// (§6.3): inviting notifies another person. Unreadable args → true (fail-closed).
func hasInvitees(raw json.RawMessage) bool {
	var a calendarEventArgs
	if err := decodeArgs(raw, &a); err != nil {
		return true
	}
	for _, e := range a.Invitees {
		if strings.TrimSpace(e) != "" {
			return true
		}
	}
	return false
}

func calendarTools(create EventCreate) []Tool {
	return []Tool{
		{
			Name: ToolCalendarCreate,
			Description: "Create an event on the signed-in user's calendar. Times are RFC3339. Pass " +
				"invitees (email addresses of colleagues in the same organisation) to invite people — " +
				"they get an in-app invitation and RSVP. When the user asks for a meeting WITH someone, " +
				"add them to invitees, do not just put their name in the title. External addresses " +
				"cannot be invited to the calendar; tell those guests with " + ToolMailSend + " instead. " +
				"Set create_meet to attach a video-call link.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"title": {"type": "string", "description": "Title of the event."},
					"description": {"type": "string"},
					"location": {"type": "string"},
					"starts_at": {"type": "string", "description": "Start time, RFC3339 (e.g. 2026-07-22T10:00:00+02:00)."},
					"ends_at": {"type": "string", "description": "End time, RFC3339. Omit for a one-hour event."},
					"invitees": {"type": "array", "items": {"type": "string"}, "description": "Email addresses of colleagues to invite (RSVP). Come from the user or their contacts."},
					"create_meet": {"type": "boolean", "description": "Attach a video-call (Meet) link to the event."}
				},
				"required": ["title", "starts_at"]
			}`),
			Writes: true,
			// Solo, the event is internal (no confirmation). WITH guests, it notifies another
			// person — inviting → §6.3 — so it confirms the content in view. ConfirmWhen decides
			// per call.
			ConfirmWhen: hasInvitees,
			Handler: func(ctx context.Context, id Identity, raw json.RawMessage) (Result, error) {
				var a calendarEventArgs
				if err := decodeArgs(raw, &a); err != nil {
					return Result{}, err
				}
				a.Title = strings.TrimSpace(a.Title)
				if a.Title == "" {
					return Result{}, fmt.Errorf("title is required")
				}
				starts, err := time.Parse(time.RFC3339, strings.TrimSpace(a.StartsAt))
				if err != nil {
					return Result{}, fmt.Errorf("starts_at must be an RFC3339 time: %w", err)
				}
				// Without an explicit end, one hour — the interface's default for events with no
				// set duration.
				ends := starts.Add(time.Hour)
				if s := strings.TrimSpace(a.EndsAt); s != "" {
					ends, err = time.Parse(time.RFC3339, s)
					if err != nil {
						return Result{}, fmt.Errorf("ends_at must be an RFC3339 time: %w", err)
					}
				}
				if !ends.After(starts) {
					return Result{}, fmt.Errorf("the event must end after it starts")
				}
				ref, err := create(ctx, id, NewEvent{
					Title:       a.Title,
					Description: strings.TrimSpace(a.Description),
					Location:    strings.TrimSpace(a.Location),
					StartsAt:    starts,
					EndsAt:      ends,
					CreateMeet:  a.CreateMeet,
					Invitees:    cleanAddrs(a.Invitees),
				})
				if err != nil {
					return Result{}, fmt.Errorf("event creation failed: %w", err)
				}
				return Result{
					Summary: fmt.Sprintf("%s — %s", a.Title, starts.Format("2 Jan 15:04")),
					Data:    ref,
				}, nil
			},
		},
	}
}
