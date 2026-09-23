// cmd/worker: runs the provisioning state machine (§7), always resuming from the
// last saved state, idempotently. See internal/provisioning.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/beskosorg/beskos/backend/internal/ai"
	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/chat"
	"github.com/beskosorg/beskos/backend/internal/config"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/edition"
	"github.com/beskosorg/beskos/backend/internal/httpapi"
	"github.com/beskosorg/beskos/backend/internal/mailbox"
	"github.com/beskosorg/beskos/backend/internal/provisioning"
	"github.com/beskosorg/beskos/backend/internal/secretbox"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
	"github.com/beskosorg/beskos/backend/internal/storage"
	"github.com/beskosorg/beskos/backend/internal/wiki"
)

type vapidKeys struct{ public, private, subject string }

// scanReminders fires the event reminders whose moment has arrived: bell notification + Web Push
// for the owner, and marks reminded so it does not repeat. The text is in pt-BR like the other
// notifications.
func scanReminders(ctx context.Context, q *db.Queries, v vapidKeys) {
	rows, err := q.EventsToRemind(ctx)
	if err != nil {
		log.Printf("reminder-scan: query failed: %v", err)
		return
	}
	for _, e := range rows {
		owner := uuid.UUID(e.OwnerUserID.Bytes)
		body := "Starts at " + e.StartsAt.Format("15:04")
		if _, nerr := q.CreateNotification(ctx, db.CreateNotificationParams{
			TenantID: e.TenantID, UserID: owner, Kind: "event_reminder",
			Title: "Reminder: " + e.Title, Body: body, Link: "/mail/calendar", MeetingID: pgtype.UUID{},
		}); nerr != nil {
			log.Printf("reminder-scan: notify failed for event %s: %v", e.ID, nerr)
			continue
		}
		sendPushToUser(ctx, q, v, owner, map[string]string{"kind": "event_reminder", "title": "Lembrete: " + e.Title, "body": body, "link": "/mail/calendar"})
		_ = q.MarkEventReminded(ctx, e.ID)
	}
	if len(rows) > 0 {
		log.Printf("reminder-scan: fired %d reminder(s)", len(rows))
	}
}

// scanScheduledMessages sends the chat messages whose time has arrived: creates the message and
// publishes it on the hub (same path as a normal send), so whoever is in the channel sees it live;
// then removes it.
func scanScheduledMessages(ctx context.Context, q *db.Queries, hub *chat.Hub) {
	rows, err := q.DueScheduledMessages(ctx)
	if err != nil {
		log.Printf("sched-msg: query failed: %v", err)
		return
	}
	for _, sm := range rows {
		msg, cerr := q.CreateMessage(ctx, db.CreateMessageParams{TenantID: sm.TenantID, ChannelID: sm.ChannelID, UserID: sm.UserID, Body: sm.Body})
		if cerr != nil {
			log.Printf("sched-msg: create failed: %v", cerr)
			continue
		}
		if hub != nil {
			members, _ := q.ListMemberUserIDs(ctx, sm.ChannelID)
			rcpt := make([]string, 0, len(members))
			for _, m := range members {
				rcpt = append(rcpt, m.String())
			}
			payload, _ := json.Marshal(map[string]any{
				"id": msg.ID.String(), "channel_id": sm.ChannelID.String(), "user_id": sm.UserID.String(),
				"parent_id": "", "body": sm.Body, "author": sm.AuthorName, "author_email": sm.AuthorEmail,
				"created_at": msg.CreatedAt.Format(time.RFC3339), "reactions": []any{}, "attachments": []any{}, "reply_count": 0,
			})
			hub.Publish(ctx, chat.Event{Type: "message", ChannelID: sm.ChannelID.String(), Recipients: rcpt, Payload: payload})
		}
		_ = q.PurgeScheduledMessage(ctx, sm.ID)
	}
	if len(rows) > 0 {
		log.Printf("sched-msg: sent %d scheduled message(s)", len(rows))
	}
}

