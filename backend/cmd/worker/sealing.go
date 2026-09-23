package main

import (
	"context"
	"log"
	"time"

	"github.com/beskosorg/beskos/backend/internal/storage"
)

// Background sealing: walks the bucket and encrypts whatever is still in plaintext.
//
// Covers both cases with the same mechanism, because they are the same problem — an object in
// storage that did not go through our encryption:
//
//   - content predating encryption existing;
//   - composed recordings, written directly by LiveKit Egress with S3 credentials.
//
// Holds no state at all about progress. What decides whether an object is already sealed is the
// object itself (the magic in the header), which makes every pass idempotent and immune to
// restarts: a database marker that got out of sync would lead to encrypting the encrypted.
//
// PACE. A pause between objects, on purpose: this runs on the same server that serves customers'
// mail, and migrating fast is not worth interrupting whoever is working. A pass that does not
// finish is not a problem — the next one continues where the previous one stopped, because it
// skips what is already done.

const (
	sealingInterval     = 10 * time.Minute
	pauseBetweenObjects = 200 * time.Millisecond
	// Cap per pass: keeps the pass short and the log readable.
	maxPerPass = 500
)

func startSealing(st *storage.Service) {
	if st == nil || !st.Encrypting() {
		log.Printf("sealing worker disabled (no storage or FILE_ENC_KEYS unset)")
		return
	}
	go func() {
		// First pass delayed: startup already has enough to do.
		time.Sleep(time.Minute)
		for {
			sealPass(context.Background(), st)
			time.Sleep(sealingInterval)
		}
	}()
	log.Printf("sealing worker started (encrypts objects left in the clear)")
}

func sealPass(ctx context.Context, st *storage.Service) {
	start := time.Now()
	var seen, sealed, failures int

	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	for obj := range st.ListObjects(ctx, "") {
		seen++
		if !obj.Sealable() {
			continue
		}
		cifrado, err := st.IsEncrypted(ctx, obj.Key)
		if err != nil {
			failures++
			log.Printf("sealing: %s: %v", obj.Key, err)
			continue
		}
		if cifrado {
			continue
		}
		if err := st.Seal(ctx, obj.Key); err != nil {
			failures++
			log.Printf("sealing: %s: %v", obj.Key, err)
			continue
		}
		sealed++
		if sealed >= maxPerPass {
			break
		}
		time.Sleep(pauseBetweenObjects)
	}

	if sealed > 0 || failures > 0 {
		log.Printf("sealing: %d objects seen, %d sealed, %d failures, in %s",
			seen, sealed, failures, time.Since(start).Round(time.Second))
	}
}
