package mailmigrate

// The copy itself: one job, one folder at a time, resumable at any point.
//
// SHAPE OF THE LOOP. Claim a job → list the source's folders (idempotent, so a resume finds its own
// progress) → take the next unfinished folder → walk it in pages of ascending UID, importing each
// message and recording the highest UID reached → next folder → done. Nothing is held in memory
// between passes except the connection, so a restart costs at most one page.
//
// WHAT IT DELIBERATELY DOES NOT DO. It does not delete anything at the source. The customer's old
// account is left exactly as it was found, because a migration that empties the place you are
// leaving removes the only fallback you had if it went wrong.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/mailbox"
	"github.com/beskosorg/beskos/backend/internal/secretbox"
	"github.com/beskosorg/beskos/backend/internal/storage"
)

// pageSize is how many messages one pass copies from one folder before writing progress and looking
// at the job again. Small enough that a crash loses seconds of work and a cancellation is noticed
// quickly; large enough that a 40,000-message folder is not 40,000 database writes.
const pageSize = 50

// maxMessageBytes caps one message. Above this, the source is either broken or holding something no
// mail system should have accepted; either way, one message must not exhaust the worker's memory.
const maxMessageBytes = 200 << 20

// Runner copies mailboxes. One per worker process.
type Runner struct {
	q           *db.Queries
	box         *secretbox.Box
	stalwartURL string
	// store holds uploads while a file-sourced job runs. nil = only server-sourced jobs can run,
	// which is what a deployment without object storage honestly supports.
	store *storage.Service
}

func NewRunner(q *db.Queries, box *secretbox.Box, stalwartURL string) *Runner {
	return &Runner{q: q, box: box, stalwartURL: stalwartURL}
}

// WithStorage enables importing from an uploaded file.
func (r *Runner) WithStorage(st *storage.Service) *Runner { r.store = st; return r }

// Tick takes at most one job and works on it for one pass. Returns true if it did anything, so the
// caller can poll fast while there is work and slowly when there is none.
func (r *Runner) Tick(ctx context.Context) bool {
	job, err := r.q.ClaimMailMigration(ctx)
	if err != nil {
		return false // no rows = nothing to do; anything else is reported by the caller's next pass
	}
	if err := r.run(ctx, job); err != nil {
		r.fail(ctx, job, err)
		return true
	}
	return true
}

// fail decides whether a job is over or merely interrupted. This is the whole of law 2.9 in one
// function: a rejected password will never succeed however many times it is retried, and a refused
// connection very well might.
func (r *Runner) fail(ctx context.Context, job db.MailMigration, err error) {
	code, permanent := classify(err)
	if permanent {
		log.Printf("mail-migration %s: giving up (%s): %v", job.ID, code, err)
		// A job that will never run again must not leave a copy of the mailbox in the bucket.
		r.eraseUpload(ctx, job)
		_ = r.q.FinishMailMigration(ctx, db.FinishMailMigrationParams{
			ID: job.ID, Status: "failed", ErrorCode: code, LastError: truncate(err.Error(), 500),
		})
		return
	}
	_ = r.q.ReleaseMailMigration(ctx, db.ReleaseMailMigrationParams{
		ID: job.ID, ErrorCode: code, LastError: truncate(err.Error(), 500),
	})
	// A pass that fails once is weather. A count that keeps climbing is a migration that is not
	// going to finish, and it must be loud rather than retrying politely forever (law 2.8).
	n := job.ConsecutiveFailures + 1
	switch {
	case n >= 10:
		log.Printf("mail-migration %s: ALERT: %d consecutive failed passes against %s — "+
			"this migration is not progressing and nobody has been told: %v",
			job.ID, n, job.SourceHost, err)
		// Ten failed passes is not a bad minute, it is a broken job. Stopping it puts the reason on
		// the customer's screen, which is the only thing that gets it fixed.
		r.eraseUpload(ctx, job)
		_ = r.q.FinishMailMigration(ctx, db.FinishMailMigrationParams{
			ID: job.ID, Status: "failed", ErrorCode: code, LastError: truncate(err.Error(), 500),
		})
	case n >= 3:
		log.Printf("mail-migration %s: %d consecutive failed passes against %s: %v",
			job.ID, n, job.SourceHost, err)
	default:
		log.Printf("mail-migration %s: pass failed, will retry: %v", job.ID, err)
	}
}

