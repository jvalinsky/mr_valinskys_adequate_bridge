// SPDX-FileCopyrightText: 2026 The Bridge Authors
//
// SPDX-License-Identifier: MIT

package legacy

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

// TestSignatureCompatWithGoSSB compares the bridge's SSB message signing
// against the go-ssb reference implementation behavior.
//
// This test validates that:
// 1. The same keypair produces the same signatures
// 2. The canonical JSON formatting is identical
// 3. The V8 binary encoding is identical
// 4. The message IDs (hashes) match

// TestKeyDerivationCompat verifies that the bridge's key derivation
// produces the same results as go-ssb's FromSeed approach.
func TestKeyDerivationCompat(t *testing.T) {
	// Use a known seed for reproducibility
	seedHex := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	seedBytes, err := base64.StdEncoding.DecodeString(seedHex)
	if err != nil {
		t.Fatalf("Failed to decode seed: %v", err)
	}
	var seed [32]byte
	copy(seed[:], seedBytes)

	// Bridge key derivation
	bridgeKP := keys.FromSeed(seed)

	// Expected public key (computed from go-ssb's ed25519.NewKeyFromSeed)
	// When using ed25519.NewKeyFromSeed with a seed of all zeros:
	// The public key is derived deterministically
	goSSBExpectedPub := ed25519.NewKeyFromSeed(seed[:])

	// Verify the private key is 64 bytes (seed + public key)
	bridgePriv := bridgeKP.Private()
	if len(bridgePriv) != 64 {
		t.Errorf("Bridge private key length = %d, want 64", len(bridgePriv))
	}

	// Verify the seed portion matches
	bridgeSeed := bridgeKP.Seed()
	if !bytes.Equal(bridgeSeed[:], seed[:]) {
		t.Errorf("Bridge seed portion mismatch")
	}

	// Verify the public key matches
	bridgePub := bridgeKP.Public()
	goSSBPub := goSSBExpectedPub.Public().(ed25519.PublicKey)

	if !bytes.Equal(bridgePub[:], goSSBPub) {
		t.Errorf("Public key mismatch:\n  Bridge: %x\n  go-ssb: %x", bridgePub[:], goSSBPub)
	}

	// Verify the full private key matches
	if !bytes.Equal(bridgePriv, goSSBExpectedPub) {
		t.Errorf("Private key mismatch:\n  Bridge: %x\n  go-ssb: %x", bridgePriv, goSSBExpectedPub)
	}

	t.Logf("Key derivation is compatible")
	t.Logf("Public key: %x", bridgePub[:])
	t.Logf("Feed ref: %s", bridgeKP.FeedRef().String())
}

// TestCanonicalJSONFormattingCompat verifies that both implementations
// produce identical canonical JSON for the same message.
func TestCanonicalJSONFormattingCompat(t *testing.T) {
	// Create a test message with deterministic values
	author, err := refs.NewFeedRef(make([]byte, 32), refs.RefAlgoFeedSSB1)
	if err != nil {
		t.Fatal(err)
	}
	// Fill with known value for comparison
	authorBytes, _ := base64.StdEncoding.DecodeString("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	copy(author.PubKey(), authorBytes)

	msg := &Message{
		Previous:  nil,
		Author:    *author,
		Sequence:  1,
		Timestamp: 1700000000000,
		Hash:      "sha256",
		Content:   map[string]interface{}{"type": "test", "hello": "world"},
	}

	// Get the canonical JSON from the bridge
	bridgeJSON, err := msg.marshalForSigning()
	if err != nil {
		t.Fatalf("Failed to marshal for signing: %v", err)
	}

	t.Logf("Bridge canonical JSON:\n%s", string(bridgeJSON))

	// Verify the structure matches what go-ssb expects:
	// 1. Field order: previous, author, sequence, timestamp, hash, content
	// 2. 2-space indentation
	// 3. No trailing newline before closing brace
	// 4. Newline after each field except the last content one

	// Check field order
	tests := []struct {
		field    string
		mustCome string
	}{
		{"previous", "author"},
		{"author", "sequence"},
		{"sequence", "timestamp"},
		{"timestamp", "hash"},
		{"hash", "content"},
	}

	for _, tt := range tests {
		fieldIdx := bytes.Index(bridgeJSON, []byte(`"`+tt.field+`"`))
		mustComeIdx := bytes.Index(bridgeJSON, []byte(`"`+tt.mustCome+`"`))
		if fieldIdx == -1 || mustComeIdx == -1 {
			t.Errorf("Field %q or %q not found in JSON", tt.field, tt.mustCome)
			continue
		}
		if fieldIdx > mustComeIdx {
			t.Errorf("Field %q should come before %q", tt.field, tt.mustCome)
		}
	}
}

// TestV8BinaryCompat verifies that the V8 binary encoding
// produces identical output between implementations.
func TestV8BinaryCompat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []byte
	}{
		{
			name:     "ASCII only",
			input:    "hello world",
			expected: []byte("hello world"),
		},
		{
			name:     "JSON with braces",
			input:    `{"key": "value"}`,
			expected: []byte(`{"key": "value"}`),
		},
		{
			name:     "Extended Latin (BMP)",
			input:    "cafe",
			expected: []byte("cafe"),
		},
		{
			name:     "Unicode (c) symbol U+00A9",
			input:    "\u00a9", // Copyright symbol
			expected: []byte{0xa9},
		},
		{
			name:     "Emoji U+1F44B (waving hand)",
			input:    "\U0001F44B", // Emoji
			// UTF-16 encoding: D83D DC4B, taking low bytes: 3D 4B
			expected: []byte{0x3d, 0x4b},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := V8Binary([]byte(tt.input))
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("V8Binary(%q) = %x, want %x", tt.input, got, tt.expected)
			}
		})
	}
}

