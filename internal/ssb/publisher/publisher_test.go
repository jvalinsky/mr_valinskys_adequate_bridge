package publisher

import (
	"crypto/ed25519"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

func TestMessageSigning(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}

	keyPair := keys.FromSeed(*(*[32]byte)(seed[:32]))

	pub := keyPair.Public()

	feedRef, err := refs.NewFeedRef(pub[:], refs.RefAlgoFeedSSB1)
	if err != nil {
		t.Fatalf("failed to create feed ref: %v", err)
	}

	msg := &legacy.Message{
		Author:    *feedRef,
		Sequence:  1,
		Timestamp: time.Now().UnixMilli(),
		Hash:      "sha256",
		Content:   map[string]interface{}{"type": "test", "text": "hello"},
	}

	msgRef, sig, err := msg.Sign(keyPair, nil)
	if err != nil {
		t.Fatalf("failed to sign message: %v", err)
	}

	if len(sig) != ed25519.SignatureSize {
		t.Errorf("signature length: got %d, want %d", len(sig), ed25519.SignatureSize)
	}

	if msgRef == nil {
		t.Error("message ref is nil")
	}

	contentToSign, err := msg.MarshalForSigning()
	if err != nil {
		t.Fatalf("failed to marshal for signing: %v", err)
	}

	if !ed25519.Verify(pub[:], contentToSign, sig) {
		t.Error("signature verification failed")
	}
}

func TestHMACMessageSigning(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}

	keyPair := keys.FromSeed(*(*[32]byte)(seed[:32]))
	hmacKey := []byte("test-hmac-key-32-bytes-long!!")

	pub := keyPair.Public()
	feedRef, _ := refs.NewFeedRef(pub[:], refs.RefAlgoFeedSSB1)

	msg := &legacy.Message{
		Author:    *feedRef,
		Sequence:  1,
		Timestamp: time.Now().UnixMilli(),
		Hash:      "sha256",
		Content:   map[string]interface{}{"type": "test", "text": "hello with hmac"},
	}

	msgRef, sig, err := msg.Sign(keyPair, hmacKey)
	if err != nil {
		t.Fatalf("failed to sign message with HMAC: %v", err)
	}

	if len(sig) != ed25519.SignatureSize {
		t.Errorf("signature length: got %d, want %d", len(sig), ed25519.SignatureSize)
	}

	if msgRef == nil {
		t.Error("message ref is nil")
	}

	contentToSign, _ := msg.MarshalForSigning()
	h := sha256.New()
	h.Write(hmacKey)
	h.Write(contentToSign)
	hashed := h.Sum(nil)

	if !ed25519.Verify(pub[:], hashed, sig) {
		t.Error("HMAC signature verification failed")
	}
}

func TestKeyDerivation(t *testing.T) {
	masterSeed := make([]byte, 32)
	for i := range masterSeed {
		masterSeed[i] = byte(i + 1)
	}

	did1 := "did:plc:test123"
	did2 := "did:plc:test456"

	kp1, feed1, err := DeriveKeyPair(masterSeed, did1)
	if err != nil {
		t.Fatalf("failed to derive keypair 1: %v", err)
	}

	kp2, feed2, err := DeriveKeyPair(masterSeed, did2)
	if err != nil {
		t.Fatalf("failed to derive keypair 2: %v", err)
	}

	if feed1.Equal(feed2) {
		t.Error("different DIDs should produce different feeds")
	}

	pub1 := kp1.Public()
	pub2 := kp2.Public()

	if string(pub1[:]) == string(pub2[:]) {
		t.Error("different DIDs should produce different keys")
	}

	kp1Again, feed1Again, err := DeriveKeyPair(masterSeed, did1)
	if err != nil {
		t.Fatalf("failed to re-derive keypair: %v", err)
	}

	if !feed1.Equal(feed1Again) {
		t.Error("same DID should produce same feed")
	}

	pub1Again := kp1Again.Public()
	if string(pub1[:]) != string(pub1Again[:]) {
		t.Error("same DID should produce same public key")
	}
}

func TestMessageRef(t *testing.T) {
	hash := sha256.Sum256([]byte("test content"))
	msgRef, err := refs.NewMessageRef(hash[:], refs.RefAlgoMessageSSB1)
	if err != nil {
		t.Fatalf("failed to create message ref: %v", err)
	}

	refStr := msgRef.String()
	if refStr[0] != '%' {
		t.Errorf("message ref should start with %%: %s", refStr)
	}

	parsed, err := refs.ParseMessageRef(refStr)
	if err != nil {
		t.Fatalf("failed to parse message ref: %v", err)
	}

	if !parsed.Equal(*msgRef) {
		t.Error("parsed ref should equal original")
	}
}

func TestFeedRef(t *testing.T) {
	pub := make([]byte, 32)
	for i := range pub {
		pub[i] = byte(i)
	}

	feedRef, err := refs.NewFeedRef(pub, refs.RefAlgoFeedSSB1)
	if err != nil {
		t.Fatalf("failed to create feed ref: %v", err)
	}

	refStr := feedRef.String()
	if refStr[0] != '@' {
		t.Errorf("feed ref should start with @: %s", refStr)
	}

	parsed, err := refs.ParseFeedRef(refStr)
	if err != nil {
		t.Fatalf("failed to parse feed ref: %v", err)
	}

	if !parsed.Equal(*feedRef) {
		t.Error("parsed ref should equal original")
	}
}

func TestVerifyMessageCompat(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i * 2)
	}

	keyPair := keys.FromSeed(*(*[32]byte)(seed[:32]))
	pub := keyPair.Public()

	feedRef, _ := refs.NewFeedRef(pub[:], refs.RefAlgoFeedSSB1)

	msg := &legacy.Message{
		Author:    *feedRef,
		Sequence:  1,
		Timestamp: 1700000000000,
		Hash:      "sha256",
		Content:   map[string]interface{}{"type": "about", "name": "Test User"},
	}

	_, sig, err := msg.Sign(keyPair, nil)
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}

	contentToSign, _ := msg.MarshalForSigning()

	if !VerifyMessage(contentToSign, sig, pub[:]) {
		t.Error("message should verify correctly")
	}

	tamperedContent := []byte(`{"author":"` + feedRef.String() + `","sequence":2}`)
	if VerifyMessage(tamperedContent, sig, pub[:]) {
		t.Error("tampered message should not verify")
	}
}
