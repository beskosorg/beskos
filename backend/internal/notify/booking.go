package notify

import "fmt"

// BookingConfirmed builds the subject and plain-text body sent to whoever just booked a meeting
// slot on a public booking page. The booker has no stored language preference (they are not a
// registered user), so this follows the host's language -- the same fallback the rest of this
// package uses when the recipient's own preference isn't known.
// BookingEvent builds the title and description for the calendar event created from a public
// booking. It follows the host's language -- the same fallback BookingConfirmed uses.
func BookingEvent(lang, pageTitle, name, email, phone, company, note string) (title, desc string) {
	title = pageTitle
	if title == "" {
		titlePT := "Reunião" // pt-BR default meeting title -- lang:ok
		title = tr(lang, titlePT, "Meeting")
	}
	title += " · " + name

	labelPT := "Reserva pela página pública.\nNome: %s\nE-mail: %s" // lang:ok
	labelEN := "Booked from the public page.\nName: %s\nEmail: %s"
	desc = fmt.Sprintf(tr(lang, labelPT, labelEN), name, email)
	if phone != "" {
		phonePT := "\nTelefone: " // lang:ok
		desc += tr(lang, phonePT, "\nPhone: ") + phone
	}
	if company != "" {
		companyPT := "\nEmpresa: " // lang:ok
		desc += tr(lang, companyPT, "\nCompany: ") + company
	}
	if note != "" {
		notePT := "\nNota: " // lang:ok
		desc += tr(lang, notePT, "\nNote: ") + note
	}
	return title, desc
}

func BookingConfirmed(lang, title, who, when string, durationMin int, meetURL string) (subject, body string) {
	subjectPT := "Reunião confirmada: " + title // pt-BR subject line -- lang:ok
	subject = tr(lang, subjectPT, "Meeting confirmed: "+title)
	bodyPT := "A sua reunião com %s está confirmada.\r\nQuando: %s\r\nDuração: %d min\r\n" // lang:ok
	bodyEN := "Your meeting with %s is confirmed.\r\nWhen: %s\r\nDuration: %d min\r\n"
	body = fmt.Sprintf(tr(lang, bodyPT, bodyEN), who, when, durationMin)
	if meetURL != "" {
		videoLabelPT := "Videochamada: " // lang:ok
		body += tr(lang, videoLabelPT, "Video call: ") + meetURL + "\r\n"
	}
	return subject, body
}
