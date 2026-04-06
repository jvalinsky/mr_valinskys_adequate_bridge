package atindex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

func TestSubscribeEvictsSlowConsumer(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := service.Subscribe(subCtx, 0)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	for i := 0; i < 2048; i++ {
		cursor := int64(i + 1)
		service.broadcast(Notification{
			Kind:   EventKindRecord,
			Cursor: cursor,
			Record: &db.ATProtoRecordEvent{
				Cursor: cursor,
				ATURI:  "at://did:plc:alice/app.bsky.feed.post/post1",
			},
		})
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-stream:
			if !ok {
				if service.subscriberCount() != 0 {
					t.Fatalf("expected subscriber to be removed after eviction, got %d", service.subscriberCount())
				}
				return
			}
		case <-deadline:
			t.Fatalf("subscription did not close after slow-consumer overflow")
		}
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

func (s *Service) subscriberCount() int {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	return len(s.subscribers)
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

// ---------- Phase A: Simple passthroughs ----------

func TestGetRepoInfoExists(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))

	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:       "did:plc:alice",
		Tracking:  true,
		SyncState: StateSynced,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	info, err := service.GetRepoInfo(ctx, "did:plc:alice")
	if err != nil {
		t.Fatalf("GetRepoInfo: %v", err)
	}
	if info == nil {
		t.Fatal("expected repo info, got nil")
	}
	if info.DID != "did:plc:alice" {
		t.Errorf("expected did:plc:alice, got %q", info.DID)
	}
}

func TestGetRepoInfoNotFound(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))

	info, err := service.GetRepoInfo(ctx, "did:plc:nobody")
	if err != nil {
		t.Fatalf("GetRepoInfo: %v", err)
	}
	if info != nil {
		t.Error("expected nil for nonexistent repo")
	}
}

func TestGetRecordExists(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))

	if err := database.UpsertATProtoRecord(ctx, db.ATProtoRecord{
		DID:        "did:plc:alice",
		Collection: "app.bsky.feed.post",
		RKey:       "post1",
		ATURI:      "at://did:plc:alice/app.bsky.feed.post/post1",
		ATCID:      "bafy-post1",
		RecordJSON: `{"text":"hello"}`,
	}); err != nil {
		t.Fatalf("seed record: %v", err)
	}

	record, err := service.GetRecord(ctx, "at://did:plc:alice/app.bsky.feed.post/post1")
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if record == nil {
		t.Fatal("expected record, got nil")
	}
	if record.ATCID != "bafy-post1" {
		t.Errorf("expected bafy-post1, got %q", record.ATCID)
	}
}

func TestGetRecordNotFound(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))

	record, err := service.GetRecord(ctx, "at://nonexistent")
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if record != nil {
		t.Error("expected nil for nonexistent record")
	}
}

func TestListRecordsBasic(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))

	if err := database.UpsertATProtoRecord(ctx, db.ATProtoRecord{
		DID:        "did:plc:alice",
		Collection: "app.bsky.feed.post",
		RKey:       "post1",
		ATURI:      "at://did:plc:alice/app.bsky.feed.post/post1",
		ATCID:      "bafy-post1",
		RecordJSON: `{"text":"hello"}`,
	}); err != nil {
		t.Fatalf("seed record: %v", err)
	}

	records, err := service.ListRecords(ctx, "did:plc:alice", "app.bsky.feed.post", "", 10)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
}

func TestUntrackRepo(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))

	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:       "did:plc:alice",
		Tracking:  true,
		SyncState: StateSynced,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	if err := service.UntrackRepo(ctx, "did:plc:alice"); err != nil {
		t.Fatalf("UntrackRepo: %v", err)
	}

	repo, err := database.GetATProtoRepo(ctx, "did:plc:alice")
	if err != nil {
		t.Fatalf("GetATProtoRepo: %v", err)
	}
	if repo == nil {
		t.Fatal("expected repo, got nil")
	}
	if repo.Tracking {
		t.Error("expected tracking=false after untrack")
	}
}

func TestUntrackRepoIdempotent(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))

	// Untrack a repo that doesn't exist -> nil error.
	if err := service.UntrackRepo(ctx, "did:plc:nobody"); err != nil {
		t.Fatalf("UntrackRepo nonexistent: %v", err)
	}
}

