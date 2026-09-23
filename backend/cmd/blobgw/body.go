package main

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
)

// checker reads the body while computing its digest, to compare it at the end against what the
// client signed.
//
// WHY STREAMING and not reading it all up front: an attachment of dozens of MiB does not have to
// fit in memory to be verified. The price is that verification only concludes AFTER the object
// has been written — hence the caller deleting what it wrote when it does not match.
type checker struct {
	source   io.Reader
	digest   hash.Hash
	declared string
}

// unsignedPayload is what AWS accepts for "I did not sign the body". It shows up in clients that
// stream; here there is nothing to compare and verification is skipped.
const unsignedPayload = "UNSIGNED-PAYLOAD"

func newChecker(r io.Reader, declared string) *checker {
	c := &checker{source: r, declared: declared}
	if declared != unsignedPayload {
		c.digest = sha256.New()
	}
	return c
}

func (c *checker) Read(p []byte) (int, error) {
	n, err := c.source.Read(p)
	if n > 0 && c.digest != nil {
		c.digest.Write(p[:n])
	}
	return n, err
}

func (c *checker) verify() error {
	if c.digest == nil {
		return nil
	}
	got := hex.EncodeToString(c.digest.Sum(nil))
	if got != c.declared {
		return errf("body digest %s, signed %s", got[:12], first(c.declared, 12))
	}
	return nil
}

func first(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
