package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
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
