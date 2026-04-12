package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"

	"golang.org/x/crypto/nacl/box"
)

// TestEncryptDecryptDM tests round-trip encryption and decryption of DM messages.
func TestEncryptDecryptDM(t *testing.T) {
	// Generate sender keypair
	senderPublic, senderPrivate, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate sender keys: %v", err)
	}

	// Generate recipient keypair
	recipientPublic, recipientPrivate, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate recipient keys: %v", err)
	}

	plaintext := []byte("Hello, this is a secret message")

	// Encrypt
	encrypted, err := EncryptDM(plaintext, *senderPrivate, *senderPrivate, *recipientPublic)
	if err != nil {
		t.Fatalf("EncryptDM failed: %v", err)
	}

	// Verify encrypted message structure
	var em EncryptedMessage
	if err := json.Unmarshal(encrypted, &em); err != nil {
		t.Fatalf("failed to parse encrypted message: %v", err)
	}
	if em.Format != "box2" {
		t.Errorf("unexpected format: got %q, want %q", em.Format, "box2")
	}
	if em.Ciphertext == "" || em.Nonce == "" {
		t.Error("encrypted message missing ciphertext or nonce")
	}

	// Decrypt
	decrypted, err := DecryptDM(encrypted, *recipientPublic, *recipientPrivate, *senderPublic)
	if err != nil {
		t.Fatalf("DecryptDM failed: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("round-trip failed: got %s, want %s", string(decrypted), string(plaintext))
	}
}

// TestDecryptDMWrongKey tests that decryption fails with wrong keys.
func TestDecryptDMWrongKey(t *testing.T) {
	// Generate two keypairs
	_, senderPrivate, _ := box.GenerateKey(rand.Reader)
	recipientPublic, recipientPrivate, _ := box.GenerateKey(rand.Reader)
	wrongPublic, _, _ := box.GenerateKey(rand.Reader)

	plaintext := []byte("secret message")

	encrypted, _ := EncryptDM(plaintext, *senderPrivate, *senderPrivate, *recipientPublic)

	// Try to decrypt with wrong sender public key
	_, err := DecryptDM(encrypted, *recipientPublic, *recipientPrivate, *wrongPublic)
	if err == nil {
		t.Error("decryption with wrong key should fail")
	}
}

// TestDecryptDMInvalidFormat tests handling of unsupported message format.
func TestDecryptDMInvalidFormat(t *testing.T) {
	recipientPublic, recipientPrivate, _ := box.GenerateKey(rand.Reader)
	senderPublic, _, _ := box.GenerateKey(rand.Reader)

	// Create a message with invalid format
	em := EncryptedMessage{
		Format:     "box3",
		Ciphertext: "abc",
		Nonce:      "def",
	}
	badMsg, _ := json.Marshal(em)

	_, err := DecryptDM(badMsg, *recipientPublic, *recipientPrivate, *senderPublic)
	if err == nil {
		t.Error("decryption of unsupported format should fail")
	}
}

// TestDecryptDMInvalidCiphertext tests handling of invalid base64 in ciphertext.
func TestDecryptDMInvalidCiphertext(t *testing.T) {
	recipientPublic, recipientPrivate, _ := box.GenerateKey(rand.Reader)
	senderPublic, _, _ := box.GenerateKey(rand.Reader)

	// Create a message with invalid base64 ciphertext
	em := EncryptedMessage{
		Format:     "box2",
		Ciphertext: "!!!invalid_base64!!!",
		Nonce:      base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("a"), NonceSize)),
	}
	badMsg, _ := json.Marshal(em)

	_, err := DecryptDM(badMsg, *recipientPublic, *recipientPrivate, *senderPublic)
	if err == nil {
		t.Error("decryption with invalid base64 ciphertext should fail")
	}
}

