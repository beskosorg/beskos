package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Sync delta upload: for a large file that changed only a little, sending everything again is
// wasteful.
//
// Deliberate design: storage remains **one whole blob per file** (content-addressed, with dedup
// and versions). We didn't switch to storing blocks — that would mean touching download,
// versions, OnlyOffice, zip, thumbnails... Instead the server:
//  1. exposes the block hashes of the CURRENT content (`GET .../blocks`);
//  2. accepts only the changed blocks (`PUT .../delta`) and **reconstructs** the full file,
//     copying the rest from the old blob.
// Result: network usage drops without touching the rest of the system.

const (
	defaultBlockSize = 4 << 20  // 4 MiB
	minBlockSize     = 64 << 10 // 64 KiB
	maxBlockSize     = 16 << 20 // 16 MiB
)

// blockSizeParam reads and validates ?block_size=, falling back to the default.
func blockSizeParam(r *http.Request) (int64, bool) {
	v := r.URL.Query().Get("block_size")
	if v == "" {
		return defaultBlockSize, true
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < minBlockSize || n > maxBlockSize {
		return 0, false
	}
	return n, true
}

// handleSyncFileBlocks: GET /api/sync/files/{id}/blocks?block_size=N — SHA-256 hashes per block of
// the current content. The client compares them against its own and finds out which blocks it
// actually needs to send.
func (s *Server) handleSyncFileBlocks(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "files.invalid_id")
		return
	}
	bs, ok := blockSizeParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "sync.bad_block_size")
		return
	}
	f, err := s.q.GetStorageFile(r.Context(), db.GetStorageFileParams{ID: id, OwnerUserID: ownerPg(mu)})
	if err != nil {
		writeErr(w, http.StatusNotFound, "files.not_found")
		return
	}
	obj, err := s.storage.Get(r.Context(), f.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer obj.Close()

	hashes := make([]string, 0, 8)
	buf := make([]byte, bs)
	for {
		n, rerr := io.ReadFull(obj, buf)
		if n > 0 {
			sum := sha256.Sum256(buf[:n])
			hashes = append(hashes, hex.EncodeToString(sum[:]))
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			writeErr(w, http.StatusBadGateway, "storage.download_failed")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"block_size": bs,
		"total_size": f.Size,
		"hash":       f.Hash, // hash of the whole file: serves as If-Match on write
		"hashes":     hashes,
	})
}

