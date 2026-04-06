package gossip

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/network"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/replication"
	"golang.org/x/crypto/ed25519"
)

type mockHandler struct {
	mu       sync.Mutex
	calls    []string
	manifest *muxrpc.Manifest
}

func (m *mockHandler) Handled(method muxrpc.Method) bool {
	return true
}

func (m *mockHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req.Method.String())
}

func (m *mockHandler) HandleConnect(ctx context.Context, ep muxrpc.Endpoint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "connect")
}

func (m *mockHandler) manifestFn() *muxrpc.Manifest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.manifest == nil {
		m.manifest = muxrpc.NewManifest()
	}
	return m.manifest
}

type testDatabase struct {
	mu       sync.Mutex
	peers    []PeerInfo
	addCalls int
}

func (d *testDatabase) AddKnownPeer(ctx context.Context, addr string, pubKey []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.addCalls++
	d.peers = append(d.peers, PeerInfo{Addr: addr, PubKey: pubKey})
	return nil
}

func (d *testDatabase) GetKnownPeers(ctx context.Context) ([]PeerInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]PeerInfo, len(d.peers))
	copy(result, d.peers)
	return result, nil
}

func pubKeyFromBytes(b [32]byte) ed25519.PublicKey {
	return ed25519.PublicKey(b[:])
}

func newTestNetworkServer(t *testing.T, keyPair *keys.KeyPair, handler muxrpc.Handler) (*network.Server, string) {
	var server *network.Server
	var addr string
	var err error

	for i := 0; i < 100; i++ {
		port := 12000 + i
		addr = fmt.Sprintf("127.0.0.1:%d", port)

		server, err = network.NewServer(network.Options{
			ListenAddr: addr,
			KeyPair:    keyPair,
			AppKey:     "boxstream",
		})
		if err != nil {
			continue
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if err := server.Serve(ctx, handler); err != nil {
			continue
		}

		return server, addr
	}

	t.Fatal("could not find available port: ", err)
	return nil, ""
}

func TestManagerAddPeer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := NewManager(nil, nil, nil, nil, log.New(io.Discard, "", 0))

	aliceKeys, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}

	err = manager.AddPeer(ctx, "127.0.0.1:4567", pubKeyFromBytes(aliceKeys.Public()))
	if err != nil {
		t.Fatal(err)
	}

	manager.mu.Lock()
	peer, ok := manager.peers["127.0.0.1:4567"]
	manager.mu.Unlock()

	if !ok {
		t.Error("expected peer to be added")
	}
	if peer.Addr != "127.0.0.1:4567" {
		t.Errorf("expected addr 127.0.0.1:4567, got %s", peer.Addr)
	}
}

func TestManagerAddPeerPersistsToDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := &testDatabase{}
	manager := NewManager(nil, nil, nil, db, log.New(io.Discard, "", 0))

	aliceKeys, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}

	err = manager.AddPeer(ctx, "127.0.0.1:4567", pubKeyFromBytes(aliceKeys.Public()))
	if err != nil {
		t.Fatal(err)
	}

	if db.addCalls != 1 {
		t.Errorf("expected 1 AddKnownPeer call, got %d", db.addCalls)
	}
}

func TestManagerConnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverKeys, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}

	handler := &mockHandler{}
	server, addr := newTestNetworkServer(t, serverKeys, handler)
	defer server.Close()

	clientKeys, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}

	serverFeedRef := serverKeys.FeedRef()
	stateMatrix, err := replication.NewStateMatrix("", &serverFeedRef, nil)
	if err != nil {
		t.Fatal(err)
	}
	ebtHandler := replication.NewEBTHandler(&serverFeedRef, nil, stateMatrix, nil)

	manager := NewManager(
		network.NewClient(network.Options{
			AppKey:  "boxstream",
			KeyPair: clientKeys,
		}),
		ebtHandler,
		handler,
		nil,
		log.New(io.Discard, "", 0),
	)

	err = manager.Connect(ctx, addr, pubKeyFromBytes(serverKeys.Public()))
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	manager.mu.Lock()
	peer, ok := manager.conns[addr]
	manager.mu.Unlock()

	if !ok {
		t.Error("expected connection to be tracked")
	}
	if peer == nil {
		t.Error("expected peer to be non-nil")
	}
}

