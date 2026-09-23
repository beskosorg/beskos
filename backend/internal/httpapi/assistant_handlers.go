package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/assistant"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/mailbox"
	"github.com/beskosorg/beskos/backend/internal/office"
)

// buildAssistant assembles the agentic engine. It's called at startup; the dependencies are
// closures over the Server, so they read the optional fields (storage, encryption) at the time of
// use rather than here — which allows building the engine before the Enable* calls run.
func (s *Server) buildAssistant() *assistant.Engine {
	reg := assistant.BuildRegistry(assistant.Deps{
		Mail:          s.assistantMailAccess,
		Drive:         s.assistantDriveSearch,
		Docs:          s.assistantDocsCreate,
		DocsRead:      s.assistantDocsRead,
		DocsEdit:      s.assistantDocsEdit,
		DocsShare:     s.assistantDocsShare,
		Tasks:         s.assistantTaskCreate,
		Calendar:      s.assistantCalendarCreate,
		Contacts:      s.assistantContactsSearch,
		ContactCreate: s.assistantContactCreate,
		ContactShare:  s.assistantContactShare,
		// Wiki (§7.5): read + write-with-confirmation, always through the same visible surface of the service.
		WikiSearch:     s.assistantWikiSearch,
		WikiRead:       s.assistantWikiRead,
		WikiCreatePage: s.assistantWikiCreate,
		WikiEditPage:   s.assistantWikiEdit,
		// Notes: private by default, so each adapter re-checks NoteRole for the caller.
		NotesSearch: s.assistantNotesSearch,
		NotesRead:   s.assistantNotesRead,
		NotesCreate: s.assistantNotesCreate,
		NotesEdit:   s.assistantNotesEdit,
	})
	// The Confirmer is what makes `mail.send` executable at all: without it the engine refuses
	// everything that goes outbound, instead of letting it through (§6.3, fail-closed).
	return assistant.NewEngine(s.ai, reg, assistantAudit{q: s.q}, assistant.DefaultMaxSteps).
		WithConfirmer(s.confirms)
}

// assistantMailAccess opens the AUTHENTICATED user's mailbox. The (user, tenant) pair comes from
// the Identity, which in turn comes from the session — never from model-supplied arguments. The
// query filters by tenant on purpose: even with a correct user_id, the wrong tenant returns nothing.
func (s *Server) assistantMailAccess(ctx context.Context, id assistant.Identity) (assistant.MailBox, error) {
	mu, err := s.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: id.UserID, TenantID: id.TenantID})
	if err != nil {
		return nil, fmt.Errorf("mail user lookup: %w", err)
	}
	cred, ok := s.decryptCred(mu.JmapCred)
	if !ok {
		return nil, fmt.Errorf("mailbox credential unavailable")
	}
	// Wraps the mailbox so that, on send, it records the recipients as contacts — the same thing
	// the webmail does via /contacts/seen, but on the server side because the assistant sends from
	// here.
	return recordingMailBox{MailBox: mailbox.NewUserClient(s.sw.BaseURL(), cred), s: s, id: id}, nil
}

// recordingMailBox is the assistant's mailbox that, besides sending, records the recipients as
// contacts. Only SendEmail changes behavior; everything else delegates. An error recording
// contacts doesn't affect the send.
type recordingMailBox struct {
	assistant.MailBox
	s  *Server
	id assistant.Identity
}

func (b recordingMailBox) SendEmail(ctx context.Context, d mailbox.Draft) (string, error) {
	// The plan's daily send limit also applies to the assistant.
	rcpts := len(d.To) + len(d.Cc)
	if !b.s.mailSendAllowed(ctx, b.id.TenantID, rcpts) {
		return "", fmt.Errorf("daily send limit reached for this plan")
	}
	emailID, err := b.MailBox.SendEmail(ctx, d)
	if err == nil {
		b.s.recordMailSent(ctx, b.id.TenantID, rcpts)
		b.s.recordRecipientContacts(ctx, b.id.TenantID, b.id.UserID, append(append([]string{}, d.To...), d.Cc...))
	}
	return emailID, err
}

