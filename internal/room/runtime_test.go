package room

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	roomhandlers "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/handlers/room"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb/sqlite"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomstate"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream"
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

func TestRuntimeInitHandlersUsesProvidedMux(t *testing.T) {
	kp, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	roomDB, err := sqlite.Open(filepath.Join(t.TempDir(), "room.sqlite"))
	if err != nil {
		t.Fatalf("open room db: %v", err)
	}
	defer roomDB.Close()

	providedMux := &muxrpc.HandlerMux{}
	rt := &Runtime{
		cfg: Config{
			HandlerMux: providedMux,
		},
		keyPair: kp,
		roomDB:  roomDB,
		state:   roomstate.NewManager(),
	}

	rt.initHandlers()

	if rt.handler != providedMux {
		t.Fatalf("expected runtime handler to use provided mux")
	}
	if !providedMux.Handled(muxrpc.Method{"whoami"}) {
		t.Fatalf("expected whoami handler to be registered on provided mux")
	}
	if !providedMux.Handled(muxrpc.Method{"tunnel", "announce"}) {
		t.Fatalf("expected tunnel handlers to be registered on provided mux")
	}
	if rt.manifest == nil {
		t.Fatalf("expected room manifest")
	}
	entries := rt.manifest.EntriesByType()
	if !containsString(entries["async"], "room.metadata") {
		t.Fatalf("expected room.metadata in room manifest async entries: %#v", entries)
	}
	if !containsString(entries["duplex"], "tunnel.connect") {
		t.Fatalf("expected tunnel.connect in room manifest duplex entries: %#v", entries)
	}
}

func TestRuntimeInitHandlersAllocatesMuxWhenNil(t *testing.T) {
	kp, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	roomDB, err := sqlite.Open(filepath.Join(t.TempDir(), "room.sqlite"))
	if err != nil {
		t.Fatalf("open room db: %v", err)
	}
	defer roomDB.Close()

	rt := &Runtime{
		cfg:     Config{},
		keyPair: kp,
		roomDB:  roomDB,
		state:   roomstate.NewManager(),
	}

	rt.initHandlers()

	mux, ok := rt.handler.(*muxrpc.HandlerMux)
	if !ok || mux == nil {
		t.Fatalf("expected runtime to allocate a handler mux when config.HandlerMux is nil")
	}
	if !mux.Handled(muxrpc.Method{"whoami"}) {
		t.Fatalf("expected whoami handler to be registered on allocated mux")
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
		"Open room: anyone can create an invite",
		"Create invite",
		"Browse bridged bots",
		"Open to everyone",
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
	if !strings.Contains(string(body), "Join with an invite") {
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
	if payload.Status != "error" {
		t.Fatalf("expected error status, got %q", payload.Status)
	}
	if !strings.Contains(payload.Error, "authenticated member role") {
		t.Fatalf("expected explanatory error, got %q", payload.Error)
	}
}

func TestRuntimeCreateInviteHTMLRedirectsToLoginOutsideOpenMode(t *testing.T) {
	rt := startTestRuntime(t, "community", nil)
	client := newNoRedirectClient(2 * time.Second)

	req, err := http.NewRequest(http.MethodGet, "http://"+rt.HTTPAddr()+"/create-invite", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("create invite request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "/login?next=%2Fcreate-invite") {
		t.Fatalf("expected login redirect with next parameter, got %q", loc)
	}
}

func TestRuntimeCreateInviteCommunityModeAllowsAuthenticatedMember(t *testing.T) {
	rt := startTestRuntime(t, "community", nil)
	username := seedFallbackMember(t, rt, roomdb.RoleMember, 0x21, "pw-member")
	client := loginRuntimeMember(t, rt, username, "pw-member", "/create-invite")

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
		t.Fatalf("expected 200, got %d\nbody: %s", resp.StatusCode, string(body))
	}
}

func TestRuntimeCreateInviteRestrictedModeRolePolicy(t *testing.T) {
	tests := []struct {
		name       string
		role       roomdb.Role
		expectCode int
	}{
		{name: "member denied", role: roomdb.RoleMember, expectCode: http.StatusForbidden},
		{name: "moderator allowed", role: roomdb.RoleModerator, expectCode: http.StatusOK},
		{name: "admin allowed", role: roomdb.RoleAdmin, expectCode: http.StatusOK},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt := startTestRuntime(t, "restricted", nil)
			username := seedFallbackMember(t, rt, tc.role, byte(0x30+i), "pw-restricted")
			client := loginRuntimeMember(t, rt, username, "pw-restricted", "/create-invite")

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

			if resp.StatusCode != tc.expectCode {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected %d, got %d\nbody: %s", tc.expectCode, resp.StatusCode, string(body))
			}
		})
	}
}

func TestRuntimeJoinFacadeSupportsTokenAndInviteAliases(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}
	token := createInviteAndExtractToken(t, client, rt.HTTPAddr())

	facadeResp, err := client.Get("http://" + rt.HTTPAddr() + "/join?invite=" + url.QueryEscape(token) + "&encoding=json")
	if err != nil {
		t.Fatalf("join facade json request failed: %v", err)
	}
	defer facadeResp.Body.Close()

	if facadeResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(facadeResp.Body)
		t.Fatalf("expected 200, got %d\nbody: %s", facadeResp.StatusCode, string(body))
	}

	var payload struct {
		Status string `json:"status"`
		Invite string `json:"invite"`
		PostTo string `json:"postTo"`
	}
	if err := json.NewDecoder(facadeResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode façade payload: %v", err)
	}
	if payload.Invite != token {
		t.Fatalf("expected invite token %q, got %q", token, payload.Invite)
	}
	if !strings.Contains(payload.PostTo, "/invite/consume") {
		t.Fatalf("expected postTo consume endpoint, got %q", payload.PostTo)
	}

	htmlResp, err := client.Get("http://" + rt.HTTPAddr() + "/join?token=" + url.QueryEscape(token) + "&invite=bad-token")
	if err != nil {
		t.Fatalf("join html request failed: %v", err)
	}
	defer htmlResp.Body.Close()

	if htmlResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(htmlResp.Body)
		t.Fatalf("expected 200, got %d\nbody: %s", htmlResp.StatusCode, string(body))
	}

	body, _ := io.ReadAll(htmlResp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, `id="claim-invite-uri"`) {
		t.Fatalf("join page missing claim link\nbody:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, "Open in SSB client") {
		t.Fatalf("join page missing deep-link copy\nbody:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, "claim-http-invite") {
		t.Fatalf("join page missing claim-http-invite action\nbody:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, "/join-fallback?token=") {
		t.Fatalf("join page missing fallback link\nbody:\n%s", bodyStr)
	}
}

func TestRuntimeJoinFallbackAndManualRoutes(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}
	token := createInviteAndExtractToken(t, client, rt.HTTPAddr())

	fallbackResp, err := client.Get("http://" + rt.HTTPAddr() + "/join-fallback?token=" + url.QueryEscape(token))
	if err != nil {
		t.Fatalf("fallback route failed: %v", err)
	}
	defer fallbackResp.Body.Close()

	if fallbackResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for fallback page, got %d", fallbackResp.StatusCode)
	}
	fallbackBody, _ := io.ReadAll(fallbackResp.Body)
	if !strings.Contains(string(fallbackBody), "Deep link did not open?") {
		t.Fatalf("fallback page missing updated copy\nbody:\n%s", string(fallbackBody))
	}
	if !strings.Contains(string(fallbackBody), "Open manual claim") {
		t.Fatalf("fallback page missing manual claim action\nbody:\n%s", string(fallbackBody))
	}

	manualResp, err := client.Get("http://" + rt.HTTPAddr() + "/join-manually?token=" + url.QueryEscape(token))
	if err != nil {
		t.Fatalf("manual route failed: %v", err)
	}
	defer manualResp.Body.Close()

	if manualResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for manual page, got %d", manualResp.StatusCode)
	}
	body, _ := io.ReadAll(manualResp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "Paste your feed ID to consume the invite") {
		t.Fatalf("manual page missing updated copy\nbody:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, "SSB feed ID") {
		t.Fatalf("manual page missing feed ID label\nbody:\n%s", bodyStr)
	}
	if !strings.Contains(bodyStr, `action="/invite/consume"`) {
		t.Fatalf("manual page missing consume form action\nbody:\n%s", bodyStr)
	}
}

