package muxrpc

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/codec"
)

type mockReadWriteCloser struct {
	io.Reader
	io.Writer
	closed bool
}

func (m *mockReadWriteCloser) Close() error {
	m.closed = true
	return nil
}

func TestPackerDecoderExtraction(t *testing.T) {
	m := &mockReadWriteCloser{
		Reader: &bytes.Buffer{},
		Writer: &bytes.Buffer{},
	}
	pkr := NewPacker(m)
	ctx := context.Background()

	// 1. Add raw data to decoder
	// FlagJSON, len=5, req=1, body="hello"
	p1 := []byte{byte(codec.FlagJSON), 0, 0, 0, 5, 0, 0, 0, 1, 'h', 'e', 'l', 'l', 'o'}
	pkr.AddRawData(p1)

	// 2. NextHeader should extract from decoder
	var hdr codec.Header
	if err := pkr.NextHeader(ctx, &hdr); err != nil {
		t.Fatalf("NextHeader failed: %v", err)
	}
	if hdr.Len != 5 || hdr.Req != 1 {
		t.Errorf("hdr: %+v", hdr)
	}

	// 3. NextPacket should extract from decoder
	p, err := pkr.NextPacket(ctx)
	if err != nil {
		t.Fatalf("NextPacket failed: %v", err)
	}
	if string(p.Body) != "hello" {
		t.Errorf("got body: %s", p.Body)
	}

	// 4. Test EncodePacket (ensures encoder was initialized)
	encoded, err := pkr.EncodePacket(codec.Packet{Body: []byte("hi")})
	if err != nil {
		t.Fatalf("EncodePacket failed: %v", err)
	}
	if len(encoded) != codec.HeaderSize+2 {
		t.Errorf("encoded len: %d", len(encoded))
	}
}

func TestPackerClose(t *testing.T) {
	m := &mockReadWriteCloser{
		Reader: &bytes.Buffer{},
		Writer: &bytes.Buffer{},
	}
	pkr := NewPacker(m)

	if err := pkr.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if !m.closed {
		t.Error("underlying closer NOT called")
	}

	// Second close should be no-op or return same error
	if err := pkr.Close(); err != nil {
		t.Fatalf("Second Close failed: %v", err)
	}
}
