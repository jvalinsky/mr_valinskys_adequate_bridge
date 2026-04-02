package atindex

import (
	"bytes"
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"
	"time"

	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/mapper"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
	appbsky "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/appbsky"
	lexutil "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/lexutil"
	atrepo "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/repo"
)

func TestTrackRepoDoesNotRequeueSyncedRepo(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))
	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:        "did:plc:steady",
		Tracking:   true,
		Reason:     "initial",
		SyncState:  StateSynced,
		Generation: 7,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	if err := service.TrackRepo(ctx, "did:plc:steady", "scheduler"); err != nil {
		t.Fatalf("track repo: %v", err)
	}

	repo, err := database.GetATProtoRepo(ctx, "did:plc:steady")
	if err != nil {
		t.Fatalf("reload repo: %v", err)
	}
	if repo.Generation != 7 {
		t.Fatalf("generation changed unexpectedly: got %d want 7", repo.Generation)
	}
	if repo.SyncState != StateSynced {
		t.Fatalf("sync state changed unexpectedly: got %q", repo.SyncState)
	}
	if repo.Reason != "scheduler" {
		t.Fatalf("reason not refreshed: got %q", repo.Reason)
	}
	if service.isQueued("did:plc:steady") {
		t.Fatalf("synced repo should not have been queued")
	}
}

func TestRequestResyncQueuesAndBumpsGeneration(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))
	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:        "did:plc:steady",
		Tracking:   true,
		Reason:     "initial",
		SyncState:  StateSynced,
		Generation: 7,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	if err := service.RequestResync(ctx, "did:plc:steady", "cli_backfill"); err != nil {
		t.Fatalf("request resync: %v", err)
	}

	repo, err := database.GetATProtoRepo(ctx, "did:plc:steady")
	if err != nil {
		t.Fatalf("reload repo: %v", err)
	}
	if repo.Generation != 8 {
		t.Fatalf("generation did not bump: got %d want 8", repo.Generation)
	}
	if repo.SyncState != StatePending {
		t.Fatalf("sync state not reset to pending: got %q", repo.SyncState)
	}
	if repo.Reason != "cli_backfill" {
		t.Fatalf("reason not updated: got %q", repo.Reason)
	}
	if !service.isQueued("did:plc:steady") {
		t.Fatalf("resync should queue the repo")
	}
}

func TestHandleCommitLeavesSourceCursorBehindOnApplyFailure(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))
	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:        "did:plc:broken",
		Tracking:   true,
		SyncState:  StateSynced,
		Generation: 1,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	err := service.HandleCommit(ctx, &atproto.SyncSubscribeRepos_Commit{
		Repo:   "did:plc:broken",
		Seq:    42,
		Rev:    "rev1",
		Blocks: []byte("not a car"),
	})
	if err == nil {
		t.Fatalf("expected commit apply failure")
	}

	source, err := database.GetATProtoSource(ctx, "default-relay")
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if source != nil && source.LastSeq != 0 {
		t.Fatalf("source cursor advanced on failed apply: %#v", source)
	}
}

func TestHandleCommitUpdatesSourceCursorWithoutWritingReplayCheckpoint(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))
	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:        "did:plc:alice",
		Tracking:   true,
		SyncState:  StateSynced,
		Generation: 1,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	evt, atURI := mustCreateCommitEvent(t, "did:plc:alice", 77)
	if err := service.HandleCommit(ctx, evt); err != nil {
		t.Fatalf("handle commit: %v", err)
	}

	source, err := database.GetATProtoSource(ctx, "default-relay")
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if source == nil || source.LastSeq != 77 {
		t.Fatalf("unexpected source cursor after successful commit: %#v", source)
	}

	replayCursor, ok, err := database.GetBridgeState(ctx, "atproto_event_cursor")
	if err != nil {
		t.Fatalf("get replay cursor: %v", err)
	}
	if ok || replayCursor != "" {
		t.Fatalf("producer should not write replay cursor, got ok=%v value=%q", ok, replayCursor)
	}

	record, err := database.GetATProtoRecord(ctx, atURI)
	if err != nil {
		t.Fatalf("get record: %v", err)
	}
	if record == nil {
		t.Fatalf("expected indexed record for %s", atURI)
	}

	events, err := database.ListATProtoEventsAfter(ctx, 0, 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func TestSubscribeSkipsBufferedDuplicateAfterReplay(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))
	cursor, err := database.AppendATProtoEvent(ctx, db.ATProtoRecordEvent{
		DID:        "did:plc:alice",
		Collection: mapper.RecordTypePost,
		RKey:       "post1",
		ATURI:      "at://did:plc:alice/app.bsky.feed.post/post1",
		ATCID:      "bafyrecord",
		Action:     "upsert",
		Rev:        "rev1",
		RecordJSON: `{"$type":"app.bsky.feed.post","text":"hello"}`,
	})
	if err != nil {
		t.Fatalf("append event: %v", err)
	}

	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := service.Subscribe(subCtx, 0)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	first := <-stream
	if first.Cursor != cursor {
		t.Fatalf("unexpected replay cursor: got %d want %d", first.Cursor, cursor)
	}

	service.broadcast(Notification{
		Kind:   EventKindRecord,
		Cursor: cursor,
		Record: &db.ATProtoRecordEvent{Cursor: cursor, ATURI: "at://did:plc:alice/app.bsky.feed.post/post1"},
	})

	select {
	case note := <-stream:
		t.Fatalf("received duplicate replay/live note: %#v", note)
	case <-time.After(100 * time.Millisecond):
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

func openServiceTestDB(t *testing.T) *db.DB {
	t.Helper()

	database, err := db.Open(filepath.Join(t.TempDir(), "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return database
}

func (s *Service) isQueued(did string) bool {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	_, ok := s.queued[did]
	return ok
}

func mustCreateCommitEvent(t *testing.T, did string, seq int64) (*atproto.SyncSubscribeRepos_Commit, string) {
	t.Helper()

	ctx := context.Background()
	repo := atrepo.NewRepo(did, newTestBlockstore())
	recordCID, path, err := repo.CreateRecord(ctx, mapper.RecordTypePost, &appbsky.FeedPost{
		LexiconTypeID: mapper.RecordTypePost,
		Text:          "hello from atindex test",
		CreatedAt:     "2026-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("create record: %v", err)
	}
	commitCID, rev, err := repo.Commit(ctx, func(context.Context, string, []byte) ([]byte, error) {
		return []byte("test-signature"), nil
	})
	if err != nil {
		t.Fatalf("commit repo: %v", err)
	}

	buf := new(bytes.Buffer)
	if err := repo.WriteCAR(buf); err != nil {
		t.Fatalf("write car: %v", err)
	}

	return &atproto.SyncSubscribeRepos_Commit{
		Repo:   did,
		Seq:    seq,
		Rev:    rev,
		Commit: lexutil.LexLink(commitCID),
		Ops: []*atproto.SyncSubscribeRepos_RepoOp{{
			Action: "create",
			Path:   path,
			Cid:    ptrLexLink(recordCID),
		}},
		Blocks: buf.Bytes(),
	}, "at://" + did + "/" + path
}

func ptrLexLink(value cid.Cid) *lexutil.LexLink {
	link := lexutil.LexLink(value)
	return &link
}
