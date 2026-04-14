package room

import (
	"context"
	"io"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream"
)

func TestPeerConnectsToRoom(t *testing.T) {
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
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("failed to start room: %v", err)
	}
	defer rt.Close()

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
}

func TestPeerCallsRoomMetadata(t *testing.T) {
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
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("failed to start room: %v", err)
	}
	defer rt.Close()

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

	var metadata interface{}
	err = rpc.Async(ctx, &metadata, muxrpc.TypeJSON, muxrpc.Method{"room", "metadata"})
	if err != nil {
		t.Fatalf("room.metadata call failed: %v", err)
	}

	t.Logf("metadata response: %+v", metadata)
}

func TestPeerAnnouncesTunnel(t *testing.T) {
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
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("failed to start room: %v", err)
	}
	defer rt.Close()

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

	tunnelAddr := "net:1.2.3.4:8008~shs:" + clientKey.FeedRef().String()
	var announceResp bool
	err = rpc.Sync(ctx, &announceResp, muxrpc.TypeJSON, muxrpc.Method{"tunnel", "announce"}, tunnelAddr)
	if err != nil {
		t.Fatalf("tunnel.announce call failed: %v", err)
	}
	if !announceResp {
		t.Fatalf("expected tunnel.announce to return true")
	}

	t.Logf("announce response: %+v", announceResp)
}

func TestPeerFetchesAttendants(t *testing.T) {
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
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("failed to start room: %v", err)
	}
	defer rt.Close()

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

	src, err := rpc.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"room", "attendants"})
	if err != nil {
		t.Fatalf("room.attendants source failed: %v", err)
	}

	if !src.Next(ctx) {
		if err := src.Err(); err != nil {
			t.Fatalf("source error: %v", err)
		}
	}

	body, err := src.Bytes()
	if err != nil {
		t.Fatalf("source bytes: %v", err)
	}

	t.Logf("attendants response: %s", string(body))
}

func TestPeerCreatesAndUsesInvite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("failed to start room: %v", err)
	}
	defer rt.Close()

	inviterKey, _ := keys.Generate()

	conn, err := net.Dial("tcp", rt.Addr())
	if err != nil {
		t.Fatalf("failed to dial room: %v", err)
	}
	defer conn.Close()

	appKey := secretstream.NewAppKey("boxstream")
	shs, err := secretstream.NewClient(conn, appKey, inviterKey.Private(), rt.RoomFeed().PubKey())
	if err != nil {
		t.Fatalf("failed to create SHS client: %v", err)
	}
	if err := shs.Handshake(); err != nil {
		t.Fatalf("SHS handshake failed: %v", err)
	}

	rpc := muxrpc.NewServer(ctx, shs, nil, nil)

	var inviteCode string
	err = rpc.Async(ctx, &inviteCode, muxrpc.TypeString, muxrpc.Method{"room", "createInvite"})
	if err != nil {
		t.Fatalf("room.createInvite failed: %v", err)
	}

	t.Logf("invite code: %s", inviteCode)

	if inviteCode == "" {
		t.Fatal("expected non-empty invite code")
	}

	inviteeKey, _ := keys.Generate()

	conn2, err := net.Dial("tcp", rt.Addr())
	if err != nil {
		t.Fatalf("failed to dial room with invitee: %v", err)
	}
	defer conn2.Close()

	shs2, err := secretstream.NewClient(conn2, appKey, inviteeKey.Private(), rt.RoomFeed().PubKey())
	if err != nil {
		t.Fatalf("failed to create SHS client for invitee: %v", err)
	}
	if err := shs2.Handshake(); err != nil {
		t.Fatalf("SHS handshake failed for invitee: %v", err)
	}

	rpc2 := muxrpc.NewServer(ctx, shs2, nil, nil)

	var joined interface{}
	err = rpc2.Async(ctx, &joined, muxrpc.TypeJSON, muxrpc.Method{"room", "join"}, inviteCode)
	if err != nil {
		t.Fatalf("room.join failed: %v", err)
	}

	t.Logf("join response: %+v", joined)
}
