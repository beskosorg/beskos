package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/filecrypt"
)

// Integration test against local MinIO and Postgres (docs/engineering-standards.md §2.7). It is skipped without the
// envs, so it doesn't break `go test ./...` on a machine without containers.
//
//	STORAGE_TEST_ENDPOINT=localhost:9000 STORAGE_TEST_KEY=… STORAGE_TEST_SECRET=… \
//	STORAGE_TEST_BUCKET=beskos STORAGE_TEST_DB=postgres://… \
//	go test ./internal/storage -run TestIntegration -v
//
// What this test proves, and what the filecrypt tests cannot prove: that what ends up IN THE BUCKET
// is unreadable. The unit tests verify the format's round trip — they would pass just the same if
// Service forgot to encrypt before uploading. Here we look at the bytes on the other side.

func testService(t *testing.T) (*Service, *db.Queries, uuid.UUID) {
	t.Helper()
	endpoint := os.Getenv("STORAGE_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("STORAGE_TEST_ENDPOINT not set; skipping the integration test")
	}

	s, err := New(endpoint, os.Getenv("STORAGE_TEST_KEY"), os.Getenv("STORAGE_TEST_SECRET"),
		os.Getenv("STORAGE_TEST_BUCKET"), "", false)
	if err != nil {
		t.Fatalf("connect to storage: %v", err)
	}

	pool, err := pgxpool.New(context.Background(), os.Getenv("STORAGE_TEST_DB"))
	if err != nil {
		t.Fatalf("connect to the database: %v", err)
	}
	t.Cleanup(pool.Close)
	q := db.New(pool)

	// A tenant that really exists: the tenant key has a reference to `tenants`.
	tenant := testTenant(t, pool)

	kek := make([]byte, filecrypt.KeySize)
	if _, err := rand.Read(kek); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableEncryption(q, "1:"+hex.EncodeToString(kek)); err != nil {
		t.Fatalf("enable encryption: %v", err)
	}
	if !s.Encrypting() {
		t.Fatal("encryption should be on")
	}
	return s, q, tenant
}

