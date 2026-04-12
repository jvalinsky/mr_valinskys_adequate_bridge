package bfe

import (
	"bytes"
	"testing"
)

// TestEncodeFeedEd25519 tests encoding ed25519 feed references.
func TestEncodeFeedEd25519(t *testing.T) {
	pubKey := bytes.Repeat([]byte("a"), 32)
	encoded := EncodeFeed("ed25519", pubKey)

	if len(encoded) != 34 {
		t.Errorf("encoded length: got %d, want 34", len(encoded))
	}
	if encoded[0] != TypeFeed {
		t.Errorf("type byte: got %d, want %d", encoded[0], TypeFeed)
	}
	if encoded[1] != 0x00 {
		t.Errorf("format code: got %d, want 0", encoded[1])
	}
	if !bytes.Equal(encoded[2:34], pubKey) {
		t.Error("public key mismatch")
	}
}

// TestEncodeFeedGabbyGrove tests encoding gabbygrove-v1 feed references.
func TestEncodeFeedGabbyGrove(t *testing.T) {
	pubKey := bytes.Repeat([]byte("b"), 32)
	encoded := EncodeFeed("gabbygrove-v1", pubKey)

	if len(encoded) != 34 {
		t.Errorf("encoded length: got %d, want 34", len(encoded))
	}
	if encoded[1] != 0x01 {
		t.Errorf("format code: got %d, want 1", encoded[1])
	}
}

// TestDecodeFeedRoundTrip tests round-trip encode/decode for feeds.
func TestDecodeFeedRoundTrip(t *testing.T) {
	tests := []struct {
		algo   string
		pubKey []byte
	}{
		{"ed25519", bytes.Repeat([]byte{1}, 32)},
		{"gabbygrove-v1", bytes.Repeat([]byte{2}, 32)},
		{"bamboo", bytes.Repeat([]byte{3}, 32)},
		{"bendybutt-v1", bytes.Repeat([]byte{4}, 32)},
		{"buttwoo-v1", bytes.Repeat([]byte{5}, 32)},
		{"indexed-v1", bytes.Repeat([]byte{6}, 32)},
	}

	for _, tt := range tests {
		t.Run(tt.algo, func(t *testing.T) {
			encoded := EncodeFeed(tt.algo, tt.pubKey)
			algo, pubKey, err := DecodeFeed(encoded)
			if err != nil {
				t.Fatalf("DecodeFeed failed: %v", err)
			}
			if algo != tt.algo {
				t.Errorf("algo mismatch: got %q, want %q", algo, tt.algo)
			}
			if !bytes.Equal(pubKey, tt.pubKey) {
				t.Error("public key mismatch")
			}
		})
	}
}

// TestDecodeFeedInvalidType tests error on wrong type byte.
func TestDecodeFeedInvalidType(t *testing.T) {
	badData := make([]byte, 34)
	badData[0] = 0xFF // wrong type

	_, _, err := DecodeFeed(badData)
	if err != ErrInvalidBFE {
		t.Errorf("expected ErrInvalidBFE, got %v", err)
	}
}

// TestDecodeFeedTooShort tests error on undersized input.
func TestDecodeFeedTooShort(t *testing.T) {
	_, _, err := DecodeFeed([]byte("short"))
	if err != ErrInvalidBFE {
		t.Errorf("expected ErrInvalidBFE, got %v", err)
	}
}

// TestDecodeFeedUnknownFormat tests error on unknown format code.
func TestDecodeFeedUnknownFormat(t *testing.T) {
	badData := make([]byte, 34)
	badData[0] = TypeFeed
	badData[1] = 0xFF // unknown format code

	_, _, err := DecodeFeed(badData)
	if err != ErrInvalidFormat {
		t.Errorf("expected ErrInvalidFormat, got %v", err)
	}
}

// TestEncodeMessageSHA256 tests encoding sha256 message references.
func TestEncodeMessageSHA256(t *testing.T) {
	hash := bytes.Repeat([]byte("x"), 32)
	encoded := EncodeMessage("sha256", hash)

	if len(encoded) != 34 {
		t.Errorf("encoded length: got %d, want 34", len(encoded))
	}
	if encoded[0] != TypeMessage {
		t.Errorf("type byte: got %d, want %d", encoded[0], TypeMessage)
	}
	if encoded[1] != 0x00 {
		t.Errorf("format code: got %d, want 0", encoded[1])
	}
	if !bytes.Equal(encoded[2:34], hash) {
		t.Error("hash mismatch")
	}
}

