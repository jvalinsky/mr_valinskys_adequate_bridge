package blobbridge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestBridgeRecordBlobsMapsExternalThumbsToMentionsAndMarkdown(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	blobStore := newTestBlobStore()

	bridge := New(
		database,
		blobStore,
		&fakeLexClient{payload: []byte("fake-thumb-bytes")},
		log.New(io.Discard, "", 0),
	)

	mapped := map[string]interface{}{
		"type": "post",
		"text": "Check this out",
	}
	raw := []byte(`{
		"text":"Check this out",
		"embed":{
			"$type":"app.bsky.embed.external",
			"external":{
				"uri":"https://example.com",
				"title":"Example Title",
				"description":"Example Description",
				"thumb":{"$type":"blob","ref":{"$link":"bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"},"mimeType":"image/jpeg","size":321}
			}
		},
		"createdAt":"2023-01-01T00:00:00Z"
	}`)

	if err := bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapper.RecordTypePost, mapped, raw); err != nil {
		t.Fatalf("bridge record blobs: %v", err)
	}

	mentions, ok := mapped["mentions"].([]map[string]interface{})
	if !ok || len(mentions) != 1 {
		t.Fatalf("expected one blob mention for external thumb, got %+v", mapped["mentions"])
	}
	if mentions[0]["name"] != "Example Title" || mentions[0]["type"] != "image/jpeg" {
		t.Fatalf("unexpected blob mention metadata: %+v", mentions[0])
	}
	if !strings.Contains(mapped["text"].(string), "![Example Title]") {
		t.Fatalf("expected markdown attachment in text, got %v", mapped["text"])
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
		typ   string
		index int
		want  string
	}{
		{"", "attachment", 1, "bridged attachment 1"},
		{"  ", "blob", 2, "bridged blob 2"},
		{"Description", "any", 3, "Description"},
	}
	for _, tt := range tests {
		got := labelOrFallback(tt.label, tt.typ, tt.index)
		if got != tt.want {
			t.Errorf("labelOrFallback(%q, %q, %d) = %q, want %q", tt.label, tt.typ, tt.index, got, tt.want)
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

func TestBridgeRecordBlobsUnsupportedType(t *testing.T) {
	b := New(nil, nil, nil, nil)
	err := b.BridgeRecordBlobs(context.Background(), "did:plc:alice", "app.bsky.feed.like", map[string]interface{}{}, nil)
	if err != nil {
		t.Errorf("Expected nil error for unsupported type, got %v", err)
	}
}

func TestConfiguredHTTPClient(t *testing.T) {
	c := configuredHTTPClient(nil)
	if c == nil {
		t.Errorf("Expected non-nil fallback client")
	}

	existing := &http.Client{Timeout: 5 * time.Second}
	c2 := configuredHTTPClient(existing)
	if c2 != existing {
		t.Errorf("Expected existing client to be returned")
	}
}

// --- Additional tests for 100% coverage ---

func TestBridgePostBlobsUnmarshalError(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	bridge := New(database, newTestBlobStore(), &fakeLexClient{}, log.New(io.Discard, "", 0))
	mapped := map[string]interface{}{"text": "hello"}
	err = bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapper.RecordTypePost, mapped, []byte(`{invalid`))
	if err == nil {
		t.Fatal("expected unmarshal error for invalid post JSON")
	}
}

func TestBridgeProfileBlobsUnmarshalError(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	bridge := New(database, newTestBlobStore(), &fakeLexClient{}, log.New(io.Discard, "", 0))
	mapped := map[string]interface{}{}
	err = bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapper.RecordTypeProfile, mapped, []byte(`{invalid`))
	if err == nil {
		t.Fatal("expected unmarshal error for invalid profile JSON")
	}
}