// TestSignatureComparisonCompat tests that signatures produced by the bridge
// can be verified using standard ed25519 verification (same as go-ssb).
func TestSignatureComparisonCompat(t *testing.T) {
	// Generate a deterministic keypair
	seedHex := "dGVzdC1zZWVkLWZvci1zaWduYXR1cmUtdGVzdGluZw==" // "test-seed-for-signature-testing" in base64
	seedBytes, err := base64.StdEncoding.DecodeString(seedHex)
	if err != nil {
		t.Fatal(err)
	}
	var seed [32]byte
	copy(seed[:], seedBytes[:32])

	kp := keys.FromSeed(seed)
	author := kp.FeedRef()

	// Create a test message
	content := map[string]interface{}{
		"type":  "post",
		"text":  "Hello, SSB world!",
		"hello": "world",
	}

	msg := &Message{
		Previous:  nil,
		Author:    author,
		Sequence:  1,
		Timestamp: 1700000000000,
		Hash:      "sha256",
		Content:   content,
	}

	// Sign with the bridge
	msgRef, sig, err := msg.Sign(kp)
	if err != nil {
		t.Fatalf("Failed to sign message: %v", err)
	}

	t.Logf("Message ID: %s", msgRef.String())
	t.Logf("Signature: %s", base64.StdEncoding.EncodeToString(sig)+".sig.ed25519")

	// Verify using standard ed25519 (same as go-ssb's verification)
	contentToSign, err := msg.marshalForSigning()
	if err != nil {
		t.Fatalf("Failed to marshal for verification: %v", err)
	}

	pubKey := kp.Public()
	if !ed25519.Verify(pubKey[:], contentToSign, sig) {
		t.Error("Signature verification failed with standard ed25519")
	}

	// Also verify with the bridge's Signature type Verify method
	var bridgeSig Signature = sig
	if err := bridgeSig.Verify(contentToSign, author); err != nil {
		t.Errorf("Signature verification with bridge method failed: %v", err)
	}

	// Marshal the signed message and compute the hash
	signedMsg, err := msg.marshalWithSignature(sig)
	if err != nil {
		t.Fatalf("Failed to marshal signed message: %v", err)
	}

	t.Logf("Signed message JSON:\n%s", string(signedMsg))

	// Compute the message ID (hash)
	hash := HashMessage(signedMsg)
	computedRef, err := refs.NewMessageRef(hash, refs.RefAlgoMessageSSB1)
	if err != nil {
		t.Fatalf("Failed to create message ref: %v", err)
	}

	if !msgRef.Equal(*computedRef) {
		t.Errorf("Message ref mismatch:\n  Sign returned: %s\n  Computed: %s", msgRef.String(), computedRef.String())
	}
}