// TestEncodeMessageBendyButt tests encoding bendybutt-v1 message references.
func TestEncodeMessageBendyButt(t *testing.T) {
	hash := bytes.Repeat([]byte("y"), 32)
	encoded := EncodeMessage("bendybutt-v1", hash)

	if encoded[1] != 0x04 {
		t.Errorf("format code: got %d, want 4", encoded[1])
	}
}

// TestDecodeMessageRoundTrip tests round-trip encode/decode for messages.
func TestDecodeMessageRoundTrip(t *testing.T) {
	tests := []struct {
		algo string
		hash []byte
	}{
		{"sha256", bytes.Repeat([]byte{1}, 32)},
		{"gabbygrove-v1", bytes.Repeat([]byte{2}, 32)},
		{"cloaked", bytes.Repeat([]byte{3}, 32)},
		{"bamboo", bytes.Repeat([]byte{4}, 32)},
		{"bendybutt-v1", bytes.Repeat([]byte{5}, 32)},
		{"buttwoo-v1", bytes.Repeat([]byte{6}, 32)},
		{"indexed-v1", bytes.Repeat([]byte{7}, 32)},
	}

	for _, tt := range tests {
		t.Run(tt.algo, func(t *testing.T) {
			encoded := EncodeMessage(tt.algo, tt.hash)
			algo, hash, err := DecodeMessage(encoded)
			if err != nil {
				t.Fatalf("DecodeMessage failed: %v", err)
			}
			if algo != tt.algo {
				t.Errorf("algo mismatch: got %q, want %q", algo, tt.algo)
			}
			if !bytes.Equal(hash, tt.hash) {
				t.Error("hash mismatch")
			}
		})
	}
}

// TestDecodeMessageInvalidType tests error on wrong type byte.
func TestDecodeMessageInvalidType(t *testing.T) {
	badData := make([]byte, 34)
	badData[0] = 0xFF // wrong type

	_, _, err := DecodeMessage(badData)
	if err != ErrInvalidBFE {
		t.Errorf("expected ErrInvalidBFE, got %v", err)
	}
}

// TestDecodeMessageTooShort tests error on undersized input.
func TestDecodeMessageTooShort(t *testing.T) {
	_, _, err := DecodeMessage([]byte("short"))
	if err != ErrInvalidBFE {
		t.Errorf("expected ErrInvalidBFE, got %v", err)
	}
}

// TestDecodeMessageUnknownFormat tests error on unknown format code.
func TestDecodeMessageUnknownFormat(t *testing.T) {
	badData := make([]byte, 34)
	badData[0] = TypeMessage
	badData[1] = 0xFF // unknown format code

	_, _, err := DecodeMessage(badData)
	if err != ErrInvalidFormat {
		t.Errorf("expected ErrInvalidFormat, got %v", err)
	}
}

// TestEncodeBlobSHA256 tests encoding blob references.
func TestEncodeBlobSHA256(t *testing.T) {
	hash := bytes.Repeat([]byte("z"), 32)
	encoded := EncodeBlob(hash)

	if len(encoded) != 34 {
		t.Errorf("encoded length: got %d, want 34", len(encoded))
	}
	if encoded[0] != TypeBlob {
		t.Errorf("type byte: got %d, want %d", encoded[0], TypeBlob)
	}
	if encoded[1] != 0x00 {
		t.Errorf("format code: got %d, want 0", encoded[1])
	}
	if !bytes.Equal(encoded[2:34], hash) {
		t.Error("hash mismatch")
	}
}

// TestDecodeBlobRoundTrip tests round-trip encode/decode for blobs.
func TestDecodeBlobRoundTrip(t *testing.T) {
	hash := bytes.Repeat([]byte{42}, 32)
	encoded := EncodeBlob(hash)

	decoded, err := DecodeBlob(encoded)
	if err != nil {
		t.Fatalf("DecodeBlob failed: %v", err)
	}

	if !bytes.Equal(decoded, hash) {
		t.Error("hash mismatch")
	}
}

// TestDecodeBlobInvalidType tests error on wrong type byte.
func TestDecodeBlobInvalidType(t *testing.T) {
	badData := make([]byte, 34)
	badData[0] = 0xFF // wrong type

	_, err := DecodeBlob(badData)
	if err != ErrInvalidBFE {
		t.Errorf("expected ErrInvalidBFE, got %v", err)
	}
}

// TestDecodeBlobTooShort tests error on undersized input.
func TestDecodeBlobTooShort(t *testing.T) {
	_, err := DecodeBlob([]byte("short"))
	if err != ErrInvalidBFE {
		t.Errorf("expected ErrInvalidBFE, got %v", err)
	}
}

