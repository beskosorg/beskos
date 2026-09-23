package notify

import (
	"strings"
	"testing"
)

func TestBookingConfirmedFollowsTheHostLanguage(t *testing.T) {
	subjectPT, bodyPT := BookingConfirmed("pt-BR", "Weekly sync", "Ana", "02/01/2006 15:04", 30, "https://beskos.com/j/room")
	if !strings.Contains(subjectPT, "confirmada") || !strings.Contains(bodyPT, "confirmada") {
		t.Errorf("pt-BR subject/body = %q / %q, want the Portuguese wording", subjectPT, bodyPT)
	}
	if !strings.Contains(bodyPT, "https://beskos.com/j/room") {
		t.Error("the meeting link must be in the body")
	}

	subjectEN, bodyEN := BookingConfirmed("en", "Weekly sync", "Ana", "02/01/2006 15:04", 30, "")
	if !strings.Contains(subjectEN, "confirmed") || !strings.Contains(bodyEN, "confirmed") {
		t.Errorf("en subject/body = %q / %q, want the English wording", subjectEN, bodyEN)
	}
	if strings.Contains(bodyEN, "Video call") {
		t.Error("an empty meetURL must not add a video-call line")
	}
}

func TestBookingEventFollowsTheHostLanguage(t *testing.T) {
	titlePT, descPT := BookingEvent("pt-BR", "", "Ana", "ana@example.com", "555", "Acme", "call me back")
	if !strings.Contains(titlePT, "Reunião") { // pt-BR default title text under test -- lang:ok
		t.Errorf("pt-BR default title = %q, want the Portuguese wording", titlePT)
	}
	if !strings.Contains(descPT, "Telefone") || !strings.Contains(descPT, "Empresa") || !strings.Contains(descPT, "Nota") {
		t.Errorf("pt-BR description = %q, want the Portuguese labels", descPT)
	}

	titleEN, descEN := BookingEvent("en", "Weekly sync", "Ana", "ana@example.com", "", "", "")
	if titleEN != "Weekly sync · Ana" {
		t.Errorf("en title = %q, want the page title kept as-is", titleEN)
	}
	if strings.Contains(descEN, "Phone") || strings.Contains(descEN, "Company") || strings.Contains(descEN, "Note") {
		t.Error("empty optional fields must not add their label lines")
	}
	if !strings.Contains(descEN, "Booked from the public page") {
		t.Errorf("en description = %q, want the English wording", descEN)
	}
}