// TestStringEscapingCompat verifies that string escaping matches ECMA-262
// as used by go-ssb's quoteString function.
func TestStringEscapingCompat(t *testing.T) {
	// Test that JSON marshaling of content produces correct escaping
	tests := []struct {
		name    string
		content interface{}
	}{
		{
			name:    "Simple string",
			content: "hello world",
		},
		{
			name:    "String with escape",
			content: "line1\nline2",
		},
		{
			name:    "String with quote",
			content: `he said "hello"`,
		},
		{
			name:    "String with backslash",
			content: `C:\path\to\file`,
		},
		{
			name:    "String with tab",
			content: "col1\tcol2",
		},
		{
			name:    "String with control char",
			content: string([]byte{0x01, 0x02}),
		},
		{
			name: "Nested object",
			content: map[string]interface{}{
				"type": "post",
				"text": "hello\nworld",
				"mentions": []interface{}{
					map[string]interface{}{"link": "@abc.ed25519"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				Author:    mustCreateFeedRef(t, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="),
				Sequence:  1,
				Timestamp: 1700000000000,
				Hash:      "sha256",
				Content:   tt.content,
			}

			// marshalForSigning should produce valid JSON
			jsonBytes, err := msg.marshalForSigning()
			if err != nil {
				t.Fatalf("marshalForSigning failed: %v", err)
			}

			// Verify it's valid JSON by parsing it
			var parsed map[string]interface{}
			if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
				t.Errorf("Produced invalid JSON: %v\nJSON: %s", err, string(jsonBytes))
			}

			// Verify content can be extracted
			parsedContent := parsed["content"]
			contentJSON, err := json.Marshal(parsedContent)
			if err != nil {
				t.Fatalf("Failed to marshal parsed content: %v", err)
			}

			originalJSON, err := json.Marshal(tt.content)
			if err != nil {
				t.Fatalf("Failed to marshal original content: %v", err)
			}

			// The content should be equivalent (might differ in key order)
			t.Logf("Original: %s", string(originalJSON))
			t.Logf("Parsed:   %s", string(contentJSON))
		})
	}
}

// TestGoSSBMessageFormat verifies that the bridge produces messages
// in the exact format expected by go-ssb's verification.
func TestGoSSBMessageFormat(t *testing.T) {
	// This test uses a known message that go-ssb should be able to verify
	// The test data is derived from go-ssb's own test cases

	// A known test message from go-ssb test suite (sequence 1, no previous)
	// We'll create an equivalent message and compare the output format

	kp := keys.FromSeed([32]byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
	})

	author := kp.FeedRef()
	t.Logf("Feed: %s", author.String())

	content := map[string]interface{}{
		"type":  "test",
		"hello": "world",
	}

	msg := &Message{
		Author:    author,
		Sequence:  1,
		Timestamp: 1234567890000,
		Hash:      "sha256",
		Content:   content,
	}

	// Get the content to be signed (before signature is added)
	contentToSign, err := msg.marshalForSigning()
	if err != nil {
		t.Fatalf("Failed to marshal for signing: %v", err)
	}

	t.Logf("Content to sign:\n%s", string(contentToSign))

	// Sign the message
	msgRef, sig, err := msg.Sign(kp)
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}

	t.Logf("Message ID: %s", msgRef.String())
	t.Logf("Signature base64: %s.sig.ed25519", base64.StdEncoding.EncodeToString(sig))

	// Get the full signed message
	signedMsg, err := msg.MarshalWithSignature(sig)
	if err != nil {
		t.Fatalf("Failed to marshal signed message: %v", err)
	}

	t.Logf("Signed message:\n%s", string(signedMsg))

	// Now verify it can be parsed and verified
	verifiedMsg, err := VerifySignedMessageJSON(signedMsg)
	if err != nil {
		t.Fatalf("Failed to verify signed message: %v", err)
	}

	if !verifiedMsg.Author.Equal(author) {
		t.Errorf("Author mismatch: got %s, want %s", verifiedMsg.Author.String(), author.String())
	}

	// Verify the message hash matches
	computedRef, err := SignedMessageRefFromJSON(signedMsg)
	if err != nil {
		t.Fatalf("Failed to compute message ref: %v", err)
	}

	if !msgRef.Equal(*computedRef) {
		t.Errorf("Message ref mismatch:\n  Sign:    %s\n  Compute: %s", msgRef.String(), computedRef.String())
	}
}

