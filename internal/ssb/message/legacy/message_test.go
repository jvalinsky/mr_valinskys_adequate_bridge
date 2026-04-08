package legacy

import (
	"bytes"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
)

func TestCanonicalJSONOrdering(t *testing.T) {
	input := []byte(`{"z":1,"a":2,"m":3}`)

	output := CanonicalJSON(input)

	// Check order of keys in output
	zIdx := bytes.Index(output, []byte(`"z"`))
	aIdx := bytes.Index(output, []byte(`"a"`))
	mIdx := bytes.Index(output, []byte(`"m"`))

	if zIdx == -1 || aIdx == -1 || mIdx == -1 {
		t.Fatalf("Missing keys in output: %s", string(output))
	}

	if !(zIdx < aIdx && aIdx < mIdx) {
		t.Errorf("Expected order z, a, m; got relative indices z:%d, a:%d, m:%d", zIdx, aIdx, mIdx)
	}
}

func TestV8Binary(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []byte
	}{
		{"ASCII", "hello", []byte("hello")},
		{"BMP", "©", []byte{0xa9}},         // U+00A9
		{"Emoji", "👋", []byte{0x3d, 0x4b}}, // U+1F44B -> UTF16: D83D DC4B -> Low bytes: 3D 4B
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := V8Binary([]byte(tt.input))
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("V8Binary(%s) = %x, want %x", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSignedMessageVerify(t *testing.T) {
	aliceKeys, _ := keys.Generate()

	msg := &Message{
		Author:    aliceKeys.FeedRef(),
		Sequence:  1,
		Timestamp: 1700000000000,
		Hash:      "sha256",
		Content:   map[string]interface{}{"type": "post", "text": "hello"},
	}

	_, sig, err := msg.Sign(aliceKeys, nil)
	if err != nil {
		t.Fatal(err)
	}

	signed := &SignedMessage{
		Author:    msg.Author,
		Sequence:  msg.Sequence,
		Timestamp: msg.Timestamp,
		Hash:      msg.Hash,
		Content:   msg.Content,
		Signature: sig,
	}

	if err := signed.Verify(); err != nil {
		t.Errorf("Verification failed: %v", err)
	}

	// Tamper
	signed.Sequence = 2
	if err := signed.Verify(); err == nil {
		t.Error("Verification should have failed after tampering")
	}
}

func TestExtractSignature(t *testing.T) {
	s, err := NewSignatureFromBase64([]byte("YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYQ==.sig.ed25519"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 64 {
		t.Errorf("expected 64, got %d", len(s))
	}
}

func TestVerifySignedMessageJSONPreservesContentOrder(t *testing.T) {
	raw := []byte(`{"previous":null,"author":"@6iF2pmL9+jpnM515551HTgVVOGCUZ9qfE8Y3DmdFz7w=.ed25519","sequence":1,"timestamp":1775622534000,"hash":"sha256","content":{"type":"contact","contact":"@HY3zOj73zbLT5wG76eUZXIKTMB4to/voRbYWESXyVtA=.ed25519","following":true},"signature":"IFefnN3fb4bEpWfFtMD2lyn30yQXtmSPVCB0JQQv05WkHVADzz675PiMAf5JLXosTUPfP02IvTeKHdQd1JGPAw==.sig.ed25519"}`)

	msg, err := VerifySignedMessageJSON(raw)
	if err != nil {
		t.Fatalf("verify signed message JSON: %v", err)
	}
	if got, want := msg.Author.String(), "@6iF2pmL9+jpnM515551HTgVVOGCUZ9qfE8Y3DmdFz7w=.ed25519"; got != want {
		t.Fatalf("author = %s, want %s", got, want)
	}

	msgRef, err := SignedMessageRefFromJSON(raw)
	if err != nil {
		t.Fatalf("signed message ref: %v", err)
	}
	if got, want := msgRef.String(), "%+ofkHa7VpmLgrdhkjtY9SFYoOOp+F7KiEHlG9y4s8eo=.sha256"; got != want {
		t.Fatalf("message ref = %s, want %s", got, want)
	}
}