// TestDecodeBlobInvalidFormat tests error on invalid format code.
func TestDecodeBlobInvalidFormat(t *testing.T) {
	badData := make([]byte, 34)
	badData[0] = TypeBlob
	badData[1] = 0xFF // invalid format code

	_, err := DecodeBlob(badData)
	if err != ErrInvalidFormat {
		t.Errorf("expected ErrInvalidFormat, got %v", err)
	}
}

// TestEncodeSignature tests encoding signature references.
func TestEncodeSignature(t *testing.T) {
	sig := bytes.Repeat([]byte("s"), 64)
	encoded := EncodeSignature(sig)

	expectedLen := 2 + 64
	if len(encoded) != expectedLen {
		t.Errorf("encoded length: got %d, want %d", len(encoded), expectedLen)
	}
	if encoded[0] != TypeSignature {
		t.Errorf("type byte: got %d, want %d", encoded[0], TypeSignature)
	}
	if encoded[1] != 0x00 {
		t.Errorf("format code: got %d, want 0", encoded[1])
	}
	if !bytes.Equal(encoded[2:66], sig) {
		t.Error("signature mismatch")
	}
}

// TestDecodeSignatureRoundTrip tests round-trip encode/decode for signatures.
func TestDecodeSignatureRoundTrip(t *testing.T) {
	sig := bytes.Repeat([]byte{99}, 64)
	encoded := EncodeSignature(sig)

	decoded, err := DecodeSignature(encoded)
	if err != nil {
		t.Fatalf("DecodeSignature failed: %v", err)
	}

	if !bytes.Equal(decoded, sig) {
		t.Error("signature mismatch")
	}
}

// TestDecodeSignatureInvalidType tests error on wrong type byte.
func TestDecodeSignatureInvalidType(t *testing.T) {
	badData := make([]byte, 66)
	badData[0] = 0xFF // wrong type

	_, err := DecodeSignature(badData)
	if err != ErrInvalidBFE {
		t.Errorf("expected ErrInvalidBFE, got %v", err)
	}
}

// TestDecodeSignatureTooShort tests error on undersized input.
func TestDecodeSignatureTooShort(t *testing.T) {
	_, err := DecodeSignature([]byte("short"))
	if err != ErrInvalidBFE {
		t.Errorf("expected ErrInvalidBFE, got %v", err)
	}
}

// TestDecodeSignatureInvalidFormat tests error on invalid format code.
func TestDecodeSignatureInvalidFormat(t *testing.T) {
	badData := make([]byte, 66)
	badData[0] = TypeSignature
	badData[1] = 0xFF // invalid format code

	_, err := DecodeSignature(badData)
	if err != ErrInvalidFormat {
		t.Errorf("expected ErrInvalidFormat, got %v", err)
	}
}

// TestEncodeNil tests encoding nil values.
func TestEncodeNil(t *testing.T) {
	encoded := EncodeNil()

	if len(encoded) != 2 {
		t.Errorf("encoded length: got %d, want 2", len(encoded))
	}
	if encoded[0] != TypeGeneric {
		t.Errorf("type byte: got %d, want %d", encoded[0], TypeGeneric)
	}
	if encoded[1] != 0x02 {
		t.Errorf("format code: got %d, want 2", encoded[1])
	}
}

// TestEncodeString tests encoding UTF-8 strings.
func TestEncodeString(t *testing.T) {
	s := "Hello, World!"
	encoded := EncodeString(s)

	expectedLen := 2 + len(s)
	if len(encoded) != expectedLen {
		t.Errorf("encoded length: got %d, want %d", len(encoded), expectedLen)
	}
	if encoded[0] != TypeGeneric {
		t.Errorf("type byte: got %d, want %d", encoded[0], TypeGeneric)
	}
	if encoded[1] != 0x00 {
		t.Errorf("format code: got %d, want 0", encoded[1])
	}
	if string(encoded[2:]) != s {
		t.Errorf("string mismatch: got %q, want %q", string(encoded[2:]), s)
	}
}

// TestDecodeStringRoundTrip tests round-trip encode/decode for strings.
func TestDecodeStringRoundTrip(t *testing.T) {
	tests := []string{
		"",
		"a",
		"Hello, World!",
		"UTF-8 with émojis 🎉",
		"multiline\nstring",
	}

	for _, s := range tests {
		t.Run(s, func(t *testing.T) {
			encoded := EncodeString(s)
			decoded, err := DecodeString(encoded)
			if err != nil {
				t.Fatalf("DecodeString failed: %v", err)
			}
			if decoded != s {
				t.Errorf("string mismatch: got %q, want %q", decoded, s)
			}
		})
	}
}

// TestDecodeStringInvalidType tests error on wrong type byte.
func TestDecodeStringInvalidType(t *testing.T) {
	badData := []byte{0xFF, 0x00, 'h', 'e', 'l', 'l', 'o'}
	_, err := DecodeString(badData)
	if err != ErrInvalidFormat {
		t.Errorf("expected ErrInvalidFormat, got %v", err)
	}
}

