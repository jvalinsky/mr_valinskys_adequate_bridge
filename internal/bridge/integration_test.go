package bridge

import (
	"bytes"
	"context"
	"io"
	"log"
	"testing"
	"time"

	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
	appbsky "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/appbsky"
	atrepo "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/repo"
)

type integrationBlockstore struct {
	blocks map[string]blockformat.Block
}

func newIntegrationBlockstore() *integrationBlockstore {
	return &integrationBlockstore{blocks: make(map[string]blockformat.Block)}
}

func (bs *integrationBlockstore) Put(_ context.Context, blk blockformat.Block) error {
	bs.blocks[blk.Cid().KeyString()] = blk
	return nil
}

func (bs *integrationBlockstore) Get(_ context.Context, c cid.Cid) (blockformat.Block, error) {
	blk, ok := bs.blocks[c.KeyString()]
	if !ok {
		return nil, &ipld.ErrNotFound{Cid: c}
	}
	return blk, nil
}

func createBridgeTestCAR(did string, records map[string]interface{}) ([]byte, error) {
	ctx := context.Background()
	bs := newIntegrationBlockstore()
	rr := atrepo.NewRepo(did, bs)

	for path, record := range records {
		for i := 0; i < len(path); i++ {
			if path[i] == '/' {
				collection := path[:i]
				switch r := record.(type) {
				case *appbsky.FeedPost:
					_, _, err := rr.CreateRecord(ctx, collection, r)
					if err != nil {
						return nil, err
					}
				case *appbsky.FeedLike:
					_, _, err := rr.CreateRecord(ctx, collection, r)
					if err != nil {
						return nil, err
					}
				case *appbsky.GraphFollow:
					_, _, err := rr.CreateRecord(ctx, collection, r)
					if err != nil {
						return nil, err
					}
				default:
					continue
				}
				break
			}
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

func TestIntegrationHandleCommitWithNilEvent(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	processor := NewProcessor(database, log.New(io.Discard, "", 0))

	err = processor.HandleCommit(context.Background(), nil)
	if err != nil {
		t.Fatalf("HandleCommit with nil: %v", err)
	}
}

func TestIntegrationHandleCommitWithEmptyRepo(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	processor := NewProcessor(database, log.New(io.Discard, "", 0))

	err = processor.HandleCommit(context.Background(), &atproto.SyncSubscribeRepos_Commit{
		Repo: "",
	})
	if err != nil {
		t.Fatalf("HandleCommit with empty repo: %v", err)
	}
}

func TestIntegrationHandleCommitWithUnknownAccount(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	processor := NewProcessor(database, log.New(io.Discard, "", 0))

	records := map[string]interface{}{
		"app.bsky.feed.post/test1": &appbsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          "Test post",
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		},
	}
	carData, err := createBridgeTestCAR("did:plc:unknown", records)
	if err != nil {
		t.Fatalf("create test CAR: %v", err)
	}

	err = processor.HandleCommit(context.Background(), &atproto.SyncSubscribeRepos_Commit{
		Repo:   "did:plc:unknown",
		Blocks: carData,
		Ops: []*atproto.SyncSubscribeRepos_RepoOp{
			{Action: "create", Path: "app.bsky.feed.post/test1"},
		},
	})
	if err != nil {
		t.Fatalf("HandleCommit with unknown account: %v", err)
	}
}

func TestIntegrationHandleCommitWithInactiveAccount(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:inactive",
		SSBFeedID: "@inactive.ed25519",
		Active:    false,
	}); err != nil {
		t.Fatalf("add inactive account: %v", err)
	}

	processor := NewProcessor(database, log.New(io.Discard, "", 0))

	records := map[string]interface{}{
		"app.bsky.feed.post/test1": &appbsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          "Test post",
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		},
	}
	carData, err := createBridgeTestCAR("did:plc:inactive", records)
	if err != nil {
		t.Fatalf("create test CAR: %v", err)
	}

	err = processor.HandleCommit(ctx, &atproto.SyncSubscribeRepos_Commit{
		Repo:   "did:plc:inactive",
		Blocks: carData,
		Ops: []*atproto.SyncSubscribeRepos_RepoOp{
			{Action: "create", Path: "app.bsky.feed.post/test1"},
		},
	})
	if err != nil {
		t.Fatalf("HandleCommit with inactive account: %v", err)
	}
}

func TestIntegrationHandleCommitWithCreateAndDelete(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:test",
		SSBFeedID: "@test.ed25519",
		Active:    true,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	records := map[string]interface{}{
		"app.bsky.feed.post/test1": &appbsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          "Test post",
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		},
	}
	carData, err := createBridgeTestCAR("did:plc:test", records)
	if err != nil {
		t.Fatalf("create test CAR: %v", err)
	}

	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%test.sha256"}),
	)

	err = processor.HandleCommit(ctx, &atproto.SyncSubscribeRepos_Commit{
		Repo:   "did:plc:test",
		Blocks: carData,
		Seq:    100,
		Ops: []*atproto.SyncSubscribeRepos_RepoOp{
			{Action: "create", Path: "app.bsky.feed.post/test1"},
			{Action: "delete", Path: "app.bsky.feed.post/test1"},
		},
	})
	if err != nil {
		t.Fatalf("HandleCommit with create/delete: %v", err)
	}
}

