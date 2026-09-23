package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// EffectLog gives write tools IDEMPOTENCY (law 5.3, §7.5).
//
// A background task that restarts — because the server went down midway — replays the cycle from
// the start. Without this guard, an e-mail that was already sent would go out again. With it, the
// second attempt recognizes the effect and returns the summary from the first, without touching anything.
//
// Only applies to WRITE tools: repeating a search is cheap and harmless, and recording every read
// would just fill up the table.
type EffectLog interface {
	// Seen returns the summary of an effect already produced, if it has already happened.
	Seen(ctx context.Context, fingerprint string) (string, bool, error)
	// Record marks the effect as done.
	Record(ctx context.Context, fingerprint, summary string) error
}

// effectFingerprint identifies an effect by (tool + arguments).
//
// The arguments are NORMALIZED before being summarized: the model can emit the same keys in a
// different order or with different spacing between runs, and two forms of the same request have
// to yield the same fingerprint — otherwise the guard wouldn't guard anything.
//
// Deliberate consequence: two DIFFERENT sends within the same task both go out, as they should.
// The guard prevents repetition, not plurality.
func effectFingerprint(tool string, args json.RawMessage) string {
	h := sha256.New()
	h.Write([]byte(tool))
	h.Write([]byte{0})
	h.Write([]byte(canonicalJSON(args)))
	return hex.EncodeToString(h.Sum(nil))
}

// canonicalJSON rewrites the JSON with keys in order and without extra whitespace. A value that
// fails to deserialize comes back unchanged: a less stable fingerprint is better than none at all.
func canonicalJSON(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	out, err := json.Marshal(sortKeys(v))
	if err != nil {
		return string(raw)
	}
	return string(out)
}

// sortKeys rebuilds maps with keys in order. encoding/json already serializes maps in key order,
// so it's enough to ensure everything that's an object goes through map[string]any.
func sortKeys(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(t))
		for _, k := range keys {
			out[k] = sortKeys(t[k])
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = sortKeys(e)
		}
		return out
	default:
		return v
	}
}
