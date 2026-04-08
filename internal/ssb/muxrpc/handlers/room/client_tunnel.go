package room

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

// ClientTunnelConnectHandler proxies room-originated tunnel.connect streams into a
// local muxrpc handler tree so a room-connected peer can serve its own methods.
type ClientTunnelConnectHandler struct {
	feed  refs.FeedRef
	inner muxrpc.Handler
}

func NewClientTunnelConnectHandler(feed refs.FeedRef, inner muxrpc.Handler) *ClientTunnelConnectHandler {
	return &ClientTunnelConnectHandler{
		feed:  feed,
		inner: inner,
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
	if args.Target != (refs.FeedRef{}) && h.feed != (refs.FeedRef{}) && !args.Target.Equal(h.feed) {
		req.CloseWithError(fmt.Errorf("tunnel.connect: target mismatch want=%s got=%s", h.feed.String(), args.Target.String()))
		return
	}

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
	innerRPC := muxrpc.NewServer(innerCtx, streamConn, h.inner, nil)
	<-innerRPC.Wait()
	_ = innerRPC.Terminate()
	_ = streamConn.Close()
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
