package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

type HistoryStreamHandler struct {
	store *feedlog.StoreImpl
}

type HistoryStreamArgs struct {
	ID       string `json:"id"`
	Sequence int64  `json:"sequence"`
	Limit    int    `json:"limit"`
	Live     bool   `json:"live"`
	Old      bool   `json:"old"`
}

func NewHistoryStreamHandler(store *feedlog.StoreImpl) *HistoryStreamHandler {
	return &HistoryStreamHandler{store: store}
}

func (h *HistoryStreamHandler) Handled(m muxrpc.Method) bool {
	return len(m) == 1 && m[0] == "createHistoryStream"
}

func (h *HistoryStreamHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "source" {
		req.CloseWithError(fmt.Errorf("createHistoryStream is a source handler"))
		return
	}

	var args HistoryStreamArgs
	if len(req.RawArgs) > 0 {
		if err := json.Unmarshal(req.RawArgs, &args); err != nil {
			req.CloseWithError(fmt.Errorf("parse args: %w", err))
			return
		}
	}

	if args.ID == "" {
		req.CloseWithError(fmt.Errorf("createHistoryStream: id is required"))
		return
	}

	feedRef, err := refs.ParseFeedRef(args.ID)
	if err != nil {
		req.CloseWithError(fmt.Errorf("parse feed ref: %w", err))
		return
	}

	logs := h.store.Logs()
	log, err := logs.Get(feedRef.String())
	if err == feedlog.ErrNotFound {
		req.CloseWithError(nil)
		return
	}
	if err != nil {
		req.CloseWithError(fmt.Errorf("get log: %w", err))
		return
	}

	if args.Limit == 0 {
		args.Limit = 100
	}

	startSeq := args.Sequence
	if startSeq < 0 {
		startSeq = 0
	}

	go h.streamMessages(ctx, req, log, startSeq, int64(args.Limit))
}

func (h *HistoryStreamHandler) streamMessages(ctx context.Context, req *muxrpc.Request, log feedlog.Log, startSeq, limit int64) {
	sink, err := req.ResponseSink()
	if err != nil {
		req.CloseWithError(fmt.Errorf("get response sink: %w", err))
		return
	}

	defer sink.Close()

	seq, err := log.Seq()
	if err != nil {
		return
	}

	for i := startSeq + 1; i <= seq && i <= startSeq+limit; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := log.Get(i)
		if err != nil {
			continue
		}

		var content map[string]interface{}
		if err := json.Unmarshal(msg.Value, &content); err != nil {
			content = map[string]interface{}{"text": string(msg.Value)}
		}

		msgData := map[string]interface{}{
			"key":       msg.Key,
			"value":     content,
			"timestamp": msg.Metadata.Timestamp,
			"signature": fmt.Sprintf("%x", msg.Metadata.Sig),
		}

		data, err := json.Marshal(msgData)
		if err != nil {
			continue
		}

		if _, err := sink.Write(data); err != nil {
			return
		}
	}
}

func (h *HistoryStreamHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {
}
