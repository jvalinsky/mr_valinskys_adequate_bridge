package legacy_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

// TestSignatureCompatibility compares bridge's signature format against expected SSB format
func TestSignatureCompatibility(t *testing.T) {
	// Use fixed seed for deterministic test
	seed := [32]byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24,
		25, 26, 27, 28, 29, 30, 31, 32,
	}
	keyPair := keys.FromSeed(seed)
	pub := keyPair.Public()

	feedRef, err := refs.NewFeedRef(pub[:], refs.RefAlgoFeedSSB1)
	if err != nil {
		t.Fatalf("failed to create feed ref: %v", err)
	}

	msg := &legacy.Message{
		Previous:  nil, // First message
		Author:    *feedRef,
		Sequence:  1,
		Timestamp: 1700000000000,
		Hash:      "sha256",
		Content:   map[string]interface{}{"type": "post", "text": "Hello, world!"},
	}

	// Sign the message
	msgRef, sig, err := msg.Sign(keyPair)
	if err != nil {
		t.Fatalf("failed to sign message: %v", err)
	}

	// Verify signature format
	if len(sig) != ed25519.SignatureSize {
		t.Errorf("signature length: got %d, want %d", len(sig), ed25519.SignatureSize)
	}

	// Get the bytes that were signed
	contentToSign, err := msg.MarshalForSigning()
	if err != nil {
		t.Fatalf("failed to marshal for signing: %v", err)
	}

	// Verify signature manually
	if !ed25519.Verify(pub[:], contentToSign, sig) {
		t.Error("signature failed manual verification")
	}

	t.Logf("Message Ref: %s", msgRef.String())
	t.Logf("Signature (base64): %s", base64.StdEncoding.EncodeToString(sig))
	t.Logf("Content to sign (len=%d):\n%s", len(contentToSign), string(contentToSign))
}

// TestCanonicalJSONFormat verifies the JSON formatting matches SSB spec
func TestCanonicalJSONFormat(t *testing.T) {
	// Use fixed seed for deterministic test
	seed := [32]byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24,
		25, 26, 27, 28, 29, 30, 31, 32,
	}
	keyPair := keys.FromSeed(seed)
	pub := keyPair.Public()

	feedRef, err := refs.NewFeedRef(pub[:], refs.RefAlgoFeedSSB1)
	if err != nil {
		t.Fatalf("failed to create feed ref: %v", err)
	}

	msg := &legacy.Message{
		Previous:  nil, // First message
		Author:    *feedRef,
		Sequence:  1,
		Timestamp: 1700000000000,
		Hash:      "sha256",
		Content:   map[string]interface{}{"type": "post", "text": "Hello, world!"},
	}

	contentToSign, err := msg.MarshalForSigning()
	if err != nil {
		t.Fatalf("failed to marshal for signing: %v", err)
	}

	// Expected format requirements:
	// - 2-space indentation
	// - \n newlines (no \r)
	// - fields in order: previous, author, sequence, timestamp, hash, content
	// - no trailing newline
	// - one space after colon in key-value pairs

	// Check for CRLF
	if bytes.Contains(contentToSign, []byte("\r")) {
		t.Error("JSON contains CRLF, expected LF only")
	}

	// Check for trailing newline
	if len(contentToSign) > 0 && contentToSign[len(contentToSign)-1] == '\n' {
		t.Error("JSON has trailing newline, expected no trailing newline")
	}

	// Check indentation (2 spaces)
	lines := bytes.Split(contentToSign, []byte("\n"))
	for i, line := range lines {
		if i == 0 {
			continue // First line is "{"
		}
		if i == len(lines)-1 {
			continue // Last line is "}"
		}
		// Check that line starts with 2 spaces (for depth 1)
		if len(line) > 0 && line[0] != ' ' {
			t.Errorf("Line %d not indented: %s", i+1, string(line))
		}
	}

	// Check field order
	expectedOrder := []string{"previous", "author", "sequence", "timestamp", "hash", "content"}
	for i, field := range expectedOrder {
		if !bytes.Contains(lines[i+1], []byte(`"`+field+`"`)) {
			t.Errorf("Field %q not at expected position %d", field, i+1)
		}
	}

	t.Logf("Canonical JSON:\n%s", string(contentToSign))
}

// TestExtractSignatureCompat verifies our ExtractSignature matches go-ssb behavior
func TestExtractSignatureCompat(t *testing.T) {
	// Create a signed message
	seed := [32]byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
		17, 18, 19, 20, 21, 22, 23, 24,
		25, 26, 27, 28, 29, 30, 31, 32,
	}
	keyPair := keys.FromSeed(seed)
	pub := keyPair.Public()

	feedRef, err := refs.NewFeedRef(pub[:], refs.RefAlgoFeedSSB1)
	if err != nil {
		t.Fatalf("failed to create feed ref: %v", err)
	}

	msg := &legacy.Message{
		Previous:  nil,
		Author:    *feedRef,
		Sequence:  1,
		Timestamp: 1700000000000,
		Hash:      "sha256",
		Content:   map[string]interface{}{"type": "post", "text": "Test"},
	}

	_, sig, err := msg.Sign(keyPair)
	if err != nil {
		t.Fatalf("failed to sign message: %v", err)
	}

	// Get the full signed message
	signedBytes, err := msg.MarshalWithSignature(sig)
	if err != nil {
		t.Fatalf("failed to marshal with signature: %v", err)
	}

	// Extract signature
	extractedMsg, extractedSig, err := legacy.ExtractSignature(signedBytes)
	if err != nil {
		t.Fatalf("failed to extract signature: %v", err)
	}

	// Verify extracted signature matches original
	if !bytes.Equal(sig, extractedSig) {
		t.Error("extracted signature doesn't match original")
	}

	// Verify extracted message (without signature) matches what was signed
	originalMsgToSign, err := msg.MarshalForSigning()
	if err != nil {
		t.Fatalf("failed to marshal for signing: %v", err)
	}

	if !bytes.Equal(originalMsgToSign, extractedMsg) {
		t.Errorf("extracted message doesn't match original\nGot:\n%s\nExpected:\n%s",
			string(extractedMsg), string(originalMsgToSign))
	}

	// Verify signature verifies against extracted message
	if !ed25519.Verify(pub[:], extractedMsg, extractedSig) {
		t.Error("signature verification failed on extracted message")
	}
}
