package room

// Tests for diagnosing why mobile SSB clients don't see bridge bots.
//
// These tests cover 6 theories identified during investigation:
//
//   Theory 1: room.attendants membership gate blocks non-member clients
//   Theory 2: Missing inner SHS inside tunnel.connect duplex stream
//   Theory 3: Bridge bot timing gap during 30s reconnect cycle
//   Theory 4: App key mismatch at SHS (documented via manual test)
//   Theory 5: Silent attendant event drop when broadcast channel full
//   Theory 6: PeerRegistry vs state.peers inconsistency on tunnel.connect

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	roomhandlers "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/handlers/room"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream"
)

// connectAsClient performs SHS and returns an authenticated muxrpc.Server.
func connectAsClientWithHandler(t *testing.T, ctx context.Context, addr string, clientKey *keys.KeyPair, roomPubKey []byte, handler muxrpc.Handler) (net.Conn, *muxrpc.Server) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial room: %v", err)
	}
	appKey := secretstream.NewAppKey("boxstream")
	shs, err := secretstream.NewClient(conn, appKey, clientKey.Private(), roomPubKey)
	if err != nil {
		conn.Close()
		t.Fatalf("shs client init: %v", err)
	}
	if err := shs.Handshake(); err != nil {
		conn.Close()
		t.Fatalf("shs handshake: %v", err)
	}
	rpc := muxrpc.NewServer(ctx, shs, handler, nil)
	return conn, rpc
}

// The conn is NOT closed by this helper.
func connectAsClient(t *testing.T, ctx context.Context, addr string, clientKey *keys.KeyPair, roomPubKey []byte) (net.Conn, *muxrpc.Server) {
	t.Helper()
	return connectAsClientWithHandler(t, ctx, addr, clientKey, roomPubKey, nil)
}

// startRoom starts a room with the given mode. The caller should defer rt.Close().
func startRoom(t *testing.T, ctx context.Context, mode string) *Runtime {
	t.Helper()
	rt, err := Start(ctx, Config{
		ListenAddr:     "127.0.0.1:0",
		HTTPListenAddr: "127.0.0.1:0",
		RepoPath:       t.TempDir(),
		Mode:           mode,
	}, log.New(io.Discard, "", 0))
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("start room: %v", err)
	}
	return rt
}

// isSSBError returns true and the error message if raw bytes are an SSB MuxRPC
// error packet: {"name":"Error","message":"..."}.
func isSSBError(raw []byte) (bool, string) {
	var e struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		return false, ""
	}
	if e.Name == "Error" {
		return true, e.Message
	}
	return false, ""
}