// assistantDriveSearch reuses the SAME queries as the Drive search in the UI — including the LIKE
// wildcard escaping. No new SQL (§5): what the assistant sees is what the user sees.
func (s *Server) assistantDriveSearch(ctx context.Context, id assistant.Identity, query string, limit int) ([]assistant.DriveHit, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	owner := pgtype.UUID{Bytes: id.UserID, Valid: true}
	pattern := likePattern(query)
	files, err := s.q.SearchStorageFiles(ctx, db.SearchStorageFilesParams{OwnerUserID: owner, Name: pattern})
	if err != nil {
		return nil, fmt.Errorf("file search: %w", err)
	}
	folders, err := s.q.SearchStorageFolders(ctx, db.SearchStorageFoldersParams{OwnerUserID: id.UserID, Name: pattern})
	if err != nil {
		return nil, fmt.Errorf("folder search: %w", err)
	}
	// Folders outside the assistant's reach (§6.6): whatever is in an excluded folder doesn't show
	// up in search, and the excluded folders themselves don't either. This is the valve that
	// offsets the broad-by-default reach (decision §12.1).
	excluded := s.assistantExcludedFolders(ctx, id.TenantID)
	out := make([]assistant.DriveHit, 0, limit)
	for _, f := range files {
		if len(out) >= limit {
			break
		}
		if f.FolderID.Valid && excluded[uuidText(f.FolderID)] {
			continue
		}
		hit := assistant.DriveHit{ID: f.ID.String(), Name: f.Name, Kind: "file", Size: f.Size}
		if f.FolderID.Valid {
			hit.Folder = uuidText(f.FolderID)
		}
		out = append(out, hit)
	}
	for _, f := range folders {
		if len(out) >= limit {
			break
		}
		if excluded[f.ID.String()] {
			continue
		}
		out = append(out, assistant.DriveHit{ID: f.ID.String(), Name: f.Name, Kind: "folder"})
	}
	return out, nil
}

