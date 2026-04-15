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

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	legacyhandlers "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/handlers"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bots"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/room"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/network"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream"
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

	seedIntegrationFeed(t, ssbRT.Node().Store(), feedA, "integration feed A")
	seedIntegrationFeed(t, ssbRT.Node().Store(), feedB, "integration feed B")

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

	if err := tunnelConnectHistoryProbe(ctx, roomRT.Addr(), roomRT.RoomFeed(), feedA, appKey); err != nil {
		t.Fatalf("tunnel.connect history probe for feedA failed: %v", err)
	}
	if err := tunnelConnectHistoryProbe(ctx, roomRT.Addr(), roomRT.RoomFeed(), feedB, appKey); err != nil {
		t.Fatalf("tunnel.connect history probe for feedB failed: %v", err)
	}
}

func TestRoomMemberIngestViaRoomTunnelInnerSHS(t *testing.T) {
	const appKey = ""

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tempDir := t.TempDir()
	database, err := db.Open(filepath.Join(tempDir, "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	logger := log.New(io.Discard, "", 0)
	ssbRT, err := ssbruntime.Open(ctx, ssbruntime.Config{
		RepoPath:   filepath.Join(tempDir, "bridge-ssb-repo"),
		ListenAddr: "127.0.0.1:0",
		MasterSeed: []byte("room-member-ingest-seed"),
		AppKey:     appKey,
		GossipDB:   database,
	}, logger)
	if err != nil {
		t.Fatalf("open bridge ssb runtime: %v", err)
	}
	defer ssbRT.Close()

	roomRT, err := room.Start(ctx, room.Config{
		ListenAddr:            "127.0.0.1:0",
		HTTPListenAddr:        "127.0.0.1:0",
		RepoPath:              filepath.Join(tempDir, "room-repo"),
		Mode:                  "community",
		AppKey:                appKey,
		BridgeAccountLister:   database,
		BridgeAccountDetailer: database,
		HandlerMux:            ssbRT.Node().HandlerMux(),
	}, logger)
	if err != nil {
		t.Fatalf("start room runtime: %v", err)
	}
	defer roomRT.Close()

	targetStore, err := feedlog.NewStore(feedlog.Config{
		DBPath:     filepath.Join(tempDir, "target-feed.sqlite"),
		RepoPath:   filepath.Join(tempDir, "target-repo"),
		BlobSubdir: "blobs",
	})
	if err != nil {
		t.Fatalf("open target feed store: %v", err)
	}
	defer targetStore.Close()

	targetKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate target key: %v", err)
	}
	targetFeed := targetKey.FeedRef()
	seedIntegrationFeed(t, targetStore, targetFeed, "room member ingest target")
	if err := roomRT.AddMember(ctx, targetFeed, roomdb.RoleMember); err != nil {
		t.Fatalf("add target member: %v", err)
	}

	manager, err := newRoomMemberIngestManager(roomMemberIngestManagerConfig{
		AccountLister: database,
		RoomRuntime:   roomRT,
		Sbot:          ssbRT.Node(),
		ReceiveLog:    ssbRT.ReceiveLog(),
		Store:         ssbRT.Node().Store(),
		AppKey:        appKey,
	}, logger)
	if err != nil {
		t.Fatalf("new room member ingest manager: %v", err)
	}
	manager.Start(ctx)
	defer manager.Stop()
	roomRT.SetAnnounceHook(func(feed refs.FeedRef) error {
		return manager.Announce(feed)
	})

	targetHandler := newBridgedPeerSessionHandler(targetFeed, targetKey, secretstream.NewAppKey(appKey), targetStore, logger)
	targetClient := network.NewClient(network.Options{KeyPair: targetKey, AppKey: appKey})
	targetPeer, err := targetClient.Connect(ctx, roomRT.Addr(), roomRT.RoomFeed().PubKey(), targetHandler)
	if err != nil {
		t.Fatalf("connect target peer to room: %v", err)
	}
	defer targetPeer.Conn.Close()
	if err := announceRoomPeer(ctx, targetPeer.RPC()); err != nil {
		t.Fatalf("announce target peer: %v", err)
	}

	requireEventually(t, 12*time.Second, func() bool {
		log, err := ssbRT.Node().Store().Logs().Get(targetFeed.String())
		if err != nil {
			return false
		}
		seq, err := log.Seq()
		return err == nil && seq >= 1
	}, "room member ingest to append target feed through room tunnel")
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

func seedIntegrationFeed(t *testing.T, store *feedlog.StoreImpl, feed refs.FeedRef, text string) {
	t.Helper()
	log, err := store.Logs().Create(feed.String())
	if err != nil {
		t.Fatalf("create feed log %s: %v", feed.String(), err)
	}
	sig := make([]byte, 64)
	for i := range sig {
		sig[i] = byte(i + 1)
	}
	_, err = log.Append([]byte(fmt.Sprintf(`{"type":"post","text":%q}`, text)), &feedlog.Metadata{
		Author:    feed.String(),
		Sequence:  1,
		Timestamp: time.Now().UTC().UnixMilli(),
		Sig:       sig,
		Hash:      "%integration-" + text + ".sha256",
	})
	if err != nil {
		t.Fatalf("append feed log %s: %v", feed.String(), err)
	}
}

func tunnelConnectHistoryProbe(ctx context.Context, roomAddr string, roomFeed, target refs.FeedRef, appKey string) error {
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
	defer source.Cancel(nil)
	defer sink.Close()

	streamConn := muxrpc.NewByteStreamConn(dialCtx, source, sink, peer.Conn.RemoteAddr())
	shsClient, err := secretstream.NewClient(streamConn, secretstream.NewAppKey(appKey), kp.Private(), target.PubKey())
	if err != nil {
		return fmt.Errorf("inner SHS init: %w", err)
	}
	if err := shsClient.Handshake(); err != nil {
		return fmt.Errorf("inner SHS handshake: %w", err)
	}
	defer shsClient.Close()

	innerRPC := muxrpc.NewServer(dialCtx, shsClient, nil, nil)
	defer innerRPC.Terminate()

	old := true
	keys := true
	historySource, err := innerRPC.Source(dialCtx, muxrpc.TypeJSON, muxrpc.Method{"createHistoryStream"}, map[string]interface{}{
		"id":       target.String(),
		"sequence": 0,
		"old":      old,
		"keys":     keys,
		"live":     false,
		"limit":    1,
	})
	if err != nil {
		return fmt.Errorf("createHistoryStream: %w", err)
	}
	defer historySource.Cancel(nil)

	if !historySource.Next(dialCtx) {
		if err := historySource.Err(); err != nil {
			return fmt.Errorf("history source: %w", err)
		}
		return fmt.Errorf("history source returned no frames")
	}
	payload, err := historySource.Bytes()
	if err != nil {
		return fmt.Errorf("history frame bytes: %w", err)
	}
	var envelope struct {
		Key   string `json:"key"`
		Value struct {
			Author    string `json:"author"`
			Sequence  int64  `json:"sequence"`
			Signature string `json:"signature"`
		} `json:"value"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("decode history envelope: %w", err)
	}
	if envelope.Key == "" || envelope.Value.Author != target.String() || envelope.Value.Sequence != 1 || envelope.Value.Signature == "" {
		return fmt.Errorf("invalid history envelope: %+v", envelope)
	}
	return nil
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
