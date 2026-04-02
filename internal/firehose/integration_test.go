package firehose

import (
	"bytes"
	"context"
	"testing"
	"time"

	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/appbsky"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/repo"
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

func createIntegrationCAR(did string, records map[string]interface{}) ([]byte, error) {
	ctx := context.Background()
	bs := newIntegrationBlockstore()
	rr := repo.NewRepo(did, bs)

	for path, record := range records {
		parts := splitPath(path)
		if len(parts) != 2 {
			continue
		}
		collection := parts[0]
		switch r := record.(type) {
		case *appbsky.FeedPost:
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
	}

	_, _, err := rr.Commit(ctx, func(context.Context, string, []byte) ([]byte, error) {
		return []byte("integration-test-signature"), nil
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

func splitPath(path string) []string {
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			return []string{path[:i], path[i+1:]}
		}
	}
	return nil
}

func TestIntegrationParseCommitWithMultipleRecords(t *testing.T) {
	records := map[string]interface{}{
		"app.bsky.feed.post/post1": &appbsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          "Integration test post 1",
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		},
		"app.bsky.feed.post/post2": &appbsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          "Integration test post 2",
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		},
		"app.bsky.graph.follow/follow1": &appbsky.GraphFollow{
			LexiconTypeID: "app.bsky.graph.follow",
			Subject:       "did:plc:target",
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		},
	}
	carData, err := createIntegrationCAR("did:plc:integration-test", records)
	if err != nil {
		t.Fatalf("create integration CAR: %v", err)
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

func TestIntegrationProcessOpsWithMultipleActions(t *testing.T) {
	records := map[string]interface{}{
		"app.bsky.feed.post/action1": &appbsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          "Action test post",
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		},
	}
	carData, err := createIntegrationCAR("did:plc:integration-test", records)
	if err != nil {
		t.Fatalf("create integration CAR: %v", err)
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
				Path:   "app.bsky.feed.post/action1",
			},
			{
				Action: "delete",
				Path:   "app.bsky.feed.post/action1",
			},
		},
	}

	err = ProcessOps(context.Background(), rr, evt)
	if err != nil {
		t.Fatalf("ProcessOps: %v", err)
	}
}

func TestIntegrationCARWithLargeRecord(t *testing.T) {
	longText := ""
	for i := 0; i < 1000; i++ {
		longText += "This is a long post for integration testing. "
	}

	records := map[string]interface{}{
		"app.bsky.feed.post/large": &appbsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          longText,
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		},
	}
	carData, err := createIntegrationCAR("did:plc:integration-test", records)
	if err != nil {
		t.Fatalf("create integration CAR with large record: %v", err)
	}

	rr, err := ParseCommit(context.Background(), &atproto.SyncSubscribeRepos_Commit{
		Blocks: carData,
	})
	if err != nil {
		t.Fatalf("ParseCommit: %v", err)
	}

	if rr == nil {
		t.Fatal("expected non-nil repo")
	}
}

func TestIntegrationProcessOpsWithNilCid(t *testing.T) {
	records := map[string]interface{}{
		"app.bsky.feed.post/nilcid": &appbsky.FeedPost{
			LexiconTypeID: "app.bsky.feed.post",
			Text:          "Test post with nil cid",
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		},
	}
	carData, err := createIntegrationCAR("did:plc:integration-test", records)
	if err != nil {
		t.Fatalf("create integration CAR: %v", err)
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
				Path:   "app.bsky.feed.post/nilcid",
				Cid:    nil,
			},
		},
	}

	err = ProcessOps(context.Background(), rr, evt)
	if err != nil {
		t.Fatalf("ProcessOps should skip nil CID: %v", err)
	}
}