// ---------- Phase B: State machine transitions ----------

func TestFailRepo(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))

	repoInfo := &db.ATProtoRepo{
		DID:       "did:plc:fail",
		Tracking:  true,
		SyncState: StateBackfilling,
	}

	err := service.failRepo(ctx, repoInfo, fmt.Errorf("backfill error"))
	if err == nil {
		t.Fatal("expected error from failRepo")
	}
	if repoInfo.SyncState != StateError {
		t.Errorf("expected state error, got %q", repoInfo.SyncState)
	}
	if repoInfo.LastError != "backfill error" {
		t.Errorf("expected last_error, got %q", repoInfo.LastError)
	}

	// Verify persisted.
	repo, err := database.GetATProtoRepo(ctx, "did:plc:fail")
	if err != nil {
		t.Fatalf("GetATProtoRepo: %v", err)
	}
	if repo == nil || repo.SyncState != StateError {
		t.Errorf("expected persisted error state, got %+v", repo)
	}
}

func TestMarkDesynchronized(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))

	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:        "did:plc:desync",
		Tracking:   true,
		SyncState:  StateSynced,
		Generation: 5,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	repoInfo, err := database.GetATProtoRepo(ctx, "did:plc:desync")
	if err != nil {
		t.Fatalf("GetATProtoRepo: %v", err)
	}

	evt := &atproto.SyncSubscribeRepos_Commit{
		Repo:   "did:plc:desync",
		Seq:    100,
		Rev:    "rev-bad",
		Commit: lexutil.LexLink(cid.MustParse("bafkreifzjut3te2nhyekklss27nh3k72ysco7y32koao5eei66rxofr6ii")),
		Blocks: []byte("not a car"),
	}

	if err := service.markDesynchronized(ctx, repoInfo, evt, "continuity break"); err != nil {
		t.Fatalf("markDesynchronized: %v", err)
	}

	if repoInfo.SyncState != StateDesynchronized {
		t.Errorf("expected desynchronized state, got %q", repoInfo.SyncState)
	}
	if repoInfo.Generation != 6 {
		t.Errorf("expected generation 6, got %d", repoInfo.Generation)
	}
	if repoInfo.LastError != "continuity break" {
		t.Errorf("expected last_error, got %q", repoInfo.LastError)
	}

	// Verify buffer item was created.
	items, err := database.ListATProtoCommitBufferItems(ctx, "did:plc:desync", 6)
	if err != nil {
		t.Fatalf("ListATProtoCommitBufferItems: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 buffered commit, got %d", len(items))
	}

	// Verify DID was queued.
	if !service.isQueued("did:plc:desync") {
		t.Error("expected DID to be queued after desync")
	}
}

func TestHandleAccountActive(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))

	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:       "did:plc:active",
		Tracking:  true,
		SyncState: "",
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	evt := &atproto.SyncSubscribeRepos_Account{
		Did:    "did:plc:active",
		Active: true,
		Seq:    50,
		Time:   "2026-01-01T00:00:00Z",
	}

	if err := service.HandleAccount(ctx, evt); err != nil {
		t.Fatalf("HandleAccount: %v", err)
	}

	repo, err := database.GetATProtoRepo(ctx, "did:plc:active")
	if err != nil {
		t.Fatalf("GetATProtoRepo: %v", err)
	}
	if repo == nil {
		t.Fatal("expected repo")
	}
	if repo.AccountActive == nil || *repo.AccountActive != true {
		t.Error("expected account_active=true")
	}
	if repo.SyncState != StatePending {
		t.Errorf("expected sync_state=pending for active account, got %q", repo.SyncState)
	}
}

