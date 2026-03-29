//go:build docker_integration
// +build docker_integration

package firehose

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session, err := atproto.ServerCreateSession(ctx, xrpcc, &atproto.ServerCreateSession_Input{
		Identifier: "admin@test",
		Password:   "admin",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Logf("Logged in as DID: %s", session.Did)

	t.Log("Docker integration test setup complete")
}