// readAttendantsState reads the first message from an open room.attendants source
// and decodes the "state" snapshot. Returns the list of IDs and any error.
func readAttendantsState(t *testing.T, ctx context.Context, src *muxrpc.ByteSource) ([]string, error) {
	t.Helper()
	if !src.Next(ctx) {
		if err := src.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("source closed before first message")
	}
	raw, err := src.Bytes()
	if err != nil {
		return nil, err
	}
	// SSB MuxRPC delivers protocol errors as data packets, not Go errors.
	if isErr, msg := isSSBError(raw); isErr {
		return nil, fmt.Errorf("room.attendants error: %s", msg)
	}
	var msg struct {
		Type string   `json:"type"`
		IDs  []string `json:"ids"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, fmt.Errorf("parse attendants state: %v (raw: %s)", err, raw)
	}
	if msg.Type != "state" {
		return nil, fmt.Errorf("expected type=state, got %q (raw: %s)", msg.Type, raw)
	}
	return msg.IDs, nil
}

// ---------------------------------------------------------------------------
// Theory 1: room.attendants membership gate
// ---------------------------------------------------------------------------

// TestNonMemberGetsErrorFromRoomAttendants asserts that a peer with no room
// membership receives a protocol error from room.attendants in community mode.
func TestNonMemberGetsErrorFromRoomAttendants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rt := startRoom(t, ctx, "community")
	defer rt.Close()

	clientKey, _ := keys.Generate()
	conn, rpc := connectAsClient(t, ctx, rt.Addr(), clientKey, rt.RoomFeed().PubKey())
	defer conn.Close()

	src, err := rpc.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"room", "attendants"})
	if err != nil {
		t.Fatalf("open source: %v", err)
	}

	// SSB MuxRPC delivers errors as data packets ({"name":"Error","message":"..."}).
	// src.Next() returns true even for error packets; the error lives in the bytes.
	if !src.Next(ctx) {
		// Stream closed without data — check Go-level error.
		if err := src.Err(); err != nil {
			if strings.Contains(err.Error(), "membership required") {
				t.Logf("confirmed: non-member gets membership error (via Go error): %v", err)
				return
			}
			t.Fatalf("unexpected stream error: %v", err)
		}
		t.Fatal("stream closed with no data and no error")
	}
	raw, _ := src.Bytes()
	if isErr, msg := isSSBError(raw); isErr {
		if !strings.Contains(msg, "membership required") {
			t.Errorf("expected 'membership required' error, got: %s", msg)
		}
		t.Logf("confirmed: non-member gets membership error (via SSB error packet): %s", msg)
		return
	}
	// If we get here the response was a successful state message — non-members
	// can call room.attendants. This would be spec-correct but is not our intent.
	var state struct {
		Type string   `json:"type"`
		IDs  []string `json:"ids"`
	}
	_ = json.Unmarshal(raw, &state)
	t.Errorf("expected membership error for non-member, got state response with %d IDs: %v",
		len(state.IDs), state.IDs)
}

// TestRoomAttendantsOpenModeNonMember verifies that open mode allows
// non-members to consume room.attendants discovery state.
func TestRoomAttendantsOpenModeNonMember(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rt := startRoom(t, ctx, "open")
	defer rt.Close()

	clientKey, _ := keys.Generate()
	conn, rpc := connectAsClient(t, ctx, rt.Addr(), clientKey, rt.RoomFeed().PubKey())
	defer conn.Close()

	src, err := rpc.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"room", "attendants"})
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	if !src.Next(ctx) {
		if err := src.Err(); err != nil {
			t.Fatalf("expected open mode attendants state, got source error: %v", err)
		}
		t.Fatal("expected open mode attendants state, stream closed early")
	}
	raw, _ := src.Bytes()
	if isErr, msg := isSSBError(raw); isErr {
		t.Fatalf("expected open mode attendants state, got SSB error packet: %s", msg)
	}

	var state struct {
		Type string   `json:"type"`
		IDs  []string `json:"ids"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode open mode attendants state: %v (raw: %s)", err, raw)
	}
	if state.Type != "state" {
		t.Fatalf("expected type=state in open mode attendants response, got %+v", state)
	}
}

// ---------------------------------------------------------------------------
// Theory 1 continued: member sees bots
// ---------------------------------------------------------------------------