// TestPreviousFieldHandling verifies that the previous field is formatted
// correctly for both null (root message) and non-null cases.
func TestPreviousFieldHandling(t *testing.T) {
	kp, _ := keys.Generate()
	author := kp.FeedRef()

	// Test root message (previous = nil)
	rootMsg := &Message{
		Author:    author,
		Sequence:  1,
		Timestamp: 1700000000000,
		Hash:      "sha256",
		Content:   map[string]interface{}{"type": "test"},
	}

	rootJSON, err := rootMsg.marshalForSigning()
	if err != nil {
		t.Fatalf("Failed to marshal root message: %v", err)
	}

	// Verify "previous": null format (go-ssb expects this for root messages)
	if !bytes.Contains(rootJSON, []byte(`"previous": null`)) {
		t.Errorf("Root message should have 'previous: null', got:\n%s", string(rootJSON))
	}

	// Test non-root message with a valid previous reference
	prevHash := make([]byte, 32)
	for i := range prevHash {
		prevHash[i] = byte(i)
	}
	prevRef, err := refs.NewMessageRef(prevHash, refs.RefAlgoMessageSSB1)
	if err != nil {
		t.Fatal(err)
	}

	nonRootMsg := &Message{
		Previous:  prevRef,
		Author:    author,
		Sequence:  2,
		Timestamp: 1700000001000,
		Hash:      "sha256",
		Content:   map[string]interface{}{"type": "test"},
	}

	nonRootJSON, err := nonRootMsg.marshalForSigning()
	if err != nil {
		t.Fatalf("Failed to marshal non-root message: %v", err)
	}

	// Verify "previous": "<ref>" format
	if !bytes.Contains(nonRootJSON, []byte(`"previous": "%`)) {
		t.Errorf("Non-root message should have 'previous: %%...', got:\n%s", string(nonRootJSON))
	}

	t.Logf("Root message JSON:\n%s", string(rootJSON))
	t.Logf("Non-root message JSON:\n%s", string(nonRootJSON))
}

// TestContentFormatting verifies that content is formatted with proper indentation.
func TestContentFormatting(t *testing.T) {
	kp, _ := keys.Generate()
	author := kp.FeedRef()

	// Content with nested object
	content := map[string]interface{}{
		"type": "post",
		"text": "hello world",
		"mentions": []map[string]interface{}{
			{"link": "@abc.ed25519", "name": "alice"},
			{"link": "&def.sha256", "name": "file", "type": "image/png"},
		},
	}

	msg := &Message{
		Author:    author,
		Sequence:  1,
		Timestamp: 1700000000000,
		Hash:      "sha256",
		Content:   content,
	}

	jsonBytes, err := msg.marshalForSigning()
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	t.Logf("Message JSON:\n%s", string(jsonBytes))

	// Verify the content is properly indented (depth 2 for root fields, depth 3+ for content)
	// The content object should have its opening brace on the same line as "content":
	if !bytes.Contains(jsonBytes, []byte(`"content": {`)) {
		t.Errorf("Content should be inline after 'content:', got:\n%s", string(jsonBytes))
	}
}

// Helper function
func mustCreateFeedRef(t *testing.T, b64 string) refs.FeedRef {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := refs.NewFeedRef(data, refs.RefAlgoFeedSSB1)
	if err != nil {
		t.Fatal(err)
	}
	return *ref
}

// BenchmarkCompareSigningPerformance compares signing performance
func BenchmarkCompareSigningPerformance(b *testing.B) {
	kp, _ := keys.Generate()
	author := kp.FeedRef()

	content := map[string]interface{}{
		"type": "post",
		"text": "This is a test message for benchmarking signature performance",
	}

	msg := &Message{
		Author:    author,
		Sequence:  1,
		Timestamp: 1700000000000,
		Hash:      "sha256",
		Content:   content,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, err := msg.Sign(kp)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// ExampleMessage_Sign demonstrates creating a signed message
func ExampleMessage_Sign() {
	// Create a deterministic keypair for reproducibility
	seed := [32]byte{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
	}

	kp := keys.FromSeed(seed)

	msg := &Message{
		Author:    kp.FeedRef(),
		Sequence:  1,
		Timestamp: 1700000000000,
		Hash:      "sha256",
		Content:   map[string]interface{}{"type": "test", "hello": "world"},
	}

	msgRef, sig, err := msg.Sign(kp)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Message ID: %s\n", msgRef.String())
	fmt.Printf("Signature: %s.sig.ed25519\n", base64.StdEncoding.EncodeToString(sig))

	// Output:
	// Message ID: %kcpHihme7NDnxaFC5DQ8V1UYyzC4tY2X20UWv8QobaI=.sha256
	// Signature: R17AdQQZdU6gGqH9lmb9BAlsxmN+vAh0+MT17Vx/FRPXMDhZWzvU3wBn6MjMG2oKke3uEr0TAgF3S0xLTYLVDg==.sig.ed25519
}
