package firehose

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/lex/util"
	indigorepo "github.com/bluesky-social/indigo/repo"
	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"
	car "github.com/ipld/go-car"
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
	rr := &indigorepo.Repo{}
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
	rr := &indigorepo.Repo{}
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
	rr := indigorepo.NewRepo(ctx, did, bs)

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

	root, _, err := rr.Commit(ctx, func(context.Context, string, []byte) ([]byte, error) {
		return []byte("test-signature"), nil
	})
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	headerBuf := new(bytes.Buffer)
	if err := car.WriteHeader(&car.CarHeader{
		Roots:   []cid.Cid{root},
		Version: 1,
	}, headerBuf); err != nil {
		return nil, err
	}
	if _, err := buf.Write(headerBuf.Bytes()); err != nil {
		return nil, err
	}
	for _, blk := range bs.blocks {
		var total uint64
		cidBytes := blk.Cid().Bytes()
		rawData := blk.RawData()
		total = uint64(len(cidBytes) + len(rawData))

		var prefix [binary.MaxVarintLen64]byte
		prefixLen := binary.PutUvarint(prefix[:], total)
		if _, err := buf.Write(prefix[:prefixLen]); err != nil {
			return nil, err
		}
		if _, err := buf.Write(cidBytes); err != nil {
			return nil, err
		}
		if _, err := buf.Write(rawData); err != nil {
			return nil, err
		}
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

func ptrString(s string) *string {
	return &s
}

func ptrLexLink(s string) *util.LexLink {
	link, err := cid.Decode(s)
	if err != nil {
		return nil
	}
	l := util.LexLink(link)
	return &l
}
