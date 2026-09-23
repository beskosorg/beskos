package httpapi

// Moving in: the screens for bringing a mailbox over from the provider the customer is leaving.
//
// The order of the two endpoints is the whole design. /check connects, authenticates and lists what
// it found, and copies nothing. /migrations then starts the job. A customer who mistyped a password
// or a server name learns it in three seconds, on the form where they typed it -- not in a job that
// runs for an hour and then says "failed".
//
// The copying itself is in the worker (internal/mailmigrate). Nothing here blocks on a mailbox.

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/mailmigrate"
)

type migrationSourceReq struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	StartTLS bool   `json:"starttls"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// migrationFolderView is one folder as the customer will see it before deciding.
type migrationFolderView struct {
	RemoteName string `json:"remote_name"`
	LocalName  string `json:"local_name"`
	LocalRole  string `json:"local_role"`
	SkipReason string `json:"skip_reason"`
	Messages   int64  `json:"messages"`
}

// handleCheckMigrationSource logs in to the old provider and reports what is there.
//
// WHAT IT SHOWS IS WHAT WILL HAPPEN, including the folders that will NOT be copied and why. A
// customer who is told afterwards that Trash and All Mail were skipped has already spent an
// afternoon deciding we lost their mail.
func (s *Server) handleCheckMigrationSource(w http.ResponseWriter, r *http.Request) {
	var req migrationSourceReq
	if !decode(w, r, &req) {
		return
	}
	cr, ok := s.migrationCredentials(w, req)
	if !ok {
		return
	}
	folders, err := mailmigrate.Verify(r.Context(), cr)
	if err != nil {
		code := "migration.source_unreachable"
		if errors.Is(err, mailmigrate.ErrAuth) {
			code = "migration.auth_failed"
		}
		// The server's own words go to the log and no further: they are in whatever language and
		// format a foreign IMAP server chose, and the panel shows the customer a translated key
		// (law 4.4).
		log.Printf("migration check %s@%s: %v", req.Username, req.Host, err)
		writeErr(w, http.StatusBadGateway, code)
		return
	}

	out := make([]migrationFolderView, 0, len(folders))
	var total int64
	for _, f := range folders {
		t := mailmigrate.Map(f)
		out = append(out, migrationFolderView{
			RemoteName: f.Name, LocalName: t.Name, LocalRole: t.Role,
			SkipReason: string(t.Skip), Messages: int64(f.Messages),
		})
		if t.Skip == "" {
			total += int64(f.Messages)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": out, "total_messages": total})
}

// handleCreateMigration starts the copy into the caller's own mailbox.
func (s *Server) handleCreateMigration(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	var req migrationSourceReq
	if !decode(w, r, &req) {
		return
	}
	cr, ok := s.migrationCredentials(w, req)
	if !ok {
		return
	}
	// The credential has to survive a restart for the job to resume, so it is stored -- encrypted,
	// and erased the moment the job stops needing it (see migration 00138).
	//
	// FAIL CLOSED WITH NO KEY. secretbox with no key configured returns the plaintext unchanged,
	// which is the right default for a TOTP seed in a development database and the wrong one here:
	// this is the customer's password at Google or Microsoft, and for most people that is the
	// password to several other things. Refusing to start the migration is a bad afternoon;
	// storing it in the clear is a breach we would have to report.
	if s.totpBox == nil {
		log.Printf("migration create for %s refused: no MFA_ENC_KEY, the source password would be stored in the clear", u.Email)
		writeErr(w, http.StatusServiceUnavailable, "migration.storage_unavailable")
		return
	}
	enc, err := s.totpBox.Encrypt(cr.Password)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "migration.storage_unavailable")
		return
	}

	job, err := s.q.CreateMailMigration(r.Context(), db.CreateMailMigrationParams{
		TenantID: u.TenantID, MailUserID: u.ID,
		SourceHost: cr.Host, SourcePort: int32(cr.Port), SourceStarttls: cr.StartTLS,
		SourceUsername: cr.Username, SourcePassword: enc,
	})
	if err != nil {
		// The partial unique index is what rejects a second live job for the same mailbox. Two jobs
		// writing one account would race on folder creation and double-count progress.
		if strings.Contains(err.Error(), "idx_mail_migrations_live") {
			writeErr(w, http.StatusConflict, "migration.already_running")
			return
		}
		log.Printf("migration create for %s: %v", u.Email, err)
		writeErr(w, http.StatusInternalServerError, "migration.create_failed")
		return
	}
	log.Printf("migration %s queued: %s@%s → %s", job.ID, cr.Username, cr.Host, u.Email)
	writeJSON(w, http.StatusCreated, migrationView(job, nil))
}

// handleGetMigration reports the live job, or the last one if none is live.
//
// It also answers the question the app asks on first sign-in: should this person be offered the
// import at all? That verdict is computed HERE rather than in the app because it has to survive
// changing device. Somebody who said "not now" on their laptop and is then asked again on their
// phone has been nagged, and being nagged by a mail client is how a dialog gets dismissed without
// being read.
func (s *Server) handleGetMigration(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	job, err := s.q.LiveMailMigrationForUser(r.Context(), u.ID)
	if err != nil {
		job, err = s.q.LastMailMigrationForUser(r.Context(), u.ID)
		if err != nil {
			// Nobody has ever migrated this mailbox. Not an error -- it is the normal state, and it
			// is exactly the person the offer is for.
			writeJSON(w, http.StatusOK, map[string]any{"migration": nil, "prompt": !s.migrationDismissed(r, u.ID)})
			return
		}
	}
	folders, ferr := s.q.ListMailMigrationFolders(r.Context(), job.ID)
	if ferr != nil {
		log.Printf("migration %s: listing folders failed: %v", job.ID, ferr)
	}
	// A mailbox that has already imported once is never offered it again unasked: the answer to
	// "would you like to import your mail" is plainly no for somebody who just did.
	writeJSON(w, http.StatusOK, map[string]any{"migration": migrationView(job, folders), "prompt": false})
}

// migrationSettings is the slice of the user's settings this feature owns.
type migrationSettings struct {
	Migration struct {
		Prompted bool `json:"prompted"`
	} `json:"migration"`
}

// migrationDismissed reports whether this person has already been offered the import and answered.
// A failure to read the setting is treated as "already asked": the cost of being wrong that way is
// an offer nobody sees, and the other way is a dialog on every sign-in.
func (s *Server) migrationDismissed(r *http.Request, userID uuid.UUID) bool {
	raw, err := s.q.GetUserSettings(r.Context(), userID)
	if err != nil {
		return true
	}
	var st migrationSettings
	if len(raw) == 0 || json.Unmarshal(raw, &st) != nil {
		return false
	}
	return st.Migration.Prompted
}

// handleDismissMigrationPrompt records that the offer has been made, whatever the answer was.
//
// It is stored against the account and not in the browser: the same person on a laptop, a phone and
// the desktop application is one person who was asked once.
func (s *Server) handleDismissMigrationPrompt(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	// Top-level merge in the query (settings || $2), so writing this section leaves Chat, Calendar
	// and the rest untouched.
	patch := []byte(`{"migration":{"prompted":true}}`)
	if err := s.q.UpdateUserSettings(r.Context(), db.UpdateUserSettingsParams{ID: u.ID, Settings: patch}); err != nil {
		writeErr(w, http.StatusInternalServerError, "settings.failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleCancelMigration stops a job. What already landed stays: deleting mail we just copied, on the
// way out, is not a cancellation.
func (s *Server) handleCancelMigration(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "migration.not_found")
		return
	}
	// Tenant-scoped, and the job's own mailbox must be the caller's: an administrator has no
	// business cancelling somebody else's move from this route.
	job, err := s.q.GetMailMigration(r.Context(), db.GetMailMigrationParams{ID: id, TenantID: u.TenantID})
	if err != nil || job.MailUserID != u.ID {
		writeErr(w, http.StatusNotFound, "migration.not_found")
		return
	}
	if err := s.q.CancelMailMigration(r.Context(), db.CancelMailMigrationParams{ID: id, TenantID: u.TenantID}); err != nil {
		writeErr(w, http.StatusInternalServerError, "migration.cancel_failed")
		return
	}
	log.Printf("migration %s cancelled by %s", id, u.Email)
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// handleListTenantMigrations shows every migration in the organisation, for an administrator
// watching a company-wide move.
func (s *Server) handleListTenantMigrations(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	rows, err := s.q.ListTenantMailMigrations(r.Context(), u.TenantID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "migration.list_failed")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		v := migrationView(db.MailMigration{
			ID: m.ID, Status: m.Status, ErrorCode: m.ErrorCode, SourceHost: m.SourceHost,
			SourceUsername: m.SourceUsername, TotalMessages: m.TotalMessages,
			CopiedMessages: m.CopiedMessages, SkippedMessages: m.SkippedMessages,
			FailedMessages: m.FailedMessages, CreatedAt: m.CreatedAt,
			StartedAt: m.StartedAt, FinishedAt: m.FinishedAt,
		}, nil)
		v["mailbox_email"] = m.MailboxEmail
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"migrations": out})
}

// migrationCredentials validates the form and returns the credentials to use.
func (s *Server) migrationCredentials(w http.ResponseWriter, req migrationSourceReq) (mailmigrate.Credentials, bool) {
	host := strings.TrimSpace(req.Host)
	user := strings.TrimSpace(req.Username)
	if host == "" || user == "" || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "migration.missing_fields")
		return mailmigrate.Credentials{}, false
	}
	// A host with a scheme or a path is a customer pasting a webmail URL, which would otherwise fail
	// with a DNS error that explains nothing.
	if strings.ContainsAny(host, "/:@ ") {
		writeErr(w, http.StatusBadRequest, "migration.invalid_host")
		return mailmigrate.Credentials{}, false
	}
	port := req.Port
	if port == 0 {
		port = 993
		if req.StartTLS {
			port = 143
		}
	}
	if port < 1 || port > 65535 {
		writeErr(w, http.StatusBadRequest, "migration.invalid_host")
		return mailmigrate.Credentials{}, false
	}
	return mailmigrate.Credentials{
		Host: host, Port: port, StartTLS: req.StartTLS, Username: user, Password: req.Password,
	}, true
}

// migrationView is what leaves the API. The source password is not in it and never will be: it is
// write-only from the moment it arrives.
func migrationView(m db.MailMigration, folders []db.MailMigrationFolder) map[string]any {
	out := map[string]any{
		"id":               m.ID,
		"kind":             m.Kind,
		"source_file":      m.SourceFile,
		"status":           m.Status,
		"error_code":       m.ErrorCode,
		"source_host":      m.SourceHost,
		"source_username":  m.SourceUsername,
		"total_messages":   m.TotalMessages,
		"copied_messages":  m.CopiedMessages,
		"skipped_messages": m.SkippedMessages,
		"failed_messages":  m.FailedMessages,
		"created_at":       m.CreatedAt,
		"started_at":       m.StartedAt,
		"finished_at":      m.FinishedAt,
	}
	if folders == nil {
		return out
	}
	list := make([]map[string]any, 0, len(folders))
	for _, f := range folders {
		list = append(list, map[string]any{
			"remote_name":      f.RemoteName,
			"local_name":       f.LocalName,
			"local_role":       f.LocalRole,
			"skip_reason":      f.SkipReason,
			"status":           f.Status,
			"total_messages":   f.TotalMessages,
			"copied_messages":  f.CopiedMessages,
			"skipped_messages": f.SkippedMessages,
			"failed_messages":  f.FailedMessages,
		})
	}
	out["folders"] = list
	return out
}
