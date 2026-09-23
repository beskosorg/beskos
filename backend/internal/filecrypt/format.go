// Package filecrypt encrypts large objects in blocks, so as to allow reading by range.
//
// WHY IN BLOCKS and not a single pass: the product serves video with seek forward/back
// (http.ServeContent honors the Range header) and the desktop sync reads files in chunks to
// compare hashes. An encryption that requires decrypting from the start to reach the middle of a
// 2 GB file would make both of those unusable. With fixed-size blocks, a byte range
// translates into blocks, and only those get decrypted.
//
// THE OBJECT'S KEY TRAVELS INSIDE THE OBJECT, wrapped. This is not a detail: it's what makes the
// write ATOMIC. If the key lived in a separate table, saving a file would be two writes — the
// object and the row — and either order has a failure window: writing the object first leaves
// ciphertext with no key (data lost), writing the row first leaves orphan rows. With the key
// wrapped in the header there is ONE write, and the storage PUT either happens in full or does
// not happen at all. There is no order to get right because there are no two things to order.
//
// Whoever stores the object ends up with the wrapped key — and cannot open it: it would need the
// tenant's key (in our database) and the KEK (in our environment), and it has neither.
//
// FORMAT
//
//	header (32) | wrapped key (n) | block 0 | block 1 | … | block n
//
//	header:  "BSK1" (4) | version (1) | options (1) | reserved (2) | block size (4)
//	         | plaintext size (8) | nonce prefix (4) | KEK version (2)
//	         | wrapped key size (2) | reserved (4)
//	block:   AES-256-GCM of the plaintext chunk, with a 16-byte tag at the end
//
// THE MAGIC IN THE HEADER distinguishes an encrypted object from an object that predates
// encryption. It's what allows migrating the old content without downtime: as long as an object
// hasn't been rewritten, it has no magic and is served as-is.
//
// NONCE: 4-byte random prefix ‖ block index in 8 bytes. Since the key is unique per object,
// the index alone would be enough; the prefix is belt and suspenders.
//
// ASSOCIATED DATA: the block index and the last-block marker go in authenticated but not
// encrypted. Without that, whoever controlled the storage could reorder blocks or cut off the end
// of the file without decryption raising an error — each block's tag would remain valid in isolation.
package filecrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// Magic identifies an object we encrypted. Changes if the format changes incompatibly.
	Magic = "BSK1"

	// FixedHeaderSize is the fixed-size part of the header. The wrapped key comes right after
	// it, and its length is declared here — it's what allows knowing where block 0 starts after
	// reading just these 32 bytes.
	FixedHeaderSize = 32

	// TagSize is the AES-GCM tag.
	TagSize = 16

	// DefaultChunkSize — 1 MiB. Trade-off between overhead (16 bytes per block, 0.0015%) and
	// seek granularity: a jump in the video decrypts at most 1 MiB more than requested.
	DefaultChunkSize = 1 << 20

	// KeySize is the AES-256 key size, used for both DEKs and tenant keys.
	KeySize = 32

	// FlagTenantWrapped means the object's key was wrapped by the TENANT's key (the normal
	// case). Without this option, it was wrapped directly by the KEK — system objects, whose
	// storage key does not start with a tenant.
	FlagTenantWrapped = 1 << 0

	nonceSize  = 12 // 4-byte prefix + 8-byte counter
	prefixSize = 4
)

var (
	// ErrNotEncrypted means the object has none of our magic — it's legacy, plaintext content.
	ErrNotEncrypted = errors.New("filecrypt: object has no encrypted header")
	// ErrCorrupt covers an invalid tag, a truncated header, and a block out of place.
	ErrCorrupt = errors.New("filecrypt: object is corrupt or tampered with")
)

// Header describes an encrypted object.
type Header struct {
	ChunkSize     int
	PlainSize     int64
	NoncePrefix   [prefixSize]byte
	TenantWrapped bool
	// KEKVersion only has meaning when TenantWrapped is false.
	KEKVersion int
	// WrappedKey is the object's key, wrapped. Filled in by ParseWrappedKey.
	WrappedKey []byte
}

