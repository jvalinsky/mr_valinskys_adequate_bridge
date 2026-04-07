package room

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomstate"
)

type RoomMetrics interface {
	OnTunnelAnnounceFailure()
	OnTunnelConnect()
	OnTunnelAnnounce(feed string, memberCheckMs, snapshotWriteMs int64, hookFailed bool)
}

type TunnelHandler struct {
	server       *RoomServer
	announceHook func(refs.FeedRef) error
	snapshots    roomdb.RuntimeSnapshotsService
	keyPair      *keys.KeyPair
	appKey       string
	metrics      RoomMetrics
}

func NewTunnelHandler(s *RoomServer, keyPair *keys.KeyPair, appKey string) *TunnelHandler {
	return &TunnelHandler{
		server:  s,
		keyPair: keyPair,
		appKey:  appKey,
	}
}

func (h *TunnelHandler) SetRuntimeSnapshots(snapshots roomdb.RuntimeSnapshotsService) {
	h.snapshots = snapshots
}

func (h *TunnelHandler) SetMetrics(m RoomMetrics) {
	h.metrics = m
}

func (h *TunnelHandler) SetAnnounceHook(hook func(refs.FeedRef) error) {
	h.announceHook = hook
}

func (h *TunnelHandler) Handled(m muxrpc.Method) bool {
	return len(m) >= 1 && m[0] == "tunnel"
}

func (h *TunnelHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	if len(req.Method) < 2 {
		req.CloseWithError(fmt.Errorf("tunnel: missing submethod"))
		return
	}

	switch req.Method[1] {
	case "announce":
		h.handleAnnounce(ctx, req)
	case "leave":
		h.handleLeave(ctx, req)
	case "connect":
		h.handleConnect(ctx, req)
	case "endpoints":
		h.handleEndpoints(ctx, req)
	case "isRoom":
		h.handleIsRoom(ctx, req)
	case "ping":
		h.handlePing(ctx, req)
	default:
		req.CloseWithError(fmt.Errorf("tunnel: unknown method %s", req.Method[1]))
	}
}

func (h *TunnelHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}

func (h *TunnelHandler) handleAnnounce(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "sync" {
		req.CloseWithError(fmt.Errorf("tunnel.announce is sync"))
		return
	}

	feedRef, err := AuthenticatedFeedFromAddr(req.RemoteAddr())
	if err != nil {
		req.CloseWithError(fmt.Errorf("tunnel.announce: get caller: %w", err))
		return
	}

	var hookFailed bool

	memberCheckStart := time.Now()
	isMember := isInternalMember(h.server, ctx, feedRef)
	memberCheckMs := time.Since(memberCheckStart).Milliseconds()
	if !isMember {
		req.CloseWithError(fmt.Errorf("tunnel.announce: membership required"))
		return
	}

	if h.server.denied.HasFeed(ctx, feedRef) {
		req.CloseWithError(fmt.Errorf("tunnel.announce: denied"))
		return
	}

	addr := tunnelAddress(*h.server.keyPair, feedRef)
	h.server.state.AddPeer(feedRef, addr)

	snapshotWriteStart := time.Now()
	if h.snapshots != nil {
		_ = h.snapshots.UpsertTunnelEndpoint(context.Background(), feedRef, addr, time.Now().Unix())
	}
	snapshotWriteMs := time.Since(snapshotWriteStart).Milliseconds()

	if h.announceHook != nil {
		if err := h.announceHook(feedRef); err != nil {
			hookFailed = true
			if h.metrics != nil {
				h.metrics.OnTunnelAnnounceFailure()
			}
			req.CloseWithError(fmt.Errorf("tunnel.announce: hook: %w", err))
			return
		}
	}

	if h.metrics != nil {
		h.metrics.OnTunnelAnnounce(feedRef.String(), memberCheckMs, snapshotWriteMs, hookFailed)
	}

	req.Return(ctx, true)
}

func (h *TunnelHandler) handleLeave(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "sync" {
		req.CloseWithError(fmt.Errorf("tunnel.leave is sync"))
		return
	}

	feedRef, err := AuthenticatedFeedFromAddr(req.RemoteAddr())
	if err != nil {
		req.CloseWithError(fmt.Errorf("tunnel.leave: get caller: %w", err))
		return
	}
	if !isInternalMember(h.server, ctx, feedRef) {
		req.CloseWithError(fmt.Errorf("tunnel.leave: membership required"))
		return
	}

	h.server.state.RemovePeer(feedRef)
	if h.snapshots != nil {
		_ = h.snapshots.DeactivateTunnelEndpoint(context.Background(), feedRef)
	}

	req.Return(ctx, true)
}

