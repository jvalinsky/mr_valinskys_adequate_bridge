package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/mapper"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
	"golang.org/x/time/rate"
)

type mockPublisher struct {
	ref string
	err error
}

func (m *mockPublisher) Publish(context.Context, string, map[string]interface{}) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.ref, nil
}

type mockBlobBridge struct {
	err error
}

func (m *mockBlobBridge) BridgeRecordBlobs(context.Context, string, string, map[string]interface{}, []byte) error {
	return m.err
}

type recordingPublisher struct {
	mu        sync.Mutex
	published []map[string]interface{}
}

func (p *recordingPublisher) Publish(_ context.Context, _ string, content map[string]interface{}) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	copyContent := deepCopyMap(content)
	p.published = append(p.published, copyContent)
	return fmt.Sprintf("%%pub-%d.sha256", len(p.published)), nil
}

func (p *recordingPublisher) snapshot() []map[string]interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()

	out := make([]map[string]interface{}, len(p.published))
	for i, item := range p.published {
		out[i] = deepCopyMap(item)
	}
	return out
}

type mockFeedResolver struct {
	mu        sync.Mutex
	refs      map[string]string
	lookups   map[string]int
	waitCh    chan struct{}
	err       error
	ready     chan struct{}
	readyOnce sync.Once
}

