package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	roomhandlers "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/handlers/room"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	roomsqlite "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb/sqlite"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/templates"
)

// RoomOpsProvider exposes room operations data and mutating actions for admin routes.
type RoomOpsProvider interface {
	Overview(ctx context.Context) (RoomOverview, error)
	MembersList(ctx context.Context) ([]templates.RoomMemberRow, error)
	MemberRoleSet(ctx context.Context, memberID int64, role roomdb.Role) error
	MemberRemove(ctx context.Context, memberID int64) error
	InvitesList(ctx context.Context) ([]templates.RoomInviteRow, error)
	InviteCreate(ctx context.Context, createdBy int64) (string, error)
	InviteRevoke(ctx context.Context, inviteID int64) error
	AliasesList(ctx context.Context) ([]templates.RoomAliasRow, error)
	AliasRevoke(ctx context.Context, alias string) error
	DeniedList(ctx context.Context) ([]templates.RoomDeniedKeyRow, error)
	DeniedAdd(ctx context.Context, feed refs.FeedRef, comment string) error
	MemberAdd(ctx context.Context, feed refs.FeedRef, role roomdb.Role) (int64, error)
	DeniedRemove(ctx context.Context, deniedID int64) error
	AttendantsSnapshot(ctx context.Context) ([]templates.RoomAttendantRow, error)
	TunnelEndpointsSnapshot(ctx context.Context) ([]templates.RoomTunnelEndpointRow, error)
	GetRoomPeers(ctx context.Context) ([]PeerStatus, error)
	JoinURL(token string) string
	Close() error
}

// RoomOverview is an operational summary used by admin room pages.
type RoomOverview struct {
	Available             bool
	DegradedReason        string
	Mode                  roomdb.PrivacyMode
	ModeLabel             string
	ModeSummary           string
	OperatorRole          roomdb.Role
	PolicyHint            string
	MembersCount          int
	InvitesActive         int
	InvitesTotal          int
	AliasesCount          int
	DeniedCount           int
	AttendantsActive      int
	AttendantsTotal       int
	TunnelEndpointsActive int
	TunnelEndpointsTotal  int
	HealthStatus          string
	HealthDetail          string
	StatusSourceURL       string
}

// SQLiteRoomOpsProvider serves room ops data from room sqlite with optional HTTP status checks.
type SQLiteRoomOpsProvider struct {
	logger          *log.Logger
	db              *roomsqlite.DB
	roomHTTPBaseURL string
	statusClient    *roomStatusClient
	operatorRole    roomdb.Role
	roomServer      *roomhandlers.RoomServer
}

// OpenSQLiteRoomOpsProvider opens room sqlite-backed operations provider.
func OpenSQLiteRoomOpsProvider(roomRepoPath, roomHTTPBaseURL string, operatorRole roomdb.Role, logger *log.Logger) (*SQLiteRoomOpsProvider, error) {
	logger = logutil.Ensure(logger)
	roomRepoPath = strings.TrimSpace(roomRepoPath)
	if roomRepoPath == "" {
		return nil, fmt.Errorf("room repo path is required")
	}

	sqlitePath := resolveRoomSQLitePath(roomRepoPath)
	if _, err := os.Stat(sqlitePath); err != nil {
		return nil, fmt.Errorf("room sqlite unavailable at %s: %w", sqlitePath, err)
	}

	database, err := roomsqlite.Open(sqlitePath)
	if err != nil {
		return nil, fmt.Errorf("open room sqlite: %w", err)
	}

	if operatorRole == roomdb.RoleUnknown || operatorRole == roomdb.RoleNone {
		operatorRole = roomdb.RoleAdmin
	}

	baseURL := normalizeBaseURL(roomHTTPBaseURL)
	provider := &SQLiteRoomOpsProvider{
		logger:          logger,
		db:              database,
		roomHTTPBaseURL: baseURL,
		operatorRole:    operatorRole,
	}
	if baseURL != "" {
		provider.statusClient = &roomStatusClient{
			baseURL: baseURL,
			client: &http.Client{
				Timeout: 2 * time.Second,
			},
		}
	}
	return provider, nil
}