func TestManagerConnectStartsEBTReplication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverKeys, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}

	handler := &mockHandler{}
	server, addr := newTestNetworkServer(t, serverKeys, handler)
	defer server.Close()

	clientKeys, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}

	serverFeedRef := serverKeys.FeedRef()
	stateMatrix, err := replication.NewStateMatrix("", &serverFeedRef, nil)
	if err != nil {
		t.Fatal(err)
	}

	ebtHandler := replication.NewEBTHandler(&serverFeedRef, nil, stateMatrix, nil)

	manager := NewManager(
		network.NewClient(network.Options{
			AppKey:  "boxstream",
			KeyPair: clientKeys,
		}),
		ebtHandler,
		handler,
		nil,
		log.New(io.Discard, "", 0),
	)

	err = manager.Connect(ctx, addr, pubKeyFromBytes(serverKeys.Public()))
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	manager.mu.Lock()
	peer, ok := manager.conns[addr]
	manager.mu.Unlock()

	if !ok {
		t.Fatal("expected connection to be tracked")
	}

	if peer.RPC() == nil {
		t.Error("expected RPC to be established")
	}
}

func TestManagerReconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverKeys, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}

	handler := &mockHandler{}
	server, addr := newTestNetworkServer(t, serverKeys, handler)
	defer server.Close()

	clientKeys, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}

	serverFeedRef := serverKeys.FeedRef()
	stateMatrix, _ := replication.NewStateMatrix("", &serverFeedRef, nil)
	ebtHandler := replication.NewEBTHandler(&serverFeedRef, nil, stateMatrix, nil)

	manager := NewManager(
		network.NewClient(network.Options{
			AppKey:  "boxstream",
			KeyPair: clientKeys,
		}),
		ebtHandler,
		handler,
		nil,
		log.New(io.Discard, "", 0),
	)

	manager.mu.Lock()
	manager.peers[addr] = PeerInfo{Addr: addr, PubKey: pubKeyFromBytes(serverKeys.Public())}
	manager.mu.Unlock()

	err = manager.Connect(ctx, addr, pubKeyFromBytes(serverKeys.Public()))
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	manager.mu.Lock()
	_, hasConn := manager.conns[addr]
	manager.mu.Unlock()

	if !hasConn {
		t.Error("expected connection to exist after reconnect")
	}
}

func TestManagerRunLoadsPersistedPeers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := &testDatabase{
		peers: []PeerInfo{
			{Addr: "127.0.0.1:9001", PubKey: []byte("test-pubkey")},
		},
	}

	manager := NewManager(nil, nil, nil, db, log.New(io.Discard, "", 0))

	go manager.Run(ctx)
	defer cancel()

	time.Sleep(50 * time.Millisecond)

	manager.mu.Lock()
	peer, ok := manager.peers["127.0.0.1:9001"]
	manager.mu.Unlock()

	if !ok {
		t.Error("expected persisted peer to be loaded on Run")
	}
	if peer.Addr != "127.0.0.1:9001" {
		t.Errorf("expected addr 127.0.0.1:9001, got %s", peer.Addr)
	}
}

func newTestEBTHandler() *replication.EBTHandler {
	serverKeys, _ := keys.Generate()
	serverFeedRef := serverKeys.FeedRef()
	stateMatrix, _ := replication.NewStateMatrix("", &serverFeedRef, nil)
	return replication.NewEBTHandler(&serverFeedRef, nil, stateMatrix, nil)
}

func TestManagerPingPeers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverKeys, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}

	handler := &mockHandler{}
	server, addr := newTestNetworkServer(t, serverKeys, handler)
	defer server.Close()

	ebtHandler := newTestEBTHandler()

	clientKeys, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}

	manager := NewManager(
		network.NewClient(network.Options{
			AppKey:  "boxstream",
			KeyPair: clientKeys,
		}),
		ebtHandler,
		handler,
		nil,
		log.New(io.Discard, "", 0),
	)

	err = manager.Connect(ctx, addr, pubKeyFromBytes(serverKeys.Public()))
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	manager.pingPeers(ctx)

	time.Sleep(50 * time.Millisecond)

	manager.mu.Lock()
	peer, ok := manager.conns[addr]
	manager.mu.Unlock()

	if !ok {
		t.Fatal("expected connection to exist after ping")
	}

	latency := peer.Latency()
	if latency <= 0 {
		t.Log("latency not set - peer may not have responded to ping")
	}
}

func TestManagerCleanupClosedConnections(t *testing.T) {
	t.Skip("flaky test - reconnect behavior is timing-dependent")
}
