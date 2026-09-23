package filecrypt

import (
	"errors"
	"fmt"
	"io"
)

// SeekReader gives random access to the plaintext content of an encrypted object.
//
// This is what keeps video seek forward/back working after encryption: http.ServeContent
// requests a byte range, this reader translates the range into blocks, fetches only those from
// storage, and decrypts them. A jump to the middle of a 2 GB file reads 1 MiB, not 2 GB.
//
// `src` is the ENCRYPTED object with random access — in our case *minio.Object, which resolves
// each Seek with a Range request to storage.
type SeekReader struct {
	src io.ReadSeeker
	h   Header
	gcm interface {
		Open([]byte, []byte, []byte, []byte) ([]byte, error)
	}

	pos   int64 // current position, in bytes of plaintext content
	chunk []byte
	// Index of the block currently in `chunk`. -1 when nothing is loaded.
	chunkIndex int64
}

// NewSeekReader assembles the reader. The header must have already been read from `src`.
func NewSeekReader(src io.ReadSeeker, h Header, key []byte) (*SeekReader, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	return &SeekReader{src: src, h: h, gcm: gcm, chunkIndex: -1}, nil
}

// Size returns the plaintext size — what the client has to see in the HTTP headers.
func (r *SeekReader) Size() int64 { return r.h.PlainSize }

func (r *SeekReader) Read(p []byte) (int, error) {
	if r.pos >= r.h.PlainSize {
		return 0, io.EOF
	}
	idx := r.pos / int64(r.h.ChunkSize)
	if idx != r.chunkIndex {
		if err := r.load(idx); err != nil {
			return 0, err
		}
	}
	inside := r.pos - idx*int64(r.h.ChunkSize)
	n := copy(p, r.chunk[inside:])
	r.pos += int64(n)
	return n, nil
}

func (r *SeekReader) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = r.pos + offset
	case io.SeekEnd:
		abs = r.h.PlainSize + offset
	default:
		return 0, fmt.Errorf("filecrypt: invalid whence: %d", whence)
	}
	if abs < 0 {
		return 0, errors.New("filecrypt: negative position")
	}
	// Positioning past the end is legitimate (io.Seeker allows it); the next read gives EOF.
	r.pos = abs
	return abs, nil
}

// load fetches a block from storage and decrypts it.
func (r *SeekReader) load(idx int64) error {
	// The last block is shorter: the size comes from what's left until the plaintext end.
	remaining := r.h.PlainSize - idx*int64(r.h.ChunkSize)
	if remaining <= 0 {
		return io.EOF
	}
	size := int64(r.h.ChunkSize)
	last := false
	if remaining <= size {
		size, last = remaining, true
	}

	if _, err := r.src.Seek(chunkOffset(r.h, idx), io.SeekStart); err != nil {
		return err
	}
	buf := make([]byte, size+TagSize)
	if _, err := io.ReadFull(r.src, buf); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return ErrCorrupt
		}
		return err
	}
	opened, err := r.gcm.Open(nil, nonce(r.h.NoncePrefix, idx), buf, aad(idx, last))
	if err != nil {
		return ErrCorrupt
	}
	r.chunk, r.chunkIndex = opened, idx
	return nil
}