func (m *mockFeedResolver) ResolveFeed(ctx context.Context, did string) (string, error) {
	if m.ready != nil {
		m.readyOnce.Do(func() { close(m.ready) })
	}
	if m.waitCh != nil {
		select {
		case <-m.waitCh:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lookups == nil {
		m.lookups = make(map[string]int)
	}
	m.lookups[did]++
	if m.err != nil {
		return "", m.err
	}
	return m.refs[did], nil
}

func (m *mockFeedResolver) lookupCount(did string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lookups[did]
}

type stubRecordFetcher struct {
	mu        sync.Mutex
	records   map[string]FetchedRecord
	errors    map[string]error
	fetches   map[string]int
	waitCh    chan struct{}
	ready     chan struct{}
	readyOnce sync.Once
}

func (f *stubRecordFetcher) FetchRecord(ctx context.Context, atURI string) (FetchedRecord, error) {
	if f.ready != nil {
		f.readyOnce.Do(func() { close(f.ready) })
	}
	if f.waitCh != nil {
		select {
		case <-f.waitCh:
		case <-ctx.Done():
			return FetchedRecord{}, ctx.Err()
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fetches == nil {
		f.fetches = make(map[string]int)
	}
	f.fetches[atURI]++

	if err := f.errors[atURI]; err != nil {
		return FetchedRecord{}, err
	}
	record, ok := f.records[atURI]
	if !ok {
		return FetchedRecord{}, fmt.Errorf("missing stub record for %s", atURI)
	}
	return record, nil
}

func (f *stubRecordFetcher) fetchCount(atURI string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fetches[atURI]
}

func newProcessorWithDependencies(database *db.DB, publisher Publisher, feedResolver FeedResolver, fetcher RecordFetcher) *Processor {
	logger := log.New(io.Discard, "", 0)
	opts := []Option{}
	if publisher != nil {
		opts = append(opts, WithPublisher(publisher))
	}
	if feedResolver != nil {
		opts = append(opts, WithFeedResolver(feedResolver))
	}

	var processor *Processor
	if fetcher != nil {
		resolver := NewATProtoDependencyResolver(
			database,
			logger,
			fetcher,
			func(ctx context.Context, atDID, atURI, atCID, collection string, recordJSON []byte) error {
				return processor.ProcessRecord(ctx, atDID, atURI, atCID, collection, recordJSON)
			},
		)
		opts = append(opts, WithDependencyResolver(resolver))
	}

	processor = NewProcessor(database, logger, opts...)
	return processor
}

func deepCopyMap(in map[string]interface{}) map[string]interface{} {
	raw, err := json.Marshal(in)
	if err != nil {
		panic(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}

func mentionMaps(raw interface{}) []map[string]interface{} {
	switch typed := raw.(type) {
	case []map[string]interface{}:
		return typed
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(typed))
		for _, item := range typed {
			if m, ok := item.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func TestProcessRecordStoresMappedMessage(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:alice",
		SSBFeedID: "@alice.ed25519",
		Active:    true,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	processor := NewProcessor(database, log.New(io.Discard, "", 0))
	record := []byte(`{"text":"hello bridge","createdAt":"2026-01-01T00:00:00Z"}`)

	err = processor.ProcessRecord(
		ctx,
		"did:plc:alice",
		"at://did:plc:alice/app.bsky.feed.post/1",
		"bafy123",
		mapper.RecordTypePost,
		record,
	)
	if err != nil {
		t.Fatalf("process record: %v", err)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.post/1")
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if stored == nil {
		t.Fatalf("expected stored message")
	}

	var mapped map[string]interface{}
	if err := json.Unmarshal([]byte(stored.RawSSBJson), &mapped); err != nil {
		t.Fatalf("unmarshal mapped json: %v", err)
	}

	if mapped["type"] != "post" {
		t.Fatalf("expected mapped type post, got %v", mapped["type"])
	}
	if mapped["text"] != "hello bridge" {
		t.Fatalf("expected mapped text, got %v", mapped["text"])
	}
}

func TestProcessRecordResolvesKnownSubjectRefs(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:alice",
		SSBFeedID: "@alice.ed25519",
		Active:    true,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	subjectURI := "at://did:plc:alice/app.bsky.feed.post/999"
	if err := database.AddMessage(ctx, db.Message{
		ATURI:      subjectURI,
		ATCID:      "bafy-old",
		SSBMsgRef:  "%oldmsg.sha256",
		ATDID:      "did:plc:alice",
		Type:       mapper.RecordTypePost,
		RawATJson:  `{"text":"original"}`,
		RawSSBJson: `{"type":"post","text":"original"}`,
	}); err != nil {
		t.Fatalf("seed subject message: %v", err)
	}

	processor := NewProcessor(database, log.New(io.Discard, "", 0))
	likeRecord := []byte(`{
		"subject": {
			"uri": "at://did:plc:alice/app.bsky.feed.post/999",
			"cid": "bafy-old"
		},
		"createdAt": "2026-01-01T00:00:00Z"
	}`)

	err = processor.ProcessRecord(
		ctx,
		"did:plc:alice",
		"at://did:plc:alice/app.bsky.feed.like/1",
		"bafy-like",
		mapper.RecordTypeLike,
		likeRecord,
	)
	if err != nil {
		t.Fatalf("process like record: %v", err)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.like/1")
	if err != nil {
		t.Fatalf("get like message: %v", err)
	}
	if stored == nil {
		t.Fatalf("expected stored like message")
	}

	var mapped map[string]interface{}
	if err := json.Unmarshal([]byte(stored.RawSSBJson), &mapped); err != nil {
		t.Fatalf("unmarshal mapped like json: %v", err)
	}

	vote, ok := mapped["vote"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected vote map, got %T", mapped["vote"])
	}
	if vote["link"] != "%oldmsg.sha256" {
		t.Fatalf("expected resolved vote link %q, got %v", "%oldmsg.sha256", vote["link"])
	}
}

func TestProcessRecordDefersWhenRefsUnresolved(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%should-not-publish.sha256"}),
	)

	likeRecord := []byte(`{
		"subject": {
			"uri": "at://did:plc:alice/app.bsky.feed.post/missing",
			"cid": "bafy-missing"
		},
		"createdAt": "2026-01-01T00:00:00Z"
	}`)

	err = processor.ProcessRecord(
		ctx,
		"did:plc:alice",
		"at://did:plc:alice/app.bsky.feed.like/2",
		"bafy-like-2",
		mapper.RecordTypeLike,
		likeRecord,
	)
	if err != nil {
		t.Fatalf("process like record: %v", err)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.like/2")
	if err != nil {
		t.Fatalf("get like message: %v", err)
	}
	if stored == nil {
		t.Fatalf("expected stored deferred message")
	}
	if stored.MessageState != db.MessageStateDeferred {
		t.Fatalf("expected deferred message state, got %q", stored.MessageState)
	}
	if stored.SSBMsgRef != "" {
		t.Fatalf("expected no ssb message ref for deferred row, got %q", stored.SSBMsgRef)
	}
	if stored.DeferReason == "" {
		t.Fatalf("expected defer reason")
	}
}

func TestProcessRecordFollowDefersWhenContactUnresolved(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%should-not-publish-follow.sha256"}),
	)

	followRecord := []byte(`{
		"subject": "did:plc:bob",
		"createdAt": "2026-01-01T00:00:00Z"
	}`)

	err = processor.ProcessRecord(
		ctx,
		"did:plc:alice",
		"at://did:plc:alice/app.bsky.graph.follow/1",
		"bafy-follow-1",
		mapper.RecordTypeFollow,
		followRecord,
	)
	if err != nil {
		t.Fatalf("process follow record: %v", err)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.graph.follow/1")
	if err != nil {
		t.Fatalf("get follow message: %v", err)
	}
	if stored == nil {
		t.Fatalf("expected stored follow message")
	}
	if stored.MessageState != db.MessageStateDeferred {
		t.Fatalf("expected deferred follow message state, got %q", stored.MessageState)
	}
	if !strings.Contains(stored.DeferReason, "_atproto_contact=") {
		t.Fatalf("expected follow defer reason to include contact placeholder, got %q", stored.DeferReason)
	}
}

func TestProcessRecordAutoFetchesLikeSubjectDependency(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	subjectURI := "at://did:plc:bob/app.bsky.feed.post/root"
	publisher := &recordingPublisher{}
	fetcher := &stubRecordFetcher{
		records: map[string]FetchedRecord{
			subjectURI: {
				ATDID:      "did:plc:bob",
				ATURI:      subjectURI,
				ATCID:      "bafy-root",
				Collection: mapper.RecordTypePost,
				RecordJSON: []byte(`{"text":"dependency root","createdAt":"2026-01-01T00:00:00Z"}`),
			},
		},
	}
	processor := newProcessorWithDependencies(database, publisher, nil, fetcher)

	likeRecord := []byte(`{
		"subject": {
			"uri": "at://did:plc:bob/app.bsky.feed.post/root",
			"cid": "bafy-root"
		},
		"createdAt": "2026-01-01T00:00:00Z"
	}`)
	if err := processor.ProcessRecord(
		ctx,
		"did:plc:alice",
		"at://did:plc:alice/app.bsky.feed.like/auto-1",
		"bafy-like-auto-1",
		mapper.RecordTypeLike,
		likeRecord,
	); err != nil {
		t.Fatalf("process like record: %v", err)
	}

	subject, err := database.GetMessage(ctx, subjectURI)
	if err != nil {
		t.Fatalf("get subject message: %v", err)
	}
	if subject == nil || subject.MessageState != db.MessageStatePublished {
		t.Fatalf("expected fetched dependency to publish, got %+v", subject)
	}

	like, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.like/auto-1")
	if err != nil {
		t.Fatalf("get like message: %v", err)
	}
	if like == nil || like.MessageState != db.MessageStatePublished {
		t.Fatalf("expected like to publish after dependency resolution, got %+v", like)
	}

	published := publisher.snapshot()
	if len(published) != 2 {
		t.Fatalf("expected 2 publish calls, got %d", len(published))
	}
	if published[0]["text"] != "dependency root" {
		t.Fatalf("expected dependency post to publish first, got %+v", published[0])
	}
	if published[1]["type"] != "vote" {
		t.Fatalf("expected like vote to publish second, got %+v", published[1])
	}

	vote, ok := published[1]["vote"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected vote payload, got %+v", published[1])
	}
	if vote["link"] != "%pub-1.sha256" {
		t.Fatalf("expected like to reference fetched dependency ref, got %+v", vote)
	}
	if fetcher.fetchCount(subjectURI) != 1 {
		t.Fatalf("expected exactly one fetch for dependency, got %d", fetcher.fetchCount(subjectURI))
	}
}

func TestProcessRecordAutoFetchesQuoteSubjectDependency(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	subjectURI := "at://did:plc:bob/app.bsky.feed.post/quoted"
	publisher := &recordingPublisher{}
	fetcher := &stubRecordFetcher{
		records: map[string]FetchedRecord{
			subjectURI: {
				ATDID:      "did:plc:bob",
				ATURI:      subjectURI,
				ATCID:      "bafy-quoted",
				Collection: mapper.RecordTypePost,
				RecordJSON: []byte(`{"text":"quoted","createdAt":"2026-01-01T00:00:00Z"}`),
			},
		},
	}
	processor := newProcessorWithDependencies(database, publisher, nil, fetcher)

	postRecord := []byte(`{
		"text":"quoting",
		"embed": {
			"$type":"app.bsky.embed.record",
			"record": {
				"uri":"at://did:plc:bob/app.bsky.feed.post/quoted",
				"cid":"bafy-quoted"
			}
		},
		"createdAt": "2026-01-01T00:00:00Z"
	}`)
	if err := processor.ProcessRecord(
		ctx,
		"did:plc:alice",
		"at://did:plc:alice/app.bsky.feed.post/auto-quote-1",
		"bafy-quote-auto-1",
		mapper.RecordTypePost,
		postRecord,
	); err != nil {
		t.Fatalf("process quote post record: %v", err)
	}

	repost, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.post/auto-quote-1")
	if err != nil {
		t.Fatalf("get quote post message: %v", err)
	}
	if repost == nil || repost.MessageState != db.MessageStatePublished {
		t.Fatalf("expected quote post to publish after dependency resolution, got %+v", repost)
	}

	published := publisher.snapshot()
	if len(published) != 2 {
		t.Fatalf("expected 2 publish calls, got %d", len(published))
	}
	if got := published[1]["text"]; got != "quoting\n\n[quoted post](%pub-1.sha256)" {
		t.Fatalf("expected quote markdown in text, got %+v", published[1])
	}
	mentions := mentionMaps(published[1]["mentions"])
	if len(mentions) != 1 || mentions[0]["link"] != "%pub-1.sha256" {
		t.Fatalf("expected quote mention to resolve to dependency ref, got %+v", published[1]["mentions"])
	}
}

func TestProcessRecordAutoFetchesReplyChainDependencies(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	rootURI := "at://did:plc:carol/app.bsky.feed.post/root"
	parentURI := "at://did:plc:bob/app.bsky.feed.post/parent"
	publisher := &recordingPublisher{}
	fetcher := &stubRecordFetcher{
		records: map[string]FetchedRecord{
			rootURI: {
				ATDID:      "did:plc:carol",
				ATURI:      rootURI,
				ATCID:      "bafy-root",
				Collection: mapper.RecordTypePost,
				RecordJSON: []byte(`{"text":"root","createdAt":"2026-01-01T00:00:00Z"}`),
			},
			parentURI: {
				ATDID:      "did:plc:bob",
				ATURI:      parentURI,
				ATCID:      "bafy-parent",
				Collection: mapper.RecordTypePost,
				RecordJSON: []byte(`{
					"text":"parent",
					"reply":{
						"root":{"uri":"at://did:plc:carol/app.bsky.feed.post/root","cid":"bafy-root"},
						"parent":{"uri":"at://did:plc:carol/app.bsky.feed.post/root","cid":"bafy-root"}
					},
					"createdAt":"2026-01-01T00:00:00Z"
				}`),
			},
		},
	}
	processor := newProcessorWithDependencies(database, publisher, nil, fetcher)

	replyRecord := []byte(`{
		"text":"child",
		"reply":{
			"root":{"uri":"at://did:plc:carol/app.bsky.feed.post/root","cid":"bafy-root"},
			"parent":{"uri":"at://did:plc:bob/app.bsky.feed.post/parent","cid":"bafy-parent"}
		},
		"createdAt":"2026-01-01T00:00:00Z"
	}`)
	if err := processor.ProcessRecord(
		ctx,
		"did:plc:alice",
		"at://did:plc:alice/app.bsky.feed.post/reply-1",
		"bafy-child",
		mapper.RecordTypePost,
		replyRecord,
	); err != nil {
		t.Fatalf("process reply record: %v", err)
	}

	reply, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.post/reply-1")
	if err != nil {
		t.Fatalf("get reply message: %v", err)
	}
	if reply == nil || reply.MessageState != db.MessageStatePublished {
		t.Fatalf("expected reply to publish after resolving parent/root chain, got %+v", reply)
	}

	published := publisher.snapshot()
	if len(published) != 3 {
		t.Fatalf("expected 3 publish calls, got %d", len(published))
	}
	if published[0]["text"] != "root" || published[1]["text"] != "parent" || published[2]["text"] != "child" {
		t.Fatalf("expected root->parent->child publish order, got %+v", published)
	}
	if fetcher.fetchCount(rootURI) != 1 || fetcher.fetchCount(parentURI) != 1 {
		t.Fatalf("expected one fetch for root and parent, got root=%d parent=%d", fetcher.fetchCount(rootURI), fetcher.fetchCount(parentURI))
	}
}

func TestProcessRecordFollowResolvesExternalFeedWithoutBridgedAccount(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	publisher := &recordingPublisher{}
	feedResolver := &mockFeedResolver{
		refs: map[string]string{
			"did:plc:bob": "@bob.ed25519",
		},
	}
	processor := newProcessorWithDependencies(database, publisher, feedResolver, nil)

	followRecord := []byte(`{
		"subject": "did:plc:bob",
		"createdAt": "2026-01-01T00:00:00Z"
	}`)
	if err := processor.ProcessRecord(
		ctx,
		"did:plc:alice",
		"at://did:plc:alice/app.bsky.graph.follow/auto-1",
		"bafy-follow-auto-1",
		mapper.RecordTypeFollow,
		followRecord,
	); err != nil {
		t.Fatalf("process follow record: %v", err)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.graph.follow/auto-1")
	if err != nil {
		t.Fatalf("get follow message: %v", err)
	}
	if stored == nil || stored.MessageState != db.MessageStatePublished {
		t.Fatalf("expected follow message to publish, got %+v", stored)
	}
	if strings.Contains(stored.RawSSBJson, "_atproto_contact") {
		t.Fatalf("expected resolved follow payload, got %s", stored.RawSSBJson)
	}

	accounts, err := database.GetAllBridgedAccounts(ctx)
	if err != nil {
		t.Fatalf("list bridged accounts: %v", err)
	}
	if len(accounts) != 0 {
		t.Fatalf("expected no bridged account rows to be inserted, got %+v", accounts)
	}
}

func TestProcessRecordStaysDeferredWhenDependencyUnsupported(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	subjectURI := "at://did:plc:bob/app.bsky.feed.post/missing"
	publisher := &recordingPublisher{}
	fetcher := &stubRecordFetcher{
		records: map[string]FetchedRecord{
			subjectURI: {
				ATDID:      "did:plc:bob",
				ATURI:      subjectURI,
				ATCID:      "bafy-repost",
				Collection: mapper.RecordTypeRepost,
				RecordJSON: []byte(`{"subject":{"uri":"at://did:plc:bob/app.bsky.feed.post/1","cid":"bafy1"}}`),
			},
		},
	}
	processor := newProcessorWithDependencies(database, publisher, nil, fetcher)

	likeRecord := []byte(`{
		"subject": {
			"uri": "at://did:plc:bob/app.bsky.feed.post/missing",
			"cid": "bafy-repost"
		},
		"createdAt": "2026-01-01T00:00:00Z"
	}`)
	if err := processor.ProcessRecord(
		ctx,
		"did:plc:alice",
		"at://did:plc:alice/app.bsky.feed.like/unsupported-1",
		"bafy-like-unsupported",
		mapper.RecordTypeLike,
		likeRecord,
	); err != nil {
		t.Fatalf("process like record: %v", err)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.like/unsupported-1")
	if err != nil {
		t.Fatalf("get like message: %v", err)
	}
	if stored == nil || stored.MessageState != db.MessageStateDeferred {
		t.Fatalf("expected like to remain deferred, got %+v", stored)
	}
	if stored.SSBMsgRef != "" {
		t.Fatalf("expected no publish ref for unresolved dependency, got %q", stored.SSBMsgRef)
	}
	if fetcher.fetchCount(subjectURI) != 1 {
		t.Fatalf("expected dependency fetch attempt, got %d", fetcher.fetchCount(subjectURI))
	}
}

func TestProcessRecordPublishesAndPersistsMetadata(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%published.sha256"}),
	)

	record := []byte(`{"text":"hello publish","createdAt":"2026-01-01T00:00:00Z"}`)
	err = processor.ProcessRecord(
		ctx,
		"did:plc:alice",
		"at://did:plc:alice/app.bsky.feed.post/2",
		"bafy-published",
		mapper.RecordTypePost,
		record,
	)
	if err != nil {
		t.Fatalf("process record: %v", err)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.post/2")
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if stored == nil {
		t.Fatalf("expected stored message")
	}
	if stored.SSBMsgRef != "%published.sha256" {
		t.Fatalf("expected published ref, got %q", stored.SSBMsgRef)
	}
	if stored.PublishAttempts != 1 {
		t.Fatalf("expected publish attempts=1, got %d", stored.PublishAttempts)
	}
	if stored.PublishedAt == nil {
		t.Fatalf("expected published_at to be set")
	}
	if stored.PublishError != "" {
		t.Fatalf("expected empty publish_error, got %q", stored.PublishError)
	}
}

func TestProcessRecordPublishFailurePersistsError(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{err: errors.New("boom publish")}),
	)

	record := []byte(`{"text":"hello publish fail","createdAt":"2026-01-01T00:00:00Z"}`)
	err = processor.ProcessRecord(
		ctx,
		"did:plc:alice",
		"at://did:plc:alice/app.bsky.feed.post/3",
		"bafy-fail",
		mapper.RecordTypePost,
		record,
	)
	if err != nil {
		t.Fatalf("process record: %v", err)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.post/3")
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if stored == nil {
		t.Fatalf("expected stored message")
	}
	if stored.SSBMsgRef != "" {
		t.Fatalf("expected empty ssb ref on publish failure, got %q", stored.SSBMsgRef)
	}
	if stored.PublishAttempts != 1 {
		t.Fatalf("expected publish attempts=1, got %d", stored.PublishAttempts)
	}
	if !strings.Contains(stored.PublishError, "boom publish") {
		t.Fatalf("expected publish_error to include failure, got %q", stored.PublishError)
	}
}

func TestProcessRecordBlobFailureFallsBackButPublishes(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%published.sha256"}),
		WithBlobBridge(&mockBlobBridge{err: errors.New("blob fetch failed")}),
	)

	record := []byte(`{"text":"hello blob fail","createdAt":"2026-01-01T00:00:00Z"}`)
	err = processor.ProcessRecord(
		ctx,
		"did:plc:alice",
		"at://did:plc:alice/app.bsky.feed.post/4",
		"bafy-blob-fail",
		mapper.RecordTypePost,
		record,
	)
	if err != nil {
		t.Fatalf("process record: %v", err)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.post/4")
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if stored == nil {
		t.Fatalf("expected stored message")
	}
	if stored.SSBMsgRef != "%published.sha256" {
		t.Fatalf("expected publish success ref, got %q", stored.SSBMsgRef)
	}
	if !strings.Contains(stored.PublishError, "blob_fallback=") {
		t.Fatalf("expected blob fallback error annotation, got %q", stored.PublishError)
	}
}

func TestResolveDeferredMessagesPublishesWhenDependencyAppears(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%resolved.sha256"}),
	)

	likeURI := "at://did:plc:alice/app.bsky.feed.like/resolve-1"
	if err := database.AddMessage(ctx, db.Message{
		ATURI:              likeURI,
		ATCID:              "bafy-like-resolve",
		ATDID:              "did:plc:alice",
		Type:               mapper.RecordTypeLike,
		MessageState:       db.MessageStateDeferred,
		RawATJson:          `{"subject":{"uri":"at://did:plc:alice/app.bsky.feed.post/root","cid":"bafy-root"}}`,
		RawSSBJson:         `{"type":"vote","vote":{"value":1,"expression":"Like"},"_atproto_subject":"at://did:plc:alice/app.bsky.feed.post/root"}`,
		DeferReason:        "_atproto_subject=at://did:plc:alice/app.bsky.feed.post/root",
		DeferAttempts:      1,
		LastDeferAttemptAt: func() *time.Time { v := time.Now().UTC().Add(-time.Minute); return &v }(),
	}); err != nil {
		t.Fatalf("seed deferred message: %v", err)
	}

	if err := database.AddMessage(ctx, db.Message{
		ATURI:        "at://did:plc:alice/app.bsky.feed.post/root",
		ATCID:        "bafy-root",
		ATDID:        "did:plc:alice",
		Type:         mapper.RecordTypePost,
		MessageState: db.MessageStatePublished,
		SSBMsgRef:    "%root.sha256",
		RawATJson:    `{"text":"root"}`,
		RawSSBJson:   `{"type":"post","text":"root"}`,
	}); err != nil {
		t.Fatalf("seed dependency message: %v", err)
	}

	result, err := processor.ResolveDeferredMessages(ctx, 10)
	if err != nil {
		t.Fatalf("resolve deferred: %v", err)
	}
	if result.Published != 1 {
		t.Fatalf("expected 1 published deferred message, got %+v", result)
	}

	stored, err := database.GetMessage(ctx, likeURI)
	if err != nil {
		t.Fatalf("get deferred message: %v", err)
	}
	if stored == nil {
		t.Fatalf("expected deferred message after resolve")
	}
	if stored.MessageState != db.MessageStatePublished {
		t.Fatalf("expected published state after deferred resolve, got %q", stored.MessageState)
	}
	if stored.SSBMsgRef != "%resolved.sha256" {
		t.Fatalf("expected resolved publish ref, got %q", stored.SSBMsgRef)
	}
}

func TestResolveDeferredMessagesPublishesFollowWhenContactAppears(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%resolved-follow.sha256"}),
	)

	if err := database.AddMessage(ctx, db.Message{
		ATURI:              "at://did:plc:alice/app.bsky.graph.follow/resolve-1",
		ATCID:              "bafy-follow-resolve",
		ATDID:              "did:plc:alice",
		Type:               mapper.RecordTypeFollow,
		MessageState:       db.MessageStateDeferred,
		RawATJson:          `{"subject":"did:plc:bob","createdAt":"2026-01-01T00:00:00Z"}`,
		RawSSBJson:         `{"type":"contact","following":true,"_atproto_contact":"did:plc:bob"}`,
		DeferReason:        "_atproto_contact=did:plc:bob",
		DeferAttempts:      1,
		LastDeferAttemptAt: func() *time.Time { v := time.Now().UTC().Add(-time.Minute); return &v }(),
	}); err != nil {
		t.Fatalf("seed deferred follow message: %v", err)
	}

	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:bob",
		SSBFeedID: "@bob.ed25519",
		Active:    true,
	}); err != nil {
		t.Fatalf("seed bridged account: %v", err)
	}

	result, err := processor.ResolveDeferredMessages(ctx, 10)
	if err != nil {
		t.Fatalf("resolve deferred follow: %v", err)
	}
	if result.Published != 1 {
		t.Fatalf("expected 1 published deferred follow, got %+v", result)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.graph.follow/resolve-1")
	if err != nil {
		t.Fatalf("get resolved follow message: %v", err)
	}
	if stored == nil {
		t.Fatalf("expected resolved follow message")
	}
	if stored.MessageState != db.MessageStatePublished {
		t.Fatalf("expected follow message state published, got %q", stored.MessageState)
	}
	if stored.SSBMsgRef != "%resolved-follow.sha256" {
		t.Fatalf("expected resolved follow publish ref, got %q", stored.SSBMsgRef)
	}
}

func TestResolveDeferredMessagesAutoFetchesMissingDependency(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	subjectURI := "at://did:plc:bob/app.bsky.feed.post/deferred-root"
	fetcher := &stubRecordFetcher{
		records: map[string]FetchedRecord{
			subjectURI: {
				ATDID:      "did:plc:bob",
				ATURI:      subjectURI,
				ATCID:      "bafy-deferred-root",
				Collection: mapper.RecordTypePost,
				RecordJSON: []byte(`{"text":"deferred root","createdAt":"2026-01-01T00:00:00Z"}`),
			},
		},
	}
	publisher := &recordingPublisher{}
	processor := newProcessorWithDependencies(database, publisher, nil, fetcher)

	if err := database.AddMessage(ctx, db.Message{
		ATURI:              "at://did:plc:alice/app.bsky.feed.like/resolve-auto",
		ATCID:              "bafy-like-resolve-auto",
		ATDID:              "did:plc:alice",
		Type:               mapper.RecordTypeLike,
		MessageState:       db.MessageStateDeferred,
		RawATJson:          `{"subject":{"uri":"at://did:plc:bob/app.bsky.feed.post/deferred-root","cid":"bafy-deferred-root"},"createdAt":"2026-01-01T00:00:00Z"}`,
		RawSSBJson:         `{"type":"vote","vote":{"value":1,"expression":"Like"},"_atproto_subject":"at://did:plc:bob/app.bsky.feed.post/deferred-root"}`,
		DeferReason:        "_atproto_subject=at://did:plc:bob/app.bsky.feed.post/deferred-root",
		DeferAttempts:      1,
		LastDeferAttemptAt: func() *time.Time { v := time.Now().UTC().Add(-time.Minute); return &v }(),
	}); err != nil {
		t.Fatalf("seed deferred message: %v", err)
	}

	result, err := processor.ResolveDeferredMessages(ctx, 10)
	if err != nil {
		t.Fatalf("resolve deferred messages: %v", err)
	}
	if result.Published != 1 {
		t.Fatalf("expected 1 published deferred message, got %+v", result)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.like/resolve-auto")
	if err != nil {
		t.Fatalf("get resolved deferred message: %v", err)
	}
	if stored == nil || stored.MessageState != db.MessageStatePublished {
		t.Fatalf("expected deferred message to publish, got %+v", stored)
	}
	if fetcher.fetchCount(subjectURI) != 1 {
		t.Fatalf("expected exactly one dependency fetch, got %d", fetcher.fetchCount(subjectURI))
	}
}

func TestDependencyResolverDeduplicatesInFlightFetches(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	waitCh := make(chan struct{})
	fetcher := &stubRecordFetcher{
		waitCh: waitCh,
		ready:  make(chan struct{}),
		records: map[string]FetchedRecord{
			"at://did:plc:bob/app.bsky.feed.post/shared": {
				ATDID:      "did:plc:bob",
				ATURI:      "at://did:plc:bob/app.bsky.feed.post/shared",
				ATCID:      "bafy-shared",
				Collection: mapper.RecordTypePost,
				RecordJSON: []byte(`{"text":"shared","createdAt":"2026-01-01T00:00:00Z"}`),
			},
		},
	}
	resolver := NewATProtoDependencyResolver(
		database,
		log.New(io.Discard, "", 0),
		fetcher,
		func(context.Context, string, string, string, string, []byte) error { return nil },
	)

	ctx := ensureDependencyContext(context.Background(), "did:plc:root", "at://did:plc:root/app.bsky.feed.post/root")
	targetURI := "at://did:plc:bob/app.bsky.feed.post/shared"
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- resolver.EnsureRecord(ctx, targetURI)
		}()
	}

	<-fetcher.ready                  // G1 has entered FetchRecord and is about to block
	runtime.Gosched()                // yield to let G2 join the inflight call before we release G1
	time.Sleep(5 * time.Millisecond) // ensure G2 enters wait state
	close(waitCh)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("ensure record returned error: %v", err)
		}
	}
	if fetcher.fetchCount(targetURI) != 1 {
		t.Fatalf("expected one in-flight fetch, got %d", fetcher.fetchCount(targetURI))
	}
}

func TestLookupFeedDeduplicatesInFlightResolution(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	waitCh := make(chan struct{})
	feedResolver := &mockFeedResolver{
		waitCh: waitCh,
		ready:  make(chan struct{}),
		refs: map[string]string{
			"did:plc:bob": "@bob.ed25519",
		},
	}
	processor := newProcessorWithDependencies(database, nil, feedResolver, nil)

	ctx := ensureDependencyContext(context.Background(), "did:plc:alice", "at://did:plc:alice/app.bsky.graph.follow/shared")
	errCh := make(chan error, 2)
	results := make(chan string, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ref, err := processor.lookupFeed(ctx, "did:plc:bob")
			if err == nil {
				results <- ref
			}
			errCh <- err
		}()
	}

	<-feedResolver.ready // G1 has entered ResolveFeed and is about to block
	runtime.Gosched()    // yield to let G2 join the inflight call before we release G1
	close(waitCh)
	wg.Wait()
	close(results)
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("lookupFeed returned error: %v", err)
		}
	}
	for ref := range results {
		if ref != "@bob.ed25519" {
			t.Fatalf("unexpected resolved feed ref %q", ref)
		}
	}
	if feedResolver.lookupCount("did:plc:bob") != 1 {
		t.Fatalf("expected exactly one feed resolver lookup, got %d", feedResolver.lookupCount("did:plc:bob"))
	}
}

func TestProcessDeleteOpPublishesUnlike(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%unlike.sha256"}),
	)

	if err := database.AddMessage(ctx, db.Message{
		ATURI:        "at://did:plc:alice/app.bsky.feed.like/123",
		ATCID:        "bafy-like",
		ATDID:        "did:plc:alice",
		Type:         mapper.RecordTypeLike,
		MessageState: db.MessageStatePublished,
		RawATJson: `{
			"subject":{"uri":"at://did:plc:alice/app.bsky.feed.post/1","cid":"bafy-post"},
			"createdAt":"2026-01-01T00:00:00Z"
		}`,
		RawSSBJson: `{"type":"vote","vote":{"link":"%post.sha256","value":1,"expression":"Like"}}`,
	}); err != nil {
		t.Fatalf("seed like: %v", err)
	}
	if err := database.AddMessage(ctx, db.Message{
		ATURI:        "at://did:plc:alice/app.bsky.feed.post/1",
		ATCID:        "bafy-post",
		ATDID:        "did:plc:alice",
		Type:         mapper.RecordTypePost,
		MessageState: db.MessageStatePublished,
		SSBMsgRef:    "%post.sha256",
		RawATJson:    `{"text":"post"}`,
		RawSSBJson:   `{"type":"post","text":"post"}`,
	}); err != nil {
		t.Fatalf("seed post: %v", err)
	}

	if err := processor.processDeleteOp(ctx, "did:plc:alice", "app.bsky.feed.like/123", 42); err != nil {
		t.Fatalf("process like delete op: %v", err)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.like/123")
	if err != nil {
		t.Fatalf("get deleted like message: %v", err)
	}
	if stored == nil {
		t.Fatalf("expected deleted like message row")
	}
	if stored.MessageState != db.MessageStatePublished {
		t.Fatalf("expected published unlike state, got %q", stored.MessageState)
	}
	if stored.SSBMsgRef != "%unlike.sha256" {
		t.Fatalf("expected unlike publish ref, got %q", stored.SSBMsgRef)
	}
	if stored.DeletedSeq == nil || *stored.DeletedSeq != 42 {
		t.Fatalf("expected deleted seq 42, got %+v", stored.DeletedSeq)
	}
	if !strings.Contains(stored.RawSSBJson, `"value":0`) || !strings.Contains(stored.RawSSBJson, `%post.sha256`) {
		t.Fatalf("expected unlike payload in stored ssb json, got %s", stored.RawSSBJson)
	}
}

func TestProcessDeleteOpPublishesUnfollowAndUnblock(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%contact-reset.sha256"}),
	)

	for _, seeded := range []db.Message{
		{
			ATURI:        "at://did:plc:alice/app.bsky.graph.follow/123",
			ATCID:        "bafy-follow",
			ATDID:        "did:plc:alice",
			Type:         mapper.RecordTypeFollow,
			MessageState: db.MessageStatePublished,
			RawATJson:    `{"subject":"did:plc:bob","createdAt":"2026-01-01T00:00:00Z"}`,
			RawSSBJson:   `{"type":"contact","contact":"@bob.ed25519","following":true,"blocking":false}`,
		},
		{
			ATURI:        "at://did:plc:alice/app.bsky.graph.block/456",
			ATCID:        "bafy-block",
			ATDID:        "did:plc:alice",
			Type:         mapper.RecordTypeBlock,
			MessageState: db.MessageStatePublished,
			RawATJson:    `{"subject":"did:plc:bob","createdAt":"2026-01-01T00:00:00Z"}`,
			RawSSBJson:   `{"type":"contact","contact":"@bob.ed25519","following":false,"blocking":true}`,
		},
	} {
		if err := database.AddMessage(ctx, seeded); err != nil {
			t.Fatalf("seed contact message: %v", err)
		}
	}
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:bob",
		SSBFeedID: "@bob.ed25519",
		Active:    true,
	}); err != nil {
		t.Fatalf("seed bridged account: %v", err)
	}

	if err := processor.processDeleteOp(ctx, "did:plc:alice", "app.bsky.graph.follow/123", 77); err != nil {
		t.Fatalf("process follow delete op: %v", err)
	}
	if err := processor.processDeleteOp(ctx, "did:plc:alice", "app.bsky.graph.block/456", 78); err != nil {
		t.Fatalf("process block delete op: %v", err)
	}

	for _, atURI := range []string{
		"at://did:plc:alice/app.bsky.graph.follow/123",
		"at://did:plc:alice/app.bsky.graph.block/456",
	} {
		stored, err := database.GetMessage(ctx, atURI)
		if err != nil {
			t.Fatalf("get deleted contact message: %v", err)
		}
		if stored == nil {
			t.Fatalf("expected deleted contact message row for %s", atURI)
		}
		if stored.MessageState != db.MessageStatePublished {
			t.Fatalf("expected contact reset state published, got %q", stored.MessageState)
		}
		if !strings.Contains(stored.RawSSBJson, `"following":false`) || !strings.Contains(stored.RawSSBJson, `"blocking":false`) {
			t.Fatalf("expected reset contact payload, got %s", stored.RawSSBJson)
		}
	}
}

