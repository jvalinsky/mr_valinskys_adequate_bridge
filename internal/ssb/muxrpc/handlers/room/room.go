package room

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomstate"
)

type RoomServer struct {
	keyPair *refs.FeedRef
	members roomdb.MembersService
	aliases roomdb.AliasesService
	invites roomdb.InvitesService
	denied  roomdb.DeniedKeysService
	config  roomdb.RoomConfig
	state   *roomstate.Manager

	peerRegistry *PeerRegistry
}

type PeerRegistry struct {
	mu    sync.RWMutex
	peers map[string]*muxrpc.Server
}

func NewPeerRegistry() *PeerRegistry {
	return &PeerRegistry{
		peers: make(map[string]*muxrpc.Server),
	}
}

func (r *PeerRegistry) Register(feed refs.FeedRef, srv *muxrpc.Server) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peers[feed.String()] = srv
}

func (r *PeerRegistry) Unregister(feed refs.FeedRef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.peers, feed.String())
}

func (r *PeerRegistry) Get(feed refs.FeedRef) *muxrpc.Server {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.peers[feed.String()]
}

func NewRoomServer(
	keyPair *refs.FeedRef,
	members roomdb.MembersService,
	aliases roomdb.AliasesService,
	invites roomdb.InvitesService,
	denied roomdb.DeniedKeysService,
	config roomdb.RoomConfig,
	state *roomstate.Manager,
) *RoomServer {
	return &RoomServer{
		keyPair:      keyPair,
		members:      members,
		aliases:      aliases,
		invites:      invites,
		denied:       denied,
		config:       config,
		state:        state,
		peerRegistry: NewPeerRegistry(),
	}
}

type AliasHandler struct {
	server *RoomServer
}

func NewAliasHandler(s *RoomServer) *AliasHandler {
	return &AliasHandler{server: s}
}

func (s *RoomServer) KeyPair() *refs.FeedRef {
	return s.keyPair
}

func (s *RoomServer) PeerRegistry() *PeerRegistry {
	return s.peerRegistry
}

func (s *RoomServer) GetPeerMuxRPC(feed refs.FeedRef) *muxrpc.Server {
	if s.peerRegistry == nil {
		return nil
	}
	return s.peerRegistry.Get(feed)
}

func (h *AliasHandler) Handled(m muxrpc.Method) bool {
	return len(m) >= 1 && m[0] == "room"
}

func (h *AliasHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	if len(req.Method) < 2 {
		req.CloseWithError(fmt.Errorf("room: missing submethod"))
		return
	}

	switch req.Method[1] {
	case "registerAlias":
		h.handleRegisterAlias(ctx, req)
	case "revokeAlias":
		h.handleRevokeAlias(ctx, req)
	case "listAliases":
		h.handleListAliases(ctx, req)
	case "members":
		h.handleMembers(ctx, req)
	case "attendants":
		h.handleAttendants(ctx, req)
	case "metadata":
		h.handleMetadata(ctx, req)
	default:
		req.CloseWithError(fmt.Errorf("room: unknown method %s", req.Method[1]))
	}
}

func (h *AliasHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}

type RegisterAliasArgs struct {
	Alias     string `json:"alias"`
	Signature []byte `json:"signature"`
}

func (h *AliasHandler) handleRegisterAlias(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "async" {
		req.CloseWithError(fmt.Errorf("room.registerAlias is async"))
		return
	}

	var args RegisterAliasArgs
	if len(req.RawArgs) > 0 {
		if err := json.Unmarshal(req.RawArgs, &args); err != nil {
			req.CloseWithError(fmt.Errorf("room.registerAlias: parse args: %w", err))
			return
		}
	}

	if args.Alias == "" {
		req.CloseWithError(fmt.Errorf("room.registerAlias: alias required"))
		return
	}

	if len(args.Signature) == 0 {
		req.CloseWithError(fmt.Errorf("room.registerAlias: signature required"))
		return
	}

	caller, err := h.getCallerFeed(ctx, req)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.registerAlias: get caller: %w", err))
		return
	}

	if err := h.server.aliases.Register(ctx, args.Alias, caller, args.Signature); err != nil {
		req.CloseWithError(fmt.Errorf("room.registerAlias: register: %w", err))
		return
	}

	h.server.state.RegisterAlias(args.Alias, caller)

	req.Return(ctx, map[string]interface{}{
		"alias": args.Alias,
	})
}

