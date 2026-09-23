package config

import (
	"strings"
	"testing"
)

// setProductionBase sets every env var validateProduction requires regardless of which
// optional features are enabled, so a test only has to touch the variable it's exercising.
func setProductionBase(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://localhost/beskos")
	t.Setenv("STALWART_ADMIN_SECRET", "a-real-32-byte-secret-value-here")
	t.Setenv("MFA_ENC_KEY", "another-real-32-byte-secret-here")
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("NOTIFY_SMTP_INSECURE", "false")
}

// A production boot with OnlyOffice enabled must require an operator-supplied
// ONLYOFFICE_SOURCE_URL, not silently accept defaultOfficeSourceURL -- that constant names this
// project's own fork, and an operator who never set the variable would otherwise ship an AGPL
// v3 §13 notice pointing their users at someone else's repository instead of their own.
func TestOnlyOfficeSourceURLRequiredWhenUnset(t *testing.T) {
	setProductionBase(t)
	t.Setenv("ONLYOFFICE_URL", "http://onlyoffice:80")
	t.Setenv("ONLYOFFICE_JWT_SECRET", "a-real-32-byte-secret-value-here")
	t.Setenv("DOC_TOKEN_SECRET", "a-real-32-byte-secret-value-here")
	t.Setenv("ONLYOFFICE_SOURCE_URL", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() succeeded with OnlyOffice enabled and no explicit ONLYOFFICE_SOURCE_URL")
	}
	if !strings.Contains(err.Error(), "ONLYOFFICE_SOURCE_URL") {
		t.Fatalf("error does not mention ONLYOFFICE_SOURCE_URL: %v", err)
	}
}

func TestOnlyOfficeSourceURLAcceptedWhenSet(t *testing.T) {
	setProductionBase(t)
	t.Setenv("ONLYOFFICE_URL", "http://onlyoffice:80")
	t.Setenv("ONLYOFFICE_JWT_SECRET", "a-real-32-byte-secret-value-here")
	t.Setenv("DOC_TOKEN_SECRET", "a-real-32-byte-secret-value-here")
	t.Setenv("ONLYOFFICE_SOURCE_URL", "https://github.com/example-org/our-onlyoffice-fork")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed with an explicit ONLYOFFICE_SOURCE_URL: %v", err)
	}
	if c.OnlyOfficeSourceURL != "https://github.com/example-org/our-onlyoffice-fork" {
		t.Fatalf("OnlyOfficeSourceURL = %q, want the operator-supplied value", c.OnlyOfficeSourceURL)
	}
}

func TestOnlyOfficeSourceURLNotRequiredWhenOnlyOfficeDisabled(t *testing.T) {
	setProductionBase(t)
	t.Setenv("ONLYOFFICE_URL", "")
	t.Setenv("ONLYOFFICE_SOURCE_URL", "")

	if _, err := Load(); err != nil {
		t.Fatalf("Load() failed with OnlyOffice disabled: %v", err)
	}
}

// DevSignupEnabled mounts an unauthenticated, unlimited tenant-creation route meant only for the
// e2e scripts. A production boot must refuse it outright rather than trust that whoever set the
// env var meant to -- unlike SIGNUPS_OPEN, there is no legitimate reason for this to be true on a
// real deployment, so it gets a hard refusal instead of a warning (security audit).
func TestDevSignupEnabledRefusedInProduction(t *testing.T) {
	setProductionBase(t)
	t.Setenv("DEV_SIGNUP_ENABLED", "true")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() succeeded in production with DEV_SIGNUP_ENABLED=true")
	}
	if !strings.Contains(err.Error(), "DEV_SIGNUP_ENABLED") {
		t.Fatalf("error does not mention DEV_SIGNUP_ENABLED: %v", err)
	}
}

func TestDevSignupDisabledByDefaultInProduction(t *testing.T) {
	setProductionBase(t)

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed with DEV_SIGNUP_ENABLED unset: %v", err)
	}
	if c.DevSignupEnabled {
		t.Fatal("DevSignupEnabled defaulted to true")
	}
}
