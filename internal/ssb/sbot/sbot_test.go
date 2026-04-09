package sbot

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"golang.org/x/crypto/ed25519"
)

var _ = muxrpc.TypeJSON

func TestSbot(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sbot-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	aliceKeys, _ := keys.Generate()

	node, err := New(Options{
		RepoPath:   tempDir,
		ListenAddr: "127.0.0.1:0",
		KeyPair:    aliceKeys,
		EnableEBT:  true,
	})
	if err != nil {
		t.Fatalf("failed to create sbot: %v", err)
	}
	defer node.Shutdown()

	go func() {
		node.Serve()
	}()

	// Wait for node to start
	time.Sleep(100 * time.Millisecond)

	whoami, err := node.Whoami()
	if err != nil {
		t.Fatal(err)
	}
	if whoami != aliceKeys.FeedRef().String() {
		t.Errorf("expected %s, got %s", aliceKeys.FeedRef(), whoami)
	}

	// Test store access
	if node.Store() == nil {
		t.Error("expected non-nil store")
	}
	if node.EBT() == nil {
		t.Error("expected non-nil EBT handler")
	}
}

func TestSbotRoom(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sbot-room-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	aliceKeys, _ := keys.Generate()

	node, err := New(Options{
		RepoPath:   tempDir,
		ListenAddr: "127.0.0.1:0",
		KeyPair:    aliceKeys,
		EnableRoom: true,
		RoomMode:   "open",
	})
	if err != nil {
		t.Fatalf("failed to create sbot with room: %v", err)
	}
	defer node.Shutdown()

	if node.handlerMux == nil {
		t.Error("expected handlerMux")
	}
}

func TestSbotReplicateTracksWantedRemoteFeed(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sbot-replicate-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	aliceKeys, _ := keys.Generate()

	node, err := New(Options{
		RepoPath:   tempDir,
		ListenAddr: "127.0.0.1:0",
		KeyPair:    aliceKeys,
		EnableEBT:  true,
	})
	if err != nil {
		t.Fatalf("failed to create sbot: %v", err)
	}
	defer node.Shutdown()

	remote := refs.MustNewFeedRef(testFeedBytes(7), refs.RefAlgoFeedSSB1)
	node.Replicate(remote)

	self := aliceKeys.FeedRef()
	frontier, err := node.StateMatrix().Inspect(&self)
	if err != nil {
		t.Fatalf("inspect state matrix: %v", err)
	}

	note, ok := frontier[remote.String()]
	if !ok {
		t.Fatalf("expected replicated feed %s in frontier", remote.String())
	}
	if note.Seq != 0 {
		t.Fatalf("expected remote wanted seq 0, got %d", note.Seq)
	}
	if !note.Replicate || !note.Receive {
		t.Fatalf("expected remote feed to be marked replicate+receive, got %+v", note)
	}
}

