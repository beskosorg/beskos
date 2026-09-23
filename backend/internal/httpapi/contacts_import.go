package httpapi

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Contact import/export in vCard 3.0 and CSV — the universal address-book formats (Google, Apple,
// and Outlook export/import both). Works on top of the current model (one email/phone per contact).

// handleExportContacts returns all of the owner's contacts as vCard (default) or CSV.
func (s *Server) handleExportContacts(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	rows, err := s.q.ListContacts(r.Context(), db.ListContactsParams{OwnerUserID: mu.ID, TenantID: mu.TenantID})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "contacts.failed")
		return
	}
	// Only the owner's own (don't export contacts seen through someone else's sharing).
	own := rows[:0]
	for _, c := range rows {
		if c.OwnerUserID == mu.ID {
			own = append(own, c)
		}
	}
	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="contacts.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"name", "email", "phone", "company", "title", "notes"})
		for _, c := range own {
			_ = cw.Write([]string{c.Name, c.Email, c.Phone, c.Company, c.Title, c.Notes})
		}
		cw.Flush()
		return
	}
	w.Header().Set("Content-Type", "text/vcard; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="contacts.vcf"`)
	var b bytes.Buffer
	for _, c := range own {
		writeVCard(&b, vcardOf(c.Name, c.Email, c.Phone, c.Mobile, c.Company, c.Title, c.Notes))
	}
	_, _ = w.Write(b.Bytes())
}

// vcardFields are the fields we know how to serialize into vCard, independent of the row type
// (list, get…). vcardOf builds one from loose fields.
type vcardFields struct{ Name, Email, Phone, Mobile, Company, Title, Notes string }

func vcardOf(name, email, phone, mobile, company, title, notes string) vcardFields {
	return vcardFields{Name: name, Email: email, Phone: phone, Mobile: mobile, Company: company, Title: title, Notes: notes}
}

// writeVCard serializes a contact as vCard 3.0. Escapes comma/semicolon/line-break per the RFC.
func writeVCard(b *bytes.Buffer, c vcardFields) {
	esc := func(s string) string {
		s = strings.ReplaceAll(s, "\\", "\\\\")
		s = strings.ReplaceAll(s, "\n", "\\n")
		s = strings.ReplaceAll(s, ",", "\\,")
		s = strings.ReplaceAll(s, ";", "\\;")
		return s
	}
	b.WriteString("BEGIN:VCARD\r\nVERSION:3.0\r\n")
	fmt.Fprintf(b, "FN:%s\r\n", esc(c.Name))
	if c.Email != "" {
		fmt.Fprintf(b, "EMAIL;TYPE=INTERNET:%s\r\n", esc(c.Email))
	}
	if c.Phone != "" {
		fmt.Fprintf(b, "TEL:%s\r\n", esc(c.Phone))
	}
	if c.Mobile != "" {
		fmt.Fprintf(b, "TEL;TYPE=CELL:%s\r\n", esc(c.Mobile))
	}
	if c.Company != "" {
		fmt.Fprintf(b, "ORG:%s\r\n", esc(c.Company))
	}
	if c.Title != "" {
		fmt.Fprintf(b, "TITLE:%s\r\n", esc(c.Title))
	}
	if c.Notes != "" {
		fmt.Fprintf(b, "NOTE:%s\r\n", esc(c.Notes))
	}
	b.WriteString("END:VCARD\r\n")
}

type importedContact struct {
	Name, Email, Phone, Company, Title, Notes string
}

// handleImportContacts receives a file (multipart "file") in vCard or CSV and creates the contacts.
// Dedup by email (doesn't recreate a contact whose email already exists in the owner's address
// book). Returns the totals.
func (s *Server) handleImportContacts(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.import_no_file")
		return
	}
	defer file.Close()
	data := make([]byte, 0, header.Size)
	buf := bytes.NewBuffer(data)
	if _, err := buf.ReadFrom(file); err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.import_failed")
		return
	}
	var list []importedContact
	if strings.HasSuffix(strings.ToLower(header.Filename), ".csv") {
		list, err = parseCSVContacts(buf.Bytes())
	} else {
		list = parseVCards(buf.Bytes())
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, "contacts.import_failed")
		return
	}
	// Dedup: by email when there is one, otherwise by name (contacts without an email, like
	// companies, would otherwise re-import every time). The key is lowercased to match differences
	// that are only in casing.
	dedupKey := func(name, email string) string {
		if email != "" {
			return "e:" + strings.ToLower(email)
		}
		return "n:" + strings.ToLower(strings.TrimSpace(name))
	}
	existing := map[string]bool{}
	if rows, err := s.q.ListContacts(r.Context(), db.ListContactsParams{OwnerUserID: mu.ID, TenantID: mu.TenantID}); err == nil {
		for _, c := range rows {
			if c.OwnerUserID == mu.ID {
				existing[dedupKey(c.Name, c.Email)] = true
			}
		}
	}
	imported, skipped := 0, 0
	for _, ic := range list {
		if strings.TrimSpace(ic.Name) == "" && ic.Email == "" {
			continue
		}
		if ic.Name == "" {
			ic.Name = ic.Email // no FN: uses the email as the name
		}
		key := dedupKey(ic.Name, ic.Email)
		if existing[key] {
			skipped++
			continue
		}
		if _, err := s.q.CreateContact(r.Context(), db.CreateContactParams{
			TenantID: mu.TenantID, OwnerUserID: mu.ID, Kind: "person", Name: strings.TrimSpace(ic.Name),
			Email: ic.Email, Phone: ic.Phone, Company: ic.Company, Notes: ic.Notes, Title: ic.Title,
			Tags: []string{}, ExtraEmails: []string{}, ExtraPhones: []string{},
			AllowPhone: true, AllowEmail: true, CustomFields: json.RawMessage("{}"), // jsonb NOT NULL
		}); err != nil {
			continue
		}
		existing[key] = true
		imported++
	}
	writeJSON(w, http.StatusOK, map[string]int{"imported": imported, "skipped": skipped})
}

