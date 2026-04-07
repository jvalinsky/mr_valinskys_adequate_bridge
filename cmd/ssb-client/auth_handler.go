package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream"
)

type clientAuthHandler struct {
	kp *keys.KeyPair
}

func (h *clientAuthHandler) Handled(m muxrpc.Method) bool {
	return len(m) >= 1 && m[0] == "httpAuth"
}

func (h *clientAuthHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	if len(req.Method) < 2 {
		req.CloseWithError(fmt.Errorf("httpAuth: missing submethod"))
		return
	}

	switch req.Method[1] {
	case "requestSolution":
		h.handleRequestSolution(ctx, req)
	default:
		req.CloseWithError(fmt.Errorf("httpAuth: unknown method %s", req.Method[1]))
	}
}

func (h *clientAuthHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}

func (h *clientAuthHandler) handleRequestSolution(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "async" {
		req.CloseWithError(fmt.Errorf("httpAuth.requestSolution is async"))
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
	sid, err := secretstream.AuthenticatedFeedFromAddr(req.RemoteAddr())
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
