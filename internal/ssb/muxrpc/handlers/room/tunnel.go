package room

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomstate"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream"
)

type TunnelHandler struct {
	server       *RoomServer
	announceHook func(refs.FeedRef) error
	keyPair      *keys.KeyPair
	appKey       string
}

func NewTunnelHandler(s *RoomServer, keyPair *keys.KeyPair, appKey string) *TunnelHandler {
	return &TunnelHandler{
		server:  s,
		keyPair: keyPair,
		appKey:  appKey,
	}
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

	var args struct {
		ID   string `json:"id"`
		Addr string `json:"addr"`
	}
	if len(req.RawArgs) > 0 {
		if err := json.Unmarshal(req.RawArgs, &args); err != nil {
			req.CloseWithError(fmt.Errorf("tunnel.announce: parse args: %w", err))
			return
		}
	}

	if args.ID == "" {
		req.CloseWithError(fmt.Errorf("tunnel.announce: id required"))
		return
	}

	feedRef, err := refs.ParseFeedRef(args.ID)
	if err != nil {
		req.CloseWithError(fmt.Errorf("tunnel.announce: parse id: %w", err))
		return
	}

	if h.server.denied.HasFeed(ctx, *feedRef) {
		req.CloseWithError(fmt.Errorf("tunnel.announce: denied"))
		return
	}

	h.server.state.AddPeer(*feedRef, args.Addr)

	if h.announceHook != nil {
		if err := h.announceHook(*feedRef); err != nil {
			req.CloseWithError(fmt.Errorf("tunnel.announce: hook: %w", err))
			return
		}
	}

	req.Return(ctx, true)
}

func (h *TunnelHandler) handleLeave(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "sync" {
		req.CloseWithError(fmt.Errorf("tunnel.leave is sync"))
		return
	}

	var args struct {
		ID string `json:"id"`
	}
	if len(req.RawArgs) > 0 {
		if err := json.Unmarshal(req.RawArgs, &args); err != nil {
			req.CloseWithError(fmt.Errorf("tunnel.leave: parse args: %w", err))
			return
		}
	}

	if args.ID == "" {
		req.CloseWithError(fmt.Errorf("tunnel.leave: id required"))
		return
	}

	feedRef, err := refs.ParseFeedRef(args.ID)
	if err != nil {
		req.CloseWithError(fmt.Errorf("tunnel.leave: parse id: %w", err))
		return
	}

	h.server.state.RemovePeer(*feedRef)

	req.Return(ctx, true)
}

func (h *TunnelHandler) handleConnect(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "duplex" {
		req.CloseWithError(fmt.Errorf("tunnel.connect is duplex"))
		return
	}

	var args struct {
		ID   string `json:"id"`
		Addr string `json:"addr"`
	}
	if len(req.RawArgs) > 0 {
		if err := json.Unmarshal(req.RawArgs, &args); err != nil {
			req.CloseWithError(fmt.Errorf("tunnel.connect: parse args: %w", err))
			return
		}
	}

	if args.ID == "" {
		req.CloseWithError(fmt.Errorf("tunnel.connect: id required"))
		return
	}

	log.Printf("[TUNNEL] handleConnect: id=%s addr=%s", args.ID, args.Addr)

	targetRef, err := refs.ParseFeedRef(args.ID)
	if err != nil {
		req.CloseWithError(fmt.Errorf("tunnel.connect: parse target id: %w", err))
		return
	}

	log.Printf("[TUNNEL] handleConnect: parsed target=%s", targetRef.String())

	log.Printf("[TUNNEL] tunnel.connect from %s to %s (addr: %s)", req.RemoteAddr(), args.ID, args.Addr)

	var targetAddr string
	if args.Addr != "" {
		targetAddr = args.Addr
	} else {
		req.CloseWithError(fmt.Errorf("tunnel.connect: addr required for dialing"))
		return
	}

	go h.dialAndBridge(ctx, req, *targetRef, targetAddr)
}

