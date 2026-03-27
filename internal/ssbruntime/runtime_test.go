package ssbruntime

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"
)

func TestOpenRequiresSeed(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "repo"), nil, nil, log.New(io.Discard, "", 0))
	if err == nil {
		t.Fatalf("expected error for empty seed")
	}
}

func TestPublishStoresMessage(t *testing.T) {
	rt, err := Open(filepath.Join(t.TempDir(), "repo"), []byte("unit-test-seed"), nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("open runtime: %v", err)
	}
	defer rt.Close()

	ctx := context.Background()
	ref1, err := rt.Publish(ctx, "did:plc:alice", map[string]interface{}{
		"type": "post",
		"text": "hello from bridge",
	})
	if err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if ref1 == "" {
		t.Fatalf("expected non-empty message ref")
	}

	ref2, err := rt.Publish(ctx, "did:plc:alice", map[string]interface{}{
		"type": "post",
		"text": "second message",
	})
	if err != nil {
		t.Fatalf("second publish: %v", err)
	}
	if ref2 == "" || ref1 == ref2 {
		t.Fatalf("expected distinct non-empty message refs")
	}
}