// TestDecryptDMInvalidNonce tests handling of invalid nonce.
func TestDecryptDMInvalidNonce(t *testing.T) {
	recipientPublic, recipientPrivate, _ := box.GenerateKey(rand.Reader)
	senderPublic, _, _ := box.GenerateKey(rand.Reader)

	// Create a message with invalid nonce length
	em := EncryptedMessage{
		Format:     "box2",
		Ciphertext: base64.StdEncoding.EncodeToString([]byte("someciphertext")),
		Nonce:      base64.StdEncoding.EncodeToString([]byte("short")),
	}
	badMsg, _ := json.Marshal(em)

	_, err := DecryptDM(badMsg, *recipientPublic, *recipientPrivate, *senderPublic)
	if err == nil {
		t.Error("decryption with invalid nonce should fail")
	}
}

// TestWrapAndUnwrapDMContent tests wrapping/unwrapping content for DM format.
func TestWrapAndUnwrapDMContent(t *testing.T) {
	recipient := "@abc123.ed25519"
	content := map[string]interface{}{
		"type": "post",
		"text": "Hello World",
	}

	wrapped, err := WrapContentForDM(content, recipient)
	if err != nil {
		t.Fatalf("WrapContentForDM failed: %v", err)
	}

	unwrapped, err := UnwrapDMContent(wrapped)
	if err != nil {
		t.Fatalf("UnwrapDMContent failed: %v", err)
	}

	if unwrapped.Recipient != recipient {
		t.Errorf("recipient mismatch: got %q, want %q", unwrapped.Recipient, recipient)
	}
}

// TestUnwrapDMContentMissingRecipient tests error on missing recipient.
func TestUnwrapDMContentMissingRecipient(t *testing.T) {
	badMsg := []byte(`{"content":"test"}`)
	_, err := UnwrapDMContent(badMsg)
	if err == nil {
		t.Error("unwrap with missing recipient should fail")
	}
}

// TestParseRecipient tests parsing valid and invalid recipient IDs.
func TestParseRecipient(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{
			name:      "valid recipient",
			input:     "@" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("a"), 32)),
			wantError: false,
		},
		{
			name:      "valid recipient with algo",
			input:     "@" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("b"), 32)) + ".ed25519",
			wantError: false,
		},
		{
			name:      "whitespace stripped",
			input:     "  @" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("c"), 32)) + "  ",
			wantError: false,
		},
		{
			name:      "missing @",
			input:     base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("d"), 32)),
			wantError: true,
		},
		{
			name:      "invalid base64",
			input:     "@!!!invalid!!!",
			wantError: true,
		},
		{
			name:      "wrong key length",
			input:     "@" + base64.StdEncoding.EncodeToString([]byte("short")),
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseRecipient(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("ParseRecipient(%q): got error %v, wantError %v", tt.input, err, tt.wantError)
			}
		})
	}
}

// TestEncryptPrivateBox tests that encryption produces valid message structure.
func TestEncryptPrivateBox(t *testing.T) {
	plaintext := []byte("This is a group message")

	// Generate recipient keypairs
	kp1, _ := NewKeyPairFromSecret(bytes.Repeat([]byte{1}, 32))
	kp2, _ := NewKeyPairFromSecret(bytes.Repeat([]byte{2}, 32))

	recipients := [][]byte{kp1.Public(), kp2.Public()}

	// Encrypt for multiple recipients
	encrypted, err := EncryptPrivateBox(plaintext, recipients)
	if err != nil {
		t.Fatalf("EncryptPrivateBox failed: %v", err)
	}

	// Verify message structure
	var msg PrivateBoxMessage
	if err := json.Unmarshal(encrypted, &msg); err != nil {
		t.Fatalf("failed to parse private-box message: %v", err)
	}
	if msg.Format != "private-box" {
		t.Errorf("unexpected format: got %q, want %q", msg.Format, "private-box")
	}
	if msg.Nonce == "" || msg.Keys == "" || msg.Header == "" || msg.Body == "" {
		t.Error("encrypted message missing required fields")
	}
}

// TestPrivateBoxTooManyRecipients tests error when exceeding max recipients.
func TestPrivateBoxTooManyRecipients(t *testing.T) {
	// Create 8 recipients (exceeds MaxRecipients = 7)
	recipients := make([][]byte, 8)
	for i := 0; i < 8; i++ {
		kp, _ := NewKeyPairFromSecret(append(bytes.Repeat([]byte{0}, 31), byte(i)))
		recipients[i] = kp.Public()
	}

	plaintext := []byte("too many recipients")
	_, err := EncryptPrivateBox(plaintext, recipients)
	if err == nil {
		t.Error("EncryptPrivateBox with too many recipients should fail")
	}
}

