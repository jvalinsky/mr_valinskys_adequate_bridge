package livee2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/mr_valinskys_adequate_bridge/internal/db"
	cbg "github.com/whyrusleeping/cbor-gen"
)

func TestBridgeLiveInterop(t *testing.T) {
	if os.Getenv("LIVE_E2E_ENABLED") != "1" {
		t.Skip("set LIVE_E2E_ENABLED=1 to run live relay/room interoperability test")
	}

	host := strings.TrimSpace(getEnvDefault("LIVE_ATPROTO_HOST", "https://bsky.social"))
	identifier := requireEnv(t, "LIVE_ATPROTO_IDENTIFIER")
	password := requireEnv(t, "LIVE_ATPROTO_PASSWORD")
	targetDID := requireEnv(t, "LIVE_ATPROTO_FOLLOW_TARGET_DID")
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
	session, err := atproto.ServerCreateSession(ctx, xrpcc, &atproto.ServerCreateSession_Input{
		Identifier: identifier,
		Password:   password,
	})
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

	waitForBridgeStatus(ctx, t, dbPath, "live")

	postURI, postCID := createRecord(ctx, t, xrpcc, sourceDID, "app.bsky.feed.post", &appbsky.FeedPost{
		LexiconTypeID: "app.bsky.feed.post",
		Text:          fmt.Sprintf("bridge live e2e post %d", time.Now().UnixNano()),
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	})
	waitForPublishedRecords(ctx, t, dbPath, []string{postURI})

	likeURI, _ := createRecord(ctx, t, xrpcc, sourceDID, "app.bsky.feed.like", &appbsky.FeedLike{
		LexiconTypeID: "app.bsky.feed.like",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		Subject: &atproto.RepoStrongRef{
			Uri: postURI,
			Cid: postCID,
		},
	})
	repostURI, _ := createRecord(ctx, t, xrpcc, sourceDID, "app.bsky.feed.repost", &appbsky.FeedRepost{
		LexiconTypeID: "app.bsky.feed.repost",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		Subject: &atproto.RepoStrongRef{
			Uri: postURI,
			Cid: postCID,
		},
	})
	followURI, _ := createRecord(ctx, t, xrpcc, sourceDID, "app.bsky.graph.follow", &appbsky.GraphFollow{
		LexiconTypeID: "app.bsky.graph.follow",
		Subject:       targetDID,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	})

	expectedURIs := []string{postURI, likeURI, repostURI, followURI}
	waitForPublishedRecords(ctx, t, dbPath, expectedURIs)

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
	)
	verifyOut, err := verify.CombinedOutput()
	if err != nil {
		t.Fatalf("peer verification failed: %v\noutput:\n%s\nbridge logs:\n%s", err, string(verifyOut), summarizeLogs(bridgeLogs.String()))
	}
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

func waitForBridgeStatus(ctx context.Context, t *testing.T, dbPath, want string) {
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
			t.Fatalf("timed out waiting for bridge runtime status=%q", want)
		case <-ticker.C:
		}
	}
}

func waitForPublishedRecords(ctx context.Context, t *testing.T, dbPath string, atURIs []string) {
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
			t.Fatalf("timed out waiting for published records: %s", strings.Join(atURIs, ", "))
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