func (p *SQLiteRoomOpsProvider) Close() error {
	if p == nil || p.db == nil {
		return nil
	}
	return p.db.Close()
}

func (p *SQLiteRoomOpsProvider) SetRoomServer(srv *roomhandlers.RoomServer) {
	if p == nil {
		return
	}
	p.roomServer = srv
}

func (p *SQLiteRoomOpsProvider) GetRoomPeers(ctx context.Context) ([]PeerStatus, error) {
	if p == nil {
		return nil, nil
	}
	if p.statusClient != nil {
		status, err := p.statusClient.status(ctx)
		if err == nil && status.LivePeers > 0 {
			res := make([]PeerStatus, 0, status.LivePeers)
			for i := 0; i < status.LivePeers; i++ {
				res = append(res, PeerStatus{
					Feed: "room-peer",
					Addr: "room",
				})
			}
			return res, nil
		}
	}
	if p.roomServer != nil {
		registry := p.roomServer.PeerRegistry()
		if registry == nil {
			return nil, nil
		}
		registryMap := reflect.ValueOf(registry).Elem().FieldByName("peers")
		if !registryMap.IsValid() {
			return nil, nil
		}
		peersMap, ok := registryMap.Interface().(map[string]*muxrpc.Server)
		if !ok {
			return nil, nil
		}
		res := make([]PeerStatus, 0, len(peersMap))
		for feed := range peersMap {
			ref, err := refs.ParseFeedRef(feed)
			if err != nil {
				continue
			}
			res = append(res, PeerStatus{
				Feed: ref.String(),
			})
		}
		return res, nil
	}
	return nil, nil
}

func (p *SQLiteRoomOpsProvider) JoinURL(token string) string {
	base := p.roomHTTPBaseURL
	if base == "" {
		return "/join?token=" + url.QueryEscape(strings.TrimSpace(token))
	}
	return base + "/join?token=" + url.QueryEscape(strings.TrimSpace(token))
}

