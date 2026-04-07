package gossip

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/network"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/replication"
	"golang.org/x/crypto/ed25519"
)

type PeerInfo struct {
	Addr   string
	PubKey ed25519.PublicKey
}

type Database interface {
	AddKnownPeer(ctx context.Context, addr string, pubKey []byte) error
	GetKnownPeers(ctx context.Context) ([]PeerInfo, error)
}

type Manager struct {
	logger *log.Logger
	net    *network.Client
	ebt    *replication.EBTHandler
	mux    muxrpc.Handler // The full handler mux to register on outgoing conns
	db     Database

	mu    sync.Mutex
	peers map[string]PeerInfo
	conns map[string]*network.Peer
}

func NewManager(net *network.Client, ebt *replication.EBTHandler, mux muxrpc.Handler, database Database, logger *log.Logger) *Manager {
	if logger == nil {
		logger = log.Default()
	}
	return &Manager{
		logger: logger,
		net:    net,
		ebt:    ebt,
		mux:    mux,
		db:     database,
		peers:  make(map[string]PeerInfo),
		conns:  make(map[string]*network.Peer),
	}
}

func (m *Manager) AddPeer(ctx context.Context, addr string, pubKey ed25519.PublicKey) error {
	m.mu.Lock()
	m.peers[addr] = PeerInfo{Addr: addr, PubKey: pubKey}
	m.mu.Unlock()

	if m.db != nil {
		return m.db.AddKnownPeer(ctx, addr, []byte(pubKey))
	}
	return nil
}

func (m *Manager) Run(ctx context.Context) {
	// Load initial peers from DB
	if m.db != nil {
		peers, err := m.db.GetKnownPeers(ctx)
		if err == nil {
			m.mu.Lock()
			for _, p := range peers {
				m.peers[p.Addr] = p
			}
			m.mu.Unlock()
		}
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.reconnect(ctx)
		case <-pingTicker.C:
			m.pingPeers(ctx)
		}
	}
}

func (m *Manager) pingPeers(ctx context.Context) {
	m.mu.Lock()
	conns := make(map[string]*network.Peer)
	for addr, peer := range m.conns {
		conns[addr] = peer
	}
	m.mu.Unlock()

	for addr, peer := range conns {
		if peer.RPC() == nil {
			continue
		}
		go func(a string, p *network.Peer) {
			start := time.Now()
			var res int64
			err := p.RPC().Async(ctx, &res, muxrpc.TypeJSON, muxrpc.Method{"gossip", "ping"})
			if err == nil {
				latency := time.Since(start)
				p.SetLatency(latency)
			} else {
				m.logger.Printf("gossip: ping failed for %s: %v", a, err)
			}
		}(addr, peer)
	}
}

func (m *Manager) Connect(ctx context.Context, addr string, pubKey ed25519.PublicKey) (*network.Peer, error) {
	m.logger.Printf("gossip: attempting to connect to %s", addr)
	peer, err := m.net.Connect(ctx, addr, pubKey, m.mux)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.conns[addr] = peer
	m.mu.Unlock()

	// Start EBT replication
	go func() {
		// We initiate the duplex call
		src, sink, err := peer.RPC().Duplex(ctx, muxrpc.TypeJSON, muxrpc.Method{"ebt", "replicate"}, 3)
		if err != nil {
			m.logger.Printf("gossip: failed to initiate ebt.replicate on %s: %v", addr, err)
			return
		}

		// Now we need to link this duplex stream to our local EBT handler.
		rx := muxrpc.NewByteSourceAdapter(src)
		tx := muxrpc.NewByteSinkWriter(sink)

		// Get remote feed ref for the handler
		remoteFeed, _ := network.GetFeedRefFromAddr(peer.Conn.RemoteAddr())

		if err := m.ebt.HandleDuplex(ctx, tx, rx, addr, remoteFeed); err != nil {
			m.logger.Printf("gossip: ebt replication error on %s: %v", addr, err)
		}
	}()

	return peer, nil
}

func (m *Manager) reconnect(ctx context.Context) {
	m.mu.Lock()
	// Clean up closed connections
	for addr, peer := range m.conns {
		if peer.Conn == nil {
			delete(m.conns, addr)
			continue
		}
	}

	peers := make([]PeerInfo, 0, len(m.peers))
	for addr, info := range m.peers {
		if _, connected := m.conns[addr]; !connected {
			peers = append(peers, info)
		}
	}
	m.mu.Unlock()

	for _, p := range peers {
		go func(info PeerInfo) {
			_, err := m.Connect(ctx, info.Addr, info.PubKey)
			if err != nil {
				m.logger.Printf("gossip: failed to connect to %s: %v", info.Addr, err)
			}
		}(p)
	}
}
