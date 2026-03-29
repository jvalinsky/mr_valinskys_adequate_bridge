package handlers

import (
	"context"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
)

type WhoamiHandler struct {
	keyPair *keys.KeyPair
}

func NewWhoamiHandler(keyPair *keys.KeyPair) *WhoamiHandler {
	return &WhoamiHandler{keyPair: keyPair}
}

func (h *WhoamiHandler) Handled(m muxrpc.Method) bool {
	return len(m) == 1 && m[0] == "whoami"
}

func (h *WhoamiHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	feedRef := h.keyPair.FeedRef()
	req.Return(ctx, map[string]interface{}{
		"id": feedRef.String(),
	})
}

func (h *WhoamiHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {
}
