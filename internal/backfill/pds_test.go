package backfill

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"

	appbsky "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/appbsky"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/identity"
	atrepo "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/repo"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/xrpc"
)

func TestDIDPDSResolverResolvePDSEndpoint(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := ""
			status := http.StatusOK
			switch req.URL.Path {
			case "/did:plc:valid":
				body = `{"id":"did:plc:valid","service":[{"id":"#atproto_pds","type":"AtprotoPersonalDataServer","serviceEndpoint":"https://pds.example.com/"}]}`
			case "/did:plc:missing":
				body = `{"id":"did:plc:missing","service":[]}`
			case "/did:plc:malformed":
				body = `{"id":"did:plc:malformed","service":[{"id":"#atproto_pds","type":"AtprotoPersonalDataServer","serviceEndpoint":"not a url"}]}`
			default:
				status = http.StatusNotFound
			}
			return &http.Response{
				StatusCode: status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}

	resolver := DIDPDSResolver{
		PLCURL:     "https://plc.example.test",
		HTTPClient: client,
	}

	got, err := resolver.ResolvePDSEndpoint(context.Background(), "did:plc:valid")
	if err != nil {
		t.Fatalf("resolve valid did: %v", err)
	}
	if got != "https://pds.example.com" {
		t.Fatalf("expected normalized pds endpoint, got %q", got)
	}

	_, err = resolver.ResolvePDSEndpoint(context.Background(), "did:plc:missing")
	if !errors.Is(err, ErrMissingPDSEndpoint) {
		t.Fatalf("expected missing pds endpoint error, got %v", err)
	}

	_, err = resolver.ResolvePDSEndpoint(context.Background(), "did:plc:malformed")
	if !errors.Is(err, ErrInvalidPDSEndpoint) {
		t.Fatalf("expected invalid pds endpoint error, got %v", err)
	}

	_, err = resolver.ResolvePDSEndpoint(context.Background(), "did:web:example.com")
	if !errors.Is(err, ErrUnsupportedDIDMethod) {
		t.Fatalf("expected unsupported did method error, got %v", err)
	}
}

func TestDIDPDSResolverResolvePDSEndpointError(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("network failure")
		}),
	}

	resolver := DIDPDSResolver{
		PLCURL:     "https://plc.example.test",
		HTTPClient: client,
	}

	_, err := resolver.ResolvePDSEndpoint(context.Background(), "did:plc:any")
	if err == nil {
		t.Fatal("expected error from network failure")
	}
}

func TestRunForDIDUsesResolvedPerDIDHosts(t *testing.T) {
	ctx := context.Background()
	processor := &recordingProcessor{}
	fetcher := &stubRepoFetcher{
		payloads: map[string][]byte{
			"did:plc:alpha": mustCreateRepoCAR(t, "did:plc:alpha"),
			"did:plc:beta":  mustCreateRepoCAR(t, "did:plc:beta"),
		},
	}
	resolver := &stubHostResolver{
		hosts: map[string]string{
			"did:plc:alpha": "https://alpha-pds.example.com",
			"did:plc:beta":  "https://beta-pds.example.com",
		},
	}

	alpha := RunForDID(ctx, "did:plc:alpha", SinceFilter{}, processor, nil, resolver, fetcher)
	beta := RunForDID(ctx, "did:plc:beta", SinceFilter{}, processor, nil, resolver, fetcher)

	if alpha.Status != StatusSuccess || beta.Status != StatusSuccess {
		t.Fatalf("expected success statuses, got alpha=%s err=%v beta=%s err=%v", alpha.Status, alpha.Err, beta.Status, beta.Err)
	}
	if alpha.PDSHost != "https://alpha-pds.example.com" || beta.PDSHost != "https://beta-pds.example.com" {
		t.Fatalf("unexpected resolved hosts: alpha=%q beta=%q", alpha.PDSHost, beta.PDSHost)
	}
	if alpha.Stats.Processed == 0 || beta.Stats.Processed == 0 {
		t.Fatalf("expected both backfills to process records, got alpha=%+v beta=%+v", alpha.Stats, beta.Stats)
	}
	if fetcher.hostForDID("did:plc:alpha") != "https://alpha-pds.example.com" || fetcher.hostForDID("did:plc:beta") != "https://beta-pds.example.com" {
		t.Fatalf("repo fetcher did not use per-DID hosts: %+v", fetcher.calls)
	}
	if len(processor.atURIs) < 2 {
		t.Fatalf("expected processor to receive records, got %d", len(processor.atURIs))
	}
}

