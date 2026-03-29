package bots

import (
	"testing"
)

func TestDeriveKeyPair(t *testing.T) {
	masterSeed := []byte("test_master_seed_for_bridge_bot_manager")

	manager := NewManager(masterSeed, nil, nil, nil)

	atDID := "did:plc:abc123def456"

	kp1, err := manager.deriveKeyPair(atDID)
	if err != nil {
		t.Fatalf("failed to derive key: %v", err)
	}

	kp2, err := manager.deriveKeyPair(atDID)
	if err != nil {
		t.Fatalf("failed to derive key 2: %v", err)
	}

	// They should be identical and deterministic
	if string(kp1.Private()) != string(kp2.Private()) {
		t.Errorf("expected deterministic keys")
	}

	if kp1.FeedRef().String() != kp2.FeedRef().String() {
		t.Errorf("expected matching Feed IDs")
	}

	// Different DID should produce different key
	kp3, _ := manager.deriveKeyPair("did:plc:different")
	if kp1.FeedRef().String() == kp3.FeedRef().String() {
		t.Errorf("expected different Feed IDs for different DIDs")
	}

	t.Logf("Derived Feed ID for %s: %s", atDID, kp1.FeedRef().String())
}
