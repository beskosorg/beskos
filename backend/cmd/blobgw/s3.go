package main

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/beskosorg/beskos/backend/internal/storage"
)

// The four operations. Each one returns the status it wrote, only for logging.

// put encrypts and stores. Stalwart sends the whole body with Content-Length — never multipart —
// which is what allows encrypting it in stream, stateless and without disk.
func (g *gateway) put(ctx context.Context, w http.ResponseWriter, r *http.Request, key string) int {
	size := r.ContentLength
	if size < 0 {
		s3Err(w, http.StatusLengthRequired, "MissingContentLength", "this gateway requires Content-Length")
		return http.StatusLengthRequired
	}
	defer r.Body.Close()

	// The signature covers the DECLARED digest of the body, not the body. The real body is
	// checked in stream — without holding it in memory, which is what lets a large attachment
	// pass through here.
	body := newChecker(r.Body, r.Header.Get("X-Amz-Content-Sha256"))

	// The content type is not preserved: the object becomes opaque, and telling the provider
	// that it is a `message/rfc822` would give back part of what encryption just hid.
	if err := g.st.Put(ctx, key, body, size, ""); err != nil {
		log.Printf("blobgw: put %s: %v", key, err)
		s3Err(w, http.StatusInternalServerError, "InternalError", "failed to store")
		return http.StatusInternalServerError
	}

	// Only known at the end. A body that does not match what was signed does not stay in
	// storage: what was just written is deleted before responding.
	if err := body.verify(); err != nil {
		log.Printf("blobgw: put %s rejected: %v", key, err)
		if err := g.st.Remove(context.WithoutCancel(ctx), key); err != nil {
			log.Printf("blobgw: and could not delete %s: %v", key, err)
		}
		s3Err(w, http.StatusBadRequest, "XAmzContentSHA256Mismatch", "body does not match the signed digest")
		return http.StatusBadRequest
	}
	// The ETag must exist, but it must NOT be the encrypted object's nor pretend to be the md5
	// of what the client sent. Stalwart does not verify it (it confirms the write with a HEAD,
	// see verify_after_write); the key is returned instead, which is stable and does not lie
	// about being an md5.
	w.Header().Set("ETag", `"`+keyETag(key)+`"`)
	w.WriteHeader(http.StatusOK)
	return http.StatusOK
}

// get returns the decrypted content. With Range, it decrypts only the requested blocks — that is
// exactly why the format is chunked.
func (g *gateway) get(ctx context.Context, w http.ResponseWriter, r *http.Request, key string) int {
	interval := r.Header.Get("Range")
	if interval == "" {
		obj, err := g.st.GetSeek(ctx, key)
		if err != nil {
			return g.readErr(w, key, err)
		}
		defer obj.Close()
		total, err := obj.Seek(0, io.SeekEnd)
		if err == nil {
			_, err = obj.Seek(0, io.SeekStart)
		}
		if err != nil {
			s3Err(w, http.StatusInternalServerError, "InternalError", "failed to measure the object")
			return http.StatusInternalServerError
		}
		objectHeaders(w, obj, key, total)
		w.WriteHeader(http.StatusOK)
		if _, err := io.Copy(w, obj); err != nil {
			log.Printf("blobgw: get %s interrupted: %v", key, err)
		}
		return http.StatusOK
	}

	sr, err := g.st.GetSeek(ctx, key)
	if err != nil {
		return g.readErr(w, key, err)
	}
	defer sr.Close()

	total, err := sr.Seek(0, io.SeekEnd)
	if err != nil {
		s3Err(w, http.StatusInternalServerError, "InternalError", "failed to measure the object")
		return http.StatusInternalServerError
	}
	start, end, err := readRange(interval, total)
	if err != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", total))
		s3Err(w, http.StatusRequestedRangeNotSatisfiable, "InvalidRange", err.Error())
		return http.StatusRequestedRangeNotSatisfiable
	}
	if _, err := sr.Seek(start, io.SeekStart); err != nil {
		s3Err(w, http.StatusInternalServerError, "InternalError", "failed to seek")
		return http.StatusInternalServerError
	}

	objectHeaders(w, sr, key, total)
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)
	if _, err := io.CopyN(w, sr, end-start+1); err != nil {
		log.Printf("blobgw: get %s [%d-%d] interrupted: %v", key, start, end, err)
	}
	return http.StatusPartialContent
}