func (h *AliasHandler) handleRevokeAlias(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "async" {
		req.CloseWithError(fmt.Errorf("room.revokeAlias is async"))
		return
	}

	var args struct {
		Alias string `json:"alias"`
	}
	if len(req.RawArgs) > 0 {
		if err := json.Unmarshal(req.RawArgs, &args); err != nil {
			req.CloseWithError(fmt.Errorf("room.revokeAlias: parse args: %w", err))
			return
		}
	}

	if args.Alias == "" {
		req.CloseWithError(fmt.Errorf("room.revokeAlias: alias required"))
		return
	}

	caller, err := h.getCallerFeed(ctx, req)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.revokeAlias: get caller: %w", err))
		return
	}

	alias, err := h.server.aliases.Resolve(ctx, args.Alias)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.revokeAlias: resolve: %w", err))
		return
	}

	if !alias.Owner.Equal(caller) {
		req.CloseWithError(fmt.Errorf("room.revokeAlias: not owner"))
		return
	}

	if err := h.server.aliases.Revoke(ctx, args.Alias); err != nil {
		req.CloseWithError(fmt.Errorf("room.revokeAlias: revoke: %w", err))
		return
	}

	h.server.state.RevokeAlias(args.Alias)

	req.Return(ctx, true)
}

func (h *AliasHandler) handleListAliases(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "async" {
		req.CloseWithError(fmt.Errorf("room.listAliases is async"))
		return
	}

	aliases, err := h.server.aliases.List(ctx)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.listAliases: list: %w", err))
		return
	}

	result := make([]map[string]interface{}, 0, len(aliases))
	for _, a := range aliases {
		result = append(result, map[string]interface{}{
			"alias":  a.Name,
			"owner":  a.Owner.String(),
			"domain": fmt.Sprintf("%s.%s", a.Name, "room"),
		})
	}

	req.Return(ctx, result)
}

func (h *AliasHandler) handleMembers(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "source" {
		req.CloseWithError(fmt.Errorf("room.members is a source handler"))
		return
	}

	members, err := h.server.members.List(ctx)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.members: list: %w", err))
		return
	}

	go h.streamMembers(ctx, req, members)
}

func (h *AliasHandler) streamMembers(ctx context.Context, req *muxrpc.Request, members []roomdb.Member) {
	sink, err := req.ResponseSink()
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.members: get sink: %w", err))
		return
	}
	defer sink.Close()

	for _, m := range members {
		select {
		case <-ctx.Done():
			return
		default:
		}

		data, _ := json.Marshal(map[string]interface{}{
			"id":   m.ID,
			"feed": m.PubKey.String(),
			"role": m.Role.String(),
		})
		if _, err := sink.Write(data); err != nil {
			return
		}
	}
}

func (h *AliasHandler) handleAttendants(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "source" {
		req.CloseWithError(fmt.Errorf("room.attendants is a source handler"))
		return
	}

	peers := h.server.state.Peers()

	go h.streamAttendants(ctx, req, peers)
}

func (h *AliasHandler) streamAttendants(ctx context.Context, req *muxrpc.Request, peers []roomstate.PeerInfo) {
	sink, err := req.ResponseSink()
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.attendants: get sink: %w", err))
		return
	}
	defer sink.Close()

	for _, p := range peers {
		select {
		case <-ctx.Done():
			return
		default:
		}

		data, _ := json.Marshal(map[string]interface{}{
			"id":   p.ID.String(),
			"addr": p.Addr,
		})
		if _, err := sink.Write(data); err != nil {
			return
		}
	}
}

type MetadataResult struct {
	RoomID      string `json:"roomId"`
	RoomInfo    string `json:"roomInfo"`
	Domain      string `json:"domain"`
	Mode        string `json:"mode"`
	Description string `json:"description"`
}

func (h *AliasHandler) handleMetadata(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "async" {
		req.CloseWithError(fmt.Errorf("room.metadata is async"))
		return
	}

	mode, err := h.server.config.GetPrivacyMode(ctx)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.metadata: get mode: %w", err))
		return
	}

	modeStr := "community"
	switch mode {
	case roomdb.ModeOpen:
		modeStr = "open"
	case roomdb.ModeRestricted:
		modeStr = "restricted"
	}

	req.Return(ctx, MetadataResult{
		RoomID:      h.server.keyPair.String(),
		RoomInfo:    "ATProto Bridge SSB Room",
		Mode:        modeStr,
		Description: "SSB Room for ATProto Bridge",
	})
}

func (h *AliasHandler) getCallerFeed(ctx context.Context, req *muxrpc.Request) (refs.FeedRef, error) {
	if req.RemoteAddr() == nil {
		return refs.FeedRef{}, fmt.Errorf("no remote addr")
	}

	addr := req.RemoteAddr().String()
	for _, p := range h.server.state.Peers() {
		if p.Addr == addr {
			return p.ID, nil
		}
	}

	return refs.FeedRef{}, fmt.Errorf("caller not found")
}