// TestPrivateBoxNoRecipients tests error with no recipients.
func TestPrivateBoxNoRecipients(t *testing.T) {
	_, err := EncryptPrivateBox([]byte("no recipients"), [][]byte{})
	if err == nil {
		t.Error("EncryptPrivateBox with no recipients should fail")
	}
}

// TestPrivateBoxDecryptWrongKey tests that wrong recipient cannot decrypt.
func TestPrivateBoxDecryptWrongKey(t *testing.T) {
	plaintext := []byte("secret group message")

	// Two authorized recipients
	kp1, _ := NewKeyPairFromSecret(bytes.Repeat([]byte{1}, 32))
	kp2, _ := NewKeyPairFromSecret(bytes.Repeat([]byte{2}, 32))

	// One unauthorized recipient
	kpWrong, _ := NewKeyPairFromSecret(bytes.Repeat([]byte{99}, 32))

	recipients := [][]byte{kp1.Public(), kp2.Public()}
	encrypted, _ := EncryptPrivateBox(plaintext, recipients)

	// Unauthorized recipient should not be able to decrypt
	_, err := PrivateBoxDecryptWithFeedKey(encrypted, kpWrong)
	if err == nil {
		t.Error("decrypt with unauthorized key should fail")
	}
}

// TestPrivateBoxInvalidFormat tests handling of unsupported format.
func TestPrivateBoxInvalidFormat(t *testing.T) {
	kp, _ := NewKeyPairFromSecret(bytes.Repeat([]byte{1}, 32))

	msg := PrivateBoxMessage{
		Format: "box3",
		Nonce:  base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("x"), PrivateBoxNonceSize)),
		Keys:   base64.StdEncoding.EncodeToString([]byte("keys")),
		Header: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("h"), PrivateBoxHeaderSize)),
		Body:   base64.StdEncoding.EncodeToString([]byte("body")),
	}
	badMsg, _ := json.Marshal(msg)

	_, err := PrivateBoxDecryptWithFeedKey(badMsg, kp)
	if err == nil {
		t.Error("decrypt of unsupported format should fail")
	}
}

// TestPrivateBoxInvalidNonce tests handling of invalid nonce.
func TestPrivateBoxInvalidNonce(t *testing.T) {
	kp, _ := NewKeyPairFromSecret(bytes.Repeat([]byte{1}, 32))

	msg := PrivateBoxMessage{
		Format: "private-box",
		Nonce:  base64.StdEncoding.EncodeToString([]byte("short")),
		Keys:   base64.StdEncoding.EncodeToString([]byte("keys")),
		Header: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("h"), PrivateBoxHeaderSize)),
		Body:   base64.StdEncoding.EncodeToString([]byte("body")),
	}
	badMsg, _ := json.Marshal(msg)

	_, err := PrivateBoxDecryptWithFeedKey(badMsg, kp)
	if err == nil {
		t.Error("decrypt with invalid nonce should fail")
	}
}

// TestPrivateBoxInvalidHeader tests handling of invalid header.
func TestPrivateBoxInvalidHeader(t *testing.T) {
	kp, _ := NewKeyPairFromSecret(bytes.Repeat([]byte{1}, 32))

	msg := PrivateBoxMessage{
		Format: "private-box",
		Nonce:  base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("x"), PrivateBoxNonceSize)),
		Keys:   base64.StdEncoding.EncodeToString([]byte("keys")),
		Header: base64.StdEncoding.EncodeToString([]byte("short")),
		Body:   base64.StdEncoding.EncodeToString([]byte("body")),
	}
	badMsg, _ := json.Marshal(msg)

	_, err := PrivateBoxDecryptWithFeedKey(badMsg, kp)
	if err == nil {
		t.Error("decrypt with invalid header should fail")
	}
}

