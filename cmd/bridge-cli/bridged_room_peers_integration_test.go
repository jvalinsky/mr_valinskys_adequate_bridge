package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	legacyhandlers "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/handlers"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bots"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/room"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/network"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssbruntime"
)

func TestBridgedRoomPeerManagerIntegrationActivePeersAndTunnelTargets(t *testing.T) {
	if os.Getenv("MVAB_BRIDGED_ROOM_INTEGRATION") != "1" {
		t.Skip("set MVAB_BRIDGED_ROOM_INTEGRATION=1 to run bridged room peer integration test")
	}

	const (
		seed   = "integration-bridged-room-peers-seed"
		didA   = "did:plc:integration-a"
		didB   = "did:plc:integration-b"
		appKey = ""
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "bridge.sqlite")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	botManager := bots.NewManager([]byte(seed), nil, nil, nil)
	feedA, err := botManager.GetFeedID(didA)
	if err != nil {
		t.Fatalf("derive feedA: %v", err)
	}
	feedB, err := botManager.GetFeedID(didB)
	if err != nil {
		t.Fatalf("derive feedB: %v", err)
	}

	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{ATDID: didA, SSBFeedID: feedA.String(), Active: true}); err != nil {
		t.Fatalf("add account A: %v", err)
	}
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{ATDID: didB, SSBFeedID: feedB.String(), Active: true}); err != nil {
		t.Fatalf("add account B: %v", err)
	}

	logger := log.New(io.Discard, "", 0)
	ssbRT, err := ssbruntime.Open(ctx, ssbruntime.Config{
		RepoPath:   filepath.Join(tempDir, "ssb-repo"),
		ListenAddr: "127.0.0.1:0",
		MasterSeed: []byte(seed),
		AppKey:     appKey,
		GossipDB:   database,
	}, logger)
	if err != nil {
		t.Fatalf("open ssb runtime: %v", err)
	}
	defer ssbRT.Close()

	roomRT, err := room.Start(ctx, room.Config{
		ListenAddr:            "127.0.0.1:0",
		HTTPListenAddr:        "127.0.0.1:0",
		RepoPath:              filepath.Join(tempDir, "room-repo"),
		Mode:                  "open",
		AppKey:                appKey,
		BridgeAccountLister:   database,
		BridgeAccountDetailer: database,
		HandlerMux:            ssbRT.Node().HandlerMux(),
	}, logger)
	if err != nil {
		t.Fatalf("start room runtime: %v", err)
	}
	defer roomRT.Close()

	manager, err := newBridgedRoomPeerManager(bridgedRoomPeerManagerConfig{
		AccountLister: database,
		RoomRuntime:   roomRT,
		Store:         ssbRT.Node().Store(),
		BotSeed:       seed,
		AppKey:        appKey,
		SyncInterval:  200 * time.Millisecond,
	}, logger)
	if err != nil {
		t.Fatalf("new bridged manager: %v", err)
	}
	manager.Start(ctx)
	defer manager.Stop()
	if err := manager.Reconcile(ctx); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	requireEventually(t, 12*time.Second, func() bool {
		attendants, err := fetchActiveAttendants(ctx, roomRT.HTTPAddr())
		if err != nil {
			return false
		}
		tunnels, err := fetchActiveTunnelTargets(ctx, roomRT.HTTPAddr())
		if err != nil {
			return false
		}
		return attendants[feedA.String()] && attendants[feedB.String()] && tunnels[feedA.String()] && tunnels[feedB.String()]
	}, "both active bridged DIDs to appear in attendants and tunnels")

	if err := tunnelConnectProbe(ctx, roomRT.Addr(), roomRT.RoomFeed(), feedA, appKey); err != nil {
		t.Fatalf("tunnel.connect probe for feedA failed: %v", err)
	}
	if err := tunnelConnectProbe(ctx, roomRT.Addr(), roomRT.RoomFeed(), feedB, appKey); err != nil {
		t.Fatalf("tunnel.connect probe for feedB failed: %v", err)
	}
}

func fetchActiveAttendants(ctx context.Context, roomHTTPAddr string) (map[string]bool, error) {
	body, err := getRoomStatusBody(ctx, roomHTTPAddr, "/api/room/status/attendants")
	if err != nil {
		return nil, err
	}
	var payload struct {
		Attendants []struct {
			ID string `json:"id"`
		} `json:"attendants"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode attendants: %w", err)
	}
	out := make(map[string]bool, len(payload.Attendants))
	for _, item := range payload.Attendants {
		out[item.ID] = true
	}
	return out, nil
}

func fetchActiveTunnelTargets(ctx context.Context, roomHTTPAddr string) (map[string]bool, error) {
	body, err := getRoomStatusBody(ctx, roomHTTPAddr, "/api/room/status/tunnels")
	if err != nil {
		return nil, err
	}
	var payload struct {
		Tunnels []struct {
			Target string `json:"target"`
		} `json:"tunnels"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode tunnels: %w", err)
	}
	out := make(map[string]bool, len(payload.Tunnels))
	for _, item := range payload.Tunnels {
		out[item.Target] = true
	}
	return out, nil
}

func getRoomStatusBody(ctx context.Context, roomHTTPAddr, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+roomHTTPAddr+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(payload))
	}
	return io.ReadAll(resp.Body)
}

func tunnelConnectProbe(ctx context.Context, roomAddr string, roomFeed, target refs.FeedRef, appKey string) error {
	kp, err := keys.Generate()
	if err != nil {
		return fmt.Errorf("generate probe key: %w", err)
	}

	handler := &muxrpc.HandlerMux{}
	handler.Register(muxrpc.Method{"whoami"}, legacyhandlers.NewWhoamiHandler(kp))
	handler.Register(muxrpc.Method{"gossip", "ping"}, legacyhandlers.NewPingHandler())

	client := network.NewClient(network.Options{
		KeyPair: kp,
		AppKey:  appKey,
	})

	dialCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	peer, err := client.Connect(dialCtx, roomAddr, roomFeed.PubKey(), handler)
	if err != nil {
		return fmt.Errorf("connect room: %w", err)
	}
	defer peer.Conn.Close()

	rpc := peer.RPC()
	if rpc == nil {
		return fmt.Errorf("nil rpc endpoint")
	}

	source, sink, err := rpc.Duplex(dialCtx, muxrpc.TypeBinary, muxrpc.Method{"tunnel", "connect"}, map[string]interface{}{
		"portal": roomFeed,
		"target": target,
	})
	if err != nil {
		return fmt.Errorf("tunnel.connect: %w", err)
	}
	_ = sink.Close()
	source.Cancel(nil)

	return nil
}
