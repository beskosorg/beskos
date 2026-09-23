package filecrypt

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"strings"
	"testing"
)

// wrapExample simulates a wrapped key: this package's tests don't need to know how to open it,
// only to see it travel in the header with the right size.
var wrapExample = bytes.Repeat([]byte{0xAB}, 60)

func hdr(block int) Header {
	return Header{ChunkSize: block, TenantWrapped: true, WrappedKey: wrapExample}
}

func hdrLen() int64 { return int64(FixedHeaderSize + len(wrapExample)) }

// parseHeader reads the fixed part and the wrapped key of an encrypted object in memory.
func parseHeader(t *testing.T, b []byte) (Header, error) {
	t.Helper()
	h, err := ParseHeader(b[:FixedHeaderSize])
	if err != nil {
		return h, err
	}
	copy(h.WrappedKey, b[FixedHeaderSize:FixedHeaderSize+h.WrappedKeyLen()])
	return h, nil
}

func key(t *testing.T) []byte {
	t.Helper()
	k, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// This is the test that matters most: what comes out has to be byte for byte what went in, at
// several sizes around the block boundary — that's where index errors hide.
func TestRoundTrip(t *testing.T) {
	k := key(t)
	const block = 1024
	sizes := []int{0, 1, 100, block - 1, block, block + 1, 3*block - 1, 3 * block, 3*block + 7}

	for _, n := range sizes {
		plain := make([]byte, n)
		if _, err := rand.Read(plain); err != nil {
			t.Fatal(err)
		}
		var cifrado bytes.Buffer
		if err := EncryptTo(&cifrado, bytes.NewReader(plain), int64(n), k, hdr(block)); err != nil {
			t.Fatalf("size %d: encrypt: %v", n, err)
		}
		if got, want := int64(cifrado.Len()), EncryptedSize(int64(n), block, len(wrapExample)); got != want {
			t.Errorf("size %d: ciphertext has %d bytes, EncryptedSize said %d", n, got, want)
		}

		h, err := parseHeader(t, cifrado.Bytes())
		if err != nil {
			t.Fatalf("size %d: header: %v", n, err)
		}
		if h.PlainSize != int64(n) {
			t.Errorf("size %d: header says %d", n, h.PlainSize)
		}
		r, err := DecryptReader(bytes.NewReader(cifrado.Bytes()[h.DataOffset():]), h, k)
		if err != nil {
			t.Fatal(err)
		}
		back, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("size %d: decrypt: %v", n, err)
		}
		if !bytes.Equal(back, plain) {
			t.Errorf("size %d: content differs on the way back", n)
		}
	}
}

// An old plaintext object has to be recognized as such — that's what keeps the migration safe
// when it fails halfway through (see the file-encryption design doc, §2d).
func TestPlaintextObjectIsNotMistaken(t *testing.T) {
	_, err := ParseHeader([]byte("PK\x03\x04 this is a zip, not ours......."))
	if !errors.Is(err, ErrNotEncrypted) {
		t.Fatalf("want ErrNotEncrypted, got %v", err)
	}
}