func TestBridgeRecordBlobsMapsProfileAvatarAndBanner(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	blobStore := newTestBlobStore()
	bridge := New(
		database,
		blobStore,
		&fakeLexClient{payload: []byte("fake-blob")},
		log.New(io.Discard, "", 0),
	)

	mapped := map[string]interface{}{"type": "about"}
	validCID := "bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"
	raw := []byte(fmt.Sprintf(`{
		"displayName":"Alice",
		"avatar":{"$type":"blob","ref":{"$link":"%s"},"mimeType":"image/png","size":10},
		"banner":{"$type":"blob","ref":{"$link":"%s"},"mimeType":"image/jpeg","size":20}
	}`, validCID, validCID))

	if err := bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapper.RecordTypeProfile, mapped, raw); err != nil {
		t.Fatalf("bridge record blobs: %v", err)
	}

	if img, ok := mapped["image"].(map[string]interface{}); !ok || img["type"] != "image/png" {
		t.Errorf("avatar missing or wrong: %+v", mapped["image"])
	}
	if bnr, ok := mapped["banner"].(map[string]interface{}); !ok || bnr["type"] != "image/jpeg" {
		t.Errorf("banner missing or wrong: %+v", mapped["banner"])
	}
}

func TestBridgeProfileBlobsZeroSizeEmptyMime(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	bridge := New(database, newTestBlobStore(), &fakeLexClient{payload: []byte("avatar-bytes")}, log.New(io.Discard, "", 0))
	mapped := map[string]interface{}{}
	raw := []byte(`{
		"displayName":"Alice",
		"avatar":{"$type":"blob","ref":{"$link":"bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"},"mimeType":"","size":0}
	}`)
	err = bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapper.RecordTypeProfile, mapped, raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	image, ok := mapped["image"].(map[string]interface{})
	if !ok {
		t.Fatal("expected image metadata")
	}
	// With empty mime and zero size, those fields should be omitted
	if _, hasType := image["type"]; hasType {
		t.Error("expected no type for empty mimetype")
	}
	if _, hasSize := image["size"]; hasSize {
		t.Error("expected no size for zero size")
	}
}

func TestPostBlobCandidatesRecordWithMediaVideo(t *testing.T) {
	alt := "Embedded video"
	post := &appbsky.FeedPost{
		Embed: &appbsky.FeedPost_Embed{
			EmbedRecordWithMedia: &appbsky.EmbedRecordWithMedia{
				Media: &appbsky.EmbedRecordWithMedia_Media{
					EmbedVideo: &appbsky.EmbedVideo{
						Alt:   &alt,
						Video: &lexutil.LexBlob{MimeType: "video/mp4", Size: 5000},
					},
				},
			},
		},
	}
	candidates := postBlobCandidates(post)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].MimeType != "video/mp4" {
		t.Errorf("expected video/mp4, got %s", candidates[0].MimeType)
	}
}

func TestPostBlobCandidatesRecordWithMediaNilSubMedia(t *testing.T) {
	// Media exists but neither EmbedImages nor EmbedVideo is set
	post := &appbsky.FeedPost{
		Embed: &appbsky.FeedPost_Embed{
			EmbedRecordWithMedia: &appbsky.EmbedRecordWithMedia{
				Media: &appbsky.EmbedRecordWithMedia_Media{},
			},
		},
	}
	candidates := postBlobCandidates(post)
	if candidates != nil {
		t.Errorf("expected nil candidates, got %v", candidates)
	}
}

func TestPostBlobCandidatesRecordWithMediaNilMedia(t *testing.T) {
	post := &appbsky.FeedPost{
		Embed: &appbsky.FeedPost_Embed{
			EmbedRecordWithMedia: &appbsky.EmbedRecordWithMedia{
				Media: nil,
			},
		},
	}
	candidates := postBlobCandidates(post)
	if candidates != nil {
		t.Errorf("expected nil candidates for nil media, got %v", candidates)
	}
}

func TestVideoCandidateNilVideo(t *testing.T) {
	cs := videoCandidates(nil, 0)
	if len(cs) != 0 {
		t.Errorf("expected 0 candidates for nil video, got %d", len(cs))
	}
}

