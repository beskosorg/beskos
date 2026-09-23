package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/signer"

	"github.com/beskosorg/beskos/backend/internal/filecrypt"
	"github.com/beskosorg/beskos/backend/internal/storage"
)

// The tests talk to the gateway using a REAL S3 client (minio-go), which signs with SigV4 the
// way Stalwart signs. Writing a test client that signed "our own way" would only prove that our
// verifier agrees with itself.
//
// They need a local MinIO, like the internal/storage ones:
//
//	STORAGE_TEST_ENDPOINT=minio:9000 STORAGE_TEST_KEY=… STORAGE_TEST_SECRET=… \
//	STORAGE_TEST_BUCKET=mailin go test ./cmd/blobgw -v

const (
	testAccess = "stalwart-teste"
	testSecret = "segredo-do-stalwart-para-o-teste"
)

func build(t *testing.T) (*gateway, *httptest.Server, *minio.Client) {
	t.Helper()
	endpoint := os.Getenv("STORAGE_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("STORAGE_TEST_ENDPOINT not set; skipping")
	}
	bucket := os.Getenv("STORAGE_TEST_BUCKET")

	st, err := storage.New(endpoint, os.Getenv("STORAGE_TEST_KEY"), os.Getenv("STORAGE_TEST_SECRET"),
		bucket, "", false)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	kek := make([]byte, filecrypt.KeySize)
	if _, err := rand.Read(kek); err != nil {
		t.Fatal(err)
	}
	if err := st.EnableEncryption(nil, "1:"+hex.EncodeToString(kek)); err != nil {
		t.Fatal(err)
	}

	g := &gateway{
		st: st, bucket: bucket, prefixes: []string{"mail/"},
		cred: credential{access: testAccess, secret: testSecret, region: "auto"},
	}
	// TLS server on purpose: over plain HTTP, minio-go signs the body IN STREAM
	// (`aws-chunked`), which Stalwart does not use and this gateway rejects. Over TLS it signs
	// like rust-s3 — which is the path worth exercising.
	srv := httptest.NewTLSServer(g)
	t.Cleanup(srv.Close)

	u, _ := url.Parse(srv.URL)
	cli, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(testAccess, testSecret, ""),
		Secure:       true,
		Transport:    srv.Client().Transport,
		Region:       "auto",
		BucketLookup: minio.BucketLookupPath, // like Stalwart: with_path_style()
	})
	if err != nil {
		t.Fatal(err)
	}
	return g, srv, cli
}

// TestIntegrationGatewayLikeRustS3 exercises exactly what Stalwart sends: the REAL digest of the
// body in X-Amz-Content-Sha256, instead of the UNSIGNED-PAYLOAD that minio-go uses. The signature
// is done by minio's signer — independent of our verifier, otherwise the test would only prove
// that our own code agrees with itself.
func TestIntegrationGatewayLikeRustS3(t *testing.T) {
	g, srv, _ := build(t)
	plain := []byte("mensagem crua, assinada com o resumo do corpo")
	key := "mail/" + randomHex(t)
	t.Cleanup(func() { _ = g.st.Remove(context.Background(), key) })

	resp := byRustS3(t, srv, key, plain, hexSHA256(plain))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT signed the way Stalwart does returned %d: %s", resp.StatusCode, body)
	}
	if raw := readRaw(t, g, key); string(raw[:4]) != filecrypt.Magic {
		t.Fatal("the object was not encrypted")
	}
}

// TestIntegrationGatewaySwappedBody: the signature covers the DECLARED digest, not the body. If
// the two do not match, the object must not stay in storage.
func TestIntegrationGatewaySwappedBody(t *testing.T) {
	g, srv, _ := build(t)
	key := "mail/" + randomHex(t)
	t.Cleanup(func() { _ = g.st.Remove(context.Background(), key) })

	// Signs the digest of one content and sends another.
	resp := byRustS3(t, srv, key, []byte("o corpo que vai"), hexSHA256([]byte("o corpo assinado")))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a body different from the signed one should give 400, gave %d", resp.StatusCode)
	}
	if _, err := g.st.Get(context.Background(), key); err == nil {
		t.Fatal("the rejected object stayed in storage")
	}
}

