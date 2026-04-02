package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/templates"
)

func TestRoomOverviewWithoutProviderShowsDegradedState(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	router := chi.NewRouter()
	NewUIHandler(database, nil, nil, nil, &mockSSBStatus{}).Mount(router)

	req := httptest.NewRequest(http.MethodGet, "/room", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"Room Data Unavailable", "--room-repo-path", "Room Overview"} {
		if !strings.Contains(body, want) {
			t.Fatalf("room overview missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestRoomOverviewWithProviderRendersCounts(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	router := chi.NewRouter()
	provider := &fakeRoomOpsProvider{}
	provider.overview = RoomOverview{
		Available:             true,
		Mode:                  roomdb.ModeCommunity,
		ModeLabel:             "Community",
		ModeSummary:           "Invites require authenticated member access.",
		OperatorRole:          roomdb.RoleAdmin,
		PolicyHint:            "Mode: Community · Operator role: admin",
		MembersCount:          7,
		InvitesActive:         2,
		InvitesTotal:          5,
		AliasesCount:          4,
		DeniedCount:           1,
		AttendantsActive:      3,
		AttendantsTotal:       9,
		TunnelEndpointsActive: 2,
		TunnelEndpointsTotal:  8,
		HealthStatus:          "healthy",
		HealthDetail:          "Room runtime is running.",
	}

	NewUIHandler(database, nil, nil, nil, &mockSSBStatus{}).WithRoomOps(provider).Mount(router)

	req := httptest.NewRequest(http.MethodGet, "/room", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"Community", "7", "2 / 5", "Room Health", "healthy"} {
		if !strings.Contains(body, want) {
			t.Fatalf("room overview missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestRoomInviteCreateRedirectIncludesJoinURL(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	router := chi.NewRouter()
	provider := &fakeRoomOpsProvider{}
	provider.inviteCreateFn = func(ctx context.Context, createdBy int64) (string, error) {
		return "token-123", nil
	}
	provider.joinURL = "http://127.0.0.1:8976/join?token=token-123"

	NewUIHandler(database, nil, nil, nil, &mockSSBStatus{}).WithRoomOps(provider).Mount(router)

	req := httptest.NewRequest(http.MethodPost, "/room/invites/create", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
	location := rr.Header().Get("Location")
	for _, want := range []string{"/room/aliases", "message=Invite+created", "invite_url="} {
		if !strings.Contains(location, want) {
			t.Fatalf("redirect missing %q: %s", want, location)
		}
	}
}

func TestRoomMemberRoleSetPolicyErrorRedirects(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	router := chi.NewRouter()
	provider := &fakeRoomOpsProvider{}
	provider.memberRoleSetFn = func(ctx context.Context, memberID int64, role roomdb.Role) error {
		return fmt.Errorf("member role updates are blocked")
	}
	NewUIHandler(database, nil, nil, nil, &mockSSBStatus{}).WithRoomOps(provider).Mount(router)

	req := httptest.NewRequest(http.MethodPost, "/room/members/role", strings.NewReader("member_id=12&role=moderator"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", rr.Code)
	}
	location := rr.Header().Get("Location")
	if !strings.Contains(location, "/room/members") || !strings.Contains(location, "error=") {
		t.Fatalf("unexpected redirect: %s", location)
	}
}

type fakeRoomOpsProvider struct {
	overview        RoomOverview
	overviewErr     error
	joinURL         string
	inviteCreateFn  func(ctx context.Context, createdBy int64) (string, error)
	memberRoleSetFn func(ctx context.Context, memberID int64, role roomdb.Role) error
	memberRemoveFn  func(ctx context.Context, memberID int64) error
	inviteRevokeFn  func(ctx context.Context, inviteID int64) error
	aliasRevokeFn   func(ctx context.Context, alias string) error
	deniedAddFn     func(ctx context.Context, feed refs.FeedRef, comment string) error
	deniedRemoveFn  func(ctx context.Context, deniedID int64) error
	members         []templates.RoomMemberRow
	invites         []templates.RoomInviteRow
	aliases         []templates.RoomAliasRow
	denied          []templates.RoomDeniedKeyRow
	attendants      []templates.RoomAttendantRow
	tunnels         []templates.RoomTunnelEndpointRow
}

func (f *fakeRoomOpsProvider) Overview(ctx context.Context) (RoomOverview, error) {
	if f.overviewErr != nil {
		return RoomOverview{}, f.overviewErr
	}
	if !f.overview.Available {
		f.overview.Available = true
	}
	if f.overview.Mode == roomdb.ModeUnknown {
		f.overview.Mode = roomdb.ModeCommunity
	}
	if f.overview.ModeLabel == "" {
		f.overview.ModeLabel = "Community"
	}
	if f.overview.ModeSummary == "" {
		f.overview.ModeSummary = "Invites require authenticated member access."
	}
	if f.overview.OperatorRole == roomdb.RoleUnknown || f.overview.OperatorRole == roomdb.RoleNone {
		f.overview.OperatorRole = roomdb.RoleAdmin
	}
	if f.overview.PolicyHint == "" {
		f.overview.PolicyHint = "Mode: Community · Operator role: admin"
	}
	return f.overview, nil
}

func (f *fakeRoomOpsProvider) MembersList(ctx context.Context) ([]templates.RoomMemberRow, error) {
	return f.members, nil
}

func (f *fakeRoomOpsProvider) MemberRoleSet(ctx context.Context, memberID int64, role roomdb.Role) error {
	if f.memberRoleSetFn != nil {
		return f.memberRoleSetFn(ctx, memberID, role)
	}
	return nil
}

func (f *fakeRoomOpsProvider) MemberRemove(ctx context.Context, memberID int64) error {
	if f.memberRemoveFn != nil {
		return f.memberRemoveFn(ctx, memberID)
	}
	return nil
}

func (f *fakeRoomOpsProvider) InvitesList(ctx context.Context) ([]templates.RoomInviteRow, error) {
	return f.invites, nil
}

func (f *fakeRoomOpsProvider) InviteCreate(ctx context.Context, createdBy int64) (string, error) {
	if f.inviteCreateFn != nil {
		return f.inviteCreateFn(ctx, createdBy)
	}
	return "", nil
}

func (f *fakeRoomOpsProvider) InviteRevoke(ctx context.Context, inviteID int64) error {
	if f.inviteRevokeFn != nil {
		return f.inviteRevokeFn(ctx, inviteID)
	}
	return nil
}

func (f *fakeRoomOpsProvider) AliasesList(ctx context.Context) ([]templates.RoomAliasRow, error) {
	return f.aliases, nil
}

func (f *fakeRoomOpsProvider) AliasRevoke(ctx context.Context, alias string) error {
	if f.aliasRevokeFn != nil {
		return f.aliasRevokeFn(ctx, alias)
	}
	return nil
}

func (f *fakeRoomOpsProvider) DeniedList(ctx context.Context) ([]templates.RoomDeniedKeyRow, error) {
	return f.denied, nil
}

func (f *fakeRoomOpsProvider) DeniedAdd(ctx context.Context, feed refs.FeedRef, comment string) error {
	if f.deniedAddFn != nil {
		return f.deniedAddFn(ctx, feed, comment)
	}
	return nil
}

func (f *fakeRoomOpsProvider) DeniedRemove(ctx context.Context, deniedID int64) error {
	if f.deniedRemoveFn != nil {
		return f.deniedRemoveFn(ctx, deniedID)
	}
	return nil
}

func (f *fakeRoomOpsProvider) AttendantsSnapshot(ctx context.Context) ([]templates.RoomAttendantRow, error) {
	return f.attendants, nil
}

func (f *fakeRoomOpsProvider) TunnelEndpointsSnapshot(ctx context.Context) ([]templates.RoomTunnelEndpointRow, error) {
	return f.tunnels, nil
}

func (f *fakeRoomOpsProvider) JoinURL(token string) string {
	if f.joinURL != "" {
		return f.joinURL
	}
	return "/join?token=" + token
}

func (f *fakeRoomOpsProvider) Close() error { return nil }
