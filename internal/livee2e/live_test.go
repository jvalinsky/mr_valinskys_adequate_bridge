package livee2e

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	atproto "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
	appbsky "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/appbsky"
	lexutil "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/lexutil"
	xrpc "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/xrpc"
	cbg "github.com/whyrusleeping/cbor-gen"
)

func TestBridgeLiveInterop(t *testing.T) {
	if !liveE2EEnabled(os.Getenv) {
		t.Skip("set LIVE_E2E_ENABLED=1 (or provide it via LIVE_ATPROTO_ENV_FILE/LIVE_ATPROTO_CONFIG_FILE) to run live relay/room interoperability test")
	}

	host := strings.TrimSpace(getEnvDefault("LIVE_ATPROTO_HOST", "https://bsky.social"))
	plcURL := strings.TrimSpace(getEnvDefault("LIVE_ATPROTO_PLC_URL", host+":2582/plc"))
	authCfg, err := resolveLiveAuthConfig(os.Getenv)
	if err != nil {
		t.Fatalf("resolve live auth config: %v", err)
	}
	peerVerifyCmd := requireEnv(t, "LIVE_ROOM_PEER_VERIFY_CMD")
	relayURL := strings.TrimSpace(getEnvDefault("LIVE_RELAY_URL", "wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos"))
	seed := strings.TrimSpace(getEnvDefault("LIVE_BRIDGE_BOT_SEED", "live-e2e-seed-change-me"))
	timeout := parseDurationDefault(getEnvDefault("LIVE_E2E_TIMEOUT", "4m"), 4*time.Minute)

	moduleRoot, err := resolveModuleRoot()
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	xrpcc := &xrpc.Client{Host: strings.TrimRight(host, "/")}
	session, err := createSession(ctx, xrpcc, authCfg.SourceIdentifier, authCfg.SourceAppPassword)
	if err != nil {
		t.Fatalf("create atproto session: %v", err)
	}
	sourceDID := session.Did
	xrpcc.Auth = &xrpc.AuthInfo{
		AccessJwt:  session.AccessJwt,
		RefreshJwt: session.RefreshJwt,
		Did:        session.Did,
		Handle:     session.Handle,
	}
	targetDID := authCfg.FollowTargetDID
	if targetDID == "" {
		targetClient := &xrpc.Client{Host: strings.TrimRight(host, "/")}
		targetSession, err := createSession(ctx, targetClient, authCfg.TargetIdentifier, authCfg.TargetAppPassword)
		if err != nil {
			t.Fatalf("derive follow target DID via target app-password session: %v", err)
		}
		targetDID = strings.TrimSpace(targetSession.Did)
		if targetDID == "" {
			t.Fatalf("derive follow target DID via target app-password session: session did is empty")
		}
	}

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "bridge.sqlite")
	repoPath := filepath.Join(tempDir, "ssb-repo")
	roomMuxAddr := getEnvDefault("LIVE_ROOM_MUXRPC_ADDR", "127.0.0.1:9898")
	roomHTTPAddr := getEnvDefault("LIVE_ROOM_HTTP_ADDR", "127.0.0.1:9876")
	roomMode := strings.TrimSpace(getEnvDefault("LIVE_ROOM_MODE", "community"))

	runBridgeCommand(ctx, t, moduleRoot, "account add source", []string{
		"--db", dbPath,
		"--bot-seed", seed,
		"account", "add", sourceDID,
	})
	runBridgeCommand(ctx, t, moduleRoot, "account add follow target", []string{
		"--db", dbPath,
		"--bot-seed", seed,
		"account", "add", targetDID,
	})

	var bridgeLogs bytes.Buffer
	startArgs := []string{
		"--db", dbPath,
		"--relay-url", relayURL,
		"--bot-seed", seed,
		"start",
		"--repo-path", repoPath,
		"--room-enable",
		"--room-listen-addr", roomMuxAddr,
		"--room-http-listen-addr", roomHTTPAddr,
		"--room-mode", roomMode,
		"--publish-workers", "2",
		"--xrpc-host", host,
		"--plc-url", plcURL,
		"--atproto-insecure",
	}
	bridgeProc := exec.CommandContext(ctx, "go", append([]string{"run", "./cmd/bridge-cli"}, startArgs...)...)
	bridgeProc.Dir = moduleRoot
	bridgeProc.Stdout = &bridgeLogs
	bridgeProc.Stderr = &bridgeLogs
	if err := bridgeProc.Start(); err != nil {
		t.Fatalf("start bridge process: %v", err)
	}
	defer func() {
		_ = bridgeProc.Process.Kill()
		done := make(chan struct{})
		go func() {
			_ = bridgeProc.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			// Avoid hanging the test process on stubborn subprocess teardown.
		}
	}()

	waitForBridgeStatus(ctx, t, dbPath, "live", &bridgeLogs)

	postURI, postCID := createRecord(ctx, t, xrpcc, sourceDID, "app.bsky.feed.post", &appbsky.FeedPost{
		LexiconTypeID: "app.bsky.feed.post",
		Text:          fmt.Sprintf("bridge live e2e post %d", time.Now().UnixNano()),
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	})
	waitForPublishedRecords(ctx, t, dbPath, []string{postURI}, &bridgeLogs)

	likeURI, _ := createRecord(ctx, t, xrpcc, sourceDID, "app.bsky.feed.like", &appbsky.FeedLike{
		LexiconTypeID: "app.bsky.feed.like",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		Subject: &appbsky.RepoStrongRef{
			Uri: postURI,
			Cid: postCID,
		},
	})

	followURI, _ := createRecord(ctx, t, xrpcc, sourceDID, "app.bsky.graph.follow", &appbsky.GraphFollow{
		LexiconTypeID: "app.bsky.graph.follow",
		Subject:       targetDID,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	})

	expectedURIs := []string{postURI, likeURI, followURI}
	waitForPublishedRecords(ctx, t, dbPath, expectedURIs, &bridgeLogs)

	verify := exec.CommandContext(ctx, "sh", "-lc", peerVerifyCmd)
	verify.Dir = moduleRoot
	verify.Env = append(os.Environ(),
		fmt.Sprintf("LIVE_ROOM_MUXRPC_ADDR=%s", roomMuxAddr),
		fmt.Sprintf("LIVE_ROOM_HTTP_ADDR=%s", roomHTTPAddr),
		fmt.Sprintf("LIVE_BRIDGE_DB_PATH=%s", dbPath),
		fmt.Sprintf("LIVE_BRIDGE_REPO_PATH=%s", repoPath),
		fmt.Sprintf("LIVE_BRIDGE_SOURCE_DID=%s", sourceDID),
		fmt.Sprintf("LIVE_BRIDGE_TARGET_DID=%s", targetDID),
		fmt.Sprintf("LIVE_BRIDGE_EXPECTED_URIS=%s", strings.Join(expectedURIs, ",")),
		"LIVE_REQUIRE_ACTIVE_BRIDGED_PEERS=1",
	)
	verifyOut, err := verify.CombinedOutput()
	if err != nil {
		t.Fatalf("peer verification failed: %v\noutput:\n%s\nbridge logs:\n%s", err, string(verifyOut), summarizeLogs(bridgeLogs.String()))
	}
}