func testTenant(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO tenants (id, nome) VALUES ($1, $2)`, id, "storage-test")
	if err != nil {
		t.Fatalf("create test tenant: %v", err)
	}
	t.Cleanup(func() {
		// The tenant's key disappears with it (ON DELETE CASCADE) — that's the deletion guarantee
		// migration 00128 exists to give.
		_, _ = pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, id)
	})
	return id
}

func TestIntegrationEncryptsInBucket(t *testing.T) {
	s, _, tenant := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// Bigger than one chunk, to actually exercise more than one chunk.
	plain := bytes.Repeat([]byte("conteudo confidencial do cliente. "), 40_000)
	key := fmt.Sprintf("%s/teste-%s", tenant, uuid.NewString())
	t.Cleanup(func() { _ = s.Remove(context.Background(), key) })

	if err := s.Put(ctx, key, bytes.NewReader(plain), int64(len(plain)), "application/octet-stream"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// 1. What is IN THE BUCKET is not what was sent.
	raw := readRaw(t, s, key)
	if bytes.Contains(raw, []byte("conteudo confidencial")) {
		t.Fatal("the object in the bucket contains the plaintext")
	}
	if string(raw[:4]) != filecrypt.Magic {
		t.Fatalf("the object in the bucket lacks the %q magic, it starts with %q", filecrypt.Magic, raw[:4])
	}

	// 2. The object's key travels inside it — that's what makes the write a single operation.
	h, err := filecrypt.ParseHeader(raw[:filecrypt.FixedHeaderSize])
	if err != nil {
		t.Fatalf("header: %v", err)
	}
	if !h.TenantWrapped {
		t.Fatal("the key should be wrapped by the tenant key")
	}
	if h.WrappedKeyLen() == 0 {
		t.Fatal("the header carries no wrapped key")
	}
	if h.PlainSize != int64(len(plain)) {
		t.Fatalf("plaintext size %d, want %d", h.PlainSize, len(plain))
	}

	// 3. And it comes back whole through Get.
	r, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer r.Close()
	back, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(back, plain) {
		t.Fatalf("came back different: %d bytes vs %d", len(back), len(plain))
	}
}

func TestIntegrationSeekInTheMiddle(t *testing.T) {
	s, _, tenant := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// 5 MiB: the seek lands in the middle of a chunk that is neither the first nor the last, which
	// is the case where a chunk-arithmetic error shows up.
	plain := make([]byte, 5<<20)
	if _, err := rand.Read(plain); err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("%s/seek-%s", tenant, uuid.NewString())
	t.Cleanup(func() { _ = s.Remove(context.Background(), key) })

	if err := s.Put(ctx, key, bytes.NewReader(plain), int64(len(plain)), "video/mp4"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	sr, err := s.GetSeek(ctx, key)
	if err != nil {
		t.Fatalf("GetSeek: %v", err)
	}
	defer sr.Close()

	const skip = 3<<20 + 12345 // in the middle of the fourth chunk
	if _, err := sr.Seek(skip, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	chunk := make([]byte, 4096)
	if _, err := io.ReadFull(sr, chunk); err != nil {
		t.Fatalf("read after the seek: %v", err)
	}
	if !bytes.Equal(chunk, plain[skip:skip+4096]) {
		t.Fatal("the content after the seek is not the original file's")
	}
}

// TestIntegrationSealing proves the path composite recordings and content predating encryption
// take: an object that reached the bucket in the clear is sealed without changing place or name.
func TestIntegrationSealing(t *testing.T) {
	s, _, tenant := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	plain := []byte("recording written by the egress, straight to the bucket, without going through us")
	key := fmt.Sprintf("%s/recordings/selar-%s.mp4", tenant, uuid.NewString())
	t.Cleanup(func() { _ = s.Remove(context.Background(), key) })

	// Writes behind encryption's back, the way LiveKit does with the S3 credentials.
	unencrypted := &Service{client: s.client, bucket: s.bucket}
	if err := unencrypted.Put(ctx, key, bytes.NewReader(plain), int64(len(plain)), "video/mp4"); err != nil {
		t.Fatalf("Put in the clear: %v", err)
	}

	cifrado, err := s.IsEncrypted(ctx, key)
	if err != nil {
		t.Fatalf("IsEncrypted: %v", err)
	}
	if cifrado {
		t.Fatal("it was just written in the clear and claims to be encrypted")
	}

	// Before sealing, the plaintext object HAS to keep being readable — that's what allows
	// migrating without downtime.
	if read := readAll(t, s, ctx, key); !bytes.Equal(read, plain) {
		t.Fatal("an object predating encryption stopped being readable")
	}

	if err := s.Seal(ctx, key); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if raw := readRaw(t, s, key); string(raw[:4]) != filecrypt.Magic {
		t.Fatal("after sealing the object is still in the clear in the bucket")
	}
	if read := readAll(t, s, ctx, key); !bytes.Equal(read, plain) {
		t.Fatal("the content changed with sealing")
	}

	// Sealing again cannot encrypt the encrypted: the background pass repeats every ten minutes
	// and goes through every object, including the ones it has already sealed.
	if err := s.Seal(ctx, key); err != nil {
		t.Fatalf("second sealing: %v", err)
	}
	if read := readAll(t, s, ctx, key); !bytes.Equal(read, plain) {
		t.Fatal("sealing twice broke the object")
	}
}

// TestIntegrationEmptyObject covers the case that showed up on the first day in production: the
// Drive folder markers are zero bytes, the header request answers 416, and the sealing pass used to
// fill the log with errors that weren't errors.
func TestIntegrationEmptyObject(t *testing.T) {
	s, _, tenant := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	key := fmt.Sprintf("%s/pasta-%s/", tenant, uuid.NewString())
	t.Cleanup(func() { _ = s.Remove(context.Background(), key) })

	unencrypted := &Service{client: s.client, bucket: s.bucket}
	if err := unencrypted.Put(ctx, key, bytes.NewReader(nil), 0, "application/x-directory"); err != nil {
		t.Fatalf("create marker: %v", err)
	}

	cifrado, err := s.IsEncrypted(ctx, key)
	if err != nil {
		t.Fatalf("IsEncrypted on an empty object should be silent, gave: %v", err)
	}
	if cifrado {
		t.Fatal("a zero-byte object cannot be encrypted")
	}
	if (Object{Key: key, Size: 0}).Sealable() {
		t.Fatal("a folder marker should not be sealed")
	}
}

// TestIntegrationSealingHidesContentType: after sealing, the provider no longer knows that's a
// video. The type is metadata about the content, and the content has become opaque — telling it
// that it's a `video/mp4` would hand back part of what encryption hid. Whoever serves the file gets
// the type from the name in our database (`mime.TypeByExtension`), never from storage.
func TestIntegrationSealingHidesContentType(t *testing.T) {
	s, _, tenant := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	key := fmt.Sprintf("%s/recordings/tipo-%s.mp4", tenant, uuid.NewString())
	t.Cleanup(func() { _ = s.Remove(context.Background(), key) })

	unencrypted := &Service{client: s.client, bucket: s.bucket}
	if err := unencrypted.Put(ctx, key, bytes.NewReader([]byte("filme")), 5, "video/mp4"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Seal(ctx, key); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if info.ContentType == "video/mp4" {
		t.Fatal("storage still knows the encrypted object is a video")
	}
}

// readRaw fetches the bytes directly from the bucket, without going through Service — it's
// literally what the storage provider has. A keyless Service can't be reused here: it refuses to
// serve an encrypted object, and rightly so (returning encrypted bytes as if they were the file
// would be worse than failing).
func readRaw(t *testing.T, s *Service, key string) []byte {
	t.Helper()
	r, err := s.client.GetObject(context.Background(), s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatalf("raw read: %v", err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("raw read: %v", err)
	}
	return b
}

func readAll(t *testing.T, s *Service, ctx context.Context, key string) []byte {
	t.Helper()
	r, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return b
}