// TestMemberSeesConnectedBridgeBots verifies the happy path: a mobile client
// that IS a room member can call room.attendants and sees bridge bots in the
// initial state snapshot.
func TestMemberSeesConnectedBridgeBots(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rt := startRoom(t, ctx, "community")
	defer rt.Close()

	// Register two "bridge bots" as room members and connect them.
	bot1Key, _ := keys.Generate()
	bot2Key, _ := keys.Generate()
	bot1Feed := bot1Key.FeedRef()
	bot2Feed := bot2Key.FeedRef()

	if err := rt.AddMember(ctx, bot1Feed, roomdb.RoleMember); err != nil {
		t.Fatalf("add bot1 as member: %v", err)
	}
	if err := rt.AddMember(ctx, bot2Feed, roomdb.RoleMember); err != nil {
		t.Fatalf("add bot2 as member: %v", err)
	}

	// Connect both bots. The room server will add them to attendants because
	// they are members.
	bot1Conn, _ := connectAsClient(t, ctx, rt.Addr(), bot1Key, rt.RoomFeed().PubKey())
	defer bot1Conn.Close()
	bot2Conn, _ := connectAsClient(t, ctx, rt.Addr(), bot2Key, rt.RoomFeed().PubKey())
	defer bot2Conn.Close()

	// Give the room a moment to process the connections and call AddAttendant.
	time.Sleep(50 * time.Millisecond)

	// Now connect as a "mobile client" that is also a member.
	mobileKey, _ := keys.Generate()
	mobileFeed := mobileKey.FeedRef()
	if err := rt.AddMember(ctx, mobileFeed, roomdb.RoleMember); err != nil {
		t.Fatalf("add mobile as member: %v", err)
	}
	mobileConn, mobileRPC := connectAsClient(t, ctx, rt.Addr(), mobileKey, rt.RoomFeed().PubKey())
	defer mobileConn.Close()

	src, err := mobileRPC.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"room", "attendants"})
	if err != nil {
		t.Fatalf("open room.attendants source: %v", err)
	}

	ids, err := readAttendantsState(t, ctx, src)
	if err != nil {
		t.Fatalf("read attendants state: %v", err)
	}

	t.Logf("attendants state: %v", ids)

	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	if !idSet[bot1Feed.String()] {
		t.Errorf("bot1 (%s) not found in attendants; got: %v", bot1Feed.String(), ids)
	}
	if !idSet[bot2Feed.String()] {
		t.Errorf("bot2 (%s) not found in attendants; got: %v", bot2Feed.String(), ids)
	}
}

// ---------------------------------------------------------------------------
// Theory 3: join event delivery when bot connects after mobile client subscribes
// ---------------------------------------------------------------------------

// TestAttendantsStreamDeliversBotJoinEvent verifies that a mobile client
// receives a "joined" event when a bridge bot connects AFTER the client
// has already subscribed to room.attendants.
func TestAttendantsStreamDeliversBotJoinEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rt := startRoom(t, ctx, "community")
	defer rt.Close()

	botKey, _ := keys.Generate()
	botFeed := botKey.FeedRef()
	if err := rt.AddMember(ctx, botFeed, roomdb.RoleMember); err != nil {
		t.Fatalf("add bot as member: %v", err)
	}

	mobileKey, _ := keys.Generate()
	mobileFeed := mobileKey.FeedRef()
	if err := rt.AddMember(ctx, mobileFeed, roomdb.RoleMember); err != nil {
		t.Fatalf("add mobile as member: %v", err)
	}

	// Mobile client subscribes FIRST, before the bot connects.
	mobileConn, mobileRPC := connectAsClient(t, ctx, rt.Addr(), mobileKey, rt.RoomFeed().PubKey())
	defer mobileConn.Close()

	src, err := mobileRPC.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"room", "attendants"})
	if err != nil {
		t.Fatalf("open room.attendants source: %v", err)
	}

	// Read initial state — should be empty (no bots connected yet) or contain
	// only the mobile client itself.
	ids, err := readAttendantsState(t, ctx, src)
	if err != nil {
		t.Fatalf("read initial attendants state: %v", err)
	}
	t.Logf("initial state: %v", ids)

	// Now connect the bot.
	botConn, _ := connectAsClient(t, ctx, rt.Addr(), botKey, rt.RoomFeed().PubKey())
	defer botConn.Close()

	// Read the next event from the stream — it should be the bot joining.
	joinEventCtx, joinCancel := context.WithTimeout(ctx, 5*time.Second)
	defer joinCancel()

	if !src.Next(joinEventCtx) {
		t.Fatalf("no event received after bot connected: %v", src.Err())
	}
	raw, err := src.Bytes()
	if err != nil {
		t.Fatalf("read event bytes: %v", err)
	}

	var event struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatalf("parse event: %v (raw: %s)", err, raw)
	}
	t.Logf("received event: %+v", event)

	if event.Type != "joined" {
		t.Errorf("expected type=joined, got %q", event.Type)
	}
	if event.ID != botFeed.String() {
		t.Errorf("expected bot feed %s, got %s", botFeed.String(), event.ID)
	}
}

