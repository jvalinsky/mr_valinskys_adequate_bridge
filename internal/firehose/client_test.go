package firehose

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"sync/atomic"
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

func TestClientStreamURLWithCursor(t *testing.T) {
	handler := &mockHandler{}
	client := NewClient(
		"wss://example.com/xrpc/com.atproto.sync.subscribeRepos",
		handler,
		log.New(os.Stdout, "", 0),
		WithCursor(1234),
	)

	u, err := client.streamURL()
	if err != nil {
		t.Fatalf("streamURL: %v", err)
	}
	if !strings.Contains(u, "cursor=1234") {
		t.Fatalf("expected cursor query in URL, got %s", u)
	}
}

func TestIsFatalStreamError(t *testing.T) {
	cases := []struct {
		err   error
		fatal bool
	}{
		{err: errors.New("build stream URL: parse"), fatal: true},
		{err: errors.New("failed to dial (status=401): bad handshake"), fatal: true},
		{err: errors.New("failed to dial (status=403): bad handshake"), fatal: true},
		{err: errors.New("failed to dial (status=404): bad handshake"), fatal: true},
		{err: errors.New("unsupported protocol scheme wsx"), fatal: true},
		{err: errors.New("temporary network reset"), fatal: false},
		{err: context.Canceled, fatal: false},
	}

	for _, tc := range cases {
		if got := IsFatalStreamError(tc.err); got != tc.fatal {
			t.Fatalf("err=%v expected fatal=%v got=%v", tc.err, tc.fatal, got)
		}
	}
}

func TestRunWithReconnectLoopRetriesTransientAndSucceeds(t *testing.T) {
	var attempts atomic.Int32
	cfg := ReconnectConfig{
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		Jitter:         0,
	}

	err := runWithReconnectLoop(context.Background(), log.New(os.Stdout, "", 0), cfg, func(context.Context) error {
		n := attempts.Add(1)
		if n < 3 {
			return errors.New("temporary disconnect")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected eventual success, got %v", err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestRunWithReconnectLoopStopsOnFatal(t *testing.T) {
	var attempts atomic.Int32
	cfg := ReconnectConfig{
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		Jitter:         0,
	}

	err := runWithReconnectLoop(context.Background(), log.New(os.Stdout, "", 0), cfg, func(context.Context) error {
		attempts.Add(1)
		return errors.New("failed to dial (status=401): bad handshake")
	})
	if err == nil {
		t.Fatalf("expected fatal error")
	}
	if attempts.Load() != 1 {
		t.Fatalf("expected 1 attempt for fatal error, got %d", attempts.Load())
	}
}
