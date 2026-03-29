package bridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	indigorepo "github.com/bluesky-social/indigo/repo"
	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"
	car "github.com/ipld/go-car"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
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
	rr := indigorepo.NewRepo(ctx, did, bs)

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
			Subject: &atproto.RepoStrongRef{
				Uri: "at://did:plc:test/app.bsky.feed.post/1",
				Cid: "bafytest",
			},
		},
	}
	carData, err := createBridgeTestCAR("did:plc:test", records)
	if err != nil {
		t.Fatalf("create test CAR: %v", err)
	}

	rr, err := indigorepo.ReadRepoFromCar(ctx, bytes.NewReader(carData))
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

	rr, err := indigorepo.ReadRepoFromCar(ctx, bytes.NewReader(carData))
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