// ---------------------------------------------------------------------------
// Theory 3: bot disconnect → reconnect produces leave + join events
// ---------------------------------------------------------------------------

// TestBotDisconnectReconnectProducesEvents ensures that when a bridge bot
// disconnects and reconnects, the mobile client receives exactly one "left"
// followed by one "joined" event. A missing "joined" event after reconnect
// would cause the mobile client to lose sight of the bot permanently.
func TestBotDisconnectReconnectProducesEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	rt := startRoom(t, ctx, "community")
	defer rt.Close()

	botKey, _ := keys.Generate()
	botFeed := botKey.FeedRef()
	if err := rt.AddMember(ctx, botFeed, roomdb.RoleMember); err != nil {
		t.Fatalf("add bot as member: %v", err)
	}
	mobileKey, _ := keys.Generate()
	mobileFeed := mobileKey.FeedRef()
	if err := rt.AddMember(ctx, mobileFeed, roomdb.RoleMember); err != nil {
		t.Fatalf("add mobile as member: %v", err)
	}

	// Bot connects first.
	botConn, _ := connectAsClient(t, ctx, rt.Addr(), botKey, rt.RoomFeed().PubKey())

	time.Sleep(30 * time.Millisecond)

	// Mobile subscribes and reads initial state containing the bot.
	mobileConn, mobileRPC := connectAsClient(t, ctx, rt.Addr(), mobileKey, rt.RoomFeed().PubKey())
	defer mobileConn.Close()

	src, err := mobileRPC.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"room", "attendants"})
	if err != nil {
		t.Fatalf("open room.attendants source: %v", err)
	}

	ids, err := readAttendantsState(t, ctx, src)
	if err != nil {
		t.Fatalf("read initial state: %v", err)
	}
	t.Logf("initial state: %v", ids)

	// Disconnect the bot.
	botConn.Close()

	// Expect "left" event.
	readEventCtx, readCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readCancel()

	if !src.Next(readEventCtx) {
		t.Fatalf("no left event received: %v", src.Err())
	}
	raw, _ := src.Bytes()
	var leftEvent struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(raw, &leftEvent); err != nil {
		t.Fatalf("parse left event: %v (raw: %s)", err, raw)
	}
	t.Logf("left event: %+v", leftEvent)
	if leftEvent.Type != "left" {
		t.Errorf("expected type=left, got %q", leftEvent.Type)
	}
	if leftEvent.ID != botFeed.String() {
		t.Errorf("expected bot feed in left event, got %s", leftEvent.ID)
	}

	// Reconnect the bot.
	botConn2, _ := connectAsClient(t, ctx, rt.Addr(), botKey, rt.RoomFeed().PubKey())
	defer botConn2.Close()

	// Expect "joined" event.
	joinCtx, joinCancel2 := context.WithTimeout(ctx, 5*time.Second)
	defer joinCancel2()

	if !src.Next(joinCtx) {
		t.Fatalf("no joined event after bot reconnected: %v", src.Err())
	}
	raw, _ = src.Bytes()
	var joinEvent struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(raw, &joinEvent); err != nil {
		t.Fatalf("parse join event: %v (raw: %s)", err, raw)
	}
	t.Logf("join event after reconnect: %+v", joinEvent)
	if joinEvent.Type != "joined" {
		t.Errorf("expected type=joined after reconnect, got %q", joinEvent.Type)
	}
	if joinEvent.ID != botFeed.String() {
		t.Errorf("expected bot feed in joined event, got %s", joinEvent.ID)
	}
}

// ---------------------------------------------------------------------------
// Theory 5: silent event drop when attendant channel overflows
// ---------------------------------------------------------------------------