func (p *SQLiteRoomOpsProvider) Overview(ctx context.Context) (RoomOverview, error) {
	mode, err := p.db.RoomConfig().GetPrivacyMode(ctx)
	if err != nil {
		return RoomOverview{}, fmt.Errorf("room mode: %w", err)
	}

	members, err := p.db.Members().List(ctx)
	if err != nil {
		return RoomOverview{}, fmt.Errorf("members list: %w", err)
	}
	invites, err := p.db.Invites().List(ctx)
	if err != nil {
		return RoomOverview{}, fmt.Errorf("invites list: %w", err)
	}
	aliases, err := p.db.Aliases().List(ctx)
	if err != nil {
		return RoomOverview{}, fmt.Errorf("aliases list: %w", err)
	}
	denied, err := p.db.DeniedKeys().List(ctx)
	if err != nil {
		return RoomOverview{}, fmt.Errorf("denied list: %w", err)
	}
	attendants, err := p.db.RuntimeSnapshots().ListAttendants(ctx, false)
	if err != nil {
		return RoomOverview{}, fmt.Errorf("attendants snapshot: %w", err)
	}
	tunnels, err := p.db.RuntimeSnapshots().ListTunnelEndpoints(ctx, false)
	if err != nil {
		return RoomOverview{}, fmt.Errorf("tunnel endpoints snapshot: %w", err)
	}

	overview := RoomOverview{
		Available:            true,
		Mode:                 mode,
		ModeLabel:            roomModeLabel(mode),
		ModeSummary:          roomModeSummary(mode),
		OperatorRole:         p.operatorRole,
		PolicyHint:           roomPolicyHint(mode, p.operatorRole),
		MembersCount:         len(members),
		InvitesTotal:         len(invites),
		AliasesCount:         len(aliases),
		DeniedCount:          len(denied),
		AttendantsTotal:      len(attendants),
		TunnelEndpointsTotal: len(tunnels),
		HealthStatus:         "unknown",
		HealthDetail:         "Room HTTP status endpoint is not configured.",
	}
	for _, invite := range invites {
		if invite.Active {
			overview.InvitesActive++
		}
	}
	for _, att := range attendants {
		if att.Active {
			overview.AttendantsActive++
		}
	}
	for _, t := range tunnels {
		if t.Active {
			overview.TunnelEndpointsActive++
		}
	}

	if p.statusClient != nil {
		overview.StatusSourceURL = p.statusClient.baseURL + "/api/room/status"
		if healthErr := p.statusClient.healthz(ctx); healthErr == nil {
			overview.HealthStatus = "healthy"
			overview.HealthDetail = "Room HTTP endpoint responded to /healthz."
		} else {
			overview.HealthStatus = "degraded"
			overview.HealthDetail = "Room HTTP /healthz check failed: " + healthErr.Error()
		}

		if status, err := p.statusClient.status(ctx); err == nil {
			mode := strings.TrimSpace(status.Mode)
			if mode == "" {
				mode = strings.TrimSpace(status.PrivacyMode)
			}
			if mode != "" {
				overview.ModeLabel = strings.Title(mode)
			}
			health := strings.TrimSpace(status.Health)
			if health == "" && mode != "" {
				health = "healthy"
			}
			overview.HealthStatus = health
			if overview.HealthStatus == "" {
				overview.HealthStatus = "unknown"
			}
			if strings.TrimSpace(status.Summary) != "" {
				overview.HealthDetail = strings.TrimSpace(status.Summary)
			} else if mode != "" {
				overview.HealthDetail = fmt.Sprintf(
					"Live attendants %d, live peers %d, active tunnels %d.",
					status.LiveAttendants,
					status.LivePeers,
					status.ActiveTunnels,
				)
			}
			overview.AttendantsActive = maxInt(overview.AttendantsActive, status.ActiveAttendants)
			overview.AttendantsTotal = maxInt(overview.AttendantsTotal, status.TotalAttendants)
			overview.TunnelEndpointsActive = maxInt(overview.TunnelEndpointsActive, status.ActiveTunnels)
			overview.TunnelEndpointsTotal = maxInt(overview.TunnelEndpointsTotal, status.TotalTunnels)
		}
	}

	return overview, nil
}

func (p *SQLiteRoomOpsProvider) MembersList(ctx context.Context) ([]templates.RoomMemberRow, error) {
	members, err := p.db.Members().List(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]templates.RoomMemberRow, 0, len(members))
	for _, member := range members {
		rows = append(rows, templates.RoomMemberRow{
			ID:      member.ID,
			FeedID:  member.PubKey.String(),
			Role:    member.Role.String(),
			RoleRaw: roomRoleValue(member.Role),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Role == rows[j].Role {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].Role > rows[j].Role
	})
	return rows, nil
}

func (p *SQLiteRoomOpsProvider) MemberRoleSet(ctx context.Context, memberID int64, role roomdb.Role) error {
	policy, err := p.policy(ctx)
	if err != nil {
		return err
	}
	if !policy.canMutateMembers {
		return fmt.Errorf("member role updates are blocked by policy (%s)", policy.hint)
	}
	if role != roomdb.RoleMember && role != roomdb.RoleModerator && role != roomdb.RoleAdmin {
		return fmt.Errorf("invalid role")
	}
	return p.db.Members().SetRole(ctx, memberID, role)
}

func (p *SQLiteRoomOpsProvider) MemberRemove(ctx context.Context, memberID int64) error {
	policy, err := p.policy(ctx)
	if err != nil {
		return err
	}
	if !policy.canMutateMembers {
		return fmt.Errorf("member removal is blocked by policy (%s)", policy.hint)
	}
	return p.db.Members().RemoveID(ctx, memberID)
}

func (p *SQLiteRoomOpsProvider) InvitesList(ctx context.Context) ([]templates.RoomInviteRow, error) {
	invites, err := p.db.Invites().List(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]templates.RoomInviteRow, 0, len(invites))
	for _, invite := range invites {
		status := "consumed"
		if invite.Active {
			status = "active"
		}
		rows = append(rows, templates.RoomInviteRow{
			ID:        invite.ID,
			Status:    status,
			Active:    invite.Active,
			CreatedBy: invite.CreatedBy,
			CreatedAt: formatUnixTimestamp(invite.CreatedAt),
		})
	}
	return rows, nil
}