// head confirms the object exists and reports the PLAINTEXT size — which is what the client
// expects, not the encrypted one. Stalwart uses this right after every PUT (verify_after_write)
// to defend against storage backends that confirm writes before making them visible.
func (g *gateway) head(ctx context.Context, w http.ResponseWriter, key string) int {
	sr, err := g.st.GetSeek(ctx, key)
	if err != nil {
		return g.readErr(w, key, err)
	}
	defer sr.Close()

	total, err := sr.Seek(0, io.SeekEnd)
	if err != nil {
		s3Err(w, http.StatusInternalServerError, "InternalError", "failed to measure the object")
		return http.StatusInternalServerError
	}
	objectHeaders(w, sr, key, total)
	w.WriteHeader(http.StatusOK)
	return http.StatusOK
}

// objectHeaders writes what an S3 client expects from a GET or HEAD. Last-Modified is not
// decoration: a legitimate client rejects the response without it (minio-go caught this during
// testing), and it comes for free from the response already made to storage.
func objectHeaders(w http.ResponseWriter, obj *storage.SeekObject, key string, total int64) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("ETag", `"`+keyETag(key)+`"`)
	when, err := obj.ModTime()
	if err != nil {
		when = time.Now().UTC()
	}
	w.Header().Set("Last-Modified", when.UTC().Format(http.TimeFormat))
}

func (g *gateway) delete(ctx context.Context, w http.ResponseWriter, key string) int {
	if err := g.st.Remove(ctx, key); err != nil {
		log.Printf("blobgw: delete %s: %v", key, err)
		s3Err(w, http.StatusInternalServerError, "InternalError", "failed to delete")
		return http.StatusInternalServerError
	}
	w.WriteHeader(http.StatusNoContent)
	return http.StatusNoContent
}

// readErr translates the storage error. 404 must pass through as 404: Stalwart distinguishes
// "does not exist" (a legitimate response) from "it broke" (which it retries with backoff).
func (g *gateway) readErr(w http.ResponseWriter, key string, err error) int {
	var resp minio.ErrorResponse
	if errors.As(err, &resp) && (resp.StatusCode == http.StatusNotFound || resp.Code == "NoSuchKey") {
		s3Err(w, http.StatusNotFound, "NoSuchKey", "does not exist")
		return http.StatusNotFound
	}
	log.Printf("blobgw: read %s: %v", key, err)
	s3Err(w, http.StatusInternalServerError, "InternalError", "failed to read")
	return http.StatusInternalServerError
}

// readRange parses `bytes=start-end`, with the open forms on either side. Returns inclusive
// bounds, as HTTP requires.
func readRange(header string, total int64) (int64, int64, error) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, errf("range %q is not in bytes", header)
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if strings.Contains(spec, ",") {
		return 0, 0, errf("multiple ranges are not supported")
	}
	from, until, ok := strings.Cut(spec, "-")
	if !ok {
		return 0, 0, errf("malformed range %q", spec)
	}

	switch {
	case from == "" && until == "":
		return 0, 0, errf("empty range")
	case from == "": // suffix: the last N bytes
		n, err := strconv.ParseInt(until, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, errf("invalid suffix %q", until)
		}
		if n > total {
			n = total
		}
		return total - n, total - 1, nil
	}

	start, err := strconv.ParseInt(from, 10, 64)
	if err != nil || start < 0 {
		return 0, 0, errf("invalid start %q", from)
	}
	if start >= total {
		return 0, 0, errf("start %d past the end (%d)", start, total)
	}
	end := total - 1
	if until != "" {
		if end, err = strconv.ParseInt(until, 10, 64); err != nil {
			return 0, 0, errf("invalid end %q", until)
		}
		if end > total-1 {
			end = total - 1
		}
	}
	if end < start {
		return 0, 0, errf("end %d before start %d", end, start)
	}
	return start, end, nil
}

// keyETag gives a stable, honest ETag. Stalwart's keys are already the content digest, so this
// identifies the version without pretending to be the md5 of bytes nobody has.
func keyETag(key string) string {
	if i := strings.LastIndexByte(key, '/'); i >= 0 {
		return key[i+1:]
	}
	return key
}

type xmlErr struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}

func s3Err(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_ = xml.NewEncoder(w).Encode(xmlErr{Code: code, Message: message})
}

func errf(format string, args ...any) error { return fmt.Errorf(format, args...) }