// TestAttendantsChannelDoesNotDropEventsUnderBurst connects more than 16 bots
// in rapid succession and verifies all join events reach the subscriber.
// The broadcast channel has a buffer of 16; events are dropped silently on
// overflow. See internal/ssb/roomstate/roomstate.go:215.
func TestAttendantsChannelDoesNotDropEventsUnderBurst(t *testing.T) {
	const botCount = 20 // intentionally exceeds channel buffer of 16

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rt := startRoom(t, ctx, "community")
	defer rt.Close()

	// Register all bots as members before any of them connect.
	botKeys := make([]*keys.KeyPair, botCount)
	for i := range botKeys {
		kp, _ := keys.Generate()
		botKeys[i] = kp
		if err := rt.AddMember(ctx, kp.FeedRef(), roomdb.RoleMember); err != nil {
			t.Fatalf("add bot%d as member: %v", i, err)
		}
	}

	mobileKey, _ := keys.Generate()
	mobileFeed := mobileKey.FeedRef()
	if err := rt.AddMember(ctx, mobileFeed, roomdb.RoleMember); err != nil {
		t.Fatalf("add mobile as member: %v", err)
	}

	// Mobile subscribes before any bot connects.
	mobileConn, mobileRPC := connectAsClient(t, ctx, rt.Addr(), mobileKey, rt.RoomFeed().PubKey())
	defer mobileConn.Close()

	src, err := mobileRPC.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"room", "attendants"})
	if err != nil {
		t.Fatalf("open room.attendants source: %v", err)
	}

	initialIDs, err := readAttendantsState(t, ctx, src)
	if err != nil {
		t.Fatalf("read initial state: %v", err)
	}
	t.Logf("initial state (expect empty): %v", initialIDs)

	// Connect all bots simultaneously to maximize burst pressure.
	var wg sync.WaitGroup
	botConns := make([]net.Conn, botCount)
	for i, kp := range botKeys {
		wg.Add(1)
		go func(idx int, k *keys.KeyPair) {
			defer wg.Done()
			conn, _ := connectAsClient(t, ctx, rt.Addr(), k, rt.RoomFeed().PubKey())
			botConns[idx] = conn
		}(i, kp)
	}
	wg.Wait()
	defer func() {
		for _, c := range botConns {
			if c != nil {
				c.Close()
			}
		}
	}()

	// Collect events for up to 3 seconds.
	collectCtx, collectCancel := context.WithTimeout(ctx, 3*time.Second)
	defer collectCancel()

	joinedIDs := make(map[string]int)
	for {
		if !src.Next(collectCtx) {
			break
		}
		raw, err := src.Bytes()
		if err != nil {
			break
		}
		var event struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			continue
		}
		if event.Type == "joined" {
			joinedIDs[event.ID]++
		}
	}

	t.Logf("received joined events: %d unique IDs out of %d bots", len(joinedIDs), botCount)

	// Check that every bot's join event was received.
	var dropped []string
	for _, kp := range botKeys {
		feed := kp.FeedRef().String()
		if joinedIDs[feed] == 0 {
			dropped = append(dropped, feed[:20]+"…")
		}
	}
	if len(dropped) > 0 {
		t.Errorf("dropped %d/%d join events (channel buffer overflow at 16): %v",
			len(dropped), botCount, dropped)
	}
}

// ---------------------------------------------------------------------------
// Theory 6: tunnel.endpoints is accessible without membership
// ---------------------------------------------------------------------------

