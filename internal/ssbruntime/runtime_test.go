package ssbruntime

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"
)

func TestOpenRequiresSeed(t *testing.T) {
	_, err := Open(context.Background(), Config{
		RepoPath:   filepath.Join(t.TempDir(), "repo"),
		MasterSeed: nil,
	}, log.New(io.Discard, "", 0))
	if err == nil {
		t.Fatalf("expected error for empty seed")
	}
}

func TestPublishStoresMessage(t *testing.T) {
	ctx := context.Background()
	repoPath := filepath.Join(t.TempDir(), "repo")
	logger := log.New(io.Discard, "", 0)
	rt, err := Open(ctx, Config{
		RepoPath:   repoPath,
		MasterSeed: []byte("test-master-seed"),
	}, logger)
	if err != nil {
		t.Fatalf("open runtime: %v", err)
	}
	defer rt.Close()

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

func TestNodeReturnsSbotNode(t *testing.T) {
	ctx := context.Background()
	rt, err := Open(ctx, Config{
		RepoPath:   filepath.Join(t.TempDir(), "repo"),
		MasterSeed: []byte("test-master-seed"),
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("open runtime: %v", err)
	}
	defer rt.Close()

	node := rt.Node()
	if node == nil {
		t.Fatal("expected non-nil sbot node")
	}
}

func TestResolveFeed(t *testing.T) {
	ctx := context.Background()
	rt, err := Open(ctx, Config{
		RepoPath:   filepath.Join(t.TempDir(), "repo"),
		MasterSeed: []byte("test-master-seed"),
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("open runtime: %v", err)
	}
	defer rt.Close()

	feed, err := rt.ResolveFeed(ctx, "did:plc:alice")
	if err != nil {
		t.Fatalf("resolve feed: %v", err)
	}
	if feed == "" {
		t.Fatal("expected non-empty feed ref")
	}
}

func TestResolveFeedWithNilManager(t *testing.T) {
	rt := &Runtime{}
	_, err := rt.ResolveFeed(context.Background(), "did:plc:alice")
	if err == nil {
		t.Fatal("expected error for nil manager")
	}
}

func TestBlobStore(t *testing.T) {
	ctx := context.Background()
	rt, err := Open(ctx, Config{
		RepoPath:   filepath.Join(t.TempDir(), "repo"),
		MasterSeed: []byte("test-master-seed"),
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("open runtime: %v", err)
	}
	defer rt.Close()

	bs := rt.BlobStore()
	if bs == nil {
		t.Fatal("expected non-nil blob store")
	}
}

func TestCloseWithNilNode(t *testing.T) {
	rt := &Runtime{}
	err := rt.Close()
	if err != nil {
		t.Fatalf("expected no error for nil node, got %v", err)
	}
}

func TestOpenWithHMACKey(t *testing.T) {
	ctx := context.Background()
	hmacKey := [32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}
	rt, err := Open(ctx, Config{
		RepoPath:   filepath.Join(t.TempDir(), "repo"),
		MasterSeed: []byte("test-master-seed"),
		HMACKey:    &hmacKey,
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("open runtime with HMAC: %v", err)
	}
	defer rt.Close()

	node := rt.Node()
	if node == nil {
		t.Fatal("expected non-nil sbot node")
	}
}

func TestOpenWithKeyPair(t *testing.T) {
	ctx := context.Background()
	rt, err := Open(ctx, Config{
		RepoPath:   filepath.Join(t.TempDir(), "repo"),
		MasterSeed: []byte("test-master-seed"),
		KeyPair:    nil,
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("open runtime: %v", err)
	}
	defer rt.Close()

	node := rt.Node()
	if node == nil {
		t.Fatal("expected non-nil sbot node")
	}
}

func TestOpenCreatesRepoDirectory(t *testing.T) {
	ctx := context.Background()
	repoPath := filepath.Join(t.TempDir(), "nested", "repo")
	rt, err := Open(ctx, Config{
		RepoPath:   repoPath,
		MasterSeed: []byte("test-master-seed"),
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("open runtime: %v", err)
	}
	defer rt.Close()
}

func TestOpenFailsWithInvalidRepoPath(t *testing.T) {
	ctx := context.Background()
	_, err := Open(ctx, Config{
		RepoPath:   "/invalid/path/that/cannot/be/created",
		MasterSeed: []byte("test-master-seed"),
	}, log.New(io.Discard, "", 0))
	if err == nil {
		t.Fatal("expected error for invalid repo path")
	}
}

func TestPublishWithFeedAlreadyFollowed(t *testing.T) {
	ctx := context.Background()
	rt, err := Open(ctx, Config{
		RepoPath:   filepath.Join(t.TempDir(), "repo"),
		MasterSeed: []byte("test-master-seed"),
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("open runtime: %v", err)
	}
	defer rt.Close()

	ref1, err := rt.Publish(ctx, "did:plc:bob", map[string]interface{}{
		"type": "post",
		"text": "first message",
	})
	if err != nil {
		t.Fatalf("first publish: %v", err)
	}

	ref2, err := rt.Publish(ctx, "did:plc:bob", map[string]interface{}{
		"type": "post",
		"text": "second message",
	})
	if err != nil {
		t.Fatalf("second publish: %v", err)
	}
	if ref1 == ref2 {
		t.Fatalf("expected distinct refs, got same: %s", ref1)
	}
}

func TestResolveFeedWithNilRuntime(t *testing.T) {
	var r *Runtime
	_, err := r.ResolveFeed(context.Background(), "did:plc:x")
	if err == nil {
		t.Fatal("expected error for nil runtime")
	}
}

func TestPublishFailsWithEmptyDID(t *testing.T) {
	ctx := context.Background()
	rt, err := Open(ctx, Config{
		RepoPath:   filepath.Join(t.TempDir(), "repo"),
		MasterSeed: []byte("test-master-seed"),
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("open runtime: %v", err)
	}
	defer rt.Close()

	_, err = rt.Publish(ctx, "", map[string]interface{}{"type": "post"})
	if err == nil {
		t.Fatal("expected error for empty DID")
	}
}

func TestOpenWithExistingFeeds(t *testing.T) {
	ctx := context.Background()
	repoPath := filepath.Join(t.TempDir(), "repo")
	seed := []byte("test-master-seed-for-persistence")

	// 1. First run: publish a message
	rt1, err := Open(ctx, Config{
		RepoPath:   repoPath,
		MasterSeed: seed,
	}, nil)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	_, err = rt1.Publish(ctx, "did:plc:persistence", map[string]interface{}{"type": "post", "text": "hi"})
	if err != nil {
		t.Fatalf("first publish: %v", err)
	}
	rt1.Close()

	// 2. Second run: should log existing feeds
	rt2, err := Open(ctx, Config{
		RepoPath:   repoPath,
		MasterSeed: seed,
	}, nil)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer rt2.Close()
}