func TestRuntimeInviteConsumeJSONSuccess(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}
	token := createInviteAndExtractToken(t, client, rt.HTTPAddr())

	memberKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate member key: %v", err)
	}

	body := fmt.Sprintf(`{"invite":%q,"id":%q}`, token, memberKey.FeedRef().String())
	req, err := http.NewRequest(http.MethodPost, "http://"+rt.HTTPAddr()+"/invite/consume", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build consume request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("consume request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d\nbody: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Status             string `json:"status"`
		MultiserverAddress string `json:"multiserverAddress"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode consume response: %v", err)
	}
	if payload.Status != "successful" {
		t.Fatalf("expected successful status, got %q", payload.Status)
	}
	wantAddr := "net:" + rt.Addr() + "~shs:" + base64.StdEncoding.EncodeToString(rt.RoomFeed().PubKey())
	if payload.MultiserverAddress != wantAddr {
		t.Fatalf("expected multiserver address %q, got %q", wantAddr, payload.MultiserverAddress)
	}

	joinResp, err := client.Get("http://" + rt.HTTPAddr() + "/join?token=" + url.QueryEscape(token))
	if err != nil {
		t.Fatalf("join request failed: %v", err)
	}
	defer joinResp.Body.Close()
	if joinResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected token to be consumed, got status %d", joinResp.StatusCode)
	}
}

func TestInviteMultiserverAddressUsesDomainHostForms(t *testing.T) {
	kp, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate room key pair: %v", err)
	}

	wantSuffix := "~shs:" + base64.StdEncoding.EncodeToString(kp.FeedRef().PubKey())
	cases := []struct {
		name       string
		domain     string
		listenAddr string
		wantHost   string
		wantPort   string
	}{
		{
			name:       "raw host",
			domain:     "bridge",
			listenAddr: "[::]:8989",
			wantHost:   "bridge",
			wantPort:   "8989",
		},
		{
			name:       "https url",
			domain:     "https://bridge.example",
			listenAddr: "[::]:8989",
			wantHost:   "bridge.example",
			wantPort:   "8989",
		},
		{
			name:       "https url with port",
			domain:     "https://bridge.example:443",
			listenAddr: "[::]:8989",
			wantHost:   "bridge.example",
			wantPort:   "8989",
		},
		{
			name:       "empty domain keeps concrete listener host",
			domain:     "",
			listenAddr: "127.0.0.1:8989",
			wantHost:   "127.0.0.1",
			wantPort:   "8989",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &inviteHandler{
				keyPair:    kp,
				domain:     tc.domain,
				muxrpcAddr: tc.listenAddr,
			}
			wantAddr := "net:" + net.JoinHostPort(tc.wantHost, tc.wantPort) + wantSuffix
			got := h.multiserverAddress()
			if got != wantAddr {
				t.Fatalf("expected %q, got %q", wantAddr, got)
			}
		})
	}
}

func TestRuntimeInviteConsumeJSONUsesHTTPSDomainHost(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := Start(ctx, Config{
		ListenAddr:     "0.0.0.0:0",
		HTTPListenAddr: "127.0.0.1:0",
		RepoPath:       t.TempDir(),
		Mode:           "open",
		HTTPSDomain:    "https://bridge.example:443",
	}, log.New(io.Discard, "", 0))
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("start runtime: %v", err)
	}
	defer rt.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	token := createInviteAndExtractToken(t, client, rt.HTTPAddr())

	memberKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate member key: %v", err)
	}

	body := fmt.Sprintf(`{"invite":%q,"id":%q}`, token, memberKey.FeedRef().String())
	req, err := http.NewRequest(http.MethodPost, "http://"+rt.HTTPAddr()+"/invite/consume", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build consume request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("consume request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		consumeBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d\nbody: %s", resp.StatusCode, string(consumeBody))
	}

	var payload struct {
		Status             string `json:"status"`
		MultiserverAddress string `json:"multiserverAddress"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode consume response: %v", err)
	}
	if payload.Status != "successful" {
		t.Fatalf("expected successful status, got %q", payload.Status)
	}

	_, port, err := net.SplitHostPort(rt.Addr())
	if err != nil {
		t.Fatalf("split room addr %q: %v", rt.Addr(), err)
	}
	wantAddr := "net:" + net.JoinHostPort("bridge.example", port) + "~shs:" + base64.StdEncoding.EncodeToString(rt.RoomFeed().PubKey())
	if payload.MultiserverAddress != wantAddr {
		t.Fatalf("expected multiserver address %q, got %q", wantAddr, payload.MultiserverAddress)
	}
}

func TestRuntimeInviteConsumeDeniedKeyDoesNotConsumeInvite(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}
	token := createInviteAndExtractToken(t, client, rt.HTTPAddr())

	memberKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate member key: %v", err)
	}
	if err := rt.roomDB.DeniedKeys().Add(context.Background(), memberKey.FeedRef(), "test denied"); err != nil {
		t.Fatalf("add denied key: %v", err)
	}

	body := fmt.Sprintf(`{"invite":%q,"id":%q}`, token, memberKey.FeedRef().String())
	req, err := http.NewRequest(http.MethodPost, "http://"+rt.HTTPAddr()+"/invite/consume", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build consume request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("consume request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d\nbody: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode denied response: %v", err)
	}
	if payload.Status != "error" || !strings.Contains(strings.ToLower(payload.Error), "denied") {
		t.Fatalf("unexpected denied response: %+v", payload)
	}

	joinResp, err := client.Get("http://" + rt.HTTPAddr() + "/join?token=" + url.QueryEscape(token))
	if err != nil {
		t.Fatalf("join request failed: %v", err)
	}
	defer joinResp.Body.Close()
	if joinResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(joinResp.Body)
		t.Fatalf("expected invite to remain active, got %d\nbody: %s", joinResp.StatusCode, string(body))
	}
}

func TestRuntimeInviteConsumeAcceptsTokenAliasInForm(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}
	token := createInviteAndExtractToken(t, client, rt.HTTPAddr())

	memberKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate member key: %v", err)
	}

	resp, err := client.PostForm("http://"+rt.HTTPAddr()+"/invite/consume", url.Values{
		"token": {token},
		"id":    {memberKey.FeedRef().String()},
	})
	if err != nil {
		t.Fatalf("form consume request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d\nbody: %s", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Invite consumed") {
		t.Fatalf("expected consumed html page\nbody:\n%s", string(body))
	}
}

func TestRuntimeJoinPostDelegatesToConsume(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}
	token := createInviteAndExtractToken(t, client, rt.HTTPAddr())

	memberKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate member key: %v", err)
	}

	resp, err := client.PostForm("http://"+rt.HTTPAddr()+"/join?token="+url.QueryEscape(token), url.Values{
		"id": {memberKey.FeedRef().String()},
	})
	if err != nil {
		t.Fatalf("join post request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d\nbody: %s", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Invite consumed") {
		t.Fatalf("expected consumed html page\nbody:\n%s", string(body))
	}
}

func TestRuntimeInviteManagementAccessMatrix(t *testing.T) {
	tests := []struct {
		name       string
		mode       string
		role       roomdb.Role
		login      bool
		expectCode int
	}{
		{name: "open anonymous allowed", mode: "open", login: false, expectCode: http.StatusOK},
		{name: "community anonymous redirect", mode: "community", login: false, expectCode: http.StatusSeeOther},
		{name: "community member allowed", mode: "community", role: roomdb.RoleMember, login: true, expectCode: http.StatusOK},
		{name: "restricted member redirect", mode: "restricted", role: roomdb.RoleMember, login: true, expectCode: http.StatusSeeOther},
		{name: "restricted moderator allowed", mode: "restricted", role: roomdb.RoleModerator, login: true, expectCode: http.StatusOK},
		{name: "restricted admin allowed", mode: "restricted", role: roomdb.RoleAdmin, login: true, expectCode: http.StatusOK},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt := startTestRuntime(t, tc.mode, nil)

			client := newNoRedirectClient(2 * time.Second)
			if tc.login {
				username := seedFallbackMember(t, rt, tc.role, byte(0x60+i), "pw-management")
				client = loginRuntimeMember(t, rt, username, "pw-management", "/invites")
			}

			resp, err := client.Get("http://" + rt.HTTPAddr() + "/invites")
			if err != nil {
				t.Fatalf("management page request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.expectCode {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected %d, got %d\nbody: %s", tc.expectCode, resp.StatusCode, string(body))
			}
		})
	}
}

func TestRuntimeInviteManagementJSONUnauthorized(t *testing.T) {
	rt := startTestRuntime(t, "community", nil)
	client := &http.Client{Timeout: 2 * time.Second}

	req, err := http.NewRequest(http.MethodGet, "http://"+rt.HTTPAddr()+"/invites?encoding=json", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("invite management request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d\nbody: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Status != "error" {
		t.Fatalf("expected error status, got %q", payload.Status)
	}
}

func TestRuntimeInviteManagementRenderingAndJSON(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	username := seedFallbackMember(t, rt, roomdb.RoleMember, 0x69, "pw-manage")
	client := loginRuntimeMember(t, rt, username, "pw-manage", "/invites")
	token := createInviteAndExtractToken(t, client, rt.HTTPAddr())
	_ = createInviteAndExtractToken(t, client, rt.HTTPAddr()) // keep one active invite

	memberKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate member key: %v", err)
	}
	body := fmt.Sprintf(`{"invite":%q,"id":%q}`, token, memberKey.FeedRef().String())
	consumeReq, _ := http.NewRequest(http.MethodPost, "http://"+rt.HTTPAddr()+"/invite/consume", strings.NewReader(body))
	consumeReq.Header.Set("Content-Type", "application/json")
	consumeResp, err := client.Do(consumeReq)
	if err != nil {
		t.Fatalf("consume request failed: %v", err)
	}
	consumeResp.Body.Close()
	if consumeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected consume 200, got %d", consumeResp.StatusCode)
	}

	htmlResp, err := client.Get("http://" + rt.HTTPAddr() + "/invites")
	if err != nil {
		t.Fatalf("management HTML request failed: %v", err)
	}
	defer htmlResp.Body.Close()

	if htmlResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(htmlResp.Body)
		t.Fatalf("expected 200, got %d\nbody: %s", htmlResp.StatusCode, string(body))
	}
	htmlBody, _ := io.ReadAll(htmlResp.Body)
	bodyStr := string(htmlBody)
	for _, want := range []string{
		"Manage room invites",
		"Create invite",
		"Active invites",
		"Consumed and revoked",
		"/invites/revoke",
	} {
		if !strings.Contains(bodyStr, want) {
			t.Fatalf("management page missing %q\nbody:\n%s", want, bodyStr)
		}
	}
	if strings.Contains(strings.ToLower(bodyStr), "copy old invite url") {
		t.Fatalf("management page should not imply old invite URLs are recoverable\nbody:\n%s", bodyStr)
	}

	jsonResp, err := client.Get("http://" + rt.HTTPAddr() + "/invites?encoding=json")
	if err != nil {
		t.Fatalf("management JSON request failed: %v", err)
	}
	defer jsonResp.Body.Close()
	if jsonResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(jsonResp.Body)
		t.Fatalf("expected 200, got %d\nbody: %s", jsonResp.StatusCode, string(body))
	}

	var payload struct {
		Status   string                `json:"status"`
		Active   []inviteManagementRow `json:"active"`
		Inactive []inviteManagementRow `json:"inactive"`
	}
	if err := json.NewDecoder(jsonResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Status != "successful" {
		t.Fatalf("expected successful status, got %q", payload.Status)
	}
	if len(payload.Inactive) == 0 {
		t.Fatalf("expected at least one inactive invite after consume")
	}
}

