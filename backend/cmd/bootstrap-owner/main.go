// cmd/bootstrap-owner creates the first organization and owner mailbox on a self-hosted
// installation. A fresh Community install has no public sign-up route (that path is hosted-only);
// this is how a self-hosted operator gets in the first time. It refuses to run once any
// organization already exists, so it is safe to leave in an install script or a Makefile target.
//
// Usage (after Stalwart itself is bootstrapped and the api/worker migrations have run):
//
//	DATABASE_URL=... STALWART_URL=... STALWART_ADMIN_ID=admin STALWART_ADMIN_SECRET=... \
//	OWNER_ORG_NAME="Acme" OWNER_DOMAIN=acme.example OWNER_LOGIN=admin OWNER_NAME="Jane Doe" \
//	go run ./cmd/bootstrap-owner
//
// OWNER_PASSWORD is optional: when unset, a random password is generated and printed once, the
// same convention cmd/bootstrap already uses for the Stalwart recovery admin.
package main

import (
	"context"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/beskosorg/beskos/backend/internal/auth"
	"github.com/beskosorg/beskos/backend/internal/billing"
	"github.com/beskosorg/beskos/backend/internal/config"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/provisioning"
	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db pool: %v", err)
	}
	defer pool.Close()
	q := db.New(pool)

	if n, err := q.CountTenants(ctx); err != nil {
		log.Fatalf("count tenants: %v", err)
	} else if n > 0 {
		log.Fatalf("this installation already has %d organization(s); bootstrap-owner only runs on an empty database", n)
	}

	password := os.Getenv("OWNER_PASSWORD")
	generated := password == ""
	if generated {
		password, err = auth.NewSessionToken()
		if err != nil {
			log.Fatalf("generate password: %v", err)
		}
	}
	in := provisioning.OwnerInput{
		OrgName:  os.Getenv("OWNER_ORG_NAME"),
		Domain:   os.Getenv("OWNER_DOMAIN"),
		Login:    os.Getenv("OWNER_LOGIN"),
		Name:     os.Getenv("OWNER_NAME"),
		Password: password,
	}

	sw := stalwart.New(cfg.StalwartURL, cfg.StalwartAdminID, cfg.StalwartAdminPwd)
	res, err := provisioning.CreateOwner(ctx, q, sw, billing.DefaultPlanID, true, in)
	if err != nil {
		log.Fatalf("bootstrap owner: %v", err)
	}

	log.Printf("Organization %q created. Owner: %s", res.Tenant.Name, res.Email)
	if generated {
		log.Printf("Generated password (shown only now): %s", password)
	}
	log.Printf("The domain still needs its DNS records published before external mail flows; see docs/self-hosting.md.")
}
