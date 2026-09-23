// stalwart-blobstore points Stalwart's block storage at our encryption gateway.
//
// WHY A COMMAND and not the admin interface: Stalwart's admin listens on 127.0.0.1 on the VPS, on
// purpose — mail admin credentials pass through there. Exposing it to the internet to make a
// two-field change would trade a claim on the security page for five minutes of convenience.
// CapRover's apps already reach Stalwart from inside; this command runs there and opens nothing.
//
// READ BEFORE WRITING. The first thing it does is print the entire current configuration to the
// log. If the change goes wrong, the rollback is in the log and not in someone's memory.
//
//	--view    shows the current configuration and touches nothing (default)
//	--apply writes the change
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"time"

	"github.com/beskosorg/beskos/backend/internal/stalwart"
)

func main() {
	log.SetFlags(0)
	apply := flag.Bool("apply", false, "write the change; without this it only reports")
	flag.Parse()

	log.Printf("=== run %s ===", time.Now().UTC().Format(time.RFC3339))

	c := stalwart.New(env("STALWART_URL"), env("STALWART_ADMIN_ID"), env("STALWART_ADMIN_SECRET"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	current, err := c.GetBlobStore(ctx)
	if err != nil {
		log.Fatalf("read the current configuration: %v", err)
	}
	log.Printf("singleton id: %s", current.ID)
	log.Printf("CURRENT CONFIGURATION (keep this — it is the rollback):\n%s", indent(current.Config))

	target := stalwart.S3BlobStore{
		Bucket:    env("BLOBGW_S3_BUCKET"),
		Endpoint:  env("BLOBGW_ENDPOINT"),
		Region:    envOr("BLOBGW_REGION", "auto"),
		AccessKey: env("BLOBGW_ACCESS_KEY"),
		SecretKey: env("BLOBGW_SECRET_KEY"),
		KeyPrefix: envOr("BLOBGW_KEY_PREFIX", "mail/"),
	}
	log.Printf("TARGET: %s, bucket %s, prefix %s", target.Endpoint, target.Bucket, target.KeyPrefix)

	if !*apply {
		log.Printf("nothing was changed (--apply was not given)")
		wait()
		return
	}

	if err := c.SetBlobStoreS3(ctx, current.ID, target); err != nil {
		log.Fatalf("apply: %v", err)
	}
	log.Printf("applied")

	// Without this, Stalwart keeps using the old storage until it restarts — and new mail would
	// go to the old place without any error at all.
	if err := c.ReloadSettings(ctx); err != nil {
		log.Fatalf("reload the configuration: %v", err)
	}
	log.Printf("configuration reloaded")

	// Read it back. "I wrote it" is not the same as "it is there": an accepted `set` and a
	// confirmed state are different things, and this one is easy to check.
	after, err := c.GetBlobStore(ctx)
	if err != nil {
		log.Fatalf("re-read: %v", err)
	}
	log.Printf("CONFIGURATION NOW:\n%s", indent(after.Config))
	wait()
}

// wait keeps the container alive after the work is done. Without this CapRover sees the process
// exit, marks it as crashed and restarts it in a loop — and the log, which is this command's
// result, would get lost among the repetitions.
func wait() {
	log.Printf("done; the app can be deleted")
	// Sleeping in a loop, not `select {}`: with no cases, Go detects a deadlock and aborts the
	// process — which makes CapRover restart in a loop and mixes the log of several runs
	// together, which is exactly what this command must not do, because the log IS the result.
	for {
		time.Sleep(time.Hour)
	}
}

func indent(b []byte) string {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(b)
	}
	return string(pretty)
}

func env(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("%s missing", name)
	}
	return v
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