func TestRuntimeInviteRevokePolicyAndBehavior(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	anonClient := newNoRedirectClient(2 * time.Second)

	token := createInviteAndExtractToken(t, &http.Client{Timeout: 2 * time.Second}, rt.HTTPAddr())
	_ = token
	inviteID := latestInviteID(t, rt)

	anonResp, err := anonClient.PostForm("http://"+rt.HTTPAddr()+"/invites/revoke", url.Values{
		"id": {strconv.FormatInt(inviteID, 10)},
	})
	if err != nil {
		t.Fatalf("anonymous revoke request failed: %v", err)
	}
	defer anonResp.Body.Close()
	if anonResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected anonymous revoke redirect, got %d", anonResp.StatusCode)
	}
	if !strings.Contains(anonResp.Header.Get("Location"), "/login?next=%2Finvites") {
		t.Fatalf("expected login redirect, got %q", anonResp.Header.Get("Location"))
	}

	authUsername := seedFallbackMember(t, rt, roomdb.RoleMember, 0x70, "pw-revoke")
	authClient := loginRuntimeMember(t, rt, authUsername, "pw-revoke", "/invites")
	authRevokeResp, err := authClient.PostForm("http://"+rt.HTTPAddr()+"/invites/revoke", url.Values{
		"id": {strconv.FormatInt(inviteID, 10)},
	})
	if err != nil {
		t.Fatalf("authenticated revoke request failed: %v", err)
	}
	defer authRevokeResp.Body.Close()
	if authRevokeResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on successful revoke, got %d", authRevokeResp.StatusCode)
	}
	if !strings.Contains(authRevokeResp.Header.Get("Location"), "/invites?message=") {
		t.Fatalf("expected success redirect to invites, got %q", authRevokeResp.Header.Get("Location"))
	}

	invite, err := rt.roomDB.Invites().GetByID(context.Background(), inviteID)
	if err != nil {
		t.Fatalf("load invite: %v", err)
	}
	if invite.Active {
		t.Fatalf("expected invite to be inactive after revoke")
	}
}

func TestRuntimeInviteRevokeJSONInvalidIDAndUnauthorized(t *testing.T) {
	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}

	unauthReqBody := `{"id":1}`
	unauthReq, _ := http.NewRequest(http.MethodPost, "http://"+rt.HTTPAddr()+"/invites/revoke", strings.NewReader(unauthReqBody))
	unauthReq.Header.Set("Content-Type", "application/json")
	unauthReq.Header.Set("Accept", "application/json")
	unauthResp, err := client.Do(unauthReq)
	if err != nil {
		t.Fatalf("unauthorized JSON revoke request failed: %v", err)
	}
	defer unauthResp.Body.Close()
	if unauthResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(unauthResp.Body)
		t.Fatalf("expected 403, got %d\nbody: %s", unauthResp.StatusCode, string(body))
	}

	username := seedFallbackMember(t, rt, roomdb.RoleMember, 0x71, "pw-revoke-json")
	authClient := loginRuntimeMember(t, rt, username, "pw-revoke-json", "/invites")

	invalidReq, _ := http.NewRequest(http.MethodPost, "http://"+rt.HTTPAddr()+"/invites/revoke", strings.NewReader(`{"id":0}`))
	invalidReq.Header.Set("Content-Type", "application/json")
	invalidReq.Header.Set("Accept", "application/json")
	invalidResp, err := authClient.Do(invalidReq)
	if err != nil {
		t.Fatalf("invalid JSON revoke request failed: %v", err)
	}
	defer invalidResp.Body.Close()
	if invalidResp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(invalidResp.Body)
		t.Fatalf("expected 400, got %d\nbody: %s", invalidResp.StatusCode, string(body))
	}

	var payload struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(invalidResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode invalid response: %v", err)
	}
	if payload.Status != "error" {
		t.Fatalf("expected error status, got %q", payload.Status)
	}
}

func TestRuntimeInvitesNavVisibilityByModeAndAuth(t *testing.T) {
	tests := []struct {
		name          string
		mode          string
		role          roomdb.Role
		login         bool
		expectInvites bool
	}{
		{name: "open anonymous shows nav", mode: "open", login: false, expectInvites: true},
		{name: "community anonymous hides nav", mode: "community", login: false, expectInvites: false},
		{name: "community member shows nav", mode: "community", role: roomdb.RoleMember, login: true, expectInvites: true},
		{name: "restricted member hides nav", mode: "restricted", role: roomdb.RoleMember, login: true, expectInvites: false},
		{name: "restricted moderator shows nav", mode: "restricted", role: roomdb.RoleModerator, login: true, expectInvites: true},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt := startTestRuntime(t, tc.mode, nil)

			var client *http.Client
			if tc.login {
				username := seedFallbackMember(t, rt, tc.role, byte(0x80+i), "pw-nav")
				client = loginRuntimeMember(t, rt, username, "pw-nav", "/")
			} else {
				client = &http.Client{Timeout: 2 * time.Second}
			}

			body, status := getRuntimePath(t, client, rt, "/")
			if status != http.StatusOK {
				t.Fatalf("expected 200, got %d", status)
			}

			hasInvitesNav := strings.Contains(body, `href="/invites"`)
			if hasInvitesNav != tc.expectInvites {
				t.Fatalf("expected invites nav=%v, got %v\nbody:\n%s", tc.expectInvites, hasInvitesNav, body)
			}
		})
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

func newNoRedirectClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func loginRuntimeMember(t *testing.T, rt *Runtime, username, password, next string) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}

	client := &http.Client{
		Timeout: 2 * time.Second,
		Jar:     jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{
		"username": {username},
		"password": {password},
	}
	if next != "" {
		form.Set("next", next)
	}

	resp, err := client.PostForm("http://"+rt.HTTPAddr()+"/login", form)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 303 from login, got %d\nbody: %s", resp.StatusCode, string(body))
	}

	return client
}

func seedFallbackMember(t *testing.T, rt *Runtime, role roomdb.Role, feedFill byte, password string) string {
	t.Helper()

	feed := mustRuntimeTestFeedRef(t, feedFill)
	memberID, err := rt.roomDB.Members().Add(context.Background(), *feed, role)
	if err != nil {
		t.Fatalf("add member: %v", err)
	}

	if err := rt.roomDB.AuthFallback().SetPassword(context.Background(), memberID, password); err != nil {
		t.Fatalf("set fallback password: %v", err)
	}

	return fmt.Sprintf("member-%d", memberID)
}