func TestRunForDIDClassifiesAuthRequired(t *testing.T) {
	result := RunForDID(
		context.Background(),
		"did:plc:auth",
		SinceFilter{},
		&recordingProcessor{},
		nil,
		&stubHostResolver{hosts: map[string]string{"did:plc:auth": "https://auth.example.com"}},
		&stubRepoFetcher{errs: map[string]error{
			"did:plc:auth": &xrpc.Error{
				StatusCode: http.StatusUnauthorized,
				Wrapped:    &xrpc.XRPCError{ErrStr: "AuthMissing", Message: "Authentication Required"},
			},
		}},
	)

	if result.Status != StatusAuthRequired {
		t.Fatalf("expected auth_required, got %s err=%v", result.Status, result.Err)
	}
}

func TestRunForDIDClassifiesMalformedDIDDocument(t *testing.T) {
	result := RunForDID(
		context.Background(),
		"did:plc:broken",
		SinceFilter{},
		&recordingProcessor{},
		nil,
		&stubHostResolver{errs: map[string]error{
			"did:plc:broken": fmt.Errorf("%w: broken", ErrInvalidPDSEndpoint),
		}},
		&stubRepoFetcher{},
	)

	if result.Status != StatusMalformedDIDDoc {
		t.Fatalf("expected malformed_did_doc, got %s err=%v", result.Status, result.Err)
	}
}

type stubHostResolver struct {
	hosts map[string]string
	errs  map[string]error
}

func (r *stubHostResolver) ResolvePDSEndpoint(_ context.Context, did string) (string, error) {
	if err, ok := r.errs[did]; ok {
		return "", err
	}
	return r.hosts[did], nil
}

type stubRepoFetcher struct {
	payloads map[string][]byte
	errs     map[string]error
	calls    []repoFetchCall
}

type repoFetchCall struct {
	did  string
	host string
}

func (f *stubRepoFetcher) FetchRepo(_ context.Context, host, did string) ([]byte, error) {
	f.calls = append(f.calls, repoFetchCall{did: did, host: host})
	if err, ok := f.errs[did]; ok {
		return nil, err
	}
	return f.payloads[did], nil
}

func (f *stubRepoFetcher) hostForDID(did string) string {
	for _, call := range f.calls {
		if call.did == did {
			return call.host
		}
	}
	return ""
}

type recordingProcessor struct {
	atURIs []string
}

func (p *recordingProcessor) ProcessRecord(_ context.Context, _ string, atURI, _, _ string, _ []byte) error {
	p.atURIs = append(p.atURIs, atURI)
	return nil
}

type errorProcessor struct{}

func (p *errorProcessor) ProcessRecord(_ context.Context, _, _, _, _ string, _ []byte) error {
	return fmt.Errorf("process error")
}

type mapBlockstore struct {
	blocks map[string]blockformat.Block
}

func newMapBlockstore() *mapBlockstore {
	return &mapBlockstore{blocks: make(map[string]blockformat.Block)}
}

func (bs *mapBlockstore) Put(_ context.Context, blk blockformat.Block) error {
	bs.blocks[blk.Cid().KeyString()] = blk
	return nil
}

func (bs *mapBlockstore) Get(_ context.Context, c cid.Cid) (blockformat.Block, error) {
	blk, ok := bs.blocks[c.KeyString()]
	if !ok {
		return nil, &ipld.ErrNotFound{Cid: c}
	}
	return blk, nil
}

func mustCreateRepoCAR(t *testing.T, did string) []byte {
	t.Helper()

	ctx := context.Background()
	bs := newMapBlockstore()
	rr := atrepo.NewRepo(did, bs)
	if _, _, err := rr.CreateRecord(ctx, "app.bsky.feed.post", &appbsky.FeedPost{
		LexiconTypeID: "app.bsky.feed.post",
		Text:          "hello from backfill test",
		CreatedAt:     "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("create record: %v", err)
	}

	_, _, err := rr.Commit(ctx, func(context.Context, string, []byte) ([]byte, error) {
		return []byte("test-signature"), nil
	})
	if err != nil {
		t.Fatalf("commit repo: %v", err)
	}

	buf := new(bytes.Buffer)
	if err := rr.WriteCAR(buf); err != nil {
		t.Fatalf("write car: %v", err)
	}
	return buf.Bytes()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestFixedHostResolver(t *testing.T) {
	resolver := FixedHostResolver{Host: "https://pds.example.com"}
	got, err := resolver.ResolvePDSEndpoint(context.Background(), "any-did")
	if err != nil {
		t.Fatalf("resolve endpoint: %v", err)
	}
	if got != "https://pds.example.com" {
		t.Fatalf("expected https://pds.example.com, got %q", got)
	}
}

func TestConfiguredHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 5 * time.Second}
	got := configuredHTTPClient(custom)
	if got != custom {
		t.Fatalf("expected custom client")
	}
	got = configuredHTTPClient(nil)
	if got == nil {
		t.Fatalf("expected non-nil default client")
	}
	if got.Timeout != 10*time.Second {
		t.Fatalf("expected 10s timeout, got %v", got.Timeout)
	}
}

