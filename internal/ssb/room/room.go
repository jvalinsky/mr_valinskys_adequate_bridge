package room

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream"
)

type Role int

const (
	RoleMember Role = iota
	RoleModerator
	RoleAdmin
)

type Member struct {
	ID   refs.FeedRef
	Role Role
}

type Alias struct {
	Feed   refs.FeedRef
	Alias  string
	Active bool
}

type Attendant struct {
	Feed refs.FeedRef
	Role Role
}

type RoomState struct {
	mu           sync.RWMutex
	attendants   map[string]*Attendant
	members      map[string]Member
	aliases      map[string]Alias
	deniedKeys   map[string]struct{}
	roomMetaFeed refs.FeedRef
}

func NewRoomState(roomMetaFeed refs.FeedRef) *RoomState {
	return &RoomState{
		attendants:   make(map[string]*Attendant),
		members:      make(map[string]Member),
		aliases:      make(map[string]Alias),
		deniedKeys:   make(map[string]struct{}),
		roomMetaFeed: roomMetaFeed,
	}
}

func (rs *RoomState) AddAttendant(feed refs.FeedRef, role Role) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	rs.attendants[feed.String()] = &Attendant{
		Feed: feed,
		Role: role,
	}
}

func (rs *RoomState) RemoveAttendant(feed refs.FeedRef) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	delete(rs.attendants, feed.String())
}

func (rs *RoomState) GetAttendants() []*Attendant {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	result := make([]*Attendant, 0, len(rs.attendants))
	for _, a := range rs.attendants {
		result = append(result, a)
	}
	return result
}

func (rs *RoomState) AddMember(feed refs.FeedRef, role Role) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	rs.members[feed.String()] = Member{
		ID:   feed,
		Role: role,
	}
	return nil
}

func (rs *RoomState) RemoveMember(feed refs.FeedRef) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	delete(rs.members, feed.String())
}

func (rs *RoomState) IsMember(feed refs.FeedRef) bool {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	_, ok := rs.members[feed.String()]
	return ok
}

func (rs *RoomState) GetMemberRole(feed refs.FeedRef) Role {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	if m, ok := rs.members[feed.String()]; ok {
		return m.Role
	}
	return -1
}

func (rs *RoomState) AddAlias(feed refs.FeedRef, alias string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	rs.aliases[alias] = Alias{
		Feed:   feed,
		Alias:  alias,
		Active: true,
	}
	return nil
}

func (rs *RoomState) RemoveAlias(alias string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	delete(rs.aliases, alias)
}

func (rs *RoomState) ResolveAlias(alias string) *refs.FeedRef {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	if a, ok := rs.aliases[alias]; ok && a.Active {
		return &a.Feed
	}
	return nil
}

func (rs *RoomState) GetAliases(feed refs.FeedRef) []Alias {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	var result []Alias
	for _, a := range rs.aliases {
		if a.Feed.Equal(feed) && a.Active {
			result = append(result, a)
		}
	}
	return result
}

func (rs *RoomState) DenyKey(pubKey string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	rs.deniedKeys[pubKey] = struct{}{}
}

func (rs *RoomState) AllowKey(pubKey string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	delete(rs.deniedKeys, pubKey)
}

func (rs *RoomState) IsDenied(pubKey string) bool {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	_, denied := rs.deniedKeys[pubKey]
	return denied
}

type Server struct {
	ctx     context.Context
	addr    string
	keyPair *keys.KeyPair
	state   *RoomState
	appKey  string

	listener net.Listener

	mu    sync.RWMutex
	peers map[string]*Peer
}

type Peer struct {
	Feed   refs.FeedRef
	Conn   net.Conn
	Role   Role
	isAuth bool

	muxrpcServer *muxrpc.Server
}

type Options struct {
	ListenAddr   string
	KeyPair      *keys.KeyPair
	AppKey       string
	RoomMetaFeed refs.FeedRef
}

func NewServer(opts Options) (*Server, error) {
	return &Server{
		addr:    opts.ListenAddr,
		keyPair: opts.KeyPair,
		state:   NewRoomState(opts.RoomMetaFeed),
		appKey:  opts.AppKey,
		peers:   make(map[string]*Peer),
	}, nil
}

func (s *Server) Serve(ctx context.Context, mux *muxrpc.HandlerMux) error {
	s.ctx = ctx

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("room: failed to listen: %w", err)
	}
	s.listener = ln

	go s.acceptLoop(mux)
	<-ctx.Done()
	return ctx.Err()
}

func (s *Server) acceptLoop(mux *muxrpc.HandlerMux) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				continue
			}
		}

		go s.handleConn(conn, mux)
	}
}

