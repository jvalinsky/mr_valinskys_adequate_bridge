package boxstream

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"
)

func TestBoxstreamEngine(t *testing.T) {
	var secret [32]byte
	var nonce [24]byte
	_, _ = io.ReadFull(rand.Reader, secret[:])
	_, _ = io.ReadFull(rand.Reader, nonce[:])

	boxer := NewBoxerEngine(&nonce, &secret)
	unboxer := NewUnboxerEngine(&nonce, &secret)

	msg := []byte("hello world")
	encrypted := boxer.EncryptMessage(msg)

	// Feed in chunks to test fragmentation
	unboxer.AddRawData(encrypted[:10])
	m1, err := unboxer.NextMessage()
	if err != nil {
		t.Fatal(err)
	}
	if m1 != nil {
		t.Fatal("expected no message yet")
	}

	unboxer.AddRawData(encrypted[10:])
	m2, err := unboxer.NextMessage()
	if err != nil {
		t.Fatal(err)
	}
	if m2 == nil {
		t.Fatal("expected message")
	}

	if !bytes.Equal(msg, m2) {
		t.Errorf("expected %q, got %q", msg, m2)
	}
}

func TestBoxstreamGoodbye(t *testing.T) {
	var secret [32]byte
	var nonce [24]byte
	_, _ = io.ReadFull(rand.Reader, secret[:])
	_, _ = io.ReadFull(rand.Reader, nonce[:])

	boxer := NewBoxerEngine(&nonce, &secret)
	unboxer := NewUnboxerEngine(&nonce, &secret)

	goodbyeEnc := boxer.EncryptGoodbye()
	unboxer.AddRawData(goodbyeEnc)
	
	msg, err := unboxer.NextMessage()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(msg, goodbye[:]) {
		t.Errorf("expected goodbye, got %v", msg)
	}
}
