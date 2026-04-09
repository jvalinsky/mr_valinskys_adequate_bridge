package network

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"golang.org/x/crypto/ed25519"
)

type mockHandler struct{}

func (m *mockHandler) Handled(muxrpc.Method) bool                     { return true }
func (m *mockHandler) HandleCall(context.Context, *muxrpc.Request)    {}
func (m *mockHandler) HandleConnect(context.Context, muxrpc.Endpoint) {}

func TestNetwork(t *testing.T) {
	aliceKeys, _ := keys.Generate()
	bobKeys, _ := keys.Generate()

	appKey := "test-app-key"

	server, err := NewServer(Options{
		ListenAddr: "127.0.0.1:0",
		KeyPair:    aliceKeys,
		AppKey:     appKey,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := server.Serve(ctx, &mockHandler{}); err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	addr := server.ln.Addr().String()

	client := NewClient(Options{
		KeyPair: bobKeys,
		AppKey:  appKey,
	})

	alicePub := aliceKeys.Public()
	peer, err := client.Connect(ctx, addr, ed25519.PublicKey(alicePub[:]), nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer peer.Conn.Close()

	// Wait for server to register peer
	time.Sleep(100 * time.Millisecond)

	peers := server.Peers()
	if len(peers) != 1 {
		t.Errorf("expected 1 peer, got %d", len(peers))
	} else {
		if !peers[0].ID.Equal(bobKeys.FeedRef()) {
			t.Errorf("expected peer ID %s, got %s", bobKeys.FeedRef(), peers[0].ID)
		}
	}
}

func TestGetFeedRefFromAddr(t *testing.T) {
	pub := make([]byte, 32)
	for i := range pub {
		pub[i] = byte(i)
	}
	addr := Addr{
		PubKey: pub,
	}

	ref, err := GetFeedRefFromAddr(addr)
	if err != nil {
		t.Fatal(err)
	}

	if string(ref.PubKey()) != string(pub) {
		t.Errorf("unexpected pubkey: %x", ref.PubKey())
	}
}

func TestNetworkManifestBlobsGet(t *testing.T) {
	s := &Server{}
	m := s.newManifest()

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

	var found bool
	for _, n := range names {
		if n == "blobs.get" {
			found = true
			break
		}
	}
	if !found {
		t.Error("blobs.get should be registered as source")
	}
}