func createInviteAndExtractToken(t *testing.T, client *http.Client, addr string) string {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/create-invite", nil)
	if err != nil {
		t.Fatalf("build create invite request: %v", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("create invite failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 creating invite, got %d\nbody: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}

	parsed, err := url.Parse(payload.URL)
	if err != nil {
		t.Fatalf("parse invite url %q: %v", payload.URL, err)
	}
	token := parsed.Query().Get("token")
	if token == "" {
		t.Fatalf("invite URL missing token: %q", payload.URL)
	}
	return token
}

func latestInviteID(t *testing.T, rt *Runtime) int64 {
	t.Helper()

	invites, err := rt.roomDB.Invites().List(context.Background())
	if err != nil {
		t.Fatalf("list invites: %v", err)
	}
	if len(invites) == 0 {
		t.Fatal("expected at least one invite")
	}
	return invites[0].ID
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

type runtimeRoomClient struct {
	key      *keys.KeyPair
	conn     net.Conn
	endpoint *muxrpc.Server
}

func (c *runtimeRoomClient) Close() error {
	if c == nil {
		return nil
	}
	if c.conn != nil {
		err := c.conn.Close()
		if err != nil && strings.Contains(err.Error(), "use of closed network connection") {
			return nil
		}
		if err != nil {
			return err
		}
	}
	if c.endpoint != nil {
		_ = c.endpoint.Terminate()
	}
	return nil
}

func connectRuntimeRoomClient(t *testing.T, ctx context.Context, rt *Runtime, key *keys.KeyPair, handler muxrpc.Handler) *runtimeRoomClient {
	t.Helper()

	conn, err := net.Dial("tcp", rt.Addr())
	if err != nil {
		t.Fatalf("dial muxrpc: %v", err)
	}

	appKey := secretstream.NewAppKey("boxstream")
	shs, err := secretstream.NewClient(conn, appKey, key.Private(), rt.RoomFeed().PubKey())
	if err != nil {
		_ = conn.Close()
		t.Fatalf("new SHS client: %v", err)
	}
	if err := shs.Handshake(); err != nil {
		_ = conn.Close()
		t.Fatalf("SHS handshake: %v", err)
	}

	client := &runtimeRoomClient{
		key:      key,
		conn:     conn,
		endpoint: muxrpc.NewServer(ctx, shs, handler, nil),
	}
	t.Cleanup(func() {
		_ = client.Close()
	})
	return client
}

func readSourceJSON(t *testing.T, ctx context.Context, src *muxrpc.ByteSource, dst interface{}) {
	t.Helper()

	if !src.Next(ctx) {
		if err := src.Err(); err != nil {
			t.Fatalf("source next failed: %v", err)
		}
		t.Fatal("source closed before next frame")
	}

	body, err := src.Bytes()
	if err != nil {
		t.Fatalf("source bytes: %v", err)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		t.Fatalf("decode source frame %q: %v", string(body), err)
	}
}

type tunnelConnectCapture struct {
	Origin refs.FeedRef `json:"origin"`
	Portal refs.FeedRef `json:"portal"`
	Target refs.FeedRef `json:"target"`
}

type captureTunnelHandler struct {
	mu      sync.Mutex
	args    tunnelConnectCapture
	argsCh  chan tunnelConnectCapture
	bytesCh chan []byte
}

func newCaptureTunnelHandler() *captureTunnelHandler {
	return &captureTunnelHandler{
		argsCh:  make(chan tunnelConnectCapture, 1),
		bytesCh: make(chan []byte, 1),
	}
}

func (h *captureTunnelHandler) Handled(m muxrpc.Method) bool {
	return len(m) == 2 && m[0] == "tunnel" && m[1] == "connect"
}

func (h *captureTunnelHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	if req.Type != "duplex" {
		req.CloseWithError(fmt.Errorf("expected duplex call"))
		return
	}

	var rawArgs []json.RawMessage
	if err := json.Unmarshal(req.RawArgs, &rawArgs); err != nil || len(rawArgs) != 1 {
		req.CloseWithError(fmt.Errorf("expected single tunnel.connect arg"))
		return
	}

	var args tunnelConnectCapture
	if err := json.Unmarshal(rawArgs[0], &args); err != nil {
		req.CloseWithError(fmt.Errorf("decode tunnel.connect args: %w", err))
		return
	}

	h.mu.Lock()
	h.args = args
	h.mu.Unlock()
	select {
	case h.argsCh <- args:
	default:
	}

	source := req.Source()
	sink, err := req.ResponseSink()
	if err != nil {
		req.CloseWithError(err)
		return
	}

	go func() {
		defer sink.Close()
		defer req.Close()
		if source == nil || !source.Next(ctx) {
			return
		}
		body, err := source.Bytes()
		if err != nil {
			return
		}
		cloned := append([]byte(nil), body...)
		select {
		case h.bytesCh <- cloned:
		default:
		}
		_, _ = sink.Write(cloned)
	}()
}

func (h *captureTunnelHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func waitForRuntimeStatusSummary(t *testing.T, client *http.Client, rt *Runtime, check func(roomStatusSummary) bool) roomStatusSummary {
	t.Helper()

	deadline := time.Now().Add(8 * time.Second)
	var last roomStatusSummary
	for time.Now().Before(deadline) {
		body, status := getRuntimePath(t, client, rt, "/status")
		if status == http.StatusOK {
			if err := json.Unmarshal([]byte(body), &last); err == nil && check(last) {
				return last
			}
		}
		time.Sleep(25 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for runtime status: %+v", last)
	return last
}

func TestRuntimeRoomMetadataReportsAuthenticatedMembership(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rt := startTestRuntime(t, "open", nil)

	memberKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate member key: %v", err)
	}
	if _, err := rt.roomDB.Members().Add(ctx, memberKey.FeedRef(), roomdb.RoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}
	memberClient := connectRuntimeRoomClient(t, ctx, rt, memberKey, nil)

	externalKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate external key: %v", err)
	}
	externalClient := connectRuntimeRoomClient(t, ctx, rt, externalKey, nil)

	var memberMeta struct {
		Name       string   `json:"name"`
		Membership bool     `json:"membership"`
		Features   []string `json:"features"`
	}
	if err := memberClient.endpoint.Async(ctx, &memberMeta, muxrpc.TypeJSON, muxrpc.Method{"room", "metadata"}); err != nil {
		t.Fatalf("member metadata: %v", err)
	}
	if !memberMeta.Membership {
		t.Fatalf("expected member metadata membership=true for internal member: %+v", memberMeta)
	}
	for _, want := range []string{"tunnel", "room2", "httpInvite", "httpAuth", "alias"} {
		if !containsString(memberMeta.Features, want) {
			t.Fatalf("member metadata missing feature %q: %+v", want, memberMeta.Features)
		}
	}

	var externalMeta struct {
		Name       string   `json:"name"`
		Membership bool     `json:"membership"`
		Features   []string `json:"features"`
	}
	if err := externalClient.endpoint.Async(ctx, &externalMeta, muxrpc.TypeJSON, muxrpc.Method{"room", "metadata"}); err != nil {
		t.Fatalf("external metadata: %v", err)
	}
	if externalMeta.Membership {
		t.Fatalf("expected external metadata membership=false for non-member: %+v", externalMeta)
	}
}

func TestRuntimeRoomAttendantsEmitStateJoinAndLeft(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rt := startTestRuntime(t, "open", nil)

	memberOne, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate member one key: %v", err)
	}
	memberTwo, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate member two key: %v", err)
	}
	for _, kp := range []*keys.KeyPair{memberOne, memberTwo} {
		if _, err := rt.roomDB.Members().Add(ctx, kp.FeedRef(), roomdb.RoleMember); err != nil {
			t.Fatalf("add member %s: %v", kp.FeedRef(), err)
		}
	}

	clientOne := connectRuntimeRoomClient(t, ctx, rt, memberOne, nil)
	var meta struct {
		Membership bool `json:"membership"`
	}
	if err := clientOne.endpoint.Async(ctx, &meta, muxrpc.TypeJSON, muxrpc.Method{"room", "metadata"}); err != nil {
		t.Fatalf("prime member one connection: %v", err)
	}

	src, err := clientOne.endpoint.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"room", "attendants"})
	if err != nil {
		t.Fatalf("room attendants source: %v", err)
	}

	var state struct {
		Type string   `json:"type"`
		IDs  []string `json:"ids"`
	}
	readSourceJSON(t, ctx, src, &state)
	if state.Type != "state" {
		t.Fatalf("expected state event, got %+v", state)
	}
	if len(state.IDs) != 1 || state.IDs[0] != memberOne.FeedRef().String() {
		t.Fatalf("unexpected attendants state: %+v", state)
	}

	clientTwo := connectRuntimeRoomClient(t, ctx, rt, memberTwo, nil)

	var joined struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	readSourceJSON(t, ctx, src, &joined)
	if joined.Type != "joined" || joined.ID != memberTwo.FeedRef().String() {
		t.Fatalf("unexpected joined event: %+v", joined)
	}

	if err := clientTwo.Close(); err != nil {
		t.Fatalf("close member two: %v", err)
	}
}

func TestRuntimeMarksSnapshotsInactiveOnStartup(t *testing.T) {
	repoPath := t.TempDir()
	db, err := sqlite.Open(filepath.Join(repoPath, "room.sqlite"))
	if err != nil {
		t.Fatalf("open room db: %v", err)
	}

	attendant := mustRuntimeTestFeedRef(t, 0x51)
	tunnel := mustRuntimeTestFeedRef(t, 0x52)
	ctx := context.Background()
	if err := db.RuntimeSnapshots().UpsertAttendant(ctx, *attendant, "attendant:1", 11); err != nil {
		t.Fatalf("seed attendant snapshot: %v", err)
	}
	if err := db.RuntimeSnapshots().UpsertTunnelEndpoint(ctx, *tunnel, "tunnel:1", 22); err != nil {
		t.Fatalf("seed tunnel snapshot: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seeded room db: %v", err)
	}

	startCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := Start(startCtx, Config{
		ListenAddr:     "127.0.0.1:0",
		HTTPListenAddr: "127.0.0.1:0",
		RepoPath:       repoPath,
		Mode:           "community",
	}, log.New(io.Discard, "", 0))
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("start runtime: %v", err)
	}
	defer rt.Close()

	attendants, err := rt.roomDB.RuntimeSnapshots().ListAttendants(ctx, false)
	if err != nil {
		t.Fatalf("list attendants: %v", err)
	}
	if len(attendants) != 1 {
		t.Fatalf("expected one seeded attendant, got %d", len(attendants))
	}
	if attendants[0].Active {
		t.Fatalf("expected seeded attendant to be inactive after startup: %+v", attendants[0])
	}

	activeAttendants, err := rt.roomDB.RuntimeSnapshots().ListAttendants(ctx, true)
	if err != nil {
		t.Fatalf("list active attendants: %v", err)
	}
	if len(activeAttendants) != 0 {
		t.Fatalf("expected no active attendants after startup, got %d", len(activeAttendants))
	}

	tunnels, err := rt.roomDB.RuntimeSnapshots().ListTunnelEndpoints(ctx, false)
	if err != nil {
		t.Fatalf("list tunnels: %v", err)
	}
	if len(tunnels) != 1 {
		t.Fatalf("expected one seeded tunnel, got %d", len(tunnels))
	}
	if tunnels[0].Active {
		t.Fatalf("expected seeded tunnel to be inactive after startup: %+v", tunnels[0])
	}

	activeTunnels, err := rt.roomDB.RuntimeSnapshots().ListTunnelEndpoints(ctx, true)
	if err != nil {
		t.Fatalf("list active tunnels: %v", err)
	}
	if len(activeTunnels) != 0 {
		t.Fatalf("expected no active tunnels after startup, got %d", len(activeTunnels))
	}
}