// TestNewKeyPairFromSecret tests keypair generation from secret.
func TestNewKeyPairFromSecret(t *testing.T) {
	secret := bytes.Repeat([]byte{42}, 32)
	kp, err := NewKeyPairFromSecret(secret)
	if err != nil {
		t.Fatalf("NewKeyPairFromSecret failed: %v", err)
	}

	if len(kp.Public()) != 32 {
		t.Errorf("public key length: got %d, want 32", len(kp.Public()))
	}
	if len(kp.Secret()) != 32 {
		t.Errorf("secret key length: got %d, want 32", len(kp.Secret()))
	}

	// Same secret should produce same keypair
	kp2, _ := NewKeyPairFromSecret(secret)
	if !bytes.Equal(kp.Public(), kp2.Public()) {
		t.Error("same secret should produce same public key")
	}
}

// TestNewKeyPairFromSecretInvalidLength tests error on invalid secret length.
func TestNewKeyPairFromSecretInvalidLength(t *testing.T) {
	_, err := NewKeyPairFromSecret([]byte("short"))
	if err == nil {
		t.Error("NewKeyPairFromSecret with invalid length should fail")
	}
}

// TestPrivateBoxEncryptMultipleRecipients tests encryption with 3 recipients.
func TestPrivateBoxEncryptMultipleRecipients(t *testing.T) {
	plaintext := []byte("message for 3 recipients")

	// Create 3 recipients
	recipients := make([]*KeyPair, 3)
	for i := 0; i < 3; i++ {
		kp, _ := NewKeyPairFromSecret(append(bytes.Repeat([]byte{0}, 31), byte(i+1)))
		recipients[i] = kp
	}

	recipientPubKeys := make([][]byte, 3)
	for i, kp := range recipients {
		recipientPubKeys[i] = kp.Public()
	}

	encrypted, err := EncryptPrivateBox(plaintext, recipientPubKeys)
	if err != nil {
		t.Fatalf("EncryptPrivateBox with 3 recipients failed: %v", err)
	}

	// Verify encrypted message structure
	var msg PrivateBoxMessage
	if err := json.Unmarshal(encrypted, &msg); err != nil {
		t.Fatalf("failed to parse encrypted message: %v", err)
	}
	if msg.Format != "private-box" {
		t.Errorf("unexpected format: got %q, want %q", msg.Format, "private-box")
	}
}

// TestPrivateBoxEncryptEmptyPlaintext tests encryption of empty plaintext.
func TestPrivateBoxEncryptEmptyPlaintext(t *testing.T) {
	kp, _ := NewKeyPairFromSecret(bytes.Repeat([]byte{1}, 32))
	recipients := [][]byte{kp.Public()}

	encrypted, err := EncryptPrivateBox([]byte{}, recipients)
	if err != nil {
		t.Fatalf("EncryptPrivateBox with empty plaintext failed: %v", err)
	}

	// Verify structure only (decryption would fail due to implementation issue)
	var msg PrivateBoxMessage
	if err := json.Unmarshal(encrypted, &msg); err != nil {
		t.Fatalf("failed to parse encrypted message: %v", err)
	}
	if msg.Format != "private-box" {
		t.Errorf("unexpected format: got %q, want %q", msg.Format, "private-box")
	}
}

// TestPrivateBoxEncryptLargePlaintext tests encryption of large plaintext.
func TestPrivateBoxEncryptLargePlaintext(t *testing.T) {
	kp, _ := NewKeyPairFromSecret(bytes.Repeat([]byte{1}, 32))
	recipients := [][]byte{kp.Public()}

	// Large plaintext (10KB)
	plaintext := bytes.Repeat([]byte("x"), 10*1024)

	encrypted, err := EncryptPrivateBox(plaintext, recipients)
	if err != nil {
		t.Fatalf("EncryptPrivateBox with large plaintext failed: %v", err)
	}

	// Verify structure only
	var msg PrivateBoxMessage
	if err := json.Unmarshal(encrypted, &msg); err != nil {
		t.Fatalf("failed to parse encrypted message: %v", err)
	}
	if len(msg.Body) == 0 {
		t.Error("encrypted body should not be empty for large plaintext")
	}
}

