package bots

import (
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
)

type mockLog struct{}

func (m *mockLog) Seq() (int64, error) { return 0, nil }
func (m *mockLog) Append(content []byte, metadata *feedlog.Metadata) (int64, error) {
	return 1, nil
}
func (m *mockLog) Get(seq int64) (*feedlog.StoredMessage, error) { return nil, nil }
func (m *mockLog) Query(specs ...feedlog.QuerySpec) (feedlog.Source, error) {
	return nil, nil
}
func (m *mockLog) Close() error { return nil }

type mockMultiLog struct{}

func (m *mockMultiLog) List() ([]string, error) { return nil, nil }
func (m *mockMultiLog) Get(author string) (feedlog.Log, error) {
	return &mockLog{}, nil
}
func (m *mockMultiLog) Create(author string) (feedlog.Log, error) {
	return &mockLog{}, nil
}
func (m *mockMultiLog) Has(author string) (bool, error) { return false, nil }
func (m *mockMultiLog) Close() error                    { return nil }

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

	if string(kp1.Private()) != string(kp2.Private()) {
		t.Errorf("expected deterministic keys")
	}

	if kp1.FeedRef().String() != kp2.FeedRef().String() {
		t.Errorf("expected matching Feed IDs")
	}

	kp3, _ := manager.deriveKeyPair("did:plc:different")
	if kp1.FeedRef().String() == kp3.FeedRef().String() {
		t.Errorf("expected different Feed IDs for different DIDs")
	}
}

func TestGetFeedID(t *testing.T) {
	masterSeed := []byte("test_master_seed_for_bridge_bot_manager")
	manager := NewManager(masterSeed, nil, nil, nil)

	atDID := "did:plc:test123"
	feedRef, err := manager.GetFeedID(atDID)
	if err != nil {
		t.Fatalf("failed to get feed ID: %v", err)
	}

	if feedRef.String() == "" {
		t.Errorf("expected non-empty feed ID")
	}

	feedRef2, err := manager.GetFeedID(atDID)
	if err != nil {
		t.Fatalf("failed to get feed ID second time: %v", err)
	}

	if feedRef.String() != feedRef2.String() {
		t.Errorf("expected same feed ID for same DID")
	}
}

func TestGetPublisher(t *testing.T) {
	masterSeed := []byte("test_master_seed_for_bridge_bot_manager")
	rxLog := &mockLog{}
	users := &mockMultiLog{}

	manager := NewManager(masterSeed, rxLog, users, nil)

	atDID := "did:plc:publisher123"
	pub, err := manager.GetPublisher(atDID)
	if err != nil {
		t.Fatalf("failed to get publisher: %v", err)
	}
	if pub == nil {
		t.Fatalf("expected non-nil publisher")
	}

	pub2, err := manager.GetPublisher(atDID)
	if err != nil {
		t.Fatalf("failed to get publisher second time: %v", err)
	}

	if pub != pub2 {
		t.Errorf("expected same publisher instance for same DID")
	}
}

func TestGetPublisherWithHMAC(t *testing.T) {
	masterSeed := []byte("test_master_seed_for_bridge_bot_manager")
	rxLog := &mockLog{}
	users := &mockMultiLog{}
	hmacKey := &[32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}

	manager := NewManager(masterSeed, rxLog, users, hmacKey)

	atDID := "did:plc:hmac123"
	pub, err := manager.GetPublisher(atDID)
	if err != nil {
		t.Fatalf("failed to get publisher with HMAC: %v", err)
	}
	if pub == nil {
		t.Fatalf("expected non-nil publisher with HMAC")
	}
}

func TestGetFeedIDDifferentDIDs(t *testing.T) {
	masterSeed := []byte("test_master_seed_for_bridge_bot_manager")
	manager := NewManager(masterSeed, nil, nil, nil)

	dids := []string{
		"did:plc:aaa111",
		"did:plc:bbb222",
		"did:plc:ccc333",
	}

	feedRefs := make(map[string]string)
	for _, did := range dids {
		feedRef, err := manager.GetFeedID(did)
		if err != nil {
			t.Fatalf("failed to get feed ID for %s: %v", did, err)
		}
		feedRefs[did] = feedRef.String()
	}

	for i, did1 := range dids {
		for _, did2 := range dids[i+1:] {
			if feedRefs[did1] == feedRefs[did2] {
				t.Errorf("expected different feed IDs for different DIDs")
			}
		}
	}
}

type errorLog struct{}

func (e *errorLog) Seq() (int64, error) { return 0, nil }
func (e *errorLog) Append(content []byte, metadata *feedlog.Metadata) (int64, error) {
	return 0, nil
}
func (e *errorLog) Get(seq int64) (*feedlog.StoredMessage, error) { return nil, nil }
func (e *errorLog) Query(specs ...feedlog.QuerySpec) (feedlog.Source, error) {
	return nil, nil
}
func (e *errorLog) Close() error { return nil }

type errorMultiLog struct{}

func (e *errorMultiLog) List() ([]string, error) { return nil, nil }
func (e *errorMultiLog) Get(author string) (feedlog.Log, error) {
	return nil, feedlog.ErrNotFound
}
func (e *errorMultiLog) Create(author string) (feedlog.Log, error) {
	return nil, feedlog.ErrNotFound
}
func (e *errorMultiLog) Has(author string) (bool, error) { return false, nil }
func (e *errorMultiLog) Close() error                    { return nil }

func TestGetPublisherWithError(t *testing.T) {
	masterSeed := []byte("test_master_seed_for_bridge_bot_manager")
	rxLog := &errorLog{}
	users := &errorMultiLog{}

	manager := NewManager(masterSeed, rxLog, users, nil)

	atDID := "did:plc:error123"
	_, err := manager.GetPublisher(atDID)
	if err == nil {
		t.Fatalf("expected error when user log returns error")
	}
}
