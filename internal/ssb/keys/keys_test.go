package keys

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerate(t *testing.T) {
	kp, err := Generate()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	pub := kp.Public()
	if len(pub) != 32 {
		t.Errorf("expected 32-byte public key, got %d", len(pub))
	}

	seed := kp.Seed()
	if len(seed) != 32 {
		t.Errorf("expected 32-byte seed, got %d", len(seed))
	}
}

func TestFromSeed(t *testing.T) {
	var seed [32]byte
	seed[0] = 1
	seed[1] = 2

	kp1 := FromSeed(seed)
	kp2 := FromSeed(seed)

	if kp1.Public() != kp2.Public() {
		t.Errorf("same seed should produce same public key")
	}
}

func TestSignVerify(t *testing.T) {
	kp, err := Generate()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	msg := []byte("test message")
	sig, err := kp.Sign(msg)
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}

	if !kp.Verify(msg, sig) {
		t.Errorf("signature verification failed")
	}

	if kp.Verify([]byte("wrong message"), sig) {
		t.Errorf("should not verify wrong message")
	}
}

func TestSignWithHMAC(t *testing.T) {
	kp, err := Generate()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	msg := []byte("test message")
	hmacKey := []byte("hmac-secret-key")

	sig, err := SignWithHMAC(kp, msg, hmacKey)
	if err != nil {
		t.Fatalf("failed to sign with HMAC: %v", err)
	}

	pub := kp.Public()
	if !VerifyWithHMAC(pub[:], msg, sig, hmacKey) {
		t.Errorf("HMAC signature verification failed")
	}

	if VerifyWithHMAC(pub[:], []byte("wrong message"), sig, hmacKey) {
		t.Errorf("should not verify wrong message")
	}

	if VerifyWithHMAC(pub[:], msg, sig, []byte("wrong hmac key")) {
		t.Errorf("should not verify with wrong HMAC key")
	}
}

func TestCurve25519Conversion(t *testing.T) {
	kp, err := Generate()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	pub, priv := kp.ToCurve25519()

	if len(pub) != 32 {
		t.Errorf("expected 32-byte curve25519 public key, got %d", len(pub))
	}

	if len(priv) != 32 {
		t.Errorf("expected 32-byte curve25519 private key, got %d", len(priv))
	}
}

func TestCurve25519Public_Deterministic(t *testing.T) {
	var testKey [32]byte
	testKey[0] = 9
	testKey[31] = 128

	result1 := Curve25519Public(testKey)
	result2 := Curve25519Public(testKey)

	if result1 != result2 {
		t.Errorf("same input should produce same output")
	}
}

func TestCurve25519Public_NotAllZero(t *testing.T) {
	var testKey [32]byte
	testKey[0] = 9
	testKey[31] = 128

	result := Curve25519Public(testKey)

	allZero := true
	for _, b := range result {
		if b != 0 {
			allZero = false
			break
		}
	}

	if allZero {
		t.Errorf("conversion should not produce all-zero output for non-zero input")
	}
}

func TestParseSecret(t *testing.T) {
	kp, err := Generate()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	var buf bytes.Buffer
	if err := EncodeSecret(kp, &buf); err != nil {
		t.Fatalf("failed to encode secret: %v", err)
	}

	parsed, err := ParseSecret(&buf)
	if err != nil {
		t.Fatalf("failed to parse secret: %v", err)
	}

	if kp.Public() != parsed.Public() {
		t.Errorf("parsed key pair has different public key")
	}

	if !bytes.Equal(kp.Private(), parsed.Private()) {
		t.Errorf("parsed key pair has different private key")
	}
}

func TestSecureCompare(t *testing.T) {
	a := []byte("test")
	b := []byte("test")
	c := []byte("other")

	if !SecureCompare(a, b) {
		t.Errorf("should be equal")
	}

	if SecureCompare(a, c) {
		t.Errorf("should not be equal")
	}
}

func TestFromSeedString(t *testing.T) {
	seed := make([]byte, 32)
	seed[0] = 1
	seed[1] = 2

	kp := FromSeed(*(*[32]byte)(seed))

	seedStr := base64.StdEncoding.EncodeToString(seed)
	kp2, err := FromSeedString(seedStr)
	if err != nil {
		t.Fatalf("failed to parse seed string: %v", err)
	}

	if kp.Public() != kp2.Public() {
		t.Errorf("parsed key pair doesn't match")
	}
}

func TestSaveDoesNotOverwriteExistingSecret(t *testing.T) {
	tempDir := t.TempDir()
	secretPath := filepath.Join(tempDir, "secret")

	first, err := Generate()
	if err != nil {
		t.Fatalf("generate first key pair: %v", err)
	}
	second, err := Generate()
	if err != nil {
		t.Fatalf("generate second key pair: %v", err)
	}

	if err := Save(first, secretPath); err != nil {
		t.Fatalf("save first secret: %v", err)
	}
	if err := Save(second, secretPath); err == nil {
		t.Fatal("expected second save to fail for existing secret")
	}

	loaded, err := Load(secretPath)
	if err != nil {
		t.Fatalf("load existing secret: %v", err)
	}
	if loaded.Public() != first.Public() {
		t.Fatal("existing secret was overwritten")
	}

	if _, err := os.Stat(secretPath); err != nil {
		t.Fatalf("secret path missing after failed save: %v", err)
	}
}