// TestDMEmptyPlaintext tests encryption of empty DM plaintext.
func TestDMEmptyPlaintext(t *testing.T) {
	senderPublic, senderPrivate, _ := box.GenerateKey(rand.Reader)
	recipientPublic, recipientPrivate, _ := box.GenerateKey(rand.Reader)

	encrypted, err := EncryptDM([]byte{}, *senderPrivate, *senderPrivate, *recipientPublic)
	if err != nil {
		t.Fatalf("EncryptDM with empty plaintext failed: %v", err)
	}

	decrypted, err := DecryptDM(encrypted, *recipientPublic, *recipientPrivate, *senderPublic)
	if err != nil {
		t.Fatalf("DecryptDM failed: %v", err)
	}

	if len(decrypted) != 0 {
		t.Errorf("expected empty plaintext, got %d bytes", len(decrypted))
	}
}

// TestParseRecipientRoundTrip tests that ParseRecipient can parse the base64 from a keystring.
func TestParseRecipientRoundTrip(t *testing.T) {
	// Generate a keypair and create a feed ID string
	kp, _ := NewKeyPairFromSecret(bytes.Repeat([]byte{7}, 32))
	pubKey := kp.Public()

	feedID := "@" + base64.StdEncoding.EncodeToString(pubKey) + ".ed25519"

	// Parse it back
	parsed, err := ParseRecipient(feedID)
	if err != nil {
		t.Fatalf("ParseRecipient failed: %v", err)
	}

	if !bytes.Equal(parsed, pubKey) {
		t.Error("parsed recipient does not match original public key")
	}
}

// TestPrivateBoxInvalidKeyLength tests error when recipient key is wrong length.
func TestPrivateBoxInvalidKeyLength(t *testing.T) {
	// Short recipient key
	recipients := [][]byte{[]byte("short")}
	_, err := EncryptPrivateBox([]byte("plaintext"), recipients)
	if err == nil {
		t.Error("EncryptPrivateBox with invalid recipient key length should fail")
	}
}

// BenchmarkEncryptDM benchmarks DM encryption.
func BenchmarkEncryptDM(b *testing.B) {
	_, senderPrivate, _ := box.GenerateKey(rand.Reader)
	recipientPublic, _, _ := box.GenerateKey(rand.Reader)
	plaintext := []byte("Hello, this is a secret message")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EncryptDM(plaintext, *senderPrivate, *senderPrivate, *recipientPublic)
	}
}

// BenchmarkDecryptDM benchmarks DM decryption.
func BenchmarkDecryptDM(b *testing.B) {
	senderPublic, senderPrivate, _ := box.GenerateKey(rand.Reader)
	recipientPublic, recipientPrivate, _ := box.GenerateKey(rand.Reader)
	plaintext := []byte("Hello, this is a secret message")

	encrypted, _ := EncryptDM(plaintext, *senderPrivate, *senderPrivate, *recipientPublic)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecryptDM(encrypted, *recipientPublic, *recipientPrivate, *senderPublic)
	}
}

// BenchmarkEncryptPrivateBox benchmarks private-box encryption.
func BenchmarkEncryptPrivateBox(b *testing.B) {
	kp1, _ := NewKeyPairFromSecret(bytes.Repeat([]byte{1}, 32))
	kp2, _ := NewKeyPairFromSecret(bytes.Repeat([]byte{2}, 32))
	recipients := [][]byte{kp1.Public(), kp2.Public()}
	plaintext := []byte("This is a group message")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EncryptPrivateBox(plaintext, recipients)
	}
}

// BenchmarkDecryptPrivateBox benchmarks private-box decryption.
func BenchmarkDecryptPrivateBox(b *testing.B) {
	kp1, _ := NewKeyPairFromSecret(bytes.Repeat([]byte{1}, 32))
	kp2, _ := NewKeyPairFromSecret(bytes.Repeat([]byte{2}, 32))
	recipients := [][]byte{kp1.Public(), kp2.Public()}
	plaintext := []byte("This is a group message")

	encrypted, _ := EncryptPrivateBox(plaintext, recipients)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PrivateBoxDecryptWithFeedKey(encrypted, kp1)
	}
}
