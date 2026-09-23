package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/filecrypt"
)

// Envelope encryption for objects (docs/file-encryption.md). lang:ok
//
// It lives HERE, and not in the handlers, on purpose: there are 33 points in the code that read and
// write files — mail, chat, wiki, boards, presentations, recordings, OnlyOffice, sync — and none of
// them change. The storage service is the only one that talks to the bucket, so it's the only place
// where encryption has to exist. Spreading it across the handlers would guarantee one of them got
// forgotten.
//
// WITHOUT A KEY CONFIGURED the service writes in the clear, as before. That's what lets this ship
// to production without a jump: first the code lands, then the key, and only then do new objects
// start being born encrypted. Reading keeps working in both cases because detection is by the magic
// in the object's header.

// keyring resolves keys and keeps the tenants' keys in memory, since there are few of them and they
// are read very often.
type keyring struct {
	q   *db.Queries
	kek *filecrypt.KEK

	mu     sync.RWMutex
	tenant map[uuid.UUID][]byte
}

// EnableEncryption turns encryption on. `kekConfig` comes from the environment (FILE_ENC_KEYS);
// empty leaves the service writing in the clear. Returns an error if the config exists but is
// malformed — starting up with an invalid key would be worse than not starting up.
func (s *Service) EnableEncryption(q *db.Queries, kekConfig string) error {
	kek, err := filecrypt.ParseKEK(kekConfig)
	if err != nil {
		return err
	}
	if kek == nil {
		return nil // no key: old behavior, in the clear
	}
	s.keys = &keyring{q: q, kek: kek, tenant: map[uuid.UUID][]byte{}}
	return nil
}

// Encrypting reports whether new objects are born encrypted.
func (s *Service) Encrypting() bool { return s.keys != nil }