func TestRuntimeStatusEndpointsTrackAttendantAndTunnelSnapshots(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rt := startTestRuntime(t, "open", nil)
	client := &http.Client{Timeout: 2 * time.Second}

	memberKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate member key: %v", err)
	}
	if _, err := rt.roomDB.Members().Add(ctx, memberKey.FeedRef(), roomdb.RoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}

	roomClient := connectRuntimeRoomClient(t, ctx, rt, memberKey, nil)

	var announced bool
	if err := roomClient.endpoint.Sync(ctx, &announced, muxrpc.TypeJSON, muxrpc.Method{"tunnel", "announce"}); err != nil {
		t.Fatalf("announce tunnel: %v", err)
	}
	if !announced {
		t.Fatalf("expected tunnel.announce to return true")
	}

	summary := waitForRuntimeStatusSummary(t, client, rt, func(s roomStatusSummary) bool {
		return s.ActiveAttendants == 1 && s.TotalAttendants == 1 && s.ActiveTunnels == 1 && s.TotalTunnels == 1 && s.LiveAttendants == 1 && s.LivePeers == 1
	})
	if summary.RoomID != rt.RoomFeed().String() {
		t.Fatalf("unexpected room id in summary: %+v", summary)
	}
	if summary.PrivacyMode != "open" {
		t.Fatalf("unexpected privacy mode in summary: %+v", summary)
	}
	if summary.HTTPAddr != rt.HTTPAddr() || summary.MuxrpcAddr != rt.Addr() {
		t.Fatalf("unexpected runtime addresses in summary: %+v", summary)
	}

	tunnels, status := getRuntimePath(t, client, rt, "/status/tunnels")
	if status != http.StatusOK {
		t.Fatalf("tunnels status route expected 200, got %d", status)
	}
	var tunnelPayload struct {
		ActiveOnly bool               `json:"activeOnly"`
		Tunnels    []roomStatusTunnel `json:"tunnels"`
	}
	if err := json.NewDecoder(strings.NewReader(tunnels)).Decode(&tunnelPayload); err != nil {
		t.Fatalf("decode tunnels status: %v", err)
	}
	if !tunnelPayload.ActiveOnly {
		t.Fatalf("expected active-only tunnel list by default: %+v", tunnelPayload)
	}
	if len(tunnelPayload.Tunnels) != 1 {
		t.Fatalf("expected one active tunnel, got %+v", tunnelPayload)
	}
	if tunnelPayload.Tunnels[0].Target != memberKey.FeedRef().String() || !tunnelPayload.Tunnels[0].Active {
		t.Fatalf("unexpected tunnel status payload: %+v", tunnelPayload.Tunnels[0])
	}
	wantTunnelAddr := fmt.Sprintf("tunnel:%s:%s~shs:%s", rt.RoomFeed().String(), memberKey.FeedRef().String(), base64.StdEncoding.EncodeToString(memberKey.FeedRef().PubKey()))
	if tunnelPayload.Tunnels[0].Addr != wantTunnelAddr {
		t.Fatalf("unexpected tunnel address: got %q want %q", tunnelPayload.Tunnels[0].Addr, wantTunnelAddr)
	}

	attendants, status := getRuntimePath(t, client, rt, "/status/attendants")
	if status != http.StatusOK {
		t.Fatalf("attendants status route expected 200, got %d", status)
	}
	var attendantPayload struct {
		ActiveOnly bool                  `json:"activeOnly"`
		Attendants []roomStatusAttendant `json:"attendants"`
	}
	if err := json.NewDecoder(strings.NewReader(attendants)).Decode(&attendantPayload); err != nil {
		t.Fatalf("decode attendants status: %v", err)
	}
	if !attendantPayload.ActiveOnly {
		t.Fatalf("expected active-only attendants list by default: %+v", attendantPayload)
	}
	if len(attendantPayload.Attendants) != 1 {
		t.Fatalf("expected one active attendant, got %+v", attendantPayload)
	}
	if attendantPayload.Attendants[0].ID != memberKey.FeedRef().String() || !attendantPayload.Attendants[0].Active {
		t.Fatalf("unexpected attendant status payload: %+v", attendantPayload.Attendants[0])
	}

	if err := roomClient.Close(); err != nil {
		t.Fatalf("close runtime room client: %v", err)
	}

	waitForRuntimeStatusSummary(t, client, rt, func(s roomStatusSummary) bool {
		return s.ActiveAttendants == 0 && s.ActiveTunnels == 0 && s.TotalAttendants == 1 && s.TotalTunnels == 1 && s.LiveAttendants == 0 && s.LivePeers == 0
	})

	allTunnels, status := getRuntimePath(t, client, rt, "/status/tunnels?all=true")
	if status != http.StatusOK {
		t.Fatalf("all tunnels status route expected 200, got %d", status)
	}
	var allTunnelPayload struct {
		ActiveOnly bool               `json:"activeOnly"`
		Tunnels    []roomStatusTunnel `json:"tunnels"`
	}
	if err := json.NewDecoder(strings.NewReader(allTunnels)).Decode(&allTunnelPayload); err != nil {
		t.Fatalf("decode all tunnels status: %v", err)
	}
	if allTunnelPayload.ActiveOnly {
		t.Fatalf("expected inactive-inclusive tunnel list when all=true: %+v", allTunnelPayload)
	}
	if len(allTunnelPayload.Tunnels) != 1 || allTunnelPayload.Tunnels[0].Active {
		t.Fatalf("expected one inactive tunnel snapshot after disconnect: %+v", allTunnelPayload.Tunnels)
	}

	allAttendants, status := getRuntimePath(t, client, rt, "/status/attendants?all=true")
	if status != http.StatusOK {
		t.Fatalf("all attendants status route expected 200, got %d", status)
	}
	var allAttendantPayload struct {
		ActiveOnly bool                  `json:"activeOnly"`
		Attendants []roomStatusAttendant `json:"attendants"`
	}
	if err := json.NewDecoder(strings.NewReader(allAttendants)).Decode(&allAttendantPayload); err != nil {
		t.Fatalf("decode all attendants status: %v", err)
	}
	if allAttendantPayload.ActiveOnly {
		t.Fatalf("expected inactive-inclusive attendants list when all=true: %+v", allAttendantPayload)
	}
	if len(allAttendantPayload.Attendants) != 1 || allAttendantPayload.Attendants[0].Active {
		t.Fatalf("expected one inactive attendant snapshot after disconnect: %+v", allAttendantPayload.Attendants)
	}
}

func TestRuntimeMemberConnectInvokesAnnounceHook(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rt := startTestRuntime(t, "open", nil)

	memberKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate member key: %v", err)
	}
	if _, err := rt.roomDB.Members().Add(ctx, memberKey.FeedRef(), roomdb.RoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}

	announced := make(chan refs.FeedRef, 1)
	rt.SetAnnounceHook(func(feed refs.FeedRef) error {
		select {
		case announced <- feed:
		default:
		}
		return nil
	})

	_ = connectRuntimeRoomClient(t, ctx, rt, memberKey, nil)

	select {
	case feed := <-announced:
		if !feed.Equal(memberKey.FeedRef()) {
			t.Fatalf("expected announce hook feed %s, got %s", memberKey.FeedRef(), feed)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for announce hook on member connect: %v", ctx.Err())
	}
	if !rt.state.HasPeer(memberKey.FeedRef()) {
		t.Fatalf("expected member connect to register tunnel peer for %s", memberKey.FeedRef())
	}
}

func TestRuntimeAliasRegisterEndpointAndRevoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rt := startTestRuntime(t, "open", nil)

	memberKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate member key: %v", err)
	}
	if _, err := rt.roomDB.Members().Add(ctx, memberKey.FeedRef(), roomdb.RoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}

	client := connectRuntimeRoomClient(t, ctx, rt, memberKey, nil)
	alias := "oak"
	registrationMsg := []byte("=room-alias-registration:" + rt.RoomFeed().String() + ":" + memberKey.FeedRef().String() + ":" + alias)
	signature := ed25519.Sign(memberKey.Private(), registrationMsg)

	var aliasURL string
	if err := client.endpoint.Async(ctx, &aliasURL, muxrpc.TypeString, muxrpc.Method{"room", "registerAlias"}, alias, signature); err != nil {
		t.Fatalf("register alias: %v", err)
	}
	wantAliasURL := "http://" + rt.HTTPAddr() + "/oak"
	if aliasURL != wantAliasURL {
		t.Fatalf("expected alias URL %q, got %q", wantAliasURL, aliasURL)
	}

	aliasMux := newServeMux(ctx, rt.roomDB, rt.state, rt.keyPair, rt.cfg.HTTPSDomain, rt.HTTPAddr(), rt.Addr(), rt.roomSrv)
	jsonReq := httptest.NewRequest(http.MethodGet, "/oak?encoding=json", nil)
	jsonRec := httptest.NewRecorder()
	aliasMux.ServeHTTP(jsonRec, jsonReq)
	if jsonRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from alias json, got %d\nbody: %s", jsonRec.Code, jsonRec.Body.String())
	}

	var payload struct {
		Status             string `json:"status"`
		MultiserverAddress string `json:"multiserverAddress"`
		RoomID             string `json:"roomId"`
		UserID             string `json:"userId"`
		Alias              string `json:"alias"`
		Signature          string `json:"signature"`
	}
	if err := json.NewDecoder(jsonRec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode alias json: %v", err)
	}
	if payload.Status != "successful" {
		t.Fatalf("expected successful alias status, got %+v", payload)
	}
	if payload.RoomID != rt.RoomFeed().String() || payload.UserID != memberKey.FeedRef().String() || payload.Alias != alias {
		t.Fatalf("unexpected alias payload: %+v", payload)
	}
	wantAddr := "net:" + rt.Addr() + "~shs:" + base64.StdEncoding.EncodeToString(rt.RoomFeed().PubKey())
	if payload.MultiserverAddress != wantAddr {
		t.Fatalf("expected multiserver address %q, got %q", wantAddr, payload.MultiserverAddress)
	}
	wantSig := base64.StdEncoding.EncodeToString(signature) + ".sig.ed25519"
	if payload.Signature != wantSig {
		t.Fatalf("expected signature %q, got %q", wantSig, payload.Signature)
	}

	htmlReq := httptest.NewRequest(http.MethodGet, "/oak", nil)
	htmlRec := httptest.NewRecorder()
	aliasMux.ServeHTTP(htmlRec, htmlReq)
	if htmlRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from alias html, got %d\nbody: %s", htmlRec.Code, htmlRec.Body.String())
	}
	if !strings.Contains(htmlRec.Body.String(), "consume-alias") || !strings.Contains(htmlRec.Body.String(), "oak") {
		t.Fatalf("alias html missing expected content\nbody:\n%s", htmlRec.Body.String())
	}

	var revoked bool
	if err := client.endpoint.Async(ctx, &revoked, muxrpc.TypeJSON, muxrpc.Method{"room", "revokeAlias"}, alias); err != nil {
		t.Fatalf("revoke alias: %v", err)
	}
	if !revoked {
		t.Fatalf("expected room.revokeAlias to return true")
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/oak", nil)
	missingRec := httptest.NewRecorder()
	aliasMux.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after alias revoke, got %d\nbody: %s", missingRec.Code, missingRec.Body.String())
	}
}