func (p *SQLiteRoomOpsProvider) InviteCreate(ctx context.Context, createdBy int64) (string, error) {
	policy, err := p.policy(ctx)
	if err != nil {
		return "", err
	}
	if !policy.canCreateInvite {
		return "", fmt.Errorf("invite creation is blocked by policy (%s)", policy.hint)
	}
	return p.db.Invites().Create(ctx, createdBy)
}

func (p *SQLiteRoomOpsProvider) InviteRevoke(ctx context.Context, inviteID int64) error {
	policy, err := p.policy(ctx)
	if err != nil {
		return err
	}
	if !policy.canRevokeInvite {
		return fmt.Errorf("invite revoke is blocked by policy (%s)", policy.hint)
	}
	return p.db.Invites().Revoke(ctx, inviteID)
}

func (p *SQLiteRoomOpsProvider) AliasesList(ctx context.Context) ([]templates.RoomAliasRow, error) {
	aliases, err := p.db.Aliases().List(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]templates.RoomAliasRow, 0, len(aliases))
	for _, alias := range aliases {
		rows = append(rows, templates.RoomAliasRow{
			Name:       alias.Name,
			OwnerFeed:  alias.Owner.String(),
			ReversePTR: alias.ReversePTR,
		})
	}
	return rows, nil
}

func (p *SQLiteRoomOpsProvider) AliasRevoke(ctx context.Context, alias string) error {
	policy, err := p.policy(ctx)
	if err != nil {
		return err
	}
	if !policy.canRevokeAlias {
		return fmt.Errorf("alias revoke is blocked by policy (%s)", policy.hint)
	}
	return p.db.Aliases().Revoke(ctx, strings.ToLower(strings.TrimSpace(alias)))
}

func (p *SQLiteRoomOpsProvider) DeniedList(ctx context.Context) ([]templates.RoomDeniedKeyRow, error) {
	entries, err := p.db.DeniedKeys().List(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]templates.RoomDeniedKeyRow, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, templates.RoomDeniedKeyRow{
			ID:      entry.ID,
			FeedID:  entry.PubKey.String(),
			Comment: entry.Comment,
			AddedAt: formatUnixTimestamp(entry.AddedAt),
		})
	}
	return rows, nil
}

func (p *SQLiteRoomOpsProvider) DeniedAdd(ctx context.Context, feed refs.FeedRef, comment string) error {
	policy, err := p.policy(ctx)
	if err != nil {
		return err
	}
	if !policy.canMutateDenied {
		return fmt.Errorf("denied-key updates are blocked by policy (%s)", policy.hint)
	}
	return p.db.DeniedKeys().Add(ctx, feed, strings.TrimSpace(comment))
}

func (p *SQLiteRoomOpsProvider) MemberAdd(ctx context.Context, feed refs.FeedRef, role roomdb.Role) (int64, error) {
	policy, err := p.policy(ctx)
	if err != nil {
		return 0, err
	}
	if !policy.canMutateMembers {
		return 0, fmt.Errorf("member addition is blocked by policy (%s)", policy.hint)
	}
	if role != roomdb.RoleMember && role != roomdb.RoleModerator && role != roomdb.RoleAdmin {
		return 0, fmt.Errorf("invalid role")
	}
	return p.db.Members().Add(ctx, feed, role)
}

func (p *SQLiteRoomOpsProvider) DeniedRemove(ctx context.Context, deniedID int64) error {
	policy, err := p.policy(ctx)
	if err != nil {
		return err
	}
	if !policy.canMutateDenied {
		return fmt.Errorf("denied-key updates are blocked by policy (%s)", policy.hint)
	}
	return p.db.DeniedKeys().RemoveID(ctx, deniedID)
}

