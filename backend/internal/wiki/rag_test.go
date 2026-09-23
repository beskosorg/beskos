package wiki

import (
	"strings"
	"testing"
)

func TestChunkTextSmall(t *testing.T) {
	chunks := chunkText("A short paragraph that easily fits one chunk.")
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk, got %d", len(chunks))
	}
}

func TestChunkTextSplitsOnParagraphs(t *testing.T) {
	para := strings.Repeat("palavra ", 150) // ~1200 chars
	text := para + "\n" + para + "\n" + para
	chunks := chunkText(text)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if len(c) > chunkSize+chunkSize/2 {
			t.Errorf("chunk %d too big: %d chars", i, len(c))
		}
	}
}

func TestChunkTextHardSplitsGiantParagraph(t *testing.T) {
	text := strings.Repeat("x", chunkSize*3)
	chunks := chunkText(text)
	if len(chunks) < 3 {
		t.Fatalf("giant paragraph should hard-split, got %d chunks", len(chunks))
	}
}

func TestChunkTextEmptyAndTiny(t *testing.T) {
	if got := chunkText(""); len(got) != 0 {
		t.Errorf("empty text should give no chunks, got %v", got)
	}
	if got := chunkText("hi"); len(got) != 0 {
		t.Errorf("below-minimum text should give no chunks, got %v", got)
	}
}

func TestChunkTextCap(t *testing.T) {
	// 200 paragraphs of ~1000 chars each → would blow past the ceiling without the cap.
	text := strings.Repeat(strings.Repeat("y", 1000)+"\n", 200)
	if got := chunkText(text); len(got) > maxChunks {
		t.Fatalf("chunks above cap: %d > %d", len(got), maxChunks)
	}
}

func TestVecLiteral(t *testing.T) {
	got := vecLiteral([]float32{0.5, -1, 2.25})
	if got != "[0.5,-1,2.25]" {
		t.Errorf("vecLiteral = %q", got)
	}
	if got := vecLiteral(nil); got != "[]" {
		t.Errorf("empty literal = %q", got)
	}
}