func TestHandleAccountDeactivated(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))

	status := "deactivated"
	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:       "did:plc:deact",
		Tracking:  true,
		SyncState: StateSynced,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	evt := &atproto.SyncSubscribeRepos_Account{
		Did:    "did:plc:deact",
		Active: false,
		Status: &status,
		Seq:    51,
		Time:   "2026-01-01T00:00:00Z",
	}

	if err := service.HandleAccount(ctx, evt); err != nil {
		t.Fatalf("HandleAccount: %v", err)
	}

	repo, err := database.GetATProtoRepo(ctx, "did:plc:deact")
	if err != nil {
		t.Fatalf("GetATProtoRepo: %v", err)
	}
	if repo == nil {
		t.Fatal("expected repo")
	}
	if repo.SyncState != StateDeactivated {
		t.Errorf("expected sync_state=deactivated, got %q", repo.SyncState)
	}
}

func TestHandleIdentityWithHandle(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))

	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:       "did:plc:identity",
		Tracking:  true,
		SyncState: StateSynced,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	handle := "alice.test"
	evt := &atproto.SyncSubscribeRepos_Identity{
		Did:    "did:plc:identity",
		Handle: &handle,
		Seq:    60,
		Time:   "2026-01-01T00:00:00Z",
	}

	if err := service.HandleIdentity(ctx, evt); err != nil {
		t.Fatalf("HandleIdentity: %v", err)
	}

	repo, err := database.GetATProtoRepo(ctx, "did:plc:identity")
	if err != nil {
		t.Fatalf("GetATProtoRepo: %v", err)
	}
	if repo == nil {
		t.Fatal("expected repo")
	}
	if repo.Handle != "alice.test" {
		t.Errorf("expected handle alice.test, got %q", repo.Handle)
	}
}

func TestHandleIdentityNoResolver(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	// No resolver configured -> should not panic, skips PDS resolution.
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))

	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:       "did:plc:noresolver",
		Tracking:  true,
		SyncState: StateSynced,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	evt := &atproto.SyncSubscribeRepos_Identity{
		Did:  "did:plc:noresolver",
		Seq:  61,
		Time: "2026-01-01T00:00:00Z",
	}

	if err := service.HandleIdentity(ctx, evt); err != nil {
		t.Fatalf("HandleIdentity no resolver: %v", err)
	}
}

// ---------- Phase C: Buffer/drain cycle ----------

func TestBufferCommit(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))

	evt := &atproto.SyncSubscribeRepos_Commit{
		Repo:   "did:plc:buffer",
		Seq:    70,
		Rev:    "rev1",
		Commit: lexutil.LexLink(cid.MustParse("bafkreifzjut3te2nhyekklss27nh3k72ysco7y32koao5eei66rxofr6ii")),
		Blocks: []byte("not a car"),
	}

	if err := service.bufferCommit(ctx, evt, 3); err != nil {
		t.Fatalf("bufferCommit: %v", err)
	}

	items, err := database.ListATProtoCommitBufferItems(ctx, "did:plc:buffer", 3)
	if err != nil {
		t.Fatalf("ListATProtoCommitBufferItems: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 buffered item, got %d", len(items))
	}
	if items[0].Rev != "rev1" {
		t.Errorf("expected rev1, got %q", items[0].Rev)
	}

	// Verify DID was queued.
	if !service.isQueued("did:plc:buffer") {
		t.Error("expected DID to be queued after buffering")
	}
}

func TestDrainBufferedCommitsEmpty(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))

	repoInfo := &db.ATProtoRepo{
		DID:        "did:plc:drain",
		Generation: 1,
	}

	if err := service.drainBufferedCommits(ctx, repoInfo); err != nil {
		t.Fatalf("drainBufferedCommits empty: %v", err)
	}
}

