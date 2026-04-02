package boxstream

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"
)

func TestBoxStream(t *testing.T) {
	key := make([]byte, 32)
	nonce := make([]byte, 24)
	rand.Read(key)
	rand.Read(nonce)

	buf := &bytes.Buffer{}
	var k [32]byte
	var n [24]byte
	copy(k[:], key)
	copy(n[:], nonce)

	w := NewBoxer(buf, &n, &k)
	r := NewUnboxer(buf, &n, &k)

	msg1 := []byte("hello world")
	msg2 := []byte("secret message")

	// Write 1
	nWrite1, err := w.Write(msg1)
	if err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if nWrite1 != len(msg1) {
		t.Errorf("write 1 len: %d", nWrite1)
	}

	// Write 2
	nWrite2, err := w.Write(msg2)
	if err != nil {
		t.Fatalf("write 2: %v", err)
	}
	if nWrite2 != len(msg2) {
		t.Errorf("write 2 len: %d", nWrite2)
	}

	// Read 1
	got1 := make([]byte, len(msg1))
	_, err = io.ReadFull(r, got1)
	if err != nil {
		t.Fatalf("read 1: %v", err)
	}
	if !bytes.Equal(got1, msg1) {
		t.Errorf("got 1: %s", got1)
	}

	// Read 2
	got2 := make([]byte, len(msg2))
	_, err = io.ReadFull(r, got2)
	if err != nil {
		t.Fatalf("read 2: %v", err)
	}
	if !bytes.Equal(got2, msg2) {
		t.Errorf("got 2: %s", got2)
	}
}

func TestBoxStreamError(t *testing.T) {
	key := make([]byte, 32)
	nonce := make([]byte, 24)

	buf := &bytes.Buffer{}
	var k [32]byte
	var n [24]byte
	copy(k[:], key)
	copy(n[:], nonce)

	r := NewUnboxer(buf, &n, &k)

	// Corrupt header length in the buffer
	// (Writing corrupted data is harder than reading it)
	buf.Write(make([]byte, 50)) // garbage

	got := make([]byte, 10)
	_, err := r.Read(got)
	if err == nil {
		t.Fatal("expected error for corrupted stream data")
	}
}
