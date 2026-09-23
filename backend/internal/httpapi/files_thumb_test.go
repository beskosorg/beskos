package httpapi

import "testing"

// A tiny file can declare an enormous image in its header alone; the check has to run on the
// declared dimensions BEFORE image.Decode allocates a pixel buffer for them, or the cap is
// pointless (the memory bomb already went off by the time anything looks at the result).
func TestDimensionsWithinLimitRejectsAnEnormousDeclaredImage(t *testing.T) {
	if dimensionsWithinLimit(100000, 100000) {
		t.Error("a 10-billion-pixel image should be rejected")
	}
}

func TestDimensionsWithinLimitAcceptsAnOrdinaryPhoto(t *testing.T) {
	if !dimensionsWithinLimit(4000, 3000) {
		t.Error("a 12-megapixel photo should be accepted")
	}
}

func TestDimensionsWithinLimitRejectsNonPositive(t *testing.T) {
	cases := [][2]int{{0, 100}, {100, 0}, {-1, 100}, {100, -1}}
	for _, c := range cases {
		if dimensionsWithinLimit(c[0], c[1]) {
			t.Errorf("dimensionsWithinLimit(%d, %d) = true, want false", c[0], c[1])
		}
	}
}

// A hostile width*height near MaxInt32 must not silently wrap around into a small, accepted
// product on a 32-bit multiplication -- exactly the case the int64 widening exists to close.
func TestDimensionsWithinLimitDoesNotOverflow(t *testing.T) {
	if dimensionsWithinLimit(1<<16, 1<<16) {
		t.Error("65536x65536 should be rejected, not wrap around into a false accept")
	}
}
