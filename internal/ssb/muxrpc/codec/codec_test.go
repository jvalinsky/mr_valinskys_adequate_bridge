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