func TestNormalizeServiceEndpoint(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"https://pds.example.com", "https://pds.example.com", false},
		{"http://pds.example.com", "http://pds.example.com", false},
		{"  https://pds.example.com  ", "https://pds.example.com", false},
		{"https://pds.example.com/", "https://pds.example.com", false},
		{"https://pds.example.com/some/path", "https://pds.example.com/some/path", false},
		{"https://pds.example.com?query=1", "https://pds.example.com", false},
		{"https://pds.example.com#fragment", "https://pds.example.com", false},
		{"", "", true},
		{"   ", "", true},
		{"ftp://pds.example.com", "", true},
		{"://pds.example.com", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := NormalizeServiceEndpoint(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NormalizeServiceEndpoint(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("NormalizeServiceEndpoint(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestClassifyDIDResult(t *testing.T) {
	tests := []struct {
		err      error
		expected DIDStatus
	}{
		{nil, StatusSuccess},
		{ErrUnsupportedDIDMethod, StatusUnsupportedDID},
		{ErrMissingPDSEndpoint, StatusMalformedDIDDoc},
		{ErrInvalidPDSEndpoint, StatusMalformedDIDDoc},
		{errors.New("some other error"), StatusTransportError},
	}

	for _, tt := range tests {
		t.Run(string(tt.expected), func(t *testing.T) {
			got := classifyDIDResult(tt.err)
			if got != tt.expected {
				t.Errorf("classifyDIDResult(%v) = %s, want %s", tt.err, got, tt.expected)
			}
		})
	}
}

func TestClassifyDIDResultXRPCErrors(t *testing.T) {
	tests := []struct {
		statusCode int
		body       string
		expected   DIDStatus
	}{
		{http.StatusUnauthorized, "AuthMissing", StatusAuthRequired},
		{http.StatusForbidden, "InvalidSignature", StatusAuthRequired},
		{http.StatusNotFound, "RecordNotFound", StatusNotFound},
		{http.StatusInternalServerError, "InternalError", StatusTransportError},
	}

	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			err := &xrpc.Error{
				StatusCode: tt.statusCode,
				Wrapped:    &xrpc.XRPCError{ErrStr: tt.body, Message: "test"},
			}
			got := classifyDIDResult(err)
			if got != tt.expected {
				t.Errorf("classifyDIDResult(xrpc %d) = %s, want %s", tt.statusCode, got, tt.expected)
			}
		})
	}
}

func TestClassifyDIDResultNotFoundWithBadXRPCBody(t *testing.T) {
	err := &xrpc.Error{
		StatusCode: http.StatusNotFound,
		Wrapped:    &xrpc.XRPCError{ErrStr: "NotFound", Message: "failed to decode xrpc error message"},
	}
	got := classifyDIDResult(err)
	if got != StatusTransportError {
		t.Errorf("classifyDIDResult() = %s, want StatusTransportError for bad xrpc body", got)
	}
}

func TestRunForDIDWithNilResolver(t *testing.T) {
	result := RunForDID(
		context.Background(),
		"did:plc:test",
		SinceFilter{},
		&recordingProcessor{},
		nil,
		nil,
		nil,
	)
	if result.Status != StatusTransportError {
		t.Fatalf("expected StatusTransportError for nil resolver, got %s", result.Status)
	}
}

func TestRunForDIDWithNilFetcher(t *testing.T) {
	resolver := &stubHostResolver{hosts: map[string]string{"did:plc:test": "https://pds.example.com"}}
	result := RunForDID(
		context.Background(),
		"did:plc:test",
		SinceFilter{},
		&recordingProcessor{},
		nil,
		resolver,
		nil,
	)
	if result.Status != StatusTransportError {
		t.Fatalf("expected StatusTransportError for nil fetcher, got %s", result.Status)
	}
}