func (h *TunnelHandler) handleConnect(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "duplex" {
		req.CloseWithError(fmt.Errorf("tunnel.connect is duplex"))
		return
	}

	var args struct {
		Origin refs.FeedRef `json:"origin"`
		Portal refs.FeedRef `json:"portal"`
		Target refs.FeedRef `json:"target"`
	}
	if err := parseSingleObjectArg(req.RawArgs, &args); err != nil {
		req.CloseWithError(fmt.Errorf("tunnel.connect: parse args: %w", err))
		return
	}

	if args.Target == (refs.FeedRef{}) {
		req.CloseWithError(fmt.Errorf("tunnel.connect: target required"))
		return
	}
	if args.Origin != (refs.FeedRef{}) {
		req.CloseWithError(fmt.Errorf("tunnel.connect: forwarded target handling is not available in the room server"))
		return
	}

	origin, err := AuthenticatedFeedFromAddr(req.RemoteAddr())
	if err != nil {
		req.CloseWithError(fmt.Errorf("tunnel.connect: get caller: %w", err))
		return
	}
	if args.Portal != (refs.FeedRef{}) && !args.Portal.Equal(*h.server.keyPair) {
		req.CloseWithError(fmt.Errorf("tunnel.connect: portal mismatch"))
		return
	}
	if !h.server.state.HasPeer(args.Target) {
		req.CloseWithError(fmt.Errorf("tunnel.connect: target not announced"))
		return
	}

	callerSink, err := req.ResponseSink()
	if err != nil {
		req.CloseWithError(fmt.Errorf("tunnel.connect: get caller sink: %w", err))
		return
	}

	callerSource := req.Source()
	if callerSource == nil {
		callerSink.Close()
		req.CloseWithError(fmt.Errorf("tunnel.connect: get caller source"))
		return
	}

	targetEndpoint := h.server.GetPeerMuxRPC(args.Target)
	if targetEndpoint == nil {
		callerSink.Close()
		callerSource.Cancel(fmt.Errorf("tunnel: target unavailable"))
		req.CloseWithError(fmt.Errorf("tunnel.connect: target unavailable"))
		return
	}

	if h.metrics != nil {
		h.metrics.OnTunnelConnect()
	}

	targetSource, targetSink, err := targetEndpoint.Duplex(ctx, muxrpc.TypeBinary, muxrpc.Method{"tunnel", "connect"}, map[string]interface{}{
		"origin": origin,
		"portal": *h.server.keyPair,
		"target": args.Target,
	})
	if err != nil {
		callerSink.Close()
		callerSource.Cancel(fmt.Errorf("tunnel: target connect failed"))
		req.CloseWithError(fmt.Errorf("tunnel.connect: target connect failed: %w", err))
		return
	}

	var wg sync.WaitGroup
	cleanupOnce := sync.Once{}

	cleanup := func() {
		cleanupOnce.Do(func() {
			callerSource.Cancel(fmt.Errorf("tunnel: closing"))
			targetSource.Cancel(fmt.Errorf("tunnel: closing"))
			_ = callerSink.Close()
			_ = targetSink.Close()
		})
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		h.copySourceToSink(ctx, callerSource, targetSink, cleanup)
		cleanup()
	}()
	go func() {
		defer wg.Done()
		h.copySourceToSink(ctx, targetSource, callerSink, cleanup)
		cleanup()
	}()

	wg.Wait()
	_ = req.Close()
}

func (h *TunnelHandler) handleEndpoints(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "source" {
		req.CloseWithError(fmt.Errorf("tunnel.endpoints is a source handler"))
		return
	}

	peers, events, cancel := h.server.state.SubscribeEndpoints()
	go h.streamEndpoints(ctx, req, peers, events, cancel)
}

func (h *TunnelHandler) streamEndpoints(ctx context.Context, req *muxrpc.Request, peers []roomstate.PeerInfo, events <-chan roomstate.TunnelEvent, cancel func()) {
	sink, err := req.ResponseSink()
	if err != nil {
		req.CloseWithError(fmt.Errorf("tunnel.endpoints: get sink: %w", err))
		return
	}
	defer sink.Close()
	defer cancel()

	for _, p := range peers {
		select {
		case <-ctx.Done():
			return
		default:
		}

		data, _ := json.Marshal(map[string]interface{}{
			"type": "joined",
			"id":   p.ID.String(),
			"addr": p.Addr,
		})
		if _, err := sink.Write(data); err != nil {
			return
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			payload, _ := json.Marshal(map[string]interface{}{
				"type": evt.Type,
				"id":   evt.Info.ID.String(),
				"addr": evt.Info.Addr,
			})
			if _, err := sink.Write(payload); err != nil {
				return
			}
		}
	}
}

func (h *TunnelHandler) copySourceToSink(ctx context.Context, source *muxrpc.ByteSource, sink *muxrpc.ByteSink, onClose func()) {
	defer onClose()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if !source.Next(ctx) {
			return
		}
		data, err := source.Bytes()
		if err != nil {
			return
		}
		if len(data) == 0 {
			continue
		}
		if _, err := sink.Write(data); err != nil {
			return
		}
	}
}

func tunnelAddress(portal refs.FeedRef, target refs.FeedRef) string {
	return fmt.Sprintf(
		"tunnel:%s:%s~shs:%s",
		portal.String(),
		target.String(),
		base64.StdEncoding.EncodeToString(target.PubKey()),
	)
}

func (h *TunnelHandler) handleIsRoom(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "async" {
		req.CloseWithError(fmt.Errorf("tunnel.isRoom is async"))
		return
	}

	// Room2 spec: tunnel.isRoom returns true if this is a room server.
	// Clients may call with empty args or with {id: <feedref>}.
	var args struct {
		ID string `json:"id"`
	}
	if len(req.RawArgs) > 0 {
		// Try to parse as object; ignore errors for empty arrays like []
		_ = json.Unmarshal(req.RawArgs, &args)
	}

	// If an ID was provided, check it matches; otherwise just confirm we're a room
	if args.ID != "" {
		req.Return(ctx, args.ID == h.server.keyPair.String())
		return
	}
	req.Return(ctx, true)
}

func (h *TunnelHandler) handlePing(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "sync" {
		req.CloseWithError(fmt.Errorf("tunnel.ping is sync"))
		return
	}

	req.Return(ctx, true)
}
