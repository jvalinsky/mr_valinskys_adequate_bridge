package room

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
)

func TestRuntimeConfigValidationRejectsInvalidMode(t *testing.T) {
	cfg := Config{
		ListenAddr:     "127.0.0.1:8989",
		HTTPListenAddr: "127.0.0.1:8976",
		RepoPath:       t.TempDir(),
		Mode:           "not-a-mode",
	}

	err := cfg.withDefaults().validate()
	if err == nil || !strings.Contains(err.Error(), "room-mode") {
		t.Fatalf("expected room-mode validation error, got %v", err)
	}
}

func TestRuntimeConfigValidationRequiresDomainForNonLoopback(t *testing.T) {
	cfg := Config{
		ListenAddr:     "0.0.0.0:8989",
		HTTPListenAddr: "127.0.0.1:8976",
		RepoPath:       t.TempDir(),
		Mode:           "community",
	}

	err := cfg.withDefaults().validate()
	if err == nil || !strings.Contains(err.Error(), "room-https-domain") {
		t.Fatalf("expected room-https-domain validation error, got %v", err)
	}
}

func TestRuntimeStartsAndServesHealth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := Start(ctx, Config{
		ListenAddr:     "127.0.0.1:0",
		HTTPListenAddr: "127.0.0.1:0",
		RepoPath:       t.TempDir(),
		Mode:           "community",
	}, log.New(io.Discard, "", 0))
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("start runtime: %v", err)
	}
	defer rt.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + rt.HTTPAddr() + "/healthz")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRuntimeServesLandingBotsAndStockRoutes(t *testing.T) {
	database := openTestBridgeAccountsDB(t, []db.BridgedAccount{
		{
			ATDID:     "did:plc:runtime-active-bot",
			SSBFeedID: mustRuntimeTestFeedRef(t, 3).String(),
			Active:    true,
		},
		{
			ATDID:     "did:plc:runtime-inactive-bot",
			SSBFeedID: mustRuntimeTestFeedRef(t, 4).String(),
			Active:    false,
		},
	})

	rt := startTestRuntime(t, "open", database)
	client := &http.Client{Timeout: 2 * time.Second}

	landingBody, landingStatus := getRuntimePath(t, client, rt, "/")
	if landingStatus != http.StatusOK {
		t.Fatalf("landing page expected 200, got %d", landingStatus)
	}
	for _, want := range []string{
		"Create room invite",
		"Browse bridged bots",
		"Open room sign-in",
	} {
		if !strings.Contains(landingBody, want) {
			t.Fatalf("landing page missing %q\nbody:\n%s", want, landingBody)
		}
	}

	botsBody, botsStatus := getRuntimePath(t, client, rt, "/bots")
	if botsStatus != http.StatusOK {
		t.Fatalf("bots page expected 200, got %d", botsStatus)
	}
	// Cards show abbreviated DID and link to detail page.
	if !strings.Contains(botsBody, "did:plc:runtime-a") {
		t.Fatalf("bots page missing active bridged bot abbreviation\nbody:\n%s", botsBody)
	}
	if !strings.Contains(botsBody, "/bots/did:plc:runtime-active-bot") {
		t.Fatalf("bots page missing detail link\nbody:\n%s", botsBody)
	}
	if strings.Contains(botsBody, "did:plc:runtime-inactive-bot") {
		t.Fatalf("bots page unexpectedly included inactive bridged bot\nbody:\n%s", botsBody)
	}

	// Bot detail page.
	detailBody, detailStatus := getRuntimePath(t, client, rt, "/bots/did:plc:runtime-active-bot")
	if detailStatus != http.StatusOK {
		t.Fatalf("detail page expected 200, got %d", detailStatus)
	}
	for _, want := range []string{
		"did:plc:runtime-active-bot",
		mustRuntimeTestFeedRef(t, 3).String(),
		"Copy DID",
		"Bot detail",
	} {
		if !strings.Contains(detailBody, want) {
			t.Fatalf("detail page missing %q\nbody:\n%s", want, detailBody)
		}
	}

	authBody, authStatus := getRuntimePath(t, client, rt, "/login")
	if authStatus != http.StatusOK {
		t.Fatalf("auth route expected 200, got %d", authStatus)
	}
	if !strings.Contains(authBody, "/fallback/login") {
		t.Fatalf("stock auth page missing fallback auth link\nbody:\n%s", authBody)
	}
}

func TestRuntimeCreateInviteJSONOpenMode(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}

	req, err := http.NewRequest(http.MethodPost, "http://"+rt.HTTPAddr()+"/create-invite", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("create invite request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d\nbody:\n%s", resp.StatusCode, string(body))
	}

	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}
	if !strings.Contains(payload["url"], "/join?token=") {
		t.Fatalf("expected invite facade url, got %q", payload["url"])
	}
}