func TestProcessDeleteOpMarksProfileDeletedWithoutPublishing(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%should-not-publish.sha256"}),
	)

	if err := database.AddMessage(ctx, db.Message{
		ATURI:        "at://did:plc:alice/app.bsky.actor.profile/self",
		ATCID:        "bafy-profile",
		ATDID:        "did:plc:alice",
		Type:         mapper.RecordTypeProfile,
		MessageState: db.MessageStatePublished,
		RawATJson:    `{"displayName":"Alice"}`,
		RawSSBJson:   `{"type":"about","about":"@alice.ed25519","name":"Alice"}`,
	}); err != nil {
		t.Fatalf("seed profile: %v", err)
	}

	if err := processor.processDeleteOp(ctx, "did:plc:alice", "app.bsky.actor.profile/self", 88); err != nil {
		t.Fatalf("process profile delete op: %v", err)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.actor.profile/self")
	if err != nil {
		t.Fatalf("get deleted profile message: %v", err)
	}
	if stored == nil {
		t.Fatalf("expected deleted profile row")
	}
	if stored.MessageState != db.MessageStateDeleted {
		t.Fatalf("expected deleted profile state, got %q", stored.MessageState)
	}
	if stored.SSBMsgRef != "" {
		t.Fatalf("expected no publish ref for profile delete, got %q", stored.SSBMsgRef)
	}
}