func TestRuntimeAliasJSONUsesHTTPSDomainHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rt, err := Start(ctx, Config{
		ListenAddr:     "0.0.0.0:0",
		HTTPListenAddr: "127.0.0.1:0",
		RepoPath:       t.TempDir(),
		Mode:           "open",
		HTTPSDomain:    "https://bridge.example:443",
	}, log.New(io.Discard, "", 0))
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("start runtime: %v", err)
	}
	defer rt.Close()

	memberKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate member key: %v", err)
	}
	if _, err := rt.roomDB.Members().Add(ctx, memberKey.FeedRef(), roomdb.RoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}

	alias := "oakhost"
	signature := ed25519.Sign(memberKey.Private(), []byte("alias-host-check"))
	if err := rt.roomDB.Aliases().Register(ctx, alias, memberKey.FeedRef(), signature); err != nil {
		t.Fatalf("register alias: %v", err)
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + rt.HTTPAddr() + "/" + alias + "?encoding=json")
	if err != nil {
		t.Fatalf("alias request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 alias response, got %d\nbody: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Status             string `json:"status"`
		MultiserverAddress string `json:"multiserverAddress"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode alias json: %v", err)
	}
	if payload.Status != "successful" {
		t.Fatalf("expected successful alias status, got %q", payload.Status)
	}

	_, port, err := net.SplitHostPort(rt.Addr())
	if err != nil {
		t.Fatalf("split room addr %q: %v", rt.Addr(), err)
	}
	wantAddr := "net:" + net.JoinHostPort("bridge.example", port) + "~shs:" + base64.StdEncoding.EncodeToString(rt.RoomFeed().PubKey())
	if payload.MultiserverAddress != wantAddr {
		t.Fatalf("expected multiserver address %q, got %q", wantAddr, payload.MultiserverAddress)
	}
}

func TestRuntimeRegisterAliasURLUsesConfiguredHTTPSDomain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rt, err := Start(ctx, Config{
		ListenAddr:     "0.0.0.0:0",
		HTTPListenAddr: "127.0.0.1:0",
		RepoPath:       t.TempDir(),
		Mode:           "open",
		HTTPSDomain:    "https://bridge.example:443",
	}, log.New(io.Discard, "", 0))
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("start runtime: %v", err)
	}
	defer rt.Close()

	memberKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate member key: %v", err)
	}
	if _, err := rt.roomDB.Members().Add(ctx, memberKey.FeedRef(), roomdb.RoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}

	client := connectRuntimeRoomClient(t, ctx, rt, memberKey, nil)
	alias := "oakhttps"
	registrationMsg := []byte("=room-alias-registration:" + rt.RoomFeed().String() + ":" + memberKey.FeedRef().String() + ":" + alias)
	signature := ed25519.Sign(memberKey.Private(), registrationMsg)

	var aliasURL string
	if err := client.endpoint.Async(ctx, &aliasURL, muxrpc.TypeString, muxrpc.Method{"room", "registerAlias"}, alias, signature); err != nil {
		t.Fatalf("register alias: %v", err)
	}
	if aliasURL != "https://bridge.example:443/oakhttps" {
		t.Fatalf("expected configured https alias URL, got %q", aliasURL)
	}
}

func TestRuntimeTunnelConnectForwardsPortalArgsAndBytes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rt := startTestRuntime(t, "open", nil)

	targetKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate target key: %v", err)
	}
	if _, err := rt.roomDB.Members().Add(ctx, targetKey.FeedRef(), roomdb.RoleMember); err != nil {
		t.Fatalf("add target member: %v", err)
	}

	targetHandler := newCaptureTunnelHandler()
	targetClient := connectRuntimeRoomClient(t, ctx, rt, targetKey, targetHandler)

	var announced bool
	if err := targetClient.endpoint.Sync(ctx, &announced, muxrpc.TypeJSON, muxrpc.Method{"tunnel", "announce"}); err != nil {
		t.Fatalf("announce target: %v", err)
	}
	if !announced {
		t.Fatalf("expected tunnel.announce to return true")
	}

	originKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate origin key: %v", err)
	}
	originClient := connectRuntimeRoomClient(t, ctx, rt, originKey, nil)

	src, sink, err := originClient.endpoint.Duplex(ctx, muxrpc.TypeBinary, muxrpc.Method{"tunnel", "connect"}, struct {
		Portal refs.FeedRef `json:"portal"`
		Target refs.FeedRef `json:"target"`
	}{
		Portal: rt.RoomFeed(),
		Target: targetKey.FeedRef(),
	})
	if err != nil {
		t.Fatalf("open tunnel.connect: %v", err)
	}

	payload := []byte("ping through room")
	if _, err := sink.Write(payload); err != nil {
		t.Fatalf("write tunnel payload: %v", err)
	}

	var forwardedArgs tunnelConnectCapture
	select {
	case forwardedArgs = <-targetHandler.argsCh:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for target tunnel args: %v", ctx.Err())
	}
	if !forwardedArgs.Origin.Equal(originKey.FeedRef()) {
		t.Fatalf("expected origin %s, got %s", originKey.FeedRef(), forwardedArgs.Origin)
	}
	if !forwardedArgs.Portal.Equal(rt.RoomFeed()) {
		t.Fatalf("expected portal %s, got %s", rt.RoomFeed(), forwardedArgs.Portal)
	}
	if !forwardedArgs.Target.Equal(targetKey.FeedRef()) {
		t.Fatalf("expected target %s, got %s", targetKey.FeedRef(), forwardedArgs.Target)
	}

	select {
	case got := <-targetHandler.bytesCh:
		if !bytes.Equal(got, payload) {
			t.Fatalf("target received %q, want %q", got, payload)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for target payload: %v", ctx.Err())
	}

	if !src.Next(ctx) {
		t.Fatalf("tunnel response missing: %v", src.Err())
	}
	echoed, err := src.Bytes()
	if err != nil {
		t.Fatalf("read tunnel response: %v", err)
	}
	if !bytes.Equal(echoed, payload) {
		t.Fatalf("expected echoed payload %q, got %q", payload, echoed)
	}

	_ = sink.Close()
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
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
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

func TestAuthHandlersSuccess(t *testing.T) {
	h := &authHandler{authFallback: &mockAuthFallbackSuccess{}}

	// Login success
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=pw"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.handleLogin(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", rr.Code)
	}

	// Reset password success
	req2 := httptest.NewRequest(http.MethodPost, "/reset-password", strings.NewReader("token=good&password=new"))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr2 := httptest.NewRecorder()
	h.handleResetPassword(rr2, req2)
	if rr2.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", rr2.Code)
	}
}

type mockAuthFallbackSuccess struct{}

func (m *mockAuthFallbackSuccess) Check(ctx context.Context, u, p string) (int64, error) {
	return 1, nil
}
func (m *mockAuthFallbackSuccess) SetPassword(ctx context.Context, id int64, p string) error {
	return nil
}
func (m *mockAuthFallbackSuccess) CreateResetToken(ctx context.Context, c, f int64) (string, error) {
	return "token", nil
}
func (m *mockAuthFallbackSuccess) SetPasswordWithToken(ctx context.Context, t, p string) error {
	return nil
}

func TestAuthHandlersMethods(t *testing.T) {
	h := &authHandler{}
	methods := []string{http.MethodPut, http.MethodDelete}
	for _, m := range methods {
		req := httptest.NewRequest(m, "/login", nil)
		rr := httptest.NewRecorder()
		h.handleLogin(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Error()
		}

		req2 := httptest.NewRequest(m, "/reset-password", nil)
		rr2 := httptest.NewRecorder()
		h.handleResetPassword(rr2, req2)
		if rr2.Code != http.StatusMethodNotAllowed {
			t.Error()
		}
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

	// 2. room mode not open
	h2 := &inviteHandler{config: &mockRoomConfig{mode: roomdb.ModeCommunity}}
	rr2 := httptest.NewRecorder()
	h2.handleCreateInvite(rr2, req)
	if rr2.Code != http.StatusSeeOther {
		t.Errorf("expected 303, got %d", rr2.Code)
	}

	// 3. create fail
	h3 := &inviteHandler{
		config: &mockRoomConfig{mode: roomdb.ModeOpen},
		roomDB: &mockInvitesService{createErr: fmt.Errorf("fail")},
	}
	reqPost := httptest.NewRequest(http.MethodPost, "/create-invite", nil)
	reqPost.Header.Set("Accept", "application/json")
	rr3 := httptest.NewRecorder()
	h3.handleCreateInvite(rr3, reqPost)
	if rr3.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr3.Code)
	}

	// 4. handleJoin MethodNotAllowed
	h4 := &inviteHandler{}
	req4 := httptest.NewRequest(http.MethodPut, "/join?token=test", nil)
	rr4 := httptest.NewRecorder()
	h4.handleJoin(rr4, req4)
	if rr4.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr4.Code)
	}
}

func TestInviteHandlersMethods(t *testing.T) {
	h := &inviteHandler{}
	req := httptest.NewRequest(http.MethodPut, "/create-invite", nil)
	rr := httptest.NewRecorder()
	h.handleCreateInvite(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

type mockInvitesService struct {
	roomdb.InvitesService
	createErr error
}

func (m *mockInvitesService) Create(ctx context.Context, id int64) (string, error) {
	return "", m.createErr
}

func TestHandleJoinPost(t *testing.T) {
	h := &inviteHandler{}
	req := httptest.NewRequest(http.MethodPost, "/join?token=test", nil)
	rr := httptest.NewRecorder()
	h.handleJoin(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

type mockRoomConfig struct {
	err  error
	mode roomdb.PrivacyMode
}

func (m *mockRoomConfig) GetPrivacyMode(ctx context.Context) (roomdb.PrivacyMode, error) {
	if m.err != nil {
		return roomdb.ModeUnknown, m.err
	}
	return m.mode, nil
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

	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
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
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("listen http test socket: %v", err)
	}
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
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("listen muxrpc test socket: %v", err)
	}
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

func TestRoomMuxRPCConnWithSHS(t *testing.T) {
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

	// Client keypair
	clientKey, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}

	// Dial the muxrpc listener
	conn, err := net.Dial("tcp", rt.Addr())
	if err != nil {
		t.Fatalf("dial muxrpc: %v", err)
	}
	defer conn.Close()

	// SHS Client
	appKey := secretstream.NewAppKey("boxstream")
	client, err := secretstream.NewClient(conn, appKey, clientKey.Private(), rt.RoomFeed().PubKey())
	if err != nil {
		t.Fatal(err)
	}

	if err := client.Handshake(); err != nil {
		t.Fatalf("client handshake failed: %v", err)
	}

	// MuxRPC Client
	rpc := muxrpc.NewServer(ctx, client, nil, nil)

	var resp struct {
		ID string `json:"id"`
	}
	err = rpc.Async(ctx, &resp, muxrpc.TypeJSON, muxrpc.Method{"whoami"})
	if err != nil {
		t.Fatalf("whoami call failed: %v", err)
	}

	if resp.ID != rt.RoomFeed().String() {
		t.Errorf("expected room id %s, got %s", rt.RoomFeed().String(), resp.ID)
	}
}

func TestConnWrapperMethods(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	cw := &connWrapper{Conn: client}

	// Write through wrapper, read from server side
	go func() {
		_, _ = cw.Write([]byte("hello"))
	}()
	buf := make([]byte, 5)
	n, err := server.Read(buf)
	if err != nil {
		t.Fatalf("read from server: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Fatalf("expected hello, got %q", string(buf[:n]))
	}

	// Write from server, read through wrapper
	go func() {
		_, _ = server.Write([]byte("world"))
	}()
	buf2 := make([]byte, 5)
	n, err = cw.Read(buf2)
	if err != nil {
		t.Fatalf("read from wrapper: %v", err)
	}
	if string(buf2[:n]) != "world" {
		t.Fatalf("expected world, got %q", string(buf2[:n]))
	}

	// RemoteAddr
	addr := cw.RemoteAddr()
	if addr == nil {
		t.Fatal("expected non-nil RemoteAddr")
	}

	// Close
	if err := cw.Close(); err != nil {
		t.Fatalf("close wrapper: %v", err)
	}
}

func TestNewRoomServer(t *testing.T) {
	feedRef := mustRuntimeTestFeedRef(t, 1)
	rs := newRoomServer(feedRef, nil, nil)
	if rs == nil {
		t.Fatal("expected non-nil roomServer")
	}
	if rs.keyPair != feedRef {
		t.Error("keyPair not set correctly")
	}
	if rs.db != nil {
		t.Error("expected nil db")
	}
	if rs.state != nil {
		t.Error("expected nil state")
	}
}

func TestWhoamiHandlerHandled(t *testing.T) {
	h := &whoamiHandler{}

	if !h.Handled(muxrpc.Method{"whoami"}) {
		t.Error("expected Handled to return true for whoami")
	}
	if h.Handled(muxrpc.Method{"notWhoami"}) {
		t.Error("expected Handled to return false for notWhoami")
	}
	if h.Handled(muxrpc.Method{"whoami", "extra"}) {
		t.Error("expected Handled to return false for multi-segment method")
	}
	if h.Handled(muxrpc.Method{}) {
		t.Error("expected Handled to return false for empty method")
	}
}

func TestWhoamiHandlerHandleCallNonAsync(t *testing.T) {
	feedRef := mustRuntimeTestFeedRef(t, 5)
	srv := roomhandlers.NewRoomServer(feedRef, nil, nil, nil, nil, nil, nil, "")
	h := &whoamiHandler{srv: srv}

	// Non-async request: should call CloseWithError.
	// We only need to verify no panic; CloseWithError on a nil sink is safe enough
	// to test by calling directly. The code path calls req.CloseWithError.
	req := &muxrpc.Request{Type: "source"}
	h.HandleCall(context.Background(), req)
	// If we get here without panic, the non-async path was exercised.
}

func TestWhoamiHandlerHandleConnect(t *testing.T) {
	h := &whoamiHandler{}
	// HandleConnect is a no-op; just call it for coverage.
	h.HandleConnect(context.Background(), nil)
}

func TestAddMemberSuccess(t *testing.T) {
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

	feed := mustRuntimeTestFeedRef(t, 7)
	if err := rt.AddMember(ctx, *feed, roomdb.RoleMember); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
}

func TestConfigWithDefaultsAllEmpty(t *testing.T) {
	cfg := Config{}
	out := cfg.withDefaults()

	if out.ListenAddr != defaultMUXRPCListenAddr {
		t.Errorf("expected default listen addr %q, got %q", defaultMUXRPCListenAddr, out.ListenAddr)
	}
	if out.HTTPListenAddr != defaultHTTPListenAddr {
		t.Errorf("expected default HTTP listen addr %q, got %q", defaultHTTPListenAddr, out.HTTPListenAddr)
	}
	if out.RepoPath != defaultRoomRepoPath {
		t.Errorf("expected default repo path %q, got %q", defaultRoomRepoPath, out.RepoPath)
	}
	if out.Mode != defaultRoomMode {
		t.Errorf("expected default mode %q, got %q", defaultRoomMode, out.Mode)
	}
}

func TestConfigWithDefaultsPreservesExplicitValues(t *testing.T) {
	cfg := Config{
		ListenAddr:     "  0.0.0.0:9999  ",
		HTTPListenAddr: "  0.0.0.0:8888  ",
		RepoPath:       "  /custom/path  ",
		Mode:           "  OPEN  ",
	}
	out := cfg.withDefaults()

	if out.ListenAddr != "  0.0.0.0:9999  " {
		t.Errorf("expected preserved listen addr, got %q", out.ListenAddr)
	}
	if out.Mode != "open" {
		t.Errorf("expected lowercased+trimmed mode 'open', got %q", out.Mode)
	}
}

func TestModeStatusAllModes(t *testing.T) {
	tests := []struct {
		mode   roomdb.PrivacyMode
		label  string
		canInv bool
	}{
		{roomdb.ModeOpen, "Open", true},
		{roomdb.ModeCommunity, "Community", false},
		{roomdb.ModeRestricted, "Restricted", false},
		{roomdb.ModeUnknown, "Unknown", false},
	}

	for _, tc := range tests {
		h := bridgeRoomHandler{roomConfig: &mockRoomConfig{mode: tc.mode}}
		status := h.modeStatus(context.Background())
		if status.Label != tc.label {
			t.Errorf("mode %v: expected label %q, got %q", tc.mode, tc.label, status.Label)
		}
		if status.CanSelfServeInvite != tc.canInv {
			t.Errorf("mode %v: expected CanSelfServeInvite=%v, got %v", tc.mode, tc.canInv, status.CanSelfServeInvite)
		}
	}

	// Test nil roomConfig
	h2 := bridgeRoomHandler{roomConfig: nil}
	status := h2.modeStatus(context.Background())
	if status.Label != "Unknown" {
		t.Errorf("nil config: expected Unknown label, got %q", status.Label)
	}

	// Test error reading config
	h3 := bridgeRoomHandler{roomConfig: &mockRoomConfig{err: fmt.Errorf("db error")}}
	status = h3.modeStatus(context.Background())
	if status.Label != "Unknown" {
		t.Errorf("error config: expected Unknown label, got %q", status.Label)
	}
}

func TestFilterAndSortBotsWithSearch(t *testing.T) {
	bots := []botCardData{
		{ATDID: "did:plc:alpha", SSBFeedID: "feedAlpha", FeedURI: "uri1", TotalMessages: 10, PublishedMessages: 8},
		{ATDID: "did:plc:beta", SSBFeedID: "feedBeta", FeedURI: "uri2", TotalMessages: 5, PublishedMessages: 3},
		{ATDID: "did:plc:gamma", SSBFeedID: "feedGamma", FeedURI: "uri3", TotalMessages: 20, PublishedMessages: 15},
	}

	// Search filter
	result := filterAndSortBots(bots, "alpha", "activity_desc")
	if len(result) != 1 {
		t.Fatalf("expected 1 result for alpha search, got %d", len(result))
	}
	if result[0].ATDID != "did:plc:alpha" {
		t.Errorf("expected alpha, got %s", result[0].ATDID)
	}

	// No search, activity_desc sort
	result = filterAndSortBots(bots, "", "activity_desc")
	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}
	if result[0].TotalMessages != 20 {
		t.Errorf("expected gamma (20 msgs) first, got %d", result[0].TotalMessages)
	}

	// newest sort
	bots2 := []botCardData{
		{ATDID: "a", CreatedUnix: 100},
		{ATDID: "b", CreatedUnix: 300},
		{ATDID: "c", CreatedUnix: 200},
	}
	result = filterAndSortBots(bots2, "", "newest")
	if result[0].CreatedUnix != 300 {
		t.Errorf("expected newest first (300), got %d", result[0].CreatedUnix)
	}

	// deferred_desc sort
	bots3 := []botCardData{
		{ATDID: "a", DeferredMessages: 5, FailedMessages: 2, TotalMessages: 10},
		{ATDID: "b", DeferredMessages: 5, FailedMessages: 3, TotalMessages: 10},
		{ATDID: "c", DeferredMessages: 10, FailedMessages: 0, TotalMessages: 5},
		{ATDID: "d", DeferredMessages: 5, FailedMessages: 3, TotalMessages: 20},
	}
	result = filterAndSortBots(bots3, "", "deferred_desc")
	if result[0].ATDID != "c" {
		t.Errorf("expected c first (most deferred), got %s", result[0].ATDID)
	}
	// Same deferred, higher failed first
	if result[1].FailedMessages != 3 || result[2].FailedMessages != 3 {
		t.Errorf("expected failed=3 next")
	}

	// Case-insensitive search
	result = filterAndSortBots(bots, "BETA", "activity_desc")
	if len(result) != 1 || result[0].ATDID != "did:plc:beta" {
		t.Errorf("case-insensitive search failed")
	}
}

func TestDerefTime(t *testing.T) {
	fallback := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// nil value
	result := derefTime(nil, fallback)
	if !result.Equal(fallback) {
		t.Errorf("expected fallback for nil, got %v", result)
	}

	// zero value
	zero := time.Time{}
	result = derefTime(&zero, fallback)
	if !result.Equal(fallback) {
		t.Errorf("expected fallback for zero time, got %v", result)
	}

	// valid value
	valid := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	result = derefTime(&valid, fallback)
	if !result.Equal(valid) {
		t.Errorf("expected valid time, got %v", result)
	}
}

func TestFormatHumanTime(t *testing.T) {
	// Zero time
	if got := formatHumanTime(time.Time{}); got != "" {
		t.Errorf("expected empty string for zero time, got %q", got)
	}

	// Non-zero time
	ts := time.Date(2025, 3, 15, 14, 30, 0, 0, time.UTC)
	got := formatHumanTime(ts)
	if got != "15 Mar 2025, 14:30 UTC" {
		t.Errorf("unexpected format: %q", got)
	}
}

func TestAbbreviateFeed(t *testing.T) {
	// Short feed (<=20 chars) returned as-is
	short := "short"
	if got := abbreviateFeed(short); got != short {
		t.Errorf("expected %q, got %q", short, got)
	}

	// Exactly 20 chars
	exact := "12345678901234567890"
	if got := abbreviateFeed(exact); got != exact {
		t.Errorf("expected %q, got %q", exact, got)
	}

	// Long feed gets abbreviated
	long := "abcdefghijklmnopqrstuvwxyz1234567890"
	got := abbreviateFeed(long)
	if !strings.HasPrefix(got, "abcdefghijkl") {
		t.Errorf("expected prefix abcdefghijkl, got %q", got)
	}
	if !strings.Contains(got, "\u2026") {
		t.Errorf("expected ellipsis in abbreviated feed, got %q", got)
	}
}

func TestAbbreviateDID(t *testing.T) {
	// Short DID (<=24 chars) returned as-is
	short := "did:plc:short"
	if got := abbreviateDID(short); got != short {
		t.Errorf("expected %q, got %q", short, got)
	}

	// Long DID gets abbreviated
	long := "did:plc:abcdefghijklmnopqrstuvwxyz"
	got := abbreviateDID(long)
	if len(got) > len(long) {
		t.Errorf("abbreviated should be shorter")
	}
	if !strings.Contains(got, "\u2026") {
		t.Errorf("expected ellipsis in abbreviated DID, got %q", got)
	}
}

func TestInviteCreationMethod(t *testing.T) {
	if !inviteCreationMethod(http.MethodGet) {
		t.Error("GET should be valid invite creation method")
	}
	if !inviteCreationMethod(http.MethodHead) {
		t.Error("HEAD should be valid invite creation method")
	}
	if !inviteCreationMethod(http.MethodPost) {
		t.Error("POST should be valid invite creation method")
	}
	if inviteCreationMethod(http.MethodPut) {
		t.Error("PUT should not be valid invite creation method")
	}
	if inviteCreationMethod(http.MethodDelete) {
		t.Error("DELETE should not be valid invite creation method")
	}
}

func TestNewBridgeRoomHandlerNilStock(t *testing.T) {
	h := newBridgeRoomHandler(nil, nil, nil, nil)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}

	// Verify it serves requests (the nil stock gets replaced with NotFoundHandler)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/some-unknown-route", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown route with nil stock, got %d", rr.Code)
	}
}

type errorBotLister struct{}

func (e *errorBotLister) ListActiveBridgedAccountsWithStats(ctx context.Context) ([]db.BridgedAccountStats, error) {
	return nil, fmt.Errorf("db connection failed")
}

func TestHandleLandingError(t *testing.T) {
	h := bridgeRoomHandler{
		stock:           http.NotFoundHandler(),
		roomConfig:      &mockRoomConfig{mode: roomdb.ModeOpen},
		bridgeBotLister: &errorBotLister{},
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.handleLanding(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for lister error, got %d", rr.Code)
	}
}

func TestHandleBotsError(t *testing.T) {
	h := bridgeRoomHandler{
		stock:           http.NotFoundHandler(),
		roomConfig:      &mockRoomConfig{mode: roomdb.ModeOpen},
		bridgeBotLister: &errorBotLister{},
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/bots", nil)
	h.handleBots(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for lister error, got %d", rr.Code)
	}
}

func TestNormalizeBotSort(t *testing.T) {
	if got := normalizeBotSort("newest"); got != "newest" {
		t.Errorf("expected newest, got %q", got)
	}
	if got := normalizeBotSort("deferred_desc"); got != "deferred_desc" {
		t.Errorf("expected deferred_desc, got %q", got)
	}
	if got := normalizeBotSort("invalid"); got != "activity_desc" {
		t.Errorf("expected activity_desc for invalid, got %q", got)
	}
	if got := normalizeBotSort(""); got != "activity_desc" {
		t.Errorf("expected activity_desc for empty, got %q", got)
	}
}

func TestWantsJSONResponse(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if wantsJSONResponse(req) {
		t.Error("expected false with no Accept header")
	}

	req.Header.Set("Accept", "application/json")
	if !wantsJSONResponse(req) {
		t.Error("expected true with application/json Accept header")
	}

	req.Header.Set("Accept", "text/html, Application/JSON")
	if !wantsJSONResponse(req) {
		t.Error("expected true with mixed-case Accept header containing application/json")
	}
}

func TestHandleInviteCreationUnavailableRedirect(t *testing.T) {
	h := bridgeRoomHandler{
		roomConfig: &mockRoomConfig{mode: roomdb.ModeCommunity},
	}

	// Without JSON Accept header, should redirect
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/create-invite", nil)
	h.handleInviteCreationUnavailable(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Errorf("expected 303 redirect, got %d", rr.Code)
	}
}

func TestServeHTTPCreateInviteRestrictedMode(t *testing.T) {
	var delegated bool
	h := bridgeRoomHandler{
		stock: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			delegated = true
			w.WriteHeader(http.StatusAccepted)
		}),
		roomConfig: &mockRoomConfig{mode: roomdb.ModeRestricted},
	}

	// create-invite should delegate to stock handler.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/create-invite", nil)
	h.ServeHTTP(rr, req)
	if !delegated {
		t.Fatal("expected create-invite to delegate to stock handler")
	}
	if rr.Code != http.StatusAccepted {
		t.Errorf("expected delegated status code, got %d", rr.Code)
	}
}

func TestServeHTTPHealthzHead(t *testing.T) {
	h := bridgeRoomHandler{stock: http.NotFoundHandler()}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, "/healthz", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for HEAD /healthz, got %d", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("expected empty body for HEAD /healthz, got %q", rr.Body.String())
	}
}

func TestBuildBotSortOptions(t *testing.T) {
	opts := buildBotSortOptions("newest")
	if len(opts) != 3 {
		t.Fatalf("expected 3 sort options, got %d", len(opts))
	}
	for _, opt := range opts {
		if opt.Value == "newest" && !opt.Selected {
			t.Error("expected newest to be selected")
		}
		if opt.Value != "newest" && opt.Selected {
			t.Errorf("expected %s to not be selected", opt.Value)
		}
	}
}

func TestSetPublicCacheHeaders(t *testing.T) {
	rr := httptest.NewRecorder()
	setPublicCacheHeaders(rr)
	if got := rr.Header().Get("Cache-Control"); got != "public, max-age=30" {
		t.Errorf("expected Cache-Control header, got %q", got)
	}
}

func TestAnnouncePeer(t *testing.T) {
	// Test nil runtime
	var r *Runtime
	r.AnnouncePeer(refs.FeedRef{}, "addr")

	// Test nil state
	r = &Runtime{}
	r.AnnouncePeer(refs.FeedRef{}, "addr")

	// Test with state
	r.state = roomstate.NewManager()
	kp, _ := keys.Generate()
	r.AnnouncePeer(kp.FeedRef(), "net:1.2.3.4:8008~shs:key")
}

func TestWhoamiHandleConnect(t *testing.T) {
	h := &whoamiHandler{}
	h.HandleConnect(context.Background(), nil)
}

func TestNormalizeNextPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"   ", ""},
		{"/", "/"},
		{"/accounts", "/accounts"},
		{"/room/123", "/room/123"},
		{"//", ""},
		{"http://example.com", ""},
		{"https://example.com", ""},
		{"ftp://example.com", ""},
		{"/http://evil.com", ""},
		{"/https://evil.com", ""},
	}
	for _, tt := range tests {
		result := normalizeNextPath(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeNextPath(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestHandleLoginSubmitMissingFields(t *testing.T) {
	h := &authHandler{}

	tests := []struct {
		name     string
		username string
		password string
		wantCode int
	}{
		{"empty username", "", "password123", http.StatusBadRequest},
		{"empty password", "user", "", http.StatusBadRequest},
		{"both empty", "", "", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(""))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.PostForm = url.Values{
				"username": {tt.username},
				"password": {tt.password},
			}
			rr := httptest.NewRecorder()
			h.handleLoginSubmit(rr, req)
			if rr.Code != tt.wantCode {
				t.Errorf("handleLoginSubmit returned %d, want %d", rr.Code, tt.wantCode)
			}
		})
	}
}

func TestHandleResetPasswordSubmitMissingFields(t *testing.T) {
	h := &authHandler{}

	tests := []struct {
		name     string
		token    string
		password string
		wantCode int
	}{
		{"empty token", "", "password123", http.StatusBadRequest},
		{"empty password", "token", "", http.StatusBadRequest},
		{"both empty", "", "", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/reset-password", strings.NewReader(""))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.PostForm = url.Values{
				"token":    {tt.token},
				"password": {tt.password},
			}
			rr := httptest.NewRecorder()
			h.handleResetPasswordSubmit(rr, req)
			if rr.Code != tt.wantCode {
				t.Errorf("handleResetPasswordSubmit returned %d, want %d", rr.Code, tt.wantCode)
			}
		})
	}
}
