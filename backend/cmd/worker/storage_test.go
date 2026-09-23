package main

import (
	"context"
	"errors"
	"testing"
)

func TestWorkerStorageDisabledWithoutEndpoint(t *testing.T) {
	t.Setenv("MINIO_ENDPOINT", "")
	st, err := workerStorage(context.Background(), nil)
	if st != nil || err != nil {
		t.Fatalf("storage=%v error=%v", st, err)
	}
}

func TestWorkerStorageHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := workerStorage(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestWorkerStorageRejectsInvalidEndpoint(t *testing.T) {
	t.Setenv("MINIO_ENDPOINT", "invalid endpoint/path")
	if _, err := workerStorage(context.Background(), nil); err == nil {
		t.Fatal("configured storage failure must prevent worker startup")
	}
}
