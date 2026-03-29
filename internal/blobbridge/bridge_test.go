package blobbridge

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/mapper"
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

type testBlobStore struct {
	data map[string][]byte
}

func newTestBlobStore() *testBlobStore {
	return &testBlobStore{data: make(map[string][]byte)}
}

func (b *testBlobStore) Put(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	hash := data
	b.data[string(hash)] = data
	return hash, nil
}

func (b *testBlobStore) Get(hash []byte) (io.ReadCloser, error) {
	data, ok := b.data[string(hash)]
	if !ok {
		return nil, io.EOF
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (b *testBlobStore) Has(hash []byte) (bool, error) {
	_, ok := b.data[string(hash)]
	return ok, nil
}

func (b *testBlobStore) Size(hash []byte) (int64, error) {
	data, ok := b.data[string(hash)]
	if !ok {
		return 0, io.EOF
	}
	return int64(len(data)), nil
}

func (b *testBlobStore) Delete(hash []byte) error {
	delete(b.data, string(hash))
	return nil
}

var _ BlobStore = (*testBlobStore)(nil)

func TestBridgeRecordBlobsMapsPostImagesToMentionsAndMarkdown(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	blobStore := newTestBlobStore()

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

	blobStore := newTestBlobStore()

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

	blobStore := newTestBlobStore()

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

	blobStore := newTestBlobStore()

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

func TestPostBlobCandidatesWithNilPost(t *testing.T) {
	if got := postBlobCandidates(nil); got != nil {
		t.Errorf("expected nil for nil post, got %v", got)
	}
}

func TestPostBlobCandidatesWithNilEmbed(t *testing.T) {
	post := &appbsky.FeedPost{Embed: nil}
	if got := postBlobCandidates(post); got != nil {
		t.Errorf("expected nil for nil embed, got %v", got)
	}
}

func TestPostBlobCandidatesWithVideo(t *testing.T) {
	alt := "Video description"
	post := &appbsky.FeedPost{
		Embed: &appbsky.FeedPost_Embed{
			EmbedVideo: &appbsky.EmbedVideo{
				Alt:   &alt,
				Video: &lexutil.LexBlob{MimeType: "video/mp4", Size: 1000},
			},
		},
	}
	candidates := postBlobCandidates(post)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
}

func TestPostBlobCandidatesWithRecordWithMedia(t *testing.T) {
	post := &appbsky.FeedPost{
		Embed: &appbsky.FeedPost_Embed{
			EmbedRecordWithMedia: &appbsky.EmbedRecordWithMedia{
				Media: &appbsky.EmbedRecordWithMedia_Media{
					EmbedImages: &appbsky.EmbedImages{
						Images: []*appbsky.EmbedImages_Image{
							{Image: &lexutil.LexBlob{MimeType: "image/png"}},
						},
					},
				},
			},
		},
	}
	candidates := postBlobCandidates(post)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate from media, got %d", len(candidates))
	}
}

func TestImageCandidatesWithNilImage(t *testing.T) {
	candidates := imageCandidates([]*appbsky.EmbedImages_Image{nil})
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates for nil image, got %d", len(candidates))
	}
}

func TestImageCandidatesSkipsNilImageBlob(t *testing.T) {
	candidates := imageCandidates([]*appbsky.EmbedImages_Image{
		{Image: nil},
	})
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates for nil image blob, got %d", len(candidates))
	}
}

func TestImageCandidatesWithAspectRatio(t *testing.T) {
	alt := "Test image"
	candidates := imageCandidates([]*appbsky.EmbedImages_Image{
		{
			Alt:         alt,
			AspectRatio: &appbsky.EmbedDefs_AspectRatio{Width: 1920, Height: 1080},
			Image:       &lexutil.LexBlob{MimeType: "image/png", Size: 1000},
		},
	})
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].Width != 1920 || candidates[0].Height != 1080 {
		t.Errorf("unexpected dimensions: %dx%d", candidates[0].Width, candidates[0].Height)
	}
}

func TestLabelOrFallback(t *testing.T) {
	tests := []struct {
		label string
		index int
		want  string
	}{
		{"", 1, "bridged attachment 1"},
		{"  ", 2, "bridged attachment 2"},
		{"Description", 3, "Description"},
	}
	for _, tt := range tests {
		got := labelOrFallback(tt.label, tt.index)
		if got != tt.want {
			t.Errorf("labelOrFallback(%q, %d) = %q, want %q", tt.label, tt.index, got, tt.want)
		}
	}
}

func TestAppendMention(t *testing.T) {
	mapped := map[string]interface{}{}
	appendMention(mapped, map[string]interface{}{"name": "Test"})
	if mapped["mentions"] == nil {
		t.Error("expected mentions in mapped")
	}
}

func TestAppendMarkdownBlock(t *testing.T) {
	if appendMarkdownBlock("hello", "world") != "hello\n\nworld" {
		t.Error("unexpected result")
	}
	if appendMarkdownBlock("", "world") != "world" {
		t.Error("expected world for empty text")
	}
	if appendMarkdownBlock("hello", "") != "hello" {
		t.Error("expected hello for empty block")
	}
}

func TestAsString(t *testing.T) {
	tests := []struct {
		input interface{}
		want  string
	}{
		{nil, ""},
		{"hello", "hello"},
		{123, "123"},
		{"", ""},
	}
	for _, tt := range tests {
		got := asString(tt.input)
		if got != tt.want {
			t.Errorf("asString(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