func (p *SQLiteRoomOpsProvider) AttendantsSnapshot(ctx context.Context) ([]templates.RoomAttendantRow, error) {
	snapshot, err := p.db.RuntimeSnapshots().ListAttendants(ctx, false)
	if err != nil {
		return nil, err
	}
	rows := make([]templates.RoomAttendantRow, 0, len(snapshot))
	for _, att := range snapshot {
		rows = append(rows, templates.RoomAttendantRow{
			FeedID:      att.ID.String(),
			Addr:        att.Addr,
			ConnectedAt: formatUnixTimestamp(att.ConnectedAt),
			LastSeenAt:  formatUnixTimestamp(att.LastSeenAt),
			Active:      att.Active,
		})
	}
	return rows, nil
}

func (p *SQLiteRoomOpsProvider) TunnelEndpointsSnapshot(ctx context.Context) ([]templates.RoomTunnelEndpointRow, error) {
	snapshot, err := p.db.RuntimeSnapshots().ListTunnelEndpoints(ctx, false)
	if err != nil {
		return nil, err
	}
	rows := make([]templates.RoomTunnelEndpointRow, 0, len(snapshot))
	for _, endpoint := range snapshot {
		rows = append(rows, templates.RoomTunnelEndpointRow{
			TargetFeed:  endpoint.Target.String(),
			Addr:        endpoint.Addr,
			AnnouncedAt: formatUnixTimestamp(endpoint.AnnouncedAt),
			LastSeenAt:  formatUnixTimestamp(endpoint.LastSeenAt),
			Active:      endpoint.Active,
		})
	}
	return rows, nil
}

type roomPolicy struct {
	mode             roomdb.PrivacyMode
	role             roomdb.Role
	hint             string
	canCreateInvite  bool
	canRevokeInvite  bool
	canRevokeAlias   bool
	canMutateMembers bool
	canMutateDenied  bool
}

func (p *SQLiteRoomOpsProvider) policy(ctx context.Context) (roomPolicy, error) {
	mode, err := p.db.RoomConfig().GetPrivacyMode(ctx)
	if err != nil {
		return roomPolicy{}, fmt.Errorf("room policy mode lookup failed: %w", err)
	}
	role := p.operatorRole
	return roomPolicy{
		mode:             mode,
		role:             role,
		hint:             roomPolicyHint(mode, role),
		canCreateInvite:  canCreateInvite(mode, role),
		canRevokeInvite:  canRevokeInvite(mode, role),
		canRevokeAlias:   canRevokeAlias(mode, role),
		canMutateMembers: canMutateMembers(mode, role),
		canMutateDenied:  canMutateDenied(mode, role),
	}, nil
}

type roomStatusClient struct {
	baseURL string
	client  *http.Client
}

type roomStatusPayload struct {
	Mode             string `json:"mode"`
	PrivacyMode      string `json:"privacyMode"`
	Health           string `json:"health"`
	Summary          string `json:"summary"`
	LiveAttendants   int    `json:"liveAttendants"`
	LivePeers        int    `json:"livePeers"`
	ActiveAttendants int    `json:"activeAttendants"`
	TotalAttendants  int    `json:"totalAttendants"`
	ActiveTunnels    int    `json:"activeTunnels"`
	TotalTunnels     int    `json:"totalTunnels"`
}

func (c *roomStatusClient) healthz(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz status %d", resp.StatusCode)
	}
	return nil
}

func (c *roomStatusClient) status(ctx context.Context) (roomStatusPayload, error) {
	candidates := []string{"/api/room/status", "/status"}
	var lastErr error
	for _, path := range candidates {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
		if err != nil {
			lastErr = err
			continue
		}
		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("status endpoint returned %d at %s", resp.StatusCode, path)
			resp.Body.Close()
			continue
		}
		var payload roomStatusPayload
		err = json.NewDecoder(resp.Body).Decode(&payload)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return payload, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("status endpoint unavailable")
	}
	return roomStatusPayload{}, lastErr
}

func resolveRoomSQLitePath(roomRepoPath string) string {
	trimmed := strings.TrimSpace(roomRepoPath)
	if strings.HasSuffix(trimmed, ".sqlite") {
		return trimmed
	}
	return filepath.Join(trimmed, "room.sqlite")
}

func normalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func formatUnixTimestamp(unix int64) string {
	if unix <= 0 {
		return "-"
	}
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}

func roomModeLabel(mode roomdb.PrivacyMode) string {
	switch mode {
	case roomdb.ModeOpen:
		return "Open"
	case roomdb.ModeCommunity:
		return "Community"
	case roomdb.ModeRestricted:
		return "Restricted"
	default:
		return "Unknown"
	}
}

func roomModeSummary(mode roomdb.PrivacyMode) string {
	switch mode {
	case roomdb.ModeOpen:
		return "Self-serve invite creation is enabled."
	case roomdb.ModeCommunity:
		return "Invites require authenticated member access."
	case roomdb.ModeRestricted:
		return "Invite management is moderator-gated and aliases are disabled."
	default:
		return "Room mode is unknown; mutating actions are treated conservatively."
	}
}

func roomPolicyHint(mode roomdb.PrivacyMode, role roomdb.Role) string {
	parts := []string{
		"Mode: " + roomModeLabel(mode),
		"Operator role: " + roomRoleValue(role),
	}
	if canCreateInvite(mode, role) {
		parts = append(parts, "invite create: allowed")
	} else {
		parts = append(parts, "invite create: blocked")
	}
	if canRevokeAlias(mode, role) {
		parts = append(parts, "alias revoke: allowed")
	} else {
		parts = append(parts, "alias revoke: blocked")
	}
	if canMutateMembers(mode, role) {
		parts = append(parts, "member edits: allowed")
	} else {
		parts = append(parts, "member edits: blocked")
	}
	if canMutateDenied(mode, role) {
		parts = append(parts, "denied-key edits: allowed")
	} else {
		parts = append(parts, "denied-key edits: blocked")
	}
	return strings.Join(parts, " · ")
}

func canCreateInvite(mode roomdb.PrivacyMode, role roomdb.Role) bool {
	switch mode {
	case roomdb.ModeOpen:
		return true
	case roomdb.ModeCommunity:
		return roleAtLeast(role, roomdb.RoleMember)
	case roomdb.ModeRestricted:
		return roleAtLeast(role, roomdb.RoleModerator)
	default:
		return false
	}
}

func canRevokeInvite(mode roomdb.PrivacyMode, role roomdb.Role) bool {
	switch mode {
	case roomdb.ModeOpen:
		return roleAtLeast(role, roomdb.RoleMember)
	case roomdb.ModeCommunity:
		return roleAtLeast(role, roomdb.RoleMember)
	case roomdb.ModeRestricted:
		return roleAtLeast(role, roomdb.RoleModerator)
	default:
		return false
	}
}

func canRevokeAlias(mode roomdb.PrivacyMode, role roomdb.Role) bool {
	if mode == roomdb.ModeRestricted {
		return false
	}
	return roleAtLeast(role, roomdb.RoleMember)
}

func canMutateMembers(_ roomdb.PrivacyMode, role roomdb.Role) bool {
	return roleAtLeast(role, roomdb.RoleAdmin)
}

func canMutateDenied(_ roomdb.PrivacyMode, role roomdb.Role) bool {
	return roleAtLeast(role, roomdb.RoleModerator)
}

func roleAtLeast(role roomdb.Role, minimum roomdb.Role) bool {
	return int(role) >= int(minimum)
}

func roomRoleValue(role roomdb.Role) string {
	switch role {
	case roomdb.RoleMember:
		return "member"
	case roomdb.RoleModerator:
		return "moderator"
	case roomdb.RoleAdmin:
		return "admin"
	default:
		return "unknown"
	}
}

func parseRoomMemberRole(raw string) (roomdb.Role, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "member":
		return roomdb.RoleMember, nil
	case "moderator":
		return roomdb.RoleModerator, nil
	case "admin":
		return roomdb.RoleAdmin, nil
	default:
		return roomdb.RoleUnknown, fmt.Errorf("invalid role")
	}
}

func parseInt64FormValue(raw string) (int64, error) {
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid integer value")
	}
	if v <= 0 {
		return 0, fmt.Errorf("value must be positive")
	}
	return v, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
