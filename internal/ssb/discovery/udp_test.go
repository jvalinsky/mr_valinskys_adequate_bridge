package discovery

import (
	"encoding/hex"
	"testing"
)

// TestEncode_NoSeed tests encoding without seed
func TestEncode_NoSeed(t *testing.T) {
	key, _ := hex.DecodeString("0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	addr := &MultiserverAddress{
		Host: "192.168.1.100",
		Port: 8008,
		Key:  key,
	}

	encoded := addr.Encode()
	expected := "net:192.168.1.100:8008~shs:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	if encoded != expected {
		t.Errorf("wrong encoding: %s", encoded)
	}
}

// TestEncode_WithSeed tests encoding with seed
func TestEncode_WithSeed(t *testing.T) {
	key, _ := hex.DecodeString("0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	seed, _ := hex.DecodeString("aabbccddee")
	addr := &MultiserverAddress{
		Host: "192.168.1.100",
		Port: 8008,
		Key:  key,
		Seed: seed,
	}

	encoded := addr.Encode()
	expected := "net:192.168.1.100:8008~shs:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20:aabbccddee"
	if encoded != expected {
		t.Errorf("wrong encoding: %s", encoded)
	}
}

// TestEncode_CustomPort tests encoding with custom port
func TestEncode_CustomPort(t *testing.T) {
	key, _ := hex.DecodeString("0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	addr := &MultiserverAddress{
		Host: "example.com",
		Port: 9999,
		Key:  key,
	}

	encoded := addr.Encode()
	expected := "net:example.com:9999~shs:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	if encoded != expected {
		t.Errorf("wrong encoding: %s", encoded)
	}
}

// TestDecode_Basic tests basic decoding
func TestDecode_Basic(t *testing.T) {
	addr := "net:192.168.1.100:8008~shs:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	m, err := DecodeMultiserverAddress(addr)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if m.Host != "192.168.1.100" {
		t.Errorf("wrong host: %s", m.Host)
	}
	if m.Port != 8008 {
		t.Errorf("wrong port: %d", m.Port)
	}
	if len(m.Key) != 32 {
		t.Errorf("wrong key length: %d", len(m.Key))
	}
	if len(m.Seed) != 0 {
		t.Errorf("expected no seed, got %d bytes", len(m.Seed))
	}
}

// TestDecode_DefaultPort tests decoding with default port (missing port)
func TestDecode_DefaultPort(t *testing.T) {
	addr := "net:192.168.1.100~shs:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	m, err := DecodeMultiserverAddress(addr)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if m.Port != 8008 {
		t.Errorf("expected default port 8008, got %d", m.Port)
	}
}

// TestDecode_WithSeed tests decoding with seed
func TestDecode_WithSeed(t *testing.T) {
	addr := "net:192.168.1.100:8008~shs:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20:aabbccddee"
	m, err := DecodeMultiserverAddress(addr)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(m.Seed) != 5 {
		t.Errorf("wrong seed length: %d", len(m.Seed))
	}
	expectedSeed, _ := hex.DecodeString("aabbccddee")
	if hex.EncodeToString(m.Seed) != hex.EncodeToString(expectedSeed) {
		t.Errorf("wrong seed value")
	}
}

// TestDecode_MissingNetPrefix tests error when missing net: prefix
func TestDecode_MissingNetPrefix(t *testing.T) {
	addr := "192.168.1.100:8008~shs:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	_, err := DecodeMultiserverAddress(addr)
	if err == nil {
		t.Fatal("expected error for missing net: prefix")
	}
}

// TestDecode_MissingShs tests error when missing ~shs marker
func TestDecode_MissingShs(t *testing.T) {
	addr := "net:192.168.1.100:8008~0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	_, err := DecodeMultiserverAddress(addr)
	if err == nil {
		t.Fatal("expected error for missing ~shs")
	}
}

// TestDecode_InvalidKeyHex tests error with invalid hex in key
func TestDecode_InvalidKeyHex(t *testing.T) {
	addr := "net:192.168.1.100:8008~shs:gggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggggg"
	_, err := DecodeMultiserverAddress(addr)
	if err == nil {
		t.Fatal("expected error for invalid key hex")
	}
}