func TestVideoCandidateNilAlt(t *testing.T) {
	cs := videoCandidates(&appbsky.EmbedVideo{
		Alt:   nil,
		Video: &lexutil.LexBlob{MimeType: "video/mp4", Size: 100},
	}, 2)
	if len(cs) == 0 {
		t.Fatal("expected at least 1 candidate")
	}
	c := cs[0]
	if c.Label != "bridged video 3" {
		t.Errorf("expected fallback label for nil alt, got %s", c.Label)
	}
}

func TestVideoCandidateWithAspectRatio(t *testing.T) {
	alt := "clip"
	cs := videoCandidates(&appbsky.EmbedVideo{
		Alt:         &alt,
		Video:       &lexutil.LexBlob{MimeType: "video/mp4", Size: 999},
		AspectRatio: &appbsky.EmbedDefs_AspectRatio{Width: 1280, Height: 720},
	}, 0)
	if len(cs) == 0 {
		t.Fatal("expected at least 1 candidate")
	}
	c := cs[0]
	if c.Width != 1280 || c.Height != 720 {
		t.Errorf("expected 1280x720, got %dx%d", c.Width, c.Height)
	}
}

func TestVideoCandidateNilVideoBlob(t *testing.T) {
	alt := "no blob"
	cs := videoCandidates(&appbsky.EmbedVideo{
		Alt:   &alt,
		Video: nil,
	}, 0)
	if len(cs) != 0 {
		t.Errorf("expected 0 candidates for nil video blob, got %d", len(cs))
	}
}

func TestAppendMentionWithExistingSliceMapInterface(t *testing.T) {
	// Test the []map[string]interface{} branch
	mapped := map[string]interface{}{
		"mentions": []map[string]interface{}{
			{"link": "existing-ref", "name": "old"},
		},
	}
	appendMention(mapped, map[string]interface{}{"link": "new-ref", "name": "New Item", "size": int64(100), "width": 800, "height": 600, "type": "image/png"})
	mentions := mapped["mentions"].([]map[string]interface{})
	if len(mentions) != 2 {
		t.Fatalf("expected 2 mentions, got %d", len(mentions))
	}
	if mentions[1]["width"] != 800 || mentions[1]["height"] != 600 {
		t.Error("expected width/height to be set")
	}
	if mentions[1]["size"] != int64(100) {
		t.Error("expected size to be set")
	}
}

func TestAppendMentionWithExistingSliceInterface(t *testing.T) {
	// Test the []interface{} branch
	mapped := map[string]interface{}{
		"mentions": []interface{}{
			map[string]interface{}{"link": "existing-ref"},
		},
	}
	appendMention(mapped, map[string]interface{}{"link": "another-ref"})
	mentions := mapped["mentions"].([]map[string]interface{})
	if len(mentions) != 2 {
		t.Fatalf("expected 2 mentions, got %d", len(mentions))
	}
}

func TestAppendMentionDuplicateLinkSkipped(t *testing.T) {
	mapped := map[string]interface{}{
		"mentions": []map[string]interface{}{
			{"link": "dup-ref"},
		},
	}
	appendMention(mapped, map[string]interface{}{"link": "dup-ref"})
	mentions := mapped["mentions"].([]map[string]interface{})
	if len(mentions) != 1 {
		t.Fatalf("expected duplicate to be skipped, got %d mentions", len(mentions))
	}
}

func TestAppendMentionSliceInterfaceNonMap(t *testing.T) {
	// []interface{} with non-map items should be skipped
	mapped := map[string]interface{}{
		"mentions": []interface{}{"not-a-map"},
	}
	appendMention(mapped, map[string]interface{}{"link": "ref"})
	mentions := mapped["mentions"].([]map[string]interface{})
	if len(mentions) != 1 {
		t.Fatalf("expected 1 mention, got %d", len(mentions))
	}
}

