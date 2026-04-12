package http

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomstate"
)

func testFeed(b byte) refs.FeedRef {
	var id [32]byte
	id[0] = b
	return *refs.MustNewFeedRef(id[:], refs.RefAlgoFeedSSB1)
}

type fixture struct {
	server  *Server
	key     refs.FeedRef
	members *fakeMembersService
	aliases *fakeAliasesService
	invites *fakeInvitesService
	config  *fakeRoomConfig
	auth    *fakeAuthService
	state   *roomstate.Manager
}

func newFixture() *fixture {
	key := testFeed(0)
	f := &fixture{
		key:     key,
		members: &fakeMembersService{byFeed: make(map[string]roomdb.Member)},
		aliases: &fakeAliasesService{byName: make(map[string]roomdb.Alias)},
		invites: &fakeInvitesService{createToken: "invite-123"},
		config:  &fakeRoomConfig{mode: roomdb.ModeOpen},
		auth:    &fakeAuthService{tokens: make(map[string]int64)},
		state:   roomstate.NewManager(),
	}
	f.server = New(Options{
		KeyPair:    &f.key,
		Members:    f.members,
		Aliases:    f.aliases,
		Invites:    f.invites,
		Config:     f.config,
		AuthTokens: f.auth,
		State:      f.state,
	})
	return f
}

func TestHandlerHealthz(t *testing.T) {
	f := newFixture()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	f.server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if rr.Body.String() != "ok\n" {
		t.Fatalf("expected body ok, got %q", rr.Body.String())
	}
}

func TestHandleHomeNotFoundForNonRoot(t *testing.T) {
	f := newFixture()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)

	f.server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rr.Code)
	}
}

func TestHandleHomeRendersCounts(t *testing.T) {
	f := newFixture()
	memberA := testFeed(1)
	memberB := testFeed(2)
	f.members.list = []roomdb.Member{
		{PubKey: memberA, Role: roomdb.RoleMember},
		{PubKey: memberB, Role: roomdb.RoleModerator},
	}
	f.aliases.list = []roomdb.Alias{
		{Name: "alpha", Owner: memberA},
	}
	f.state.AddPeer(testFeed(9), "127.0.0.1:8008")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	f.server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Members: 2 | Aliases: 1 | Online: 1") {
		t.Fatalf("unexpected body: %s", body)
	}
	if !strings.Contains(body, f.key.String()) {
		t.Fatalf("expected room id %q in body", f.key.String())
	}
}

func TestHandleJoinTokenRedirectsWhenTokenMissing(t *testing.T) {
	f := newFixture()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/join/", nil)

	f.server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected status 302, got %d", rr.Code)
	}
	if got := rr.Header().Get("Location"); got != "/join" {
		t.Fatalf("expected redirect to /join, got %q", got)
	}
}

func TestHandleJoinTokenRendersToken(t *testing.T) {
	f := newFixture()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/join/my-token", nil)

	f.server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Invite token: my-token") {
		t.Fatalf("expected token in body, got %q", rr.Body.String())
	}
}

func TestHandleLoginSSBPaths(t *testing.T) {
	t.Run("empty alias redirects", func(t *testing.T) {
		f := newFixture()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/login/", nil)

		f.server.Handler().ServeHTTP(rr, req)

		if rr.Code != http.StatusFound {
			t.Fatalf("expected status 302, got %d", rr.Code)
		}
		if got := rr.Header().Get("Location"); got != "/login" {
			t.Fatalf("expected redirect to /login, got %q", got)
		}
	})

	t.Run("alias returns challenge placeholder", func(t *testing.T) {
		f := newFixture()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/login/alice", nil)

		f.server.Handler().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "SSB login for alice") {
			t.Fatalf("unexpected body: %q", rr.Body.String())
		}
	})
}

func TestHandleBotsRendersMembers(t *testing.T) {
	f := newFixture()
	memberA := testFeed(1)
	memberB := testFeed(2)
	f.members.list = []roomdb.Member{
		{PubKey: memberA, Role: roomdb.RoleMember},
		{PubKey: memberB, Role: roomdb.RoleAdmin},
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/bots", nil)
	f.server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, memberA.String()) || !strings.Contains(body, memberB.String()) {
		t.Fatalf("expected both feed refs in body: %s", body)
	}
	if !strings.Contains(body, roomdb.RoleMember.String()) || !strings.Contains(body, roomdb.RoleAdmin.String()) {
		t.Fatalf("expected role names in body: %s", body)
	}
}

