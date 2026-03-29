// Package bots manages deterministic SSB identities derived from ATProto DIDs.
package bots

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/publisher"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

type Manager struct {
	mu         sync.Mutex
	publishers map[string]*publisher.Publisher
	masterSeed []byte
	receiveLog feedlog.Log
	userFeeds  feedlog.MultiLog
	hmacKey    *[32]byte
	keyPairs   map[string]*keys.KeyPair
}

func NewManager(masterSeed []byte, rxLog feedlog.Log, users feedlog.MultiLog, hmacKey *[32]byte) *Manager {
	return &Manager{
		publishers: make(map[string]*publisher.Publisher),
		masterSeed: masterSeed,
		receiveLog: rxLog,
		userFeeds:  users,
		hmacKey:    hmacKey,
		keyPairs:   make(map[string]*keys.KeyPair),
	}
}

func (m *Manager) GetPublisher(atDID string) (*publisher.Publisher, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if pub, ok := m.publishers[atDID]; ok {
		return pub, nil
	}

	kp, err := m.deriveKeyPair(atDID)
	if err != nil {
		return nil, fmt.Errorf("failed to derive keypair: %w", err)
	}

	var hmacKey []byte
	if m.hmacKey != nil {
		hmacKey = m.hmacKey[:]
	}

	pub, err := publisher.New(kp, m.receiveLog, m.userFeeds, publisher.WithHMAC(hmacKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create publisher: %w", err)
	}

	m.publishers[atDID] = pub
	return pub, nil
}

func (m *Manager) GetFeedID(atDID string) (refs.FeedRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	kp, err := m.deriveKeyPair(atDID)
	if err != nil {
		return refs.FeedRef{}, err
	}
	return kp.FeedRef(), nil
}

func (m *Manager) deriveKeyPair(atDID string) (*keys.KeyPair, error) {
	if atDID == "" {
		return nil, fmt.Errorf("bots: DID must not be empty")
	}

	if kp, ok := m.keyPairs[atDID]; ok {
		return kp, nil
	}

	mac := hmac.New(sha256.New, m.masterSeed)
	mac.Write([]byte(atDID))
	seed := mac.Sum(nil)

	kp := keys.FromSeed(*(*[32]byte)(seed[:32]))
	m.keyPairs[atDID] = kp
	return kp, nil
}
