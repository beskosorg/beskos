// Package httpapi exposes the control plane API (customer panel + internal admin).
// Every administrative operation passes through here and checks tenant ownership before
// talking to Stalwart (brief §2/§10). The frontend NEVER receives a Stalwart admin credential.
//
// This file holds the Server type and its constructor; the route table lives in server.go, kept
// apart so neither file grows into the other (law §1.4).
package httpapi

import (
	"context"
	"time"

	"github.com/beskosorg/beskos/backend/internal/ai"
	"github.com/beskosorg/beskos/backend/internal/assistant"
	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/chat"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/edition"
	"github.com/beskosorg/beskos/backend/internal/notify"
	"github.com/beskosorg/beskos/backend/internal/secretbox"
	"github.com/beskosorg/beskos/backend/internal/sms"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
	"github.com/beskosorg/beskos/backend/internal/storage"
	"github.com/beskosorg/beskos/backend/internal/wiki"
)

type Server struct {
	q           *db.Queries
	sw          *stalwart.Client
	ai          ai.Provider // may be nil if AI is not configured
	sms         sms.Sender
	storage     *storage.Service // may be nil if MinIO is not configured
	billing     billing.Provider
	signupsOpen bool
	// devSignupEnabled mounts the legacy /auth/signup route (see SetDevSignupEnabled). Off unless
	// explicitly turned on; config.validateProduction refuses to boot with it on in production.
	devSignupEnabled bool
	billingCfg       billing.Settings
	office           *onlyoffice   // office editing (OnlyOffice Document Server); nil = off
	meet             *meet         // video calling (LiveKit); nil = off
	salesEmail       string        // where the website's contact form delivers; empty = form disabled
	email            *notify.Email // e-mail sending (notifications); nil = off
	hub              *chat.Hub     // chat's real-time transport (Redis pub/sub); nil = off
	wiki             *wiki.Service // knowledge base (Confluence-style); service layer over the queries
	wikiEmb          *ai.Embedder  // wiki RAG embeddings (nil = semantic search off)
	push             *webPush      // Web Push (desktop notifications with the app closed); nil = off
	maxUpload        int64         // per-file cap on uploads to Files (bytes); 0 = default
	corsOrigins      []string      // origins allowed for CORS (native Tauri app, external web)
	cookieSecure     bool          // session cookie as Secure+SameSite=None (HTTPS/cross-site)
	publicURL        string        // app's public URL for absolute links that go out in e-mails (no trailing "/")
	brandURL         string        // fallback for appURL when publicURL is unset (BRAND_URL)
	mailHost         string        // public hostname of the mail server (IMAP/SMTP/JMAP) for external clients
	mtaStsHost       string        // public name serving MTA-STS policies; every customer's mta-sts.<domain> points here
	aiProxyBase      string        // DeepSeek endpoint (OpenAI-compat) for the editor's AI proxy
	aiProxyKey       string        // real DeepSeek key — server-side ONLY, never reaches the browser
	aiProxyModel     string        // model to force in the proxy (e.g. deepseek-chat)

	loginIP    *rateLimiter // login attempts per IP
	loginUser  *rateLimiter // login attempts per account (e-mail)
	mfaIP      *rateLimiter // second-factor checks per IP (locks down challenge rotation)
	smsIP      *rateLimiter // SMS MFA enrollment sends per IP (cost/abuse)
	smsUser    *rateLimiter // SMS MFA enrollment sends per account (a phone is user-supplied, not yet trusted)
	aiLimiter  *rateLimiter // AI calls per user (cost/abuse)
	pubLimiter *rateLimiter // public endpoints (Meet without login) per IP
	// presLimiter: its own bucket for the presentation PDF. A PDF is read in BURSTS (dozens of
	// requests for a large deck) and the whole room can be behind the same IP (office, home) — in
	// the shared public bucket of 60/min, two participants alone were enough to trigger 429s and
	// the presentation would never open. Still bounded (not an open tap), but with real headroom.
	presLimiter *rateLimiter
	// agentLimiter has its own bucket: one agentic run spends several calls to the model, so it
	// can't share the limit meant for a single-step AI call.
	agentLimiter *rateLimiter
	// confirms holds the assistant's actions awaiting human approval (§6.3).
	confirms *assistantConfirms

	// planCat is the cache of the plan catalog loaded from the database; see plans_catalog.go.
	planCat planCatalog

	// edition holds which Pro capabilities this instance has unlocked (edition.Load at startup).
	edition edition.Capabilities

	// agent is the assistant's agentic engine (think→act→observe cycle). The underlying provider
	// may be nil if AI is not configured — the handler is what checks.
	agent *assistant.Engine
	// agentRouter picks the agent profile from the user's message (§3.2). Kept OUTSIDE the
	// engine on purpose: routing is decided before the cycle starts, never midway.
	agentRouter *assistant.Router

	totpBox *secretbox.Box // encrypts the TOTP secret at rest (nil = stored in plaintext)

	hostedServices

	readiness func(context.Context) error // tests critical dependencies (e.g. ping to Postgres) in /readyz; nil = liveness only
}

func NewServer(q *db.Queries, sw *stalwart.Client, aiProvider ai.Provider, smsSender sms.Sender, st *storage.Service, bp billing.Provider, bset billing.Settings, ed edition.Capabilities) *Server {
	srv := &Server{
		q: q, sw: sw, ai: aiProvider, sms: smsSender, storage: st, billing: bp, billingCfg: bset, edition: ed,
		signupsOpen:    true, // open by default; main applies the actual configuration
		hostedServices: newHostedServices(),
		loginIP:        newRateLimiter(30, 5*time.Minute),
		loginUser:      newRateLimiter(8, 5*time.Minute),
		mfaIP:          newRateLimiter(20, 5*time.Minute),
		smsIP:          newRateLimiter(10, time.Hour),
		smsUser:        newRateLimiter(5, time.Hour),
		aiLimiter:      newRateLimiter(30, time.Minute),
		pubLimiter:     newRateLimiter(60, time.Minute),
		presLimiter:    newRateLimiter(600, time.Minute),
		// 10 runs per 5 minutes: each can spend up to DefaultMaxSteps calls to the model.
		agentLimiter: newRateLimiter(10, 5*time.Minute),
		confirms:     newAssistantConfirms(),
		wiki:         wiki.New(q),
		wikiEmb:      ai.NewEmbedderFromEnv(),
	}
	srv.agent = srv.buildAssistant()
	srv.agentRouter = assistant.NewRouter(aiProvider)
	return srv
}
