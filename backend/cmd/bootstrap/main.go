// cmd/bootstrap: completes the initial Stalwart bootstrap (once per instance).
// Required before any provisioning (DIVERGENCES §D3). After running, RESTART
// the Stalwart process (docker restart) for the bootstrap to take effect.
//
// Usage:
//
//	STALWART_URL=... STALWART_ADMIN_ID=admin STALWART_ADMIN_SECRET=... \
//	STALWART_HOSTNAME=mail.beskos.app STALWART_DEFAULT_DOMAIN=beskos.app \
//	go run ./cmd/bootstrap
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	sw := stalwart.New(
		env("STALWART_URL", "http://localhost:8080"),
		env("STALWART_ADMIN_ID", "admin"),
		os.Getenv("STALWART_ADMIN_SECRET"),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ok, err := sw.IsBootstrapped(ctx)
	if err != nil {
		log.Fatalf("check bootstrap: %v", err)
	}
	if ok {
		log.Printf("Stalwart is already bootstrapped - nothing to do.")
		return
	}

	res, err := sw.Bootstrap(ctx, stalwart.BootstrapInput{
		ServerHostname: env("STALWART_HOSTNAME", "mail.localhost"),
		DefaultDomain:  env("STALWART_DEFAULT_DOMAIN", "localhost"),
	})
	if err != nil {
		log.Fatalf("bootstrap: %v", err)
	}
	log.Printf("Bootstrap complete. Permanent admin: %s (secret shown only now: %s)", res.Username, res.Secret)
	log.Printf("NOW RESTART STALWART: docker compose restart stalwart")
}