// scanMessageReminders fires the message reminders: bell notification + Web Push, then removes them.
func scanMessageReminders(ctx context.Context, q *db.Queries, v vapidKeys) {
	rows, err := q.DueMessageReminders(ctx)
	if err != nil {
		log.Printf("msg-remind: query failed: %v", err)
		return
	}
	for _, rm := range rows {
		body := rm.Note
		if body == "" {
			body = "Reminder about a message in #" + rm.ChannelName
		}
		args, _ := json.Marshal(map[string]string{"channel": rm.ChannelName, "note": rm.Note})
		if _, nerr := q.CreateNotification(ctx, db.CreateNotificationParams{
			TenantID: rm.TenantID, UserID: rm.UserID, Kind: "chat_reminder",
			Title: "Reminder", Body: body, Link: "/mail/chat", MeetingID: pgtype.UUID{}, Args: args,
		}); nerr != nil {
			log.Printf("msg-remind: notify failed: %v", nerr)
			continue
		}
		sendPushToUser(ctx, q, v, rm.UserID, map[string]string{"kind": "chat_reminder", "title": "Reminder", "body": body, "link": "/mail/chat"})
		_ = q.PurgeMessageReminder(ctx, rm.ID)
	}
	if len(rows) > 0 {
		log.Printf("msg-remind: fired %d reminder(s)", len(rows))
	}
}

// sendPushToUser delivers a Web Push to all of the user's subscriptions. Prunes dead subscriptions.
func sendPushToUser(ctx context.Context, q *db.Queries, v vapidKeys, userID uuid.UUID, payload map[string]string) {
	if v.private == "" {
		return
	}
	subs, err := q.ListPushSubscriptionsByUser(ctx, userID)
	if err != nil {
		return
	}
	msg, _ := json.Marshal(payload)
	for _, sub := range subs {
		resp, err := webpush.SendNotification(msg, &webpush.Subscription{
			Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
		}, &webpush.Options{Subscriber: v.subject, VAPIDPublicKey: v.public, VAPIDPrivateKey: v.private, TTL: 60})
		if err != nil {
			continue
		}
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
			_ = q.DeletePushSubscriptionByEndpoint(ctx, sub.Endpoint)
		}
		resp.Body.Close()
	}
}

// pollNewMail checks each user's mailbox that has push enabled and notifies about new emails
// (arrived after mail_seen_at). The first check only establishes the baseline — avoids an initial
// flood.
func pollNewMail(ctx context.Context, q *db.Queries, box *secretbox.Box, stalwartURL string, v vapidKeys) {
	rows, err := q.UsersForMailPush(ctx)
	if err != nil {
		log.Printf("mail-push: query failed: %v", err)
		return
	}
	notified := 0
	for _, u := range rows {
		basic, derr := box.Decrypt(u.JmapCred)
		if derr != nil || basic == "" {
			continue
		}
		msgs, merr := mailbox.NewUserClient(stalwartURL, basic).InboxRecent(ctx, 20)
		if merr != nil || len(msgs) == 0 {
			continue
		}
		newest := msgs[0].ReceivedAt // sorted newest to oldest
		if !u.MailSeenAt.Valid {
			_ = q.UpdateMailSeen(ctx, db.UpdateMailSeenParams{ID: u.ID, MailSeenAt: pgtype.Timestamptz{Time: newest, Valid: true}})
			continue
		}
		seen := u.MailSeenAt.Time
		sent := 0
		for _, m := range msgs {
			if !m.ReceivedAt.After(seen) {
				break // descending list: the first <= seen ends it
			}
			if sent < 5 { // cap per cycle: don't burst the push service
				title := m.From
				if title == "" {
					title = "Novo e-mail"
				}
				body := m.Subject
				if body == "" {
					body = "(sem assunto)"
				}
				sendPushToUser(ctx, q, v, u.ID, map[string]string{"kind": "new_mail", "title": title, "body": body, "link": "/mail/mail"})
				sent++
				notified++
			}
		}
		if newest.After(seen) {
			_ = q.UpdateMailSeen(ctx, db.UpdateMailSeenParams{ID: u.ID, MailSeenAt: pgtype.Timestamptz{Time: newest, Valid: true}})
		}
	}
	if notified > 0 {
		log.Printf("mail-push: sent %d new-mail notification(s)", notified)
	}
}