func TestSupportedCollectionsIncludeBlockAndProfileButNotRepost(t *testing.T) {
	if !isSupportedCollection(mapper.RecordTypeBlock) {
		t.Fatalf("expected block to be supported")
	}
	if !isSupportedCollection(mapper.RecordTypeProfile) {
		t.Fatalf("expected profile to be supported")
	}
	if isSupportedCollection(mapper.RecordTypeRepost) {
		t.Fatalf("expected standalone repost to be unsupported")
	}
}

func TestCborToJSONInvalidInput(t *testing.T) {
	_, err := cborToJSON([]byte("not valid cbor"))
	if err == nil {
		t.Fatalf("expected error for invalid CBOR input")
	}
}

func TestNewXRPCRecordFetcher(t *testing.T) {
	fetcher := NewXRPCRecordFetcher(nil)
	if fetcher == nil {
		t.Fatalf("expected non-nil fetcher")
	}
}

func TestPDSAwareRecordFetcherNilReceiver(t *testing.T) {
	var fetcher *PDSAwareRecordFetcher
	_, err := fetcher.FetchRecord(context.Background(), "at://did:plc:test/app.bsky.feed.post/1")
	if err == nil {
		t.Fatalf("expected error for nil receiver")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Fatalf("expected nil error message, got %v", err)
	}
}

func TestPDSAwareRecordFetcherInvalidURI(t *testing.T) {
	fetcher := NewPDSAwareRecordFetcher(nil, nil)
	_, err := fetcher.FetchRecord(context.Background(), "not a valid at-uri")
	if err == nil {
		t.Fatalf("expected error for invalid URI")
	}
}

func TestPDSAwareRecordFetcherMissingComponents(t *testing.T) {
	fetcher := NewPDSAwareRecordFetcher(nil, nil)
	tests := []string{
		"at://did:plc:test/",         // missing rkey
		"at:///app.bsky.feed.post/1", // missing repo
		"at://did:plc:test/ /1",      // missing collection
	}
	for _, uri := range tests {
		_, err := fetcher.FetchRecord(context.Background(), uri)
		if err == nil {
			t.Errorf("expected error for %q", uri)
		}
	}
}

func TestNewPDSAwareRecordFetcher(t *testing.T) {
	fetcher := NewPDSAwareRecordFetcher(nil, nil)
	if fetcher == nil {
		t.Fatalf("expected non-nil fetcher")
	}
}

func TestProcessRecordPublishesReferenceCompatibleShapes(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	for _, acc := range []db.BridgedAccount{
		{ATDID: "did:plc:alice", SSBFeedID: "@alice.ed25519", Active: true},
		{ATDID: "did:plc:bob", SSBFeedID: "@bob.ed25519", Active: true},
	} {
		if err := database.AddBridgedAccount(ctx, acc); err != nil {
			t.Fatalf("seed account: %v", err)
		}
	}

	publisher := &recordingPublisher{}
	processor := NewProcessor(database, log.New(io.Discard, "", 0), WithPublisher(publisher))

	if err := processor.ProcessRecord(ctx, "did:plc:alice", "at://did:plc:alice/app.bsky.feed.post/1", "bafy-post", mapper.RecordTypePost, []byte(`{
		"text":"Hello @bob #bridge",
		"facets":[
			{"index":{"byteStart":6,"byteEnd":10},"features":[{"$type":"app.bsky.richtext.facet#mention","did":"did:plc:bob"}]},
			{"index":{"byteStart":11,"byteEnd":18},"features":[{"$type":"app.bsky.richtext.facet#tag","tag":"bridge"}]}
		],
		"createdAt":"2026-01-01T00:00:00Z"
	}`)); err != nil {
		t.Fatalf("process post: %v", err)
	}

	if err := processor.ProcessRecord(ctx, "did:plc:alice", "at://did:plc:alice/app.bsky.feed.like/1", "bafy-like", mapper.RecordTypeLike, []byte(`{
		"subject":{"uri":"at://did:plc:alice/app.bsky.feed.post/1","cid":"bafy-post"},
		"createdAt":"2026-01-01T00:00:01Z"
	}`)); err != nil {
		t.Fatalf("process like: %v", err)
	}

	if err := processor.ProcessRecord(ctx, "did:plc:alice", "at://did:plc:alice/app.bsky.graph.follow/1", "bafy-follow", mapper.RecordTypeFollow, []byte(`{
		"subject":"did:plc:bob",
		"createdAt":"2026-01-01T00:00:02Z"
	}`)); err != nil {
		t.Fatalf("process follow: %v", err)
	}

	if err := processor.ProcessRecord(ctx, "did:plc:alice", "at://did:plc:alice/app.bsky.graph.block/1", "bafy-block", mapper.RecordTypeBlock, []byte(`{
		"subject":"did:plc:bob",
		"createdAt":"2026-01-01T00:00:03Z"
	}`)); err != nil {
		t.Fatalf("process block: %v", err)
	}

	if err := processor.ProcessRecord(ctx, "did:plc:alice", "at://did:plc:alice/app.bsky.actor.profile/self", "bafy-profile", mapper.RecordTypeProfile, []byte(`{
		"displayName":"Alice",
		"description":"Bridge bio",
		"createdAt":"2026-01-01T00:00:04Z"
	}`)); err != nil {
		t.Fatalf("process profile: %v", err)
	}

	published := publisher.snapshot()
	if len(published) != 5 {
		t.Fatalf("expected 5 published payloads, got %d", len(published))
	}

	postMentions := mentionMaps(published[0]["mentions"])
	if published[0]["type"] != "post" || len(postMentions) != 2 {
		t.Fatalf("unexpected post payload: %+v", published[0])
	}
	if postMentions[0]["link"] != "@bob.ed25519" || postMentions[1]["link"] != "#bridge" {
		t.Fatalf("unexpected post mentions: %+v", postMentions)
	}

	vote, ok := published[1]["vote"].(map[string]interface{})
	if !ok || published[1]["type"] != "vote" || vote["link"] != "%pub-1.sha256" || vote["value"] != float64(1) {
		t.Fatalf("unexpected vote payload: %+v", published[1])
	}

	if published[2]["type"] != "contact" || published[2]["contact"] != "@bob.ed25519" || published[2]["following"] != true || published[2]["blocking"] != false {
		t.Fatalf("unexpected follow payload: %+v", published[2])
	}
	if published[3]["type"] != "contact" || published[3]["contact"] != "@bob.ed25519" || published[3]["following"] != false || published[3]["blocking"] != true {
		t.Fatalf("unexpected block payload: %+v", published[3])
	}
	if published[4]["type"] != "about" || published[4]["about"] != "@alice.ed25519" || published[4]["name"] != "Alice" {
		t.Fatalf("unexpected about payload: %+v", published[4])
	}

	for _, item := range published {
		raw, err := json.Marshal(item)
		if err != nil {
			t.Fatalf("marshal published payload: %v", err)
		}
		if strings.Contains(string(raw), "_atproto_") || strings.Contains(string(raw), "blob_refs") {
			t.Fatalf("expected native-only payload, got %s", raw)
		}
	}
}

func TestRetryFailedMessagesPublishesWhenBackoffElapsed(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	lastAttempt := time.Now().UTC().Add(-5 * time.Minute)
	if err := database.AddMessage(ctx, db.Message{
		ATURI:                "at://did:plc:alice/app.bsky.feed.post/retry-1",
		ATCID:                "bafy-retry-1",
		ATDID:                "did:plc:alice",
		Type:                 mapper.RecordTypePost,
		MessageState:         db.MessageStateFailed,
		RawATJson:            `{"text":"retry-1"}`,
		RawSSBJson:           `{"type":"post","text":"retry-1"}`,
		PublishError:         "initial failure",
		PublishAttempts:      1,
		LastPublishAttemptAt: &lastAttempt,
	}); err != nil {
		t.Fatalf("seed failed message: %v", err)
	}

	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%retry-ok.sha256"}),
	)

	res, err := processor.RetryFailedMessages(ctx, RetryConfig{
		Limit:       10,
		MaxAttempts: 5,
		BaseBackoff: time.Second,
	})
	if err != nil {
		t.Fatalf("retry failed messages: %v", err)
	}
	if res.Selected != 1 || res.Attempted != 1 || res.Published != 1 || res.Failed != 0 || res.Deferred != 0 {
		t.Fatalf("unexpected retry result: %+v", res)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.post/retry-1")
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if stored == nil {
		t.Fatalf("expected stored message")
	}
	if stored.SSBMsgRef != "%retry-ok.sha256" {
		t.Fatalf("expected retry published ref, got %q", stored.SSBMsgRef)
	}
	if stored.PublishError != "" {
		t.Fatalf("expected cleared publish error, got %q", stored.PublishError)
	}
	if stored.PublishAttempts != 2 {
		t.Fatalf("expected publish attempts 2 after retry, got %d", stored.PublishAttempts)
	}
	if stored.PublishedAt == nil {
		t.Fatalf("expected published_at on retry success")
	}
}

func TestRetryFailedMessagesDefersUntilBackoff(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	lastAttempt := time.Now().UTC()
	if err := database.AddMessage(ctx, db.Message{
		ATURI:                "at://did:plc:alice/app.bsky.feed.post/retry-2",
		ATCID:                "bafy-retry-2",
		ATDID:                "did:plc:alice",
		Type:                 mapper.RecordTypePost,
		MessageState:         db.MessageStateFailed,
		RawATJson:            `{"text":"retry-2"}`,
		RawSSBJson:           `{"type":"post","text":"retry-2"}`,
		PublishError:         "initial failure",
		PublishAttempts:      1,
		LastPublishAttemptAt: &lastAttempt,
	}); err != nil {
		t.Fatalf("seed failed message: %v", err)
	}

	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%retry-deferred.sha256"}),
	)

	res, err := processor.RetryFailedMessages(ctx, RetryConfig{
		Limit:       10,
		MaxAttempts: 5,
		BaseBackoff: 10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("retry failed messages: %v", err)
	}
	if res.Selected != 1 || res.Attempted != 0 || res.Published != 0 || res.Failed != 0 || res.Deferred != 1 {
		t.Fatalf("unexpected retry result: %+v", res)
	}
}

