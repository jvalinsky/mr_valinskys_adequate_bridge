package livee2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
	appbsky "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/appbsky"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/syntax"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/xrpc"
)

const liveReversePasswordEnv = "LIVE_E2E_REVERSE_SOURCE_PASSWORD"

func TestBridgeLiveInteropReverseSSBClient(t *testing.T) {
	if !liveE2EEnabled(os.Getenv) {
		t.Skip("set LIVE_E2E_ENABLED=1 (or provide it via LIVE_ATPROTO_ENV_FILE/LIVE_ATPROTO_CONFIG_FILE) to run live reverse interoperability tests")
	}

	host := strings.TrimSpace(getEnvDefault("LIVE_ATPROTO_HOST", "https://bsky.social"))
	plcURL := strings.TrimSpace(getEnvDefault("LIVE_ATPROTO_PLC_URL", host+":2582/plc"))
	authCfg, err := resolveLiveAuthConfig(os.Getenv)
	if err != nil {
		t.Fatalf("resolve live auth config: %v", err)
	}
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
	sourceSession, err := createSession(ctx, xrpcc, authCfg.SourceIdentifier, authCfg.SourceAppPassword)
	if err != nil {
		t.Fatalf("create source atproto session: %v", err)
	}
	sourceDID := strings.TrimSpace(sourceSession.Did)
	xrpcc.Auth = &xrpc.AuthInfo{
		AccessJwt:  sourceSession.AccessJwt,
		RefreshJwt: sourceSession.RefreshJwt,
		Did:        sourceSession.Did,
		Handle:     sourceSession.Handle,
	}

	targetDID := strings.TrimSpace(authCfg.FollowTargetDID)
	if targetDID == "" {
		targetClient := &xrpc.Client{Host: strings.TrimRight(host, "/")}
		targetSession, err := createSession(ctx, targetClient, authCfg.TargetIdentifier, authCfg.TargetAppPassword)
		if err != nil {
			t.Fatalf("create target atproto session: %v", err)
		}
		targetDID = strings.TrimSpace(targetSession.Did)
	}
	if sourceDID == "" || targetDID == "" {
		t.Fatalf("missing source/target did: source=%q target=%q", sourceDID, targetDID)
	}

	if err := os.Setenv(liveReversePasswordEnv, authCfg.SourceAppPassword); err != nil {
		t.Fatalf("set reverse password env: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv(liveReversePasswordEnv) })

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "bridge.sqlite")
	repoPath := filepath.Join(tempDir, "ssb-repo")
	clientRepoPath := filepath.Join(tempDir, "ssb-client")
	clientListenAddr := strings.TrimSpace(getEnvDefault("LIVE_SSB_CLIENT_LISTEN_ADDR", "127.0.0.1:18008"))
	clientHTTPAddr := strings.TrimSpace(getEnvDefault("LIVE_SSB_CLIENT_HTTP_ADDR", "127.0.0.1:18080"))
	clientBaseURL := "http://" + clientHTTPAddr
	roomMuxAddr := getEnvDefault("LIVE_ROOM_MUXRPC_ADDR", "127.0.0.1:9898")
	roomHTTPAddr := getEnvDefault("LIVE_ROOM_HTTP_ADDR", "127.0.0.1:9876")
	roomMode := strings.TrimSpace(getEnvDefault("LIVE_ROOM_MODE", "open"))
	reverseCredsPath := filepath.Join(tempDir, "reverse-credentials.json")

	if err := writeReverseCredentialsFile(reverseCredsPath, sourceDID, authCfg.SourceIdentifier, host, liveReversePasswordEnv); err != nil {
		t.Fatalf("write reverse credentials file: %v", err)
	}

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
	bridgeArgs := []string{
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
		"--mcp-listen-addr", "",
		"--metrics-listen-addr", "",
		"--reverse-sync-enable",
		"--reverse-credentials-file", reverseCredsPath,
		"--reverse-sync-scan-interval", "1s",
		"--reverse-sync-batch-size", "25",
	}
	bridgeProc := exec.CommandContext(ctx, "go", append([]string{"run", "./cmd/bridge-cli"}, bridgeArgs...)...)
	bridgeProc.Dir = moduleRoot
	bridgeProc.Stdout = &bridgeLogs
	bridgeProc.Stderr = &bridgeLogs
	if err := bridgeProc.Start(); err != nil {
		t.Fatalf("start bridge process: %v", err)
	}
	defer stopCommand(bridgeProc)

	waitForBridgeStatus(ctx, t, dbPath, "live", &bridgeLogs)

	var clientLogs bytes.Buffer
	clientArgs := []string{
		"run", "./cmd/ssb-client",
		"--repo-path", clientRepoPath,
		"--listen-addr", clientListenAddr,
		"--http-listen-addr", clientHTTPAddr,
		"--room-http-addr", "http://" + roomHTTPAddr,
		"--room-mode", "open",
		"serve",
	}
	clientProc := exec.CommandContext(ctx, "go", clientArgs...)
	clientProc.Dir = moduleRoot
	clientProc.Stdout = &clientLogs
	clientProc.Stderr = &clientLogs
	if err := clientProc.Start(); err != nil {
		t.Fatalf("start ssb-client process: %v", err)
	}
	defer stopCommand(clientProc)

	clientFeedID := waitForSSBClientWhoami(ctx, t, clientBaseURL, &clientLogs)
	inviteURL := createRoomInvite(ctx, t, "http://"+roomHTTPAddr, &bridgeLogs)
	joinSSBClientRoom(ctx, t, clientBaseURL, inviteURL, &clientLogs)
	waitForSSBClientPeers(ctx, t, clientBaseURL, 1, &clientLogs)

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open bridge db: %v", err)
	}
	defer database.Close()

	sourceAccount, err := database.GetBridgedAccount(ctx, sourceDID)
	if err != nil {
		t.Fatalf("get source bridged account: %v", err)
	}
	targetAccount, err := database.GetBridgedAccount(ctx, targetDID)
	if err != nil {
		t.Fatalf("get target bridged account: %v", err)
	}
	if sourceAccount == nil || targetAccount == nil {
		t.Fatalf("missing bridged accounts: source=%#v target=%#v", sourceAccount, targetAccount)
	}

	bridgeFeedID, err := readSSBFeedID(filepath.Join(repoPath, "secret"))
	if err != nil {
		t.Fatalf("read bridge feed id: %v", err)
	}

	publishSSBClientJSON(ctx, t, clientBaseURL, map[string]any{
		"type":      "contact",
		"contact":   sourceAccount.SSBFeedID,
		"following": true,
		"blocking":  false,
	}, &clientLogs)
	publishSSBClientJSON(ctx, t, clientBaseURL, map[string]any{
		"type":      "contact",
		"contact":   bridgeFeedID,
		"following": true,
		"blocking":  false,
	}, &clientLogs)

	if err := database.AddReverseIdentityMapping(ctx, db.ReverseIdentityMapping{
		SSBFeedID:    clientFeedID,
		ATDID:        sourceDID,
		Active:       true,
		AllowPosts:   true,
		AllowReplies: true,
		AllowFollows: true,
	}); err != nil {
		t.Fatalf("add reverse mapping: %v", err)
	}

	rootRef := publishSSBClientJSON(ctx, t, clientBaseURL, map[string]any{
		"type": "post",
		"text": fmt.Sprintf("ssb-client reverse root %d", time.Now().UnixNano()),
	}, &clientLogs)
	rootEvent := waitForReverseEventState(ctx, t, database, rootRef, db.ReverseEventStatePublished, &bridgeLogs)
	rootRecord := waitForATRecord(ctx, t, xrpcc, rootEvent.ResultATURI, &bridgeLogs)
	rootPost, ok := rootRecord.Value.Val.(*appbsky.FeedPost)
	if !ok {
		t.Fatalf("expected root record to be feed post, got %T", rootRecord.Value.Val)
	}
	if !strings.Contains(rootPost.Text, "ssb-client reverse root") || rootPost.Reply != nil {
		t.Fatalf("unexpected root post payload: %#v", rootPost)
	}

	replyRef := publishSSBClientJSON(ctx, t, clientBaseURL, map[string]any{
		"type":   "post",
		"text":   fmt.Sprintf("ssb-client reverse reply %d", time.Now().UnixNano()),
		"root":   rootRef,
		"branch": rootRef,
	}, &clientLogs)
	replyEvent := waitForReverseEventState(ctx, t, database, replyRef, db.ReverseEventStatePublished, &bridgeLogs)
	replyRecord := waitForATRecord(ctx, t, xrpcc, replyEvent.ResultATURI, &bridgeLogs)
	replyPost, ok := replyRecord.Value.Val.(*appbsky.FeedPost)
	if !ok {
		t.Fatalf("expected reply record to be feed post, got %T", replyRecord.Value.Val)
	}
	if replyPost.Reply == nil || replyPost.Reply.Root == nil || replyPost.Reply.Parent == nil {
		t.Fatalf("expected reply refs in reverse post: %#v", replyPost)
	}
	if replyPost.Reply.Root.Uri != rootEvent.ResultATURI || replyPost.Reply.Parent.Uri != rootEvent.ResultATURI {
		t.Fatalf("unexpected reply refs: %#v", replyPost.Reply)
	}
	if replyPost.Reply.Root.Cid != rootEvent.ResultATCID || replyPost.Reply.Parent.Cid != rootEvent.ResultATCID {
		t.Fatalf("unexpected reply CIDs: %#v", replyPost.Reply)
	}

	followRef := publishSSBClientJSON(ctx, t, clientBaseURL, map[string]any{
		"type":      "contact",
		"contact":   targetAccount.SSBFeedID,
		"following": true,
		"blocking":  false,
	}, &clientLogs)
	followEvent := waitForReverseEventState(ctx, t, database, followRef, db.ReverseEventStatePublished, &bridgeLogs)
	followRecord := waitForATRecord(ctx, t, xrpcc, followEvent.ResultATURI, &bridgeLogs)
	follow, ok := followRecord.Value.Val.(*appbsky.GraphFollow)
	if !ok {
		t.Fatalf("expected follow record, got %T", followRecord.Value.Val)
	}
	if strings.TrimSpace(follow.Subject) != targetDID {
		t.Fatalf("unexpected follow subject: %#v", follow)
	}

	unfollowRef := publishSSBClientJSON(ctx, t, clientBaseURL, map[string]any{
		"type":      "contact",
		"contact":   targetAccount.SSBFeedID,
		"following": false,
		"blocking":  false,
	}, &clientLogs)
	unfollowEvent := waitForReverseEventState(ctx, t, database, unfollowRef, db.ReverseEventStatePublished, &bridgeLogs)
	if strings.TrimSpace(unfollowEvent.ResultATURI) != strings.TrimSpace(followEvent.ResultATURI) {
		t.Fatalf("unexpected unfollow result uri: follow=%q unfollow=%q", followEvent.ResultATURI, unfollowEvent.ResultATURI)
	}
	waitForATRecordDeleted(ctx, t, xrpcc, followEvent.ResultATURI, &bridgeLogs)
}

