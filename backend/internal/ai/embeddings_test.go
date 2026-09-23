package ai

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The model returns 4096 dimensions and the column is vector(1536): the trim has to happen here,
// and the trimmed vector has to come out normalized. We also cannot request `dimensions` —
// TensorX refuses the parameter with a 400.
func TestEmbedTruncatesAndNormalizes(t *testing.T) {
	var sentDims bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if _, ok := in["dimensions"]; ok {
			sentDims = true
		}
		n := 4096
		if r.URL.Query().Get("short") != "" {
			n = 768 // simulates the model being swapped for a smaller one
		}
		vec := make([]float32, n)
		for i := range vec {
			vec[i] = 0.5 // norm != 1 on purpose, so normalization is visible
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"index": 0, "embedding": vec}},
		})
	}))
	defer srv.Close()

	e := &Embedder{apiKey: "k", baseURL: srv.URL, model: "qwen/qwen3-embedding-8b", dims: 1536, hc: srv.Client()}
	vecs, err := e.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sentDims {
		t.Error("should not send `dimensions` — TensorX answers 400")
	}
	if len(vecs[0]) != 1536 {
		t.Fatalf("vector with %d dimensions, want 1536", len(vecs[0]))
	}
	var sum float64
	for _, x := range vecs[0] {
		sum += float64(x) * float64(x)
	}
	if math.Abs(math.Sqrt(sum)-1) > 1e-5 {
		t.Errorf("unnormalized vector: norm = %v", math.Sqrt(sum))
	}

	// Vector shorter than the column: a clear error, not a garbage write.
	e2 := &Embedder{apiKey: "k", baseURL: srv.URL + "?short=1", model: "outro", dims: 1536, hc: srv.Client()}
	if _, err := e2.Embed(context.Background(), []string{"hello"}); err == nil {
		t.Error("insufficient dimension should error out")
	} else if !strings.Contains(err.Error(), "need at least 1536") {
		t.Errorf("unclear error: %v", err)
	}
}
