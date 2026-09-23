package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Embedder generates text embeddings on an OpenAI-compatible endpoint (POST {base}/embeddings).
// Provider chosen for the wiki's RAG (docs/wiki-features.md §7.2): OpenAI text-embedding-3-small,
// dimension 1536. Config only via the environment (law 2.6); without EMBEDDINGS_API_KEY, RAG stays off.
type Embedder struct {
	apiKey  string
	baseURL string
	model   string
	// dims: the dimension we store with. The column is `vector(1536)` and pgvector's ivfflat index
	// does not go past 2000 dimensions — with qwen3-embedding-8b's native 4096, either the INSERT
	// blows up, or we lose the index and search degrades to a full table scan.
	//
	// We truncate here, not in the request: TensorX refuses the `dimensions` parameter with a 400
	// (confirmed against the real endpoint). Truncating is the intended mechanism — Qwen3 is
	// trained with Matryoshka, same as OpenAI's text-embedding-3, and keeps the meaning in the
	// first components.
	dims int
	hc   *http.Client
}

// NewEmbedderFromEnv reads the config from the environment and returns nil when disabled:
//
//	EMBEDDINGS_API_KEY     required to enable
//	EMBEDDINGS_BASE_URL    required -- any OpenAI-compatible /embeddings endpoint
//	EMBEDDINGS_MODEL       required
//	EMBEDDINGS_DIMENSIONS  default 1536 -- must match the vector(N) column
//
// The address and the model have no defaults on purpose. They used to default to one vendor's
// endpoint, which meant an installation that set only the key sent that key -- and the text of
// every wiki page -- to a company it had never chosen. A key belongs to whoever it was issued by,
// and guessing that wrong is not a wrong answer, it is a disclosure.
func NewEmbedderFromEnv() *Embedder {
	key := os.Getenv("EMBEDDINGS_API_KEY")
	if key == "" {
		return nil
	}
	base, model := os.Getenv("EMBEDDINGS_BASE_URL"), os.Getenv("EMBEDDINGS_MODEL")
	if base == "" || model == "" {
		// Loud, because the alternative is a wiki whose semantic search silently never works and
		// a screen that looks identical to one still indexing (law 2.9).
		log.Printf("embeddings: EMBEDDINGS_API_KEY is set but EMBEDDINGS_BASE_URL and EMBEDDINGS_MODEL are not -- semantic search stays OFF")
		return nil
	}
	dims := 1536
	if v, err := strconv.Atoi(os.Getenv("EMBEDDINGS_DIMENSIONS")); err == nil && v > 0 {
		dims = v
	}
	return &Embedder{
		apiKey:  key,
		baseURL: strings.TrimRight(base, "/"),
		model:   model,
		dims:    dims,
		hc:      &http.Client{Timeout: 60 * time.Second},
	}
}

// Embed returns one embedding per text, in the same order as the input.
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(map[string]any{"model": e.model, "input": texts})
	if err != nil {
		return nil, fmt.Errorf("embeddings: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embeddings: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	resp, err := e.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings: request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		return nil, fmt.Errorf("embeddings: provider returned %d: %s", resp.StatusCode, apiErr.Error.Message)
	}
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("embeddings: decode response: %w", err)
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings: provider returned %d vectors for %d inputs", len(out.Data), len(texts))
	}
	vecs := make([][]float32, len(texts))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(vecs) {
			return nil, fmt.Errorf("embeddings: provider returned out-of-range index %d", d.Index)
		}
		// The dimension HAS to match the column: a vector shorter than expected is a sign the model
		// was swapped, and mixing models in the same table gives a semantic search that silently
		// returns garbage — worse than failing.
		if len(d.Embedding) < e.dims {
			return nil, fmt.Errorf("embeddings: model %q returned %d dimensions, need at least %d",
				e.model, len(d.Embedding), e.dims)
		}
		vecs[d.Index] = shorten(d.Embedding, e.dims)
	}
	return vecs, nil
}

// shorten trims the vector to the first `dims` components and renormalizes it (norm 1).
// The renormalization is not cosmetic: once trimmed, the vector loses length, and search compares
// by inner product — without this, long texts would systematically rank ahead of short ones.
func shorten(v []float32, dims int) []float32 {
	if dims <= 0 || len(v) <= dims {
		return v
	}
	out := v[:dims]
	var sum float64
	for _, x := range out {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return out
	}
	norm := float32(math.Sqrt(sum))
	for i, x := range out {
		out[i] = x / norm
	}
	return out
}