// run does one pass over one job, from whichever source it has.
func (r *Runner) run(ctx context.Context, job db.MailMigration) error {
	if job.Kind == "file" {
		return r.runFile(ctx, job)
	}
	pw, err := r.box.Decrypt(job.SourcePassword)
	if err != nil || pw == "" {
		// The password is erased when a job ends, so an empty one here means the job was finished or
		// cancelled between the claim and now. Not an error, and not something to retry.
		return errPermanent{code: "migration.credentials_gone", err: errors.New("source password is no longer stored")}
	}
	dest, acc, err := r.destination(ctx, job)
	if err != nil {
		return err
	}

	src, err := Dial(ctx, Credentials{
		Host: job.SourceHost, Port: int(job.SourcePort), StartTLS: job.SourceStarttls,
		Username: job.SourceUsername, Password: pw,
	})
	if err != nil {
		return err
	}
	defer src.Close()

	if err := r.syncFolders(ctx, job, src); err != nil {
		return err
	}
	if job.ConsecutiveFailures > 0 {
		// Say so when it recovers, not only when it breaks.
		log.Printf("mail-migration %s: recovered after %d failed passes", job.ID, job.ConsecutiveFailures)
		_ = r.q.ResetMailMigrationFailures(ctx, job.ID)
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Re-read the job each folder: this is where a cancellation from the panel is noticed.
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
			break // no rows: every folder is finished
		}
		if err := r.copyFolder(ctx, job, folder, src, dest, acc); err != nil {
			return err
		}
	}

	left, err := r.q.CountUnfinishedMailMigrationFolders(ctx, job.ID)
	if err != nil {
		return err
	}
	if left > 0 {
		return nil // more passes to come
	}
	log.Printf("mail-migration %s: finished (%s → %s)", job.ID, job.SourceHost, job.SourceUsername)
	return r.q.FinishMailMigration(ctx, db.FinishMailMigrationParams{ID: job.ID, Status: "done"})
}

// destination opens the customer's own mailbox with their stored JMAP credential.
func (r *Runner) destination(ctx context.Context, job db.MailMigration) (*mailbox.UserClient, string, error) {
	u, err := r.q.GetUserByID(ctx, job.MailUserID)
	if err != nil {
		return nil, "", err
	}
	basic, err := r.box.Decrypt(u.JmapCred)
	if err != nil || basic == "" {
		// The credential is written when the person logs in. Without it we cannot write to their own
		// mailbox, and no amount of retrying creates one -- they have to sign in once.
		return nil, "", errPermanent{
			code: "migration.destination_locked",
			err:  fmt.Errorf("no usable JMAP credential for %s; the owner has to sign in once", u.Email),
		}
	}
	dest := mailbox.NewUserClient(r.stalwartURL, basic)
	acc, err := dest.AccountID(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("open destination mailbox: %w", err)
	}
	return dest, acc, nil
}

// syncFolders records the source's folder list. Idempotent: a resumed job re-lists and finds its own
// rows, counters intact.
func (r *Runner) syncFolders(ctx context.Context, job db.MailMigration, src *Source) error {
	folders, err := src.Folders(ctx)
	if err != nil {
		return err
	}
	var total int64
	for _, f := range folders {
		t := Map(f)
		row, err := r.q.UpsertMailMigrationFolder(ctx, db.UpsertMailMigrationFolderParams{
			MigrationID: job.ID, RemoteName: f.Name, LocalName: t.Name, LocalRole: t.Role,
			SkipReason: string(t.Skip), UidValidity: int64(f.UIDValidity), TotalMessages: int64(f.Messages),
		})
		if err != nil {
			return fmt.Errorf("record folder %q: %w", f.Name, err)
		}
		if t.Skip != "" {
			continue
		}
		total += int64(f.Messages)
		// UIDVALIDITY changed under us: every recorded UID now names a different message, so the
		// only honest reading of our progress is that we have none.
		if row.UidValidity != int64(f.UIDValidity) && f.UIDValidity != 0 {
			log.Printf("mail-migration %s: folder %q changed UIDVALIDITY (%d → %d), walking it again",
				job.ID, f.Name, row.UidValidity, f.UIDValidity)
			if err := r.q.ResetMailMigrationFolder(ctx, db.ResetMailMigrationFolderParams{
				ID: row.ID, UidValidity: int64(f.UIDValidity),
			}); err != nil {
				return err
			}
		}
	}
	return r.q.SetMailMigrationTotal(ctx, db.SetMailMigrationTotalParams{ID: job.ID, TotalMessages: total})
}