// DataOffset is where block 0 starts.
func (h Header) DataOffset() int64 { return int64(FixedHeaderSize + len(h.WrappedKey)) }

// MarshalHeader serializes the fixed header followed by the wrapped key.
func MarshalHeader(h Header) []byte {
	buf := make([]byte, FixedHeaderSize, FixedHeaderSize+len(h.WrappedKey))
	copy(buf[0:4], Magic)
	buf[4] = 1 // format version
	if h.TenantWrapped {
		buf[5] |= FlagTenantWrapped
	}
	binary.BigEndian.PutUint32(buf[8:12], uint32(h.ChunkSize))
	binary.BigEndian.PutUint64(buf[12:20], uint64(h.PlainSize))
	copy(buf[20:24], h.NoncePrefix[:])
	binary.BigEndian.PutUint16(buf[24:26], uint16(h.KEKVersion))
	binary.BigEndian.PutUint16(buf[26:28], uint16(len(h.WrappedKey)))
	return append(buf, h.WrappedKey...)
}

// ParseHeader reads the fixed part. Returns ErrNotEncrypted when the magic does not match — the
// normal case for objects that were already in the bucket before this encryption existed.
//
// The wrapped key does NOT come here: the caller reads the next `WrappedKeyLen` bytes and passes
// them to ParseWrappedKey. That separation is what lets deciding, with just 32 bytes, whether it
// is worth reading more.
func ParseHeader(buf []byte) (Header, error) {
	if len(buf) < FixedHeaderSize || string(buf[0:4]) != Magic {
		return Header{}, ErrNotEncrypted
	}
	if buf[4] != 1 {
		return Header{}, fmt.Errorf("filecrypt: unknown format version %d", buf[4])
	}
	h := Header{
		ChunkSize:     int(binary.BigEndian.Uint32(buf[8:12])),
		PlainSize:     int64(binary.BigEndian.Uint64(buf[12:20])),
		TenantWrapped: buf[5]&FlagTenantWrapped != 0,
		KEKVersion:    int(binary.BigEndian.Uint16(buf[24:26])),
	}
	if h.ChunkSize <= 0 || h.PlainSize < 0 {
		return Header{}, ErrCorrupt
	}
	copy(h.NoncePrefix[:], buf[20:24])
	h.WrappedKey = make([]byte, binary.BigEndian.Uint16(buf[26:28]))
	return h, nil
}

// WrappedKeyLen says how many bytes of wrapped key follow the fixed header.
func (h Header) WrappedKeyLen() int { return len(h.WrappedKey) }

// EncryptedSize returns the size of the encrypted object. Used by the upload to declare the
// length without having to build everything in memory.
func EncryptedSize(plainSize int64, chunkSize, wrappedLen int) int64 {
	base := int64(FixedHeaderSize + wrappedLen)
	if plainSize == 0 {
		return base
	}
	blocks := (plainSize + int64(chunkSize) - 1) / int64(chunkSize)
	return base + plainSize + blocks*TagSize
}

// chunkOffset returns where, in the encrypted object, the block of index i starts.
func chunkOffset(h Header, i int64) int64 {
	return h.DataOffset() + i*int64(h.ChunkSize+TagSize)
}

// nonce assembles the block's nonce: prefix ‖ index.
func nonce(prefix [prefixSize]byte, index int64) []byte {
	n := make([]byte, nonceSize)
	copy(n[:prefixSize], prefix[:])
	binary.BigEndian.PutUint64(n[prefixSize:], uint64(index))
	return n
}

// aad is the block's associated data: index and last-block marker. Authenticated, not
// encrypted — this is what prevents reordering blocks and cutting off the end of the file.
func aad(index int64, last bool) []byte {
	b := make([]byte, 9)
	binary.BigEndian.PutUint64(b[:8], uint64(index))
	if last {
		b[8] = 1
	}
	return b
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("filecrypt: key must be %d bytes, it has %d", KeySize, len(key))
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
}
