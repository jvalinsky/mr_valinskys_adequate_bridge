package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/tangle"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
)

type TangleHandler struct {
	store *feedlog.StoreImpl
}

type TangleArgs struct {
	Name string `json:"name"`
	Root string `json:"root"`
}

func NewTangleHandler(store *feedlog.StoreImpl) *TangleHandler {
	return &TangleHandler{store: store}
}

func (h *TangleHandler) Handled(m muxrpc.Method) bool {
	return len(m) >= 1 && m[0] == "tangle"
}

func (h *TangleHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	if len(req.Method) < 2 {
		req.CloseWithError(fmt.Errorf("tangle: missing submethod"))
		return
	}

	switch req.Method[1] {
	case "getTangle":
		h.handleGetTangle(ctx, req)
	case "getMessages":
		h.handleGetMessages(ctx, req)
	case "getTips":
		h.handleGetTips(ctx, req)
	case "getRoot":
		h.handleGetRoot(ctx, req)
	case "getBacklinks":
		h.handleGetBacklinks(ctx, req)
	default:
		req.CloseWithError(fmt.Errorf("tangle: unknown method %s", req.Method[1]))
	}
}

func (h *TangleHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}

func (h *TangleHandler) handleGetTangle(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "async" {
		req.CloseWithError(fmt.Errorf("tangle.getTangle is async"))
		return
	}

	var args TangleArgs
	if err := json.Unmarshal(req.RawArgs, &args); err != nil {
		req.CloseWithError(fmt.Errorf("tangle.getTangle: parse args: %w", err))
		return
	}

	if args.Name == "" || args.Root == "" {
		req.CloseWithError(fmt.Errorf("tangle.getTangle: missing name or root"))
		return
	}

	t, err := h.store.Tangles().GetTangle(ctx, args.Name, args.Root)
	if err != nil {
		req.CloseWithError(fmt.Errorf("tangle.getTangle: %w", err))
		return
	}

	req.Return(ctx, map[string]interface{}{
		"name":         t.Name,
		"root":         t.Root,
		"tips":         t.Tips,
		"createdAt":    t.CreatedAt.Unix(),
		"messageCount": len(t.Tips),
	})
}

func (h *TangleHandler) handleGetMessages(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "source" {
		req.CloseWithError(fmt.Errorf("tangle.getMessages is a source handler"))
		return
	}

	var args struct {
		Name  string `json:"name"`
		Root  string `json:"root"`
		Since int64  `json:"since"`
	}
	if err := json.Unmarshal(req.RawArgs, &args); err != nil {
		req.CloseWithError(fmt.Errorf("tangle.getMessages: parse args: %w", err))
		return
	}

	if args.Name == "" || args.Root == "" {
		req.CloseWithError(fmt.Errorf("tangle.getMessages: missing name or root"))
		return
	}

	msgs, err := h.store.Tangles().GetTangleMessages(ctx, args.Name, args.Root, args.Since)
	if err != nil {
		req.CloseWithError(fmt.Errorf("tangle.getMessages: %w", err))
		return
	}

	sink, err := req.ResponseSink()
	if err != nil {
		req.CloseWithError(fmt.Errorf("tangle.getMessages: get sink: %w", err))
		return
	}

	sorter := tangle.NewTopologicalSorter(h.store.Tangles())
	sorted, sortErr := sorter.Sort(ctx, args.Name, args.Root)
	if sortErr != nil {
		for _, msg := range msgs {
			data, _ := json.Marshal(msg)
			sink.Write(data)
		}
	} else {
		for _, msg := range sorted {
			data, _ := json.Marshal(msg)
			sink.Write(data)
		}
	}
}

func (h *TangleHandler) handleGetTips(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "async" {
		req.CloseWithError(fmt.Errorf("tangle.getTips is async"))
		return
	}

	var args TangleArgs
	if err := json.Unmarshal(req.RawArgs, &args); err != nil {
		req.CloseWithError(fmt.Errorf("tangle.getTips: parse args: %w", err))
		return
	}

	if args.Name == "" || args.Root == "" {
		req.CloseWithError(fmt.Errorf("tangle.getTips: missing name or root"))
		return
	}

	tips, err := h.store.Tangles().GetTangleTips(ctx, args.Name, args.Root)
	if err != nil {
		req.CloseWithError(fmt.Errorf("tangle.getTips: %w", err))
		return
	}

	req.Return(ctx, tips)
}

func (h *TangleHandler) handleGetRoot(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "async" {
		req.CloseWithError(fmt.Errorf("tangle.getRoot is async"))
		return
	}

	var args struct {
		MessageKey string `json:"messageKey"`
	}
	if err := json.Unmarshal(req.RawArgs, &args); err != nil {
		req.CloseWithError(fmt.Errorf("tangle.getRoot: parse args: %w", err))
		return
	}

	if args.MessageKey == "" {
		req.CloseWithError(fmt.Errorf("tangle.getRoot: missing messageKey"))
		return
	}

	m, err := h.store.Tangles().GetTangleMembership(ctx, args.MessageKey)
	if err != nil {
		req.CloseWithError(fmt.Errorf("tangle.getRoot: %w", err))
		return
	}

	req.Return(ctx, map[string]string{
		"tangleName": m.TangleName,
		"rootKey":    m.RootKey,
	})
}

func (h *TangleHandler) handleGetBacklinks(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "source" {
		req.CloseWithError(fmt.Errorf("tangle.getBacklinks is a source handler"))
		return
	}

	var args struct {
		MessageKey string `json:"messageKey"`
	}
	if err := json.Unmarshal(req.RawArgs, &args); err != nil {
		req.CloseWithError(fmt.Errorf("tangle.getBacklinks: parse args: %w", err))
		return
	}

	if args.MessageKey == "" {
		req.CloseWithError(fmt.Errorf("tangle.getBacklinks: missing messageKey"))
		return
	}

	msgs, err := h.store.Tangles().GetMessagesByParent(ctx, args.MessageKey)
	if err != nil {
		req.CloseWithError(fmt.Errorf("tangle.getBacklinks: %w", err))
		return
	}

	sink, err := req.ResponseSink()
	if err != nil {
		req.CloseWithError(fmt.Errorf("tangle.getBacklinks: get sink: %w", err))
		return
	}

	for _, msg := range msgs {
		data, _ := json.Marshal(msg)
		sink.Write(data)
	}
}

func RegisterTangleHandler(mux *muxrpc.HandlerMux, store *feedlog.StoreImpl) {
	handler := NewTangleHandler(store)
	mux.Register(muxrpc.Method{"tangle", "getTangle"}, handler)
	mux.Register(muxrpc.Method{"tangle", "getMessages"}, handler)
	mux.Register(muxrpc.Method{"tangle", "getTips"}, handler)
	mux.Register(muxrpc.Method{"tangle", "getRoot"}, handler)
	mux.Register(muxrpc.Method{"tangle", "getBacklinks"}, handler)
}
