package main

import (
	"context"
	"log"
	"time"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/mailmigrate"
	"github.com/beskosorg/beskos/backend/internal/secretbox"
	"github.com/beskosorg/beskos/backend/internal/storage"
)

// Bringing mailboxes in from the provider a customer is leaving. See internal/mailmigrate.
//
// ONE JOB AT A TIME, ON PURPOSE. Copying a mailbox is bounded by the OLD server, not by us: Gmail
// and Exchange both throttle a client that fetches too eagerly, and some drop the connection
// outright. Running four migrations at once against the same provider is how an IP gets rate
// limited, which turns four slow migrations into four failed ones. The claim query is written to
// allow several workers (FOR UPDATE SKIP LOCKED) so this can be raised when there is a reason to;
// there is not one yet (law 1.1).

const (
	// Fast while there is work, slow when there is not. A migration the customer just started should
	// not sit for a minute before anything visibly happens -- the first folder appearing is what
	// tells them it is working.
	migrationIdleWait = 5 * time.Second
	// The purge of finished jobs. Daily is plenty for rows that live ninety days.
	migrationPurgeEvery = 24 * time.Hour
)

// startMigrationWorker runs the copier. Without an encryption key it does not start: the source
// password is stored encrypted, so with no key there is nothing this worker could decrypt anyway --
// and the API refuses to create the jobs in the first place.
func startMigrationWorker(q *db.Queries, mfaEncKey, stalwartURL string, st *storage.Service) {
	box := secretbox.New(mfaEncKey)
	if box == nil {
		log.Printf("mailbox migration worker disabled (MFA_ENC_KEY unset — source credentials cannot be stored)")
		return
	}
	if stalwartURL == "" {
		log.Printf("mailbox migration worker disabled (STALWART_URL unset — nowhere to copy mail to)")
		return
	}
	runner := mailmigrate.NewRunner(q, box, stalwartURL)
	if st != nil {
		runner.WithStorage(st)
	} else {
		// Server-sourced jobs still run; a file-sourced one would have nowhere to read from, and
		// says so rather than failing with something that reads like a bug.
		log.Printf("mailbox migration: no object storage, importing from an uploaded file is off")
	}

	go func() {
		for {
			if !runner.Tick(context.Background()) {
				time.Sleep(migrationIdleWait)
			}
		}
	}()

	go func() {
		t := time.NewTicker(migrationPurgeEvery)
		defer t.Stop()
		for range t.C {
			if err := q.PurgeFinishedMailMigrations(context.Background()); err != nil {
				log.Printf("migration purge: %v", err)
			}
		}
	}()

	log.Printf("mailbox migration worker started")
}
