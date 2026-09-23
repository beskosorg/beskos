// Package sms abstracts SMS sending (MFA codes). Generic interface to swap providers
// (Twilio, etc.) via config only — like internal/ai. Keys only on the backend (law §10).
package sms

import (
	"context"
	"fmt"
	"log"
	"os"
)

// Sender sends a short message to a phone (E.164). Implemented by each provider.
type Sender interface {
	Send(ctx context.Context, phone, message string) error
}

// LogSender only records the send in the log (dev/tests). NEVER use in production. Does not log
// the body (it contains the MFA code) nor the full phone number — it only confirms the send, with
// the phone masked.
type LogSender struct{}

func (LogSender) Send(_ context.Context, phone, _ string) error {
	log.Printf("[SMS:log] sent to %s (body omitted: contains an MFA code)", maskPhone(phone))
	return nil
}

// maskPhone shows only the last few digits: "+5511999998888" -> keeps the last 4.
func maskPhone(p string) string {
	if len(p) <= 4 {
		return "****"
	}
	return "***" + p[len(p)-4:]
}

// Config selects the provider per environment.
type Config struct {
	Provider string // "log" (default) | "sns" (Amazon SNS)
}

func FromEnv() Config { return Config{Provider: getenv("SMS_PROVIDER", "log")} }

// New builds the Sender. The only place that knows about concrete providers (adding one = new
// case). "sns": Amazon SNS (direct Publish to the number). Envs: SNS_REGION (fallback SES_REGION)
// + AWS_ACCESS_KEY_ID/SECRET (same as SES) + optional SMS_SENDER_ID. Fails closed at boot if a
// credential is missing — better not to start than to have MFA by SMS silently dead.
func New(cfg Config) (Sender, error) {
	switch cfg.Provider {
	case "log", "":
		return LogSender{}, nil
	case "sns":
		region := getenv("SNS_REGION", os.Getenv("SES_REGION"))
		key, secret := os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY")
		if region == "" || key == "" || secret == "" {
			return nil, fmt.Errorf("sms: provider sns requires SNS_REGION (or SES_REGION) + AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY")
		}
		return NewSNS(region, key, secret, os.Getenv("SMS_SENDER_ID")), nil
	default:
		return nil, fmt.Errorf("sms: unknown provider %q", cfg.Provider)
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
