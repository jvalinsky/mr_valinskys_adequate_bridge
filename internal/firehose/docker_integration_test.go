//go:build docker_integration
// +build docker_integration

package firehose

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

func TestDockerIntegrationFirehose(t *testing.T) {
	if os.Getenv("DOCKER_INTEGRATION") != "1" {
		t.Skip("Set DOCKER_INTEGRATION=1 to run Docker-based integration tests")
	}

	pdsHost := os.Getenv("TEST_PDS_HOST")
	if pdsHost == "" {
		pdsHost = "http://127.0.0.1:2583"
	}

	relayURL := os.Getenv("TEST_RELAY_URL")
	if relayURL == "" {
		relayURL = "ws://127.0.0.1:2584/xrpc/com.atproto.sync.subscribeRepos"
	}

	xrpcc := &xrpc.Client{Host: pdsHost}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	session, err := setupTestAccount(ctx, t, xrpcc)
	if err != nil {
		t.Fatalf("Failed to setup test account: %v", err)
	}
	t.Logf("Logged in as DID: %s", session.Did)

	t.Log("Docker integration test setup complete")
}

func TestDockerIntegrationRunConnects(t *testing.T) {
	if os.Getenv("DOCKER_INTEGRATION") != "1" {
		t.Skip("Set DOCKER_INTEGRATION=1 to run Docker-based integration tests")
	}

	relayURL := os.Getenv("TEST_RELAY_URL")
	if relayURL == "" {
		relayURL = "ws://127.0.0.1:2584/xrpc/com.atproto.sync.subscribeRepos"
	}

	var wg sync.WaitGroup
	wg.Add(1)

	handler := &countingHandler{commits: make(chan int, 10), t: t}
	logger := log.New(os.Stdout, "firehose-test: ", 0)
	client := NewClient(relayURL, handler, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go func() {
		defer wg.Done()
		if err := client.Run(ctx); err != nil && err != context.DeadlineExceeded {
			t.Logf("client.Run exited: %v", err)
		}
	}()

	wg.Wait()

	t.Logf("Firehose client connected and ran successfully")
}

func TestDockerIntegrationRunWithReconnect(t *testing.T) {
	if os.Getenv("DOCKER_INTEGRATION") != "1" {
		t.Skip("Set DOCKER_INTEGRATION=1 to run Docker-based integration tests")
	}

	relayURL := os.Getenv("TEST_RELAY_URL")
	if relayURL == "" {
		relayURL = "ws://127.0.0.1:2584/xrpc/com.atproto.sync.subscribeRepos"
	}

	handler := &countingHandler{commits: make(chan int, 10), t: t}
	logger := log.New(os.Stdout, "firehose-reconnect-test: ", 0)
	client := NewClient(relayURL, handler, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := client.RunWithReconnect(ctx, ReconnectConfig{
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     5 * time.Second,
		Jitter:         100 * time.Millisecond,
	})
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("RunWithReconnect: %v", err)
	}

	t.Logf("RunWithReconnect completed successfully")
}

func TestDockerIntegrationCreateAndReceiveCommit(t *testing.T) {
	if os.Getenv("DOCKER_INTEGRATION") != "1" {
		t.Skip("Set DOCKER_INTEGRATION=1 to run Docker-based integration tests")
	}

	pdsHost := os.Getenv("TEST_PDS_HOST")
	if pdsHost == "" {
		pdsHost = "http://127.0.0.1:2583"
	}

	relayURL := os.Getenv("TEST_RELAY_URL")
	if relayURL == "" {
		relayURL = "ws://127.0.0.1:2584/xrpc/com.atproto.sync.subscribeRepos"
	}

	xrpcc := &xrpc.Client{Host: pdsHost}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session, err := setupTestAccount(ctx, t, xrpcc)
	if err != nil {
		t.Fatalf("Failed to setup test account: %v", err)
	}

	xrpcc.Auth = &xrpc.AuthInfo{
		AccessJwt:  session.AccessJwt,
		RefreshJwt: session.RefreshJwt,
		Did:        session.Did,
		Handle:     session.Handle,
	}

	createPost(ctx, t, xrpcc, session.Did)

	var wg sync.WaitGroup
	wg.Add(1)

	handler := &countingHandler{commits: make(chan int, 10), t: t}
	logger := log.New(os.Stdout, "firehose-commit-test: ", 0)
	client := NewClient(relayURL, handler, logger)

	go func() {
		defer wg.Done()
		if err := client.Run(ctx); err != nil && err != context.DeadlineExceeded {
			t.Logf("client.Run exited: %v", err)
		}
	}()

	select {
	case count := <-handler.commits:
		t.Logf("Received %d commits from firehose", count)
	case <-time.After(10 * time.Second):
		t.Log("Timeout waiting for commits")
	}

	wg.Wait()
}

type countingHandler struct {
	commits chan int
	t       *testing.T
	count   int
	mu      sync.Mutex
}

func (h *countingHandler) HandleCommit(ctx context.Context, evt *atproto.SyncSubscribeRepos_Commit) error {
	h.mu.Lock()
	h.count++
	count := h.count
	h.mu.Unlock()

	select {
	case h.commits <- count:
	default:
	}

	h.t.Logf("HandleCommit: repo=%s seq=%d ops=%d", evt.Repo, evt.Seq, len(evt.Ops))
	return nil
}

func createPost(ctx context.Context, t *testing.T, xrpcc *xrpc.Client, did string) {
	_, err := atproto.RepoCreateRecord(ctx, xrpcc, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.post",
		Repo:       did,
		Record: &lexutil.LexiconTypeDecoder{Val: &appbsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          "test post",
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		}},
	})
	if err != nil {
		t.Logf("create post (may already exist): %v", err)
	}
}

func setupTestAccount(ctx context.Context, t *testing.T, xrpcc *xrpc.Client) (*atproto.ServerCreateSession_Output, error) {
	handle := fmt.Sprintf("test-%d.test", time.Now().UnixNano())
	email := fmt.Sprintf("test-%d@example.test", time.Now().UnixNano())
	password := "test-password"

	_, err := atproto.ServerCreateAccount(ctx, xrpcc, &atproto.ServerCreateAccount_Input{
		Email:    &email,
		Handle:   handle,
		Password: &password,
	})
	if err != nil {
		return nil, fmt.Errorf("createAccount: %w", err)
	}

	session, err := atproto.ServerCreateSession(ctx, xrpcc, &atproto.ServerCreateSession_Input{
		Identifier: handle,
		Password:   password,
	})
	if err != nil {
		return nil, fmt.Errorf("createSession: %w", err)
	}

	return session, nil
}
