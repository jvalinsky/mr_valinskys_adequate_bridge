package room

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
)

type HTTPAuthHandler struct {
	authTokens roomdb.AuthWithSSBService
	members    roomdb.MembersService
	kp         *keys.KeyPair
}

func NewHTTPAuthHandler(at roomdb.AuthWithSSBService, members roomdb.MembersService) *HTTPAuthHandler {
	return &HTTPAuthHandler{
		authTokens: at,
		members:    members,
	}
}

func (h *HTTPAuthHandler) WithKeyPair(kp *keys.KeyPair) *HTTPAuthHandler {
	h.kp = kp
	return h
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
	case "requestSolution":
		h.handleRequestSolution(ctx, req)
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

	if h.authTokens != nil && h.members != nil {
		m, err := h.members.GetByFeed(ctx, caller)
		if err != nil {
			req.CloseWithError(fmt.Errorf("httpAuth.invalidateAllSolutions: get member: %w", err))
			return
		}

		err = h.authTokens.WipeTokensForMember(ctx, m.ID)
		if err != nil {
			req.CloseWithError(fmt.Errorf("httpAuth.invalidateAllSolutions: failed to invalidate tokens: %w", err))
			return
		}
	}
	req.Return(ctx, true)
}

func (h *HTTPAuthHandler) handleRequestSolution(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "async" {
		req.CloseWithError(fmt.Errorf("httpAuth.requestSolution is async"))
		return
	}

	if h.kp == nil {
		req.CloseWithError(fmt.Errorf("httpAuth.requestSolution: no keypair configured"))
		return
	}

	var args []string
	if err := json.Unmarshal(req.RawArgs, &args); err != nil {
		req.CloseWithError(fmt.Errorf("httpAuth.requestSolution: parse args: %w", err))
		return
	}
	if len(args) < 2 {
		req.CloseWithError(fmt.Errorf("httpAuth.requestSolution: missing args"))
		return
	}

	sc := args[0]
	cc := args[1]

	// Reconstruct message
	// =http-auth-sign-in:${sid}:${cid}:${sc}:${cc}
	sid, err := AuthenticatedFeedFromAddr(req.RemoteAddr())
	if err != nil {
		req.CloseWithError(fmt.Errorf("httpAuth.requestSolution: get sid: %w", err))
		return
	}

	cid := h.kp.FeedRef()
	msg := fmt.Sprintf("=http-auth-sign-in:%s:%s:%s:%s", sid.String(), cid.String(), sc, cc)

	// Sign
	sig := ed25519.Sign(h.kp.Private(), []byte(msg))

	// Return signature
	req.Return(ctx, sig)
}
