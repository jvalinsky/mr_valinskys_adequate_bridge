package room

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/protocoltrace"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream"
)

// ClientTunnelConnectHandler proxies room-originated tunnel.connect streams into a
// local muxrpc handler tree so a room-connected peer can serve its own methods.
// It performs an inner Secret Handshake (SHS) over the tunnel byte stream before
// layering MuxRPC, as required by the SSB Room 2.0 spec.
type ClientTunnelConnectHandler struct {
	kp     *keys.KeyPair
	appKey secretstream.AppKey
	inner  muxrpc.Handler
}

func NewClientTunnelConnectHandler(kp *keys.KeyPair, appKey secretstream.AppKey, inner muxrpc.Handler) *ClientTunnelConnectHandler {
	return &ClientTunnelConnectHandler{
		kp:     kp,
		appKey: appKey,
		inner:  inner,
	}
}

func (h *ClientTunnelConnectHandler) Handled(m muxrpc.Method) bool {
	return len(m) == 2 && m[0] == "tunnel" && m[1] == "connect"
}

func (h *ClientTunnelConnectHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "duplex" {
		req.CloseWithError(fmt.Errorf("tunnel.connect is duplex"))
		return
	}

	args, err := parseClientTunnelConnectArgs(req.RawArgs)
	if err != nil {
		req.CloseWithError(fmt.Errorf("tunnel.connect: parse args: %w", err))
		return
	}
	if h.kp != nil {
		feed := h.kp.FeedRef()
		if args.Target != (refs.FeedRef{}) && !args.Target.Equal(feed) {
			req.CloseWithError(fmt.Errorf("tunnel.connect: target mismatch want=%s got=%s", feed.String(), args.Target.String()))
			return
		}
	}
	target := ""
	if h.kp != nil {
		target = h.kp.FeedRef().String()
	}
	protocoltrace.Emit(protocoltrace.Event{
		Phase:  "client_tunnel_connect_in",
		Method: "tunnel.connect",
		Req:    req.ID(),
		Origin: args.Origin.String(),
		Portal: args.Portal.String(),
		Target: target,
	})

	source := req.Source()
	if source == nil {
		req.CloseWithError(fmt.Errorf("tunnel.connect: request source unavailable"))
		return
	}
	sink, err := req.ResponseSink()
	if err != nil {
		req.CloseWithError(fmt.Errorf("tunnel.connect: response sink unavailable: %w", err))
		return
	}

	innerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	streamConn := muxrpc.NewByteStreamConn(innerCtx, source, sink, req.RemoteAddr())

	shs, err := secretstream.NewServer(streamConn, h.appKey, h.kp.Private())
	if err != nil {
		protocoltrace.Emit(protocoltrace.Event{
			Phase:   "client_tunnel_inner_shs_failed",
			Method:  "tunnel.connect",
			Req:     req.ID(),
			Origin:  args.Origin.String(),
			Portal:  args.Portal.String(),
			Target:  target,
			ErrKind: protocoltrace.ErrKind(err),
		})
		req.CloseWithError(fmt.Errorf("tunnel.connect: inner SHS init: %w", err))
		return
	}
	start := time.Now()
	if err := shs.Handshake(); err != nil {
		protocoltrace.Emit(protocoltrace.Event{
			Phase:    "client_tunnel_inner_shs_failed",
			Method:   "tunnel.connect",
			Req:      req.ID(),
			Origin:   args.Origin.String(),
			Portal:   args.Portal.String(),
			Target:   target,
			ErrKind:  protocoltrace.ErrKind(err),
			Duration: time.Since(start),
		})
		req.CloseWithError(fmt.Errorf("tunnel.connect: inner SHS handshake: %w", err))
		return
	}
	protocoltrace.Emit(protocoltrace.Event{
		Phase:    "client_tunnel_inner_shs_ok",
		Method:   "tunnel.connect",
		Req:      req.ID(),
		Origin:   args.Origin.String(),
		Portal:   args.Portal.String(),
		Target:   target,
		Duration: time.Since(start),
	})

	innerRPC := muxrpc.NewServer(innerCtx, shs, h.inner, nil)
	<-innerRPC.Wait()
	_ = innerRPC.Terminate()
	_ = shs.Close()
	_ = req.Close()
}

func (h *ClientTunnelConnectHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}

type clientTunnelConnectArgs struct {
	Origin refs.FeedRef `json:"origin"`
	Portal refs.FeedRef `json:"portal"`
	Target refs.FeedRef `json:"target"`
}

func parseClientTunnelConnectArgs(raw json.RawMessage) (clientTunnelConnectArgs, error) {
	var args []json.RawMessage
	if err := json.Unmarshal(raw, &args); err != nil {
		return clientTunnelConnectArgs{}, fmt.Errorf("expected muxrpc args array")
	}
	if len(args) == 0 {
		return clientTunnelConnectArgs{}, nil
	}
	if len(args) != 1 {
		return clientTunnelConnectArgs{}, fmt.Errorf("expected exactly one argument")
	}
	var parsed clientTunnelConnectArgs
	if err := json.Unmarshal(args[0], &parsed); err != nil {
		return clientTunnelConnectArgs{}, err
	}
	return parsed, nil
}