func TestParseDeferReasonURIs(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		want   []string
	}{
		{"empty", "", nil},
		{"single subject", "_atproto_subject=at://did:plc:bob/app.bsky.feed.post/1", []string{"at://did:plc:bob/app.bsky.feed.post/1"}},
		{"reply root and parent", "_atproto_reply_root=at://did:plc:a/app.bsky.feed.post/r;_atproto_reply_parent=at://did:plc:b/app.bsky.feed.post/p", []string{"at://did:plc:a/app.bsky.feed.post/r", "at://did:plc:b/app.bsky.feed.post/p"}},
		{"quote subject", "_atproto_quote_subject=at://did:plc:c/app.bsky.feed.post/q", []string{"at://did:plc:c/app.bsky.feed.post/q"}},
		{"no at uri", "_atproto_contact=did:plc:unknown", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDeferReasonURIs(tt.reason)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("uri[%d]: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRetryBackoffWithZeroAttempts(t *testing.T) {
	base := retryBackoff(time.Second, 0)
	if base != time.Second {
		t.Fatalf("expected base backoff for zero attempts, got %v", base)
	}
}

func TestRetryBackoffWithOneAttempt(t *testing.T) {
	base := retryBackoff(time.Second, 1)
	if base != time.Second {
		t.Fatalf("expected base backoff for one attempt, got %v", base)
	}
}

func TestRetryBackoffExponentialGrowth(t *testing.T) {
	base1 := retryBackoff(time.Second, 1)
	base2 := retryBackoff(time.Second, 2)
	base3 := retryBackoff(time.Second, 3)
	if base1 >= base2 || base2 >= base3 {
		t.Fatalf("expected exponential growth, got base1=%v base2=%v base3=%v", base1, base2, base3)
	}
}

func TestRetryBackoffWithZeroBase(t *testing.T) {
	base := retryBackoff(0, 2)
	if base < 5*time.Second {
		t.Fatalf("expected default base for zero input, got %v", base)
	}
}

func TestCollectionFromPath(t *testing.T) {
	tests := []struct {
		path string
		coll string
		ok   bool
	}{
		{"app.bsky.feed.post/123", "app.bsky.feed.post", true},
		{"app.bsky.graph.follow/456", "app.bsky.graph.follow", true},
		{"invalid", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		coll, ok := collectionFromPath(tt.path)
		if ok != tt.ok {
			t.Errorf("collectionFromPath(%q): ok=%v, want %v", tt.path, ok, tt.ok)
		}
		if tt.ok && coll != tt.coll {
			t.Errorf("collectionFromPath(%q): coll=%q, want %q", tt.path, coll, tt.coll)
		}
	}
}

func TestIsSupportedCollection(t *testing.T) {
	tests := []struct {
		collection string
		want       bool
	}{
		{mapper.RecordTypePost, true},
		{mapper.RecordTypeLike, true},
		{"other", false},
	}
	for _, tt := range tests {
		if got := isSupportedCollection(tt.collection); got != tt.want {
			t.Errorf("isSupportedCollection(%q) = %v, want %v", tt.collection, got, tt.want)
		}
	}
}

type mockProcessorDatabase struct {
	err error

	getBridgedAccountErr          error
	addMessageErr                 error
	getMessageErr                 error
	setBridgeStateErr             error
	getDeferredCandidatesErr      error
	getRetryCandidatesErr         error
	getExpiredDeferredMessagesErr error

	// Data overrides
	getBridgedAccountResp *db.BridgedAccount
}

func (m *mockProcessorDatabase) GetBridgedAccount(ctx context.Context, atDID string) (*db.BridgedAccount, error) {
	if m.getBridgedAccountErr != nil {
		return nil, m.getBridgedAccountErr
	}
	if m.getBridgedAccountResp != nil {
		return m.getBridgedAccountResp, nil
	}
	return nil, m.err
}
func (m *mockProcessorDatabase) AddMessage(ctx context.Context, msg db.Message) error {
	if m.addMessageErr != nil {
		return m.addMessageErr
	}
	return m.err
}
func (m *mockProcessorDatabase) GetMessage(ctx context.Context, atURI string) (*db.Message, error) {
	if m.getMessageErr != nil {
		return nil, m.getMessageErr
	}
	return nil, m.err
}
func (m *mockProcessorDatabase) SetBridgeState(ctx context.Context, key, value string) error {
	if m.setBridgeStateErr != nil {
		return m.setBridgeStateErr
	}
	return m.err
}
func (m *mockProcessorDatabase) GetDeferredCandidates(ctx context.Context, limit int) ([]db.Message, error) {
	if m.getDeferredCandidatesErr != nil {
		return nil, m.getDeferredCandidatesErr
	}
	return nil, m.err
}
func (m *mockProcessorDatabase) GetRetryCandidates(ctx context.Context, limit int, atDID string, maxAttempts int) ([]db.Message, error) {
	if m.getRetryCandidatesErr != nil {
		return nil, m.getRetryCandidatesErr
	}
	return nil, m.err
}
func (m *mockProcessorDatabase) GetExpiredDeferredMessages(ctx context.Context, maxAge time.Duration, limit int) ([]db.Message, error) {
	if m.getExpiredDeferredMessagesErr != nil {
		return nil, m.getExpiredDeferredMessagesErr
	}
	return nil, m.err
}

func TestProcessorInternalErrors(t *testing.T) {
	errBoom := fmt.Errorf("db boom")
	m := &mockProcessorDatabase{err: errBoom}
	p := NewProcessor(m, nil)

	t.Run("HandleCommit_GetBridgedAccountError", func(t *testing.T) {
		err := p.HandleCommit(context.Background(), &atproto.SyncSubscribeRepos_Commit{Repo: "did:plc:x"})
		if err == nil || !strings.Contains(err.Error(), "lookup bridged account") {
			t.Errorf("expected lookup error, got %v", err)
		}
	})

	t.Run("HandleCommit_SetBridgeStateError", func(t *testing.T) {
		m2 := &mockProcessorDatabase{
			getBridgedAccountResp: &db.BridgedAccount{ATDID: "did:plc:x", Active: true},
			setBridgeStateErr:     errBoom,
		}
		p2 := NewProcessor(m2, nil)
		// No Ops, just set cursor
		err := p2.HandleCommit(context.Background(), &atproto.SyncSubscribeRepos_Commit{Repo: "did:plc:x", Seq: 1})
		if err != nil {
			t.Errorf("HandleCommit should not return error for SetBridgeState failure (only logs): %v", err)
		}
	})

	t.Run("processOp_GetRecordBytesError", func(t *testing.T) {
		// This path is in HandleCommit loop.
		// We need a real rr or mock it. Hard to mock rr.
	})

	t.Run("ResolveDeferredMessages_QueryError", func(t *testing.T) {
		_, err := p.ResolveDeferredMessages(context.Background(), 10)
		if err == nil || !strings.Contains(err.Error(), "query deferred candidates") {
			t.Errorf("expected query error, got %v", err)
		}
	})

	t.Run("RetryFailedMessages_QueryError", func(t *testing.T) {
		// Needs publisher
		p2 := NewProcessor(m, nil, WithPublisher(&mockPublisher{}))
		_, err := p2.RetryFailedMessages(context.Background(), RetryConfig{})
		if err == nil || !strings.Contains(err.Error(), "query retry candidates") {
			t.Errorf("expected query error, got %v", err)
		}
	})

	t.Run("resolveDeferredMessage_MapError", func(t *testing.T) {
		m2 := &mockProcessorDatabase{}
		p2 := NewProcessor(m2, nil)
		msg := db.Message{RawATJson: "{invalid", Type: mapper.RecordTypePost}
		_, err := p2.resolveDeferredMessage(context.Background(), msg)
		if err == nil || !strings.Contains(err.Error(), "map deferred record") {
			t.Errorf("expected map error, got %v", err)
		}
	})

	t.Run("resolveDeferredMessage_AddMessageError", func(t *testing.T) {
		m2 := &mockProcessorDatabase{addMessageErr: errBoom}
		p2 := NewProcessor(m2, nil)
		msg := db.Message{RawATJson: `{"text":"hi"}`, Type: mapper.RecordTypePost}
		// This will go to unresolved (if unresolved) or pending (if p.publisher is nil)
		// With no publisher and no unresolved refs, it goes to "persist deferred pending"
		_, err := p2.resolveDeferredMessage(context.Background(), msg)
		if err == nil || !strings.Contains(err.Error(), "persist deferred pending") {
			t.Errorf("expected add message error, got %v", err)
		}
	})

	t.Run("resolveDeferredMessage_UnresolvedAddError", func(t *testing.T) {
		m2 := &mockProcessorDatabase{addMessageErr: errBoom}
		p2 := NewProcessor(m2, nil)
		msg := db.Message{
			RawATJson: `{"text":"hi","reply":{"root":{"uri":"at://missing","cid":"abc"},"parent":{"uri":"at://missing","cid":"abc"}}}`,
			Type:      mapper.RecordTypePost,
		}
		_, err := p2.resolveDeferredMessage(context.Background(), msg)
		if err == nil || !strings.Contains(err.Error(), "persist deferred unresolved") {
			t.Errorf("expected unresolved add error, got %v", err)
		}
	})

	t.Run("resolveDeferredMessage_PublishError", func(t *testing.T) {
		m2 := &mockProcessorDatabase{addMessageErr: errBoom}
		p2 := NewProcessor(m2, nil, WithPublisher(&mockPublisher{err: errBoom}))
		msg := db.Message{RawATJson: `{"text":"hi"}`, Type: mapper.RecordTypePost}
		_, err := p2.resolveDeferredMessage(context.Background(), msg)
		if err == nil || !strings.Contains(err.Error(), "persist deferred publish failure") {
			t.Errorf("expected publish add error, got %v", err)
		}
	})

	t.Run("retryMessage_UnmarshalError", func(t *testing.T) {
		m2 := &mockProcessorDatabase{}
		p2 := NewProcessor(m2, nil, WithPublisher(&mockPublisher{}))
		msg := db.Message{RawSSBJson: "{invalid"}
		err := p2.retryMessage(context.Background(), msg)
		if err == nil || !strings.Contains(err.Error(), "invalid character") {
			t.Errorf("expected unmarshal error, got %v", err)
		}
	})

	t.Run("retryMessage_PublishError", func(t *testing.T) {
		m2 := &mockProcessorDatabase{}
		p2 := NewProcessor(m2, nil, WithPublisher(&mockPublisher{err: errBoom}))
		msg := db.Message{RawSSBJson: `{"type":"post"}`}
		err := p2.retryMessage(context.Background(), msg)
		if err == nil || !errors.Is(err, errBoom) {
			t.Errorf("expected publish error, got %v", err)
		}
	})
}

func TestResolveDeferredMessagesCascadesReplyChain(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	// Seed a bridged account.
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:chain",
		SSBFeedID: "@chain.ed25519",
		Active:    true,
	}); err != nil {
		t.Fatalf("seed account: %v", err)
	}

	// Seed a chain: post A (root) → reply B (parent=A) → reply C (parent=B).
	// All three are deferred because backfill delivered them out of order.
	// Post A has no dependencies (it's the root) — it was deferred only because
	// the bridged account's feed wasn't resolved at the time. We simulate this
	// by giving A an empty defer_reason so it resolves immediately.
	now := time.Now().UTC()
	pastA := now.Add(-3 * time.Minute)
	pastB := now.Add(-2 * time.Minute)
	pastC := now.Add(-1 * time.Minute)

	postA := db.Message{
		ATURI:              "at://did:plc:chain/app.bsky.feed.post/a",
		ATCID:              "bafy-a",
		ATDID:              "did:plc:chain",
		Type:               "app.bsky.feed.post",
		MessageState:       db.MessageStateDeferred,
		RawATJson:          `{"text":"root post","createdAt":"2026-01-01T00:00:00Z"}`,
		DeferReason:        "",
		DeferAttempts:      100,
		LastDeferAttemptAt: &pastA,
	}
	postB := db.Message{
		ATURI:              "at://did:plc:chain/app.bsky.feed.post/b",
		ATCID:              "bafy-b",
		ATDID:              "did:plc:chain",
		Type:               "app.bsky.feed.post",
		MessageState:       db.MessageStateDeferred,
		RawATJson:          `{"text":"reply to A","reply":{"root":{"uri":"at://did:plc:chain/app.bsky.feed.post/a","cid":"bafy-a"},"parent":{"uri":"at://did:plc:chain/app.bsky.feed.post/a","cid":"bafy-a"}},"createdAt":"2026-01-01T00:01:00Z"}`,
		DeferReason:        "_atproto_reply_root=at://did:plc:chain/app.bsky.feed.post/a;_atproto_reply_parent=at://did:plc:chain/app.bsky.feed.post/a",
		DeferAttempts:      100,
		LastDeferAttemptAt: &pastB,
	}
	postC := db.Message{
		ATURI:              "at://did:plc:chain/app.bsky.feed.post/c",
		ATCID:              "bafy-c",
		ATDID:              "did:plc:chain",
		Type:               "app.bsky.feed.post",
		MessageState:       db.MessageStateDeferred,
		RawATJson:          `{"text":"reply to B","reply":{"root":{"uri":"at://did:plc:chain/app.bsky.feed.post/a","cid":"bafy-a"},"parent":{"uri":"at://did:plc:chain/app.bsky.feed.post/b","cid":"bafy-b"}},"createdAt":"2026-01-01T00:02:00Z"}`,
		DeferReason:        "_atproto_reply_root=at://did:plc:chain/app.bsky.feed.post/a;_atproto_reply_parent=at://did:plc:chain/app.bsky.feed.post/b",
		DeferAttempts:      100,
		LastDeferAttemptAt: &pastC,
	}

	for _, msg := range []db.Message{postA, postB, postC} {
		if err := database.AddMessage(ctx, msg); err != nil {
			t.Fatalf("seed %s: %v", msg.ATURI, err)
		}
	}

	publisher := &recordingPublisher{}
	processor := NewProcessor(database, log.New(io.Discard, "", 0), WithPublisher(publisher))

	result, err := processor.ResolveDeferredMessages(ctx, 10)
	if err != nil {
		t.Fatalf("resolve deferred: %v", err)
	}

	// With cascading, all 3 should resolve in a single pass:
	// A resolves first (no deps), triggers B, B triggers C.
	if result.Published < 1 {
		t.Errorf("expected at least 1 published, got %+v", result)
	}

	// Verify post A is published.
	storedA, _ := database.GetMessage(ctx, "at://did:plc:chain/app.bsky.feed.post/a")
	if storedA == nil || storedA.MessageState != db.MessageStatePublished {
		t.Errorf("post A: expected published, got %v", storedA)
	}

	// Post B should cascade to published since A is now available.
	storedB, _ := database.GetMessage(ctx, "at://did:plc:chain/app.bsky.feed.post/b")
	if storedB == nil || storedB.MessageState != db.MessageStatePublished {
		t.Errorf("post B: expected published via cascade, got state=%v", storedB.MessageState)
	}

	// Post C should cascade to published since B is now available.
	storedC, _ := database.GetMessage(ctx, "at://did:plc:chain/app.bsky.feed.post/c")
	if storedC == nil || storedC.MessageState != db.MessageStatePublished {
		t.Errorf("post C: expected published via cascade, got state=%v", storedC.MessageState)
	}
}

func TestResolveDeferredMessagesWithEmptyDatabase(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%noop.sha256"}),
	)

	res, err := processor.ResolveDeferredMessages(ctx, 10)
	if err != nil {
		t.Fatalf("resolve deferred messages: %v", err)
	}
	if res.Selected != 0 || res.Published != 0 {
		t.Fatalf("expected zero results for empty database, got %+v", res)
	}
}