// TestDecodeStringInvalidFormat tests error on wrong format code.
func TestDecodeStringInvalidFormat(t *testing.T) {
	badData := []byte{TypeGeneric, 0xFF, 'h', 'e', 'l', 'l', 'o'}
	_, err := DecodeString(badData)
	if err != ErrInvalidFormat {
		t.Errorf("expected ErrInvalidFormat, got %v", err)
	}
}

// TestDecodeStringTooShort tests error on undersized input.
func TestDecodeStringTooShort(t *testing.T) {
	_, err := DecodeString([]byte("x"))
	if err != ErrInvalidBFE {
		t.Errorf("expected ErrInvalidBFE, got %v", err)
	}
}

// TestDecodeStringEmpty tests decoding empty string.
func TestDecodeStringEmpty(t *testing.T) {
	encoded := EncodeString("")
	decoded, err := DecodeString(encoded)
	if err != nil {
		t.Fatalf("DecodeString failed: %v", err)
	}
	if decoded != "" {
		t.Errorf("expected empty string, got %q", decoded)
	}
}

// TestFeedFormatCodeMap tests feed format code mappings.
func TestFeedFormatCodeMap(t *testing.T) {
	if len(FeedFormatCodes) != len(FeedFormatCodesReverse) {
		t.Error("FeedFormatCodes and FeedFormatCodesReverse have different lengths")
	}

	for algo, code := range FeedFormatCodes {
		if FeedFormatCodesReverse[code] != algo {
			t.Errorf("roundtrip failed for feed algo %q", algo)
		}
	}
}

// TestMessageFormatCodeMap tests message format code mappings.
func TestMessageFormatCodeMap(t *testing.T) {
	if len(MessageFormatCodes) != len(MessageFormatCodesReverse) {
		t.Error("MessageFormatCodes and MessageFormatCodesReverse have different lengths")
	}

	for algo, code := range MessageFormatCodes {
		if MessageFormatCodesReverse[code] != algo {
			t.Errorf("roundtrip failed for message algo %q", algo)
		}
	}
}

// TestGenericFormatCodeMap tests generic format code mappings.
func TestGenericFormatCodeMap(t *testing.T) {
	if len(GenericFormatCodes) != len(GenericFormatCodesReverse) {
		t.Error("GenericFormatCodes and GenericFormatCodesReverse have different lengths")
	}

	for algo, code := range GenericFormatCodes {
		if GenericFormatCodesReverse[code] != algo {
			t.Errorf("roundtrip failed for generic format %q", algo)
		}
	}
}

// TestTypeConstants tests that type constants are distinct.
func TestTypeConstants(t *testing.T) {
	types := []byte{
		TypeFeed,
		TypeMessage,
		TypeBlob,
		TypeEncKey,
		TypeSignature,
		TypeEncrypted,
		TypeGeneric,
		TypeIdentity,
	}

	seen := make(map[byte]bool)
	for _, typ := range types {
		if seen[typ] {
			t.Errorf("duplicate type constant: %d", typ)
		}
		seen[typ] = true
	}
}

// BenchmarkEncodeFeed benchmarks feed encoding.
func BenchmarkEncodeFeed(b *testing.B) {
	pubKey := bytes.Repeat([]byte("a"), 32)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EncodeFeed("ed25519", pubKey)
	}
}

// BenchmarkDecodeFeed benchmarks feed decoding.
func BenchmarkDecodeFeed(b *testing.B) {
	pubKey := bytes.Repeat([]byte("a"), 32)
	encoded := EncodeFeed("ed25519", pubKey)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeFeed(encoded)
	}
}

// BenchmarkEncodeMessage benchmarks message encoding.
func BenchmarkEncodeMessage(b *testing.B) {
	hash := bytes.Repeat([]byte("x"), 32)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EncodeMessage("sha256", hash)
	}
}

// BenchmarkDecodeMessage benchmarks message decoding.
func BenchmarkDecodeMessage(b *testing.B) {
	hash := bytes.Repeat([]byte("x"), 32)
	encoded := EncodeMessage("sha256", hash)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeMessage(encoded)
	}
}

// BenchmarkEncodeString benchmarks string encoding.
func BenchmarkEncodeString(b *testing.B) {
	s := "Hello, World!"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EncodeString(s)
	}
}

// BenchmarkDecodeString benchmarks string decoding.
func BenchmarkDecodeString(b *testing.B) {
	encoded := EncodeString("Hello, World!")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DecodeString(encoded)
	}
}
