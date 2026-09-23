// Package storage is the file service on top of MinIO (S3-compatible, self-hosted, brief §3).
// Tenant isolation via key prefix (tenant_id/…) and checking in the control plane.
// Credentials only in the backend (law §10).
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/beskosorg/beskos/backend/internal/filecrypt"
)

type Service struct {
	client *minio.Client
	bucket string
	// keys is nil when encryption is off: in that case the service writes in the clear, as before.
	// See crypto.go and docs/file-encryption.md. lang:ok
	keys *keyring
}

// New connects to an S3-compatible storage backend and ensures the bucket exists. Works with MinIO
// (endpoint "minio:9000", region "") AND with Cloudflare R2 (endpoint
// "<acct>.r2.cloudflarestorage.com", useSSL=true, region "auto"). On R2 the bucket is pre-created in
// the dashboard — if we can't create it (R2 restricts CreateBucket over S3), we assume it already
// exists instead of blowing up.
func New(endpoint, accessKey, secretKey, bucket, region string, useSSL bool) (*Service, error) {
	c, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
		Region: region, // R2 signs with "auto"; MinIO stays with "" (default)
	})
	if err != nil {
		return nil, err
	}
	s := &Service{client: c, bucket: bucket}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exists, err := c.BucketExists(ctx, bucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		// On R2, buckets are created in the dashboard; the CreateBucket over S3 may be denied — not fatal.
		if err := c.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region}); err != nil {
			return nil, fmt.Errorf("bucket %q does not exist and could not be created (create it in the storage panel): %w", bucket, err)
		}
	}
	return s, nil
}

// Put uploads a blob. key must be prefixed by tenant (e.g. "<tenant>/<hash>").
//
// With encryption on, what reaches storage is encrypted and the provider never sees the content.
// It's ONE write: the object's key travels wrapped inside the object itself, so there's no second
// write to order — see docs/file-encryption.md §2d. lang:ok
func (s *Service) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if s.keys == nil {
		_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
		return err
	}

	dek, h, err := s.keys.wrapForKey(ctx, key)
	if err != nil {
		return fmt.Errorf("object key %q: %w", key, err)
	}

	// The encrypted content comes out of a pipe while the upload consumes it: a 2 GB file never
	// sits in memory nor on disk.
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(filecrypt.EncryptTo(pw, r, size, dek, h))
	}()
	defer pr.Close()

	cifrado := filecrypt.EncryptedSize(size, h.ChunkSize, h.WrappedKeyLen())
	// The content type no longer goes to storage: it's metadata about what's inside, and what's
	// inside has become opaque. Whoever serves the file already knows it from the database.
	_, err = s.client.PutObject(ctx, s.bucket, key, pr, cifrado, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	return err
}

// PutPlain writes WITHOUT encrypting, even with encryption on. It exists to undo a sealing done by
// mistake (see cmd/unseal) — an object that turns out to belong to another process, which reads it
// directly from the bucket and knows nothing of our format. It has no other legitimate use: the
// normal path is Put.
func (s *Service) PutPlain(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

// Get returns the blob's content for streaming to the client, already decrypted when applicable. An
// object predating encryption comes out as is — detection is by the magic in the header, not by
// configuration.
func (s *Service) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	return s.decrypt(ctx, key, obj)
}

// GetSeek returns the object as a ReadSeekCloser (the *minio.Object seeks via Range/StatObject). It
// lets http.ServeContent honor the Range header — a video player with seek (fast-forward/rewind).
//
// With the object encrypted, seeking becomes seeking over the PLAINTEXT content: the reader
// translates the requested position into chunks and only fetches those from storage. A jump to the
// middle of a two-hour recording reads one chunk, not the whole recording.
func (s *Service) GetSeek(ctx context.Context, key string) (*SeekObject, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}

	h, _, perr := readHeader(obj)
	if errors.Is(perr, filecrypt.ErrNotEncrypted) {
		// Object predating encryption: rewind and return it as is.
		if _, serr := obj.Seek(0, io.SeekStart); serr != nil {
			obj.Close()
			return nil, serr
		}
		return &SeekObject{ReadSeeker: obj, c: obj, source: obj}, nil
	}
	if perr != nil {
		obj.Close()
		return nil, perr
	}
	if s.keys == nil {
		obj.Close()
		return nil, errors.New("storage: object is encrypted but encryption is off (FILE_ENC_KEYS missing)")
	}
	dek, err := s.keys.unwrapFromHeader(ctx, key, h)
	if err != nil {
		obj.Close()
		return nil, err
	}
	sr, err := filecrypt.NewSeekReader(obj, h, dek)
	if err != nil {
		obj.Close()
		return nil, err
	}
	return &SeekObject{ReadSeeker: sr, c: obj, source: obj}, nil
}

// SeekObject is the object opened for seekable reading. It also carries the last-modified date,
// which comes for free in storage's response — an S3 server has to return it, and fetching it with
// a second request would cost a network round trip on every read.
type SeekObject struct {
	io.ReadSeeker
	c      io.Closer
	source *minio.Object
}

func (s *SeekObject) Close() error { return s.c.Close() }

// ModTime is when the object last changed, on storage's side.
func (s *SeekObject) ModTime() (time.Time, error) {
	info, err := s.source.Stat()
	if err != nil {
		return time.Time{}, err
	}
	return info.LastModified, nil
}

// Exists reports whether the blob already exists (used for dedup: don't re-upload identical content).
func (s *Service) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" || resp.StatusCode == 404 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Remove deletes a blob (use only when no logical record points to it anymore). There's no key to
// clean up separately: it lived inside the object and disappears with it.
func (s *Service) Remove(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}
