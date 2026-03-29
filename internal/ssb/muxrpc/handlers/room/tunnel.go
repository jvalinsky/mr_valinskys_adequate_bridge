package room

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomstate"
)

type TunnelHandler struct {
	server       *RoomServer
	announceHook func(refs.FeedRef) error
}

func NewTunnelHandler(s *RoomServer) *TunnelHandler {
	return &TunnelHandler{server: s}
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

	req.Return(ctx, map[string]interface{}{
		"id":   args.ID,
		"addr": args.Addr,
	})
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

	var args struct {
		ID string `json:"id"`
	}
	if len(req.RawArgs) > 0 {
		if err := json.Unmarshal(req.RawArgs, &args); err != nil {
			req.CloseWithError(fmt.Errorf("tunnel.isRoom: parse args: %w", err))
			return
		}
	}

	if args.ID == "" {
		req.CloseWithError(fmt.Errorf("tunnel.isRoom: id required"))
		return
	}

	isRoom := args.ID == h.server.keyPair.String()

	req.Return(ctx, isRoom)
}

func (h *TunnelHandler) handlePing(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "sync" {
		req.CloseWithError(fmt.Errorf("tunnel.ping is sync"))
		return
	}

	req.Return(ctx, true)
}