func writeReverseCredentialsFile(path, did, identifier, host, passwordEnv string) error {
	payload := map[string]map[string]string{
		strings.TrimSpace(did): {
			"identifier":   strings.TrimSpace(identifier),
			"pds_host":     strings.TrimRight(strings.TrimSpace(host), "/"),
			"password_env": strings.TrimSpace(passwordEnv),
		},
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func stopCommand(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
}

func waitForSSBClientWhoami(ctx context.Context, t *testing.T, baseURL string, clientLogs *bytes.Buffer) string {
	t.Helper()

	type whoamiResponse struct {
		FeedID string `json:"feedId"`
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/whoami", nil)
		if err == nil {
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				var payload whoamiResponse
				decodeErr := json.NewDecoder(resp.Body).Decode(&payload)
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK && decodeErr == nil && strings.TrimSpace(payload.FeedID) != "" {
					return strings.TrimSpace(payload.FeedID)
				}
			}
		}

		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for ssb-client whoami\nclient logs:\n%s", summarizeLogs(clientLogs.String()))
		case <-ticker.C:
		}
	}
}

func createRoomInvite(ctx context.Context, t *testing.T, roomBaseURL string, bridgeLogs *bytes.Buffer) string {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, roomBaseURL+"/create-invite", nil)
	if err != nil {
		t.Fatalf("build room invite request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create room invite: %v\nbridge logs:\n%s", err, summarizeLogs(bridgeLogs.String()))
	}
	defer resp.Body.Close()

	var payload struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode room invite response: %v", err)
	}
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(payload.URL) == "" {
		t.Fatalf("unexpected room invite response: status=%d url=%q", resp.StatusCode, payload.URL)
	}
	return payload.URL
}

