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

type AttendantEvent struct {
	Type string
	ID   refs.FeedRef
}

type TunnelEvent struct {
	Type string
	Info PeerInfo
}

type Manager struct {
	mu          sync.RWMutex
	peers       map[string]PeerInfo
	attendants  map[string]PeerInfo
	aliases     map[string][]refs.FeedRef
	subscribers map[chan AttendantEvent]struct{}
	tunnelSubs  map[chan TunnelEvent]struct{}
}

func NewManager() *Manager {
	return &Manager{
		peers:       make(map[string]PeerInfo),
		attendants:  make(map[string]PeerInfo),
		aliases:     make(map[string][]refs.FeedRef),
		subscribers: make(map[chan AttendantEvent]struct{}),
		tunnelSubs:  make(map[chan TunnelEvent]struct{}),
	}
}

func (m *Manager) AddPeer(id refs.FeedRef, addr string) {
	m.mu.Lock()
	info := PeerInfo{
		ID:        id,
		Addr:      addr,
		Connected: time.Now(),
	}
	m.peers[id.String()] = info
	subscribers := m.snapshotTunnelSubscribersLocked()
	m.mu.Unlock()

	m.broadcastTunnel(subscribers, TunnelEvent{Type: "joined", Info: info})
}

func (m *Manager) RemovePeer(id refs.FeedRef) {
	m.mu.Lock()
	info, existed := m.peers[id.String()]
	delete(m.peers, id.String())
	subscribers := m.snapshotTunnelSubscribersLocked()
	m.mu.Unlock()

	if existed {
		m.broadcastTunnel(subscribers, TunnelEvent{Type: "left", Info: info})
	}
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

func (m *Manager) AddAttendant(id refs.FeedRef, addr string) {
	m.mu.Lock()
	_, existed := m.attendants[id.String()]
	m.attendants[id.String()] = PeerInfo{
		ID:        id,
		Addr:      addr,
		Connected: time.Now(),
	}
	subscribers := m.snapshotSubscribersLocked()
	m.mu.Unlock()

	if !existed {
		m.broadcast(subscribers, AttendantEvent{Type: "joined", ID: id})
	}
}

func (m *Manager) RemoveAttendant(id refs.FeedRef) {
	m.mu.Lock()
	_, existed := m.attendants[id.String()]
	delete(m.attendants, id.String())
	subscribers := m.snapshotSubscribersLocked()
	m.mu.Unlock()

	if existed {
		m.broadcast(subscribers, AttendantEvent{Type: "left", ID: id})
	}
}

func (m *Manager) Attendants() []PeerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]PeerInfo, 0, len(m.attendants))
	for _, p := range m.attendants {
		result = append(result, p)
	}
	return result
}

func (m *Manager) SubscribeAttendants() ([]PeerInfo, <-chan AttendantEvent, func()) {
	ch := make(chan AttendantEvent, 16)

	m.mu.Lock()
	m.subscribers[ch] = struct{}{}
	snapshot := make([]PeerInfo, 0, len(m.attendants))
	for _, p := range m.attendants {
		snapshot = append(snapshot, p)
	}
	m.mu.Unlock()

	cancel := func() {
		m.mu.Lock()
		if _, ok := m.subscribers[ch]; ok {
			delete(m.subscribers, ch)
			close(ch)
		}
		m.mu.Unlock()
	}

	return snapshot, ch, cancel
}

func (m *Manager) SubscribeEndpoints() ([]PeerInfo, <-chan TunnelEvent, func()) {
	ch := make(chan TunnelEvent, 16)

	m.mu.Lock()
	m.tunnelSubs[ch] = struct{}{}
	snapshot := make([]PeerInfo, 0, len(m.peers))
	for _, p := range m.peers {
		snapshot = append(snapshot, p)
	}
	m.mu.Unlock()

	cancel := func() {
		m.mu.Lock()
		if _, ok := m.tunnelSubs[ch]; ok {
			delete(m.tunnelSubs, ch)
			close(ch)
		}
		m.mu.Unlock()
	}

	return snapshot, ch, cancel
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

func (m *Manager) snapshotSubscribersLocked() []chan AttendantEvent {
	subscribers := make([]chan AttendantEvent, 0, len(m.subscribers))
	for ch := range m.subscribers {
		subscribers = append(subscribers, ch)
	}
	return subscribers
}

func (m *Manager) snapshotTunnelSubscribersLocked() []chan TunnelEvent {
	subscribers := make([]chan TunnelEvent, 0, len(m.tunnelSubs))
	for ch := range m.tunnelSubs {
		subscribers = append(subscribers, ch)
	}
	return subscribers
}

func (m *Manager) broadcast(subscribers []chan AttendantEvent, event AttendantEvent) {
	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func (m *Manager) broadcastTunnel(subscribers []chan TunnelEvent, event TunnelEvent) {
	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}