func TestRuntimeJoinPageWithValidToken(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}

	createReq, _ := http.NewRequest(http.MethodPost, "http://"+rt.HTTPAddr()+"/create-invite", nil)
	createReq.Header.Set("Accept", "application/json")
	createResp, err := client.Do(createReq)
	if err != nil {
		t.Fatalf("create invite failed: %v", err)
	}
	defer createResp.Body.Close()

	var invitePayload map[string]string
	if err := json.NewDecoder(createResp.Body).Decode(&invitePayload); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}

	token := ""
	if idx := strings.Index(invitePayload["url"], "token="); idx != -1 {
		token = invitePayload["url"][idx+6:]
	}
	if token == "" {
		t.Fatalf("no token in invite url: %s", invitePayload["url"])
	}

	joinResp, err := client.Get("http://" + rt.HTTPAddr() + "/join?token=" + token)
	if err != nil {
		t.Fatalf("join page request failed: %v", err)
	}
	defer joinResp.Body.Close()

	if joinResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(joinResp.Body)
		t.Fatalf("expected 200 for valid token, got %d\nbody: %s", joinResp.StatusCode, string(body))
	}

	body, _ := io.ReadAll(joinResp.Body)
	if !strings.Contains(string(body), "Join Room") {
		t.Fatalf("join page missing expected content\nbody:\n%s", string(body))
	}
}

func TestRuntimeJoinPageWithInvalidToken(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}

	joinResp, err := client.Get("http://" + rt.HTTPAddr() + "/join?token=invalid-token-12345")
	if err != nil {
		t.Fatalf("join page request failed: %v", err)
	}
	defer joinResp.Body.Close()

	if joinResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for invalid token, got %d", joinResp.StatusCode)
	}
}

func TestRuntimeJoinPageWithNoToken(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}

	joinResp, err := client.Get("http://" + rt.HTTPAddr() + "/join")
	if err != nil {
		t.Fatalf("join page request failed: %v", err)
	}
	defer joinResp.Body.Close()

	if joinResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for no token, got %d", joinResp.StatusCode)
	}
}

func TestRuntimeCreateInviteJSONFailsOutsideOpenMode(t *testing.T) {
	rt := startTestRuntime(t, "community", nil)
	client := &http.Client{Timeout: 2 * time.Second}

	req, err := http.NewRequest(http.MethodPost, "http://"+rt.HTTPAddr()+"/create-invite", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("create invite request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d\nbody:\n%s", resp.StatusCode, string(body))
	}

	var payload struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}
	if payload.Status != "failed" {
		t.Fatalf("expected failed status, got %q", payload.Status)
	}
	if !strings.Contains(payload.Error, "room mode is open") {
		t.Fatalf("expected explanatory error, got %q", payload.Error)
	}
}

func TestRuntimeCloseIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := Start(ctx, Config{
		ListenAddr:     "127.0.0.1:0",
		HTTPListenAddr: "127.0.0.1:0",
		RepoPath:       t.TempDir(),
		Mode:           "community",
	}, log.New(io.Discard, "", 0))
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("start runtime: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = rt.Close()
	}()
	go func() {
		defer wg.Done()
		_ = rt.Close()
	}()
	wg.Wait()
}

func startTestRuntime(t *testing.T, mode string, bridgeAccounts interface {
	ActiveBridgeAccountLister
	ActiveBridgeAccountDetailer
}) *Runtime {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	rt, err := Start(ctx, Config{
		ListenAddr:            "127.0.0.1:0",
		HTTPListenAddr:        "127.0.0.1:0",
		RepoPath:              t.TempDir(),
		Mode:                  mode,
		BridgeAccountLister:   bridgeAccounts,
		BridgeAccountDetailer: bridgeAccounts,
	}, log.New(io.Discard, "", 0))
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("start runtime: %v", err)
	}

	t.Cleanup(func() {
		_ = rt.Close()
	})
	return rt
}

func getRuntimePath(t *testing.T, client *http.Client, rt *Runtime, path string) (string, int) {
	t.Helper()

	resp, err := client.Get("http://" + rt.HTTPAddr() + path)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s response: %v", path, err)
	}
	return string(body), resp.StatusCode
}