func TestIntegrationProcessOpWithUnsupportedCollection(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:test",
		SSBFeedID: "@test.ed25519",
		Active:    true,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	records := map[string]interface{}{
		"app.bsky.feed.repost/unsupported": &appbsky.FeedRepost{
			LexiconTypeID: "app.bsky.feed.repost",
			Subject: &appbsky.RepoStrongRef{
				Uri: "at://did:plc:test/app.bsky.feed.post/1",
				Cid: "bafytest",
			},
		},
	}
	carData, err := createBridgeTestCAR("did:plc:test", records)
	if err != nil {
		t.Fatalf("create test CAR: %v", err)
	}

	rr, err := atrepo.ReadRepoFromCar(ctx, bytes.NewReader(carData))
	if err != nil {
		t.Fatalf("read repo: %v", err)
	}

	processor := NewProcessor(database, log.New(io.Discard, "", 0))

	err = processor.processOp(ctx, rr, "did:plc:test", "app.bsky.feed.repost/unsupported", "bafytest", 1)
	if err != nil {
		t.Fatalf("processOp with unsupported collection: %v", err)
	}
}

func TestIntegrationProcessOpWithMissingRecord(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:test",
		SSBFeedID: "@test.ed25519",
		Active:    true,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	records := map[string]interface{}{}
	carData, err := createBridgeTestCAR("did:plc:test", records)
	if err != nil {
		t.Fatalf("create test CAR: %v", err)
	}

	rr, err := atrepo.ReadRepoFromCar(ctx, bytes.NewReader(carData))
	if err != nil {
		t.Fatalf("read repo: %v", err)
	}

	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%test.sha256"}),
	)

	err = processor.processOp(ctx, rr, "did:plc:test", "app.bsky.feed.post/missing", "bafytest", 1)
	if err == nil {
		t.Fatalf("expected error for missing record")
	}
}

func TestIntegrationHandleCommitWithSeqUpdate(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:test",
		SSBFeedID: "@test.ed25519",
		Active:    true,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	records := map[string]interface{}{
		"app.bsky.feed.post/update1": &appbsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          "Original post",
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		},
	}
	carData, err := createBridgeTestCAR("did:plc:test", records)
	if err != nil {
		t.Fatalf("create test CAR: %v", err)
	}

	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%test.sha256"}),
	)

	err = processor.HandleCommit(ctx, &atproto.SyncSubscribeRepos_Commit{
		Repo:   "did:plc:test",
		Blocks: carData,
		Seq:    200,
		Ops: []*atproto.SyncSubscribeRepos_RepoOp{
			{Action: "update", Path: "app.bsky.feed.post/update1"},
		},
	})
	if err != nil {
		t.Fatalf("HandleCommit with update: %v", err)
	}
}

func TestIntegrationHandleCommitWithNilCID(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:test",
		SSBFeedID: "@test.ed25519",
		Active:    true,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	records := map[string]interface{}{
		"app.bsky.feed.post/nilcid": &appbsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          "Test post",
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		},
	}
	carData, err := createBridgeTestCAR("did:plc:test", records)
	if err != nil {
		t.Fatalf("create test CAR: %v", err)
	}

	processor := NewProcessor(database, log.New(io.Discard, "", 0))

	err = processor.HandleCommit(ctx, &atproto.SyncSubscribeRepos_Commit{
		Repo:   "did:plc:test",
		Blocks: carData,
		Ops: []*atproto.SyncSubscribeRepos_RepoOp{
			{Action: "create", Path: "app.bsky.feed.post/nilcid", Cid: nil},
		},
	})
	if err != nil {
		t.Fatalf("HandleCommit with nil CID: %v", err)
	}
}

func TestIntegrationHandleCommitWithEmptyOps(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	processor := NewProcessor(database, log.New(io.Discard, "", 0))

	err = processor.HandleCommit(context.Background(), &atproto.SyncSubscribeRepos_Commit{
		Repo: "did:plc:test",
		Ops:  []*atproto.SyncSubscribeRepos_RepoOp{},
	})
	if err != nil {
		t.Fatalf("HandleCommit with empty ops: %v", err)
	}
}

func TestIntegrationProcessOpWithInvalidPath(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:test",
		SSBFeedID: "@test.ed25519",
		Active:    true,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	records := map[string]interface{}{
		"app.bsky.feed.post/valid": &appbsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          "Test post",
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		},
	}
	carData, err := createBridgeTestCAR("did:plc:test", records)
	if err != nil {
		t.Fatalf("create test CAR: %v", err)
	}

	rr, err := atrepo.ReadRepoFromCar(ctx, bytes.NewReader(carData))
	if err != nil {
		t.Fatalf("read repo: %v", err)
	}

	processor := NewProcessor(database, log.New(io.Discard, "", 0))

	for _, path := range []string{"invalid", "/post", "post/", "app.bsky.feed.post"} {
		err := processor.processOp(ctx, rr, "did:plc:test", path, "bafytest", 1)
		if err != nil {
			t.Errorf("processOp(%q): expected nil, got %v", path, err)
		}
	}
}
