package main

import (
	"context"
	"fmt"
	"os"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/storage"
)

// workerStorage initializes the shared storage client for background jobs.
func workerStorage(ctx context.Context, q *db.Queries) (*storage.Service, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ep := os.Getenv("MINIO_ENDPOINT")
	if ep == "" {
		return nil, nil
	}
	bucket := os.Getenv("MINIO_BUCKET")
	if bucket == "" {
		bucket = "beskos"
	}
	st, err := storage.New(ep, os.Getenv("MINIO_ACCESS_KEY"), os.Getenv("MINIO_SECRET_KEY"),
		bucket, os.Getenv("MINIO_REGION"), os.Getenv("MINIO_SSL") == "true")
	if err != nil {
		return nil, fmt.Errorf("initialize worker storage: %w", err)
	}
	// Recording assembly writes through here: without this, the final MP4 would go out in
	// plaintext while Drive's files went out encrypted. See the file-encryption design doc §3a.
	if err := st.EnableEncryption(q, os.Getenv("FILE_ENC_KEYS")); err != nil {
		return nil, fmt.Errorf("enable worker storage encryption: %w", err)
	}
	return st, nil
}
