package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/minio/minio-go/v7"

	"github.com/beskosorg/beskos/backend/internal/filecrypt"
)

// Sealing: take an object that is in the clear and rewrite it encrypted.
//
// It's the SAME operation for the two missing cases, and so it's a single piece:
//
//   - objects that were already in the bucket before encryption existed;
//   - composite recordings, which LiveKit Egress writes directly to storage with the S3
//     credentials, without going through our backend (docs/file-encryption.md §3a). lang:ok
//
// ONE WRITE. The SAME key gets rewritten: the storage PUT is atomic, whoever is reading at that
// instant gets the old or the new version, and both read fine. There's no deleting afterward, no
// key to update elsewhere, no flag in the database to keep in sync.
//
// IDEMPOTENT BY CONSTRUCTION. What decides whether an object is already sealed is the object
// itself — the magic in the header — and not a column that could fall out of sync. Sealing twice
// would encrypt the encrypted, and a database flag that failed to write would lead to exactly that.

// IsEncrypted reports whether the object is already encrypted by us. Reads only the header, with a
// Range request.
//
// An object shorter than the header cannot be encrypted, and storage answers that request with 416
// (impossible range). That's not a failure — it's the answer. It showed up on the first day in
// production, with the Drive folder markers, which are zero bytes.
func (s *Service) IsEncrypted(ctx context.Context, key string) (bool, error) {
	opts := minio.GetObjectOptions{}
	if err := opts.SetRange(0, int64(filecrypt.FixedHeaderSize)-1); err != nil {
		return false, err
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, opts)
	if err != nil {
		if tooShort(err) {
			return false, nil
		}
		return false, err
	}
	defer obj.Close()

	buf := make([]byte, filecrypt.FixedHeaderSize)
	n, err := io.ReadFull(obj, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		if tooShort(err) {
			return false, nil
		}
		return false, err
	}
	_, perr := filecrypt.ParseHeader(buf[:n])
	if errors.Is(perr, filecrypt.ErrNotEncrypted) {
		return false, nil
	}
	return perr == nil, perr
}

// tooShort recognizes storage's 416: a range was requested that the object doesn't have.
func tooShort(err error) bool {
	var resp minio.ErrorResponse
	if errors.As(err, &resp) {
		return resp.StatusCode == http.StatusRequestedRangeNotSatisfiable ||
			resp.Code == "InvalidRange"
	}
	return false
}

// Seal encrypts an object that is in the clear, rewriting it in the same place. Does nothing if it
// is already encrypted or if encryption is off.
//
// The content passes through a temporary file instead of going straight from read to write: you
// can't read and write the same key at the same time, and an object can have gigabytes that don't
// fit in memory.
func (s *Service) Seal(ctx context.Context, key string) error {
	if s.keys == nil {
		return errors.New("storage: sealing requested with encryption off")
	}
	cifrado, err := s.IsEncrypted(ctx, key)
	if err != nil {
		return err
	}
	if cifrado {
		return nil
	}

	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return err
	}
	defer obj.Close()

	tmp, err := os.CreateTemp("", "seal-*")
	if err != nil {
		return err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	size, err := io.Copy(tmp, obj)
	if err != nil {
		return fmt.Errorf("seal %q: download: %w", key, err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}

	// Put encrypts and rewrites the same key. One write, atomic.
	//
	// The content type is NOT preserved, on purpose: Put stores everything as octet-stream when it
	// encrypts. Telling the provider that opaque object is a `video/mp4` would be handing it back
	// part of what encryption just hid — and whoever serves the file gets the type from the name
	// stored in our database, never from storage.
	if err := s.Put(ctx, key, tmp, size, ""); err != nil {
		return fmt.Errorf("seal %q: re-upload: %w", key, err)
	}
	return nil
}

// Object is what the listing hands back. The size travels with it because the decision to seal
// depends on it: a zero-byte object has no content to protect.
type Object struct {
	Key  string
	Size int64
}

// Sealable reports whether this object is OURS and worth sealing.
//
// WHITELIST, and the reason is expensive: the bucket isn't only ours. Stalwart stores messages
// under `mail/…`, written by it and read by it, without going through this code. The first version
// of this function sealed everything it found and encrypted 43 production mail messages — which
// Stalwart immediately lost the ability to read. Encrypting another process's file isn't a security
// improvement, it's an outage.
//
// So the question isn't "does this look safe to encrypt?" but "did we write this?". There are only
// two families of keys that storage.Service writes: `<tenant>/…` and the old recordings under
// `recordings/…`. Everything else belongs to someone else until proven otherwise — and a shared
// bucket only gains neighbors over time.
//
// Also left out is what isn't content: the Drive folder markers are zero bytes, and encrypting them
// would trade zero bytes for ninety without hiding anything.
func (o Object) Sealable() bool {
	if o.Size == 0 || strings.HasSuffix(o.Key, "/") {
		return false
	}
	if strings.HasPrefix(o.Key, "recordings/") {
		return true
	}
	_, hasTenant := tenantFromKey(o.Key)
	return hasTenant
}

// ListObjects walks the bucket. An empty `prefix` walks everything.
func (s *Service) ListObjects(ctx context.Context, prefix string) <-chan Object {
	out := make(chan Object)
	go func() {
		defer close(out)
		for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
			Prefix: prefix, Recursive: true,
		}) {
			if obj.Err != nil {
				return
			}
			select {
			case out <- Object{Key: obj.Key, Size: obj.Size}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}