func TestClassifyDIDResultIdentityErrNotFound(t *testing.T) {
	result := classifyDIDResult(identity.ErrDIDNotFound)
	if result != StatusNotFound {
		t.Errorf("classifyDIDResult(identity.ErrDIDNotFound) = %s, want StatusNotFound", result)
	}
}

func TestStubHostResolverNotFound(t *testing.T) {
	resolver := &stubHostResolver{errs: map[string]error{
		"did:plc:notfound": identity.ErrDIDNotFound,
	}}
	host, err := resolver.ResolvePDSEndpoint(context.Background(), "did:plc:notfound")
	if err == nil {
		t.Fatalf("expected error, got host=%q", host)
	}
	if !errors.Is(err, identity.ErrDIDNotFound) {
		t.Errorf("expected identity.ErrDIDNotFound, got %v", err)
	}
}

func TestXRPCRepoFetcher(t *testing.T) {
	var called bool
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			if req.URL.Path != "/xrpc/com.atproto.sync.getRepo" {
				t.Errorf("unexpected path: %s", req.URL.Path)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}

	fetcher := XRPCRepoFetcher{HTTPClient: client}
	_, err := fetcher.FetchRepo(context.Background(), "https://pds.example.com", "did:plc:test")
	if err != nil {
		t.Fatalf("fetch repo: %v", err)
	}
	if !called {
		t.Fatal("expected HTTP client to be called")
	}
}

func TestDIDPDSResolverWithBadDID(t *testing.T) {
	resolver := DIDPDSResolver{
		PLCURL:     "https://plc.example.test",
		HTTPClient: &http.Client{},
	}
	_, err := resolver.ResolvePDSEndpoint(context.Background(), "not-a-did")
	if !errors.Is(err, ErrUnsupportedDIDMethod) {
		t.Fatalf("expected ErrUnsupportedDIDMethod, got %v", err)
	}
}

// Deleted redundant TestExtractPDSEndpointNilDoc

func TestExtractPDSEndpointEmptyEndpoint(t *testing.T) {
	// The doc structure needs to match identity.DIDDocument
	// We'll test this via DIDPDSResolver instead since it creates the document
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Valid DID document but with empty service endpoint
		w.Write([]byte(`{
			"id": "did:plc:test",
			"service": [
				{
					"id": "#atproto_pds",
					"type": "AtprotoPersonalDataServer",
					"serviceEndpoint": "   "
				}
			]
		}`))
	}))
	defer server.Close()

	resolver := DIDPDSResolver{
		PLCURL:     server.URL,
		HTTPClient: server.Client(),
	}
	_, err := resolver.ResolvePDSEndpoint(context.Background(), "did:plc:test")
	if !errors.Is(err, ErrMissingPDSEndpoint) {
		t.Fatalf("expected ErrMissingPDSEndpoint for empty endpoint, got %v", err)
	}
}

func TestNormalizeServiceEndpointErrors(t *testing.T) {
	_, err := NormalizeServiceEndpoint("   ")
	if !errors.Is(err, ErrInvalidPDSEndpoint) {
		t.Errorf("expected ErrInvalidPDSEndpoint for empty")
	}

	_, err = NormalizeServiceEndpoint("ftp://example.com")
	if !errors.Is(err, ErrInvalidPDSEndpoint) {
		t.Errorf("expected ErrInvalidPDSEndpoint for ftp")
	}

	_, err = NormalizeServiceEndpoint("http://")
	if !errors.Is(err, ErrInvalidPDSEndpoint) {
		t.Errorf("expected ErrInvalidPDSEndpoint for no host")
	}

	// Test NormalizeServiceEndpoint with spaces and trailing slash
	got, err := NormalizeServiceEndpoint("  https://example.com/  ")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got != "https://example.com" {
		t.Errorf("expected https://example.com, got %q", got)
	}
}

func TestRunForDIDWithSequenceNotice(t *testing.T) {
	since, err := ParseSince("42")
	if err != nil {
		t.Fatalf("parse since: %v", err)
	}
	result := RunForDID(
		context.Background(),
		"did:plc:seq",
		since,
		&recordingProcessor{},
		nil,
		&stubHostResolver{hosts: map[string]string{"did:plc:seq": "https://pds.example.com"}},
		&stubRepoFetcher{payloads: map[string][]byte{"did:plc:seq": mustCreateRepoCAR(t, "did:plc:seq")}},
	)
	if result.Status != StatusSuccess {
		t.Fatalf("expected success, got %s err=%v", result.Status, result.Err)
	}
	if result.Stats.Processed == 0 {
		t.Fatalf("expected processed records")
	}
}