// scanSnoozes returns to the Inbox the emails whose snooze deadline has passed. Uses each owner's
// encrypted JMAP credential (decrypted with MFA_ENC_KEY) to act on their mailbox via JMAP.
func scanSnoozes(ctx context.Context, q *db.Queries, box *secretbox.Box, stalwartURL string) {
	rows, err := q.DueSnoozes(ctx)
	if err != nil {
		log.Printf("snooze-scan: query failed: %v", err)
		return
	}
	for _, s := range rows {
		basic, derr := box.Decrypt(s.JmapCred)
		if derr != nil || basic == "" {
			log.Printf("snooze-scan: cannot decrypt cred for snooze %s: %v", s.ID, derr)
			continue // no cred means it cannot be moved; leave it for a later pass (or re-login rewrites the cred)
		}
		uc := mailbox.NewUserClient(stalwartURL, basic)
		if err := uc.MoveToInbox(ctx, s.EmailID); err != nil {
			log.Printf("snooze-scan: move failed for %s: %v", s.ID, err)
			continue
		}
		_ = q.MarkSnoozeDone(ctx, s.ID)
	}
	if len(rows) > 0 {
		log.Printf("snooze-scan: woke %d email(s)", len(rows))
	}
}

// outboxLoop sends the outbox EVENT-DRIVEN: it sleeps until the next send_at (or until a NOTIFY on
// 'outbox_new', which the API fires when it enqueues), sweeps due rows, then sleeps again. No
// constant polling — email goes out almost right after the "undo" window closes. The 60s cap is a
// safety net in case a NOTIFY is missed or the LISTEN connection drops.
func outboxLoop(pool *pgxpool.Pool, q *db.Queries, box *secretbox.Box, stalwartURL string) {
	ctx := context.Background()
	const safetyCap = 60 * time.Second

	// Dedicated connection for LISTEN. If it fails, we carry on with just the timer (degrades to
	// slow 60s polling instead of stopping sending).
	notify := make(chan struct{}, 1)
	if lc, err := pool.Acquire(ctx); err != nil {
		log.Printf("outbox: LISTEN unavailable (%v); falling back to the timer", err)
	} else if _, err := lc.Exec(ctx, "LISTEN outbox_new"); err != nil {
		log.Printf("outbox: LISTEN failed (%v); using timer only", err)
		lc.Release()
	} else {
		go func() {
			defer lc.Release()
			for {
				if _, err := lc.Conn().WaitForNotification(ctx); err != nil {
					log.Printf("outbox: WaitForNotification ended (%v); timer only from here on", err)
					return
				}
				select {
				case notify <- struct{}{}:
				default: // a signal is already pending
				}
			}
		}()
	}

	for {
		scanOutbox(ctx, q, box, stalwartURL)

		wait := safetyCap
		if next, err := q.NextOutboxDue(ctx); err == nil {
			// Something is queued: sleep until then (100ms floor so it does not spin idle), capped.
			d := time.Until(next)
			if d < 100*time.Millisecond {
				d = 100 * time.Millisecond
			}
			if d < wait {
				wait = d
			}
		}
		// err != nil usually = MIN(send_at) NULL (empty queue): sleep the cap waiting for NOTIFY.

		t := time.NewTimer(wait)
		select {
		case <-t.C:
		case <-notify:
			t.Stop()
		}
	}
}

