package room

import (
	"context"
	"fmt"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
)

type HTTPAuthHandler struct {
	authTokens roomdb.AuthWithSSBService
}

func NewHTTPAuthHandler(at roomdb.AuthWithSSBService) *HTTPAuthHandler {
	return &HTTPAuthHandler{authTokens: at}
}

func (h *HTTPAuthHandler) Handled(m muxrpc.Method) bool {
	return len(m) >= 1 && m[0] == "httpAuth"
}

func (h *HTTPAuthHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	if len(req.Method) < 2 {
		req.CloseWithError(fmt.Errorf("httpAuth: missing submethod"))
		return
	}

	switch req.Method[1] {
	case "invalidateAllSolutions":
		h.handleInvalidateAllSolutions(ctx, req)
	default:
		req.CloseWithError(fmt.Errorf("httpAuth: unknown method %s", req.Method[1]))
	}
}

func (h *HTTPAuthHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}

func (h *HTTPAuthHandler) handleInvalidateAllSolutions(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "async" {
		req.CloseWithError(fmt.Errorf("httpAuth.invalidateAllSolutions is async"))
		return
	}

	caller, err := AuthenticatedFeedFromAddr(req.RemoteAddr())
	if err != nil {
		req.CloseWithError(fmt.Errorf("httpAuth.invalidateAllSolutions: get caller: %w", err))
		return
	}

	// For now, we just acknowledge the request.
	// In a full implementation, we would revoke tokens for caller.
	_ = caller
	req.Return(ctx, true)
}