func joinSSBClientRoom(ctx context.Context, t *testing.T, baseURL, inviteURL string, clientLogs *bytes.Buffer) {
	t.Helper()

	form := url.Values{}
	form.Set("invite", inviteURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/room", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("build join room request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("join room request: %v\nclient logs:\n%s", err, summarizeLogs(clientLogs.String()))
	}
	defer resp.Body.Close()

	location := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unexpected join room status: %d location=%q\nclient logs:\n%s", resp.StatusCode, location, summarizeLogs(clientLogs.String()))
	}
	if strings.Contains(location, "error=") {
		t.Fatalf("join room redirected with error: %s\nclient logs:\n%s", location, summarizeLogs(clientLogs.String()))
	}
}

func waitForSSBClientPeers(ctx context.Context, t *testing.T, baseURL string, minCount int, clientLogs *bytes.Buffer) {
	t.Helper()

	type peersResponse struct {
		Count int `json:"count"`
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/peers", nil)
		if err == nil {
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				var payload peersResponse
				decodeErr := json.NewDecoder(resp.Body).Decode(&payload)
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK && decodeErr == nil && payload.Count >= minCount {
					return
				}
			}
		}

		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for ssb-client peers >= %d\nclient logs:\n%s", minCount, summarizeLogs(clientLogs.String()))
		case <-ticker.C:
		}
	}
}