func TestDrainBufferedCommitsApplies(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, nil, nil, "wss://relay.example.test", log.New(io.Discard, "", 0))

	// First, seed a tracked repo.
	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:        "did:plc:drain",
		Tracking:   true,
		SyncState:  StateBackfilling,
		Generation: 1,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	// Create a valid commit event.
	evt, _ := mustCreateCommitEvent(t, "did:plc:drain", 80)
	raw, _ := json.Marshal(evt)

	// Buffer it.
	if err := database.AddATProtoCommitBufferItem(ctx, db.ATProtoCommitBufferItem{
		DID:          "did:plc:drain",
		Generation:   1,
		Rev:          evt.Rev,
		Seq:          evt.Seq,
		RawEventJSON: string(raw),
	}); err != nil {
		t.Fatalf("AddATProtoCommitBufferItem: %v", err)
	}

	repoInfo, err := database.GetATProtoRepo(ctx, "did:plc:drain")
	if err != nil {
		t.Fatalf("GetATProtoRepo: %v", err)
	}

	if err := service.drainBufferedCommits(ctx, repoInfo); err != nil {
		t.Fatalf("drainBufferedCommits: %v", err)
	}

	// Verify the record was applied.
	records, err := database.ListATProtoRecords(ctx, "did:plc:drain", "app.bsky.feed.post", "", 10)
	if err != nil {
		t.Fatalf("ListATProtoRecords: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record after drain, got %d", len(records))
	}

	// Verify buffer was cleared.
	items, err := database.ListATProtoCommitBufferItems(ctx, "did:plc:drain", 1)
	if err != nil {
		t.Fatalf("ListATProtoCommitBufferItems: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 buffered items after drain, got %d", len(items))
	}
}

// ---------- Phase D: runBackfill with mocks ----------

type mockHostResolver struct {
	endpoint string
	err      error
	didErr   map[string]error
}

func (m *mockHostResolver) ResolvePDSEndpoint(ctx context.Context, did string) (string, error) {
	if err, ok := m.didErr[did]; ok {
		return "", err
	}
	return m.endpoint, m.err
}

type mockRepoFetcher struct {
	data   []byte
	err    error
	didErr map[string]error
}

func (m *mockRepoFetcher) FetchRepo(ctx context.Context, endpoint, did string) ([]byte, error) {
	if err, ok := m.didErr[did]; ok {
		return nil, err
	}
	return m.data, m.err
}

func TestRunBackfillUntrackedRepo(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	service := New(database, &mockHostResolver{}, &mockRepoFetcher{}, "wss://relay.example.test", log.New(io.Discard, "", 0))

	// DID not in DB -> returns error (GetATProtoRepo returns nil).
	err := service.runBackfill(ctx, "did:plc:notracked")
	if err != nil {
		// GetATProtoRepo returns (nil, nil) for not found, so runBackfill returns nil.
		t.Logf("runBackfill returned: %v", err)
	}
}

func TestRunBackfillResolverError(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	resolver := &mockHostResolver{err: fmt.Errorf("resolve failed")}
	service := New(database, resolver, &mockRepoFetcher{}, "wss://relay.example.test", log.New(io.Discard, "", 0))

	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:       "did:plc:resolvererr",
		Tracking:  true,
		SyncState: StatePending,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	err := service.runBackfill(ctx, "did:plc:resolvererr")
	if err == nil {
		t.Fatal("expected error from resolver failure")
	}

	repo, _ := database.GetATProtoRepo(ctx, "did:plc:resolvererr")
	if repo == nil || repo.SyncState != StateError {
		t.Errorf("expected error state after resolver failure, got %+v", repo)
	}
}

func TestRunBackfillFetcherError(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()
	resolver := &mockHostResolver{endpoint: "https://pds.example.test"}
	fetcher := &mockRepoFetcher{err: fmt.Errorf("fetch failed")}
	service := New(database, resolver, fetcher, "wss://relay.example.test", log.New(io.Discard, "", 0))

	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:       "did:plc:fetcherr",
		Tracking:  true,
		SyncState: StatePending,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	err := service.runBackfill(ctx, "did:plc:fetcherr")
	if err == nil {
		t.Fatal("expected error from fetcher failure")
	}

	repo, _ := database.GetATProtoRepo(ctx, "did:plc:fetcherr")
	if repo == nil || repo.SyncState != StateError {
		t.Errorf("expected error state after fetcher failure, got %+v", repo)
	}
}

