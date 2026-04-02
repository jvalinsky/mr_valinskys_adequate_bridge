package firehose

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/appbsky"
	atfirehose "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/firehose"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/lexutil"
	atrepo "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/repo"
)

func ptrInt64(v int64) *int64 {
	return &v
}

type mockHandler struct {
	commits int
}

func (m *mockHandler) HandleCommit(ctx context.Context, evt *atproto.SyncSubscribeRepos_Commit) error {
	m.commits++
	return nil
}

func TestWithConnectedCallback(t *testing.T) {
	called := false
	cb := func() { called = true }
	client := NewClient("", nil, nil, WithConnectedCallback(cb))
	if client.ConnectedCallback == nil {
		t.Fatal("ConnectedCallback not set")
	}
	client.ConnectedCallback()
	if !called {
		t.Fatal("callback not called")
	}
}

func TestFirehoseClient(t *testing.T) {
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

func TestClientStreamURLWithZeroCursor(t *testing.T) {
	handler := &mockHandler{}
	client := NewClient(
		"wss://example.com/xrpc/com.atproto.sync.subscribeRepos",
		handler,
		log.New(os.Stdout, "", 0),
		WithCursor(0),
	)

	u, err := client.streamURL()
	if err != nil {
		t.Fatalf("streamURL: %v", err)
	}
	if strings.Contains(u, "cursor=") {
		t.Fatalf("expected no cursor query for zero, got %s", u)
	}
}

func TestClientStreamURLWithoutCursor(t *testing.T) {
	handler := &mockHandler{}
	client := NewClient(
		"wss://example.com/xrpc/com.atproto.sync.subscribeRepos",
		handler,
		log.New(os.Stdout, "", 0),
	)

	u, err := client.streamURL()
	if err != nil {
		t.Fatalf("streamURL: %v", err)
	}
	if u != "wss://example.com/xrpc/com.atproto.sync.subscribeRepos" {
		t.Fatalf("expected original URL, got %s", u)
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
		{err: context.DeadlineExceeded, fatal: false},
		{err: io.EOF, fatal: false},
		{err: nil, fatal: false},
		{err: errors.New("malformed url detected"), fatal: true},
		{err: errors.New("build stream URL: invalid"), fatal: true},
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

func TestRunWithReconnectLoopContextCancel(t *testing.T) {
	cfg := ReconnectConfig{
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		Jitter:         0,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runWithReconnectLoop(ctx, log.New(os.Stdout, "", 0), cfg, func(context.Context) error {
		return errors.New("temporary error")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestJitterDuration(t *testing.T) {
	base := 500 * time.Millisecond
	jitter := 100 * time.Millisecond

	for i := 0; i < 10; i++ {
		d := jitterDuration(base, jitter)
		if d < 400*time.Millisecond || d > 600*time.Millisecond {
			t.Fatalf("jitter out of expected range: %v (base=%v, jitter=%v)", d, base, jitter)
		}
	}
}

func TestJitterDurationWithZeroJitter(t *testing.T) {
	d := jitterDuration(100*time.Millisecond, 0)
	if d != 100*time.Millisecond {
		t.Fatalf("expected 100ms, got %v", d)
	}
}

func TestJitterDurationWithZeroBase(t *testing.T) {
	d := jitterDuration(0, 50*time.Millisecond)
	if d < 1500*time.Millisecond {
		t.Fatalf("expected >=1.5s for zero base, got %v", d)
	}
}

func TestJitterDurationMinBound(t *testing.T) {
	d := jitterDuration(100*time.Millisecond, 200*time.Millisecond)
	if d < 250*time.Millisecond {
		t.Fatalf("expected >=250ms minimum, got %v", d)
	}
}

func TestNewClientWithEmptyURL(t *testing.T) {
	handler := &mockHandler{}
	client := NewClient("", handler, log.New(os.Stdout, "", 0))
	if client.relayURL != "wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos" {
		t.Fatalf("expected default URL, got %s", client.relayURL)
	}
}

func TestNewClientWithCustomURL(t *testing.T) {
	handler := &mockHandler{}
	customURL := "wss://custom.example.com/firehose"
	client := NewClient(customURL, handler, log.New(os.Stdout, "", 0))
	if client.relayURL != customURL {
		t.Fatalf("expected custom URL, got %s", client.relayURL)
	}
}

func TestStreamURLWithInvalidURL(t *testing.T) {
	handler := &mockHandler{}
	client := &Client{
		relayURL: "http://[ invalid",
		handler:  handler,
		logger:   log.New(os.Stdout, "", 0),
		cursor:   ptrInt64(123),
	}

	_, err := client.streamURL()
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestParseCommitWithNilBlocks(t *testing.T) {
	evt := &atproto.SyncSubscribeRepos_Commit{
		Blocks: nil,
	}
	_, err := ParseCommit(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for nil blocks")
	}
}

func TestProcessOpsWithSkipActions(t *testing.T) {
	rr := &atrepo.Repo{}
	evt := &atproto.SyncSubscribeRepos_Commit{
		Ops: []*atproto.SyncSubscribeRepos_RepoOp{
			{Action: "delete", Path: "/some/path"},
			{Action: "update", Cid: nil},
		},
	}

	err := ProcessOps(context.Background(), rr, evt)
	if err != nil {
		t.Fatalf("expected no error for skip actions, got %v", err)
	}
}

func TestProcessOpsWithEmptyOps(t *testing.T) {
	rr := &atrepo.Repo{}
	evt := &atproto.SyncSubscribeRepos_Commit{
		Ops: []*atproto.SyncSubscribeRepos_RepoOp{},
	}

	err := ProcessOps(context.Background(), rr, evt)
	if err != nil {
		t.Fatalf("expected no error for empty ops, got %v", err)
	}
}

func TestReconnectConfigDefaults(t *testing.T) {
	var attempts atomic.Int32
	cfg := ReconnectConfig{}

	err := runWithReconnectLoop(context.Background(), log.New(os.Stdout, "", 0), cfg, func(context.Context) error {
		n := attempts.Add(1)
		if n < 2 {
			return errors.New("temporary disconnect")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected eventual success, got %v", err)
	}
}

type testBlockstore struct {
	blocks map[string]blockformat.Block
}

func newTestBlockstore() *testBlockstore {
	return &testBlockstore{blocks: make(map[string]blockformat.Block)}
}

func (bs *testBlockstore) Put(_ context.Context, blk blockformat.Block) error {
	bs.blocks[blk.Cid().KeyString()] = blk
	return nil
}

func (bs *testBlockstore) Get(_ context.Context, c cid.Cid) (blockformat.Block, error) {
	blk, ok := bs.blocks[c.KeyString()]
	if !ok {
		return nil, &ipld.ErrNotFound{Cid: c}
	}
	return blk, nil
}

func createTestCAR(did string, records map[string]interface{}) ([]byte, error) {
	ctx := context.Background()
	bs := newTestBlockstore()
	rr := atrepo.NewRepo(did, bs)

	for path, record := range records {
		parts := strings.SplitN(path, "/", 2)
		if len(parts) != 2 {
			continue
		}
		collection := parts[0]
		var err error
		switch r := record.(type) {
		case *appbsky.FeedPost:
			_, _, err = rr.CreateRecord(ctx, collection, r)
		default:
			continue
		}
		if err != nil {
			return nil, err
		}
	}

	_, _, err := rr.Commit(ctx, func(context.Context, string, []byte) ([]byte, error) {
		return []byte("test-signature"), nil
	})
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	if err := rr.WriteCAR(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func TestParseCommitWithValidCAR(t *testing.T) {
	records := map[string]interface{}{
		"app.bsky.feed.post/1": &appbsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          "test post",
			CreatedAt:     "2026-01-01T00:00:00Z",
		},
	}
	carData, err := createTestCAR("did:plc:test", records)
	if err != nil {
		t.Fatalf("create test CAR: %v", err)
	}

	evt := &atproto.SyncSubscribeRepos_Commit{
		Blocks: carData,
	}

	rr, err := ParseCommit(context.Background(), evt)
	if err != nil {
		t.Fatalf("ParseCommit: %v", err)
	}
	if rr == nil {
		t.Fatal("expected non-nil repo")
	}
}

func TestParseCommitWithInvalidCAR(t *testing.T) {
	evt := &atproto.SyncSubscribeRepos_Commit{
		Blocks: []byte("not valid car data"),
	}

	_, err := ParseCommit(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for invalid CAR data")
	}
}

func TestProcessOpsWithCreateAction(t *testing.T) {
	records := map[string]interface{}{
		"app.bsky.feed.post/test1": &appbsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          "test post 1",
			CreatedAt:     "2026-01-01T00:00:00Z",
		},
	}
	carData, err := createTestCAR("did:plc:test", records)
	if err != nil {
		t.Fatalf("create test CAR: %v", err)
	}

	rr, err := ParseCommit(context.Background(), &atproto.SyncSubscribeRepos_Commit{
		Blocks: carData,
	})
	if err != nil {
		t.Fatalf("ParseCommit: %v", err)
	}

	evt := &atproto.SyncSubscribeRepos_Commit{
		Ops: []*atproto.SyncSubscribeRepos_RepoOp{
			{
				Action: "create",
				Path:   "app.bsky.feed.post/test1",
				Cid:    ptrLexLink("bafytest"),
			},
		},
	}

	err = ProcessOps(context.Background(), rr, evt)
	if err != nil {
		t.Fatalf("ProcessOps: %v", err)
	}
}

func TestProcessOpsWithDeleteAction(t *testing.T) {
	records := map[string]interface{}{
		"app.bsky.feed.post/del1": &appbsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          "post to delete",
			CreatedAt:     "2026-01-01T00:00:00Z",
		},
	}
	carData, err := createTestCAR("did:plc:test", records)
	if err != nil {
		t.Fatalf("create test CAR: %v", err)
	}

	rr, err := ParseCommit(context.Background(), &atproto.SyncSubscribeRepos_Commit{
		Blocks: carData,
	})
	if err != nil {
		t.Fatalf("ParseCommit: %v", err)
	}

	evt := &atproto.SyncSubscribeRepos_Commit{
		Ops: []*atproto.SyncSubscribeRepos_RepoOp{
			{
				Action: "delete",
				Path:   "app.bsky.feed.post/del1",
				Cid:    nil,
			},
		},
	}

	err = ProcessOps(context.Background(), rr, evt)
	if err != nil {
		t.Fatalf("ProcessOps with delete: %v", err)
	}
}

func TestProcessOpsSkipsDelete(t *testing.T) {
	records := map[string]interface{}{
		"app.bsky.feed.post/del1": &appbsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          "post to delete",
			CreatedAt:     "2026-01-01T00:00:00Z",
		},
	}
	carData, err := createTestCAR("did:plc:test", records)
	if err != nil {
		t.Fatalf("create test CAR: %v", err)
	}

	rr, err := ParseCommit(context.Background(), &atproto.SyncSubscribeRepos_Commit{
		Blocks: carData,
	})
	if err != nil {
		t.Fatalf("ParseCommit: %v", err)
	}

	evt := &atproto.SyncSubscribeRepos_Commit{
		Ops: []*atproto.SyncSubscribeRepos_RepoOp{
			{
				Action: "delete",
				Path:   "app.bsky.feed.post/del1",
				Cid:    nil,
			},
		},
	}

	err = ProcessOps(context.Background(), rr, evt)
	if err != nil {
		t.Fatalf("ProcessOps with delete: %v", err)
	}
}

func TestProcessOpsWithUnknownAction(t *testing.T) {
	records := map[string]interface{}{
		"app.bsky.feed.post/test1": &appbsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          "Test post",
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		},
	}
	carData, err := createTestCAR("did:plc:test", records)
	if err != nil {
		t.Fatalf("create test CAR: %v", err)
	}

	rr, err := ParseCommit(context.Background(), &atproto.SyncSubscribeRepos_Commit{
		Blocks: carData,
	})
	if err != nil {
		t.Fatalf("ParseCommit: %v", err)
	}

	evt := &atproto.SyncSubscribeRepos_Commit{
		Ops: []*atproto.SyncSubscribeRepos_RepoOp{
			{
				Action: "tombstone",
				Path:   "app.bsky.feed.post/test1",
				Cid:    ptrLexLink("bafytest"),
			},
		},
	}

	err = ProcessOps(context.Background(), rr, evt)
	if err != nil {
		t.Fatalf("ProcessOps with unknown action: %v", err)
	}
}

func ptrLexLink(s string) *lexutil.LexLink {
	link, err := cid.Decode(s)
	if err != nil {
		return nil
	}
	l := lexutil.LexLink(link)
	return &l
}

func TestIsFatalStreamErrorMore(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("unsupported protocol scheme"), true},
		{fmt.Errorf("malformed URL"), true},
		{fmt.Errorf("build stream url: boom"), true},
		{fmt.Errorf("failed to dial (status=401): unauthorized"), true},
		{fmt.Errorf("failed to dial (status=403): forbidden"), true},
		{fmt.Errorf("failed to dial (status=404): not found"), true},
		{fmt.Errorf("some other error"), false},
		{nil, false},
		{context.Canceled, false},
		{io.EOF, false},
	}
	for _, tt := range tests {
		if got := IsFatalStreamError(tt.err); got != tt.want {
			t.Errorf("IsFatalStreamError(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestJitterDurationMore(t *testing.T) {
	// min bound test
	d := jitterDuration(100*time.Millisecond, 500*time.Millisecond)
	if d < 250*time.Millisecond {
		t.Errorf("expected at least 250ms, got %v", d)
	}
}

func TestStreamURLMore(t *testing.T) {
	c := NewClient("wss://example.com", nil, nil)
	url, _ := c.streamURL()
	if url != "wss://example.com" {
		t.Errorf("expected wss://example.com, got %q", url)
	}

	c2 := NewClient("wss://example.com", nil, nil, WithCursor(123))
	url2, _ := c2.streamURL()
	if !strings.Contains(url2, "cursor=123") {
		t.Errorf("expected cursor in URL, got %q", url2)
	}
}

func TestClientRunDialError(t *testing.T) {
	c := NewClient("invalid-url", nil, nil)
	err := c.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestProcessOpsEdgeCases(t *testing.T) {
	// 1. Missing CID for create/update should be skipped
	evt := &atproto.SyncSubscribeRepos_Commit{
		Ops: []*atproto.SyncSubscribeRepos_RepoOp{
			{Action: "create", Path: "app.bsky.feed.post/1", Cid: nil},
		},
	}
	err := ProcessOps(context.Background(), nil, evt)
	if err != nil {
		t.Errorf("expected no error for nil CID, got %v", err)
	}

	// 2. Fetch error during process should return error
	records := map[string]interface{}{
		"app.bsky.feed.post/err": &appbsky.FeedPost{Text: "fail"},
	}
	carData, _ := createTestCAR("did:plc:test", records)
	rr, _ := ParseCommit(context.Background(), &atproto.SyncSubscribeRepos_Commit{Blocks: carData})

	evt2 := &atproto.SyncSubscribeRepos_Commit{
		Ops: []*atproto.SyncSubscribeRepos_RepoOp{
			{Action: "create", Path: "app.bsky.feed.post/nonexistent", Cid: ptrLexLink("bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku")},
		},
	}
	err = ProcessOps(context.Background(), rr, evt2)
	if err == nil || !strings.Contains(err.Error(), "getting record") {
		t.Errorf("expected fetch error, got %v", err)
	}
}

type failingHandler struct {
	err error
}

func (h *failingHandler) HandleCommit(ctx context.Context, evt *atproto.SyncSubscribeRepos_Commit) error {
	return h.err
}

func TestClientRunDialErrorWithResponse(t *testing.T) {
	// Server that returns 403 Forbidden
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	u.Scheme = "ws" // Dial will fail because it's not a real websocket server upgrade

	c := NewClient(u.String(), nil, nil)
	err := c.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status=403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}

func TestClientCallbacksCoverage(t *testing.T) {
	c := NewClient("", &mockHandler{}, log.New(io.Discard, "", 0))

	t.Run("handleRepoCommit", func(t *testing.T) {
		err := c.handleRepoCommit(&atproto.SyncSubscribeRepos_Commit{Repo: "did:plc:x"})
		if err != nil {
			t.Error(err)
		}
	})

	t.Run("handleRepoCommitError", func(t *testing.T) {
		c2 := NewClient("", &failingHandler{err: fmt.Errorf("fail")}, log.New(io.Discard, "", 0))
		err := c2.handleRepoCommit(&atproto.SyncSubscribeRepos_Commit{Repo: "did:plc:x"})
		if err != nil {
			t.Error(err)
		}
	})

	t.Run("handleRepoInfo", func(t *testing.T) {
		err := c.handleRepoInfo(&atproto.SyncSubscribeRepos_Info{Name: "info"})
		if err != nil {
			t.Error(err)
		}
	})

	t.Run("handleError", func(t *testing.T) {
		msg := "err"
		err := c.handleError(&atfirehose.ErrorFrame{Message: &msg})
		if err != nil {
			t.Error(err)
		}
	})
}

func TestClientConnectedCallbackInvoked(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	u.Scheme = "ws"

	connected := make(chan struct{})
	cb := func() { close(connected) }
	client := NewClient(u.String(), &mockHandler{}, log.New(io.Discard, "", 0), WithConnectedCallback(cb))

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	go func() {
		_ = client.Run(ctx)
	}()

	select {
	case <-connected:
		// Success
	case <-ctx.Done():
		t.Fatal("timed out waiting for ConnectedCallback")
	}
}

func TestClientRunSuccess(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Send a dummy RepoInfo message (using a raw slice for now as we just need to exercise the loop)
		// Real ATProto messages are CBOR-encoded frames.
		// For the purpose of testing the Run loop exit, we can just keep the connection open.

		time.Sleep(500 * time.Millisecond)
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	u.Scheme = "ws"

	handler := &mockHandler{}
	client := NewClient(u.String(), handler, log.New(io.Discard, "", 0))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := client.Run(ctx)
	// We expect context.DeadlineExceeded because the mock server just hangs.
	// We also tolerate connection close errors during shutdown.
	if err != nil &&
		!errors.Is(err, context.DeadlineExceeded) &&
		!strings.Contains(err.Error(), "context deadline exceeded") &&
		!strings.Contains(err.Error(), "use of closed network connection") {
		t.Fatalf("Run exited with unexpected error: %v", err)
	}
}

func TestRunWithReconnectSuccessFirstTry(t *testing.T) {
	cfg := ReconnectConfig{}
	err := runWithReconnectLoop(context.Background(), log.New(io.Discard, "", 0), cfg, func(context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestRunWithReconnectJitterDefault(t *testing.T) {
	c := NewClient("", nil, nil)
	// We can't easily check the internal state, but we can call it
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.RunWithReconnect(ctx, ReconnectConfig{InitialBackoff: 1 * time.Millisecond})
}

func TestClientRunWithReconnectFatal(t *testing.T) {
	// A 401 error is fatal and should stop the loop immediately
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	u.Scheme = "ws"

	handler := &mockHandler{}
	client := NewClient(u.String(), handler, log.New(io.Discard, "", 0))

	err := client.RunWithReconnect(context.Background(), ReconnectConfig{
		InitialBackoff: 1 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected fatal error for 401 status")
	}
	if !strings.Contains(err.Error(), "status=401") {
		t.Fatalf("unexpected error: %v", err)
	}
}
