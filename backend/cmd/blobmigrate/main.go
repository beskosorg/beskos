// blobmigrate copies the mail blocks from the old bucket to the new one, encrypting them in
// transit.
//
// WHY. Stalwart used to write to `beskos`, a bucket with NO jurisdictional restriction, and
// switched to writing through the gateway to `beskos-data`, which has European jurisdiction. The
// old blocks were left behind: an old message opened by an IMAP client looks for the original in
// the new bucket and does not find it. This brings those over.
//
//	blobmigrate            # shows what it would do
//	blobmigrate --apply    # copies
//
// Encrypts while copying, using the same storage.Service the gateway uses — the result is byte
// for byte the same as if the message had come in through the new path. Does not delete the
// source: the old bucket stays as it is until someone decides otherwise, and a migration that
// deletes while copying is a migration that cannot be repeated.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strconv"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/beskosorg/beskos/backend/internal/storage"
)

func main() {
	log.SetFlags(0)
	run := flag.Bool("apply", false, "actually copy; without this it only reports")
	prefix := flag.String("prefixo", "mail/", "prefix to migrate")
	flag.Parse()

	source, err := minio.New(env("ORIGEM_ENDPOINT"), &minio.Options{
		Creds:  credentials.NewStaticV4(env("ORIGEM_ACCESS_KEY"), env("ORIGEM_SECRET_KEY"), ""),
		Secure: true, Region: "auto",
	})
	if err != nil {
		log.Fatalf("source: %v", err)
	}
	sourceBucket := env("ORIGEM_BUCKET")

	ssl, _ := strconv.ParseBool(envOr("DESTINO_SSL", "true"))
	dest, err := storage.New(env("DESTINO_ENDPOINT"), env("DESTINO_ACCESS_KEY"),
		env("DESTINO_SECRET_KEY"), env("DESTINO_BUCKET"), envOr("DESTINO_REGION", "auto"), ssl)
	if err != nil {
		log.Fatalf("destination: %v", err)
	}
	if err := dest.EnableEncryption(nil, env("FILE_ENC_KEYS")); err != nil {
		log.Fatalf("encryption: %v", err)
	}
	if !dest.Encrypting() {
		log.Fatal("without FILE_ENC_KEYS there is no point migrating — the goal is to arrive encrypted")
	}

	ctx := context.Background()
	var seen, copied, alreadyEncrypted, failures int

	for obj := range source.ListObjects(ctx, sourceBucket, minio.ListObjectsOptions{
		Prefix: *prefix, Recursive: true,
	}) {
		if obj.Err != nil {
			log.Fatalf("list: %v", obj.Err)
		}
		if obj.Size == 0 {
			continue
		}
		seen++

		// Already encrypted there? Then leave it alone: repeating the migration must be harmless.
		if cifrado, err := dest.IsEncrypted(ctx, obj.Key); err == nil && cifrado {
			alreadyEncrypted++
			continue
		}

		if !*run {
			log.Printf("  would copy %s (%d bytes)", obj.Key, obj.Size)
			copied++
			continue
		}
		if err := copyObject(ctx, source, sourceBucket, dest, obj.Key, obj.Size); err != nil {
			log.Printf("  ✗ %s: %v", obj.Key, err)
			failures++
			continue
		}
		copied++
		log.Printf("  ✓ %s (%d bytes, encrypted)", obj.Key, obj.Size)
	}

	verb := "to copy"
	if *run {
		verb = "copied"
	}
	log.Printf("%d objects at the source, %d %s, %d were already encrypted, %d failures",
		seen, copied, verb, alreadyEncrypted, failures)
	if failures > 0 {
		os.Exit(1)
	}
}

func copyObject(ctx context.Context, source *minio.Client, bucket string, dest *storage.Service, key string, size int64) error {
	src, err := source.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return err
	}
	defer src.Close()
	return dest.Put(ctx, key, src, size, "")
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
