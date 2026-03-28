package backfill

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	indigorepo "github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/xrpc"
	blockformat "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"
	car "github.com/ipld/go-car"
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
	rr := indigorepo.NewRepo(ctx, did, bs)
	if _, _, err := rr.CreateRecord(ctx, "app.bsky.feed.post", &appbsky.FeedPost{
		LexiconTypeID: "app.bsky.feed.post",
		Text:          "hello from backfill test",
		CreatedAt:     "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("create record: %v", err)
	}

	root, _, err := rr.Commit(ctx, func(context.Context, string, []byte) ([]byte, error) {
		return []byte("test-signature"), nil
	})
	if err != nil {
		t.Fatalf("commit repo: %v", err)
	}

	buf := new(bytes.Buffer)
	if _, err := writeCARHeader(buf, root); err != nil {
		t.Fatalf("write car header: %v", err)
	}
	for _, blk := range bs.blocks {
		if _, err := ldWrite(buf, blk.Cid().Bytes(), blk.RawData()); err != nil {
			t.Fatalf("write car block: %v", err)
		}
	}
	return buf.Bytes()
}

func writeCARHeader(w io.Writer, root cid.Cid) (int64, error) {
	buf := new(bytes.Buffer)
	if err := car.WriteHeader(&car.CarHeader{
		Roots:   []cid.Cid{root},
		Version: 1,
	}, buf); err != nil {
		return 0, err
	}
	n, err := w.Write(buf.Bytes())
	return int64(n), err
}

func ldWrite(w io.Writer, parts ...[]byte) (int64, error) {
	var total uint64
	for _, part := range parts {
		total += uint64(len(part))
	}

	var prefix [binary.MaxVarintLen64]byte
	prefixLen := binary.PutUvarint(prefix[:], total)
	written, err := w.Write(prefix[:prefixLen])
	if err != nil {
		return 0, err
	}

	for _, part := range parts {
		n, err := w.Write(part)
		written += n
		if err != nil {
			return int64(written), err
		}
	}
	return int64(written), nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
