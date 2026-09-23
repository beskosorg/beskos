package billing

import (
	"os"
	"strconv"
	"strings"
)

// Config selects the provider via the environment.
type Config struct {
	Provider      string // "manual" (default) | "stripe"
	SecretKey     string
	WebhookSecret string
	SuccessURL    string
	CancelURL     string
	Currency      string
	MinPaidUsers  int
}

func FromEnv() Config {
	return Config{
		Provider:      getenv("BILLING_PROVIDER", "manual"),
		SecretKey:     os.Getenv("STRIPE_SECRET_KEY"),
		WebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		SuccessURL:    getenv("BILLING_SUCCESS_URL", "https://app.beskos.com/billing?ok=1"),
		CancelURL:     getenv("BILLING_CANCEL_URL", "https://app.beskos.com/billing?canceled=1"),
		Currency:      strings.ToUpper(getenv("BILLING_CURRENCY", "USD")),
		MinPaidUsers:  atoiDefault(os.Getenv("BILLING_MIN_PAID_USERS"), 1),
	}
}

// Settings exposes the global settings (currency + minimum users) for the handlers/UI.
func (c Config) Settings() Settings {
	cur := c.Currency
	if cur != "BRL" && cur != "EUR" && cur != "USD" && cur != "PLN" && cur != "GBP" {
		cur = "USD"
	}
	min := c.MinPaidUsers
	if min < 1 {
		min = 1
	}
	return Settings{Currency: cur, MinPaidUsers: min}
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
