package blobbridge

import (
	"bytes"
	"context"
	"io"
	"log"
	"path/filepath"
	"testing"

	lexutil "github.com/bluesky-social/indigo/lex/util"
	ssbrepo "go.cryptoscope.co/ssb/repo"

	"github.com/mr_valinskys_adequate_bridge/internal/db"
)

type fakeLexClient struct {
	payload []byte
}

func (f *fakeLexClient) LexDo(_ context.Context, _ string, _ string, _ string, _ map[string]any, _ any, out any) error {
	buf, ok := out.(*bytes.Buffer)
	if !ok {
		return nil
	}
	_, _ = buf.Write(f.payload)
	return nil
}

var _ lexutil.LexClient = (*fakeLexClient)(nil)

func TestBridgeRecordBlobsFetchesStoresAndMaps(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	repo := ssbrepo.New(filepath.Join(t.TempDir(), "repo"))
	blobStore, err := ssbrepo.OpenBlobStore(repo)
	if err != nil {
		t.Fatalf("open blobstore: %v", err)
	}

	bridge := New(
		database,
		blobStore,
		&fakeLexClient{payload: []byte("fake-image-bytes")},
		log.New(io.Discard, "", 0),
	)

	mapped := map[string]interface{}{
		"type":                "about",
		"_atproto_avatar_cid": "bafy-avatar",
	}
	raw := []byte(`{
		"avatar": {
			"$type": "blob",
			"ref": {"$link": "bafy-avatar"},
			"mimeType": "image/png"
		}
	}`)

	if err := bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapped, raw); err != nil {
		t.Fatalf("bridge record blobs: %v", err)
	}

	if _, ok := mapped["image"].(string); !ok {
		t.Fatalf("expected mapped image field")
	}
	if _, ok := mapped["blob_refs"].([]string); !ok {
		t.Fatalf("expected blob_refs field")
	}

	blob, err := database.GetBlob(context.Background(), "bafy-avatar")
	if err != nil {
		t.Fatalf("get blob mapping: %v", err)
	}
	if blob == nil || blob.SSBBlobRef == "" {
		t.Fatalf("expected blob mapping to be persisted")
	}
}

func TestBridgeRecordBlobsUsesExistingMappingWithoutFetch(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := database.AddBlob(context.Background(), db.Blob{
		ATCID:      "bafy-avatar",
		SSBBlobRef: "&existing.sha256",
		Size:       42,
		MimeType:   "image/png",
	}); err != nil {
		t.Fatalf("seed blob mapping: %v", err)
	}

	repo := ssbrepo.New(filepath.Join(t.TempDir(), "repo"))
	blobStore, err := ssbrepo.OpenBlobStore(repo)
	if err != nil {
		t.Fatalf("open blobstore: %v", err)
	}

	bridge := New(
		database,
		blobStore,
		nil,
		log.New(io.Discard, "", 0),
	)

	mapped := map[string]interface{}{
		"type":                "about",
		"_atproto_avatar_cid": "bafy-avatar",
	}

	if err := bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapped, nil); err != nil {
		t.Fatalf("bridge record blobs: %v", err)
	}

	if mapped["image"] != "&existing.sha256" {
		t.Fatalf("expected existing blob ref to be used, got %v", mapped["image"])
	}
}