// scanOutbox submits the outbox emails whose send_at has passed (undo-send and scheduled ones).
// Uses the owner's encrypted JMAP credential for the EmailSubmission/set. Called by outboxLoop
// (event-driven). Idempotent by status: only touches 'pending'.
func scanOutbox(ctx context.Context, q *db.Queries, box *secretbox.Box, stalwartURL string) {
	rows, err := q.DueOutbox(ctx)
	if err != nil {
		log.Printf("outbox-scan: query failed: %v", err)
		return
	}
	sent := 0
	for _, o := range rows {
		// Re-checks the plan's daily send limit at actual send time — the outbox entry was already
		// checked on creation, but the limit may have run out between scheduling and delivery.
		if limit, lerr := q.TenantDailySendLimit(ctx, o.TenantID); lerr == nil && limit > 0 {
			if today, terr := q.MailSentToday(ctx, o.TenantID); terr == nil && int(today)+len(o.Rcpts) > int(limit) {
				log.Printf("outbox-scan: %s over daily send limit (%d), marking failed", o.ID, limit)
				_ = q.MarkOutboxFailed(ctx, o.ID)
				continue
			}
		}
		basic, derr := box.Decrypt(o.JmapCred)
		if derr != nil || basic == "" {
			log.Printf("outbox-scan: cannot decrypt cred for outbox %s: %v", o.ID, derr)
			continue // no cred means it cannot be sent; re-login rewrites the cred and the next sweep retries
		}
		uc := mailbox.NewUserClient(stalwartURL, basic)
		if err := uc.SubmitEmail(ctx, o.EmailID, o.IdentityID, o.FromEmail, o.Rcpts); err != nil {
			log.Printf("outbox-scan: submit failed for %s: %v", o.ID, err)
			_ = q.MarkOutboxFailed(ctx, o.ID)
			continue
		}
		_ = q.MarkOutboxSent(ctx, o.ID)
		// Metering (tenant_mail_sent): counts recipients on the ACTUAL send (how SES bills).
		_ = q.IncrementMailSent(ctx, db.IncrementMailSentParams{TenantID: o.TenantID, N: int32(len(o.Rcpts))})
		sent++
	}
	if sent > 0 {
		log.Printf("outbox-scan: sent %d email(s)", sent)
	}
}