func TestHandleBotDetailNotFoundCases(t *testing.T) {
	t.Run("missing id", func(t *testing.T) {
		f := newFixture()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/bots/", nil)

		f.server.Handler().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d", rr.Code)
		}
	})

	t.Run("invalid feed", func(t *testing.T) {
		f := newFixture()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/bots/not-a-feed-ref", nil)

		f.server.Handler().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d", rr.Code)
		}
	})

	t.Run("missing member", func(t *testing.T) {
		f := newFixture()
		feed := testFeed(7)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/bots/"+feed.String(), nil)

		f.server.Handler().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected status 404, got %d", rr.Code)
		}
	})
}

func TestHandleBotDetailRendersAliasesForSelectedMember(t *testing.T) {
	f := newFixture()
	member := testFeed(1)
	other := testFeed(2)
	f.members.byFeed[member.String()] = roomdb.Member{PubKey: member, Role: roomdb.RoleModerator}
	f.aliases.list = []roomdb.Alias{
		{Name: "alpha", Owner: member},
		{Name: "beta", Owner: member},
		{Name: "gamma", Owner: other},
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/bots/"+member.String(), nil)
	f.server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Role: moderator") {
		t.Fatalf("expected role in body: %s", body)
	}
	if !strings.Contains(body, "alpha") || !strings.Contains(body, "beta") {
		t.Fatalf("expected member aliases in body: %s", body)
	}
	if strings.Contains(body, "gamma") {
		t.Fatalf("unexpected alias from other member in body: %s", body)
	}
}

func TestHandleCreateInvitePaths(t *testing.T) {
	t.Run("requires post", func(t *testing.T) {
		f := newFixture()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/create-invite", nil)

		f.server.Handler().ServeHTTP(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected status 405, got %d", rr.Code)
		}
	})

	t.Run("blocked by privacy mode", func(t *testing.T) {
		f := newFixture()
		f.config.mode = roomdb.ModeRestricted
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/create-invite", nil)

		f.server.Handler().ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d", rr.Code)
		}
	})

	t.Run("invite create error", func(t *testing.T) {
		f := newFixture()
		f.invites.createErr = errors.New("boom")
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/create-invite", nil)

		f.server.Handler().ServeHTTP(rr, req)

		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("expected status 500, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "boom") {
			t.Fatalf("expected error in body, got %q", rr.Body.String())
		}
	})

	t.Run("success http and https", func(t *testing.T) {
		cases := []struct {
			name     string
			tls      bool
			expected string
		}{
			{name: "http", tls: false, expected: "http://room.test/join?token=invite-123"},
			{name: "https", tls: true, expected: "https://room.test/join?token=invite-123"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				f := newFixture()
				rr := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/create-invite", nil)
				req.Host = "room.test"
				if tc.tls {
					req.TLS = &tls.ConnectionState{}
				}

				f.server.Handler().ServeHTTP(rr, req)

				if rr.Code != http.StatusOK {
					t.Fatalf("expected status 200, got %d", rr.Code)
				}
				if f.invites.lastCreateBy != -1 {
					t.Fatalf("expected invite creator -1, got %d", f.invites.lastCreateBy)
				}

				var payload map[string]string
				if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if payload["url"] != tc.expected {
					t.Fatalf("expected url %q, got %q", tc.expected, payload["url"])
				}
			})
		}
	})
}

