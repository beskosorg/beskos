// unseal undoes a sealing: decrypts objects and writes them back in plaintext in the same place.
//
// WHAT IT IS FOR. The sealing pass encrypted, on its first run in production, 43 messages that
// Stalwart keeps in the same bucket and reads directly, without going through our code. Encrypting
// another process's file is not added security, it is a malfunction — and it gets undone here.
//
// It stays in the repository instead of being a throwaway script: if it happens again with
// another neighbor of the bucket, we want the rollback ready and not a program written in a
// hurry during the incident.
//
//	FILE_ENC_KEYS=… MINIO_… go run ./cmd/unseal <prefix>          # shows what it would do
//	FILE_ENC_KEYS=… MINIO_… go run ./cmd/unseal --apply <prefix>  # does it
//
// Does not run without a prefix: the command that undoes encryption on everything must not exist
// by accident.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"

	"github.com/beskosorg/beskos/backend/internal/storage"
)

func main() {
	apply := flag.Bool("apply", false, "actually write; without this it only reports")
	flag.Parse()

	prefix := flag.Arg(0)
	if prefix == "" {
		log.Fatal("usage: unseal [--apply] <prefix>")
	}

	ssl, _ := strconv.ParseBool(os.Getenv("MINIO_SSL"))
	st, err := storage.New(os.Getenv("MINIO_ENDPOINT"), os.Getenv("MINIO_ACCESS_KEY"),
		os.Getenv("MINIO_SECRET_KEY"), os.Getenv("MINIO_BUCKET"), os.Getenv("MINIO_REGION"), ssl)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	// No database: only objects wrapped by the KEK get undone, which is the case for the
	// bucket's neighbors. A tenant's object would need the tenant's key, and that is not what
	// this command does.
	if err := st.EnableEncryption(nil, os.Getenv("FILE_ENC_KEYS")); err != nil {
		log.Fatalf("encryption: %v", err)
	}

	ctx := context.Background()
	var seen, done int
	for obj := range st.ListObjects(ctx, prefix) {
		seen++
		cifrado, err := st.IsEncrypted(ctx, obj.Key)
		if err != nil {
			log.Printf("%s: %v", obj.Key, err)
			continue
		}
		if !cifrado {
			continue
		}
		if !*apply {
			fmt.Printf("  would decrypt %s (%d bytes)\n", obj.Key, obj.Size)
			done++
			continue
		}
		if err := undo(ctx, st, obj.Key); err != nil {
			log.Printf("%s: %v", obj.Key, err)
			continue
		}
		done++
		fmt.Printf("  %s restored to plaintext\n", obj.Key)
	}

	verb := "to decrypt"
	if *apply {
		verb = "restored to plaintext"
	}
	fmt.Printf("%d objects seen in %q, %d %s\n", seen, prefix, done, verb)
}

// undo reads the decrypted object and rewrites it in plaintext under the same key. One write,
// like sealing: the PUT either happens in full or does not happen at all.
func undo(ctx context.Context, st *storage.Service, key string) error {
	r, err := st.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	defer r.Close()

	tmp, err := os.CreateTemp("", "unseal-*")
	if err != nil {
		return err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	n, err := io.Copy(tmp, r)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return st.PutPlain(ctx, key, tmp, n, "")
}