func readSSBFeedID(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.ID), nil
}

func publishSSBClientJSON(ctx context.Context, t *testing.T, baseURL string, content map[string]any, clientLogs *bytes.Buffer) string {
	t.Helper()

	body, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal ssb-client publish body: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/publish", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build ssb-client publish request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("publish via ssb-client: %v\nclient logs:\n%s", err, summarizeLogs(clientLogs.String()))
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read ssb-client publish response: %v", err)
	}

	var payload struct {
		Key string `json:"key"`
	}
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &payload); err != nil {
			t.Fatalf("decode ssb-client publish response: %v\nbody:\n%s", err, strings.TrimSpace(string(bodyBytes)))
		}
	}
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(payload.Key) == "" {
		t.Fatalf(
			"unexpected ssb-client publish response: status=%d key=%q body=%q\nclient logs:\n%s",
			resp.StatusCode,
			payload.Key,
			strings.TrimSpace(string(bodyBytes)),
			summarizeLogs(clientLogs.String()),
		)
	}
	return strings.TrimSpace(payload.Key)
}

func waitForPublishedMessage(ctx context.Context, t *testing.T, database *db.DB, atURI string, bridgeLogs *bytes.Buffer) *db.Message {
	t.Helper()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		msg, err := database.GetMessage(ctx, atURI)
		if err != nil {
			t.Fatalf("get published message %s: %v", atURI, err)
		}
		if msg != nil && msg.MessageState == db.MessageStatePublished && strings.TrimSpace(msg.SSBMsgRef) != "" {
			return msg
		}

		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for published message %s\nbridge logs:\n%s", atURI, summarizeLogs(bridgeLogs.String()))
		case <-ticker.C:
		}
	}
}

func waitForReverseEventState(ctx context.Context, t *testing.T, database *db.DB, sourceRef, wantState string, bridgeLogs *bytes.Buffer) *db.ReverseEvent {
	t.Helper()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		event, err := database.GetReverseEvent(ctx, sourceRef)
		if err != nil {
			t.Fatalf("get reverse event %s: %v", sourceRef, err)
		}
		if event != nil {
			if event.EventState == wantState {
				return event
			}
			if event.EventState == db.ReverseEventStateFailed {
				t.Fatalf("reverse event %s failed: %#v\nbridge logs:\n%s", sourceRef, event, summarizeLogs(bridgeLogs.String()))
			}
		}

		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for reverse event %s state=%s\nbridge logs:\n%s", sourceRef, wantState, summarizeLogs(bridgeLogs.String()))
		case <-ticker.C:
		}
	}
}

func waitForATRecord(ctx context.Context, t *testing.T, xrpcc *xrpc.Client, atURI string, bridgeLogs *bytes.Buffer) *atproto.RepoGetRecord_Output {
	t.Helper()

	parsed, err := syntax.ParseATURI(atURI)
	if err != nil {
		t.Fatalf("parse at uri %s: %v", atURI, err)
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		record, err := atproto.RepoGetRecord(ctx, xrpcc, "", parsed.Collection().String(), parsed.Authority().String(), parsed.RecordKey().String())
		if err == nil && record != nil && record.Value != nil && record.Value.Val != nil {
			return record
		}

		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for atproto record %s\nbridge logs:\n%s", atURI, summarizeLogs(bridgeLogs.String()))
		case <-ticker.C:
		}
	}
}

func waitForATRecordDeleted(ctx context.Context, t *testing.T, xrpcc *xrpc.Client, atURI string, bridgeLogs *bytes.Buffer) {
	t.Helper()

	parsed, err := syntax.ParseATURI(atURI)
	if err != nil {
		t.Fatalf("parse at uri %s: %v", atURI, err)
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		record, err := atproto.RepoGetRecord(ctx, xrpcc, "", parsed.Collection().String(), parsed.Authority().String(), parsed.RecordKey().String())
		if err != nil || record == nil || record.Value == nil || record.Value.Val == nil {
			return
		}

		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for atproto record deletion %s\nbridge logs:\n%s", atURI, summarizeLogs(bridgeLogs.String()))
		case <-ticker.C:
		}
	}
}