func TestAuthMiddlewarePaths(t *testing.T) {
	t.Run("missing cookie redirects", func(t *testing.T) {
		f := newFixture()
		h := f.server.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Fatal("next handler should not run")
		}))
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)

		h.ServeHTTP(rr, req)

		if rr.Code != http.StatusFound {
			t.Fatalf("expected status 302, got %d", rr.Code)
		}
		if got := rr.Header().Get("Location"); got != "/login" {
			t.Fatalf("expected redirect to /login, got %q", got)
		}
	})

	t.Run("bad token redirects", func(t *testing.T) {
		f := newFixture()
		f.auth.checkErr = errors.New("bad token")
		h := f.server.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			t.Fatal("next handler should not run")
		}))
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "auth_token", Value: "bad"})

		h.ServeHTTP(rr, req)

		if rr.Code != http.StatusFound {
			t.Fatalf("expected status 302, got %d", rr.Code)
		}
	})

	t.Run("valid token sets member id in context", func(t *testing.T) {
		f := newFixture()
		f.auth.tokens["good"] = 42
		called := false
		h := f.server.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			if got, ok := r.Context().Value("memberID").(int64); !ok || got != 42 {
				t.Fatalf("expected member id 42 in context, got %#v", r.Context().Value("memberID"))
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "auth_token", Value: "good"})

		h.ServeHTTP(rr, req)

		if !called {
			t.Fatal("expected next handler to run")
		}
		if rr.Code != http.StatusNoContent {
			t.Fatalf("expected status 204, got %d", rr.Code)
		}
	})
}

func TestCheckAuth(t *testing.T) {
	f := newFixture()
	f.auth.tokens["ok"] = 99

	t.Run("missing cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if _, ok := f.server.CheckAuth(req); ok {
			t.Fatal("expected auth check to fail")
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "auth_token", Value: "bad"})
		if _, ok := f.server.CheckAuth(req); ok {
			t.Fatal("expected auth check to fail")
		}
	})

	t.Run("valid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "auth_token", Value: "ok"})
		memberID, ok := f.server.CheckAuth(req)
		if !ok {
			t.Fatal("expected auth check to succeed")
		}
		if memberID != 99 {
			t.Fatalf("expected member id 99, got %d", memberID)
		}
	})
}

func TestVerifySignatureComparesBytes(t *testing.T) {
	feed := testFeed(1)
	challenge := []byte("challenge")

	if !verifySignature(feed, challenge, []byte("challenge")) {
		t.Fatal("expected matching challenge/response to verify")
	}
	if verifySignature(feed, challenge, []byte("different")) {
		t.Fatal("expected mismatched challenge/response to fail")
	}
}

func TestServeMUXRPC(t *testing.T) {
	f := newFixture()
	if err := f.server.ServeMUXRPC(context.Background()); err == nil {
		t.Fatal("expected error when muxrpc handler is missing")
	}

	f.server.SetMuxRPCHandler(noopMuxrpcHandler{})
	if err := f.server.ServeMUXRPC(context.Background()); err != nil {
		t.Fatalf("expected nil error with handler set, got: %v", err)
	}
}

func TestGenerateAuthToken(t *testing.T) {
	token := GenerateAuthToken()
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	raw, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("failed to decode generated token: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("expected 32 random bytes, got %d", len(raw))
	}
}

type noopMuxrpcHandler struct{}

func (noopMuxrpcHandler) Handled(muxrpc.Method) bool { return false }
func (noopMuxrpcHandler) HandleCall(context.Context, *muxrpc.Request) {
}
func (noopMuxrpcHandler) HandleConnect(context.Context, muxrpc.Endpoint) {
}

type fakeMembersService struct {
	list      []roomdb.Member
	listErr   error
	byFeed    map[string]roomdb.Member
	byFeedErr error
}

func (f *fakeMembersService) Add(context.Context, refs.FeedRef, roomdb.Role) (int64, error) {
	return 0, errors.New("not implemented")
}

func (f *fakeMembersService) GetByID(context.Context, int64) (roomdb.Member, error) {
	return roomdb.Member{}, errors.New("not implemented")
}

func (f *fakeMembersService) GetByFeed(_ context.Context, feed refs.FeedRef) (roomdb.Member, error) {
	if f.byFeedErr != nil {
		return roomdb.Member{}, f.byFeedErr
	}
	m, ok := f.byFeed[feed.String()]
	if !ok {
		return roomdb.Member{}, errors.New("not found")
	}
	return m, nil
}

func (f *fakeMembersService) List(context.Context) ([]roomdb.Member, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]roomdb.Member, len(f.list))
	copy(out, f.list)
	return out, nil
}

func (f *fakeMembersService) Count(context.Context) (uint, error) {
	return uint(len(f.list)), nil
}