// tenantFromKey extracts the tenant from the key's prefix (`<uuid>/…`). System keys — old
// recordings, under `recordings/…` — have no tenant, and their DEK is wrapped by the KEK.
func tenantFromKey(key string) (uuid.UUID, bool) {
	i := strings.IndexByte(key, '/')
	if i <= 0 {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(key[:i])
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// tenantKey returns a tenant's key, creating it on first use.
func (k *keyring) tenantKey(ctx context.Context, tenant uuid.UUID) ([]byte, error) {
	k.mu.RLock()
	cached, ok := k.tenant[tenant]
	k.mu.RUnlock()
	if ok {
		return cached, nil
	}

	row, err := k.q.GetTenantKey(ctx, tenant)
	if err == nil {
		key, uerr := k.kek.Unwrap(row.WrappedKey, int(row.KekVersion))
		if uerr != nil {
			return nil, uerr
		}
		k.store(tenant, key)
		return key, nil
	}

	// Doesn't exist yet: generate it. Two calls in parallel for the same tenant can't create two
	// keys — the second would make unreadable what the first encrypted. The INSERT is ON CONFLICT
	// DO NOTHING; if it returns no row, someone else won the race and we re-read theirs.
	fresh, err := filecrypt.NewKey()
	if err != nil {
		return nil, err
	}
	wrapped, version, err := k.kek.Wrap(fresh)
	if err != nil {
		return nil, err
	}
	created, err := k.q.CreateTenantKey(ctx, db.CreateTenantKeyParams{
		TenantID: tenant, WrappedKey: wrapped, KekVersion: int32(version),
	})
	if err != nil {
		// No row returned: there was a race. Re-read the key the other call created.
		row, rerr := k.q.GetTenantKey(ctx, tenant)
		if rerr != nil {
			return nil, fmt.Errorf("tenant key %s: %w", tenant, err)
		}
		key, uerr := k.kek.Unwrap(row.WrappedKey, int(row.KekVersion))
		if uerr != nil {
			return nil, uerr
		}
		k.store(tenant, key)
		return key, nil
	}
	key, err := k.kek.Unwrap(created.WrappedKey, int(created.KekVersion))
	if err != nil {
		return nil, err
	}
	k.store(tenant, key)
	return key, nil
}

func (k *keyring) store(tenant uuid.UUID, key []byte) {
	k.mu.Lock()
	k.tenant[tenant] = key
	k.mu.Unlock()
}

// wrapForKey creates the key for a new object and returns it both in the clear and wrapped. None of
// this goes to the database: the wrapped version travels in the object's header, which is what makes
// the write a single operation. See the filecrypt package comment.
func (k *keyring) wrapForKey(ctx context.Context, blobKey string) (dek []byte, h filecrypt.Header, err error) {
	dek, err = filecrypt.NewKey()
	if err != nil {
		return nil, h, err
	}
	h.ChunkSize = filecrypt.DefaultChunkSize

	if tenant, ok := tenantFromKey(blobKey); ok {
		tk, err := k.tenantKey(ctx, tenant)
		if err != nil {
			return nil, h, err
		}
		wrapped, err := filecrypt.WrapDEK(tk, dek)
		if err != nil {
			return nil, h, err
		}
		h.TenantWrapped, h.WrappedKey = true, wrapped
		return dek, h, nil
	}

	// System object (old recordings, under `recordings/…`): wrapped with the KEK, and left out of
	// per-tenant deletion. New recordings carry a tenant prefix for exactly this reason.
	wrapped, version, err := k.kek.Wrap(dek)
	if err != nil {
		return nil, h, err
	}
	h.KEKVersion, h.WrappedKey = version, wrapped
	return dek, h, nil
}

// unwrapFromHeader opens the key that came inside the object.
func (k *keyring) unwrapFromHeader(ctx context.Context, blobKey string, h filecrypt.Header) ([]byte, error) {
	if !h.TenantWrapped {
		return k.kek.Unwrap(h.WrappedKey, h.KEKVersion)
	}
	tenant, ok := tenantFromKey(blobKey)
	if !ok {
		return nil, fmt.Errorf("storage: %q claims to be tenant-wrapped but the key has no prefix", blobKey)
	}
	tk, err := k.tenantKey(ctx, tenant)
	if err != nil {
		return nil, err
	}
	return filecrypt.UnwrapDEK(tk, h.WrappedKey)
}

// decrypt prepares an object for reading: reads the header, decides whether it's encrypted, and
// returns the right reader. An object without our magic is returned as is — that's how content
// predating encryption keeps being served until the migration reaches it.
func (s *Service) decrypt(ctx context.Context, key string, obj io.ReadCloser) (io.ReadCloser, error) {
	h, read, err := readHeader(obj)
	if errors.Is(err, filecrypt.ErrNotEncrypted) {
		// Predates encryption: return what was already read glued to the rest.
		return join(io.MultiReader(bytes.NewReader(read), obj), obj), nil
	}
	if err != nil {
		obj.Close()
		return nil, err
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
	r, err := filecrypt.DecryptReader(obj, h, dek)
	if err != nil {
		obj.Close()
		return nil, err
	}
	return join(r, obj), nil
}

// readHeader consumes the fixed header and the wrapped key. It also returns the bytes read, for
// whoever needs to glue them back on when the object turns out not to be encrypted after all.
func readHeader(r io.Reader) (filecrypt.Header, []byte, error) {
	fixed := make([]byte, filecrypt.FixedHeaderSize)
	n, err := io.ReadFull(r, fixed)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return filecrypt.Header{}, fixed[:n], err
	}
	h, perr := filecrypt.ParseHeader(fixed[:n])
	if perr != nil {
		return filecrypt.Header{}, fixed[:n], perr
	}
	if l := h.WrappedKeyLen(); l > 0 {
		if _, err := io.ReadFull(r, h.WrappedKey); err != nil {
			return filecrypt.Header{}, fixed[:n], filecrypt.ErrCorrupt
		}
	}
	return h, fixed[:n], nil
}

// join glues a reader to a Closer — the storage object still has to be closed either way.
func join(r io.Reader, c io.Closer) io.ReadCloser { return &readCloser{r: r, c: c} }

type readCloser struct {
	r io.Reader
	c io.Closer
}

func (rc *readCloser) Read(p []byte) (int, error) { return rc.r.Read(p) }
func (rc *readCloser) Close() error               { return rc.c.Close() }
