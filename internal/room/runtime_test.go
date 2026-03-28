package room

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	refs "github.com/ssbc/go-ssb-refs"
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

func mustRuntimeTestFeedRef(t *testing.T, fill byte) refs.FeedRef {
	t.Helper()

	ref, err := refs.NewFeedRefFromBytes(bytes.Repeat([]byte{fill}, 32), refs.RefAlgoFeedSSB1)
	if err != nil {
		t.Fatalf("create test feed ref: %v", err)
	}
	return ref
}
