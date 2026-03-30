package sbot

import (
	"context"
	"fmt"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/codec"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/replication"
)

type EBTHandlerWrapper struct {
	ebt *replication.EBTHandler
}

func NewEBTHandlerWrapper(ebt *replication.EBTHandler) *EBTHandlerWrapper {
	return &EBTHandlerWrapper{ebt: ebt}
}

func (w *EBTHandlerWrapper) Handled(m muxrpc.Method) bool {
	return len(m) >= 2 && m[0] == "ebt" && m[1] == "replicate"
}

func (w *EBTHandlerWrapper) HandleCall(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "duplex" {
		req.CloseWithError(fmt.Errorf("ebt.replicate is duplex"))
		return
	}

	source, err := req.ResponseSource()
	if err != nil {
		req.CloseWithError(fmt.Errorf("ebt.replicate: get source: %w", err))
		return
	}

	req.Sink().SetEncoding(muxrpc.TypeJSON)

	txStream := &muxrpc.PacketStream{}
	txStream.SetPackerAndReq(req.Sink().Writer(), -req.ID())
	txStream.SetFlag(codec.FlagJSON | codec.FlagStream)
	txStream.SetRequest(req)
	tx := muxrpc.NewPacketStreamWriter(txStream)
	rx := muxrpc.NewByteSourceAdapter(source)

	go func() {
		if err := w.ebt.HandleDuplex(ctx, tx, rx, req.RemoteAddr().String()); err != nil {
			req.CloseWithError(err)
		} else {
			req.Sink().Close()
		}
	}()
}

func (w *EBTHandlerWrapper) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}