func openTestBridgeAccountsDB(t *testing.T, accounts []db.BridgedAccount) *db.DB {
	t.Helper()

	database, err := db.Open(filepath.Join(t.TempDir(), "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open bridge db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	for _, account := range accounts {
		if err := database.AddBridgedAccount(t.Context(), account); err != nil {
			t.Fatalf("add bridged account %s: %v", account.ATDID, err)
		}
	}

	return database
}

func mustRuntimeTestFeedRef(t *testing.T, fill byte) *refs.FeedRef {
	t.Helper()

	ref, err := refs.NewFeedRef(bytes.Repeat([]byte{fill}, 32), refs.RefAlgoFeedSSB1)
	if err != nil {
		t.Fatalf("create test feed ref: %v", err)
	}
	return ref
}

func TestRuntimeLoginPage(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.Get("http://" + rt.HTTPAddr() + "/login")
	if err != nil {
		t.Fatalf("login page request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Sign In") {
		t.Fatalf("login page missing Sign In text\nbody:\n%s", string(body))
	}
	if !strings.Contains(string(body), "/fallback/login") {
		t.Fatalf("login page missing fallback link\nbody:\n%s", string(body))
	}
}

func TestRuntimeLoginPostWithInvalidCredentials(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.PostForm("http://"+rt.HTTPAddr()+"/login", url.Values{
		"username": {"nonexistent"},
		"password": {"wrongpassword"},
	})
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid credentials, got %d", resp.StatusCode)
	}
}

func TestRuntimeResetPasswordPage(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.Get("http://" + rt.HTTPAddr() + "/reset-password")
	if err != nil {
		t.Fatalf("reset password page request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Reset Password") {
		t.Fatalf("reset password page missing title\nbody:\n%s", string(body))
	}
}

func TestRuntimeResetPasswordPostWithInvalidToken(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.PostForm("http://"+rt.HTTPAddr()+"/reset-password", url.Values{
		"token":    {"invalid-token"},
		"password": {"newpassword123"},
	})
	if err != nil {
		t.Fatalf("reset password request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid token, got %d", resp.StatusCode)
	}
}

func TestRuntimeCreateInviteRequiresJSONAccept(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.Post("http://"+rt.HTTPAddr()+"/create-invite", "", nil)
	if err != nil {
		t.Fatalf("create invite request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 even without JSON Accept header, got %d", resp.StatusCode)
	}
}

func TestRuntimeJoinPageWithUsedToken(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}

	createReq, _ := http.NewRequest(http.MethodPost, "http://"+rt.HTTPAddr()+"/create-invite", nil)
	createReq.Header.Set("Accept", "application/json")
	createResp, err := client.Do(createReq)
	if err != nil {
		t.Fatalf("create invite failed: %v", err)
	}
	defer createResp.Body.Close()

	var invitePayload map[string]string
	if err := json.NewDecoder(createResp.Body).Decode(&invitePayload); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}

	token := ""
	if idx := strings.Index(invitePayload["url"], "token="); idx != -1 {
		token = invitePayload["url"][idx+6:]
	}

	joinResp, err := client.Get("http://" + rt.HTTPAddr() + "/join?token=" + token)
	if err != nil {
		t.Fatalf("join page request failed: %v", err)
	}
	defer joinResp.Body.Close()

	if joinResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(joinResp.Body)
		t.Fatalf("expected 200 for valid token, got %d\nbody: %s", joinResp.StatusCode, string(body))
	}
}

func TestRuntimeLoginPostWithMissingFields(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.PostForm("http://"+rt.HTTPAddr()+"/login", url.Values{
		"username": {""},
		"password": {""},
	})
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing fields, got %d", resp.StatusCode)
	}
}

func TestRuntimeNilReceiver(t *testing.T) {
	var r *Runtime
	if r.Addr() != "" {
		t.Errorf("expected empty Addr for nil runtime, got %q", r.Addr())
	}
	if r.HTTPAddr() != "" {
		t.Errorf("expected empty HTTPAddr for nil runtime, got %q", r.HTTPAddr())
	}
	if r.RoomFeed().Ref() != "@AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=." {
		t.Errorf("expected zero RoomFeed for nil runtime, got %q", r.RoomFeed().Ref())
	}
	if err := r.AddMember(context.Background(), refs.FeedRef{}, roomdb.RoleMember); err == nil {
		t.Error("expected error from AddMember on nil runtime")
	}
}

func TestAddMemberError(t *testing.T) {
	rt := &Runtime{} // DB not initialized
	err := rt.AddMember(context.Background(), refs.FeedRef{}, roomdb.RoleMember)
	if err == nil {
		t.Fatal("expected error for uninitialized DB")
	}
}

func TestHandleJoinSubmit(t *testing.T) {
	h := &inviteHandler{}
	req := httptest.NewRequest(http.MethodPost, "/join?token=test", nil)
	rr := httptest.NewRecorder()
	h.handleJoinSubmit(rr, req, "test")
	if rr.Code != http.StatusNotImplemented {
		t.Errorf("expected 501, got %d", rr.Code)
	}
}