// uuidText prints a pgtype.UUID. It exists because nullable UUID columns arrive in this shape.
func uuidText(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// assistantAudit persists the audit record (§6.4). Failing here doesn't abort the run — the engine
// handles that —, but it goes into the server log because losing an audit record is serious.
type assistantAudit struct{ q *db.Queries }

func (a assistantAudit) Record(ctx context.Context, e assistant.AuditEntry) error {
	args := e.Args
	if len(args) == 0 || !json.Valid(args) {
		args = json.RawMessage("{}")
	}
	// An empty conversation stays NULL: a run with no thread doesn't invent a zero id.
	conv := pgtype.UUID{}
	if e.ConversationID != (uuid.UUID{}) {
		conv = pgtype.UUID{Bytes: e.ConversationID, Valid: true}
	}
	return a.q.RecordAssistantToolCall(ctx, db.RecordAssistantToolCallParams{
		TenantID:       e.Identity.TenantID,
		MailUserID:     e.Identity.UserID,
		RunID:          e.RunID,
		ConversationID: conv,
		Agent:          e.Agent,
		Tool:           e.Tool,
		Args:           args,
		Result:         e.Result,
		Error:          e.Error,
		DurationMs:     e.DurationMs,
	})
}

// assistantTaskCreate creates a task with the SAME queries as the card-creation handler (§5: no
// new SQL). The board and column are resolved here, not by the model: the model doesn't know ids,
// and forcing it to supply one would be asking it to guess.
func (s *Server) assistantTaskCreate(ctx context.Context, id assistant.Identity, t assistant.NewTask) (assistant.TaskRef, error) {
	boards, err := s.q.ListBoards(ctx, db.ListBoardsParams{TenantID: id.TenantID, UserID: pgtype.UUID{Bytes: id.UserID, Valid: true}})
	if err != nil {
		return assistant.TaskRef{}, fmt.Errorf("board lookup: %w", err)
	}
	if len(boards) == 0 {
		return assistant.TaskRef{}, fmt.Errorf("this organisation has no board yet")
	}
	board := boards[0]
	if want := strings.TrimSpace(t.Board); want != "" {
		found := false
		for _, b := range boards {
			if strings.EqualFold(b.Name, want) {
				board, found = b, true
				break
			}
		}
		if !found {
			names := make([]string, 0, len(boards))
			for _, b := range boards {
				names = append(names, b.Name)
			}
			// List the boards in the error: the model corrects itself on the first try instead of
			// guessing blindly.
			return assistant.TaskRef{}, fmt.Errorf("no board named %q; available boards: %s", want, strings.Join(names, ", "))
		}
	}

	cols, err := s.q.ListColumns(ctx, board.ID)
	if err != nil {
		return assistant.TaskRef{}, fmt.Errorf("column lookup: %w", err)
	}
	if len(cols) == 0 {
		return assistant.TaskRef{}, fmt.Errorf("board %q has no column", board.Name)
	}

	// Per-board number: the board's sequence is the source of truth, same as in the handler.
	seq, err := s.q.NextCardNumber(ctx, board.ID)
	if err != nil {
		return assistant.TaskRef{}, fmt.Errorf("card number: %w", err)
	}
	// Assignee: "task for Bob" must assign to the actual Bob — resolve the name/email against the
	// tenant's colleagues. Ambiguous or unknown → a clear error (the model asks or corrects)
	// instead of silently creating an unowned task.
	assigneeID := pgtype.UUID{}
	assigneeName := ""
	if want := strings.TrimSpace(t.Assignee); want != "" {
		cols2, cerr := s.q.ListColleagues(ctx, id.TenantID)
		if cerr != nil {
			return assistant.TaskRef{}, fmt.Errorf("colleague lookup: %w", cerr)
		}
		var matches []db.ListColleaguesRow
		for _, c := range cols2 {
			if strings.EqualFold(c.Email, want) || strings.EqualFold(c.Name, want) {
				matches = []db.ListColleaguesRow{c}
				break
			}
			if strings.Contains(strings.ToLower(c.Name), strings.ToLower(want)) ||
				strings.Contains(strings.ToLower(c.Email), strings.ToLower(want)) {
				matches = append(matches, c)
			}
		}
		if len(matches) == 0 {
			return assistant.TaskRef{}, fmt.Errorf("no colleague matching %q in this organisation", want)
		}
		if len(matches) > 1 {
			opts := make([]string, 0, len(matches))
			for _, m := range matches {
				opts = append(opts, m.Email)
			}
			return assistant.TaskRef{}, fmt.Errorf("ambiguous assignee %q: %s — use the email", want, strings.Join(opts, ", "))
		}
		assigneeID = pgtype.UUID{Bytes: matches[0].ID, Valid: true}
		assigneeName = matches[0].Name
		if assigneeName == "" {
			assigneeName = matches[0].Email
		}
	}
	card, err := s.q.CreateCard(ctx, db.CreateCardParams{
		TenantID: id.TenantID, BoardID: board.ID, ColumnID: cols[0].ID,
		Title: t.Title, Description: t.Description, Due: t.Due,
		Priority:   normalizePriority(""),
		Position:   0,
		AssigneeID: assigneeID,
		CreatedBy:  pgtype.UUID{Bytes: id.UserID, Valid: true},
		Number:     seq.CardSeq,
	})
	if err != nil {
		return assistant.TaskRef{}, fmt.Errorf("card creation: %w", err)
	}
	// Notifies the assignee (same path as the manual handler): bell + push.
	if actor, aerr := s.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: id.UserID, TenantID: id.TenantID}); aerr == nil {
		s.notifyTask(ctx, actor, card, taskWatchers(card), "New task", card.Title)
	}
	s.logCard(ctx, id.TenantID, card.ID, id.UserID, "created", "")
	s.publishBoard(ctx, id.TenantID, card.BoardID)
	return assistant.TaskRef{
		ID:       card.ID.String(),
		Board:    board.Name,
		Number:   fmt.Sprintf("%s-%d", seq.Prefix, seq.CardSeq),
		Assignee: assigneeName,
	}, nil
}

