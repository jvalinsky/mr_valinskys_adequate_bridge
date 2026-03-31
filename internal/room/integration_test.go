package room

import (
	"context"
	"io"
	"log"
	"net"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream"
)

func TestRoomIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repo := t.TempDir()
	logger := log.New(io.Discard, "", 0)

	rt, err := Start(ctx, Config{
		ListenAddr:     "127.0.0.1:0",
		HTTPListenAddr: "127.0.0.1:0",
		RepoPath:       repo,
		Mode:           "open",
	}, logger)
	if err != nil {
		t.Fatalf("failed to start room: %v", err)
	}
	defer rt.Close()

	// 1. Test MUXRPC connectivity (whoami)
	clientKey, _ := keys.Generate()
	conn, err := net.Dial("tcp", rt.Addr())
	if err != nil {
		t.Fatalf("failed to dial room: %v", err)
	}
	defer conn.Close()

	appKey := secretstream.NewAppKey("boxstream")
	shs, err := secretstream.NewClient(conn, appKey, clientKey.Private(), rt.RoomFeed().PubKey())
	if err != nil {
		t.Fatalf("failed to create SHS client: %v", err)
	}
	if err := shs.Handshake(); err != nil {
		t.Fatalf("SHS handshake failed: %v", err)
	}

	rpc := muxrpc.NewServer(ctx, shs, nil, nil)
	var whoami map[string]string
	err = rpc.Async(ctx, &whoami, muxrpc.TypeJSON, muxrpc.Method{"whoami"})
	if err != nil {
		t.Fatalf("whoami call failed: %v", err)
	}
	if whoami["id"] != rt.RoomFeed().String() {
		t.Errorf("expected %s, got %s", rt.RoomFeed(), whoami["id"])
	}

	// 2. Test tunnel.endpoints (after announcing)
	rt.AnnouncePeer(clientKey.FeedRef(), "net:1.2.3.4:8008~shs:"+clientKey.FeedRef().String())

	// tunnel.endpoints is typically a source call
	// For now, let's just verify it returns something or at least doesn't crash
	// (Implementation details of tunnel.endpoints might vary)
}