func TestWithContext(t *testing.T) {
	errBoom := fmt.Errorf("boom")
	h := withContext(func(ctx context.Context) error {
		return errBoom
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "boom") {
		t.Errorf("expected boom in body, got %q", rr.Body.String())
	}
}

func TestJoinErrors(t *testing.T) {
	if joinErrors(nil) != nil {
		t.Error("expected nil for nil input")
	}
	e1 := fmt.Errorf("err1")
	if joinErrors([]error{e1}) != e1 {
		t.Error("expected e1 for single error")
	}
	e2 := fmt.Errorf("err2")
	joined := joinErrors([]error{e1, e2})
	if joined == nil || !strings.Contains(joined.Error(), "multiple errors") {
		t.Error("expected multiple errors message")
	}
}

func TestAuthHandlersSubmit(t *testing.T) {
	// 1. handleLoginSubmit invalid credentials
	h := &authHandler{authFallback: &mockAuthFallback{}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=foo&password=bar"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.handleLogin(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}

	// 2. handleResetPasswordSubmit invalid token
	req2 := httptest.NewRequest(http.MethodPost, "/reset-password", strings.NewReader("token=bad&password=new"))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr2 := httptest.NewRecorder()
	h.handleResetPassword(rr2, req2)
	if rr2.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr2.Code)
	}
}

type mockAuthFallback struct{}

func (m *mockAuthFallback) Check(ctx context.Context, u, p string) (int64, error) {
	return 0, fmt.Errorf("fail")
}
func (m *mockAuthFallback) SetPassword(ctx context.Context, id int64, p string) error { return nil }
func (m *mockAuthFallback) CreateResetToken(ctx context.Context, c, f int64) (string, error) {
	return "", nil
}
func (m *mockAuthFallback) SetPasswordWithToken(ctx context.Context, t, p string) error {
	return fmt.Errorf("fail")
}

func TestInviteHandlersErrors(t *testing.T) {
	h := &inviteHandler{config: &mockRoomConfig{err: fmt.Errorf("mode fail")}}
	req := httptest.NewRequest(http.MethodGet, "/create-invite", nil)
	rr := httptest.NewRecorder()
	h.handleCreateInvite(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

type mockRoomConfig struct {
	err error
}

func (m *mockRoomConfig) GetPrivacyMode(ctx context.Context) (roomdb.PrivacyMode, error) {
	return roomdb.ModeUnknown, m.err
}
func (m *mockRoomConfig) SetPrivacyMode(ctx context.Context, mode roomdb.PrivacyMode) error {
	return nil
}
func (m *mockRoomConfig) GetDefaultLanguage(ctx context.Context) (string, error)    { return "", nil }
func (m *mockRoomConfig) SetDefaultLanguage(ctx context.Context, lang string) error { return nil }

func TestRuntimeHandleMUXRPCConn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := Start(ctx, Config{
		ListenAddr:     "127.0.0.1:0",
		HTTPListenAddr: "127.0.0.1:0",
		RepoPath:       t.TempDir(),
		Mode:           "community",
	}, log.New(io.Discard, "", 0))
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("start runtime: %v", err)
	}
	defer rt.Close()

	// Dial the muxrpc listener
	conn, err := net.Dial("tcp", rt.Addr())
	if err != nil {
		t.Fatalf("dial muxrpc: %v", err)
	}
	defer conn.Close()

	// Give it a moment to accept
	time.Sleep(100 * time.Millisecond)

	// Closing the context should cause handleMUXRPCConn to exit
	cancel()

	// Give it a moment to close the conn
	time.Sleep(100 * time.Millisecond)
}

func TestRuntimeHandleMUXRPCConnExit(t *testing.T) {
	rt := &Runtime{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	conn, _ := net.Dial("tcp", ln.Addr().String())
	defer conn.Close()
	// This will just return immediately because ctx is done
	rt.handleMUXRPCConn(ctx, conn)
}

func TestStartListenErrors(t *testing.T) {
	ctx := context.Background()

	// 1. Invalid HTTP listen addr
	_, err := Start(ctx, Config{
		HTTPListenAddr: "invalid",
		ListenAddr:     "127.0.0.1:0",
		RepoPath:       t.TempDir(),
		Mode:           "community",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid room HTTP listen addr") {
		t.Errorf("expected validation error for bad http addr, got %v", err)
	}

	// 2. HTTP port in use (trigger listen error)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	_, err = Start(ctx, Config{
		HTTPListenAddr: ln.Addr().String(),
		ListenAddr:     "127.0.0.1:0",
		RepoPath:       t.TempDir(),
		Mode:           "community",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "listen http") {
		t.Errorf("expected listen error for occupied http port, got %v", err)
	}

	// 3. MUXRPC port in use
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln2.Close()
	_, err = Start(ctx, Config{
		HTTPListenAddr: "127.0.0.1:0",
		ListenAddr:     ln2.Addr().String(),
		RepoPath:       t.TempDir(),
		Mode:           "community",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "listen muxrpc") {
		t.Errorf("expected listen error for occupied muxrpc port, got %v", err)
	}
}