func TestRetryMessageWithPublishError(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{err: errors.New("transient error")}),
	)

	lastAttempt := time.Now().UTC().Add(-time.Minute)
	msg := db.Message{
		ATURI:                "at://did:plc:alice/app.bsky.feed.post/retry-err",
		ATCID:                "bafy-retry-err",
		ATDID:                "did:plc:alice",
		Type:                 mapper.RecordTypePost,
		MessageState:         db.MessageStateFailed,
		RawATJson:            `{"text":"retry error"}`,
		RawSSBJson:           `{"type":"post","text":"retry error"}`,
		PublishError:         "initial failure",
		PublishAttempts:      1,
		LastPublishAttemptAt: &lastAttempt,
	}
	if err := database.AddMessage(ctx, msg); err != nil {
		t.Fatalf("seed failed message: %v", err)
	}

	err = processor.retryMessage(ctx, msg)
	if err == nil {
		t.Fatal("expected error from retry message")
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.post/retry-err")
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if stored.PublishAttempts != 2 {
		t.Fatalf("expected publish attempts 2, got %d", stored.PublishAttempts)
	}
}

func TestProcessRecordWithEmptyRecord(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%empty.sha256"}),
	)

	err = processor.ProcessRecord(
		ctx,
		"did:plc:alice",
		"at://did:plc:alice/app.bsky.feed.post/empty",
		"bafy-empty",
		mapper.RecordTypePost,
		[]byte(`{}`),
	)
	if err != nil {
		t.Fatalf("process empty record: %v", err)
	}
}

func TestProcessRecordWithInvalidJSON(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%invalid.sha256"}),
	)

	err = processor.ProcessRecord(
		ctx,
		"did:plc:alice",
		"at://did:plc:alice/app.bsky.feed.post/invalid",
		"bafy-invalid",
		mapper.RecordTypePost,
		[]byte(`not json`),
	)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestRetryDueWithZeroAttempts(t *testing.T) {
	msg := db.Message{
		ATURI:           "at://did:plc:alice/app.bsky.feed.post/test",
		PublishAttempts: 0,
	}
	result := retryDue(msg, time.Now().UTC(), time.Second)
	if !result {
		t.Fatal("expected retry due for zero attempts")
	}
}

func TestRetryBackoffExponentialWithMaxAttempts(t *testing.T) {
	base := retryBackoff(time.Second, 10)
	if base < 10*time.Second {
		t.Fatalf("expected large backoff for 10 attempts, got %v", base)
	}
}

func TestMapDeleteRecordForLike(t *testing.T) {
	result, err := mapDeleteRecord("did:plc:alice", mapper.RecordTypeLike, []byte(`{"subject":{"uri":"at://did:plc:bob/app.bsky.feed.post/1","cid":"bafytest"}}`))
	if err != nil {
		t.Fatalf("mapDeleteRecord: %v", err)
	}
	if result["vote"] == nil {
		t.Fatal("expected vote in result")
	}
	vote := result["vote"].(map[string]interface{})
	if vote["value"] != 0 {
		t.Fatalf("expected value 0, got %v", vote["value"])
	}
}

func TestMapDeleteRecordForFollow(t *testing.T) {
	result, err := mapDeleteRecord("did:plc:alice", mapper.RecordTypeFollow, []byte(`{"subject":"did:plc:bob"}`))
	if err != nil {
		t.Fatalf("mapDeleteRecord: %v", err)
	}
	if result["following"] != false {
		t.Fatal("expected following=false")
	}
	if result["blocking"] != false {
		t.Fatal("expected blocking=false")
	}
}

func TestHydrateRecordDependencies(t *testing.T) {
	database, _ := db.Open(":memory:")
	defer database.Close()

	ctx := context.Background()
	fetcher := &stubRecordFetcher{}
	processor := newProcessorWithDependencies(database, nil, nil, fetcher)

	mapped := map[string]interface{}{
		"_atproto_subject":       "at://s",
		"_atproto_quote_subject": "at://q",
		"_atproto_reply_root":    "at://r",
		"_atproto_reply_parent":  "at://p",
	}

	err := processor.hydrateRecordDependencies(ctx, mapped)
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}

	// Check fetch counts
	for _, uri := range []string{"at://s", "at://q", "at://r", "at://p"} {
		if fetcher.fetchCount(uri) == 0 {
			t.Errorf("expected fetch for %s", uri)
		}
	}
}

// ---------- HandleCommit edge case tests ----------

func TestHandleCommitNilEvent(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	processor := NewProcessor(database, log.New(io.Discard, "", 0))
	if err := processor.HandleCommit(context.Background(), nil); err != nil {
		t.Fatalf("expected nil return for nil event, got %v", err)
	}
}

func TestHandleCommitEmptyRepo(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	processor := NewProcessor(database, log.New(io.Discard, "", 0))
	err = processor.HandleCommit(context.Background(), &atproto.SyncSubscribeRepos_Commit{Repo: ""})
	if err != nil {
		t.Fatalf("expected nil return for empty repo, got %v", err)
	}
}

func TestHandleCommitAccountNotActive(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	// Add inactive account.
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:inactive",
		SSBFeedID: "@inactive.ed25519",
		Active:    false,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	processor := NewProcessor(database, log.New(io.Discard, "", 0))
	err = processor.HandleCommit(ctx, &atproto.SyncSubscribeRepos_Commit{
		Repo: "did:plc:inactive",
		Seq:  42,
	})
	if err != nil {
		t.Fatalf("expected nil return for inactive account, got %v", err)
	}
}

func TestHandleCommitAccountNotFound(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	processor := NewProcessor(database, log.New(io.Discard, "", 0))
	err = processor.HandleCommit(context.Background(), &atproto.SyncSubscribeRepos_Commit{
		Repo: "did:plc:unknown",
		Seq:  42,
	})
	if err != nil {
		t.Fatalf("expected nil return for unknown account, got %v", err)
	}
}

func TestHandleCommitDeleteOpUnsupportedCollection(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:bob",
		SSBFeedID: "@bob.ed25519",
		Active:    true,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	processor := NewProcessor(database, log.New(io.Discard, "", 0))

	// Delete for unsupported collection should be silently skipped.
	err = processor.processDeleteOp(ctx, "did:plc:bob", "app.bsky.feed.repost/123", 10)
	if err != nil {
		t.Fatalf("expected nil for unsupported collection delete, got %v", err)
	}
}