// scanDueCards notifies the assignee of cards coming due (by tomorrow) or already overdue, once
// per deadline value (due_notified_for). Dates are 'YYYY-MM-DD' (lexicographic comparison =
// chronological).
func scanDueCards(ctx context.Context, q *db.Queries) {
	today := time.Now().Format("2006-01-02")
	limit := time.Now().AddDate(0, 0, 1).Format("2006-01-02") // includes what's due by tomorrow
	cards, err := q.CardsDueForNotify(ctx, limit)
	if err != nil {
		log.Printf("due-scan: query failed: %v", err)
		return
	}
	for _, c := range cards {
		title := "Due soon"
		if c.Due < today {
			title = "Overdue"
		}
		link := fmt.Sprintf("/mail/kanban/%s/%s", c.BoardID, c.ID)
		if _, err := q.CreateNotification(ctx, db.CreateNotificationParams{
			TenantID: c.TenantID, UserID: c.AssigneeID.Bytes, Kind: "task_due",
			Title: title, Body: c.Title + " · " + c.Due, Link: link, MeetingID: pgtype.UUID{},
		}); err != nil {
			log.Printf("due-scan: notify failed for card %s: %v", c.ID, err)
			continue
		}
		_ = q.MarkCardDueNotified(ctx, c.ID)
	}
	if len(cards) > 0 {
		log.Printf("due-scan: notified %d card(s)", len(cards))
	}
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db pool: %v", err)
	}
	defer pool.Close()

	q := db.New(pool)
	sw := stalwart.New(cfg.StalwartURL, cfg.StalwartAdminID, cfg.StalwartAdminPwd)

	// Real DNS verification by default; in dev/local, test domains do not resolve, so
	// PROVISION_SKIP_DNS_VERIFY=true skips the check. Never turn this on in production.
	var verifier provisioning.DNSVerifier = provisioning.NewNetDNSVerifier()
	if os.Getenv("PROVISION_SKIP_DNS_VERIFY") == "true" {
		verifier = provisioning.AlwaysVerified{}
		log.Printf("WARNING: DNS verification disabled (PROVISION_SKIP_DNS_VERIFY=true)")
	}

	machine := provisioning.NewMachine(sw, verifier, q)
	// SES relay (production, white-label): with SES_REGION + AWS credentials, every new domain is
	// registered in SES with BYODKIM (our own key) and the zone reported hides Amazon
	// (`_spf.<brand>`, DKIM with our key, no MAIL FROM). brand = MAIL_BRAND_DOMAIN (default
	// STALWART_DEFAULT_DOMAIN).
	// The customer's zone must name hosts that exist on the public internet. Stalwart writes its own
	// container hostname into every MX and CNAME, so without this the MX handed to a customer points
	// at a container id and no mail is delivered -- with nothing in the instructions looking wrong.
	mailHost := os.Getenv("STALWART_HOSTNAME")
	machine.WithPublicHosts(mailHost, provisioning.MTASTSHost(mailHost, os.Getenv("MTA_STS_HOSTNAME")))
	if mailHost == "" {
		log.Printf("provisioning: STALWART_HOSTNAME unset — the zone given to customers will name the container, not a public host")
	}

	configureEditionProvisioning(q, machine)

	runner := provisioning.NewRunner(machine, q)

	log.Printf("provisioning worker started")

	// Kanban deadlines + email snoozes on their own ticker (1 min): independent of provisioning.
	// The box decrypts the owners' JMAP credential so the worker can return "snoozed" emails to
	// the inbox.
	box := secretbox.New(cfg.MFAEncKey)
	vapid := vapidKeys{public: cfg.VAPIDPublicKey, private: cfg.VAPIDPrivateKey, subject: cfg.VAPIDSubject}
	// Hub to publish scheduled messages live (same Redis channel as chat). nil = no Redis.
	var hub *chat.Hub
	if cfg.RedisURL != "" {
		if opt, rerr := redis.ParseURL(cfg.RedisURL); rerr == nil {
			hub = chat.New(redis.NewClient(opt))
		} else {
			log.Printf("worker: redis url invalid, scheduled messages won't broadcast live: %v", rerr)
		}
	}
	// Outbox (undo-send/scheduled) on a fast ticker (10s): undo-send needs to go out right after
	// the short window; 1 min would be too slow for the user.
	// The outbox send is EVENT-DRIVEN rather than polled at a fixed interval: the worker sleeps
	// EXACTLY until the next send_at, and the API wakes it with pg_notify('outbox_new') when it
	// enqueues. Email goes out almost right after the "undo" window, with no constant polling. The
	// 60s cap is a safety net in case a NOTIFY is missed.
	go outboxLoop(pool, q, box, cfg.StalwartURL)
	go func() {
		due := time.NewTicker(time.Minute)
		defer due.Stop()
		scanDueCards(context.Background(), q) // once at startup, not only after 1 min
		scanSnoozes(context.Background(), q, box, cfg.StalwartURL)
		scanReminders(context.Background(), q, vapid)
		scanScheduledMessages(context.Background(), q, hub)
		scanMessageReminders(context.Background(), q, vapid)
		pollNewMail(context.Background(), q, box, cfg.StalwartURL, vapid)
		for range due.C {
			scanDueCards(context.Background(), q)
			scanSnoozes(context.Background(), q, box, cfg.StalwartURL)
			scanReminders(context.Background(), q, vapid)
			scanScheduledMessages(context.Background(), q, hub)
			scanMessageReminders(context.Background(), q, vapid)
			pollNewMail(context.Background(), q, box, cfg.StalwartURL, vapid)
		}
	}()

	// Offline recording assembly (track egress → MP4 via ffmpeg; priority queue).
	// Recording assembly and sealing share the same storage service.
	stWorker, err := workerStorage(context.Background(), q)
	if err != nil {
		log.Fatalf("worker: %v", err)
	}
	startEditionWorkers(context.Background(), q, stWorker)
	startAssembly(q, stWorker)
	startSealing(stWorker)
	startWatchdog(os.Getenv("STALWART_URL"))
	startPurgeWorker(q, sw)
	// Bringing mailboxes in from the provider the customer is leaving (internal/mailmigrate).
	// stWorker is the same storage service the recording assembly and sealing already use.
	startMigrationWorker(q, cfg.MFAEncKey, cfg.StalwartURL, stWorker)

	// Background assistant tasks (§7.5 of the assistant design). The executor is built from an
	// httpapi.Server because it needs exactly the same dependencies as the synchronous engine —
	// having two builds of the same engine would mean two risk surfaces free to diverge.
	startAssistantTasks(cfg, q, sw)

	// Wiki (re)embedding for the RAG (docs/wiki-features.md §7.2): idempotent, runs every 30s and
	// processes stale published pages. Without EMBEDDINGS_API_KEY the RAG stays off.
	if emb := ai.NewEmbedderFromEnv(); emb != nil {
		wikiSvc := wiki.New(q)
		go func() {
			t := time.NewTicker(30 * time.Second)
			defer t.Stop()
			for range t.C {
				if n, err := wikiSvc.EmbedPending(context.Background(), emb, 8); err != nil {
					log.Printf("wiki embed: %v", err)
				} else if n > 0 {
					log.Printf("wiki embed: %d page(s) indexed", n)
				}
			}
		}()
		log.Printf("wiki embedding worker started")
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		runner.Tick(context.Background())
	}
}

