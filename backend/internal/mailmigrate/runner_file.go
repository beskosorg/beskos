package mailmigrate

// Running a migration whose source is an uploaded file rather than a server.
//
// It shares everything downstream with the IMAP kind: the same job row, the same per-folder
// progress, the same watermark, the same JMAP import, the same cancellation. What differs is only
// where the messages come from, which is the whole reason this is a second source and not a second
// feature.
//
// THE UPLOAD IS DELETED WHEN THE JOB ENDS. While it runs we are holding a second copy of somebody's
// entire mailbox; after it ends we are holding it for no reason. That is storage we are not
// entitled to and a breach we would have to report.

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/mailbox"
)

// filePageSize is how many messages are imported before progress is written. Smaller than the IMAP
// page because there is no round trip to a remote server between messages -- the cost here is one
// database write, and losing fewer messages to a restart is worth more.
const filePageSize = 25

// runFile does one pass over a file-sourced job.
func (r *Runner) runFile(ctx context.Context, job db.MailMigration) error {
	if r.store == nil {
		return errPermanent{code: "migration.storage_unavailable", err: errors.New("no object storage configured")}
	}
	if job.SourceKey == "" {
		// The upload was already erased, which happens when a job is finished or cancelled between
		// the claim and now. Nothing to do and nothing to retry.
		return errPermanent{code: "migration.file_gone", err: errors.New("the uploaded file is no longer stored")}
	}
	dest, acc, err := r.destination(ctx, job)
	if err != nil {
		return err
	}
	if err := r.syncFileEntries(ctx, job); err != nil {
		return err
	}
	if job.ConsecutiveFailures > 0 {
		log.Printf("mail-migration %s: recovered after %d failed passes", job.ID, job.ConsecutiveFailures)
		_ = r.q.ResetMailMigrationFailures(ctx, job.ID)
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		cur, err := r.q.GetMailMigration(ctx, db.GetMailMigrationParams{ID: job.ID, TenantID: job.TenantID})
		if err != nil {
			return err
		}
		if cur.Status != "running" {
			log.Printf("mail-migration %s: stopping, job is now %s", job.ID, cur.Status)
			return nil
		}
		folder, err := r.q.NextMailMigrationFolder(ctx, job.ID)
		if err != nil {
			break // every entry finished
		}
		if err := r.copyFileFolder(ctx, job, folder, dest, acc); err != nil {
			return err
		}
	}

	left, err := r.q.CountUnfinishedMailMigrationFolders(ctx, job.ID)
	if err != nil {
		return err
	}
	if left > 0 {
		return nil
	}
	log.Printf("mail-migration %s: finished (file %q)", job.ID, job.SourceFile)
	r.eraseUpload(ctx, job)
	return r.q.FinishMailMigration(ctx, db.FinishMailMigrationParams{ID: job.ID, Status: "done"})
}

// eraseUpload removes the parked file and forgets its key. Best effort and loud on failure: a
// mailbox left lying in the bucket is the kind of thing nobody notices until it is a finding.
func (r *Runner) eraseUpload(ctx context.Context, job db.MailMigration) {
	if r.store == nil || job.SourceKey == "" {
		return
	}
	if err := r.store.Remove(ctx, job.SourceKey); err != nil {
		log.Printf("mail-migration %s: FAILED TO DELETE the uploaded mailbox at %s: %v — "+
			"a full copy of a customer's mail is still in the bucket", job.ID, job.SourceKey, err)
		return
	}
	_ = r.q.ClearMailMigrationFile(ctx, job.ID)
}

// syncFileEntries records what the upload contains. Idempotent, so a resumed job finds its own rows
// with their progress intact.
//
// The message COUNT is deliberately not established here. Knowing it would mean reading the whole
// file before importing anything -- doubling the work on every pass for a number that is only a
// progress bar. The screen shows a sweeping bar and a rising count instead, which is what we
// honestly know.
func (r *Runner) syncFileEntries(ctx context.Context, job db.MailMigration) error {
	kind, err := DetectFile(job.SourceFile)
	if err != nil {
		var ue ErrUnsupportedFile
		if errors.As(err, &ue) {
			return errPermanent{code: ue.Code, err: err}
		}
		return err
	}

	if kind != KindZip {
		_, err := r.q.UpsertMailMigrationFolder(ctx, db.UpsertMailMigrationFolderParams{
			MigrationID: job.ID,
			// One file is one mailbox, and its name is what the customer called it.
			RemoteName: job.SourceFile, LocalName: importFolderName(job.SourceFile),
		})
		return err
	}

	zr, closer, err := r.openZip(ctx, job)
	if err != nil {
		return err
	}
	defer closer.Close()

	entries := ZipEntries(zr)
	if len(entries) == 0 {
		return errPermanent{code: "migration.file_empty", err: errors.New("no readable mailbox inside the archive")}
	}
	for _, e := range entries {
		if _, err := r.q.UpsertMailMigrationFolder(ctx, db.UpsertMailMigrationFolderParams{
			MigrationID: job.ID, RemoteName: e.Name, LocalName: importFolderName(e.Name),
		}); err != nil {
			return fmt.Errorf("record entry %q: %w", e.Name, err)
		}
	}
	return nil
}

