package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/mapper"
)

type stubHostResolver struct {
	host string
	err  error
}

func (r stubHostResolver) ResolvePDSEndpoint(_ context.Context, _ string) (string, error) {
	return r.host, r.err
}

func TestPDSAwareRecordFetcherUsesPDSHost(t *testing.T) {
	// Serve a fake PDS that returns a valid getRecord response.
	pds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repo := r.URL.Query().Get("repo")
		collection := r.URL.Query().Get("collection")
		rkey := r.URL.Query().Get("rkey")
		cid := "bafytest123"
		uri := fmt.Sprintf("at://%s/%s/%s", repo, collection, rkey)
		resp := map[string]interface{}{
			"uri":   uri,
			"cid":   cid,
			"value": map[string]interface{}{"$type": collection, "text": "hello from PDS"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer pds.Close()

	resolver := stubHostResolver{host: pds.URL}
	// fallback is nil — should not be reached when PDS resolves.
	fetcher := NewPDSAwareRecordFetcher(resolver, nil)

	rec, err := fetcher.FetchRecord(context.Background(), "at://did:plc:test123/app.bsky.feed.post/abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.ATDID != "did:plc:test123" {
		t.Errorf("got ATDID=%q, want did:plc:test123", rec.ATDID)
	}
	if rec.Collection != "app.bsky.feed.post" {
		t.Errorf("got Collection=%q, want app.bsky.feed.post", rec.Collection)
	}
	if rec.ATCID != "bafytest123" {
		t.Errorf("got ATCID=%q, want bafytest123", rec.ATCID)
	}
}

func TestPDSAwareRecordFetcherFallsBackOnResolveError(t *testing.T) {
	// Fallback AppView server.
	appview := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repo := r.URL.Query().Get("repo")
		collection := r.URL.Query().Get("collection")
		rkey := r.URL.Query().Get("rkey")
		cid := "bafyfallback"
		uri := fmt.Sprintf("at://%s/%s/%s", repo, collection, rkey)
		resp := map[string]interface{}{
			"uri":   uri,
			"cid":   cid,
			"value": map[string]interface{}{"$type": collection, "text": "from appview"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer appview.Close()

	resolver := stubHostResolver{err: fmt.Errorf("PLC lookup failed")}
	fallbackClient := newTestXRPCClient(appview.URL)
	fetcher := NewPDSAwareRecordFetcher(resolver, fallbackClient)

	rec, err := fetcher.FetchRecord(context.Background(), "at://did:plc:fallback/app.bsky.feed.post/xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.ATCID != "bafyfallback" {
		t.Errorf("got ATCID=%q, want bafyfallback (should have used fallback)", rec.ATCID)
	}
}

func TestPDSAwareRecordFetcherNilResolverUsesFallback(t *testing.T) {
	appview := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repo := r.URL.Query().Get("repo")
		collection := r.URL.Query().Get("collection")
		rkey := r.URL.Query().Get("rkey")
		cid := "bafynilresolver"
		uri := fmt.Sprintf("at://%s/%s/%s", repo, collection, rkey)
		resp := map[string]interface{}{
			"uri":   uri,
			"cid":   cid,
			"value": map[string]interface{}{"$type": collection},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer appview.Close()

	fallbackClient := newTestXRPCClient(appview.URL)
	fetcher := NewPDSAwareRecordFetcher(nil, fallbackClient)

	rec, err := fetcher.FetchRecord(context.Background(), "at://did:plc:nil/app.bsky.feed.like/abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.ATCID != "bafynilresolver" {
		t.Errorf("got ATCID=%q, want bafynilresolver", rec.ATCID)
	}
}

func TestPDSAwareRecordFetcherBadATURI(t *testing.T) {
	fetcher := NewPDSAwareRecordFetcher(stubHostResolver{host: "http://localhost"}, nil)
	_, err := fetcher.FetchRecord(context.Background(), "not-a-valid-uri")
	if err == nil {
		t.Fatal("expected error for invalid AT URI")
	}
}

func TestPDSAwareRecordFetcherNilFetcher(t *testing.T) {
	var fetcher *PDSAwareRecordFetcher
	_, err := fetcher.FetchRecord(context.Background(), "at://did:plc:x/app.bsky.feed.post/y")
	if err == nil {
		t.Fatal("expected error for nil fetcher")
	}
}

// newTestXRPCClient creates a minimal xrpc-compatible client pointed at the given URL.
func newTestXRPCClient(host string) *testLexClient {
	return &testLexClient{host: host}
}

// testLexClient is a minimal lexutil.LexClient for test use that delegates
// to a real HTTP server via the standard xrpc client path.
type testLexClient struct {
	host string
}

func (c *testLexClient) LexDo(ctx context.Context, method string, inpenc string, endpoint string, params map[string]any, bodyObj any, out any) error {
	// Build query string from params to mimic xrpc behavior.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+"/xrpc/"+endpoint, nil)
	if err != nil {
		return err
	}
	q := req.URL.Query()
	for k, v := range params {
		q.Set(k, fmt.Sprintf("%v", v))
	}
	req.URL.RawQuery = q.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// ---------- XRPCRecordFetcher tests ----------

func TestXRPCRecordFetcherSuccessfulFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repo := r.URL.Query().Get("repo")
		collection := r.URL.Query().Get("collection")
		rkey := r.URL.Query().Get("rkey")
		cid := "bafyxrpc"
		uri := fmt.Sprintf("at://%s/%s/%s", repo, collection, rkey)
		resp := map[string]interface{}{
			"uri":   uri,
			"cid":   cid,
			"value": map[string]interface{}{"$type": collection, "text": "xrpc hello"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := newTestXRPCClient(srv.URL)
	fetcher := NewXRPCRecordFetcher(client)

	rec, err := fetcher.FetchRecord(context.Background(), "at://did:plc:xrpctest/app.bsky.feed.post/rk1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.ATDID != "did:plc:xrpctest" {
		t.Errorf("got ATDID=%q, want did:plc:xrpctest", rec.ATDID)
	}
	if rec.Collection != "app.bsky.feed.post" {
		t.Errorf("got Collection=%q, want app.bsky.feed.post", rec.Collection)
	}
	if rec.ATCID != "bafyxrpc" {
		t.Errorf("got ATCID=%q, want bafyxrpc", rec.ATCID)
	}
	if len(rec.RecordJSON) == 0 {
		t.Error("expected non-empty RecordJSON")
	}
}

func TestXRPCRecordFetcherNilReceiver(t *testing.T) {
	var fetcher *XRPCRecordFetcher
	_, err := fetcher.FetchRecord(context.Background(), "at://did:plc:x/app.bsky.feed.post/y")
	if err == nil {
		t.Fatal("expected error for nil receiver")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("expected nil error message, got %v", err)
	}
}

func TestXRPCRecordFetcherNilClient(t *testing.T) {
	fetcher := &XRPCRecordFetcher{client: nil}
	_, err := fetcher.FetchRecord(context.Background(), "at://did:plc:x/app.bsky.feed.post/y")
	if err == nil {
		t.Fatal("expected error for nil client")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("expected nil error message, got %v", err)
	}
}

func TestXRPCRecordFetcherInvalidURI(t *testing.T) {
	fetcher := NewXRPCRecordFetcher(newTestXRPCClient("http://localhost"))
	_, err := fetcher.FetchRecord(context.Background(), "not-a-valid-uri")
	if err == nil {
		t.Fatal("expected error for invalid AT URI")
	}
}

func TestXRPCRecordFetcherMissingComponents(t *testing.T) {
	fetcher := NewXRPCRecordFetcher(newTestXRPCClient("http://localhost"))
	_, err := fetcher.FetchRecord(context.Background(), "at://did:plc:x")
	if err == nil {
		t.Fatal("expected error for AT URI missing collection/rkey")
	}
}

// ---------- EnsureRecord direct tests ----------

func TestEnsureRecordEmptyATURI(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	logger := log.New(io.Discard, "", 0)
	resolver := NewATProtoDependencyResolver(database, logger, nil, nil)

	err = resolver.EnsureRecord(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty AT URI")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected empty error, got %v", err)
	}
}

func TestEnsureRecordWhitespaceATURI(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	logger := log.New(io.Discard, "", 0)
	resolver := NewATProtoDependencyResolver(database, logger, nil, nil)

	err = resolver.EnsureRecord(context.Background(), "   ")
	if err == nil {
		t.Fatal("expected error for whitespace AT URI")
	}
}

func TestEnsureRecordLocalResolved(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	// Seed a fully resolved message.
	if err := database.AddMessage(ctx, db.Message{
		ATURI:        "at://did:plc:alice/app.bsky.feed.post/resolved1",
		ATCID:        "bafy-resolved",
		SSBMsgRef:    "%resolved.sha256",
		ATDID:        "did:plc:alice",
		Type:         mapper.RecordTypePost,
		MessageState: db.MessageStatePublished,
		RawATJson:    `{"text":"resolved"}`,
		RawSSBJson:   `{"type":"post","text":"resolved"}`,
	}); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	logger := log.New(io.Discard, "", 0)
	fetcher := &stubRecordFetcher{records: map[string]FetchedRecord{}}
	var processCount int
	resolver := NewATProtoDependencyResolver(database, logger, fetcher, func(ctx context.Context, atDID, atURI, atCID, collection string, recordJSON []byte) error {
		processCount++
		return nil
	})

	err = resolver.EnsureRecord(ctx, "at://did:plc:alice/app.bsky.feed.post/resolved1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if processCount != 0 {
		t.Errorf("expected no reprocess for resolved record, got %d calls", processCount)
	}
	if fetcher.fetchCount("at://did:plc:alice/app.bsky.feed.post/resolved1") != 0 {
		t.Error("expected no remote fetch for locally resolved record")
	}
}

func TestEnsureRecordLocalReprocess(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	// Seed a deferred message with raw JSON (will trigger local_reprocess).
	if err := database.AddMessage(ctx, db.Message{
		ATURI:        "at://did:plc:alice/app.bsky.feed.post/deferred1",
		ATCID:        "bafy-deferred",
		SSBMsgRef:    "",
		ATDID:        "did:plc:alice",
		Type:         mapper.RecordTypePost,
		MessageState: db.MessageStateDeferred,
		RawATJson:    `{"text":"deferred post"}`,
		RawSSBJson:   `{"type":"post","text":"deferred post"}`,
	}); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	logger := log.New(io.Discard, "", 0)
	fetcher := &stubRecordFetcher{records: map[string]FetchedRecord{}}
	var processCount int
	resolver := NewATProtoDependencyResolver(database, logger, fetcher, func(ctx context.Context, atDID, atURI, atCID, collection string, recordJSON []byte) error {
		processCount++
		return nil
	})

	err = resolver.EnsureRecord(ctx, "at://did:plc:alice/app.bsky.feed.post/deferred1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if processCount != 1 {
		t.Errorf("expected exactly 1 reprocess call, got %d", processCount)
	}
	if fetcher.fetchCount("at://did:plc:alice/app.bsky.feed.post/deferred1") != 0 {
		t.Error("expected no remote fetch for local reprocess")
	}
}

func TestEnsureRecordLocalFailed(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddMessage(ctx, db.Message{
		ATURI:        "at://did:plc:alice/app.bsky.feed.post/failed1",
		ATCID:        "bafy-failed",
		SSBMsgRef:    "",
		ATDID:        "did:plc:alice",
		Type:         mapper.RecordTypePost,
		MessageState: db.MessageStateFailed,
		RawATJson:    `{"text":"failed post"}`,
		RawSSBJson:   `{"type":"post","text":"failed post"}`,
	}); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	logger := log.New(io.Discard, "", 0)
	fetcher := &stubRecordFetcher{records: map[string]FetchedRecord{}}
	var processCount int
	resolver := NewATProtoDependencyResolver(database, logger, fetcher, func(ctx context.Context, atDID, atURI, atCID, collection string, recordJSON []byte) error {
		processCount++
		return nil
	})

	err = resolver.EnsureRecord(ctx, "at://did:plc:alice/app.bsky.feed.post/failed1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if processCount != 0 {
		t.Errorf("expected no reprocess for failed record, got %d calls", processCount)
	}
}

func TestEnsureRecordRemoteFetchUnsupportedCollection(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	logger := log.New(io.Discard, "", 0)
	fetcher := &stubRecordFetcher{
		records: map[string]FetchedRecord{
			"at://did:plc:alice/app.bsky.feed.repost/rp1": {
				ATDID:      "did:plc:alice",
				ATURI:      "at://did:plc:alice/app.bsky.feed.repost/rp1",
				ATCID:      "bafy-repost",
				Collection: "app.bsky.feed.repost",
				RecordJSON: []byte(`{"subject":{"uri":"at://x","cid":"y"}}`),
			},
		},
	}

	resolver := NewATProtoDependencyResolver(database, logger, fetcher, func(ctx context.Context, atDID, atURI, atCID, collection string, recordJSON []byte) error {
		return nil
	})

	err = resolver.EnsureRecord(ctx, "at://did:plc:alice/app.bsky.feed.repost/rp1")
	if err == nil {
		t.Fatal("expected error for unsupported collection")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected unsupported error, got %v", err)
	}
}

func TestEnsureRecordRemoteFetchNilProcessCallback(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	logger := log.New(io.Discard, "", 0)
	fetcher := &stubRecordFetcher{
		records: map[string]FetchedRecord{
			"at://did:plc:alice/app.bsky.feed.post/p1": {
				ATDID:      "did:plc:alice",
				ATURI:      "at://did:plc:alice/app.bsky.feed.post/p1",
				ATCID:      "bafy-post",
				Collection: mapper.RecordTypePost,
				RecordJSON: []byte(`{"text":"hi"}`),
			},
		},
	}

	resolver := NewATProtoDependencyResolver(database, logger, fetcher, nil)

	err = resolver.EnsureRecord(ctx, "at://did:plc:alice/app.bsky.feed.post/p1")
	if err == nil {
		t.Fatal("expected error for nil process callback")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("expected nil callback error, got %v", err)
	}
}

func TestEnsureRecordNilResolver(t *testing.T) {
	var resolver *ATProtoDependencyResolver
	err := resolver.EnsureRecord(context.Background(), "at://did:plc:x/app.bsky.feed.post/y")
	if err == nil {
		t.Fatal("expected error for nil resolver")
	}
}

func TestEnsureRecordDependencyFetchLimitExceeded(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	logger := log.New(io.Discard, "", 0)

	// Build a fetcher that always returns valid posts.
	fetcher := &stubRecordFetcher{
		records: make(map[string]FetchedRecord),
	}

	var processCount int
	resolver := NewATProtoDependencyResolver(database, logger, fetcher, func(ctx context.Context, atDID, atURI, atCID, collection string, recordJSON []byte) error {
		processCount++
		return nil
	})

	// Set up dependency context with fetchedRecords already at limit.
	scope := &dependencyScope{
		rootDID:        "did:plc:root",
		rootURI:        "at://did:plc:root/app.bsky.feed.post/root",
		visitedRecords: make(map[string]struct{}),
		fetchedRecords: maxDependencyRecords, // already at limit
	}
	ctx = context.WithValue(ctx, dependencyFrameKey{}, &dependencyFrame{
		scope: scope,
		depth: 0,
	})

	// Populate the fetcher with a record to fetch.
	testURI := "at://did:plc:alice/app.bsky.feed.post/over-limit"
	fetcher.records[testURI] = FetchedRecord{
		ATDID:      "did:plc:alice",
		ATURI:      testURI,
		ATCID:      "bafy-over-limit",
		Collection: mapper.RecordTypePost,
		RecordJSON: []byte(`{"text":"over limit"}`),
	}

	err = resolver.EnsureRecord(ctx, testURI)
	if err == nil {
		t.Fatal("expected error for fetch limit exceeded")
	}
	if !strings.Contains(err.Error(), "fetch limit exceeded") {
		t.Errorf("expected fetch limit error, got %v", err)
	}
}

// ---------- Dependency context helper tests ----------

func TestWithDependencyReasonEmptyKey(t *testing.T) {
	ctx := context.Background()
	result := withDependencyReason(ctx, "")
	// Should return original context unchanged.
	if result != ctx {
		t.Error("expected same context for empty reason key")
	}
}

func TestWithDependencyReasonWhitespaceKey(t *testing.T) {
	ctx := context.Background()
	result := withDependencyReason(ctx, "   ")
	// After trimming, it's empty, so should return original context.
	if result != ctx {
		t.Error("expected same context for whitespace-only reason key")
	}
}

func TestBeginDependencyRecordDepthExceeded(t *testing.T) {
	scope := &dependencyScope{
		rootDID:        "did:plc:root",
		rootURI:        "at://did:plc:root/app.bsky.feed.post/root",
		visitedRecords: make(map[string]struct{}),
	}
	ctx := context.WithValue(context.Background(), dependencyFrameKey{}, &dependencyFrame{
		scope: scope,
		depth: maxDependencyDepth, // already at max
	})

	_, _, _, err := beginDependencyRecord(ctx, "at://did:plc:alice/app.bsky.feed.post/deep")
	if err == nil {
		t.Fatal("expected error for depth exceeded")
	}
	if !strings.Contains(err.Error(), "depth exceeded") {
		t.Errorf("expected depth exceeded error, got %v", err)
	}
}

func TestNoteDependencyFetchNoFrame(t *testing.T) {
	// When there's no dependency frame in context, noteDependencyFetch should succeed (no-op).
	err := noteDependencyFetch(context.Background())
	if err != nil {
		t.Fatalf("expected no error for missing frame, got %v", err)
	}
}