func TestRunForDIDWithInvalidCARBytes(t *testing.T) {
	result := RunForDID(
		context.Background(),
		"did:plc:badcar",
		SinceFilter{},
		&recordingProcessor{},
		nil,
		&stubHostResolver{hosts: map[string]string{"did:plc:badcar": "https://pds.example.com"}},
		&stubRepoFetcher{payloads: map[string][]byte{"did:plc:badcar": []byte("not a valid car")}},
	)
	if result.Status != StatusTransportError {
		t.Fatalf("expected transport_error for invalid CAR, got %s", result.Status)
	}
	if result.Err == nil {
		t.Fatalf("expected error for invalid CAR bytes")
	}
}

func TestRunForDIDWithProcessorError(t *testing.T) {
	result := RunForDID(
		context.Background(),
		"did:plc:procerr",
		SinceFilter{},
		&errorProcessor{},
		nil,
		&stubHostResolver{hosts: map[string]string{"did:plc:procerr": "https://pds.example.com"}},
		&stubRepoFetcher{payloads: map[string][]byte{"did:plc:procerr": mustCreateRepoCAR(t, "did:plc:procerr")}},
	)
	if result.Status != StatusSuccess {
		t.Fatalf("expected success (errors are logged not fatal), got %s err=%v", result.Status, result.Err)
	}
	if result.Stats.Errors == 0 {
		t.Fatalf("expected error count > 0")
	}
}

func TestRunForDIDWithSinceFilterExclusion(t *testing.T) {
	// mustCreateRepoCAR creates a post with CreatedAt "2026-01-01T00:00:00Z"
	// Set since to a future date so the record is excluded
	since, err := ParseSince("2027-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("parse since: %v", err)
	}
	result := RunForDID(
		context.Background(),
		"did:plc:filtered",
		since,
		&recordingProcessor{},
		nil,
		&stubHostResolver{hosts: map[string]string{"did:plc:filtered": "https://pds.example.com"}},
		&stubRepoFetcher{payloads: map[string][]byte{"did:plc:filtered": mustCreateRepoCAR(t, "did:plc:filtered")}},
	)
	if result.Status != StatusSuccess {
		t.Fatalf("expected success, got %s err=%v", result.Status, result.Err)
	}
	if result.Stats.Skipped == 0 {
		t.Fatalf("expected skipped records from since filter")
	}
	if result.Stats.Processed != 0 {
		t.Fatalf("expected no processed records, got %d", result.Stats.Processed)
	}
}

func TestExtractPDSEndpointNonMatchingService(t *testing.T) {
	doc := &identity.DIDDocument{
		Service: []identity.DocService{
			{
				ID:              "#other_service",
				Type:            "SomeOtherService",
				ServiceEndpoint: "https://other.example.com",
			},
		},
	}
	_, err := extractPDSEndpoint(doc)
	if !errors.Is(err, ErrMissingPDSEndpoint) {
		t.Fatalf("expected ErrMissingPDSEndpoint for non-matching service, got %v", err)
	}
}

func TestExtractPDSEndpointServiceWithoutHash(t *testing.T) {
	doc := &identity.DIDDocument{
		Service: []identity.DocService{
			{
				ID:              "atproto_pds",
				Type:            "AtprotoPersonalDataServer",
				ServiceEndpoint: "https://pds.example.com",
			},
		},
	}
	_, err := extractPDSEndpoint(doc)
	if !errors.Is(err, ErrMissingPDSEndpoint) {
		t.Fatalf("expected ErrMissingPDSEndpoint for service ID without #, got %v", err)
	}
}