// TestTunnelEndpointsAccessibleWithoutMembership asserts that tunnel.endpoints
// can be called by any connected peer regardless of membership. This is the
// fallback discovery path that mobile clients can use when room.attendants
// fails due to the membership gate.
func TestTunnelEndpointsAccessibleWithoutMembership(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rt := startRoom(t, ctx, "community")
	defer rt.Close()

	// Register and connect a bot so it appears in tunnel.endpoints.
	botKey, _ := keys.Generate()
	botFeed := botKey.FeedRef()
	if err := rt.AddMember(ctx, botFeed, roomdb.RoleMember); err != nil {
		t.Fatalf("add bot as member: %v", err)
	}
	botConn, botRPC := connectAsClient(t, ctx, rt.Addr(), botKey, rt.RoomFeed().PubKey())
	defer botConn.Close()

	// Bot calls tunnel.announce to register in the tunnel endpoints list.
	var announced bool
	announceCtx, announceCancel := context.WithTimeout(ctx, 5*time.Second)
	defer announceCancel()
	if err := botRPC.Sync(announceCtx, &announced, muxrpc.TypeJSON, muxrpc.Method{"tunnel", "announce"}); err != nil {
		t.Fatalf("tunnel.announce: %v", err)
	}
	if !announced {
		t.Fatal("tunnel.announce returned false")
	}

	// Connect as a non-member.
	nonMemberKey, _ := keys.Generate()
	nonMemberConn, nonMemberRPC := connectAsClient(t, ctx, rt.Addr(), nonMemberKey, rt.RoomFeed().PubKey())
	defer nonMemberConn.Close()

	// Non-member calls tunnel.endpoints — should succeed and show the bot.
	src, err := nonMemberRPC.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"tunnel", "endpoints"})
	if err != nil {
		t.Fatalf("open tunnel.endpoints source: %v", err)
	}

	endpointsCtx, epCancel := context.WithTimeout(ctx, 3*time.Second)
	defer epCancel()

	foundBot := false
	for src.Next(endpointsCtx) {
		raw, err := src.Bytes()
		if err != nil {
			break
		}
		var ep struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Addr string `json:"addr"`
		}
		if err := json.Unmarshal(raw, &ep); err != nil {
			continue
		}
		t.Logf("tunnel.endpoints event: %+v", ep)
		if ep.ID == botFeed.String() {
			foundBot = true
			break
		}
	}

	if !foundBot {
		if err := src.Err(); err != nil {
			t.Fatalf("tunnel.endpoints error for non-member: %v (Theory 6 variant)", err)
		}
		t.Errorf("bot not found in tunnel.endpoints for non-member; endpoint may be member-gated or bot not announced")
	}
}

// ---------------------------------------------------------------------------
// Theory 2: tunnel.connect inner protocol — missing inner SHS
// ---------------------------------------------------------------------------

