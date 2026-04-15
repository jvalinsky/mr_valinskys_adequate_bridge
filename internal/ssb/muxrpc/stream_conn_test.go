package muxrpc

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestByteStreamConnCloseUnblocksReadAndRejectsWrite(t *testing.T) {
	ctx := context.Background()
	source := NewByteSource(ctx)
	sink := NewByteSink(nil)
	conn := NewByteStreamConn(ctx, source, sink, nil)

	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := conn.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("read after close err=%v, want EOF", err)
	}
	if _, err := conn.Write([]byte("hello")); !errors.Is(err, io.EOF) {
		t.Fatalf("write after close err=%v, want EOF", err)
	}
	if source.Next(ctx) {
		t.Fatal("source should be canceled after conn close")
	}
}