// assistantCalendarCreate creates an event on the AUTHENTICATED user's calendar with the SAME
// queries as the UI handler (§5: no new SQL). The (user, tenant) pair comes from the Identity — the
// model never picks the calendar (§6.2). It doesn't raise a conflict warning: there's no modal in
// chat to confirm it, and the user explicitly asked to create it.
func (s *Server) assistantCalendarCreate(ctx context.Context, id assistant.Identity, ev assistant.NewEvent) (assistant.EventRef, error) {
	mu, err := s.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: id.UserID, TenantID: id.TenantID})
	if err != nil {
		return assistant.EventRef{}, fmt.Errorf("user lookup: %w", err)
	}
	recurN, recurU := recurValues("", "")
	event, err := s.q.CreateEvent(ctx, db.CreateEventParams{
		TenantID: id.TenantID, OwnerUserID: pgtype.UUID{Bytes: id.UserID, Valid: true},
		Title: ev.Title, Description: ev.Description, Location: ev.Location, Color: "#116466",
		StartsAt: ev.StartsAt, EndsAt: ev.EndsAt, SourceEmailID: "", Tags: []string{},
		RemindMinutes: remindOrDefault(nil), AllDay: false,
		Recur: recurN, RecurUntil: recurU, CalendarID: s.calendarID(ctx, mu, ""),
	})
	if err != nil {
		return assistant.EventRef{}, fmt.Errorf("event creation: %w", err)
	}
	// Invitees: resolves each email to a COLLEAGUE in the same tenant. Externals (e.g. outside the
	// organisation) aren't invitable on the calendar — ignored here, and it's communicated by email
	// instead. syncInvites revalidates tenant ownership by UUID, but we already filter by tenant so
	// we don't trust blindly.
	if len(ev.Invitees) > 0 {
		var ids []string
		for _, email := range ev.Invitees {
			u, err := s.q.GetMailUserByEmailInTenant(ctx, db.GetMailUserByEmailInTenantParams{Email: strings.TrimSpace(email), TenantID: id.TenantID})
			if err != nil {
				continue
			}
			ids = append(ids, u.ID.String())
		}
		s.syncInvites(ctx, event, mu, ids)
	}
	ref := assistant.EventRef{
		ID: event.ID.String(), Title: event.Title, StartsAt: event.StartsAt.Format(time.RFC3339),
	}
	// Video room only if requested. Returns an ABSOLUTE guest URL (/j/<room>) so it can travel in
	// an email and be clickable outside the app; without PUBLIC_URL configured, it falls back to
	// the relative path.
	if ev.CreateMeet {
		if room := s.attachMeetToEvent(ctx, mu, event.ID, ev.Title, ev.StartsAt, ev.EndsAt); room != "" {
			if s.publicURL != "" {
				ref.MeetURL = s.publicURL + "/j/" + room
			} else {
				ref.MeetURL = "/j/" + room
			}
		}
	}
	return ref, nil
}

