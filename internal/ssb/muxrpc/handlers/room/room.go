package room

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
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
	Domain  string

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
	srv := r.peers[feed.String()]
	return srv
}

type ListedPeer struct {
	Feed refs.FeedRef
	Addr string
}

func ListPeers(registry *PeerRegistry) []ListedPeer {
	if registry == nil {
		return nil
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	res := make([]ListedPeer, 0, len(registry.peers))
	for feed := range registry.peers {
		ref, err := refs.ParseFeedRef(feed)
		if err != nil {
			continue
		}
		res = append(res, ListedPeer{
			Feed: *ref,
		})
	}
	return res
}

func (r *PeerRegistry) List() []ListedPeer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res := make([]ListedPeer, 0, len(r.peers))
	for feed := range r.peers {
		ref, err := refs.ParseFeedRef(feed)
		if err != nil {
			continue
		}
		res = append(res, ListedPeer{
			Feed: *ref,
		})
	}
	return res
}

func NewRoomServer(
	keyPair *refs.FeedRef,
	members roomdb.MembersService,
	aliases roomdb.AliasesService,
	invites roomdb.InvitesService,
	denied roomdb.DeniedKeysService,
	config roomdb.RoomConfig,
	state *roomstate.Manager,
	domain string,
) *RoomServer {
	return &RoomServer{
		keyPair:      keyPair,
		members:      members,
		aliases:      aliases,
		invites:      invites,
		denied:       denied,
		config:       config,
		state:        state,
		Domain:       domain,
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

func (h *AliasHandler) handleRegisterAlias(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "async" {
		req.CloseWithError(fmt.Errorf("room.registerAlias is async"))
		return
	}

	alias, signature, err := parseAliasRegisterArgs(req.RawArgs)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.registerAlias: parse args: %w", err))
		return
	}

	alias = strings.ToLower(strings.TrimSpace(alias))
	if alias == "" {
		req.CloseWithError(fmt.Errorf("room.registerAlias: alias required"))
		return
	}

	caller, err := h.getCallerFeed(req)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.registerAlias: get caller: %w", err))
		return
	}
	if !isInternalMember(h.server, ctx, caller) {
		req.CloseWithError(fmt.Errorf("room.registerAlias: membership required"))
		return
	}
	mode, err := h.server.config.GetPrivacyMode(ctx)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.registerAlias: get mode: %w", err))
		return
	}
	if mode == roomdb.ModeRestricted {
		req.CloseWithError(fmt.Errorf("room.registerAlias: alias feature disabled"))
		return
	}
	if err := validateAliasRegistration(*h.server.keyPair, caller, alias, signature); err != nil {
		req.CloseWithError(fmt.Errorf("room.registerAlias: invalid signature: %w", err))
		return
	}

	if err := h.server.aliases.Register(ctx, alias, caller, signature); err != nil {
		req.CloseWithError(fmt.Errorf("room.registerAlias: register: %w", err))
		return
	}

	h.server.state.RegisterAlias(alias, caller)

	req.Return(ctx, h.aliasURL(alias))
}

func (h *AliasHandler) handleRevokeAlias(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "async" {
		req.CloseWithError(fmt.Errorf("room.revokeAlias is async"))
		return
	}

	aliasName, err := parseSingleStringArg(req.RawArgs)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.revokeAlias: parse args: %w", err))
		return
	}

	aliasName = strings.ToLower(strings.TrimSpace(aliasName))
	if aliasName == "" {
		req.CloseWithError(fmt.Errorf("room.revokeAlias: alias required"))
		return
	}

	caller, err := h.getCallerFeed(req)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.revokeAlias: get caller: %w", err))
		return
	}
	if !isInternalMember(h.server, ctx, caller) {
		req.CloseWithError(fmt.Errorf("room.revokeAlias: membership required"))
		return
	}

	alias, err := h.server.aliases.Resolve(ctx, aliasName)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.revokeAlias: resolve: %w", err))
		return
	}

	if !alias.Owner.Equal(caller) {
		req.CloseWithError(fmt.Errorf("room.revokeAlias: not owner"))
		return
	}

	if err := h.server.aliases.Revoke(ctx, aliasName); err != nil {
		req.CloseWithError(fmt.Errorf("room.revokeAlias: revoke: %w", err))
		return
	}

	h.server.state.RevokeAlias(aliasName)

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

	caller, err := h.getCallerFeed(req)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.attendants: get caller: %w", err))
		return
	}
	if !isInternalMember(h.server, ctx, caller) {
		req.CloseWithError(fmt.Errorf("room.attendants: membership required"))
		return
	}

	peers, events, cancel := h.server.state.SubscribeAttendants()
	go h.streamAttendants(ctx, req, peers, events, cancel)
}

func (h *AliasHandler) streamAttendants(ctx context.Context, req *muxrpc.Request, peers []roomstate.PeerInfo, events <-chan roomstate.AttendantEvent, cancel func()) {
	sink, err := req.ResponseSink()
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.attendants: get sink: %w", err))
		return
	}
	defer sink.Close()
	defer cancel()

	state := make([]string, 0, len(peers))
	for _, p := range peers {
		state = append(state, p.ID.String())
	}
	data, _ := json.Marshal(map[string]interface{}{
		"type": "state",
		"ids":  state,
	})
	if _, err := sink.Write(data); err != nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			payload, _ := json.Marshal(map[string]interface{}{
				"type": evt.Type,
				"id":   evt.ID.String(),
			})
			if _, err := sink.Write(payload); err != nil {
				return
			}
		}
	}
}

type MetadataResult struct {
	Name       string   `json:"name"`
	Membership string   `json:"membership"`
	Features   []string `json:"features"`
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

	name := "ATProto Bridge SSB Room"
	if h.server.keyPair != nil {
		name = h.server.keyPair.String()
	}

	membership := "open"
	switch mode {
	case roomdb.ModeCommunity:
		membership = "community"
	case roomdb.ModeRestricted:
		membership = "restricted"
	}

	req.Return(ctx, MetadataResult{
		Name:       name,
		Membership: membership,
		Features:   roomFeatures(mode),
	})
}

func (h *AliasHandler) getCallerFeed(req *muxrpc.Request) (refs.FeedRef, error) {
	if req.RemoteAddr() == nil {
		return refs.FeedRef{}, fmt.Errorf("no remote addr")
	}
	return AuthenticatedFeedFromAddr(req.RemoteAddr())
}

func (h *AliasHandler) aliasURL(alias string) string {
	if h.server == nil || h.server.Domain == "" {
		return "/" + url.PathEscape(alias)
	}
	return "https://" + h.server.Domain + "/" + url.PathEscape(alias)
}
