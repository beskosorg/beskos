// cmd/api: the control plane's REST/JSON API (customer panel + internal admin).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"
	_ "time/tzdata" // embeds the IANA timezone database in the binary (slim containers lack tzdata)

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/beskosorg/beskos/backend/ee"
	"github.com/beskosorg/beskos/backend/internal/ai"
	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/chat"
	"github.com/beskosorg/beskos/backend/internal/config"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/edition"
	"github.com/beskosorg/beskos/backend/internal/httpapi"
	"github.com/beskosorg/beskos/backend/internal/mailaudit"
	"github.com/beskosorg/beskos/backend/internal/notify"
	"github.com/beskosorg/beskos/backend/internal/provisioning"
	"github.com/beskosorg/beskos/backend/internal/sms"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
	"github.com/beskosorg/beskos/backend/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// The mail audit resolves DNS through this server when set. Without it the blocklist checks are
	// answered by a refusal from every list -- see mailaudit.UseResolver.
	mailaudit.UseResolver(os.Getenv("AUDIT_RESOLVER"))

	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db pool: %v", err)
	}
	defer pool.Close()

	sw := stalwart.New(cfg.StalwartURL, cfg.StalwartAdminID, cfg.StalwartAdminPwd)

	// AI: optional. Without AI_API_KEY, the AI endpoints respond 503 (feature disabled).
	var aiProvider ai.Provider
	if p, err := ai.New(ai.FromEnv()); err != nil {
		// Two different situations printed the same line. No key at all means the assistant was
		// never wanted; a key plus a broken endpoint means somebody configured it and it does not
		// work -- and the product starts anyway, answering 503 to every AI request. That has
		// already happened here once, unnoticed, for the length of a deployment (law 2.9).
		if os.Getenv("AI_API_KEY") == "" {
			log.Printf("AI disabled (no AI_API_KEY)")
		} else {
			log.Printf("AI MISCONFIGURED -- a key is set but the assistant cannot start: %v", err)
		}
	} else {
		aiProvider = p
		log.Printf("AI enabled (provider=%s)", p.Name())
		// "Enabled" only means the configuration parsed. Whether the endpoint still offers the
		// model is a separate question, and the answer changes without anyone touching this
		// installation. Asked in the background so a slow endpoint does not hold up startup.
		if c, ok := p.(*ai.Client); ok {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				if err := c.CheckModel(ctx); err != nil {
					log.Printf("AI MISCONFIGURED -- %v", err)
				}
			}()
		}
	}
	aiCfg := ai.FromEnv() // raw endpoint/key/model for the OnlyOffice editor's AI proxy

	// SMS for MFA. Default "log" (dev). Real provider (Twilio) via SMS_PROVIDER.
	smsSender, err := sms.New(sms.FromEnv())
	if err != nil {
		log.Fatalf("sms: %v", err)
	}

	// The same *db.Queries serves the server and the file encryption keys.
	q := db.New(pool)

	// Storage (MinIO) optional. Without MINIO_ENDPOINT, the file endpoints respond 503.
	var st *storage.Service
	if ep := os.Getenv("MINIO_ENDPOINT"); ep != "" {
		bucket := os.Getenv("MINIO_BUCKET")
		if bucket == "" {
			bucket = "beskos"
		}
		st, err = storage.New(ep, os.Getenv("MINIO_ACCESS_KEY"), os.Getenv("MINIO_SECRET_KEY"),
			bucket, os.Getenv("MINIO_REGION"), os.Getenv("MINIO_SSL") == "true")
		if err != nil {
			log.Fatalf("storage: %v", err)
		}
		// Envelope encryption: without FILE_ENC_KEYS the service writes in plaintext, as before. A
		// malformed key is fatal on purpose — starting up with it would write objects that nobody
		// could open again. See the file-encryption design doc under docs/.
		if err := st.EnableEncryption(q, os.Getenv("FILE_ENC_KEYS")); err != nil {
			log.Fatalf("storage encryption: %v", err)
		}
		if st.Encrypting() {
			log.Printf("storage enabled (%s), envelope encryption ON", ep)
		} else {
			log.Printf("storage enabled (%s), NOT encrypted (set FILE_ENC_KEYS)", ep)
		}
	} else {
		log.Printf("storage disabled (set MINIO_ENDPOINT)")
	}

	// Billing. Default "manual" (dev/self-serve). Production: BILLING_PROVIDER=stripe + keys.
	billingCfg := billing.FromEnv()
	billingProvider, err := ee.NewBillingProvider(billingCfg)
	if err != nil {
		log.Fatalf("billing: %v", err)
	}
	bset := billingCfg.Settings()
	log.Printf("billing enabled (provider=%s currency=%s min_paid_users=%d)", billingProvider.Name(), bset.Currency, bset.MinPaidUsers)

	ed, err := edition.Load()
	if err != nil {
		log.Fatalf("edition: %v", err)
	}
	log.Printf("edition: %s", ed.Edition)

	srv := httpapi.NewServer(q, sw, aiProvider, smsSender, st, billingProvider, bset, ed)

	// Readiness (/readyz): pings Postgres. The orchestrator only forwards traffic once the database responds.
	srv.SetReadiness(pool.Ping)

	enableHostedServices(srv)

	// Encryption of the TOTP secret at rest (optional; without MFA_ENC_KEY it stores in plaintext — dev only).
	srv.EnableTOTPEncryption(cfg.MFAEncKey)
	if cfg.MFAEncKey != "" {
		log.Printf("TOTP secret encryption enabled")
	}

	// Office editing via OnlyOffice Document Server (optional). Without URLs/secrets, it stays off.
	srv.EnableOnlyOffice(cfg.OnlyOfficeURL, cfg.OnlyOfficeInternalURL, cfg.FilesHostURL, cfg.OnlyOfficeJWTSecret, cfg.DocTokenSecret)
	// Brand shown in the editor's "about" panel. sourceURL is the modified-source offer AGPL §13
	// requires -- config already refuses to start without it in production; in dev, "about" ships
	// without the address. Falls back to BRAND_URL when PUBLIC_URL is unset (self-hosted, no
	// public domain yet).
	brandLinkURL := cfg.PublicURL
	if brandLinkURL == "" {
		brandLinkURL = cfg.BrandURL
	}
	srv.SetOfficeBranding(cfg.OnlyOfficeSourceURL, brandLinkURL)
	if cfg.OnlyOfficeURL != "" {
		log.Printf("office editing enabled (OnlyOffice %s)", cfg.OnlyOfficeURL)
		if cfg.OnlyOfficeSourceURL == "" {
			log.Printf("warning: ONLYOFFICE_SOURCE_URL is empty — the editor's About box cannot offer the modified source")
		}
	}

	// Public app URL: used for absolute links that go out in emails (e.g. a meeting room).
	srv.SetPublicURL(cfg.PublicURL)
	// Public mail host: what we show whoever connects an external client (IMAP/SMTP).
	srv.SetMailHost(cfg.StalwartHostname)
	// The host serving MTA-STS policies. The API needs it because it corrects the zone ON READ: a
	// domain provisioned before that substitution existed would name the container forever.
	srv.SetMTASTSHost(provisioning.MTASTSHost(cfg.StalwartHostname, os.Getenv("MTA_STS_HOSTNAME")))

	// AI proxy for the OnlyOffice plugin: forwards to DeepSeek with the server-side key.
	if aiProvider != nil {
		srv.EnableAIProxy(aiCfg.BaseURL, aiCfg.APIKey, aiCfg.Model)
	}

	// Meet via LiveKit (opcional).
	srv.EnableMeet(cfg.LiveKitURL, cfg.LiveKitAPIKey, cfg.LiveKitAPISecret)
	if cfg.LiveKitURL != "" {
		log.Printf("meet enabled (LiveKit %s)", cfg.LiveKitURL)
	}
	// Recording via Egress (optional) — uses LiveKit's Twirp API with MinIO as the destination.
	if cfg.LiveKitHTTPURL != "" && os.Getenv("MINIO_ENDPOINT") != "" {
		srv.EnableMeetRecording(cfg.LiveKitHTTPURL, os.Getenv("MINIO_ENDPOINT"),
			os.Getenv("MINIO_ACCESS_KEY"), os.Getenv("MINIO_SECRET_KEY"), os.Getenv("MINIO_BUCKET"))
		log.Printf("meet recording enabled (Egress → MinIO)")
	}

	// Notification email (optional). Off, every customer-facing alert — account created,
	// trial, billing, new login, invitations and shares with outside people — stops going out,
	// and without this warning at startup nobody notices why.
	if em := notify.NewEmail(cfg.NotifySMTPHost, cfg.NotifySMTPPort, cfg.NotifyFrom, cfg.NotifySMTPUser, cfg.NotifySMTPPassword, cfg.NotifySMTPInsecure); em != nil {
		srv.EnableEmail(em)
		log.Printf("email notifications enabled (from %s via %s)", cfg.NotifyFrom, cfg.NotifySMTPHost)
	} else {
		log.Printf("WARNING: email notifications DISABLED (missing NOTIFY_SMTP_HOST/NOTIFY_FROM) — " +
			"account, trial, billing, login alerts and external shares will not be delivered")
	}

	// Where the website's contact form delivers. A real mailbox somebody reads, not a table.
	srv.SetSalesEmail(cfg.SalesEmail)
	srv.SetBrandURL(cfg.BrandURL)

	srv.SetSignupsOpen(cfg.SignupsOpen)
	if !cfg.SignupsOpen {
		log.Printf("self-service signups CLOSED (SIGNUPS_OPEN=false) — /signup shows the coming-soon page")
	}

	srv.SetDevSignupEnabled(cfg.DevSignupEnabled)
	if cfg.DevSignupEnabled {
		log.Printf("WARNING: /auth/signup is ENABLED (DEV_SIGNUP_ENABLED=true) — unauthenticated, unlimited tenant creation. Dev/e2e use only, never in production (config.validateProduction refuses it there).")
	}

	// Per-file cap in Files (configurable; default 100 GiB).
	srv.SetFileUploadLimit(cfg.FilesMaxUploadBytes)

	// CORS + cross-site cookie for the native app (Tauri). Without APP_ORIGINS, it stays off.
	srv.SetAppOrigins(cfg.AppOrigins, cfg.CookieSecure)
	if len(cfg.AppOrigins) > 0 {
		log.Printf("CORS enabled for app origins: %v (cookie secure=%v)", cfg.AppOrigins, cfg.CookieSecure)
	}

	// Web Push (optional). Without VAPID_PRIVATE_KEY, desktop push with the app closed stays off
	// (notifications with an open tab keep working client-side).
	if cfg.VAPIDPrivateKey != "" && cfg.VAPIDPublicKey != "" {
		srv.EnablePush(cfg.VAPIDPublicKey, cfg.VAPIDPrivateKey, cfg.VAPIDSubject)
		log.Printf("web push enabled")
	}

	// Realtime chat via Redis pub/sub (optional). Without REDIS_URL, chat stays REST-only.
	if cfg.RedisURL != "" {
		opt, rerr := redis.ParseURL(cfg.RedisURL)
		if rerr != nil {
			log.Fatalf("redis url: %v", rerr)
		}
		hub := chat.New(redis.NewClient(opt))
		go hub.Run(context.Background())
		srv.EnableChat(hub)
		log.Printf("chat realtime enabled (Redis %s)", cfg.RedisURL)
	}

	log.Printf("control plane listening on %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, srv.Router()); err != nil {
		log.Fatalf("http: %v", err)
	}
}