// byRustS3 builds and signs a PUT the way Stalwart's client does.
func byRustS3(t *testing.T, srv *httptest.Server, key string, body []byte, summary string) *http.Response {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	req, err := http.NewRequest(http.MethodPut,
		fmt.Sprintf("%s/%s/%s", srv.URL, os.Getenv("STORAGE_TEST_BUCKET"), key), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = u.Host
	req.ContentLength = int64(len(body))
	req.Header.Set("X-Amz-Content-Sha256", summary)
	signed := signer.SignV4(*req, testAccess, testSecret, "", "auto")
	signed.Body = io.NopCloser(bytes.NewReader(body))

	resp, err := srv.Client().Do(signed)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestIntegrationGatewayRoundTrip(t *testing.T) {
	g, _, cli := build(t)
	ctx := context.Background()

	// Larger than one block: forces encryption to actually split into chunks.
	plain := bytes.Repeat([]byte("mensagem de correio com anexo. "), 50_000)
	key := "mail/" + randomHex(t)
	t.Cleanup(func() { _ = g.st.Remove(context.Background(), key) })

	_, err := cli.PutObject(ctx, g.bucket, key, bytes.NewReader(plain), int64(len(plain)),
		minio.PutObjectOptions{DisableMultipart: true})
	if err != nil {
		t.Fatalf("PutObject: %v", err)
	}

	// 1. What ended up in the bucket is not what was sent.
	raw := readRaw(t, g, key)
	if bytes.Contains(raw, []byte("mensagem de correio")) {
		t.Fatal("the object in the bucket holds the mail text in the clear")
	}
	if string(raw[:4]) != filecrypt.Magic {
		t.Fatalf("the object in the bucket is not encrypted, it starts with %q", raw[:4])
	}

	// 2. And the client gets it back whole, none the wiser about any of this.
	obj, err := cli.GetObject(ctx, g.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Close()
	back, err := io.ReadAll(obj)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(back, plain) {
		t.Fatalf("came back different: %d bytes vs %d", len(back), len(plain))
	}
}

// TestIntegrationGatewayRange is the reason the format is chunked: Stalwart requests pieces of a
// message to serve one MIME part, and must not have to decrypt the whole attachment for that.
func TestIntegrationGatewayRange(t *testing.T) {
	g, _, cli := build(t)
	ctx := context.Background()

	plain := make([]byte, 3<<20)
	if _, err := rand.Read(plain); err != nil {
		t.Fatal(err)
	}
	key := "mail/" + randomHex(t)
	t.Cleanup(func() { _ = g.st.Remove(context.Background(), key) })

	if _, err := cli.PutObject(ctx, g.bucket, key, bytes.NewReader(plain), int64(len(plain)),
		minio.PutObjectOptions{DisableMultipart: true}); err != nil {
		t.Fatal(err)
	}

	for _, caso := range []struct{ from, until int64 }{
		{0, 99},                     // start
		{1<<20 + 500, 1<<20 + 1499}, // straddling two blocks
		{int64(len(plain)) - 10, int64(len(plain)) - 1}, // the end
	} {
		opts := minio.GetObjectOptions{}
		if err := opts.SetRange(caso.from, caso.until); err != nil {
			t.Fatal(err)
		}
		obj, err := cli.GetObject(ctx, g.bucket, key, opts)
		if err != nil {
			t.Fatal(err)
		}
		read, err := io.ReadAll(obj)
		obj.Close()
		if err != nil {
			t.Fatalf("[%d-%d] read: %v", caso.from, caso.until, err)
		}
		if !bytes.Equal(read, plain[caso.from:caso.until+1]) {
			t.Fatalf("[%d-%d] returned %d wrong bytes", caso.from, caso.until, len(read))
		}
	}
}

// TestIntegrationGatewayHeadAfterPut covers Stalwart's verify_after_write: it does a HEAD right
// after every write to defend against storage backends that confirm before making it visible.
func TestIntegrationGatewayHeadAfterPut(t *testing.T) {
	g, _, cli := build(t)
	ctx := context.Background()

	plain := []byte("uma mensagem curta")
	key := "mail/" + randomHex(t)
	t.Cleanup(func() { _ = g.st.Remove(context.Background(), key) })

	if _, err := cli.PutObject(ctx, g.bucket, key, bytes.NewReader(plain), int64(len(plain)),
		minio.PutObjectOptions{DisableMultipart: true}); err != nil {
		t.Fatal(err)
	}

	info, err := cli.StatObject(ctx, g.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		t.Fatalf("StatObject: %v", err)
	}
	// The size must be that of the plaintext. Returning the ciphertext's would make the client
	// request bytes that do not exist.
	if info.Size != int64(len(plain)) {
		t.Fatalf("HEAD says %d bytes, plaintext has %d", info.Size, len(plain))
	}

	if err := cli.RemoveObject(ctx, g.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		t.Fatalf("RemoveObject: %v", err)
	}
	if _, err := cli.StatObject(ctx, g.bucket, key, minio.StatObjectOptions{}); err == nil {
		t.Fatal("the object is still there after DELETE")
	}
}

// TestIntegrationGatewayNotFound: 404 must pass through as 404. Stalwart distinguishes "does not
// exist" (a legitimate response, returns None) from "it broke" (which it retries with backoff) —
// swapping the two would make an empty mailbox look like a failure, or worse.
func TestIntegrationGatewayNotFound(t *testing.T) {
	g, _, cli := build(t)
	_, err := cli.GetObject(context.Background(), g.bucket, "mail/"+randomHex(t), minio.GetObjectOptions{})
	if err == nil {
		// minio-go only fails on the first read
		obj, _ := cli.GetObject(context.Background(), g.bucket, "mail/nao-existe", minio.GetObjectOptions{})
		if _, err = io.ReadAll(obj); err == nil {
			t.Fatal("reading an object that does not exist should fail")
		}
	}
	if resp := minio.ToErrorResponse(err); resp.StatusCode != http.StatusNotFound && resp.Code != "NoSuchKey" {
		t.Fatalf("want 404/NoSuchKey, got %+v", resp)
	}
}

// TestGatewayRejectsUnsignedRequest and the ones after it need no storage: they stop before that.
func TestGatewayRejectsUnsignedRequest(t *testing.T) {
	g := &gateway{bucket: "b", prefixes: []string{"mail/"},
		cred: credential{access: testAccess, secret: testSecret, region: "auto"}}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/b/mail/x", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("without a signature it should give 403, gave %d", w.Code)
	}
}

func TestGatewayRejectsOutsidePrefix(t *testing.T) {
	g := &gateway{bucket: "b", prefixes: []string{"mail/"},
		cred: credential{access: testAccess, secret: testSecret, region: "auto"}}
	for _, path := range []string{"/b/recordings/x.mp4", "/b/outro/x", "/outro-balde/mail/x"} {
		w := httptest.NewRecorder()
		g.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s should give 403, gave %d", path, w.Code)
		}
	}
}

func TestGatewayRejectsUnsupportedOperation(t *testing.T) {
	g, _, cli := build(t)
	// ListObjects is one of the operations this gateway does NOT implement, on purpose. The
	// first (and, since the gateway rejects the request outright, only) item the channel yields
	// already carries the rejection, so there's nothing to gain from consuming the rest of it.
	objs := cli.ListObjects(context.Background(), g.bucket, minio.ListObjectsOptions{Prefix: "mail/"})
	obj, ok := <-objs
	if !ok {
		t.Fatal("listing should be rejected, but the channel closed with no items")
	}
	if obj.Err == nil {
		t.Fatal("listing should be rejected")
	}
}

func TestReadRange(t *testing.T) {
	cases := []struct {
		header      string
		total       int64
		from, until int64
		failure     bool
	}{
		{"bytes=0-99", 1000, 0, 99, false},
		{"bytes=100-", 1000, 100, 999, false},
		{"bytes=-100", 1000, 900, 999, false},
		{"bytes=-5000", 1000, 0, 999, false},   // suffix larger than the object
		{"bytes=0-5000", 1000, 0, 999, false},  // end past the object: gets clamped
		{"bytes=1000-", 1000, 0, 0, true},      // start past the end
		{"bytes=50-10", 1000, 0, 0, true},      // reversed
		{"bytes=0-10,20-30", 1000, 0, 0, true}, // multiple ranges
		{"itens=0-10", 1000, 0, 0, true},
	}
	for _, c := range cases {
		from, until, err := readRange(c.header, c.total)
		if c.failure {
			if err == nil {
				t.Errorf("%q should fail", c.header)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.header, err)
			continue
		}
		if from != c.from || until != c.until {
			t.Errorf("%q gave [%d-%d], want [%d-%d]", c.header, from, until, c.from, c.until)
		}
	}
}

func TestCheckerCatchesSwappedBody(t *testing.T) {
	c := newChecker(strings.NewReader("o corpo verdadeiro"), hexSHA256([]byte("outro corpo")))
	if _, err := io.ReadAll(c); err != nil {
		t.Fatal(err)
	}
	if err := c.verify(); err == nil {
		t.Fatal("a body different from the signed one had to be rejected")
	}

	ok := newChecker(strings.NewReader("o corpo verdadeiro"), hexSHA256([]byte("o corpo verdadeiro")))
	if _, err := io.ReadAll(ok); err != nil {
		t.Fatal(err)
	}
	if err := ok.verify(); err != nil {
		t.Fatalf("the correct body was rejected: %v", err)
	}
}

// readRaw fetches the bytes DIRECTLY from storage, without going through the gateway — which is
// what Cloudflare has. Does not work with a Service without encryption: that one refuses to serve
// an encrypted object, and rightly so.
func readRaw(t *testing.T, g *gateway, key string) []byte {
	t.Helper()
	cli, err := minio.New(os.Getenv("STORAGE_TEST_ENDPOINT"), &minio.Options{
		Creds:  credentials.NewStaticV4(os.Getenv("STORAGE_TEST_KEY"), os.Getenv("STORAGE_TEST_SECRET"), ""),
		Secure: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := cli.GetObject(context.Background(), g.bucket, key, minio.GetObjectOptions{})
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

func randomHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("teste-%s", hex.EncodeToString(b))
}
