package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"strings"
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