func TestExtractPDSEndpointMixedServices(t *testing.T) {
	doc := &identity.DIDDocument{
		Service: []identity.DocService{
			{
				ID:              "#other",
				Type:            "SomeOtherService",
				ServiceEndpoint: "https://other.example.com",
			},
			{
				ID:              "#atproto_pds",
				Type:            "AtprotoPersonalDataServer",
				ServiceEndpoint: "https://pds.example.com",
			},
		},
	}
	endpoint, err := extractPDSEndpoint(doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if endpoint != "https://pds.example.com" {
		t.Fatalf("expected https://pds.example.com, got %q", endpoint)
	}
}

func TestRunForDIDWithUnsupportedCollection(t *testing.T) {
	ctx := context.Background()
	did := "did:plc:unsupported"
	bs := newMapBlockstore()
	rr := atrepo.NewRepo(did, bs)
	// Create unsupported record
	rr.CreateRecord(ctx, "app.bsky.feed.repost", &appbsky.FeedRepost{
		LexiconTypeID: "app.bsky.feed.repost",
		Subject:       &appbsky.RepoStrongRef{Uri: "at://did:plc:x/app.bsky.feed.post/y", Cid: "abc"},
		CreatedAt:     "2026-01-01T00:00:00Z",
	})

	_, _, _ = rr.Commit(ctx, func(ctx context.Context, did string, data []byte) ([]byte, error) { return []byte("sig"), nil })

	buf := new(bytes.Buffer)
	if err := rr.WriteCAR(buf); err != nil {
		t.Fatalf("write car: %v", err)
	}

	result := RunForDID(ctx, did, SinceFilter{}, &recordingProcessor{}, nil,
		&stubHostResolver{hosts: map[string]string{did: "http://pds"}},
		&stubRepoFetcher{payloads: map[string][]byte{did: buf.Bytes()}})

	if result.Stats.Skipped == 0 {
		t.Error("expected skipped record for unsupported collection")
	}
}

func TestExtractPDSEndpointNilDoc(t *testing.T) {
	_, err := extractPDSEndpoint(nil)
	if !errors.Is(err, ErrMissingPDSEndpoint) {
		t.Fatalf("expected ErrMissingPDSEndpoint for nil doc, got %v", err)
	}
}

func TestClassifyDIDResult_XRPCError(t *testing.T) {
	tests := []struct {
		status int
		want   DIDStatus
	}{
		{http.StatusUnauthorized, StatusAuthRequired},
		{http.StatusNotFound, StatusNotFound},
		{http.StatusInternalServerError, StatusTransportError},
	}
	for _, tt := range tests {
		got := classifyDIDResult(&xrpc.Error{StatusCode: tt.status})
		if got != tt.want {
			t.Errorf("status %d: got %s, want %s", tt.status, got, tt.want)
		}
	}
}

func TestClassifyDIDResult_NotFoundWithDecodeError(t *testing.T) {
	err := &xrpc.Error{
		StatusCode: http.StatusNotFound,
		Wrapped:    fmt.Errorf("failed to decode xrpc error message"),
	}
	got := classifyDIDResult(err)
	if got != StatusTransportError {
		t.Errorf("expected StatusTransportError for decode error, got %s", got)
	}
}

func TestResolvePDSEndpointInvalidDID(t *testing.T) {
	resolver := DIDPDSResolver{}
	_, err := resolver.ResolvePDSEndpoint(context.Background(), "invalid-did")
	if !errors.Is(err, ErrUnsupportedDIDMethod) {
		t.Fatalf("expected ErrUnsupportedDIDMethod, got %v", err)
	}
}

func TestResolvePDSEndpointNonPlcDID(t *testing.T) {
	resolver := DIDPDSResolver{}
	_, err := resolver.ResolvePDSEndpoint(context.Background(), "did:web:example.com")
	if !errors.Is(err, ErrUnsupportedDIDMethod) {
		t.Fatalf("expected ErrUnsupportedDIDMethod for did:web, got %v", err)
	}
}

// Deleted redundant TestExtractPDSEndpointNilDoc

func TestRunForDIDWithMalformedRecord(t *testing.T) {
	ctx := context.Background()
	did := "did:plc:malformed"
	carBytes := mustCreateRepoCAR(t, did)
	if len(carBytes) < 16 {
		t.Fatalf("expected non-trivial car payload")
	}
	corrupt := append([]byte(nil), carBytes...)
	for i := len(corrupt) - 8; i < len(corrupt); i++ {
		corrupt[i] = 0xff
	}

	result := RunForDID(ctx, did, SinceFilter{}, &recordingProcessor{}, nil,
		&stubHostResolver{hosts: map[string]string{did: "http://pds"}},
		&stubRepoFetcher{payloads: map[string][]byte{did: corrupt}})

	if result.Err == nil && result.Stats.Errors == 0 {
		t.Error("expected repo-level or record-level error for malformed repo payload")
	}
}
