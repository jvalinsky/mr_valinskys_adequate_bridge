package network

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream"
	"golang.org/x/crypto/ed25519"
	"sync/atomic"
)

type Conn struct {
	net.Conn
	peer *Peer
}

type Peer struct {
	ID       refs.FeedRef
	Conn     net.Conn
	KeyPair  *keys.KeyPair
	rpc      *muxrpc.Server
	manifest *muxrpc.Manifest

	readBytes  atomic.Int64
	writeBytes atomic.Int64
	latency    atomic.Int64 // nanoseconds
}

func (p *Peer) ReadBytes() int64 {
	return p.readBytes.Load()
}

func (p *Peer) WriteBytes() int64 {
	return p.writeBytes.Load()
}

func (p *Peer) Latency() time.Duration {
	return time.Duration(p.latency.Load())
}

func (p *Peer) SetLatency(d time.Duration) {
	p.latency.Store(int64(d))
}

type statsConn struct {
	net.Conn
	p *Peer
}

func (s *statsConn) Read(p []byte) (n int, err error) {
	n, err = s.Conn.Read(p)
	s.p.readBytes.Add(int64(n))
	return n, err
}

func (s *statsConn) Write(p []byte) (n int, err error) {
	n, err = s.Conn.Write(p)
	s.p.writeBytes.Add(int64(n))
	return n, err
}

type secretConn struct {
	net.Conn
}

func (s *secretConn) Read(p []byte) (n int, err error) {
	return s.Conn.Read(p)
}

func (s *secretConn) Write(p []byte) (n int, err error) {
	return s.Conn.Write(p)
}

func (s *secretConn) Close() error {
	return s.Conn.Close()
}

func (s *secretConn) RemoteAddr() net.Addr {
	return s.Conn.RemoteAddr()
}

type Server struct {
	ctx     context.Context
	addr    string
	ln      net.Listener
	handler muxrpc.Handler

	keyPair *keys.KeyPair
	opts    Options

	mu    sync.RWMutex
	peers map[string]*Peer
}

type Options struct {
	ListenAddr string
	KeyPair    *keys.KeyPair
	AppKey     string
	Timeout    time.Duration
}

func NewServer(opts Options) (*Server, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 10 * time.Second
	}

	return &Server{
		addr:    opts.ListenAddr,
		keyPair: opts.KeyPair,
		opts:    opts,
		peers:   make(map[string]*Peer),
	}, nil
}

func (s *Server) Serve(ctx context.Context, handler muxrpc.Handler) error {
	s.ctx = ctx
	s.handler = handler

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("network: failed to listen: %w", err)
	}
	s.ln = ln

	go s.acceptLoop()
	return nil
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
			}
			continue
		}

		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	timeout := s.opts.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return
	}

	shs, err := secretstream.NewServer(conn, secretstream.NewAppKey(s.opts.AppKey), s.keyPair.Private())
	if err != nil {
		fmt.Printf("network: shs init failed: %v\n", err)
		return
	}

	if err := shs.Handshake(); err != nil {
		fmt.Printf("network: shs handshake failed: %v\n", err)
		return
	}

	remoteFeed, err := GetFeedRefFromAddr(shs.RemoteAddr())
	if err != nil {
		fmt.Printf("network: failed to get remote feed ref: %v\n", err)
		return
	}

	peer := &Peer{
		ID:      *remoteFeed,
		Conn:    shs,
		KeyPair: s.keyPair,
	}

	s.addPeer(peer)
	defer s.removePeer(peer)

	var secretConn muxrpc.Conn = &statsConn{Conn: shs, p: peer}

	_ = muxrpc.NewServer(s.ctx, secretConn, s.handler, s.newManifest())

	<-s.ctx.Done()
}

type secretConnWrapper struct {
	conn net.Conn
}

func (s *secretConnWrapper) Read(p []byte) (n int, err error) {
	return s.conn.Read(p)
}

func (s *secretConnWrapper) Write(p []byte) (n int, err error) {
	return s.conn.Write(p)
}

func (s *secretConnWrapper) Close() error {
	return s.conn.Close()
}

func (s *secretConnWrapper) RemoteAddr() net.Addr {
	return s.conn.RemoteAddr()
}

func (p *Peer) RPC() *muxrpc.Server {
	return p.rpc
}

func (s *Server) addPeer(p *Peer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers[p.ID.String()] = p
}

func (s *Server) removePeer(p *Peer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.peers, p.ID.String())
}

func (s *Server) Peers() []*Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Peer, 0, len(s.peers))
	for _, p := range s.peers {
		result = append(result, p)
	}
	return result
}

func (s *Server) newManifest() *muxrpc.Manifest {
	m := muxrpc.NewManifest()
	m.RegisterSource("createHistoryStream")
	m.RegisterAsync("gossip.ping")
	m.RegisterAsync("blobs.get")
	m.RegisterAsync("blobs.has")
	m.RegisterAsync("blobs.size")
	m.RegisterSink("blobs.add")
	m.RegisterSource("blobs.createWants")
	m.RegisterDuplex("ebt.replicate")
	m.RegisterAsync("whoami")
	m.RegisterSource("replicate.upto")
	return m
}

func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range s.peers {
		p.Conn.Close()
	}
	if s.ln != nil {
		return s.ln.Close()
	}
	return nil
}

type Client struct {
	keyPair *keys.KeyPair
	opts    Options
}

func NewClient(opts Options) *Client {
	return &Client{
		keyPair: opts.KeyPair,
		opts:    opts,
	}
}

func (c *Client) Connect(ctx context.Context, addr string, remote ed25519.PublicKey, handler muxrpc.Handler) (*Peer, error) {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("network: failed to dial %s: %w", addr, err)
	}

	shs, err := secretstream.NewClient(conn, secretstream.NewAppKey(c.opts.AppKey), c.keyPair.Private(), remote)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("network: failed to create client: %w", err)
	}

	if err := shs.Handshake(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("network: handshake failed: %w", err)
	}

	remoteFeed, err := GetFeedRefFromAddr(shs.RemoteAddr())
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("network: failed to get remote feed ref: %w", err)
	}

	peer := &Peer{
		ID:      *remoteFeed,
		Conn:    shs,
		KeyPair: c.keyPair,
	}

	sc := &statsConn{Conn: shs, p: peer}
	if handler != nil {
		peer.rpc = muxrpc.NewServer(ctx, sc, handler, nil)
	}

	return peer, nil
}

type Addr struct {
	net.Addr
	PubKey []byte
}

func (a Addr) String() string {
	return base64.StdEncoding.EncodeToString(a.PubKey)
}

func GetFeedRefFromAddr(addr net.Addr) (*refs.FeedRef, error) {
	if a, ok := addr.(secretstream.Addr); ok {
		return refs.ParseFeedRef("@" + base64.StdEncoding.EncodeToString(a.PubKey) + ".ed25519")
	}
	if a, ok := addr.(Addr); ok {
		return refs.ParseFeedRef("@" + base64.StdEncoding.EncodeToString(a.PubKey) + ".ed25519")
	}
	return nil, fmt.Errorf("network: address has no pubkey")
}

func GenerateKeyPair() (*keys.KeyPair, error) {
	return keys.Generate()
}
