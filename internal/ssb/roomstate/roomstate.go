package roomstate

import (
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

type PeerInfo struct {
	ID        refs.FeedRef
	Addr      string
	Connected time.Time
}

type Manager struct {
	mu      sync.RWMutex
	peers   map[string]PeerInfo
	aliases map[string][]refs.FeedRef
}

func NewManager() *Manager {
	return &Manager{
		peers:   make(map[string]PeerInfo),
		aliases: make(map[string][]refs.FeedRef),
	}
}

func (m *Manager) AddPeer(id refs.FeedRef, addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.peers[id.String()] = PeerInfo{
		ID:        id,
		Addr:      addr,
		Connected: time.Now(),
	}
}

func (m *Manager) RemovePeer(id refs.FeedRef) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.peers, id.String())
}

func (m *Manager) Peers() []PeerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]PeerInfo, 0, len(m.peers))
	for _, p := range m.peers {
		result = append(result, p)
	}
	return result
}

func (m *Manager) PeerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peers)
}

func (m *Manager) HasPeer(id refs.FeedRef) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.peers[id.String()]
	return ok
}

func (m *Manager) RegisterAlias(alias string, owner refs.FeedRef) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.aliases[alias] = append(m.aliases[alias], owner)
}

func (m *Manager) RevokeAlias(alias string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.aliases, alias)
}

func (m *Manager) ResolveAlias(alias string) ([]refs.FeedRef, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	owners, ok := m.aliases[alias]
	return owners, ok
}

func (m *Manager) ListAliases() map[string][]refs.FeedRef {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string][]refs.FeedRef)
	for alias, owners := range m.aliases {
		result[alias] = owners
	}
	return result
}

func (m *Manager) AliasCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.aliases)
}