func TestAppendMentionZeroSizeAndDimensions(t *testing.T) {
	mapped := map[string]interface{}{}
	appendMention(mapped, map[string]interface{}{
		"link":   "ref",
		"name":   "",
		"size":   int64(0),
		"width":  0,
		"height": 0,
		"type":   "",
	})
	mentions := mapped["mentions"].([]map[string]interface{})
	if len(mentions) != 1 {
		t.Fatalf("expected 1 mention, got %d", len(mentions))
	}
	// Zero/empty values should be omitted from the normalized mention
	if _, ok := mentions[0]["name"]; ok {
		t.Error("expected empty name to be omitted")
	}
	if _, ok := mentions[0]["size"]; ok {
		t.Error("expected zero size to be omitted")
	}
	if _, ok := mentions[0]["width"]; ok {
		t.Error("expected zero width to be omitted")
	}
	if _, ok := mentions[0]["height"]; ok {
		t.Error("expected zero height to be omitted")
	}
	if _, ok := mentions[0]["type"]; ok {
		t.Error("expected empty type to be omitted")
	}
}

func TestEnsureBlobNoClientOrResolver(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	// Bridge with no xrpc and no resolver
	bridge := &Bridge{
		db:        database,
		blobStore: newTestBlobStore(),
		logger:    log.New(io.Discard, "", 0),
	}
	mapped := map[string]interface{}{"text": "hello"}
	raw := []byte(`{
		"text":"hello",
		"embed":{
			"$type":"app.bsky.embed.images",
			"images":[
				{
					"alt":"Test",
					"image":{"$type":"blob","ref":{"$link":"bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"},"mimeType":"image/png","size":10}
				}
			]
		}
	}`)
	err = bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapper.RecordTypePost, mapped, raw)
	if err == nil {
		t.Fatal("expected error when no xrpc or resolver configured")
	}
	if !strings.Contains(err.Error(), "blob fetch unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureBlobResolverError(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	resolver := &fakeHostResolver{err: fmt.Errorf("resolve failure")}
	bridge := NewWithResolver(database, newTestBlobStore(), resolver, nil, log.New(io.Discard, "", 0))

	mapped := map[string]interface{}{"text": "hello"}
	raw := []byte(`{
		"text":"hello",
		"embed":{
			"$type":"app.bsky.embed.images",
			"images":[
				{
					"alt":"Test",
					"image":{"$type":"blob","ref":{"$link":"bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"},"mimeType":"image/png","size":10}
				}
			]
		}
	}`)
	err = bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapper.RecordTypePost, mapped, raw)
	if err == nil {
		t.Fatal("expected error from resolver failure")
	}
	if !strings.Contains(err.Error(), "resolve blob host") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureBlobFetchError(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	// Server that returns an error status
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"InternalServerError","message":"fail"}`))
	}))
	defer server.Close()

	resolver := &fakeHostResolver{host: server.URL}
	bridge := NewWithResolver(database, newTestBlobStore(), resolver, server.Client(), log.New(io.Discard, "", 0))

	mapped := map[string]interface{}{"text": "hello"}
	raw := []byte(`{
		"text":"hello",
		"embed":{
			"$type":"app.bsky.embed.images",
			"images":[
				{
					"alt":"Test",
					"image":{"$type":"blob","ref":{"$link":"bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"},"mimeType":"image/png","size":10}
				}
			]
		}
	}`)
	err = bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapper.RecordTypePost, mapped, raw)
	if err == nil {
		t.Fatal("expected error from blob fetch failure")
	}
	if !strings.Contains(err.Error(), "fetch blob") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type failPutBlobStore struct {
	*testBlobStore
}

func (f *failPutBlobStore) Put(r io.Reader) ([]byte, error) {
	return nil, fmt.Errorf("put failed")
}

type closerBlobStore struct {
	*testBlobStore
	db *db.DB
}

func (c *closerBlobStore) Put(r io.Reader) ([]byte, error) {
	hash, err := c.testBlobStore.Put(r)
	c.db.Close()
	return hash, err
}

func TestEnsureBlobStorePutError(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("image-data"))
	}))
	defer server.Close()

	resolver := &fakeHostResolver{host: server.URL}
	bridge := NewWithResolver(database, &failPutBlobStore{newTestBlobStore()}, resolver, server.Client(), log.New(io.Discard, "", 0))

	mapped := map[string]interface{}{"text": "hello"}
	raw := []byte(`{
		"text":"hello",
		"embed":{
			"$type":"app.bsky.embed.images",
			"images":[
				{
					"alt":"Test",
					"image":{"$type":"blob","ref":{"$link":"bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"},"mimeType":"image/png","size":10}
				}
			]
		}
	}`)
	err = bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapper.RecordTypePost, mapped, raw)
	if err == nil {
		t.Fatal("expected error from blob store put failure")
	}
	if !strings.Contains(err.Error(), "store blob") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureBlobGetBlobError(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// Close it to make queries fail
	database.Close()

	bridge := &Bridge{
		db:        database,
		blobStore: newTestBlobStore(),
		logger:    log.New(io.Discard, "", 0),
	}
	raw := []byte(`{"text":"hello","embed":{"$type":"app.bsky.embed.images","images":[{"alt":"Test","image":{"$type":"blob","ref":{"$link":"bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"},"mimeType":"image/png","size":10}}]}}`)
	err = bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapper.RecordTypePost, map[string]interface{}{}, raw)
	if err == nil {
		t.Fatal("expected error from query blob mapping failure")
	}
	if !strings.Contains(err.Error(), "query blob mapping") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureBlobAddBlobError(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// No defer close, we will close it mid-way

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("image-data"))
	}))
	defer server.Close()

	resolver := &fakeHostResolver{host: server.URL}
	bridge := NewWithResolver(database, newTestBlobStore(), resolver, server.Client(), log.New(io.Discard, "", 0))

	// We want GetBlob to succeed (returning nil), but AddBlob to fail.
	// In SQLite :memory:, if we close the connection, all subsequent calls fail.
	// But SyncGetBlob and Put happen between GetBlob and AddBlob.
	// We can use a custom BlobStore that closes the DB!

	bridge.blobStore = &closerBlobStore{testBlobStore: newTestBlobStore(), db: database}

	raw := []byte(`{"text":"hello","embed":{"$type":"app.bsky.embed.images","images":[{"alt":"Test","image":{"$type":"blob","ref":{"$link":"bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"},"mimeType":"image/png","size":10}}]}}`)
	err = bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapper.RecordTypePost, map[string]interface{}{}, raw)
	if err == nil {
		t.Fatal("expected error from persist blob mapping failure")
	}
	if !strings.Contains(err.Error(), "persist blob mapping") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientForDIDNoClientConfigured(t *testing.T) {
	bridge := &Bridge{}
	_, err := bridge.clientForDID(context.Background(), "did:plc:alice")
	if err == nil {
		t.Fatal("expected error when no client configured")
	}
	if !strings.Contains(err.Error(), "no blob fetch client configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEscapeMarkdownLabel(t *testing.T) {
	if got := escapeMarkdownLabel("hello]world]"); got != "hello\\]world\\]" {
		t.Errorf("unexpected escaped label: %s", got)
	}
}

func TestBridgeProfileBlobsEnsureBlobError(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	// Bridge with no xrpc/resolver -> ensureBlob will fail
	bridge := &Bridge{
		db:        database,
		blobStore: newTestBlobStore(),
		logger:    log.New(io.Discard, "", 0),
	}
	mapped := map[string]interface{}{}
	raw := []byte(`{
		"displayName":"Alice",
		"avatar":{"$type":"blob","ref":{"$link":"bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku"},"mimeType":"image/png","size":42}
	}`)
	err = bridge.BridgeRecordBlobs(context.Background(), "did:plc:alice", mapper.RecordTypeProfile, mapped, raw)
	if err == nil {
		t.Fatal("expected error from profile ensureBlob failure")
	}
}
