//go:build docker_integration
// +build docker_integration

package bridge

import (
	"context"
	"os"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
)

func TestDockerIntegrationBridge(t *testing.T) {
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

	ctx := context.Background()
	_ = ctx

	dbPath := t.TempDir() + "/bridge_test.sqlite"
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	t.Logf("Database opened at: %s", dbPath)
	t.Logf("PDS Host: %s", pdsHost)
	t.Logf("Relay URL: %s", relayURL)
	t.Log("Docker integration test setup complete")
}
