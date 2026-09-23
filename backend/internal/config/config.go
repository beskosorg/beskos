// Package config loads configuration via the environment. Secrets NEVER in code (brief §10).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr    string
	DatabaseURL string
	RedisURL    string // real-time chat pub/sub (optional; without it chat falls back to REST only)

	// GLOBAL Stalwart admin — used only by the backend. Never exposed to the frontend (brief §2).
	StalwartURL      string
	StalwartAdminID  string
	StalwartAdminPwd string

	// Default hostname/domain used in the Stalwart bootstrap (DIVERGENCES §D3).
	StalwartHostname      string
	StalwartDefaultDomain string

	// OnlyOffice Document Server (office editing). OnlyOfficeURL = URL the BROWSER uses
	// (api.js + editor); FilesHostURL = URL of OUR API as the Document Server reaches it
	// (download/callback). OnlyOfficeJWTSecret is shared with the DS (signs the config);
	// DocTokenSecret internally signs the download/callback URLs. Secrets via env.
	// OnlyOfficeSourceURL = public address of the modified Document Server source. We run the
	// official image with a branding patch (deploy/onlyoffice/), and AGPL v3 §13 requires
	// offering that source to anyone who uses the editor over the network. Shown in "About".
	OnlyOfficeURL         string
	OnlyOfficeInternalURL string
	FilesHostURL          string
	OnlyOfficeJWTSecret   string
	OnlyOfficeSourceURL   string
	DocTokenSecret        string

	// Meet (LiveKit). LiveKitURL = browser WebSocket; key/secret sign the room tokens.
	// LiveKitHTTPURL = Twirp API (our Go → LiveKit) for recording via Egress.
	LiveKitURL       string
	LiveKitHTTPURL   string
	LiveKitAPIKey    string
	LiveKitAPISecret string

	// E-mail notifications (optional). Without NOTIFY_FROM/HOST, sending stays disabled.
	NotifySMTPHost     string
	NotifySMTPPort     string
	NotifyFrom         string
	NotifySMTPUser     string
	NotifySMTPPassword string
	NotifySMTPInsecure bool

	// Web Push (desktop notifications with Beskos closed). Without VAPID_PRIVATE_KEY, push stays
	// disabled (notifications with an open tab keep working client-side). The VAPID key pair
	// is generated once; the subject is a contact mailto: required by the protocol.
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string

	// Maximum size per file when uploading to Files. Configurable (FILES_MAX_UPLOAD_BYTES, in
	// bytes); default 100 GiB. The plan's total quota still applies on top of this.
	FilesMaxUploadBytes int64

	// Native app (Tauri): allowed CORS origins (APP_ORIGINS, comma-separated), and whether the
	// session cookie is Secure/SameSite=None (COOKIE_SECURE=true in production HTTPS).
	AppOrigins   []string
	CookieSecure bool

	// The app's public URL (PUBLIC_URL, e.g.: https://app.beskos.com), used to build absolute
	// links that go outward — such as a meeting room link in an e-mail. Empty → the
	// links stay relative (useful in dev; in an e-mail they would be useless).
	PublicURL string

	// Branding (BRAND_NAME/BRAND_URL/SALES_EMAIL): the identity used wherever the product faces
	// someone outside it -- OnlyOffice's "about" panel, and the link destination when PUBLIC_URL
	// is empty. A self-hosted instance overrides these with its own.
	BrandName  string
	BrandURL   string
	SalesEmail string
	// SignupsOpen: open self-service registration. When closed, /signup shows "coming soon" and
	// the API refuses — so it can be opened to the public whenever wanted, with no new deploy.
	SignupsOpen bool

	// DevSignupEnabled mounts /auth/signup, a legacy unauthenticated tenant-creation endpoint the
	// e2e scripts (scripts/e2e-*.sh) still use for test convenience. It bypasses SignupsOpen, rate
	// limiting, and the billing signup flow (/public/signup) entirely, so it defaults to OFF and
	// validateProduction refuses to boot with it on in production (security audit) — a stray "true" in
	// an env file must never reopen free, unlimited tenant creation on a real deployment.
	DevSignupEnabled bool

	// Environment: "development" (default) or "production". In production the config fails closed —
	// it refuses empty or weak secrets, or known dev defaults (audit H8/H10).
	Environment string

	// MFAEncKey encrypts the TOTP secret at rest (AES-GCM). Empty = stored in cleartext (dev).
	MFAEncKey string
}

