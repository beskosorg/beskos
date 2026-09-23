package httpapi

// Taking in a mailbox the customer has on their own disk.
//
// This is the half that IMAP cannot reach. Years of POP3 in Thunderbird's Local Folders, an archive
// exported before an account was closed, a mailbox handed over on a memory stick when somebody
// left: none of it is on any server, so no login finds it.
//
// The body is STREAMED to object storage and never held in memory. A mail archive is measured in
// gigabytes, and reading one into RAM to inspect it would take the API down on the first customer
// with real volume.

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/mailmigrate"
)

// maxImportBytes caps one upload.
//
// THIS NUMBER IS THE PROXY'S, NOT OURS. CapRover's nginx refuses a body over 500 MB before the
// request ever reaches this process, and it answers with its own HTML error page -- so a larger
// limit here would not be generosity, it would be a promise the infrastructure breaks after the
// customer has spent twenty minutes uploading. Measured against production, not assumed: 400 MB
// passes through, 600 MB comes back 413 from nginx.
//
// Raising it means giving the api app a custom nginx block with `client_max_body_size` AND
// `proxy_request_buffering off` -- without the second, the proxy spools the whole upload to the
// disk of the machine that also runs the mail server. Until that is done, the screen tells people
// to upload folders separately, which is honest and which mbox makes easy.
const maxImportBytes = 500 << 20

// handleUploadMigrationFile parks an uploaded mailbox and queues the job that reads it.
func (s *Server) handleUploadMigrationFile(w http.ResponseWriter, r *http.Request) {
	u := mailUserFrom(r.Context())
	if s.storage == nil {
		writeErr(w, http.StatusServiceUnavailable, "migration.storage_unavailable")
		return
	}

	// The name arrives in the query rather than the body: the body is the mailbox, and parsing a
	// multipart envelope around several gigabytes to read one string is work for nothing.
	name := filepath.Base(strings.TrimSpace(r.URL.Query().Get("name")))
	if name == "" || name == "." || name == "/" {
		writeErr(w, http.StatusBadRequest, "migration.missing_fields")
		return
	}
	// Refuse a format we cannot read BEFORE the upload, not after. Discovering that a .pst is
	// unreadable at the end of a two-gigabyte upload is the worst possible moment to say so.
	if _, err := mailmigrate.DetectFile(name); err != nil {
		var ue mailmigrate.ErrUnsupportedFile
		if errors.As(err, &ue) {
			writeErr(w, http.StatusUnsupportedMediaType, ue.Code)
			return
		}
		writeErr(w, http.StatusUnsupportedMediaType, "migration.file_unreadable")
		return
	}

	size := r.ContentLength
	if size <= 0 {
		// Object storage needs the length up front to stream without buffering the whole body.
		writeErr(w, http.StatusLengthRequired, "migration.file_size_unknown")
		return
	}
	if size > maxImportBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "migration.file_too_large")
		return
	}

	// Its own prefix, never under "mail/" -- that belongs to Stalwart and sweeping the bucket needs
	// an allow-list. Tenant-scoped like every other object we write.
	key := fmt.Sprintf("migrations/%s/%s", u.TenantID, uuid.New())
	body := http.MaxBytesReader(w, r.Body, maxImportBytes)
	if err := s.storage.Put(r.Context(), key, body, size, "application/octet-stream"); err != nil {
		log.Printf("migration upload for %s: %v", u.Email, err)
		writeErr(w, http.StatusBadGateway, "storage.upload_failed")
		return
	}

	job, err := s.q.CreateMailFileMigration(r.Context(), db.CreateMailFileMigrationParams{
		TenantID: u.TenantID, MailUserID: u.ID, SourceFile: name, SourceKey: key,
	})
	if err != nil {
		// The upload has nowhere to belong now, so it goes: an orphaned copy of somebody's mailbox
		// is exactly the thing that must not survive a failed request.
		if rerr := s.storage.Remove(r.Context(), key); rerr != nil {
			log.Printf("migration upload for %s: the job failed AND the file was left at %s: %v", u.Email, key, rerr)
		}
		if strings.Contains(err.Error(), "idx_mail_migrations_live") {
			writeErr(w, http.StatusConflict, "migration.already_running")
			return
		}
		log.Printf("migration file job for %s: %v", u.Email, err)
		writeErr(w, http.StatusInternalServerError, "migration.create_failed")
		return
	}
	log.Printf("migration %s queued from file %q (%d bytes) → %s", job.ID, name, size, u.Email)
	writeJSON(w, http.StatusCreated, migrationView(job, nil))
}