// TestTunnelConnectInnerProtocol verifies that tunnel.connect successfully establishes
// an inner SHS connection when connecting to a bridge bot, as required by the
// Room 2.0 spec. It also verifies that the MuxRPC session layered on top works.
func TestTunnelConnectInnerProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rt := startRoom(t, ctx, "community")
	defer rt.Close()

	// Bot and client are both members.
	botKey, _ := keys.Generate()
	botFeed := botKey.FeedRef()
	if err := rt.AddMember(ctx, botFeed, roomdb.RoleMember); err != nil {
		t.Fatalf("add bot as member: %v", err)
	}
	clientKey, _ := keys.Generate()
	clientFeed := clientKey.FeedRef()
	if err := rt.AddMember(ctx, clientFeed, roomdb.RoleMember); err != nil {
		t.Fatalf("add client as member: %v", err)
	}

	appKey := secretstream.NewAppKey("boxstream")

	// Set up the bot side: it must handle tunnel.connect, performing SHS server handshake.
	hmux := &muxrpc.HandlerMux{}
	tunnelHandler := roomhandlers.NewClientTunnelConnectHandler(botKey, appKey, &dummyInnerHandler{})
	hmux.Register(muxrpc.Method{"tunnel", "connect"}, tunnelHandler)

	botConn, botRPC := connectAsClientWithHandler(t, ctx, rt.Addr(), botKey, rt.RoomFeed().PubKey(), hmux)
	defer botConn.Close()

	var announced bool
	announceCtx, announceCancel := context.WithTimeout(ctx, 5*time.Second)
	defer announceCancel()
	if err := botRPC.Sync(announceCtx, &announced, muxrpc.TypeJSON, muxrpc.Method{"tunnel", "announce"}); err != nil {
		t.Fatalf("tunnel.announce: %v", err)
	}

	time.Sleep(30 * time.Millisecond)

	// Client opens tunnel.connect to the bot.
	clientConn, clientRPC := connectAsClient(t, ctx, rt.Addr(), clientKey, rt.RoomFeed().PubKey())
	defer clientConn.Close()

	tunnelCtx, tunnelCancel := context.WithTimeout(ctx, 5*time.Second)
	defer tunnelCancel()
	source, sink, err := clientRPC.Duplex(tunnelCtx, muxrpc.TypeBinary, muxrpc.Method{"tunnel", "connect"}, map[string]interface{}{
		"portal": rt.RoomFeed().String(),
		"target": botFeed.String(),
	})
	if err != nil {
		t.Fatalf("tunnel.connect duplex: %v", err)
	}
	defer sink.Close()

	// Act as inner SHS client
	streamConn := muxrpc.NewByteStreamConn(tunnelCtx, source, sink, clientConn.RemoteAddr())
	shsClient, err := secretstream.NewClient(streamConn, appKey, clientKey.Private(), botFeed.PubKey())
	if err != nil {
		t.Fatalf("inner SHS client init: %v", err)
	}
	if err := shsClient.Handshake(); err != nil {
		t.Fatalf("inner SHS handshake failed: %v", err)
	}

	// Layer inner MuxRPC over the SHS channel to test end-to-end
	innerRPC := muxrpc.NewServer(tunnelCtx, shsClient, nil, nil)
	defer innerRPC.Terminate()

	// Call our dummy method exposed by the bot's innerMux
	var reply string
	if err := innerRPC.Sync(tunnelCtx, &reply, muxrpc.TypeString, muxrpc.Method{"dummy"}); err != nil {
		t.Fatalf("inner RPC dummy call failed: %v", err)
	}
	if reply != "hello" {
		t.Fatalf("expected dummy reply 'hello', got %q", reply)
	}

	t.Log("Successfully negotiated inner SHS and MuxRPC over tunnel.connect")
}

// dummyInnerHandler just replies to "dummy" with "hello"
type dummyInnerHandler struct{}

func (h *dummyInnerHandler) Handled(m muxrpc.Method) bool { 
	return len(m) == 1 && m[0] == "dummy" 
}
func (h *dummyInnerHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	req.Return(ctx, "hello")
}
func (h *dummyInnerHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}

// TestInboundTunnelConnectFromManyverseSimulatedClient verifies that a simulated
// standard mobile client (which sends SHS bytes immediately onto the raw tunnel)
// successfully completes the handshake against our bot's tunnel handler.
func TestInboundTunnelConnectFromManyverseSimulatedClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rt := startRoom(t, ctx, "community")
	defer rt.Close()

	botKey, _ := keys.Generate()
	botFeed := botKey.FeedRef()
	rt.AddMember(ctx, botFeed, roomdb.RoleMember)

	clientKey, _ := keys.Generate()
	rt.AddMember(ctx, clientKey.FeedRef(), roomdb.RoleMember)

	appKey := secretstream.NewAppKey("boxstream")
	hmux := &muxrpc.HandlerMux{}
	tunnelHandler := roomhandlers.NewClientTunnelConnectHandler(botKey, appKey, &dummyInnerHandler{})
	hmux.Register(muxrpc.Method{"tunnel", "connect"}, tunnelHandler)

	botConn, botRPC := connectAsClientWithHandler(t, ctx, rt.Addr(), botKey, rt.RoomFeed().PubKey(), hmux)
	defer botConn.Close()

	var announced bool
	botRPC.Sync(ctx, &announced, muxrpc.TypeJSON, muxrpc.Method{"tunnel", "announce"})

	time.Sleep(30 * time.Millisecond)

	clientConn, clientRPC := connectAsClient(t, ctx, rt.Addr(), clientKey, rt.RoomFeed().PubKey())
	defer clientConn.Close()

	source, sink, err := clientRPC.Duplex(ctx, muxrpc.TypeBinary, muxrpc.Method{"tunnel", "connect"}, map[string]interface{}{
		"portal": rt.RoomFeed().String(),
		"target": botFeed.String(),
	})
	if err != nil {
		t.Fatalf("tunnel.connect failed: %v", err)
	}

	// Behave like Manyverse:
	streamConn := muxrpc.NewByteStreamConn(ctx, source, sink, clientConn.RemoteAddr())
	shsClient, err := secretstream.NewClient(streamConn, appKey, clientKey.Private(), botFeed.PubKey())
	if err != nil {
		t.Fatalf("simulated manyverse SHS init: %v", err)
	}
	if err := shsClient.Handshake(); err != nil {
		t.Errorf("simulated manyverse SHS handshake failed: %v", err)
	} else {
		t.Log("Successfully negotiated inner SHS matching Manyverse behavior")
	}
	shsClient.Close()
	sink.Close()
}