func (f *fakeMembersService) RemoveFeed(context.Context, refs.FeedRef) error {
	return nil
}

func (f *fakeMembersService) RemoveID(context.Context, int64) error {
	return nil
}

func (f *fakeMembersService) SetRole(context.Context, int64, roomdb.Role) error {
	return nil
}

type fakeAliasesService struct {
	list       []roomdb.Alias
	listErr    error
	byName     map[string]roomdb.Alias
	resolveErr error
}

func (f *fakeAliasesService) Resolve(_ context.Context, alias string) (roomdb.Alias, error) {
	if f.resolveErr != nil {
		return roomdb.Alias{}, f.resolveErr
	}
	a, ok := f.byName[alias]
	if !ok {
		return roomdb.Alias{}, errors.New("not found")
	}
	return a, nil
}

func (f *fakeAliasesService) GetByID(context.Context, int64) (roomdb.Alias, error) {
	return roomdb.Alias{}, errors.New("not implemented")
}

func (f *fakeAliasesService) List(context.Context) ([]roomdb.Alias, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]roomdb.Alias, len(f.list))
	copy(out, f.list)
	return out, nil
}

func (f *fakeAliasesService) Register(context.Context, string, refs.FeedRef, []byte) error {
	return nil
}

func (f *fakeAliasesService) Revoke(context.Context, string) error {
	return nil
}

type fakeInvitesService struct {
	createToken  string
	createErr    error
	lastCreateBy int64
}

func (f *fakeInvitesService) Create(_ context.Context, createdBy int64) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	f.lastCreateBy = createdBy
	if f.createToken == "" {
		return "invite-default", nil
	}
	return f.createToken, nil
}

func (f *fakeInvitesService) Consume(context.Context, string, refs.FeedRef) (roomdb.Invite, error) {
	return roomdb.Invite{}, errors.New("not implemented")
}

func (f *fakeInvitesService) GetByToken(context.Context, string) (roomdb.Invite, error) {
	return roomdb.Invite{}, errors.New("not implemented")
}

func (f *fakeInvitesService) GetByID(context.Context, int64) (roomdb.Invite, error) {
	return roomdb.Invite{}, errors.New("not implemented")
}

func (f *fakeInvitesService) List(context.Context) ([]roomdb.Invite, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeInvitesService) Count(context.Context, bool) (uint, error) {
	return 0, errors.New("not implemented")
}

func (f *fakeInvitesService) Revoke(context.Context, int64) error {
	return errors.New("not implemented")
}

type fakeRoomConfig struct {
	mode roomdb.PrivacyMode
	err  error
}

func (f *fakeRoomConfig) GetPrivacyMode(context.Context) (roomdb.PrivacyMode, error) {
	if f.err != nil {
		return roomdb.ModeUnknown, f.err
	}
	return f.mode, nil
}

func (f *fakeRoomConfig) SetPrivacyMode(context.Context, roomdb.PrivacyMode) error {
	return nil
}

func (f *fakeRoomConfig) GetDefaultLanguage(context.Context) (string, error) {
	return "en", nil
}

func (f *fakeRoomConfig) SetDefaultLanguage(context.Context, string) error {
	return nil
}

type fakeAuthService struct {
	tokens   map[string]int64
	checkErr error
}

func (f *fakeAuthService) CreateToken(context.Context, int64) (string, error) {
	return "", errors.New("not implemented")
}

func (f *fakeAuthService) CheckToken(_ context.Context, token string) (int64, error) {
	if f.checkErr != nil {
		return 0, f.checkErr
	}
	memberID, ok := f.tokens[token]
	if !ok {
		return 0, errors.New("not found")
	}
	return memberID, nil
}

func (f *fakeAuthService) RemoveToken(context.Context, string) error {
	return nil
}

func (f *fakeAuthService) WipeTokensForMember(context.Context, int64) error {
	return nil
}

func (f *fakeAuthService) RotateToken(context.Context, string) (string, error) {
	return "", errors.New("not implemented")
}

func (f *fakeAuthService) GetTokenInfo(context.Context, string) (roomdb.TokenInfo, error) {
	return roomdb.TokenInfo{
		CreatedAt:  time.Unix(0, 0),
		LastUsedAt: time.Unix(0, 0),
	}, nil
}
