package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/formats"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

type HistoryStreamHandler struct {
	store *feedlog.StoreImpl
}

type HistoryStreamArgs struct {
	ID       string `json:"id"`
	Sequence int64  `json:"sequence"`
	Seq      *int64 `json:"seq"`
	Limit    int    `json:"limit"`
	Live     bool   `json:"live"`
	Old      *bool  `json:"old"`
	Keys     *bool  `json:"keys"`
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

	args, err := parseHistoryStreamArgs(req.RawArgs)
	if err != nil {
		req.CloseWithError(fmt.Errorf("parse args: %w", err))
		return
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
	feedFormat := formats.FeedFromRef(feedRef)
	if feedFormat != formats.FeedEd25519 {
		req.CloseWithError(formats.UnsupportedFeed(feedFormat, "createHistoryStream", "history"))
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

	startSeq := args.Sequence
	if startSeq < 0 {
		startSeq = 0
	}

	go h.streamMessages(ctx, req, log, startSeq, args)
}

func (h *HistoryStreamHandler) streamMessages(ctx context.Context, req *muxrpc.Request, log feedlog.Log, startSeq int64, args HistoryStreamArgs) {
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

	nextSeq := startSeq + 1
	sent := 0
	limit := args.Limit
	shouldSendBacklog := args.Old == nil || *args.Old

	if shouldSendBacklog {
		for nextSeq <= seq {
			if limit > 0 && sent >= limit {
				return
			}
			if err := h.writeMessage(ctx, sink, log, nextSeq, args); err != nil {
				return
			}
			nextSeq++
			sent++
		}
	} else {
		nextSeq = seq + 1
	}

	if !args.Live || (limit > 0 && sent >= limit) {
		return
	}

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentSeq, err := log.Seq()
			if err != nil {
				return
			}
			for nextSeq <= currentSeq {
				if limit > 0 && sent >= limit {
					return
				}
				if err := h.writeMessage(ctx, sink, log, nextSeq, args); err != nil {
					return
				}
				nextSeq++
				sent++
			}
		}
	}
}

func (h *HistoryStreamHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {
}

func parseHistoryStreamArgs(raw json.RawMessage) (HistoryStreamArgs, error) {
	var args HistoryStreamArgs
	if len(raw) == 0 {
		return args, nil
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return args, fmt.Errorf("expected muxrpc args array")
	}
	if len(arr) == 0 {
		return args, nil
	}
	if len(arr) != 1 {
		return args, fmt.Errorf("expected exactly one argument")
	}
	if err := json.Unmarshal(arr[0], &args); err != nil {
		return args, err
	}
	if args.Seq != nil {
		if args.Sequence != 0 && args.Sequence != *args.Seq {
			return args, fmt.Errorf("sequence and seq conflict")
		}
		args.Sequence = *args.Seq
	}
	return args, nil
}

func (h *HistoryStreamHandler) writeMessage(ctx context.Context, sink *muxrpc.ByteSink, log feedlog.Log, seq int64, args HistoryStreamArgs) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	msg, err := log.Get(seq)
	if err != nil {
		return fmt.Errorf("failed to get message at seq %d: %w", seq, err)
	}
	if msg.MessageFormat != "" && msg.MessageFormat != string(formats.MessageSHA256) {
		return formats.UnsupportedMessage(formats.MessageFormat(msg.MessageFormat), "createHistoryStream", "history")
	}

	raw, err := feedlog.ClassicSignedMessageRaw(msg, nil, seq)
	if err != nil {
		return fmt.Errorf("derive classic wire payload: %w", err)
	}

	if args.Keys == nil || *args.Keys {
		body := struct {
			Key       string          `json:"key"`
			Value     json.RawMessage `json:"value"`
			Timestamp int64           `json:"timestamp"`
		}{
			Key:       msg.Key,
			Value:     raw,
			Timestamp: msg.Received,
		}
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal message: %w", err)
		}
		_, err = sink.Write(data)
		return err
	}

	_, err = sink.Write(raw)
	return err
}
