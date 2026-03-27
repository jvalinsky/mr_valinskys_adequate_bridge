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

func (m *mockBlobBridge) BridgeRecordBlobs(context.Context, string, map[string]interface{}, []byte) error {
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

func TestProcessRecordAutoFetchesRepostSubjectDependency(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	subjectURI := "at://did:plc:bob/app.bsky.feed.post/repost-root"
	publisher := &recordingPublisher{}
	fetcher := &stubRecordFetcher{
		records: map[string]FetchedRecord{
			subjectURI: {
				ATDID:      "did:plc:bob",
				ATURI:      subjectURI,
				ATCID:      "bafy-repost-root",
				Collection: mapper.RecordTypePost,
				RecordJSON: []byte(`{"text":"repost root","createdAt":"2026-01-01T00:00:00Z"}`),
			},
		},
	}
	processor := newProcessorWithDependencies(database, publisher, nil, fetcher)

	repostRecord := []byte(`{
		"subject": {
			"uri": "at://did:plc:bob/app.bsky.feed.post/repost-root",
			"cid": "bafy-repost-root"
		},
		"createdAt": "2026-01-01T00:00:00Z"
	}`)
	if err := processor.ProcessRecord(
		ctx,
		"did:plc:alice",
		"at://did:plc:alice/app.bsky.feed.repost/auto-1",
		"bafy-repost-auto-1",
		mapper.RecordTypeRepost,
		repostRecord,
	); err != nil {
		t.Fatalf("process repost record: %v", err)
	}

	repost, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.repost/auto-1")
	if err != nil {
		t.Fatalf("get repost message: %v", err)
	}
	if repost == nil || repost.MessageState != db.MessageStatePublished {
		t.Fatalf("expected repost to publish after dependency resolution, got %+v", repost)
	}

	published := publisher.snapshot()
	if len(published) != 2 {
		t.Fatalf("expected 2 publish calls, got %d", len(published))
	}
	if published[1]["text"] != "[%pub-1.sha256]" {
		t.Fatalf("expected repost to reference dependency ref in text, got %+v", published[1])
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
				ATCID:      "bafy-profile",
				Collection: mapper.RecordTypeProfile,
				RecordJSON: []byte(`{"displayName":"unsupported profile"}`),
			},
		},
	}
	processor := newProcessorWithDependencies(database, publisher, nil, fetcher)

	likeRecord := []byte(`{
		"subject": {
			"uri": "at://did:plc:bob/app.bsky.feed.post/missing",
			"cid": "bafy-profile"
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

func TestProcessDeleteOpMarksDeletedAndPublishesTombstone(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%tombstone.sha256"}),
	)

	if err := processor.processDeleteOp(ctx, "did:plc:alice", "app.bsky.feed.post/123", 42); err != nil {
		t.Fatalf("process delete op: %v", err)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.feed.post/123")
	if err != nil {
		t.Fatalf("get deleted message: %v", err)
	}
	if stored == nil {
		t.Fatalf("expected deleted message row")
	}
	if stored.MessageState != db.MessageStateDeleted {
		t.Fatalf("expected deleted state, got %q", stored.MessageState)
	}
	if stored.SSBMsgRef != "%tombstone.sha256" {
		t.Fatalf("expected tombstone publish ref, got %q", stored.SSBMsgRef)
	}
	if stored.DeletedSeq == nil || *stored.DeletedSeq != 42 {
		t.Fatalf("expected deleted seq 42, got %+v", stored.DeletedSeq)
	}
}

func TestProcessDeleteOpMarksFollowDeletedAndPublishesTombstone(t *testing.T) {
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	processor := NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%follow-tombstone.sha256"}),
	)

	if err := processor.processDeleteOp(ctx, "did:plc:alice", "app.bsky.graph.follow/123", 77); err != nil {
		t.Fatalf("process follow delete op: %v", err)
	}

	stored, err := database.GetMessage(ctx, "at://did:plc:alice/app.bsky.graph.follow/123")
	if err != nil {
		t.Fatalf("get deleted follow message: %v", err)
	}
	if stored == nil {
		t.Fatalf("expected deleted follow message row")
	}
	if stored.MessageState != db.MessageStateDeleted {
		t.Fatalf("expected follow deleted state, got %q", stored.MessageState)
	}
	if stored.SSBMsgRef != "%follow-tombstone.sha256" {
		t.Fatalf("expected follow tombstone publish ref, got %q", stored.SSBMsgRef)
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