type liveAuthConfig struct {
	SourceIdentifier  string
	SourceAppPassword string
	TargetIdentifier  string
	TargetAppPassword string
	FollowTargetDID   string
	ConfigPath        string
}

func resolveLiveAuthConfig(getenv func(string) string) (liveAuthConfig, error) {
	cfgPath := getConfigPath(getenv)

	fileValues := map[string]string{}
	if cfgPath != "" {
		var err error
		fileValues, err = parseShellEnvFile(cfgPath)
		if err != nil {
			return liveAuthConfig{}, fmt.Errorf("parse %s: %w", cfgPath, err)
		}
	}

	lookup := func(key string) string {
		if val := strings.TrimSpace(getenv(key)); val != "" {
			return val
		}
		return strings.TrimSpace(fileValues[key])
	}

	cfg := liveAuthConfig{
		ConfigPath: cfgPath,
		SourceIdentifier: firstNonEmpty(
			lookup("LIVE_ATPROTO_SOURCE_IDENTIFIER"),
			lookup("LIVE_ATPROTO_IDENTIFIER"),
		),
		SourceAppPassword: firstNonEmpty(
			lookup("LIVE_ATPROTO_SOURCE_APP_PASSWORD"),
			lookup("LIVE_ATPROTO_PASSWORD"),
		),
		TargetIdentifier: firstNonEmpty(
			lookup("LIVE_ATPROTO_TARGET_IDENTIFIER"),
		),
		TargetAppPassword: firstNonEmpty(
			lookup("LIVE_ATPROTO_TARGET_APP_PASSWORD"),
		),
		FollowTargetDID: firstNonEmpty(
			lookup("LIVE_ATPROTO_FOLLOW_TARGET_DID"),
			lookup("LIVE_ATPROTO_TARGET_DID"),
		),
	}

	if cfg.SourceIdentifier == "" {
		return liveAuthConfig{}, errors.New("missing source identifier: set LIVE_ATPROTO_SOURCE_IDENTIFIER (or legacy LIVE_ATPROTO_IDENTIFIER)")
	}
	if cfg.SourceAppPassword == "" {
		return liveAuthConfig{}, errors.New("missing source app password: set LIVE_ATPROTO_SOURCE_APP_PASSWORD (or legacy LIVE_ATPROTO_PASSWORD)")
	}
	if cfg.FollowTargetDID == "" && (cfg.TargetIdentifier == "" || cfg.TargetAppPassword == "") {
		return liveAuthConfig{}, errors.New("missing follow target auth: set LIVE_ATPROTO_FOLLOW_TARGET_DID or both LIVE_ATPROTO_TARGET_IDENTIFIER and LIVE_ATPROTO_TARGET_APP_PASSWORD")
	}

	return cfg, nil
}