// copyFolder walks one folder from its watermark to the end, one page at a time.
func (r *Runner) copyFolder(ctx context.Context, job db.MailMigration, f db.MailMigrationFolder,
	src *Source, dest *mailbox.UserClient, acc string) error {

	if err := r.q.StartMailMigrationFolder(ctx, f.ID); err != nil {
		return err
	}
	st, err := src.Select(ctx, f.RemoteName)
	if err != nil {
		// One unreadable folder must not sink the other forty. Record it and move on -- and because
		// it is recorded, the customer sees which folder and why, instead of a total that never adds
		// up.
		log.Printf("mail-migration %s: folder %q cannot be opened, skipping it: %v", job.ID, f.RemoteName, err)
		return r.q.FinishMailMigrationFolder(ctx, db.FinishMailMigrationFolderParams{
			ID: f.ID, Status: "failed", LastError: truncate(err.Error(), 300),
		})
	}
	if int64(st.UIDValidity) != f.UidValidity && st.UIDValidity != 0 {
		if err := r.q.ResetMailMigrationFolder(ctx, db.ResetMailMigrationFolderParams{
			ID: f.ID, UidValidity: int64(st.UIDValidity),
		}); err != nil {
			return err
		}
		f.LastUid = 0
	}

	target, err := r.targetMailbox(ctx, dest, acc, f)
	if err != nil {
		return err
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		msgs, err := src.FetchAfter(ctx, uint32(f.LastUid), pageSize)
		if err != nil {
			return fmt.Errorf("fetch from %q: %w", f.RemoteName, err)
		}
		if len(msgs) == 0 {
			break
		}
		copied, skipped, failed, highest := r.importPage(ctx, job, f, msgs, dest, acc, target)
		if err := r.q.AdvanceMailMigrationFolder(ctx, db.AdvanceMailMigrationFolderParams{
			ID: f.ID, LastUid: int64(highest), Copied: copied, Skipped: skipped, Failed: failed,
		}); err != nil {
			return err
		}
		if err := r.q.AddMailMigrationProgress(ctx, db.AddMailMigrationProgressParams{
			ID: job.ID, Copied: copied, Skipped: skipped, Failed: failed,
		}); err != nil {
			return err
		}
		f.LastUid = int64(highest)

		// Between pages is where a cancellation is honoured, and the only place the loop can be left
		// without losing a page of work.
		cur, err := r.q.GetMailMigration(ctx, db.GetMailMigrationParams{ID: job.ID, TenantID: job.TenantID})
		if err != nil || cur.Status != "running" {
			return nil
		}
	}
	return r.q.FinishMailMigrationFolder(ctx, db.FinishMailMigrationFolderParams{ID: f.ID, Status: "done"})
}