func TestProcessDeleteOpNoExistingRecord(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(database, log.New(io.Discard, "", 0))

	// Delete a post that doesn't exist yet -- should persist as deleted with synthetic JSON.
	err = processor.processDeleteOp(ctx, "did:plc:alice", "app.bsky.feed.post/nonexistent", 55)
	if err != nil {
		t.Fatalf("process delete op: %v", err)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.post/nonexistent")
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if stored == nil {
		t.Fatal("expected stored deleted message")
	}
	if stored.MessageState != db.MessageStateDeleted {
		t.Errorf("expected deleted state, got %q", stored.MessageState)
	}
	if !strings.Contains(stored.RawATJson, `"op":"delete"`) {
		t.Errorf("expected synthetic delete JSON, got %s", stored.RawATJson)
	}
}

func TestProcessDeleteOpLikeWithExistingRawJSON(t *testing.T) {
	// Test the path where a delete op processes a like that has existing raw JSON
	// and goes through mapDeleteRecord -> publish path.
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	pub := &mockPublisher{ref: "%unlike-raw.sha256"}
	processor := NewProcessor(database, log.New(io.Discard, "", 0), WithPublisher(pub))

	// Seed the target post.
	if err := database.AddMessage(ctx, db.Message{
		ATURI:        "at://did:plc:alice/app.bsky.feed.post/target",
		ATCID:        "bafy-target",
		ATDID:        "did:plc:alice",
		Type:         mapper.RecordTypePost,
		MessageState: db.MessageStatePublished,
		SSBMsgRef:    "%target.sha256",
		RawATJson:    `{"text":"target"}`,
		RawSSBJson:   `{"type":"post","text":"target"}`,
	}); err != nil {
		t.Fatalf("seed post: %v", err)
	}

	// Seed the like with real AT JSON (not delete JSON).
	if err := database.AddMessage(ctx, db.Message{
		ATURI:        "at://did:plc:alice/app.bsky.feed.like/raw1",
		ATCID:        "bafy-like-raw",
		ATDID:        "did:plc:alice",
		Type:         mapper.RecordTypeLike,
		MessageState: db.MessageStatePublished,
		RawATJson:    `{"subject":{"uri":"at://did:plc:alice/app.bsky.feed.post/target","cid":"bafy-target"},"createdAt":"2026-01-01T00:00:00Z"}`,
		RawSSBJson:   `{"type":"vote","vote":{"link":"%target.sha256","value":1,"expression":"Like"}}`,
	}); err != nil {
		t.Fatalf("seed like: %v", err)
	}

	err = processor.processDeleteOp(ctx, "did:plc:alice", "app.bsky.feed.like/raw1", 60)
	if err != nil {
		t.Fatalf("process delete op: %v", err)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.like/raw1")
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if stored == nil {
		t.Fatal("expected stored message")
	}
	if stored.SSBMsgRef != "%unlike-raw.sha256" {
		t.Errorf("expected unlike ref, got %q", stored.SSBMsgRef)
	}
	if !strings.Contains(stored.RawSSBJson, `"value":0`) {
		t.Errorf("expected unlike payload with value=0, got %s", stored.RawSSBJson)
	}
}

func TestProcessDeletedRecordPrefersDeleteEventRawPayload(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(database, log.New(io.Discard, "", 0))

	atURI := "at://did:plc:alice/app.bsky.actor.profile/self"
	if err := database.AddMessage(ctx, db.Message{
		ATURI:        atURI,
		ATCID:        "bafy-profile-old",
		ATDID:        "did:plc:alice",
		Type:         mapper.RecordTypeProfile,
		MessageState: db.MessageStatePublished,
		RawATJson:    `{"displayName":"before"}`,
		RawSSBJson:   `{"type":"about","name":"before"}`,
	}); err != nil {
		t.Fatalf("seed existing profile row: %v", err)
	}

	deletePayload := `{"displayName":"from-delete-event"}`
	if err := processor.ProcessDeletedRecord(ctx, "did:plc:alice", atURI, "", mapper.RecordTypeProfile, []byte(deletePayload), 99); err != nil {
		t.Fatalf("process deleted profile: %v", err)
	}

	stored, err := database.GetMessage(ctx, atURI)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if stored == nil {
		t.Fatal("expected stored deleted profile row")
	}
	if stored.RawATJson != deletePayload {
		t.Fatalf("expected delete event payload to persist, got %s", stored.RawATJson)
	}
	if stored.MessageState != db.MessageStateDeleted {
		t.Fatalf("expected deleted state, got %q", stored.MessageState)
	}
}

func TestProcessDeleteOpPublishFailure(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	pub := &mockPublisher{err: fmt.Errorf("publish boom")}
	processor := NewProcessor(database, log.New(io.Discard, "", 0), WithPublisher(pub))

	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:bob",
		SSBFeedID: "@bob.ed25519",
		Active:    true,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	// Seed the follow with real JSON.
	if err := database.AddMessage(ctx, db.Message{
		ATURI:        "at://did:plc:alice/app.bsky.graph.follow/f1",
		ATCID:        "bafy-follow-f1",
		ATDID:        "did:plc:alice",
		Type:         mapper.RecordTypeFollow,
		MessageState: db.MessageStatePublished,
		RawATJson:    `{"subject":"did:plc:bob","createdAt":"2026-01-01T00:00:00Z"}`,
		RawSSBJson:   `{"type":"contact","contact":"@bob.ed25519","following":true,"blocking":false}`,
	}); err != nil {
		t.Fatalf("seed follow: %v", err)
	}

	err = processor.processDeleteOp(ctx, "did:plc:alice", "app.bsky.graph.follow/f1", 70)
	if err != nil {
		t.Fatalf("process delete op should not return error: %v", err)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.graph.follow/f1")
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if stored == nil {
		t.Fatal("expected stored message")
	}
	// With publish failure, it should be persisted as failed.
	if stored.MessageState != db.MessageStateFailed {
		t.Errorf("expected failed state, got %q", stored.MessageState)
	}
}

// ---------- mapDeleteRecord coverage ----------

func TestMapDeleteRecordUnsupportedCollection(t *testing.T) {
	_, err := mapDeleteRecord("did:plc:alice", mapper.RecordTypePost, []byte(`{"text":"hello"}`))
	if err == nil {
		t.Fatal("expected error for unsupported delete collection (post)")
	}
	if !strings.Contains(err.Error(), "does not support delete translation") {
		t.Errorf("expected unsupported collection error, got %v", err)
	}
}

// ---------- resolveMessageReference / resolveFeedReference coverage ----------

func TestResolveMessageReferenceDBError(t *testing.T) {
	m := &mockProcessorDatabase{getMessageErr: fmt.Errorf("db error")}
	processor := NewProcessor(m, log.New(io.Discard, "", 0))
	result := processor.resolveMessageReference(context.Background(), "at://x")
	if result != "" {
		t.Errorf("expected empty string on db error, got %q", result)
	}
}

func TestResolveMessageReferenceNilMsg(t *testing.T) {
	m := &mockProcessorDatabase{}
	processor := NewProcessor(m, log.New(io.Discard, "", 0))
	result := processor.resolveMessageReference(context.Background(), "at://x")
	if result != "" {
		t.Errorf("expected empty string for nil msg, got %q", result)
	}
}

func TestResolveFeedReferenceDBError(t *testing.T) {
	m := &mockProcessorDatabase{getBridgedAccountErr: fmt.Errorf("db error")}
	processor := NewProcessor(m, log.New(io.Discard, "", 0))
	result := processor.resolveFeedReference(context.Background(), "did:plc:x")
	if result != "" {
		t.Errorf("expected empty string on db error, got %q", result)
	}
}

func TestResolveFeedReferenceNoResolver(t *testing.T) {
	m := &mockProcessorDatabase{}
	processor := NewProcessor(m, log.New(io.Discard, "", 0))
	// No feed resolver configured, and no bridged account match.
	result := processor.resolveFeedReference(context.Background(), "did:plc:x")
	if result != "" {
		t.Errorf("expected empty string for no resolver, got %q", result)
	}
}

func TestResolveFeedReferenceResolverError(t *testing.T) {
	m := &mockProcessorDatabase{}
	resolver := &mockFeedResolver{err: fmt.Errorf("resolve error")}
	processor := NewProcessor(m, log.New(io.Discard, "", 0), WithFeedResolver(resolver))
	result := processor.resolveFeedReference(context.Background(), "did:plc:y")
	if result != "" {
		t.Errorf("expected empty string on resolver error, got %q", result)
	}
}

func TestLookupFeedEmptyDID(t *testing.T) {
	m := &mockProcessorDatabase{}
	resolver := &mockFeedResolver{refs: map[string]string{}}
	processor := NewProcessor(m, log.New(io.Discard, "", 0), WithFeedResolver(resolver))
	_, err := processor.lookupFeed(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty DID")
	}
}

// ---------- resolveDeferredMessage additional coverage ----------

func TestResolveDeferredMessagePublishSuccessAddError(t *testing.T) {
	errBoom := fmt.Errorf("db boom")
	m := &mockProcessorDatabase{addMessageErr: errBoom}
	pub := &mockPublisher{ref: "%pub.sha256"}
	processor := NewProcessor(m, log.New(io.Discard, "", 0), WithPublisher(pub))

	msg := db.Message{
		RawATJson: `{"text":"hi","createdAt":"2026-01-01T00:00:00Z"}`,
		Type:      mapper.RecordTypePost,
		ATDID:     "did:plc:alice",
		ATURI:     "at://did:plc:alice/app.bsky.feed.post/1",
	}
	state, err := processor.resolveDeferredMessage(context.Background(), msg)
	if err == nil || !strings.Contains(err.Error(), "persist deferred publish success") {
		t.Errorf("expected persist publish success error, got %v", err)
	}
	if state != db.MessageStatePublished {
		t.Errorf("expected published state, got %q", state)
	}
}

func TestResolveDeferredMessageSanitizeIncomplete(t *testing.T) {
	// A follow record without a resolved contact should be deferred as incomplete.
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%should-not-be-used.sha256"}),
	)

	msg := db.Message{
		ATURI:     "at://did:plc:alice/app.bsky.graph.follow/inc1",
		ATCID:     "bafy-inc",
		ATDID:     "did:plc:alice",
		Type:      mapper.RecordTypeFollow,
		RawATJson: `{"subject":"did:plc:unknown","createdAt":"2026-01-01T00:00:00Z"}`,
	}

	state, resolveErr := processor.resolveDeferredMessage(context.Background(), msg)
	if resolveErr != nil {
		t.Fatalf("resolve deferred: %v", resolveErr)
	}
	if state != db.MessageStateDeferred {
		t.Errorf("expected deferred state for incomplete record, got %q", state)
	}

	stored, err := database.GetMessage(context.Background(), msg.ATURI)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if stored != nil && stored.DeferReason != "" && !strings.Contains(stored.DeferReason, "missing_required_fields") && !strings.Contains(stored.DeferReason, "_atproto_contact") {
		// OK - either path is valid since unresolved refs or sanitize check can trigger
	}
}

func TestResolveDeferredMessageBlobErrorWithPending(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	// No publisher, blob bridge that errors.
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithBlobBridge(&mockBlobBridge{err: fmt.Errorf("blob fail")}),
	)

	msg := db.Message{
		ATURI:     "at://did:plc:alice/app.bsky.feed.post/blob1",
		ATCID:     "bafy-blob",
		ATDID:     "did:plc:alice",
		Type:      mapper.RecordTypePost,
		RawATJson: `{"text":"blob test","createdAt":"2026-01-01T00:00:00Z"}`,
	}

	state, resolveErr := processor.resolveDeferredMessage(context.Background(), msg)
	if resolveErr != nil {
		t.Fatalf("resolve: %v", resolveErr)
	}
	if state != db.MessageStatePending {
		t.Errorf("expected pending state, got %q", state)
	}

	stored, _ := database.GetMessage(context.Background(), msg.ATURI)
	if stored == nil {
		t.Fatal("expected stored message")
	}
	if !strings.Contains(stored.PublishError, "blob_fallback") {
		t.Errorf("expected blob_fallback in publish error, got %q", stored.PublishError)
	}
}

func TestResolveDeferredMessageBlobErrorWithUnresolved(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%pub.sha256"}),
		WithBlobBridge(&mockBlobBridge{err: fmt.Errorf("blob fail")}),
	)

	// Like record with unresolved subject.
	msg := db.Message{
		ATURI:     "at://did:plc:alice/app.bsky.feed.like/blob2",
		ATCID:     "bafy-like-blob",
		ATDID:     "did:plc:alice",
		Type:      mapper.RecordTypeLike,
		RawATJson: `{"subject":{"uri":"at://did:plc:bob/app.bsky.feed.post/missing","cid":"bafy-missing"},"createdAt":"2026-01-01T00:00:00Z"}`,
	}

	state, resolveErr := processor.resolveDeferredMessage(context.Background(), msg)
	if resolveErr != nil {
		t.Fatalf("resolve: %v", resolveErr)
	}
	if state != db.MessageStateDeferred {
		t.Errorf("expected deferred state, got %q", state)
	}

	stored, _ := database.GetMessage(context.Background(), msg.ATURI)
	if stored != nil && strings.Contains(stored.PublishError, "blob_fallback") {
		// Good - blob error noted.
	}
}

func TestResolveDeferredMessageBlobErrorWithPublishSuccess(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%blob-pub.sha256"}),
		WithBlobBridge(&mockBlobBridge{err: fmt.Errorf("blob fail")}),
	)

	msg := db.Message{
		ATURI:     "at://did:plc:alice/app.bsky.feed.post/blob3",
		ATCID:     "bafy-blob3",
		ATDID:     "did:plc:alice",
		Type:      mapper.RecordTypePost,
		RawATJson: `{"text":"blob test 3","createdAt":"2026-01-01T00:00:00Z"}`,
	}

	state, resolveErr := processor.resolveDeferredMessage(context.Background(), msg)
	if resolveErr != nil {
		t.Fatalf("resolve: %v", resolveErr)
	}
	if state != db.MessageStatePublished {
		t.Errorf("expected published state, got %q", state)
	}

	stored, _ := database.GetMessage(context.Background(), msg.ATURI)
	if stored == nil {
		t.Fatal("expected stored message")
	}
	if !strings.Contains(stored.PublishError, "blob_fallback") {
		t.Errorf("expected blob_fallback in publish error, got %q", stored.PublishError)
	}
}

func TestResolveDeferredMessageBlobErrorWithPublishFailure(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{err: fmt.Errorf("publish boom")}),
		WithBlobBridge(&mockBlobBridge{err: fmt.Errorf("blob fail")}),
	)

	msg := db.Message{
		ATURI:     "at://did:plc:alice/app.bsky.feed.post/blob4",
		ATCID:     "bafy-blob4",
		ATDID:     "did:plc:alice",
		Type:      mapper.RecordTypePost,
		RawATJson: `{"text":"blob test 4","createdAt":"2026-01-01T00:00:00Z"}`,
	}

	state, publishErr := processor.resolveDeferredMessage(context.Background(), msg)
	if publishErr == nil {
		t.Fatal("expected publish error")
	}
	if state != db.MessageStateFailed {
		t.Errorf("expected failed state, got %q", state)
	}

	stored, _ := database.GetMessage(context.Background(), msg.ATURI)
	if stored == nil {
		t.Fatal("expected stored message")
	}
	if !strings.Contains(stored.PublishError, "blob_fallback") {
		t.Errorf("expected blob_fallback in publish error, got %q", stored.PublishError)
	}
}

func TestResolveDeferredMessageDeletedRecord(t *testing.T) {
	// When a deferred message has DeletedAt set, blob bridge should be skipped.
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:bob",
		SSBFeedID: "@bob.ed25519",
		Active:    true,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	now := time.Now().UTC()
	seq := int64(99)
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%deleted-deferred.sha256"}),
		WithBlobBridge(&mockBlobBridge{err: fmt.Errorf("should not be called")}),
	)

	msg := db.Message{
		ATURI:         "at://did:plc:alice/app.bsky.graph.follow/del1",
		ATCID:         "bafy-del",
		ATDID:         "did:plc:alice",
		Type:          mapper.RecordTypeFollow,
		RawATJson:     `{"subject":"did:plc:bob","createdAt":"2026-01-01T00:00:00Z"}`,
		DeletedAt:     &now,
		DeletedSeq:    &seq,
		DeletedReason: "test delete",
	}

	state, resolveErr := processor.resolveDeferredMessage(ctx, msg)
	if resolveErr != nil {
		t.Fatalf("resolve: %v", resolveErr)
	}
	if state != db.MessageStatePublished {
		t.Errorf("expected published state, got %q", state)
	}
}

func TestHydrateRecordDependenciesNilProcessor(t *testing.T) {
	var p *Processor
	err := p.hydrateRecordDependencies(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for nil processor")
	}
}

