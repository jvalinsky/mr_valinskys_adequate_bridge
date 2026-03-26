package firehose

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
)

type mockHandler struct {
	commits int
}

func (m *mockHandler) HandleCommit(ctx context.Context, evt *atproto.SyncSubscribeRepos_Commit) error {
	m.commits++
	return nil
}

func TestFirehoseClient(t *testing.T) {
	// Only run this test if explicitly requested, as it requires network access
	if os.Getenv("TEST_FIREHOSE") == "" {
		t.Skip("Skipping firehose test; set TEST_FIREHOSE=1 to run")
	}

	handler := &mockHandler{}
	logger := log.New(os.Stdout, "firehose-test: ", log.LstdFlags)
	client := NewClient("", handler, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Run(ctx)
	if err != nil && err != context.DeadlineExceeded && err.Error() != "con err at read: read tcp: use of closed network connection" {
		t.Logf("client.Run exited with: %v (expected on timeout)", err)
	}

	if handler.commits == 0 {
		t.Log("Warning: No commits received in 5 seconds")
	} else {
		t.Logf("Received %d commits", handler.commits)
	}
}