// parseCSVContacts reads a CSV with a header and maps known columns (name/email/phone/company/
// title/notes) by name — column order doesn't matter.
func parseCSVContacts(data []byte) ([]importedContact, error) {
	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord = -1 // rows with a variable number of fields don't break
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	col := map[string]int{}
	for i, h := range records[0] {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	at := func(rec []string, key string) string {
		if i, ok := col[key]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	out := make([]importedContact, 0, len(records)-1)
	for _, rec := range records[1:] {
		out = append(out, importedContact{
			Name: at(rec, "name"), Email: at(rec, "email"), Phone: at(rec, "phone"),
			Company: at(rec, "company"), Title: at(rec, "title"), Notes: at(rec, "notes"),
		})
	}
	return out, nil
}

// parseVCards reads one or several VCARDs and extracts the fields our model supports. Unfolds
// lines (continuation starts with a space/tab) and handles the basic vCard unescaping.
func parseVCards(data []byte) []importedContact {
	var out []importedContact
	var cur *importedContact
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var logical string
	flush := func(line string) {
		if line == "" {
			return
		}
		up := strings.ToUpper(line)
		switch {
		case up == "BEGIN:VCARD":
			cur = &importedContact{}
		case up == "END:VCARD":
			if cur != nil {
				out = append(out, *cur)
				cur = nil
			}
		case cur != nil:
			applyVCardLine(cur, line)
		}
	}
	for sc.Scan() {
		raw := strings.TrimRight(sc.Text(), "\r")
		if len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\t') { // continuation of the previous line
			logical += raw[1:]
			continue
		}
		flush(logical)
		logical = raw
	}
	flush(logical)
	return out
}

func applyVCardLine(c *importedContact, line string) {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return
	}
	name, value := line[:colon], unescapeVCard(line[colon+1:])
	prop := strings.ToUpper(name)
	if i := strings.IndexByte(prop, ';'); i >= 0 {
		prop = prop[:i] // discards the parameters (TYPE=...)
	}
	switch prop {
	case "FN":
		c.Name = value
	case "EMAIL":
		if c.Email == "" {
			c.Email = value
		}
	case "TEL":
		c.Phone = firstNonEmpty(c.Phone, value)
	case "ORG":
		c.Company = strings.TrimRight(value, ";") // ORG can come as "Company;Department"
	case "TITLE":
		c.Title = value
	case "NOTE":
		c.Notes = value
	}
}

func unescapeVCard(s string) string {
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\N", "\n")
	s = strings.ReplaceAll(s, "\\,", ",")
	s = strings.ReplaceAll(s, "\\;", ";")
	s = strings.ReplaceAll(s, "\\\\", "\\")
	return s
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// handleContactVCard returns ONE contact as vCard 3.0 — the webmail uses this to attach the card
// to an email ("send contact"). Scoped by contactOr404 (tenant + owner/sharing), like the rest.
func (s *Server) handleContactVCard(w http.ResponseWriter, r *http.Request) {
	c, ok := s.contactOr404(w, r)
	if !ok {
		return
	}
	var b bytes.Buffer
	writeVCard(&b, vcardOf(c.Name, c.Email, c.Phone, c.Mobile, c.Company, c.Title, c.Notes))
	w.Header().Set("Content-Type", "text/vcard; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+vcardFilename(c.Name)+`"`)
	_, _ = w.Write(b.Bytes())
}

// vcardFilename generates a safe filename from the contact's name (not accent-stripping, e.g.
// "Zoe Doe" -> "Zoe-Doe.vcf"; we keep it simple: swaps whatever doesn't fit for "-").
func vcardFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "contact.vcf"
	}
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		case r == ' ':
			return '-'
		default:
			return -1
		}
	}, name)
	if safe == "" {
		safe = "contact"
	}
	return safe + ".vcf"
}