// copyFileFolder imports one entry, resuming from its watermark.
func (r *Runner) copyFileFolder(ctx context.Context, job db.MailMigration, f db.MailMigrationFolder,
	dest *mailbox.UserClient, acc string) error {

	if err := r.q.StartMailMigrationFolder(ctx, f.ID); err != nil {
		return err
	}
	target, err := r.targetMailbox(ctx, dest, acc, f)
	if err != nil {
		return err
	}

	stream, closer, kind, err := r.openEntry(ctx, job, f.RemoteName)
	if err != nil {
		log.Printf("mail-migration %s: entry %q cannot be read, skipping it: %v", job.ID, f.RemoteName, err)
		return r.q.FinishMailMigrationFolder(ctx, db.FinishMailMigrationFolderParams{
			ID: f.ID, Status: "failed", LastError: truncate(err.Error(), 300),
		})
	}
	defer closer.Close()

	// A page held in memory, flushed to the database every filePageSize messages. Same shape as the
	// IMAP side so a restart costs one page there and one page here.
	var copied, skipped, failed int64
	var ordinal int64
	cancelled := false

	flush := func() error {
		if copied+skipped+failed == 0 {
			return nil
		}
		if err := r.q.AdvanceMailMigrationFolder(ctx, db.AdvanceMailMigrationFolderParams{
			ID: f.ID, LastUid: ordinal, Copied: copied, Skipped: skipped, Failed: failed,
		}); err != nil {
			return err
		}
		if err := r.q.AddMailMigrationProgress(ctx, db.AddMailMigrationProgressParams{
			ID: job.ID, Copied: copied, Skipped: skipped, Failed: failed,
		}); err != nil {
			return err
		}
		copied, skipped, failed = 0, 0, 0
		return nil
	}

	_, readErr := ReadMessages(stream, kind, f.LastUid, func(m Message) error {
		ordinal = int64(m.UID)
		if len(m.Raw) == 0 {
			failed++
		} else {
			switch err := dest.Import(ctx, acc, target, mailbox.ImportedMessage{
				Raw: m.Raw, ReceivedAt: m.InternalDate, Keywords: Keywords(m.Flags),
			}); {
			case err == nil:
				copied++
			case errors.Is(err, mailbox.ErrAlreadyImported):
				skipped++
			default:
				log.Printf("mail-migration %s: %q message %d failed to import: %v",
					job.ID, f.RemoteName, m.UID, err)
				failed++
			}
		}
		if copied+skipped+failed < filePageSize {
			return nil
		}
		if err := flush(); err != nil {
			return err
		}
		// Between pages is where a cancellation is honoured, exactly as on the IMAP side.
		cur, err := r.q.GetMailMigration(ctx, db.GetMailMigrationParams{ID: job.ID, TenantID: job.TenantID})
		if err != nil {
			return err
		}
		if cur.Status != "running" {
			cancelled = true
			return errStopWalk
		}
		return nil
	})
	if ferr := flush(); ferr != nil {
		return ferr
	}
	if cancelled {
		return nil
	}
	if readErr != nil && !errors.Is(readErr, errStopWalk) {
		// The file is malformed from here on. What was already imported stays imported, and the
		// entry is marked so the customer can see which one and how far it got.
		log.Printf("mail-migration %s: entry %q stopped at message %d: %v", job.ID, f.RemoteName, ordinal, readErr)
		return r.q.FinishMailMigrationFolder(ctx, db.FinishMailMigrationFolderParams{
			ID: f.ID, Status: "failed", LastError: truncate(readErr.Error(), 300),
		})
	}
	return r.q.FinishMailMigrationFolder(ctx, db.FinishMailMigrationFolderParams{ID: f.ID, Status: "done"})
}

// errStopWalk ends a walk early without it counting as a failure.
var errStopWalk = errors.New("mailmigrate: stop")

// openEntry returns a reader positioned at the start of one mailbox in the upload.
func (r *Runner) openEntry(ctx context.Context, job db.MailMigration, entry string) (io.Reader, io.Closer, FileKind, error) {
	kind, err := DetectFile(job.SourceFile)
	if err != nil {
		return nil, nil, "", err
	}
	if kind != KindZip {
		obj, oerr := r.store.Get(ctx, job.SourceKey)
		if oerr != nil {
			return nil, nil, "", oerr
		}
		return obj, obj, kind, nil
	}

	zr, closer, zerr := r.openZip(ctx, job)
	if zerr != nil {
		return nil, nil, "", zerr
	}
	for _, f := range zr.File {
		if cleanEntryName(f.Name) != entry {
			continue
		}
		rc, oerr := f.Open()
		if oerr != nil {
			closer.Close()
			return nil, nil, "", oerr
		}
		ek, _ := DetectFile(f.Name)
		if ek == "" {
			ek = KindMbox
		}
		return rc, multiCloser{rc, closer}, ek, nil
	}
	closer.Close()
	return nil, nil, "", fmt.Errorf("entry %q is no longer in the archive", entry)
}

// openZip reads the archive directory. A zip is read back-to-front, so it needs random access --
// which is why the upload is fetched seekably rather than streamed.
func (r *Runner) openZip(ctx context.Context, job db.MailMigration) (*zip.Reader, io.Closer, error) {
	obj, err := r.store.GetSeek(ctx, job.SourceKey)
	if err != nil {
		return nil, nil, err
	}
	size, err := obj.Seek(0, io.SeekEnd)
	if err != nil {
		obj.Close()
		return nil, nil, err
	}
	zr, err := zip.NewReader(&seekReaderAt{obj}, size)
	if err != nil {
		obj.Close()
		return nil, nil, errPermanent{code: "migration.file_unreadable", err: err}
	}
	return zr, obj, nil
}

// seekReaderAt adapts a seeking reader to the ReaderAt that archive/zip wants. Single-threaded by
// construction: one job, one pass, one reader.
type seekReaderAt struct{ rs io.ReadSeeker }

func (s *seekReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if _, err := s.rs.Seek(off, io.SeekStart); err != nil {
		return 0, err
	}
	return io.ReadFull(s.rs, p)
}

type multiCloser []io.Closer

func (m multiCloser) Close() error {
	var first error
	for _, c := range m {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
