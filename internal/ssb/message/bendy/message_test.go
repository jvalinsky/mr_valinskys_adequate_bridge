package bendy

import (
	"bytes"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/bfe"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
)

func TestBendyMessageEncoding(t *testing.T) {
	kp, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}

	pub := kp.Public()
	author := bfe.EncodeFeed("ed25519", pub[:])
	content := map[string]interface{}{
		"type":  "post",
		"text":  "hello world",
		"check": true,
	}

	msg, err := CreateMessage(author, 1, nil, time.Now().Unix(), content, kp)
	if err != nil {
		t.Fatalf("failed to create message: %v", err)
	}

	// 1. Verify self-consistency
	err = msg.Verify()
	if err != nil {
		t.Errorf("verification failed: %v", err)
	}

	// 2. Test encoding/decoding roundtrip
	encoded, err := msg.Encode()
	if err != nil {
		t.Fatalf("failed to encode: %v", err)
	}

	decoded, err := FromStoredMessage(encoded)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if !bytes.Equal(msg.Signature, decoded.Signature) {
		t.Errorf("signature mismatch after roundtrip")
	}

	if msg.Sequence != decoded.Sequence {
		t.Errorf("sequence mismatch: %d != %d", msg.Sequence, decoded.Sequence)
	}

	// 3. Verify decoded message
	err = decoded.Verify()
	if err != nil {
		t.Errorf("verification of decoded message failed: %v", err)
	}
}

func TestBFEEncodingBugs(t *testing.T) {
	// Test Boolean encoding
	boolTrue := encodeValueToBFE(true).([]byte)
	boolFalse := encodeValueToBFE(false).([]byte)

	if bytes.Equal(boolTrue, boolFalse) {
		t.Errorf("bool true and false produced identical BFE: %v", boolTrue)
	}

	expectedTrue := []byte{bfe.TypeGeneric, 0x01, 0x01}
	if !bytes.Equal(boolTrue, expectedTrue) {
		t.Errorf("bool true BFE mismatch: %v != %v", boolTrue, expectedTrue)
	}

	// Test String encoding (should be raw UTF-8 after header)
	strVal := "hello"
	bfeStr := bfe.EncodeString(strVal)
	if !bytes.Equal(bfeStr[2:], []byte(strVal)) {
		t.Errorf("string BFE should be raw UTF-8: %v != %v", bfeStr[2:], []byte(strVal))
	}
}