func TestSbotConnectUsesSbotLifecycleAndCleansUpPeers(t *testing.T) {
	serverDir, err := os.MkdirTemp("", "sbot-connect-server-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(serverDir)

	clientDir, err := os.MkdirTemp("", "sbot-connect-client-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(clientDir)

	serverKeys, _ := keys.Generate()
	clientKeys, _ := keys.Generate()
	serverAddr := reserveLoopbackAddr(t)
	clientAddr := reserveLoopbackAddr(t)

	serverNode, err := New(Options{
		RepoPath:   serverDir,
		ListenAddr: serverAddr,
		KeyPair:    serverKeys,
		EnableEBT:  true,
	})
	if err != nil {
		t.Fatalf("failed to create server sbot: %v", err)
	}
	defer serverNode.Shutdown()

	clientNode, err := New(Options{
		RepoPath:   clientDir,
		ListenAddr: clientAddr,
		KeyPair:    clientKeys,
		EnableEBT:  true,
	})
	if err != nil {
		t.Fatalf("failed to create client sbot: %v", err)
	}
	defer clientNode.Shutdown()

	go func() { _ = serverNode.Serve() }()
	go func() { _ = clientNode.Serve() }()

	time.Sleep(100 * time.Millisecond)

	connectCtx, cancelConnect := context.WithCancel(context.Background())
	serverPub := serverKeys.Public()
	peer, err := clientNode.Connect(connectCtx, serverAddr, ed25519.PublicKey(serverPub[:]))
	if err != nil {
		t.Fatalf("connect client to server: %v", err)
	}

	cancelConnect()

	waitForTest(t, 2*time.Second, func() bool {
		return len(clientNode.Peers()) == 1 && len(serverNode.Peers()) == 1
	}, "expected connection to survive request context cancellation")

	if err := peer.Conn.Close(); err != nil {
		t.Fatalf("close peer connection: %v", err)
	}

	waitForTest(t, 2*time.Second, func() bool {
		return len(clientNode.Peers()) == 0 && len(serverNode.Peers()) == 0
	}, "expected peer cleanup after connection close")
}

func TestSbotEnsureBlobFetchesFromConnectedPeer(t *testing.T) {
	serverDir, err := os.MkdirTemp("", "sbot-blob-server-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(serverDir)

	clientDir, err := os.MkdirTemp("", "sbot-blob-client-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(clientDir)

	serverKeys, _ := keys.Generate()
	clientKeys, _ := keys.Generate()
	serverAddr := reserveLoopbackAddr(t)
	clientAddr := reserveLoopbackAddr(t)

	serverNode, err := New(Options{
		RepoPath:   serverDir,
		ListenAddr: serverAddr,
		KeyPair:    serverKeys,
		EnableEBT:  true,
	})
	if err != nil {
		t.Fatalf("failed to create server sbot: %v", err)
	}
	defer serverNode.Shutdown()

	clientNode, err := New(Options{
		RepoPath:   clientDir,
		ListenAddr: clientAddr,
		KeyPair:    clientKeys,
		EnableEBT:  true,
	})
	if err != nil {
		t.Fatalf("failed to create client sbot: %v", err)
	}
	defer clientNode.Shutdown()

	go func() { _ = serverNode.Serve() }()
	go func() { _ = clientNode.Serve() }()

	time.Sleep(100 * time.Millisecond)

	serverPub := serverKeys.Public()
	if _, err := clientNode.Connect(context.Background(), serverAddr, ed25519.PublicKey(serverPub[:])); err != nil {
		t.Fatalf("connect client to server: %v", err)
	}

	waitForTest(t, 2*time.Second, func() bool {
		return len(clientNode.Peers()) == 1 && len(serverNode.Peers()) == 1
	}, "expected both peers to be connected")

	payload := []byte("blob-data-from-peer")
	hash, err := serverNode.BlobStore().BlobStore().Put(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("store server blob: %v", err)
	}
	ref, err := refs.NewBlobRef(hash)
	if err != nil {
		t.Fatalf("new blob ref: %v", err)
	}

	if err := clientNode.EnsureBlob(context.Background(), ref); err != nil {
		t.Fatalf("ensure blob: %v", err)
	}

	has, err := clientNode.BlobStore().BlobStore().Has(ref.Hash())
	if err != nil {
		t.Fatalf("check client blob: %v", err)
	}
	if !has {
		t.Fatalf("expected client blob store to contain fetched blob %s", ref.String())
	}
}

func testFeedBytes(fill byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = fill
	}
	return out
}

func waitForTest(t *testing.T, timeout time.Duration, fn func() bool, message string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}

	t.Fatal(message)
}

func reserveLoopbackAddr(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback addr: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close reserved loopback addr: %v", err)
	}
	return addr
}

func TestSbotManifestRoomMethods(t *testing.T) {
	m := newManifest(true, true)

	b, err := m.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	type manifestEntry struct {
		Type  string   `json:"type"`
		Names []string `json:"names"`
	}
	var entries []manifestEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	typeMap := make(map[string][]string)
	for _, e := range entries {
		typeMap[e.Type] = e.Names
	}

	names, ok := typeMap["source"]
	if !ok {
		t.Fatal("missing source in manifest")
	}

	var attendantsFound, membersFound bool
	for _, n := range names {
		if n == "room.attendants" {
			attendantsFound = true
		}
		if n == "room.members" {
			membersFound = true
		}
	}

	if !attendantsFound {
		t.Error("room.attendants should be registered as source")
	}
	if !membersFound {
		t.Error("room.members should be registered as source")
	}

	_, hasAsync := typeMap["async"]
	if hasAsync {
		for _, n := range typeMap["async"] {
			if n == "room.members" {
				t.Error("room.members should NOT be registered as async")
			}
		}
	}
}