func liveE2EEnabled(getenv func(string) string) bool {
	if strings.TrimSpace(getenv("LIVE_E2E_ENABLED")) == "1" {
		return true
	}

	cfgPath := getConfigPath(getenv)
	if cfgPath == "" {
		return false
	}
	values, err := parseShellEnvFile(cfgPath)
	if err != nil {
		return false
	}
	return strings.TrimSpace(values["LIVE_E2E_ENABLED"]) == "1"
}

func getConfigPath(getenv func(string) string) string {
	return strings.TrimSpace(firstNonEmpty(
		getenv("LIVE_ATPROTO_CONFIG_FILE"),
		getenv("LIVE_ATPROTO_ENV_FILE"),
	))
}

func parseShellEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		raw = strings.TrimPrefix(raw, "export ")
		key, val, ok := strings.Cut(raw, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE", lineNum)
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", lineNum)
		}
		values[key] = trimOptionalMatchingQuotes(val)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func trimOptionalMatchingQuotes(raw string) string {
	if len(raw) < 2 {
		return raw
	}
	first := raw[0]
	last := raw[len(raw)-1]
	if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
		return raw[1 : len(raw)-1]
	}
	return raw
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func createSession(ctx context.Context, xrpcc *xrpc.Client, identifier, password string) (*atproto.ServerCreateSession_Output, error) {
	return atproto.ServerCreateSession(ctx, xrpcc, &atproto.ServerCreateSession_Input{
		Identifier: identifier,
		Password:   password,
	})
}

func runBridgeCommand(ctx context.Context, t *testing.T, moduleRoot, label string, args []string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "go", append([]string{"run", "./cmd/bridge-cli"}, args...)...)
	cmd.Dir = moduleRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\noutput:\n%s", label, err, string(out))
	}
}

func waitForBridgeStatus(ctx context.Context, t *testing.T, dbPath, want string, bridgeLogs *bytes.Buffer) {
	t.Helper()
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open bridge db while waiting for status: %v", err)
	}
	defer database.Close()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		status, ok, err := database.GetBridgeState(ctx, "bridge_runtime_status")
		if err != nil {
			t.Fatalf("read bridge runtime status: %v", err)
		}
		if ok && status == want {
			return
		}

		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for bridge runtime status=%q\nbridge logs:\n%s", want, summarizeLogs(bridgeLogs.String()))
		case <-ticker.C:
		}
	}
}

func waitForPublishedRecords(ctx context.Context, t *testing.T, dbPath string, atURIs []string, bridgeLogs *bytes.Buffer) {
	t.Helper()

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open bridge db while waiting for records: %v", err)
	}
	defer database.Close()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		allPublished := true
		for _, atURI := range atURIs {
			msg, err := database.GetMessage(ctx, atURI)
			if err != nil {
				t.Fatalf("read message %s: %v", atURI, err)
			}
			if msg == nil || msg.MessageState != db.MessageStatePublished || strings.TrimSpace(msg.SSBMsgRef) == "" {
				allPublished = false
				break
			}
		}
		if allPublished {
			return
		}

		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for published records: %s\nbridge logs:\n%s", strings.Join(atURIs, ", "), summarizeLogs(bridgeLogs.String()))
		case <-ticker.C:
		}
	}
}

func createRecord(ctx context.Context, t *testing.T, xrpcc *xrpc.Client, did, collection string, record cbg.CBORMarshaler) (string, string) {
	t.Helper()

	out, err := atproto.RepoCreateRecord(ctx, xrpcc, &atproto.RepoCreateRecord_Input{
		Collection: collection,
		Repo:       did,
		Record:     &lexutil.LexiconTypeDecoder{Val: record},
	})
	if err != nil {
		t.Fatalf("create %s record: %v", collection, err)
	}
	return out.Uri, out.Cid
}

func resolveModuleRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return "", fmt.Errorf("module root not found from %s: %w", wd, err)
	}
	return root, nil
}

func summarizeLogs(raw string) string {
	if len(raw) <= 8000 {
		return raw
	}
	return raw[len(raw)-8000:]
}

func requireEnv(t *testing.T, key string) string {
	t.Helper()
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		t.Fatalf("%s is required when LIVE_E2E_ENABLED=1", key)
	}
	return val
}

func getEnvDefault(key, fallback string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	return val
}

func parseDurationDefault(raw string, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}