// startAssistantTasks starts the consumer of the assistant's task queue. Without AI configured,
// it does nothing — and saying so in the log is better than silently consuming nothing.
func startAssistantTasks(cfg *config.Config, q *db.Queries, sw *stalwart.Client) {
	aiProvider, err := ai.New(ai.FromEnv())
	if err != nil || aiProvider == nil {
		// Same distinction the API makes: not configured is a choice, configured-and-broken is a
		// fault, and they used to print the same sentence (law 2.9).
		if os.Getenv("AI_API_KEY") == "" {
			log.Printf("assistant tasks disabled (no AI_API_KEY)")
		} else {
			log.Printf("assistant tasks MISCONFIGURED -- a key is set but no provider could be built: %v", err)
		}
		return
	}
	// Optional storage: without MinIO, docs.create in background tasks returns a translatable
	// error instead of writing into a void. Same behavior as the synchronous server.
	var st *storage.Service
	if ep := os.Getenv("MINIO_ENDPOINT"); ep != "" {
		if s, serr := storage.New(ep, os.Getenv("MINIO_ACCESS_KEY"), os.Getenv("MINIO_SECRET_KEY"),
			os.Getenv("MINIO_BUCKET"), os.Getenv("MINIO_REGION"), os.Getenv("MINIO_SSL") == "true"); serr == nil {
			if err := s.EnableEncryption(q, os.Getenv("FILE_ENC_KEYS")); err != nil {
				log.Fatalf("storage encryption: %v", err)
			}
			st = s
		} else {
			log.Printf("assistant tasks: storage unavailable, docs.create will fail: %v", serr)
		}
	}
	ed, err := edition.Load()
	if err != nil {
		log.Fatalf("edition: %v", err)
	}
	srv := httpapi.NewServer(q, sw, aiProvider, nil, st, billing.ManualProvider{}, billing.Settings{}, ed)
	srv.SetPublicURL(cfg.PublicURL)         // absolute links in the alert e-mails the worker sends
	srv.SetBrandURL(cfg.BrandURL)           // fallback for those links when PUBLIC_URL is unset
	srv.EnableTOTPEncryption(cfg.MFAEncKey) // decrypts the mailboxes' JMAP credential
	worker := httpapi.NewAssistantTaskWorker(srv)

	go func() {
		// Fast polling when there is work, slow when there is not: a task that was just
		// requested should not wait seconds to start, but an empty queue does not need to be
		// checked ten times a second.
		for {
			if !worker.Tick(context.Background()) {
				time.Sleep(3 * time.Second)
			}
		}
	}()
	log.Printf("assistant task worker started")
}