// Seek: the whole point of the block format. Each read from an arbitrary position has to
// return exactly the tail of the content.
func TestSeekReadsFromAnyPosition(t *testing.T) {
	k := key(t)
	const block = 256
	plain := make([]byte, 5*block+33)
	if _, err := rand.Read(plain); err != nil {
		t.Fatal(err)
	}
	var cifrado bytes.Buffer
	if err := EncryptTo(&cifrado, bytes.NewReader(plain), int64(len(plain)), k, hdr(block)); err != nil {
		t.Fatal(err)
	}
	h, err := parseHeader(t, cifrado.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	sr, err := NewSeekReader(bytes.NewReader(cifrado.Bytes()), h, k)
	if err != nil {
		t.Fatal(err)
	}
	if sr.Size() != int64(len(plain)) {
		t.Fatalf("Size() = %d, want %d", sr.Size(), len(plain))
	}

	// Positions chosen at the boundaries and in the middle of blocks, which is where mistakes happen.
	for _, off := range []int64{0, 1, block - 1, block, block + 1, 3 * block, int64(len(plain)) - 1} {
		if _, err := sr.Seek(off, io.SeekStart); err != nil {
			t.Fatalf("off %d: seek: %v", off, err)
		}
		got, err := io.ReadAll(sr)
		if err != nil {
			t.Fatalf("off %d: read: %v", off, err)
		}
		if !bytes.Equal(got, plain[off:]) {
			t.Errorf("off %d: read %d bytes, want %d", off, len(got), len(plain)-int(off))
		}
	}

	// SeekEnd is what http.ServeContent uses to discover the size.
	if p, err := sr.Seek(0, io.SeekEnd); err != nil || p != int64(len(plain)) {
		t.Fatalf("SeekEnd = %d, %v", p, err)
	}
}

// Tampering with a byte has to raise an error, not silently wrong content.
func TestTamperingIsDetected(t *testing.T) {
	k := key(t)
	plain := bytes.Repeat([]byte("a"), 3000)
	var cifrado bytes.Buffer
	if err := EncryptTo(&cifrado, bytes.NewReader(plain), int64(len(plain)), k, hdr(1024)); err != nil {
		t.Fatal(err)
	}
	b := cifrado.Bytes()
	b[int(hdrLen())+10] ^= 0xff

	h, _ := parseHeader(t, b)
	r, err := DecryptReader(bytes.NewReader(b[h.DataOffset():]), h, k)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(r); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
}

// Truncating the end has to raise an error. Without the last-block marker in the associated data, a
// cut-off file would pass as valid — each remaining block is still individually well-sealed.
func TestTruncationIsDetected(t *testing.T) {
	k := key(t)
	plain := bytes.Repeat([]byte("b"), 3000)
	var cifrado bytes.Buffer
	if err := EncryptTo(&cifrado, bytes.NewReader(plain), int64(len(plain)), k, hdr(1024)); err != nil {
		t.Fatal(err)
	}
	b := cifrado.Bytes()
	truncated := b[:len(b)-(1024+TagSize)] // one block short

	h, _ := parseHeader(t, truncated)
	r, err := DecryptReader(bytes.NewReader(truncated[h.DataOffset():]), h, k)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(r); err == nil {
		t.Fatal("a truncated file was accepted as complete")
	}
}

// A wrong key must not return content.
func TestWrongKey(t *testing.T) {
	k1, k2 := key(t), key(t)
	var cifrado bytes.Buffer
	if err := EncryptTo(&cifrado, strings.NewReader("segredo"), 7, k1, hdr(1024)); err != nil {
		t.Fatal(err)
	}
	h, _ := parseHeader(t, cifrado.Bytes())
	r, err := DecryptReader(bytes.NewReader(cifrado.Bytes()[h.DataOffset():]), h, k2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(r); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("a wrong key should give ErrCorrupt, gave %v", err)
	}
}

func TestKEKWrapsAndUnwraps(t *testing.T) {
	k1 := strings.Repeat("11", KeySize)
	k2 := strings.Repeat("22", KeySize)
	kek, err := ParseKEK("1:" + k1 + ",2:" + k2)
	if err != nil {
		t.Fatal(err)
	}
	if kek.CurrentVersion() != 2 {
		t.Fatalf("the current one should be the highest, got %d", kek.CurrentVersion())
	}

	material := key(t)
	wrapped, version, err := kek.Wrap(material)
	if err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("wrapped with v%d", version)
	}
	opened, err := kek.Unwrap(wrapped, version)
	if err != nil || !bytes.Equal(opened, material) {
		t.Fatalf("did not come back the same: %v", err)
	}

	// Old material keeps opening as long as the old KEK stays configured — that's what
	// lets the rotation run in the background without stopping the service.
	old, err := kek.WrapWithVersion(material, 1)
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := kek.Unwrap(old, 1); err != nil || !bytes.Equal(opened, material) {
		t.Fatalf("v1 did not unwrap: %v", err)
	}
}

func TestKEKRejectsInvalidConfig(t *testing.T) {
	cases := map[string]string{
		"short hex":       "1:abcd",
		"not hex":         "1:" + strings.Repeat("zz", KeySize),
		"invalid version": "x:" + strings.Repeat("11", KeySize),
	}
	for name, value := range cases {
		if _, err := ParseKEK(value); err == nil {
			t.Errorf("%s: should reject", name)
		}
	}
	// Empty is legitimate: it means encryption is disabled.
	if k, err := ParseKEK(""); err != nil || k != nil {
		t.Errorf("empty should give (nil, nil), gave (%v, %v)", k, err)
	}
}

func TestDEKIsWrappedByTenantKey(t *testing.T) {
	tenant, dek := key(t), key(t)
	wrapped, err := WrapDEK(tenant, dek)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := UnwrapDEK(tenant, wrapped)
	if err != nil || !bytes.Equal(opened, dek) {
		t.Fatalf("DEK did not come back: %v", err)
	}
	// Another tenant's key does not open it — this is what backs the per-tenant deletion promise.
	if _, err := UnwrapDEK(key(t), wrapped); !errors.Is(err, ErrCorrupt) {
		t.Fatal("another tenant's key unwrapped the DEK")
	}
}

func TestHeaderRoundTrip(t *testing.T) {
	var prefix [prefixSize]byte
	copy(prefix[:], []byte{1, 2, 3, 4})
	h := Header{
		ChunkSize: 4096, PlainSize: 123456789, NoncePrefix: prefix,
		TenantWrapped: true, WrappedKey: wrapExample,
	}
	raw := MarshalHeader(h)
	back, err := ParseHeader(raw[:FixedHeaderSize])
	if err != nil {
		t.Fatal(err)
	}
	copy(back.WrappedKey, raw[FixedHeaderSize:])

	if back.ChunkSize != h.ChunkSize || back.PlainSize != h.PlainSize ||
		back.NoncePrefix != h.NoncePrefix || !back.TenantWrapped {
		t.Fatalf("header changed: %+v vs %+v", back, h)
	}
	// The wrapped key has to come back byte for byte: it's what opens the object.
	if !bytes.Equal(back.WrappedKey, h.WrappedKey) {
		t.Fatal("the wrapped key did not survive the round trip")
	}
	// System object: with no tenant, the KEK version is what counts.
	hdr := MarshalHeader(Header{ChunkSize: 4096, KEKVersion: 7, WrappedKey: wrapExample})
	v2, err := ParseHeader(hdr[:FixedHeaderSize])
	if err != nil {
		t.Fatal(err)
	}
	if v2.TenantWrapped || v2.KEKVersion != 7 {
		t.Fatalf("system object: %+v", v2)
	}
}