// IsProduction reports whether we are in production (APP_ENV=production).
func (c *Config) IsProduction() bool { return c.Environment == "production" }

// knownDevSecrets are development default values that must NEVER go to production.
var knownDevSecrets = map[string]bool{
	"dev-oo-jwt-secret-change-me":            true,
	"dev-doc-token-secret-change-me":         true,
	"devkey":                                 true,
	"devsecret_livekit_change_me_0123456789": true,
	"dev-admin-secret":                       true,
	"devpass":                                true,
	"devminio123":                            true,
	"changeme":                               true,
}

// defaultOfficeSourceURL is where we publish the modified Document Server source we serve —
// the offer AGPL v3 §13 requires. It is not a secret and does not vary by environment, and
// forgetting it used to bring down the boot in production, so it ships as a default instead of
// a manual step in the panel.
// WARNING: whoever changes the branding patch MUST point this (or the env var) at their own
// repository — otherwise the editor promises users a source that does not match what is running.
const defaultOfficeSourceURL = "https://github.com/denerFernandes/beskos-docs-server"

func Load() (*Config, error) {
	c := &Config{
		HTTPAddr:              getenv("HTTP_ADDR", ":8000"),
		DatabaseURL:           os.Getenv("DATABASE_URL"),
		RedisURL:              getenv("REDIS_URL", ""),
		StalwartURL:           getenv("STALWART_URL", "http://localhost:8080"),
		StalwartAdminID:       getenv("STALWART_ADMIN_ID", "admin"),
		StalwartAdminPwd:      os.Getenv("STALWART_ADMIN_SECRET"),
		StalwartHostname:      getenv("STALWART_HOSTNAME", "mail.localhost"),
		StalwartDefaultDomain: getenv("STALWART_DEFAULT_DOMAIN", "localhost"),
		OnlyOfficeURL:         getenv("ONLYOFFICE_URL", ""),
		OnlyOfficeInternalURL: getenv("ONLYOFFICE_INTERNAL_URL", ""),
		FilesHostURL:          getenv("FILES_HOST_URL", ""),
		OnlyOfficeJWTSecret:   os.Getenv("ONLYOFFICE_JWT_SECRET"),
		OnlyOfficeSourceURL:   strings.TrimRight(getenv("ONLYOFFICE_SOURCE_URL", defaultOfficeSourceURL), "/"),
		DocTokenSecret:        os.Getenv("DOC_TOKEN_SECRET"),
		LiveKitURL:            getenv("LIVEKIT_URL", ""),
		LiveKitHTTPURL:        getenv("LIVEKIT_HTTP_URL", ""),
		LiveKitAPIKey:         getenv("LIVEKIT_API_KEY", ""),
		LiveKitAPISecret:      os.Getenv("LIVEKIT_API_SECRET"),
		NotifySMTPHost:        getenv("NOTIFY_SMTP_HOST", ""),
		NotifySMTPPort:        getenv("NOTIFY_SMTP_PORT", "587"),
		// Sender for customer notices. The address has to exist as a mailbox in Stalwart (or be
		// accepted by the configured SMTP) — otherwise the server refuses to send.
		NotifyFrom:          getenv("NOTIFY_FROM", "Beskos <noreply@beskos.com>"),
		BrandName:           getenv("BRAND_NAME", "Beskos"),
		BrandURL:            getenv("BRAND_URL", "https://beskos.com"),
		SalesEmail:          getenv("SALES_EMAIL", "sales@beskos.com"),
		NotifySMTPUser:      getenv("NOTIFY_SMTP_USER", ""),
		NotifySMTPPassword:  os.Getenv("NOTIFY_SMTP_PASSWORD"),
		NotifySMTPInsecure:  getenv("NOTIFY_SMTP_INSECURE", "false") == "true",
		VAPIDPublicKey:      getenv("VAPID_PUBLIC_KEY", ""),
		VAPIDPrivateKey:     os.Getenv("VAPID_PRIVATE_KEY"),
		VAPIDSubject:        getenv("VAPID_SUBJECT", "mailto:admin@localhost"),
		FilesMaxUploadBytes: getenvInt64("FILES_MAX_UPLOAD_BYTES", 100<<30), // 100 GiB
		AppOrigins:          splitList(getenv("APP_ORIGINS", "")),
		CookieSecure:        getenv("COOKIE_SECURE", "false") == "true",
		PublicURL:           strings.TrimRight(getenv("PUBLIC_URL", ""), "/"),
		SignupsOpen:         getenv("SIGNUPS_OPEN", "true") != "false",
		DevSignupEnabled:    getenv("DEV_SIGNUP_ENABLED", "false") == "true",
		Environment:         getenv("APP_ENV", "development"),
		MFAEncKey:           os.Getenv("MFA_ENC_KEY"),
	}
	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if c.IsProduction() {
		if err := c.validateProduction(); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// validateProduction refuses to boot when a required secret for an enabled feature is empty,
// weak, or a known dev default — instead of coming up in an insecure state (audit H8/H10).
func (c *Config) validateProduction() error {
	var errs []string
	// checkSecret requires a strong secret (≥16 chars) and a non-default one when `enabled`.
	checkSecret := func(name, val string, enabled bool) {
		if !enabled {
			return
		}
		switch {
		case val == "":
			errs = append(errs, name+" is required in production")
		case knownDevSecrets[val]:
			errs = append(errs, name+" uses a known dev default — set a real secret")
		case len(val) < 16:
			errs = append(errs, name+" is too short (need ≥16 chars)")
		}
	}
	checkSecret("STALWART_ADMIN_SECRET", c.StalwartAdminPwd, true)
	// Encrypts the JMAP credential and the TOTP secret at rest — mandatory in production (brokering H11).
	checkSecret("MFA_ENC_KEY", c.MFAEncKey, true)
	checkSecret("ONLYOFFICE_JWT_SECRET", c.OnlyOfficeJWTSecret, c.OnlyOfficeURL != "")
	checkSecret("DOC_TOKEN_SECRET", c.DocTokenSecret, c.OnlyOfficeURL != "")
	checkSecret("LIVEKIT_API_SECRET", c.LiveKitAPISecret, c.LiveKitURL != "")
	// AGPL v3 §13 compliance: we serve a modified Document Server, so operators have to tell
	// their own users where the modified source is. c.OnlyOfficeSourceURL is never empty --
	// it falls back to defaultOfficeSourceURL, which names this project's own fork, not the
	// operator's -- so the check reads the raw env var to require an explicit value, not the
	// defaulted field, or an operator who never set it would silently ship a notice pointing
	// their users at someone else's repository.
	if c.OnlyOfficeURL != "" && os.Getenv("ONLYOFFICE_SOURCE_URL") == "" {
		errs = append(errs, "ONLYOFFICE_SOURCE_URL is required in production (AGPL v3 §13: publish the modified Document Server source) -- point it at your own fork, not the upstream default")
	}
	if c.LiveKitURL != "" && knownDevSecrets[c.LiveKitAPIKey] {
		errs = append(errs, "LIVEKIT_API_KEY uses a known dev default — set a real key")
	}
	if c.VAPIDPrivateKey != "" && knownDevSecrets[c.VAPIDPrivateKey] {
		errs = append(errs, "VAPID_PRIVATE_KEY uses a known dev default")
	}
	// Transport hygiene in production.
	if !c.CookieSecure {
		errs = append(errs, "COOKIE_SECURE must be true in production (session cookie over HTTPS)")
	}
	if c.NotifySMTPInsecure {
		errs = append(errs, "NOTIFY_SMTP_INSECURE must be false in production (TLS verification)")
	}
	if c.DevSignupEnabled {
		errs = append(errs, "DEV_SIGNUP_ENABLED must be false in production (unauthenticated, unlimited tenant creation)")
	}
	if len(errs) > 0 {
		return fmt.Errorf("insecure production config: %s", strings.Join(errs, "; "))
	}
	return nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// splitList splits "a, b ,c" into a clean list (empty → nil).
func splitList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func getenvInt64(k string, def int64) int64 {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return def
}
