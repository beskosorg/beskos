package filecrypt

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// EncryptTo encrypts `src` (with `plainSize` bytes) into `dst`, block by block. It never loads
// the whole file into memory: a 2 GB upload spends one block at a time.
//
// `wrapped` is this object's key already wrapped, which travels in the header — it's what makes
// the write atomic (see the package comment). `key` is the same key in plaintext, to encrypt the blocks.
//
// Returns an error if `src` ends before or after `plainSize` — the size goes in the header and is
// what allows serving ranges without reading the whole object, so it has to be right.
func EncryptTo(dst io.Writer, src io.Reader, plainSize int64, key []byte, h Header) error {
	chunkSize := h.ChunkSize
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
		h.ChunkSize = chunkSize
	}
	gcm, err := newGCM(key)
	if err != nil {
		return err
	}

	var prefix [prefixSize]byte
	if _, err := rand.Read(prefix[:]); err != nil {
		return err
	}
	h.PlainSize, h.NoncePrefix = plainSize, prefix
	if _, err := dst.Write(MarshalHeader(h)); err != nil {
		return err
	}

	buf := make([]byte, chunkSize)
	out := make([]byte, 0, chunkSize+TagSize)
	var readCount int64
	for i := int64(0); ; i++ {
		n, rerr := io.ReadFull(src, buf)
		if n > 0 {
			readCount += int64(n)
			last := readCount >= plainSize
			out = gcm.Seal(out[:0], nonce(prefix, i), buf[:n], aad(i, last))
			if _, werr := dst.Write(out); werr != nil {
				return werr
			}
			if last {
				break
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) || errors.Is(rerr, io.ErrUnexpectedEOF) {
				break
			}
			return rerr
		}
	}
	if readCount != plainSize {
		return fmt.Errorf("filecrypt: wanted %d bytes, read %d", plainSize, readCount)
	}
	return nil
}

// DecryptReader returns a sequential reader of the plaintext content. It's the normal download
// path: read from start to end, one block at a time.
//
// The header must already have been consumed from `src` and passed in `h` — the caller needs to
// read it first to know whether the object is even encrypted.
func DecryptReader(src io.Reader, h Header, key []byte) (io.Reader, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	return &decryptor{src: src, h: h, gcm: gcm, buf: make([]byte, h.ChunkSize+TagSize)}, nil
}

type decryptor struct {
	src io.Reader
	h   Header
	gcm interface {
		Open([]byte, []byte, []byte, []byte) ([]byte, error)
	}
	buf       []byte // encrypted block read from storage
	plain     []byte // plaintext leftover from the current block, not yet delivered
	index     int64
	readCount int64 // plaintext bytes already delivered
	end       bool
}

func (d *decryptor) Read(p []byte) (int, error) {
	for len(d.plain) == 0 {
		if d.end || d.readCount >= d.h.PlainSize {
			return 0, io.EOF
		}
		// How much plaintext is left decides this block's size: the last one is shorter.
		remaining := d.h.PlainSize - d.readCount
		size := int64(d.h.ChunkSize)
		last := false
		if remaining <= size {
			size, last = remaining, true
		}
		n, err := io.ReadFull(d.src, d.buf[:size+TagSize])
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return 0, ErrCorrupt // the header promised more bytes than exist
			}
			return 0, err
		}
		opened, err := d.gcm.Open(nil, nonce(d.h.NoncePrefix, d.index), d.buf[:n], aad(d.index, last))
		if err != nil {
			return 0, ErrCorrupt
		}
		d.plain = opened
		d.readCount += int64(len(opened))
		d.index++
		d.end = last
	}
	n := copy(p, d.plain)
	d.plain = d.plain[n:]
	return n, nil
}