// importPage imports one page and returns what happened to it.
//
// A MESSAGE THAT WILL NOT IMPORT IS COUNTED, NOT FATAL. One malformed message out of forty thousand
// must not cost the customer the other 39,999 -- but it must not disappear quietly either, which is
// what failed_messages on the screen is for.
func (r *Runner) importPage(ctx context.Context, job db.MailMigration, f db.MailMigrationFolder,
	msgs []Message, dest *mailbox.UserClient, acc, target string) (copied, skipped, failed int64, highest uint32) {

	highest = uint32(f.LastUid)
	for _, m := range msgs {
		if m.UID > highest {
			highest = m.UID
		}
		if len(m.Raw) == 0 || len(m.Raw) > maxMessageBytes {
			log.Printf("mail-migration %s: %q uid %d is %d bytes, not importing it",
				job.ID, f.RemoteName, m.UID, len(m.Raw))
			failed++
			continue
		}
		err := dest.Import(ctx, acc, target, mailbox.ImportedMessage{
			Raw: m.Raw, ReceivedAt: m.InternalDate, Keywords: Keywords(m.Flags),
		})
		switch {
		case err == nil:
			copied++
		case errors.Is(err, mailbox.ErrAlreadyImported):
			skipped++
		default:
			log.Printf("mail-migration %s: %q uid %d failed to import: %v", job.ID, f.RemoteName, m.UID, err)
			failed++
		}
	}
	return copied, skipped, failed, highest
}

// targetMailbox resolves, and creates if needed, the folder the messages land in.
func (r *Runner) targetMailbox(ctx context.Context, dest *mailbox.UserClient, acc string, f db.MailMigrationFolder) (string, error) {
	boxes, err := dest.Mailboxes(ctx, acc)
	if err != nil {
		return "", err
	}
	if f.LocalRole != "" {
		for _, b := range boxes {
			if b.Role == f.LocalRole {
				return b.ID, nil
			}
		}
		// The account has no folder with that role (some do not have an Archive). Fall through and
		// create it by name rather than dropping the mail somewhere it does not belong.
	}
	name := f.LocalName
	if name == "" {
		name = f.LocalRole
	}
	if name == "" {
		return "", fmt.Errorf("folder %q maps to nothing", f.RemoteName)
	}
	return ensurePath(ctx, dest, acc, boxes, strings.Split(name, "/"))
}

// ensurePath walks a nested folder name, creating what is missing. "Clients/2024/Invoices" is three
// folders here, as it was three folders there -- flattening it into one named "Clients/2024/Invoices"
// would give the customer a mailbox they cannot navigate.
func ensurePath(ctx context.Context, dest *mailbox.UserClient, acc string, boxes []mailbox.MailboxRef, parts []string) (string, error) {
	parent := ""
	for _, part := range parts {
		found := ""
		for _, b := range boxes {
			if b.ParentID == parent && strings.EqualFold(b.Name, part) {
				found = b.ID
				break
			}
		}
		if found == "" {
			id, err := dest.CreateMailbox(ctx, acc, part, parent)
			if err != nil {
				return "", fmt.Errorf("create folder %q: %w", part, err)
			}
			boxes = append(boxes, mailbox.MailboxRef{ID: id, Name: part, ParentID: parent})
			found = id
		}
		parent = found
	}
	if parent == "" {
		return "", fmt.Errorf("empty folder path")
	}
	return parent, nil
}

// errPermanent marks a failure that will still be a failure next time, and carries the key the panel
// translates for the customer.
type errPermanent struct {
	code string
	err  error
}

func (e errPermanent) Error() string { return e.err.Error() }
func (e errPermanent) Unwrap() error { return e.err }

// classify turns an error into a translatable key and a verdict: is another attempt worth anything?
func classify(err error) (code string, permanent bool) {
	var p errPermanent
	if errors.As(err, &p) {
		return p.code, true
	}
	if errors.Is(err, ErrAuth) {
		// Retrying a rejected password does not fix it and does lock the account out.
		return "migration.auth_failed", true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "migration.interrupted", false
	}
	return "migration.source_unreachable", false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Verify checks credentials without copying anything, so the customer finds out the password is
// wrong on the screen where they typed it rather than in a job that fails an hour later.
//
// It returns the folder list, which doubles as the answer to "what is going to happen": the panel
// shows what will be copied and what will not, BEFORE anything is.
func Verify(ctx context.Context, cr Credentials) ([]Folder, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	src, err := Dial(ctx, cr)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	return src.Folders(ctx)
}
