package discovery

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/gossip"
	"golang.org/x/crypto/ed25519"
)

const (
	BroadcastPort   = 8008
	BroadcastAddr   = "255.255.255.255:8008"
	BroadcastPeriod = 1 * time.Second
)

type MultiserverAddress struct {
	Host string
	Port int
	Key  []byte
	Seed []byte
}

func (m *MultiserverAddress) Encode() string {
	keyHex := hex.EncodeToString(m.Key)
	var seedStr string
	if len(m.Seed) > 0 {
		seedStr = ":" + hex.EncodeToString(m.Seed)
	}
	return fmt.Sprintf("net:%s:%d~shs:%s%s", m.Host, m.Port, keyHex, seedStr)
}

func DecodeMultiserverAddress(addr string) (*MultiserverAddress, error) {
	if len(addr) < 4 || !strings.HasPrefix(addr, "net:") {
		return nil, fmt.Errorf("invalid multiserver address: missing net: prefix")
	}

	rest := addr[4:]
	var host string
	var port int
	var keyHex string
	var seedHex string

	if idx := strings.Index(rest, "~"); idx > 0 {
		hostPort := rest[:idx]
		rest = rest[idx+5:]
		if sp := strings.LastIndex(hostPort, ":"); sp > 0 {
			host = hostPort[:sp]
			fmt.Sscanf(hostPort[sp+1:], "%d", &port)
		} else {
			host = hostPort
			port = 8008
		}
	} else {
		return nil, fmt.Errorf("invalid multiserver address: missing ~shs")
	}

	if idx := strings.Index(rest, ":"); idx > 0 {
		keyHex = rest[:idx]
		seedHex = rest[idx+1:]
	} else {
		keyHex = rest
	}

	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("invalid key: %w", err)
	}

	var seed []byte
	if len(seedHex) > 0 {
		seed, err = hex.DecodeString(seedHex)
		if err != nil {
			return nil, fmt.Errorf("invalid seed: %w", err)
		}
	}

	return &MultiserverAddress{Host: host, Port: port, Key: key, Seed: seed}, nil
}

type UDPDiscovery struct {
	logger    *slog.Logger
	localAddr string
	pubKey    ed25519.PublicKey

	conn     *net.UDPConn
	callback func(addr string, pubKey ed25519.PublicKey)
	stopCh   chan struct{}
}

func NewUDPDiscovery(localAddr string, pubKey ed25519.PublicKey, logger *slog.Logger) *UDPDiscovery {
	if logger == nil {
		logger = slog.Default()
	}
	return &UDPDiscovery{
		logger:    logger,
		localAddr: localAddr,
		pubKey:    pubKey,
		stopCh:    make(chan struct{}),
	}
}

func (u *UDPDiscovery) Start(ctx context.Context, onPeer func(addr string, pubKey ed25519.PublicKey)) error {
	u.callback = onPeer

	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", BroadcastPort))
	if err != nil {
		return fmt.Errorf("resolve UDP addr: %w", err)
	}

	u.conn, err = net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("listen UDP: %w", err)
	}

	u.logger.Info("UDP discovery listening", "port", BroadcastPort)

	go u.broadcastLoop(ctx)
	go u.readLoop(ctx)

	return nil
}

func (u *UDPDiscovery) broadcastLoop(ctx context.Context) {
	ticker := time.NewTicker(BroadcastPeriod)
	defer ticker.Stop()

	msAddr := &MultiserverAddress{
		Host: u.localAddr,
		Port: BroadcastPort,
		Key:  u.pubKey,
	}
	encoded := msAddr.Encode()

	buf := make([]byte, 1024)
	n := copy(buf, encoded)

	targetAddr, err := net.ResolveUDPAddr("udp", BroadcastAddr)
	if err != nil {
		u.logger.Error("resolve broadcast addr", "error", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			u.conn.WriteToUDP(buf[:n], targetAddr)
		}
	}
}

func (u *UDPDiscovery) readLoop(ctx context.Context) {
	buf := make([]byte, 4096)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			u.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, srcAddr, err := u.conn.ReadFromUDP(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				u.logger.Error("read UDP", "error", err)
				continue
			}

			msg := string(buf[:n])
			if !strings.HasPrefix(msg, "net:") {
				continue
			}

			msAddr, err := DecodeMultiserverAddress(msg)
			if err != nil {
				u.logger.Debug("decode multiserver address", "error", err, "from", srcAddr.IP)
				continue
			}

			if len(msAddr.Key) != 32 {
				u.logger.Debug("invalid key length", "length", len(msAddr.Key))
				continue
			}

			peerPubKey := ed25519.PublicKey(msAddr.Key)
			peerAddr := fmt.Sprintf("%s:%d", srcAddr.IP.String(), msAddr.Port)

			u.logger.Debug("discovered peer", "addr", peerAddr)

			if u.callback != nil {
				u.callback(peerAddr, peerPubKey)
			}
		}
	}
}

func (u *UDPDiscovery) Stop() error {
	close(u.stopCh)
	if u.conn != nil {
		return u.conn.Close()
	}
	return nil
}

type Manager struct {
	discovery *UDPDiscovery
	gossip    *gossip.Manager
	logger    *slog.Logger
	ctx       context.Context
	cancel    context.CancelFunc
}

func NewManager(gossipMgr *gossip.Manager, localAddr string, pubKey ed25519.PublicKey, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		gossip: gossipMgr,
		logger: logger,
	}
}

func (m *Manager) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	discovery := NewUDPDiscovery("", nil, m.logger)
	err := discovery.Start(m.ctx, func(addr string, pubKey ed25519.PublicKey) {
		m.logger.Debug("UDP discovered peer", "addr", addr)
		if m.gossip != nil {
			m.gossip.AddPeer(m.ctx, addr, pubKey)
		}
	})
	if err != nil {
		return fmt.Errorf("start discovery: %w", err)
	}

	m.discovery = discovery
	return nil
}

func (m *Manager) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}
	if m.discovery != nil {
		return m.discovery.Stop()
	}
	return nil
}
