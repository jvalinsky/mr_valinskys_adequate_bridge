package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr_valinskys_adequate_bridge/internal/db"
	"github.com/mr_valinskys_adequate_bridge/internal/mapper"
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
	mu      sync.Mutex
	refs    map[string]string
	lookups map[string]int
	waitCh  chan struct{}
	err     error
}

func (m *mockFeedResolver) ResolveFeed(ctx context.Context, did string) (string, error) {
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
	mu      sync.Mutex
	records map[string]FetchedRecord
	errors  map[string]error
	fetches map[string]int
	waitCh  chan struct{}
}

func (f *stubRecordFetcher) FetchRecord(ctx context.Context, atURI string) (FetchedRecord, error) {
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

	ctx := context.Background()
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

	time.Sleep(50 * time.Millisecond)
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

	time.Sleep(50 * time.Millisecond)
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
