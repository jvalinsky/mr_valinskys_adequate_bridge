package handlers

import (
	"context"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
)

type PingHandler struct{}

func NewPingHandler() *PingHandler {
	return &PingHandler{}
}

func (h *PingHandler) Handled(m muxrpc.Method) bool {
	if len(m) >= 1 && m[0] == "gossip" {
		if len(m) == 2 && m[1] == "ping" {
			return true
		}
	}
	return false
}

func (h *PingHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	req.Return(ctx, true)
}

func (h *PingHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {
}