// assistantContactsSearch searches the user's contacts (same queries as the UI, §5). Filters by
// name/email/company in Go — this way the assistant can resolve "Bob" and, when there's more than
// one match, list them instead of guessing. Scoped by tenant+owner (§6.2).
func (s *Server) assistantContactsSearch(ctx context.Context, id assistant.Identity, query string, limit int) ([]assistant.ContactHit, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	rows, err := s.q.ListContacts(ctx, db.ListContactsParams{OwnerUserID: id.UserID, TenantID: id.TenantID})
	if err != nil {
		return nil, fmt.Errorf("contact list: %w", err)
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]assistant.ContactHit, 0, limit)
	// Colleagues first: "Bob" is almost always someone in the organization, and may not be in the
	// user's personal contacts. Same pattern as InviteePicker.
	seen := map[string]bool{}
	if cols, cerr := s.q.ListColleagues(ctx, id.TenantID); cerr == nil {
		for _, c := range cols {
			if strings.EqualFold(c.Email, id.Email) {
				continue // the user themself isn't a recipient to resolve
			}
			hay := strings.ToLower(c.Name + " " + c.Email)
			if q != "" && !strings.Contains(hay, q) {
				continue
			}
			out = append(out, assistant.ContactHit{Name: c.Name, Email: c.Email, Company: "colleague"})
			seen[strings.ToLower(c.Email)] = true
			if len(out) >= limit {
				return out, nil
			}
		}
	}
	for _, c := range rows {
		if c.OwnerUserID != id.UserID {
			continue // security: only the user's own (ListContacts may include shared ones)
		}
		if seen[strings.ToLower(c.Email)] {
			continue // already came in as a colleague
		}
		hay := strings.ToLower(c.Name + " " + c.Email + " " + c.Company)
		if q != "" && !strings.Contains(hay, q) {
			continue
		}
		out = append(out, assistant.ContactHit{Name: c.Name, Email: c.Email, Company: c.Company})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// assistantContactCreate creates a contact (person) in the user's address book, with the SAME
// queries as the UI (§5). Scoped by tenant+owner (§6.2). Internal — it never leaves the
// organisation.
func (s *Server) assistantContactCreate(ctx context.Context, id assistant.Identity, c assistant.NewContact) (assistant.ContactHit, error) {
	created, err := s.q.CreateContact(ctx, db.CreateContactParams{
		TenantID: id.TenantID, OwnerUserID: id.UserID, Kind: "person",
		Name: strings.TrimSpace(c.Name), Email: strings.TrimSpace(c.Email),
		Phone: strings.TrimSpace(c.Phone), Company: strings.TrimSpace(c.Company),
		Tags: []string{}, ExtraEmails: []string{}, ExtraPhones: []string{},
		AllowPhone: true, AllowEmail: true, CustomFields: json.RawMessage("{}"), // jsonb NOT NULL
	})
	if err != nil {
		return assistant.ContactHit{}, fmt.Errorf("contact create: %w", err)
	}
	return assistant.ContactHit{Name: created.Name, Email: created.Email, Company: created.Company}, nil
}

// assistantContactShare shares a contact with a COLLEAGUE: copies the contact card (from the
// user's own address book) to the colleague's address book, in the same tenant (§6.2). The contact
// is resolved by email in the user's own address book; the destination has to be a colleague in
// the tenant — nothing leaves the organisation.
func (s *Server) assistantContactShare(ctx context.Context, id assistant.Identity, contactEmail, toEmail string) (assistant.ContactShareResult, error) {
	contactEmail = strings.ToLower(strings.TrimSpace(contactEmail))
	toEmail = strings.ToLower(strings.TrimSpace(toEmail))
	// Destination colleague: has to be in the SAME tenant (never shares outward).
	colleague, err := s.q.GetMailUserByEmailInTenant(ctx, db.GetMailUserByEmailInTenantParams{Email: toEmail, TenantID: id.TenantID})
	if err != nil {
		return assistant.ContactShareResult{}, fmt.Errorf("the recipient is not a colleague in this organisation")
	}
	// Contact to share: looked up in the user's OWN address book (ownership, §6.2).
	rows, err := s.q.ListContacts(ctx, db.ListContactsParams{OwnerUserID: id.UserID, TenantID: id.TenantID})
	if err != nil {
		return assistant.ContactShareResult{}, fmt.Errorf("contact lookup: %w", err)
	}
	var src *db.ListContactsRow
	for i := range rows {
		if rows[i].OwnerUserID == id.UserID && strings.EqualFold(rows[i].Email, contactEmail) {
			src = &rows[i]
			break
		}
	}
	if src == nil {
		return assistant.ContactShareResult{}, fmt.Errorf("no contact of yours with that e-mail")
	}
	// Copies the card into the colleague's address book (light idempotency: if that email is
	// already there, it isn't duplicated).
	dst, _ := s.q.ListContacts(ctx, db.ListContactsParams{OwnerUserID: colleague.ID, TenantID: id.TenantID})
	for _, d := range dst {
		if d.OwnerUserID == colleague.ID && strings.EqualFold(d.Email, contactEmail) {
			return assistant.ContactShareResult{Contact: src.Name, To: colleague.Name}, nil
		}
	}
	if _, err := s.q.CreateContact(ctx, db.CreateContactParams{
		TenantID: id.TenantID, OwnerUserID: colleague.ID, Kind: src.Kind,
		Name: src.Name, Email: src.Email, Phone: src.Phone, Company: src.Company,
		Title: src.Title, Mobile: src.Mobile, Notes: src.Notes,
		Tags: []string{}, ExtraEmails: []string{}, ExtraPhones: []string{},
		AllowPhone: true, AllowEmail: true, CustomFields: json.RawMessage("{}"), // jsonb NOT NULL
	}); err != nil {
		return assistant.ContactShareResult{}, fmt.Errorf("contact copy: %w", err)
	}
	who := colleague.Name
	if who == "" {
		who = colleague.Email
	}
	return assistant.ContactShareResult{Contact: src.Name, To: who}, nil
}

// assistantDocsShare shares a Drive file of the user's with COLLEAGUES in the same tenant, with
// the SAME queries as the UI's sharing feature (§5: no new SQL). Checks file ownership (§6.2) and
// ignores addresses that aren't colleagues — internal sharing grants access to the app; an
// external party would need a public link, which is a different thing.
func (s *Server) assistantDocsShare(ctx context.Context, id assistant.Identity, fileID string, emails []string, role string) (assistant.ShareResult, error) {
	fid, err := uuid.Parse(fileID)
	if err != nil {
		return assistant.ShareResult{}, fmt.Errorf("invalid file id")
	}
	mu, err := s.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: id.UserID, TenantID: id.TenantID})
	if err != nil {
		return assistant.ShareResult{}, fmt.Errorf("user lookup: %w", err)
	}
	f, err := s.q.GetStorageFile(ctx, db.GetStorageFileParams{ID: fid, OwnerUserID: ownerPg(mu)})
	if err != nil {
		return assistant.ShareResult{}, fmt.Errorf("file not found or not yours")
	}
	level := "leitor"
	if role == "editor" {
		level = "editor"
	}
	var shared []string
	for _, email := range emails {
		target, terr := s.q.GetMailUserByEmailInTenant(ctx, db.GetMailUserByEmailInTenantParams{Email: email, TenantID: id.TenantID})
		if terr != nil {
			continue // not a colleague in this tenant: don't share blindly
		}
		if _, err := s.q.CreateShare(ctx, db.CreateShareParams{
			FileID: f.ID, TenantID: f.TenantID, Principal: email, Level: level,
		}); err != nil {
			continue
		}
		s.notifyUser(ctx, f.TenantID, target.ID, "file_share",
			"New shared file", mu.Email+" shared \""+f.Name+"\" with you", fileLink(f.ID), uuid.Nil,
			map[string]string{"actor": mu.Email, "file": f.Name})
		shared = append(shared, email)
	}
	if len(shared) == 0 {
		return assistant.ShareResult{}, fmt.Errorf("none of those addresses is a colleague in this organisation")
	}
	return assistant.ShareResult{File: f.Name, SharedWith: shared}, nil
}

