package stalwart

import (
	"context"
	"encoding/json"
	"fmt"
)

// Stalwart's blob store — where it keeps the raw message and attachments.
//
// It is a SINGLETON object in its registry (`x:BlobStore`, see Stalwart's
// resources/schema/schema.json). Touching this changes where mail is written, so the two
// operations are kept separate: reading returns exactly what's there, so it can be saved before
// being overwritten.

// BlobStore is the configuration as Stalwart returns it. It stays as raw JSON on purpose: there
// are many variants (Default, S3, Azure, FileSystem, PostgreSql...) and this code only needs to
// carry them, not understand them. Modeling all of them would be inventing work for the day
// Stalwart adds one more.
type BlobStore struct {
	ID     string
	Config json.RawMessage
}

// singletonID is the id of Stalwart's registry singleton objects. It's not our own convention: in
// its own code it's the number 20080258862541, which in the custom base32 alphabet it uses for
// ids spells out exactly this. A `get` with `ids: null` returns an empty list, and `query` doesn't
// even exist for these objects, so the id has to be sent explicitly.
const singletonID = "singleton"

// GetBlobStoreRaw returns Stalwart's response exactly as-is, for diagnostics.
func (c *Client) GetBlobStoreRaw(ctx context.Context) (json.RawMessage, error) {
	return c.call(ctx, "x:BlobStore/get", map[string]any{"ids": []string{singletonID}})
}

// GetBlobStore reads the current configuration.
func (c *Client) GetBlobStore(ctx context.Context) (*BlobStore, error) {
	raw, err := c.GetBlobStoreRaw(ctx)
	if err != nil {
		return nil, err
	}
	var r struct {
		List []json.RawMessage `json:"list"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("decode BlobStore/get: %w", err)
	}
	if len(r.List) == 0 {
		return nil, fmt.Errorf("stalwart: BlobStore/get returned no list; raw response: %s", raw)
	}
	return &BlobStore{ID: singletonID, Config: r.List[0]}, nil
}

// S3BlobStore is the S3 variant, with Stalwart's schema field names.
type S3BlobStore struct {
	Bucket    string
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	KeyPrefix string
}

// SetBlobStoreS3 points the blob store at an S3 destination.
//
// `verifyAfterWrite` stays ON: Stalwart does a HEAD after every write to guard against stores
// that acknowledge before making the object visible. Turning it off would save one network round
// trip per message and trade that for losing mail silently.
func (c *Client) SetBlobStoreS3(ctx context.Context, id string, s S3BlobStore) error {
	cfg := map[string]any{
		"@type":  "S3",
		"bucket": s.Bucket,
		"region": map[string]any{
			"@type":          "Custom",
			"customEndpoint": s.Endpoint,
			"customRegion":   s.Region,
		},
		"accessKey": s.AccessKey,
		"secretKey": map[string]any{"@type": "Value", "secret": s.SecretKey},
		"keyPrefix": s.KeyPrefix,

		"maxRetries":        3,
		"timeout":           30000,
		"verifyAfterWrite":  true,
		"allowInvalidCerts": false,
	}
	raw, err := c.call(ctx, "x:BlobStore/set", map[string]any{
		"update": map[string]any{id: cfg},
	})
	if err != nil {
		return err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("decode BlobStore/set: %w", err)
	}
	if len(r.NotUpdated) > 0 {
		return fmt.Errorf("stalwart rejected the change: %s", string(mustJSON(r.NotUpdated)))
	}
	return nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// ReloadSettings makes Stalwart re-read the registry configuration.
//
// This is REQUIRED after changing the blob store: `set` saves the definition, but the process
// keeps using the one it holds in memory — and new mail keeps going to the old location, with no
// error at all, which is the quietest possible way for a change to not happen.
//
// In its model this is an action object created in the registry, not a method of its own:
// `x:Action/set` with the `ReloadSettings` variant. The change is broadcast across the cluster.
func (c *Client) ReloadSettings(ctx context.Context) error {
	raw, err := c.call(ctx, "x:Action/set", map[string]any{
		"create": map[string]any{"a1": map[string]any{"@type": "ReloadSettings"}},
	})
	if err != nil {
		return err
	}
	var r setResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("decode Action/set: %w", err)
	}
	if len(r.NotCreated) > 0 {
		return fmt.Errorf("reload rejected: %s", string(mustJSON(r.NotCreated)))
	}
	return nil
}