func (s *Server) handleConn(conn net.Conn, mux *muxrpc.HandlerMux) {
	defer conn.Close()

	shs, err := secretstream.NewServer(conn, secretstream.NewAppKey(s.appKey), s.keyPair.Private())
	if err != nil {
		return
	}

	if err := shs.Handshake(); err != nil {
		return
	}

	remotePubKey := shs.RemotePubKey()
	feedRef, err := refs.ParseFeedRef("@" + base64.StdEncoding.EncodeToString(remotePubKey) + ".ed25519")
	if err != nil {
		return
	}

	peer := &Peer{
		Feed: *feedRef,
		Conn: conn,
	}

	s.addPeer(peer)
	defer s.removePeer(peer)

	srv := muxrpc.NewServer(s.ctx, conn, mux, nil)
	s.SetPeerMuxRPC(peer.Feed, srv)

	<-s.ctx.Done()
}

func (s *Server) addPeer(p *Peer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peers[p.Feed.String()] = p
}

func (s *Server) removePeer(p *Peer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.peers, p.Feed.String())
}

func (s *Server) GetPeers() []*Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Peer, 0, len(s.peers))
	for _, p := range s.peers {
		result = append(result, p)
	}
	return result
}

func (s *Server) GetPeerByFeed(feed refs.FeedRef) *Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.peers[feed.String()]
}

func (s *Server) SetPeerMuxRPC(feed refs.FeedRef, srv *muxrpc.Server) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.peers[feed.String()]; ok {
		p.muxrpcServer = srv
	}
}

func (s *Server) GetPeerMuxRPC(feed refs.FeedRef) *muxrpc.Server {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.peers[feed.String()]; ok {
		return p.muxrpcServer
	}
	return nil
}

func (s *Server) Authenticate(feed refs.FeedRef, sig []byte, msg []byte) bool {
	if s.keyPair.Verify(msg, sig) {
		if s.state.IsDenied(base64.StdEncoding.EncodeToString(feed.PubKey())) {
			return false
		}
		return true
	}
	return false
}

func (s *Server) AddMember(feed refs.FeedRef, role Role) error {
	return s.state.AddMember(feed, role)
}

func (s *Server) RemoveMember(feed refs.FeedRef) {
	s.state.RemoveMember(feed)
}

func (s *Server) AddAlias(feed refs.FeedRef, alias string) error {
	if !s.state.IsMember(feed) {
		return fmt.Errorf("room: not a member")
	}
	return s.state.AddAlias(feed, alias)
}

func (s *Server) RemoveAlias(alias string) {
	s.state.RemoveAlias(alias)
}

func (s *Server) GetAliases(feed refs.FeedRef) []Alias {
	return s.state.GetAliases(feed)
}

func (s *Server) DenyKey(pubKey string) {
	s.state.DenyKey(pubKey)
}

func (s *Server) AllowKey(pubKey string) {
	s.state.AllowKey(pubKey)
}

func (s *Server) RoomMetaFeed() refs.FeedRef {
	return s.state.roomMetaFeed
}

func (s *Server) Attendants() []*Attendant {
	return s.state.GetAttendants()
}

type Invite struct {
	ID        string
	CreatedBy refs.FeedRef
	Uses      int
	MaxUses   int
	Expires   time.Time
}

type InviteManager struct {
	mu      sync.RWMutex
	invites map[string]*Invite
}

func NewInviteManager() *InviteManager {
	return &InviteManager{
		invites: make(map[string]*Invite),
	}
}

func (im *InviteManager) Create(owner refs.FeedRef, maxUses int, expires time.Duration) (*Invite, error) {
	im.mu.Lock()
	defer im.mu.Unlock()

	invite := &Invite{
		ID:        generateInviteID(),
		CreatedBy: owner,
		Uses:      0,
		MaxUses:   maxUses,
		Expires:   time.Now().Add(expires),
	}

	im.invites[invite.ID] = invite
	return invite, nil
}

func (im *InviteManager) Use(id string) (*Invite, error) {
	im.mu.Lock()
	defer im.mu.Unlock()

	invite, ok := im.invites[id]
	if !ok {
		return nil, fmt.Errorf("invite: not found")
	}

	if time.Now().After(invite.Expires) {
		return nil, fmt.Errorf("invite: expired")
	}

	if invite.Uses >= invite.MaxUses {
		return nil, fmt.Errorf("invite: max uses reached")
	}

	invite.Uses++
	return invite, nil
}

func (im *InviteManager) Revoke(id string) {
	im.mu.Lock()
	defer im.mu.Unlock()

	delete(im.invites, id)
}

func generateInviteID() string {
	_, seed, err := ed25519.GenerateKey(nil)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(seed[:16])
}

type RoomConfig struct {
	Name        string
	Description string
	Privacy     PrivacyMode
	AppKey      string
}

type PrivacyMode int

const (
	PrivacyModeOpen PrivacyMode = iota
	PrivacyModePrivate
)

func (rc RoomConfig) IsPrivate() bool {
	return rc.Privacy == PrivacyModePrivate
}