func TestRunBackfillSuccess(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()

	// Create a valid CAR.
	repo := atrepo.NewRepo("did:plc:backfill", newTestBlockstore())
	recordCID, path, err := repo.CreateRecord(ctx, mapper.RecordTypePost, &appbsky.FeedPost{
		LexiconTypeID: mapper.RecordTypePost,
		Text:          "backfill post",
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
	_ = commitCID // used internally

	buf := new(bytes.Buffer)
	if err := repo.WriteCAR(buf); err != nil {
		t.Fatalf("write car: %v", err)
	}

	resolver := &mockHostResolver{endpoint: "https://pds.example.test"}
	fetcher := &mockRepoFetcher{data: buf.Bytes()}
	service := New(database, resolver, fetcher, "wss://relay.example.test", log.New(io.Discard, "", 0))

	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:        "did:plc:backfill",
		Tracking:   true,
		SyncState:  StatePending,
		Generation: 1,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	if err := service.runBackfill(ctx, "did:plc:backfill"); err != nil {
		t.Fatalf("runBackfill: %v", err)
	}

	repoInfo, err := database.GetATProtoRepo(ctx, "did:plc:backfill")
	if err != nil {
		t.Fatalf("GetATProtoRepo: %v", err)
	}
	if repoInfo == nil {
		t.Fatal("expected repo info")
	}
	if repoInfo.SyncState != StateSynced {
		t.Errorf("expected synced state, got %q", repoInfo.SyncState)
	}
	if repoInfo.CurrentRev != rev {
		t.Errorf("expected rev %q, got %q", rev, repoInfo.CurrentRev)
	}

	// Verify record was stored.
	record, err := database.GetATProtoRecord(ctx, "at://did:plc:backfill/"+path)
	if err != nil {
		t.Fatalf("GetATProtoRecord: %v", err)
	}
	if record == nil {
		t.Fatal("expected record after backfill")
	}
	if record.ATCID != recordCID.String() {
		t.Errorf("expected at_cid %q, got %q", recordCID.String(), record.ATCID)
	}
}

// ---------- Phase E: Start/runWorker ----------

func TestStartProcessesQueue(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()

	// Create a valid CAR for backfill.
	repo := atrepo.NewRepo("did:plc:worker", newTestBlockstore())
	_, _, err := repo.CreateRecord(ctx, mapper.RecordTypePost, &appbsky.FeedPost{
		LexiconTypeID: mapper.RecordTypePost,
		Text:          "worker post",
		CreatedAt:     "2026-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("create record: %v", err)
	}
	_, _, err = repo.Commit(ctx, func(context.Context, string, []byte) ([]byte, error) {
		return []byte("test-signature"), nil
	})
	if err != nil {
		t.Fatalf("commit repo: %v", err)
	}

	carBuf := new(bytes.Buffer)
	if err := repo.WriteCAR(carBuf); err != nil {
		t.Fatalf("write car: %v", err)
	}

	resolver := &mockHostResolver{endpoint: "https://pds.example.test"}
	fetcher := &mockRepoFetcher{data: carBuf.Bytes()}
	service := New(database, resolver, fetcher, "wss://relay.example.test", log.New(io.Discard, "", 0))

	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:        "did:plc:worker",
		Tracking:   true,
		SyncState:  StatePending,
		Generation: 1,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	workerCtx, cancel := context.WithCancel(context.Background())
	service.Start(workerCtx)

	// Enqueue the DID.
	service.enqueue("did:plc:worker")

	// Wait for backfill to complete.
	time.Sleep(500 * time.Millisecond)

	repoInfo, err := database.GetATProtoRepo(ctx, "did:plc:worker")
	if err != nil {
		t.Fatalf("GetATProtoRepo: %v", err)
	}
	if repoInfo == nil {
		t.Fatal("expected repo info")
	}
	if repoInfo.SyncState != StateSynced {
		t.Errorf("expected synced state, got %q", repoInfo.SyncState)
	}

	cancel()
}

func TestWorkerContinuesAfterError(t *testing.T) {
	database := openServiceTestDB(t)
	defer database.Close()

	ctx := context.Background()

	// First DID will fail (resolver error).
	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:        "did:plc:fail1",
		Tracking:   true,
		SyncState:  StatePending,
		Generation: 1,
	}); err != nil {
		t.Fatalf("seed fail1: %v", err)
	}

	// Second DID will succeed.
	repo := atrepo.NewRepo("did:plc:success2", newTestBlockstore())
	_, _, err := repo.CreateRecord(ctx, mapper.RecordTypePost, &appbsky.FeedPost{
		LexiconTypeID: mapper.RecordTypePost,
		Text:          "success post",
		CreatedAt:     "2026-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("create record: %v", err)
	}
	_, _, err = repo.Commit(ctx, func(context.Context, string, []byte) ([]byte, error) {
		return []byte("test-signature"), nil
	})
	if err != nil {
		t.Fatalf("commit repo: %v", err)
	}
	carBuf := new(bytes.Buffer)
	if err := repo.WriteCAR(carBuf); err != nil {
		t.Fatalf("write car: %v", err)
	}

	resolver := &mockHostResolver{
		endpoint: "https://pds.example.test",
		didErr:   map[string]error{"did:plc:fail1": fmt.Errorf("resolve failed")},
	}
	fetcher := &mockRepoFetcher{data: carBuf.Bytes()}
	service := New(database, resolver, fetcher, "wss://relay.example.test", log.New(io.Discard, "", 0))

	if err := database.UpsertATProtoRepo(ctx, db.ATProtoRepo{
		DID:        "did:plc:success2",
		Tracking:   true,
		SyncState:  StatePending,
		Generation: 1,
	}); err != nil {
		t.Fatalf("seed success2: %v", err)
	}

	workerCtx, cancel := context.WithCancel(context.Background())
	service.Start(workerCtx)

	// Enqueue both DIDs.
	service.enqueue("did:plc:fail1")
	time.Sleep(100 * time.Millisecond) // Let fail1 get picked up
	service.enqueue("did:plc:success2")

	// Wait for both to be processed.
	time.Sleep(1 * time.Second)

	// fail1 should be in error state.
	repo1, _ := database.GetATProtoRepo(ctx, "did:plc:fail1")
	if repo1 == nil || repo1.SyncState != StateError {
		t.Errorf("expected fail1 in error state, got %+v", repo1)
	}

	// success2 should be synced.
	repo2, _ := database.GetATProtoRepo(ctx, "did:plc:success2")
	if repo2 == nil || repo2.SyncState != StateSynced {
		t.Errorf("expected success2 in synced state, got %+v", repo2)
	}

	cancel()
}

func TestExistingATCID(t *testing.T) {
	existing := &db.ATProtoRecord{ATCID: "bafyexisting"}
	result := existingATCID(existing, nil)
	if result != "bafyexisting" {
		t.Errorf("expected 'bafyexisting', got %q", result)
	}

	existing = &db.ATProtoRecord{ATCID: "   "}
	result = existingATCID(existing, nil)
	if result != "" {
		t.Errorf("expected empty string for whitespace ATCID, got %q", result)
	}

	result = existingATCID(nil, nil)
	if result != "" {
		t.Errorf("expected empty string for nil inputs, got %q", result)
	}
}

func TestExistingJSON(t *testing.T) {
	result := existingJSON(nil)
	if result != "" {
		t.Errorf("expected empty string for nil record, got %q", result)
	}

	existing := &db.ATProtoRecord{RecordJSON: `{"key":"value"}`}
	result = existingJSON(existing)
	if result != `{"key":"value"}` {
		t.Errorf("expected JSON string, got %q", result)
	}
}

func TestShouldQueueTrack(t *testing.T) {
	tests := []struct {
		state    string
		expected bool
	}{
		{"", true},
		{StatePending, true},
		{StateBackfilling, true},
		{StateDesynchronized, true},
		{StateError, true},
		{StateSynced, false},
		{StateDeleted, false},
		{StateDeactivated, false},
		{StateTakendown, false},
		{StateSuspended, false},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			result := shouldQueueTrack(tt.state)
			if result != tt.expected {
				t.Errorf("shouldQueueTrack(%q) = %v, want %v", tt.state, result, tt.expected)
			}
		})
	}
}

func TestIsKnownSyncState(t *testing.T) {
	knownStates := []string{StatePending, StateBackfilling, StateSynced, StateDesynchronized, StateDeleted, StateDeactivated, StateTakendown, StateSuspended, StateError}
	for _, state := range knownStates {
		if !isKnownSyncState(state) {
			t.Errorf("isKnownSyncState(%q) should be true", state)
		}
	}

	if isKnownSyncState("unknown_state") {
		t.Error("isKnownSyncState('unknown_state') should be false")
	}
}