// ---------------------------------------------------------------------------
// Theory 4: app key mismatch is detectable at SHS
// ---------------------------------------------------------------------------

// TestAppKeyMismatchFailsAtSHS asserts that a client using the wrong app key
// fails at the SHS handshake phase. This ensures the app key diagnostic step
// for Theory 4 is mechanically sound.
func TestAppKeyMismatchFailsAtSHS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rt := startRoom(t, ctx, "open")
	defer rt.Close()

	clientKey, _ := keys.Generate()
	conn, err := net.Dial("tcp", rt.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Use a different app key from the room's "boxstream" default.
	wrongAppKey := secretstream.NewAppKey("wrongkey")
	shs, err := secretstream.NewClient(conn, wrongAppKey, clientKey.Private(), rt.RoomFeed().PubKey())
	if err != nil {
		t.Fatalf("shs client init: %v", err)
	}
	if handshakeErr := shs.Handshake(); handshakeErr == nil {
		t.Error("expected SHS handshake to fail with wrong app key, but it succeeded")
		return
	} else {
		t.Logf("confirmed: mismatched app key fails at SHS: %v", handshakeErr)
	}
}

// ---------------------------------------------------------------------------
// Regression: tunnel.announce must be called as sync, not async
// ---------------------------------------------------------------------------

// TestTunnelAnnounceRequiresSyncType confirms that tunnel.announce is a sync
// call (not async). TestPeerAnnouncesTunnel called it as async and silently
// received an error response without failing.
func TestTunnelAnnounceRequiresSyncType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rt := startRoom(t, ctx, "open")
	defer rt.Close()

	clientKey, _ := keys.Generate()
	if err := rt.AddMember(ctx, clientKey.FeedRef(), roomdb.RoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}
	conn, rpc := connectAsClient(t, ctx, rt.Addr(), clientKey, rt.RoomFeed().PubKey())
	defer conn.Close()

	var result bool
	syncCtx, syncCancel := context.WithTimeout(ctx, 5*time.Second)
	defer syncCancel()
	if err := rpc.Sync(syncCtx, &result, muxrpc.TypeJSON, muxrpc.Method{"tunnel", "announce"}); err != nil {
		t.Fatalf("tunnel.announce as sync: %v", err)
	}
	if !result {
		t.Error("tunnel.announce returned false")
	}
}

// ---------------------------------------------------------------------------
// Helpers used across multiple tests
// ---------------------------------------------------------------------------

// drainSource reads all available messages from a source until context expires.
// Used to consume remaining events without blocking.
func drainSource(ctx context.Context, src *muxrpc.ByteSource) [][]byte {
	var msgs [][]byte
	for src.Next(ctx) {
		if b, err := src.Bytes(); err == nil {
			msgs = append(msgs, bytes.Clone(b))
		}
	}
	return msgs
}

// feedIDIn returns true if target appears in the slice of feed ID strings.
func feedIDIn(target refs.FeedRef, ids []string) bool {
	for _, id := range ids {
		if id == target.String() {
			return true
		}
	}
	return false
}