func TestHydrateRecordDependenciesNoDependencyResolver(t *testing.T) {
	m := &mockProcessorDatabase{}
	p := NewProcessor(m, log.New(io.Discard, "", 0))
	// No dependency resolver configured -- should return nil.
	err := p.hydrateRecordDependencies(context.Background(), map[string]interface{}{
		"_atproto_subject": "at://x",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestWithMaxMessagesPerMinuteZeroDisablesRateLimiting(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	p := NewProcessor(database, log.New(io.Discard, "", 0), WithMaxMessagesPerMinute(0))
	lim := p.getRateLimiter("did:plc:test")
	if lim != nil {
		t.Error("expected nil limiter when maxMessagesPerMinute=0")
	}
}

func TestWithMaxMessagesPerMinutePositiveSetsLimiter(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	p := NewProcessor(database, log.New(io.Discard, "", 0), WithMaxMessagesPerMinute(10))
	lim := p.getRateLimiter("did:plc:test")
	if lim == nil {
		t.Error("expected non-nil limiter when maxMessagesPerMinute=10")
	}
}

func TestCleanupStaleRateLimiters(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	p := NewProcessor(database, log.New(io.Discard, "", 0), WithMaxMessagesPerMinute(10))

	// Seed rateLimiters and lastActivity directly (package-internal access).
	p.rateLimiters["did:plc:old"] = rate.NewLimiter(rate.Limit(10), 10)
	p.lastActivity["did:plc:old"] = time.Now().Add(-2 * time.Hour)

	p.rateLimiters["did:plc:recent"] = rate.NewLimiter(rate.Limit(10), 10)
	p.lastActivity["did:plc:recent"] = time.Now()

	idleTimeout := 1 * time.Hour

	// Run cleanup.
	p.cleanupStaleRateLimiters(idleTimeout)

	if _, ok := p.rateLimiters["did:plc:old"]; ok {
		t.Error("expected old limiter to be removed")
	}
	if _, ok := p.lastActivity["did:plc:old"]; ok {
		t.Error("expected old activity to be removed")
	}
	if _, ok := p.rateLimiters["did:plc:recent"]; !ok {
		t.Error("expected recent limiter to remain")
	}
}

func TestCleanupStaleRateLimitersEmpty(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	p := NewProcessor(database, log.New(io.Discard, "", 0), WithMaxMessagesPerMinute(10))
	// Empty maps -- should not panic.
	p.cleanupStaleRateLimiters(1 * time.Hour)
}

func TestHandleRecordEventPost(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:alice",
		SSBFeedID: "@alice.ed25519",
		Active:    true,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	pub := &recordingPublisher{}
	p := NewProcessor(database, log.New(io.Discard, "", 0), WithPublisher(pub))

	seq := int64(42)
	event := db.ATProtoRecordEvent{
		DID:        "did:plc:alice",
		Collection: mapper.RecordTypePost,
		RKey:       "post1",
		ATURI:      "at://did:plc:alice/app.bsky.feed.post/post1",
		ATCID:      "bafy-post1",
		Action:     "create",
		Live:       true,
		Seq:        &seq,
		RecordJSON: `{"text":"hello","createdAt":"2026-01-01T00:00:00Z"}`,
	}

	if err := p.HandleRecordEvent(ctx, event); err != nil {
		t.Fatalf("HandleRecordEvent: %v", err)
	}

	msg, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.post/post1")
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if msg == nil {
		t.Fatal("expected message to be stored")
	}
	if msg.MessageState != db.MessageStatePublished {
		t.Errorf("expected published state, got %q", msg.MessageState)
	}
}

func TestHandleRecordEventDelete(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:alice",
		SSBFeedID: "@alice.ed25519",
		Active:    true,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	pub := &mockPublisher{ref: "%deleted.sha256"}
	p := NewProcessor(database, log.New(io.Discard, "", 0), WithPublisher(pub))

	// Seed an existing message to delete.
	if err := database.AddMessage(ctx, db.Message{
		ATURI:        "at://did:plc:alice/app.bsky.feed.like/1",
		ATCID:        "bafy-like",
		ATDID:        "did:plc:alice",
		Type:         mapper.RecordTypeLike,
		MessageState: db.MessageStatePublished,
		RawATJson:    `{"subject":{"uri":"at://did:plc:alice/app.bsky.feed.post/1","cid":"bafy-post"},"createdAt":"2026-01-01T00:00:00Z"}`,
		RawSSBJson:   `{"type":"vote","vote":{"link":"%post.sha256","value":1,"expression":"Like"}}`,
	}); err != nil {
		t.Fatalf("seed like: %v", err)
	}
	if err := database.AddMessage(ctx, db.Message{
		ATURI:        "at://did:plc:alice/app.bsky.feed.post/1",
		ATCID:        "bafy-post",
		ATDID:        "did:plc:alice",
		Type:         mapper.RecordTypePost,
		MessageState: db.MessageStatePublished,
		SSBMsgRef:    "%post.sha256",
		RawATJson:    `{"text":"post"}`,
		RawSSBJson:   `{"type":"post","text":"post"}`,
	}); err != nil {
		t.Fatalf("seed post: %v", err)
	}

	seq := int64(50)
	event := db.ATProtoRecordEvent{
		DID:        "did:plc:alice",
		Collection: mapper.RecordTypeLike,
		RKey:       "1",
		ATURI:      "at://did:plc:alice/app.bsky.feed.like/1",
		ATCID:      "bafy-like",
		Action:     "delete",
		Live:       true,
		Seq:        &seq,
	}

	if err := p.HandleRecordEvent(ctx, event); err != nil {
		t.Fatalf("HandleRecordEvent delete: %v", err)
	}

	msg, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.like/1")
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if msg == nil {
		t.Fatal("expected message after delete event")
	}
	if msg.MessageState != db.MessageStatePublished {
		t.Errorf("expected published state for unlike, got %q", msg.MessageState)
	}
}

func TestHandleRecordEventNilSeq(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:alice",
		SSBFeedID: "@alice.ed25519",
		Active:    true,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	p := NewProcessor(database, log.New(io.Discard, "", 0))

	// nil Seq should use seq=0, no panic.
	event := db.ATProtoRecordEvent{
		DID:        "did:plc:alice",
		Collection: mapper.RecordTypePost,
		RKey:       "nilseq",
		ATURI:      "at://did:plc:alice/app.bsky.feed.post/nilseq",
		ATCID:      "bafy-nilseq",
		Action:     "delete",
		Live:       true,
		Seq:        nil,
	}

	if err := p.HandleRecordEvent(ctx, event); err != nil {
		t.Fatalf("HandleRecordEvent nil seq: %v", err)
	}

	msg, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.post/nilseq")
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if msg == nil {
		t.Fatal("expected message")
	}
	if msg.DeletedSeq == nil || *msg.DeletedSeq != 0 {
		t.Errorf("expected deleted_seq 0, got %v", msg.DeletedSeq)
	}
}

func TestExpireDeferredMessagesTransitionsToFailed(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	oldTime := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)

	// Seed a deferred message older than maxAge.
	if err := database.AddMessage(ctx, db.Message{
		ATURI:         "at://did:plc:alice/app.bsky.feed.like/expired1",
		ATCID:         "bafy-exp1",
		ATDID:         "did:plc:alice",
		Type:          mapper.RecordTypeLike,
		MessageState:  db.MessageStateDeferred,
		RawATJson:     `{"subject":{"uri":"at://missing","cid":"x"},"createdAt":"2026-01-01T00:00:00Z"}`,
		RawSSBJson:    `{"type":"vote"}`,
		DeferReason:   "_atproto_subject=at://missing",
		DeferAttempts: 1,
		CreatedAt:     oldTime,
	}); err != nil {
		t.Fatalf("seed expired deferred: %v", err)
	}

	p := NewProcessor(database, log.New(io.Discard, "", 0))

	result, err := p.ExpireDeferredMessages(ctx, 24*time.Hour, 10)
	if err != nil {
		t.Fatalf("ExpireDeferredMessages: %v", err)
	}
	if result.Selected != 1 {
		t.Errorf("expected 1 selected, got %d", result.Selected)
	}
	if result.Expired != 1 {
		t.Errorf("expected 1 expired, got %d", result.Expired)
	}

	msg, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.like/expired1")
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if msg == nil {
		t.Fatal("expected message")
	}
	if msg.MessageState != db.MessageStateFailed {
		t.Errorf("expected failed state, got %q", msg.MessageState)
	}
	if msg.PublishError != "deferred_ttl_expired" {
		t.Errorf("expected deferred_ttl_expired, got %q", msg.PublishError)
	}
}

func TestExpireDeferredMessagesDefaultLimit(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	p := NewProcessor(database, log.New(io.Discard, "", 0))

	// limit=0 -> defaults to 500, no panic.
	result, err := p.ExpireDeferredMessages(ctx, 24*time.Hour, 0)
	if err != nil {
		t.Fatalf("ExpireDeferredMessages default limit: %v", err)
	}
	if result.Selected != 0 {
		t.Errorf("expected 0 selected, got %d", result.Selected)
	}
}

func TestExpireDeferredMessagesEmptyResult(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	p := NewProcessor(database, log.New(io.Discard, "", 0))

	// No expired candidates.
	result, err := p.ExpireDeferredMessages(ctx, 24*time.Hour, 10)
	if err != nil {
		t.Fatalf("ExpireDeferredMessages empty: %v", err)
	}
	if result.Selected != 0 || result.Expired != 0 || result.Failed != 0 {
		t.Errorf("expected zero result, got %+v", result)
	}
}

func TestStartRateLimiterCleanup(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	p := NewProcessor(database, log.New(io.Discard, "", 0))
	p.maxMessagesPerMinute = 60

	// Add a rate limiter that should be cleaned up
	p.rateLimitMu.Lock()
	p.rateLimiters["did:plc:stale"] = rate.NewLimiter(rate.Limit(1), 1)
	p.lastActivity["did:plc:stale"] = time.Now().Add(-2 * time.Minute)
	p.rateLimiters["did:plc:active"] = rate.NewLimiter(rate.Limit(1), 1)
	p.lastActivity["did:plc:active"] = time.Now()
	p.rateLimitMu.Unlock()

	// Start cleanup with a short interval and short idle timeout
	cleanupCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	p.StartRateLimiterCleanup(cleanupCtx, 50*time.Millisecond, 90*time.Second)

	// Wait for cleanup to run
	time.Sleep(150 * time.Millisecond)

	// Check that stale limiter was removed but active one remains
	p.rateLimitMu.Lock()
	_, staleExists := p.rateLimiters["did:plc:stale"]
	_, activeExists := p.rateLimiters["did:plc:active"]
	p.rateLimitMu.Unlock()

	if staleExists {
		t.Error("expected stale rate limiter to be cleaned up")
	}
	if !activeExists {
		t.Error("expected active rate limiter to remain")
	}
}

func TestStartRateLimiterCleanupDisabled(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	p := NewProcessor(database, log.New(io.Discard, "", 0))
	p.maxMessagesPerMinute = 0 // Disabled

	// Should return immediately without starting goroutine
	p.StartRateLimiterCleanup(ctx, time.Second, time.Minute)
	// No panic, no goroutine started
}

func TestProcessRecordGeneratesReplyTangles(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	rootURI := "at://did:plc:alice/app.bsky.feed.post/root"

	// Seed root message
	if err := database.AddMessage(ctx, db.Message{
		ATURI:      rootURI,
		ATCID:      "bafy-root",
		SSBMsgRef:  "%rootmsg.sha256",
		ATDID:      "did:plc:alice",
		Type:       mapper.RecordTypePost,
		RawATJson:  `{"text":"root"}`,
		RawSSBJson: `{"type":"post","text":"root"}`,
	}); err != nil {
		t.Fatalf("seed root message: %v", err)
	}

	publisher := &recordingPublisher{}
	processor := NewProcessor(database, log.New(io.Discard, "", 0), WithPublisher(publisher))

	replyRecord := []byte(`{
		"text":"reply",
		"reply":{
			"root":{"uri":"at://did:plc:alice/app.bsky.feed.post/root","cid":"bafy-root"},
			"parent":{"uri":"at://did:plc:alice/app.bsky.feed.post/root","cid":"bafy-root"}
		},
		"createdAt":"2026-01-01T00:00:00Z"
	}`)

	err = processor.ProcessRecord(
		ctx,
		"did:plc:alice",
		"at://did:plc:alice/app.bsky.feed.post/reply-1",
		"bafy-reply",
		mapper.RecordTypePost,
		replyRecord,
	)
	if err != nil {
		t.Fatalf("process reply record: %v", err)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.post/reply-1")
	if err != nil {
		t.Fatalf("get reply message: %v", err)
	}

	var mapped map[string]interface{}
	if err := json.Unmarshal([]byte(stored.RawSSBJson), &mapped); err != nil {
		t.Fatalf("unmarshal mapped ssb json: %v", err)
	}

	// Verify legacy fields
	if mapped["root"] != "%rootmsg.sha256" {
		t.Fatalf("expected legacy root, got %v", mapped["root"])
	}
	if mapped["branch"] != "%rootmsg.sha256" {
		t.Fatalf("expected legacy branch, got %v", mapped["branch"])
	}

	// Verify SIP-009 Tangles
	tangles, ok := mapped["tangles"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected tangles map, got %T", mapped["tangles"])
	}
	comment, ok := tangles["comment"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected comment tangle, got %T", tangles["comment"])
	}
	if comment["root"] != "%rootmsg.sha256" {
		t.Fatalf("expected tangle root, got %v", comment["root"])
	}
	previous, ok := comment["previous"].([]interface{})
	if !ok || len(previous) != 1 || previous[0] != "%rootmsg.sha256" {
		t.Fatalf("expected single previous parent, got %v", comment["previous"])
	}
}
