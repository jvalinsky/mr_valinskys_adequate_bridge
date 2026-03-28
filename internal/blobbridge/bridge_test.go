package blobbridge

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	lexutil "github.com/bluesky-social/indigo/lex/util"
	ssbrepo "go.cryptoscope.co/ssb/repo"

	"github.com/mr_valinskys_adequate_bridge/internal/db"
	"github.com/mr_valinskys_adequate_bridge/internal/mapper"
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

type fakeHostResolver struct {
	host string
	err  error
	dids []string
}

func (f *fakeHostResolver) ResolvePDSEndpoint(_ context.Context, did string) (string, error) {
	f.dids = append(f.dids, did)
	if f.err != nil {
		return "", f.err
	}
	return f.host, nil
}

func TestBridgeRecordBlobsMapsPostImagesToMentionsAndMarkdown(t *testing.T) {
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
		"type": "post",
		"text": "hello",
	}
	raw := []byte(`{
		"text":"hello",
		"embed":{
			"$type":"app.bsky.embed.images",
			"images":[
				{
					"alt":"Sunset",
					"aspectRatio":{"width":800,"height":600},
					"image":{"$type":"blob","ref":{"$link":"bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"},"mimeType":"image/png","size":1234}
				}
			]
		},
		"createdAt":"2023-01-01T00:00:00Z"
	}`)

	if err := bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapper.RecordTypePost, mapped, raw); err != nil {
		t.Fatalf("bridge record blobs: %v", err)
	}

	mentions, ok := mapped["mentions"].([]map[string]interface{})
	if !ok || len(mentions) != 1 {
		t.Fatalf("expected one blob mention, got %+v", mapped["mentions"])
	}
	if mentions[0]["name"] != "Sunset" || mentions[0]["type"] != "image/png" {
		t.Fatalf("unexpected blob mention metadata: %+v", mentions[0])
	}
	if got := mapped["text"]; got == nil || got.(string) == "hello" {
		t.Fatalf("expected markdown attachment text, got %v", got)
	}

	blob, err := database.GetBlob(context.Background(), "bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku")
	if err != nil {
		t.Fatalf("get blob mapping: %v", err)
	}
	if blob == nil || blob.SSBBlobRef == "" {
		t.Fatalf("expected blob mapping to be persisted")
	}
}

func TestBridgeRecordBlobsIgnoresExternalThumbs(t *testing.T) {
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

	bridge := New(database, blobStore, nil, log.New(io.Discard, "", 0))

	mapped := map[string]interface{}{
		"type": "post",
		"text": "hello",
	}
	raw := []byte(`{
		"text":"hello",
		"embed":{
			"$type":"app.bsky.embed.external",
			"external":{
				"uri":"https://example.com",
				"title":"Example",
				"description":"Desc",
				"thumb":{"$type":"blob","ref":{"$link":"bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"},"mimeType":"image/jpeg","size":321}
			}
		},
		"createdAt":"2023-01-01T00:00:00Z"
	}`)

	if err := bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapper.RecordTypePost, mapped, raw); err != nil {
		t.Fatalf("bridge record blobs: %v", err)
	}

	if _, ok := mapped["mentions"]; ok {
		t.Fatalf("expected no blob mention for external thumb: %+v", mapped)
	}
	if mapped["text"] != "hello" {
		t.Fatalf("expected unchanged text, got %v", mapped["text"])
	}
}

func TestBridgeRecordBlobsMapsProfileAvatarUsingExistingBlob(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := database.AddBlob(context.Background(), db.Blob{
		ATCID:      "bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku",
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

	bridge := New(database, blobStore, nil, log.New(io.Discard, "", 0))

	mapped := map[string]interface{}{
		"type": "about",
	}
	raw := []byte(`{
		"displayName":"Alice",
		"avatar":{"$type":"blob","ref":{"$link":"bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"},"mimeType":"image/png","size":42}
	}`)

	if err := bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapper.RecordTypeProfile, mapped, raw); err != nil {
		t.Fatalf("bridge record blobs: %v", err)
	}

	image, ok := mapped["image"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected image metadata map, got %T", mapped["image"])
	}
	if image["link"] != "&existing.sha256" || image["type"] != "image/png" {
		t.Fatalf("unexpected profile image mapping: %+v", image)
	}
}

func TestBridgeRecordBlobsFetchesBlobFromResolvedDIDPDS(t *testing.T) {
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

	var requestedPath string
	var requestedDID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedDID = r.URL.Query().Get("did")
		_, _ = w.Write([]byte("resolved-pds-image"))
	}))
	defer server.Close()

	resolver := &fakeHostResolver{host: server.URL}
	bridge := NewWithResolver(database, blobStore, resolver, server.Client(), log.New(io.Discard, "", 0))

	mapped := map[string]interface{}{
		"type": "post",
		"text": "hello",
	}
	raw := []byte(`{
		"text":"hello",
		"embed":{
			"$type":"app.bsky.embed.images",
			"images":[
				{
					"alt":"Resolved blob",
					"image":{"$type":"blob","ref":{"$link":"bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"},"mimeType":"image/png","size":11}
				}
			]
		}
	}`)

	if err := bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapper.RecordTypePost, mapped, raw); err != nil {
		t.Fatalf("bridge record blobs: %v", err)
	}

	if len(resolver.dids) != 1 || resolver.dids[0] != "did:plc:alice" {
		t.Fatalf("resolver dids = %+v", resolver.dids)
	}
	if requestedPath != "/xrpc/com.atproto.sync.getBlob" {
		t.Fatalf("unexpected blob path %q", requestedPath)
	}
	if requestedDID != "did:plc:alice" {
		t.Fatalf("unexpected blob did query %q", requestedDID)
	}
}
