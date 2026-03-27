// Package bots manages deterministic SSB identities derived from ATProto DIDs.
package bots

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"sync"

	"go.cryptoscope.co/margaret"
	"go.cryptoscope.co/margaret/multilog"
	"go.cryptoscope.co/ssb"
	"go.cryptoscope.co/ssb/message"
	refs "go.mindeco.de/ssb-refs"
)

// Manager derives per-DID keypairs and caches corresponding publishers.
type Manager struct {
	mu         sync.Mutex
	publishers map[string]ssb.Publisher
	masterSeed []byte
	receiveLog margaret.Log
	userFeeds  multilog.MultiLog
	hmacKey    *[32]byte
}

type botKeyPair struct {
	id   refs.FeedRef
	pair ed25519.PrivateKey
}

func (b botKeyPair) ID() refs.FeedRef {
	return b.id
}

func (b botKeyPair) Secret() ed25519.PrivateKey {
	return b.pair
}

// NewManager creates a new bot manager that derives keys from masterSeed
// and uses the provided logs to publish messages.
func NewManager(masterSeed []byte, rxLog margaret.Log, users multilog.MultiLog, hmacKey *[32]byte) *Manager {
	return &Manager{
		publishers: make(map[string]ssb.Publisher),
		masterSeed: masterSeed,
		receiveLog: rxLog,
		userFeeds:  users,
		hmacKey:    hmacKey,
	}
}

// GetPublisher returns a publisher for the given AT DID, generating it if needed.
func (m *Manager) GetPublisher(atDID string) (ssb.Publisher, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if pub, ok := m.publishers[atDID]; ok {
		return pub, nil
	}

	kp, err := m.deriveKeyPair(atDID)
	if err != nil {
		return nil, fmt.Errorf("failed to derive keypair: %w", err)
	}

	opts := []message.PublishOption{}
	if m.hmacKey != nil {
		opts = append(opts, message.SetHMACKey(m.hmacKey))
	}

	pub, err := message.OpenPublishLog(m.receiveLog, m.userFeeds, kp, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to open publish log: %w", err)
	}

	m.publishers[atDID] = pub
	return pub, nil
}

// GetFeedID returns the SSB feed ID for atDID without creating a publisher.
func (m *Manager) GetFeedID(atDID string) (refs.FeedRef, error) {
	kp, err := m.deriveKeyPair(atDID)
	if err != nil {
		return refs.FeedRef{}, err
	}
	return kp.ID(), nil
}

// deriveKeyPair deterministically derives an Ed25519 keypair from atDID.
func (m *Manager) deriveKeyPair(atDID string) (ssb.KeyPair, error) {
	mac := hmac.New(sha256.New, m.masterSeed)
	mac.Write([]byte(atDID))
	seed := mac.Sum(nil)

	// Ed25519 seeds are always 32 bytes.
	privKey := ed25519.NewKeyFromSeed(seed[:ed25519.SeedSize])
	pubKey := privKey.Public().(ed25519.PublicKey)

	feedRef, err := refs.NewFeedRefFromBytes(pubKey, refs.RefAlgoFeedSSB1)
	if err != nil {
		return nil, err
	}

	return botKeyPair{
		id:   feedRef,
		pair: privKey,
	}, nil
}
