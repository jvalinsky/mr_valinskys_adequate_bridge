package secretstream

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"
)

func TestHandshakeMachine(t *testing.T) {
	appKey := NewAppKey("test")

	_, alicePriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	bobPub, bobPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// Alice is client, Bob is server
	alice, err := NewClientHandshake(appKey, alicePriv, bobPub)
	if err != nil {
		t.Fatal(err)
	}

	bob, err := NewServerHandshake(appKey, bobPriv)
	if err != nil {
		t.Fatal(err)
	}

	// Step 1: Alice sends challenge
	aliceChallenge, err := alice.Next(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(aliceChallenge) != 64 {
		t.Errorf("expected 64 byte challenge, got %d", len(aliceChallenge))
	}

	// Step 2: Bob receives Alice's challenge and sends his own
	bobChallenge, err := bob.Next(aliceChallenge)
	if err != nil {
		t.Fatal(err)
	}
	if len(bobChallenge) != 64 {
		t.Errorf("expected 64 byte challenge, got %d", len(bobChallenge))
	}

	// Step 3: Alice receives Bob's challenge and sends client auth
	aliceAuth, err := alice.Next(bobChallenge)
	if err != nil {
		t.Fatal(err)
	}
	if len(aliceAuth) != 112 {
		t.Errorf("expected 112 byte client auth, got %d", len(aliceAuth))
	}

	// Step 4: Bob receives Alice's auth and sends server accept
	bobAccept, err := bob.Next(aliceAuth)
	if err != nil {
		t.Fatal(err)
	}
	if len(bobAccept) != 80 {
		t.Errorf("expected 80 byte server accept, got %d", len(bobAccept))
	}

	// Step 5: Alice receives Bob's accept
	aliceFinal, err := alice.Next(bobAccept)
	if err != nil {
		t.Fatal(err)
	}
	if aliceFinal != nil {
		t.Errorf("expected nil output from alice at the end")
	}

	if !alice.IsDone() {
		t.Error("alice handshake not done")
	}
	if !bob.IsDone() {
		t.Error("bob handshake not done")
	}
}

func TestHandshakeMachinePartial(t *testing.T) {
	appKey := NewAppKey("test")

	_, alicePriv, _ := ed25519.GenerateKey(rand.Reader)
	bobPub, bobPriv, _ := ed25519.GenerateKey(rand.Reader)

	alice, _ := NewClientHandshake(appKey, alicePriv, bobPub)
	bob, _ := NewServerHandshake(appKey, bobPriv)

	// Alice starts
	aliceChallenge, _ := alice.Next(nil)

	// Feed Bob the challenge in two 32-byte chunks
	out, err := bob.Next(aliceChallenge[:32])
	if err != ErrNeedInput {
		t.Errorf("expected ErrNeedInput, got %v", err)
	}
	if out != nil {
		t.Error("expected nil output")
	}

	bobChallenge, err := bob.Next(aliceChallenge[32:])
	if err != nil {
		t.Fatal(err)
	}
	if len(bobChallenge) != 64 {
		t.Error("expected bob challenge")
	}
}

func TestOriginalHandshake(t *testing.T) {
	appKey := NewAppKey("test")
	_, alicePriv, _ := ed25519.GenerateKey(rand.Reader)
	bobPub, bobPriv, _ := ed25519.GenerateKey(rand.Reader)

	p1, p2 := net.Pipe()
	defer p1.Close()
	defer p2.Close()

	errHandler := make(chan error, 2)

	go func() {
		client, err := NewClient(p1, appKey, alicePriv, bobPub)
		if err != nil {
			errHandler <- err
			return
		}
		errHandler <- client.Handshake()
	}()

	go func() {
		server, err := NewServer(p2, appKey, bobPriv)
		if err != nil {
			errHandler <- err
			return
		}
		errHandler <- server.Handshake()
	}()

	for i := 0; i < 2; i++ {
		err := <-errHandler
		if err != nil {
			t.Errorf("Handshake %d failed: %v", i, err)
		}
	}
}
