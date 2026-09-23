// blobgw is an S3 server that encrypts whatever passes through its hands.
//
// WHY. Stalwart writes messages and attachments straight to object storage, with S3 credentials,
// without going through our code — and so they stayed readable in Cloudflare, the weakest link
// compared to what we say publicly. Stalwart's S3 storage has no encryption option at all
// (no SSE-S3, no SSE-C, no client key: the field list is bucket, region, accessKey, secretKey,
// securityToken, profile, timeout, maxRetries, verifyAfterWrite, keyPrefix and allowInvalidCerts).
// And we do not modify Stalwart (docs/engineering-standards.md §5.4).
//
// The way out is to speak its language. Stalwart points its endpoint here instead of at
// Cloudflare; we encrypt with the format that already exists (internal/filecrypt) and forward it.
// It has no idea anything changed.
//
// WHY IT IS SMALL. Having read Stalwart's S3 client (crates/store/src/backend/s3/mod.rs), it uses
// exactly four operations — GET, GET with Range, whole-body PUT, HEAD and DELETE — in path style
// (`with_path_style`). It does NOT use multipart: `put_object` receives the whole body at once.
// That is what lets this service hold no state at all, and therefore scale across replicas.
//
// What is NOT here, on purpose: multipart, pre-signed URLs, listings. LiveKit needs all of that
// for recordings and so it keeps being handled by background sealing. One piece that serves two
// clients with different needs ends up serving both badly.
//
// PREFIX ALLOWLIST. The gateway only accepts keys within the configured prefixes. It is the same
// lesson sealing was expensive to learn: whoever shares storage says explicitly what is theirs,
// instead of assuming everything is.
package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/beskosorg/beskos/backend/internal/storage"
)

type gateway struct {
	st *storage.Service
	// bucket is the name the client uses in the path. It is only used for validation: the real
	// destination bucket comes from the storage configuration.
	bucket string
	// prefixes this gateway accepts serving.
	prefixes []string
	cred     credential
}

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)

	st, err := storage.New(
		env("BLOBGW_S3_ENDPOINT"), env("BLOBGW_S3_ACCESS_KEY"), env("BLOBGW_S3_SECRET_KEY"),
		env("BLOBGW_S3_BUCKET"), envOr("BLOBGW_S3_REGION", "auto"),
		envOr("BLOBGW_S3_SSL", "true") == "true",
	)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}

	// No database: keys served here have no tenant. A message is addressed by its content
	// digest and Stalwart deduplicates it across accounts — it can belong to more than one
	// tenant at the same time, so it cannot be encrypted with just one tenant's key. It gets
	// wrapped by the KEK, like the other system objects.
	if err := st.EnableEncryption(nil, env("FILE_ENC_KEYS")); err != nil {
		log.Fatalf("encryption: %v", err)
	}
	if !st.Encrypting() {
		log.Fatal("blobgw: FILE_ENC_KEYS missing — this service exists to encrypt; starting without a key would be worse than not starting")
	}

	g := &gateway{
		st:       st,
		bucket:   env("BLOBGW_S3_BUCKET"),
		prefixes: strings.Split(envOr("BLOBGW_PREFIXES", "mail/"), ","),
		cred: credential{
			access: env("BLOBGW_ACCESS_KEY"),
			secret: env("BLOBGW_SECRET_KEY"),
			region: envOr("BLOBGW_REGION", "auto"),
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.Handle("/", g)

	port := envOr("PORT", "8090")
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
		// Generous: a large attachment uploading over a slow connection must not be cut off midway.
		ReadTimeout:       10 * time.Minute,
		WriteTimeout:      10 * time.Minute,
		ReadHeaderTimeout: 30 * time.Second,
	}
	log.Printf("blobgw listening on :%s, bucket %s, prefixes %v", port, g.bucket, g.prefixes)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func env(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("blobgw: %s missing", name)
	}
	return v
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// ServeHTTP dispatches. Anything that is not one of the four operations Stalwart uses responds 501
// — which is more honest than pretending the whole of S3 was implemented.
func (g *gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	key, err := g.key(r.URL.Path)
	if err != nil {
		// WITH logging: a rejected request that leaves no trace is indistinguishable from a
		// request that never arrived. It took a lengthy diagnosis to realize the client
		// was sending the old bucket name and we were answering 403 in silence.
		log.Printf("%s %s -> 403 rejected: %v", r.Method, r.URL.Path, err)
		s3Err(w, http.StatusForbidden, "AccessDenied", err.Error())
		return
	}
	if err := g.cred.check(r); err != nil {
		log.Printf("blobgw: signature rejected on %s %s: %v", r.Method, key, err)
		s3Err(w, http.StatusForbidden, "SignatureDoesNotMatch", "invalid signature")
		return
	}

	ctx := r.Context()
	var status int
	switch r.Method {
	case http.MethodPut:
		status = g.put(ctx, w, r, key)
	case http.MethodGet:
		status = g.get(ctx, w, r, key)
	case http.MethodHead:
		status = g.head(ctx, w, key)
	case http.MethodDelete:
		status = g.delete(ctx, w, key)
	default:
		s3Err(w, http.StatusNotImplemented, "NotImplemented", "operation not supported by this gateway")
		status = http.StatusNotImplemented
	}
	log.Printf("%s %s -> %d (%s)", r.Method, key, status, time.Since(start).Round(time.Millisecond))
}

// key extracts the object key from the path and confirms it is within one of our prefixes.
//
// The client uses path style (`/<bucket>/<key>`), which is what Stalwart configures.
func (g *gateway) key(path string) (string, error) {
	p := strings.TrimPrefix(path, "/")
	bucket, key, ok := strings.Cut(p, "/")
	if !ok || bucket != g.bucket {
		return "", errf("unknown bucket %q", bucket)
	}
	for _, pre := range g.prefixes {
		if pre != "" && strings.HasPrefix(key, pre) {
			return key, nil
		}
	}
	return "", errf("key %q outside the served prefixes", key)
}