// handleSyncUpdateFileDelta: PUT /api/sync/files/{id}/delta?block_size=&total_size=&blocks=1,4,9
// with If-Match. The body is the concatenation, IN ORDER, of the blocks listed in `blocks`. The
// server reconstructs the file: a changed block comes from the body, an unchanged block is copied
// from the old blob.
func (s *Server) handleSyncUpdateFileDelta(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "files.invalid_id")
		return
	}
	base := r.Header.Get("If-Match")
	if base == "" {
		writeErr(w, http.StatusPreconditionRequired, "sync.if_match_required")
		return
	}
	bs, ok := blockSizeParam(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "sync.bad_block_size")
		return
	}
	totalSize, err := strconv.ParseInt(r.URL.Query().Get("total_size"), 10, 64)
	if err != nil || totalSize < 0 || totalSize > s.maxUploadLimit() {
		writeErr(w, http.StatusBadRequest, "sync.bad_total_size")
		return
	}
	changed, err := parseBlockList(r.URL.Query().Get("blocks"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "sync.bad_block_list")
		return
	}

	cur, err := s.q.GetStorageFile(r.Context(), db.GetStorageFileParams{ID: id, OwnerUserID: ownerPg(mu)})
	if err != nil {
		writeErr(w, http.StatusNotFound, "files.not_found")
		return
	}
	// The content must be exactly the one the client computed the delta against — otherwise the
	// "unchanged" blocks we're about to copy are no longer the ones it saw.
	if cur.Hash != base {
		writeErr(w, http.StatusPreconditionFailed, "sync.conflict")
		return
	}

	old, err := s.storage.Get(r.Context(), cur.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer old.Close()

	tmp, err := os.CreateTemp("", "mailin-delta-*")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.temp_failed")
		return
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()

	body := http.MaxBytesReader(w, r.Body, s.maxUploadLimit())
	hasher := sha256.New()
	out := io.MultiWriter(tmp, hasher) // writes while computing the final hash at the same time

	if err := rebuildFromDelta(out, old, body, cur.Size, totalSize, bs, changed); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	hash := hex.EncodeToString(hasher.Sum(nil))
	if hash == cur.Hash { // delta changed nothing: idempotent no-op
		writeJSON(w, http.StatusOK, cur)
		return
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.temp_failed")
		return
	}

	blobKey, code, ecode := s.putBlobWithQuota(r, mu, hash, totalSize, tmp)
	if code != 0 {
		writeErr(w, code, ecode)
		return
	}
	// History: snapshots the previous state before swapping the content (same as a normal upload).
	_ = s.q.SnapshotFileVersion(r.Context(), db.SnapshotFileVersionParams{
		FileID: cur.ID, TenantID: mu.TenantID, BlobKey: cur.BlobKey, Hash: cur.Hash,
		Size: cur.Size, Version: cur.Version, CreatedBy: pgtype.UUID{Bytes: mu.ID, Valid: true},
	})
	rec, err := s.q.UpdateStorageFileContent(r.Context(), db.UpdateStorageFileContentParams{
		ID: cur.ID, TenantID: mu.TenantID, BlobKey: blobKey, Hash: hash, Size: totalSize,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.record_failed")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// parseBlockList reads "0,3,7" into a set of indices. Empty = no block changed.
func parseBlockList(v string) (map[int64]bool, error) {
	out := map[int64]bool{}
	if strings.TrimSpace(v) == "" {
		return out, nil
	}
	for _, part := range strings.Split(v, ",") {
		n, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || n < 0 {
			return nil, err
		}
		out[n] = true
	}
	return out, nil
}

// rebuildFromDelta writes the NEW content to `out`, block by block: changed ones come from `body`,
// the rest are copied from `old`. Deliberately isolated from HTTP — it's the part with real
// correctness risk (file growing/shrinking, partial final block), which is why it has tests.
//
// Both sides are sequential streams, so `old` is advanced even on changed blocks, to stay aligned
// with the block index.
func rebuildFromDelta(out io.Writer, old io.Reader, body io.Reader, oldSize, totalSize, bs int64, changed map[int64]bool) error {
	var written int64
	for i := int64(0); written < totalSize; i++ {
		want := bs
		if rem := totalSize - written; rem < want {
			want = rem // the last block is partial
		}
		// How much this block occupied in the old content (0 if the file grew).
		oldRem := oldSize - i*bs
		if oldRem < 0 {
			oldRem = 0
		}
		if oldRem > bs {
			oldRem = bs
		}

		if changed[i] {
			if _, err := io.CopyN(out, body, want); err != nil {
				return errDeltaShortBody
			}
			if oldRem > 0 {
				if _, err := io.CopyN(io.Discard, old, oldRem); err != nil {
					return errDeltaOldRead
				}
			}
		} else {
			// Unchanged block: it must exist in the old content. If it doesn't, the client got
			// the block markings wrong — we refuse rather than write garbage.
			if oldRem < want {
				return errDeltaMissingBlock
			}
			if _, err := io.CopyN(out, old, want); err != nil {
				return errDeltaOldRead
			}
		}
		written += want
	}
	return nil
}

// Delta errors carrying the same text as the error_codes returned to the client.
var (
	errDeltaShortBody    = deltaErr("sync.delta_short_body")
	errDeltaMissingBlock = deltaErr("sync.delta_missing_block")
	errDeltaOldRead      = deltaErr("storage.download_failed")
)

type deltaErr string

func (e deltaErr) Error() string { return string(e) }