// assistantDocsCreate builds the Office file IN GO (decision §12.1, point 2) and saves it to the
// user's Drive, with the SAME quota and creation-logging logic as the UI (§5). Returns nil (out of
// the catalog) when storage isn't configured.
func (s *Server) assistantDocsCreate(ctx context.Context, id assistant.Identity, name string, plan office.Plan) (assistant.DriveHit, error) {
	if s.storage == nil {
		return assistant.DriveHit{}, fmt.Errorf("file storage is not configured")
	}
	data, err := office.Build(plan)
	if err != nil {
		return assistant.DriveHit{}, fmt.Errorf("build file: %w", err)
	}
	ext, _ := office.Ext(plan.Kind)
	if !strings.HasSuffix(strings.ToLower(name), "."+ext) {
		name += "." + ext
	}
	mu, err := s.q.GetMailUserInTenant(ctx, db.GetMailUserInTenantParams{ID: id.UserID, TenantID: id.TenantID})
	if err != nil {
		return assistant.DriveHit{}, fmt.Errorf("user lookup: %w", err)
	}
	rec, code := s.storeUserFile(ctx, mu, name, data, pgtype.UUID{})
	if code != "" {
		// The code is already an i18n key; the engine wraps it as a tool error and the frontend
		// translates it (law 4.6).
		return assistant.DriveHit{}, fmt.Errorf("%s", code)
	}
	return assistant.DriveHit{ID: rec.ID.String(), Name: rec.Name, Kind: "file", Size: rec.Size}, nil
}
