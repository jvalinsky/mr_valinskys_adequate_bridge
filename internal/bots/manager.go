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
	keyPairs   map[string]*keys.KeyPair
}

func NewManager(masterSeed []byte, rxLog feedlog.Log, users feedlog.MultiLog) *Manager {
	return &Manager{
		publishers: make(map[string]*publisher.Publisher),
		masterSeed: masterSeed,
		receiveLog: rxLog,
		userFeeds:  users,
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

	pub, err := publisher.New(kp, m.receiveLog, m.userFeeds)
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

// GetKeyPair returns the deterministic keypair for an ATProto DID.
func (m *Manager) GetKeyPair(atDID string) (*keys.KeyPair, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	kp, err := m.deriveKeyPair(atDID)
	if err != nil {
		return nil, err
	}
	return kp, nil
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
