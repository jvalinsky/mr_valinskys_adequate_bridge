package room

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomstate"
)

type RoomHandler struct {
	server  *RoomServer
	feedRef refs.FeedRef
}

func NewRoomHandler(s *RoomServer, feedRef refs.FeedRef) *RoomHandler {
	return &RoomHandler{
		server:  s,
		feedRef: feedRef,
	}
}

func (h *RoomHandler) Handled(m muxrpc.Method) bool {
	return len(m) >= 1 && m[0] == "room"
}

func (h *RoomHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	if len(req.Method) < 2 {
		req.CloseWithError(fmt.Errorf("room: missing submethod"))
		return
	}

	switch req.Method[1] {
	case "createInvite":
		h.handleCreateInvite(ctx, req)
	case "join":
		h.handleJoin(ctx, req)
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

func (h *RoomHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}

func (h *RoomHandler) getCallerFeed(req *muxrpc.Request) (refs.FeedRef, error) {
	if req.RemoteAddr() == nil {
		return refs.FeedRef{}, fmt.Errorf("no remote addr")
	}
	return AuthenticatedFeedFromAddr(req.RemoteAddr())
}

func (h *RoomHandler) handleCreateInvite(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "async" {
		req.CloseWithError(fmt.Errorf("room.createInvite is async"))
		return
	}

	mode, err := h.server.config.GetPrivacyMode(ctx)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.createInvite: get privacy mode: %w", err))
		return
	}

	if mode == roomdb.ModeRestricted {
		req.CloseWithError(fmt.Errorf("room.createInvite: invite creation disabled in restricted mode"))
		return
	}

	var createdBy int64
	if mode == roomdb.ModeCommunity {
		caller, err := h.getCallerFeed(req)
		if err != nil {
			req.CloseWithError(fmt.Errorf("room.createInvite: get caller: %w", err))
			return
		}
		if !isInternalMember(h.server, ctx, caller) {
			req.CloseWithError(fmt.Errorf("room.createInvite: membership required in community mode"))
			return
		}
		member, err := h.server.members.GetByFeed(ctx, caller)
		if err == nil {
			createdBy = member.ID
		}
	}

	token, err := h.server.invites.Create(ctx, createdBy)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.createInvite: create: %w", err))
		return
	}

	req.Return(ctx, token)
}

func (h *RoomHandler) handleJoin(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "async" {
		req.CloseWithError(fmt.Errorf("room.join is async"))
		return
	}

	caller, err := h.getCallerFeed(req)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.join: get caller: %w", err))
		return
	}

	if h.server.denied.HasFeed(ctx, caller) {
		req.CloseWithError(fmt.Errorf("room.join: denied"))
		return
	}

	var args struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(req.RawArgs, &args); err != nil || args.Token == "" {
		token, tokErr := parseSingleStringArg(req.RawArgs)
		if tokErr != nil {
			req.CloseWithError(fmt.Errorf("room.join: parse args: %w", err))
			return
		}
		args.Token = token
	}

	if args.Token == "" {
		req.CloseWithError(fmt.Errorf("room.join: token required"))
		return
	}

	_, err = h.server.invites.Consume(ctx, args.Token, caller)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.join: consume: %w", err))
		return
	}

	req.Return(ctx, map[string]interface{}{
		"success": true,
	})
}

func (h *RoomHandler) handleRegisterAlias(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "async" {
		req.CloseWithError(fmt.Errorf("room.registerAlias is async"))
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
	alias, sig, err := parseAliasRegisterArgs(req.RawArgs)
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.registerAlias: parse args: %w", err))
		return
	}

	alias = strings.ToLower(strings.TrimSpace(alias))
	if alias == "" {
		req.CloseWithError(fmt.Errorf("room.registerAlias: alias required"))
		return
	}

	if err := validateAliasRegistration(*h.server.keyPair, caller, alias, sig); err != nil {
		req.CloseWithError(fmt.Errorf("room.registerAlias: invalid signature: %w", err))
		return
	}
	if err := h.server.aliases.Register(ctx, alias, caller, sig); err != nil {
		req.CloseWithError(fmt.Errorf("room.registerAlias: register: %w", err))
		return
	}

	h.server.state.RegisterAlias(alias, caller)

	req.Return(ctx, h.aliasURL(alias))
}

func (h *RoomHandler) handleRevokeAlias(ctx context.Context, req *muxrpc.Request) {
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

func (h *RoomHandler) handleListAliases(ctx context.Context, req *muxrpc.Request) {
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

func (h *RoomHandler) handleMembers(ctx context.Context, req *muxrpc.Request) {
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

func (h *RoomHandler) streamMembers(ctx context.Context, req *muxrpc.Request, members []roomdb.Member) {
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

func (h *RoomHandler) handleAttendants(ctx context.Context, req *muxrpc.Request) {
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

func (h *RoomHandler) streamAttendants(ctx context.Context, req *muxrpc.Request, peers []roomstate.PeerInfo, events <-chan roomstate.AttendantEvent, cancel func()) {
	sink, err := req.ResponseSink()
	if err != nil {
		req.CloseWithError(fmt.Errorf("room.attendants: get sink: %w", err))
		return
	}
	defer sink.Close()
	defer cancel()

	bridgeFeed := ""
	if h.server.keyPair != nil {
		bridgeFeed = h.server.keyPair.String()
	}

	state := make([]string, 0, len(peers))
	for _, p := range peers {
		if p.ID.String() != bridgeFeed {
			state = append(state, p.ID.String())
		}
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
			if evt.ID.String() == bridgeFeed {
				continue
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

func (h *RoomHandler) handleMetadata(ctx context.Context, req *muxrpc.Request) {
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

	membership := false
	if caller, err := h.getCallerFeed(req); err == nil {
		membership = isInternalMember(h.server, ctx, caller)
	}

	req.Return(ctx, MetadataResult{
		Name:       name,
		Membership: membership,
		Features:   roomFeatures(mode),
	})
}

func (h *RoomHandler) aliasURL(alias string) string {
	if h.server == nil {
		return buildAliasURL("", alias)
	}
	return buildAliasURL(h.server.Domain, alias)
}

func lower(s string) string {
	if len(s) <= 64 {
		return s
	}
	return strings.ToLower(s)
}
