package muxrpc

import (
	"fmt"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/codec"
)

func TestByteSink_CloseWithError_IsCounter(t *testing.T) {
	bs := &ByteSink{
		req:       42,
		isCounter: true,
		closed:    false,
	}

	bs.CloseWithError(fmt.Errorf("test error"))

	if bs.isCounter != true {
		t.Errorf("isCounter should be preserved")
	}

	if !bs.closed {
		t.Errorf("should be marked as closed")
	}
}

type recordingPacketWriter struct {
	packets []codec.Packet
}

func (w *recordingPacketWriter) WritePacket(p codec.Packet) error {
	w.packets = append(w.packets, p)
	return nil
}

func TestByteSinkCloseBinaryStreamUsesJSONTermination(t *testing.T) {
	writer := &recordingPacketWriter{}
	bs := NewByteSink(writer)
	bs.SetReqID(7)
	bs.SetEncoding(TypeBinary)
	bs.SetFlag(codec.FlagStream)

	if _, err := bs.Write([]byte("payload")); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := bs.Close(); err != nil {
		t.Fatalf("close sink: %v", err)
	}

	if len(writer.packets) != 2 {
		t.Fatalf("expected payload and close packets, got %d", len(writer.packets))
	}
	payload := writer.packets[0]
	if payload.Flag.Get(codec.FlagJSON) || payload.Flag.Get(codec.FlagEndErr) {
		t.Fatalf("payload frame should stay binary stream data, got flag %08b", payload.Flag)
	}
	end := writer.packets[1]
	if !end.Flag.Get(codec.FlagJSON) || !end.Flag.Get(codec.FlagStream) || !end.Flag.Get(codec.FlagEndErr) {
		t.Fatalf("end frame should be JSON stream termination, got flag %08b", end.Flag)
	}
	if len(end.Body) != 0 {
		t.Fatalf("expected empty termination body, got %q", string(end.Body))
	}
}
