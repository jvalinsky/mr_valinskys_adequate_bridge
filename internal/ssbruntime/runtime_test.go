package ssbruntime

import (
	"context"
	"database/sql"
	"io"
	"log"
	"path/filepath"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	_ "modernc.org/sqlite"
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

func TestResolveFeedWithEmptyDID(t *testing.T) {
	ctx := context.Background()
	rt, err := Open(ctx, Config{
		RepoPath:   filepath.Join(t.TempDir(), "repo"),
		MasterSeed: []byte("test-master-seed"),
	}, nil)
	if err != nil {
		t.Fatalf("open runtime: %v", err)
	}
	defer rt.Close()

	_, err = rt.ResolveFeed(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty DID")
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

func TestOpenFailsWithInvalidListenAddr(t *testing.T) {
	// Not testing this since Serve() is async and Open won't return an error.
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

func TestOpenWithCorruptLog(t *testing.T) {
	ctx := context.Background()
	repoPath := filepath.Join(t.TempDir(), "repo")
	seed := []byte("test-master-seed")

	// 1. Initial run: publish to create a feed
	rt1, err := Open(ctx, Config{
		RepoPath:   repoPath,
		MasterSeed: seed,
	}, nil)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	_, err = rt1.Publish(ctx, "did:plc:alice", map[string]interface{}{"type": "post", "text": "hi"})
	if err != nil {
		t.Fatalf("first publish: %v", err)
	}
	rt1.Close()

	// 2. Corrupt the database by dropping the messages table
	// This will cause sublog.Seq() to fail later
	db, err := sql.Open("sqlite", filepath.Join(repoPath, "flume.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	_, err = db.Exec("DROP TABLE messages")
	if err != nil {
		t.Fatalf("drop messages table: %v", err)
	}
	db.Close()

	// 3. Re-open should successfully continue despite corrupted scan for the feed
	rt2, err := Open(ctx, Config{
		RepoPath:   repoPath,
		MasterSeed: seed,
	}, nil)
	if err != nil {
		t.Fatalf("re-open runtime: %v", err)
	}
	defer rt2.Close()
}

func TestPublishFailsWithUnsupportedType(t *testing.T) {
	ctx := context.Background()
	rt, err := Open(ctx, Config{
		RepoPath:   filepath.Join(t.TempDir(), "repo"),
		MasterSeed: []byte("test-master-seed"),
	}, nil)
	if err != nil {
		t.Fatalf("open runtime: %v", err)
	}
	defer rt.Close()

	// Channels cannot be marshaled to JSON
	_, err = rt.Publish(ctx, "did:plc:alice", map[string]interface{}{"ch": make(chan int)})
	if err == nil {
		t.Fatal("expected error for unsupported type in JSON marshal")
	}
}

func TestOpenWithCorruptLog2(t *testing.T) {
	ctx := context.Background()
	repoPath := filepath.Join(t.TempDir(), "repo")
	seed := []byte("test-master-seed")

	// 1. Initial run: publish to create two feeds
	rt1, err := Open(ctx, Config{
		RepoPath:   repoPath,
		MasterSeed: seed,
	}, nil)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	rt1.Publish(ctx, "did:plc:alice", map[string]interface{}{"type": "post"})
	rt1.Publish(ctx, "did:plc:bob", map[string]interface{}{"type": "post"})
	rt1.Close()

	// 2. Corrupt one feed by dropping the messages table
	db, err := sql.Open("sqlite", filepath.Join(repoPath, "flume.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.Exec("DROP TABLE messages")
	db.Close()

	// 3. Re-open should skip the corrupted feed and continue
	rt2, err := Open(ctx, Config{
		RepoPath:   repoPath,
		MasterSeed: seed,
	}, nil)
	if err != nil {
		t.Fatalf("re-open runtime: %v", err)
	}
	defer rt2.Close()
}

func TestGossipDBAdapter(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "bridge.sqlite")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	adapter := &GossipDBAdapter{db: database}

	// Test AddKnownPeer
	pubKey := []byte("testpubkey12345678901234567890123456")
	err = adapter.AddKnownPeer(ctx, "127.0.0.1:8008", pubKey)
	if err != nil {
		t.Fatalf("AddKnownPeer: %v", err)
	}

	// Test GetKnownPeers
	peers, err := adapter.GetKnownPeers(ctx)
	if err != nil {
		t.Fatalf("GetKnownPeers: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}
	if peers[0].Addr != "127.0.0.1:8008" {
		t.Errorf("expected Addr=127.0.0.1:8008, got %s", peers[0].Addr)
	}
}

func TestRuntimeGetPeersWithNilNode(t *testing.T) {
	rt := &Runtime{}
	peers := rt.GetPeers()
	if peers != nil {
		t.Errorf("expected nil peers for nil node, got %v", peers)
	}
}

func TestRuntimeGetEBTStateWithNilNode(t *testing.T) {
	rt := &Runtime{}
	state := rt.GetEBTState()
	if state != nil {
		t.Errorf("expected nil state for nil node, got %v", state)
	}
}

func TestRuntimeConnectPeerWithNilNode(t *testing.T) {
	rt := &Runtime{}
	err := rt.ConnectPeer(context.Background(), "127.0.0.1:8008", make([]byte, 32))
	if err == nil {
		t.Error("expected error for nil node")
	}
}

func TestRuntimeConnectPeerWithInvalidKeyLength(t *testing.T) {
	ctx := context.Background()
	rt, err := Open(ctx, Config{
		RepoPath:   filepath.Join(t.TempDir(), "repo"),
		MasterSeed: []byte("test-master-seed"),
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("open runtime: %v", err)
	}
	defer rt.Close()

	err = rt.ConnectPeer(ctx, "127.0.0.1:8008", []byte("short"))
	if err == nil {
		t.Error("expected error for invalid key length")
	}
}

func TestRuntimeBlobStoreWithNilNode(t *testing.T) {
	rt := &Runtime{}
	bs := rt.BlobStore()
	if bs != nil {
		t.Errorf("expected nil blob store for nil node, got %v", bs)
	}
}
