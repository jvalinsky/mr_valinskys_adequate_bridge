package codec

import (
	"bytes"
	"io"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	buf := &bytes.Buffer{}

	w := NewWriter(buf)

	pkt := Packet{
		Flag: FlagJSON,
		Req:  42,
		Body: []byte(`{"test":"value"}`),
	}

	if err := w.WritePacket(pkt); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	r := NewReader(buf)

	got, err := r.ReadPacket()
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	if got.Req != pkt.Req {
		t.Errorf("req: got %d, want %d", got.Req, pkt.Req)
	}

	if got.Flag != pkt.Flag {
		t.Errorf("flag: got %v, want %v", got.Flag, pkt.Flag)
	}

	if string(got.Body) != string(pkt.Body) {
		t.Errorf("body: got %s, want %s", got.Body, pkt.Body)
	}
}

func TestClosePacket(t *testing.T) {
	buf := &bytes.Buffer{}

	w := NewWriter(buf)

	if err := w.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	r := NewReader(buf)

	_, err := r.ReadPacket()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestFlagOperations(t *testing.T) {
	var f Flag

	if f.Get(FlagJSON) {
		t.Error("empty flag should not have FlagJSON")
	}

	f = FlagJSON
	if !f.Get(FlagJSON) {
		t.Error("FlagJSON should be set")
	}

	f = FlagStream | FlagString
	if !f.Get(FlagStream) || !f.Get(FlagString) {
		t.Error("both flags should be set")
	}

	f = f.Clear(FlagStream)
	if f.Get(FlagStream) || !f.Get(FlagString) {
		t.Error("FlagStream should be cleared, FlagString should remain")
	}
}

func TestDecoder(t *testing.T) {
	d := NewDecoder()

	// Need input for header
	_, err := d.NextPacket()
	if err != ErrNeedInput {
		t.Errorf("expected ErrNeedInput, got %v", err)
	}

	// Add partial header
	d.AddData([]byte{byte(FlagJSON)})
	_, err = d.NextPacket()
	if err != ErrNeedInput {
		t.Errorf("expected ErrNeedInput, got %v", err)
	}

	// Add rest of header (len=5, req=1)
	d.AddData([]byte{0, 0, 0, 5, 0, 0, 0, 1})
	_, err = d.NextPacket()
	if err != ErrNeedInput {
		t.Errorf("expected ErrNeedInput for body, got %v", err)
	}

	// Add partial body
	d.AddData([]byte("hel"))
	_, err = d.NextPacket()
	if err != ErrNeedInput {
		t.Errorf("expected ErrNeedInput for rest of body, got %v", err)
	}

	// Add rest of body and next packet header
	// Packet 1: "hello"
	// Packet 2: FlagString, len=2, req=2, body="hi"
	d.AddData([]byte("lo"))
	d.AddData([]byte{byte(FlagString), 0, 0, 0, 2, 0, 0, 0, 2, 'h', 'i'})

	p1, err := d.NextPacket()
	if err != nil {
		t.Fatalf("p1 failed: %v", err)
	}
	if string(p1.Body) != "hello" {
		t.Errorf("p1 body: %s", p1.Body)
	}

	p2, err := d.NextPacket()
	if err != nil {
		t.Fatalf("p2 failed: %v", err)
	}
	if string(p2.Body) != "hi" {
		t.Errorf("p2 body: %s", p2.Body)
	}

	// Test EOF packet (all zeros)
	d.AddData([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0})
	_, err = d.NextPacket()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestEncoderError(t *testing.T) {
	e := Encoder{}
	p := Packet{
		Body: make([]byte, maxBufferSize+1),
	}
	_, err := e.EncodePacket(p)
	if err == nil {
		t.Fatal("expected error for oversized packet")
	}
}