// TestDecode_KeyTooShort tests error when key is too short
func TestDecode_KeyTooShort(t *testing.T) {
	addr := "net:192.168.1.100:8008~shs:0102030405060708090a0b0c0d0e0f1011121314151617"
	_, err := DecodeMultiserverAddress(addr)
	if err == nil {
		t.Fatal("expected error for key too short")
	}
}

// TestDecode_KeyTooLong tests error when key is too long
func TestDecode_KeyTooLong(t *testing.T) {
	addr := "net:192.168.1.100:8008~shs:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f2021"
	_, err := DecodeMultiserverAddress(addr)
	if err == nil {
		t.Fatal("expected error for key too long")
	}
}

// TestDecode_InvalidSeedHex tests error with invalid hex in seed
func TestDecode_InvalidSeedHex(t *testing.T) {
	addr := "net:192.168.1.100:8008~shs:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20:gggggg"
	_, err := DecodeMultiserverAddress(addr)
	if err == nil {
		t.Fatal("expected error for invalid seed hex")
	}
}

// TestRoundTrip_AllFields tests encoding and decoding round-trip with all fields
func TestRoundTrip_AllFields(t *testing.T) {
	key, _ := hex.DecodeString("0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	seed, _ := hex.DecodeString("aabbccddee")
	original := &MultiserverAddress{
		Host: "192.168.1.100",
		Port: 9999,
		Key:  key,
		Seed: seed,
	}

	encoded := original.Encode()
	decoded, err := DecodeMultiserverAddress(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.Host != original.Host {
		t.Errorf("host mismatch: %s != %s", decoded.Host, original.Host)
	}
	if decoded.Port != original.Port {
		t.Errorf("port mismatch: %d != %d", decoded.Port, original.Port)
	}
	if hex.EncodeToString(decoded.Key) != hex.EncodeToString(original.Key) {
		t.Errorf("key mismatch")
	}
	if hex.EncodeToString(decoded.Seed) != hex.EncodeToString(original.Seed) {
		t.Errorf("seed mismatch")
	}
}

// TestRoundTrip_NoSeed tests round-trip without seed
func TestRoundTrip_NoSeed(t *testing.T) {
	key, _ := hex.DecodeString("0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	original := &MultiserverAddress{
		Host: "example.com",
		Port: 8008,
		Key:  key,
	}

	encoded := original.Encode()
	decoded, err := DecodeMultiserverAddress(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if decoded.Host != original.Host || decoded.Port != original.Port {
		t.Errorf("host/port mismatch")
	}
	if len(decoded.Seed) != 0 {
		t.Errorf("expected no seed")
	}
}

// TestNewUDPDiscovery_NilLogger tests NewUDPDiscovery with nil logger
func TestNewUDPDiscovery_NilLogger(t *testing.T) {
	pubKey := make([]byte, 32)
	discovery := NewUDPDiscovery("127.0.0.1", pubKey, nil)
	if discovery == nil {
		t.Fatal("NewUDPDiscovery returned nil")
	}
	if discovery.logger == nil {
		t.Fatal("logger should not be nil")
	}
}

// TestDecode_EmptyString tests error with empty address string
func TestDecode_EmptyString(t *testing.T) {
	_, err := DecodeMultiserverAddress("")
	if err == nil {
		t.Fatal("expected error for empty string")
	}
}

// TestEncode_IPv6Address tests encoding with IPv6 address
func TestEncode_IPv6Address(t *testing.T) {
	key, _ := hex.DecodeString("0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	addr := &MultiserverAddress{
		Host: "[::1]",
		Port: 8008,
		Key:  key,
	}

	encoded := addr.Encode()
	if !contains(encoded, "net:[::1]:8008~shs:") {
		t.Errorf("wrong IPv6 encoding: %s", encoded)
	}
}

// TestDecode_IPv6Address tests decoding with IPv6 address
func TestDecode_IPv6Address(t *testing.T) {
	addr := "net:[::1]:8008~shs:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	m, err := DecodeMultiserverAddress(addr)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if m.Host != "[::1]" {
		t.Errorf("wrong IPv6 host: %s", m.Host)
	}
	if m.Port != 8008 {
		t.Errorf("wrong port: %d", m.Port)
	}
}

// contains is a helper function
func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