func (h *TunnelHandler) dialAndBridge(ctx context.Context, req *muxrpc.Request, targetRef refs.FeedRef, targetAddr string) {
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

	log.Printf("[TUNNEL] Dialing target peer at %s", targetAddr)

	conn, err := h.dialPeer(ctx, targetAddr, targetRef)
	if err != nil {
		log.Printf("[TUNNEL] Failed to dial target peer: %v", err)
		callerSink.Close()
		callerSource.Cancel(fmt.Errorf("tunnel: dial failed"))
		req.CloseWithError(fmt.Errorf("tunnel.connect: dial failed: %w", err))
		return
	}

	log.Printf("[TUNNEL] Successfully connected to target, starting bridge")

	var wg sync.WaitGroup
	cleanupOnce := sync.Once{}

	cleanup := func() {
		cleanupOnce.Do(func() {
			callerSource.Cancel(fmt.Errorf("tunnel: closing"))
			callerSink.Close()
			conn.Close()
		})
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		h.copyStreamToConn(ctx, callerSource, conn, cleanup)
		log.Printf("[TUNNEL] Caller -> Target stream closed")
		cleanup()
	}()
	go func() {
		defer wg.Done()
		h.copyConnToStream(ctx, conn, callerSink, cleanup)
		log.Printf("[TUNNEL] Target -> Caller stream closed")
		cleanup()
	}()

	wg.Wait()
	log.Printf("[TUNNEL] Tunnel bridge complete")

	req.Close()
}

func parseMultiServerAddr(addr string) (string, error) {
	if strings.HasPrefix(addr, "net:") {
		addr = strings.TrimPrefix(addr, "net:")
	}
	parts := strings.Split(addr, "~shs:")
	if len(parts) < 1 {
		return "", fmt.Errorf("parse addr: invalid format")
	}
	return parts[0], nil
}

func (h *TunnelHandler) dialPeer(ctx context.Context, addr string, expectedFeed refs.FeedRef) (net.Conn, error) {
	tcpAddr, err := parseMultiServerAddr(addr)
	if err != nil {
		return nil, fmt.Errorf("parse addr: %w", err)
	}

	log.Printf("[TUNNEL] dialPeer: raw=%s parsed=%s expectedFeed=%s", addr, tcpAddr, expectedFeed.String())

	conn, err := net.DialTimeout("tcp", tcpAddr, 10*1000000000)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	shs, err := secretstream.NewClient(conn, secretstream.NewAppKey(h.appKey), h.keyPair.Private(), expectedFeed.PubKey())
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("create client: %w", err)
	}

	if err := shs.Handshake(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("handshake: %w", err)
	}

	return conn, nil
}

func (h *TunnelHandler) copyStreamToConn(ctx context.Context, source *muxrpc.ByteSource, conn net.Conn, onClose func()) {
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
		if len(data) > 0 {
			if _, err := conn.Write(data); err != nil {
				return
			}
		}
	}
}

func (h *TunnelHandler) copyConnToStream(ctx context.Context, conn net.Conn, sink *muxrpc.ByteSink, onClose func()) {
	defer onClose()
	buf := make([]byte, 8192)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, err := conn.Read(buf)
		if n > 0 {
			sink.Write(buf[:n])
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			log.Printf("[TUNNEL] Read error: %v", err)
			return
		}
	}
}

func (h *TunnelHandler) handleEndpoints(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "source" {
		req.CloseWithError(fmt.Errorf("tunnel.endpoints is a source handler"))
		return
	}

	var args struct {
		ID string `json:"id"`
	}
	if len(req.RawArgs) > 0 {
		if err := json.Unmarshal(req.RawArgs, &args); err != nil {
			req.CloseWithError(fmt.Errorf("tunnel.endpoints: parse args: %w", err))
			return
		}
	}

	peers := h.server.state.Peers()

	go h.streamEndpoints(ctx, req, peers)
}

func (h *TunnelHandler) streamEndpoints(ctx context.Context, req *muxrpc.Request, peers []roomstate.PeerInfo) {
	sink, err := req.ResponseSink()
	if err != nil {
		req.CloseWithError(fmt.Errorf("tunnel.endpoints: get sink: %w", err))
		return
	}
	defer sink.Close()

	for _, p := range peers {
		select {
		case <-ctx.Done():
			return
		default:
		}

		data, _ := json.Marshal(map[string]interface{}{
			"id":   p.ID.String(),
			"addr": p.Addr,
		})
		if _, err := sink.Write(data); err != nil {
			return
		}
	}
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
