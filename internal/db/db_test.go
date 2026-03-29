package db

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDB(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Test adding and getting account
	acc := BridgedAccount{
		ATDID:     "did:plc:123",
		SSBFeedID: "@abc.ed25519",
		Active:    true,
	}

	if err := db.AddBridgedAccount(ctx, acc); err != nil {
		t.Fatalf("failed to add account: %v", err)
	}

	gotAcc, err := db.GetBridgedAccount(ctx, "did:plc:123")
	if err != nil {
		t.Fatalf("failed to get account: %v", err)
	}
	if gotAcc == nil {
		t.Fatalf("expected account, got nil")
	}
	if gotAcc.SSBFeedID != acc.SSBFeedID {
		t.Errorf("expected ssb_feed_id %q, got %q", acc.SSBFeedID, gotAcc.SSBFeedID)
	}

	// Test adding and getting message
	msg := Message{
		ATURI:        "at://did:plc:123/app.bsky.feed.post/456",
		ATCID:        "bafy123",
		SSBMsgRef:    "%msg123.sha256",
		ATDID:        "did:plc:123",
		Type:         "app.bsky.feed.post",
		MessageState: MessageStatePublished,
		RawATJson:    `{"text":"hello"}`,
		RawSSBJson:   `{"type":"post","text":"hello"}`,
	}

	if err := db.AddMessage(ctx, msg); err != nil {
		t.Fatalf("failed to add message: %v", err)
	}

	gotMsg, err := db.GetMessage(ctx, msg.ATURI)
	if err != nil {
		t.Fatalf("failed to get message: %v", err)
	}
	if gotMsg == nil {
		t.Fatalf("expected message, got nil")
	}
	if gotMsg.SSBMsgRef != msg.SSBMsgRef {
		t.Errorf("expected ssb_msg_ref %q, got %q", msg.SSBMsgRef, gotMsg.SSBMsgRef)
	}
	if gotMsg.PublishAttempts != 0 {
		t.Errorf("expected publish attempts 0, got %d", gotMsg.PublishAttempts)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if err := db.AddMessage(ctx, Message{
		ATURI:                msg.ATURI,
		ATCID:                msg.ATCID,
		SSBMsgRef:            msg.SSBMsgRef,
		ATDID:                msg.ATDID,
		Type:                 msg.Type,
		MessageState:         MessageStateFailed,
		RawATJson:            msg.RawATJson,
		RawSSBJson:           msg.RawSSBJson,
		PublishedAt:          &now,
		PublishError:         "temporary publish error",
		PublishAttempts:      1,
		LastPublishAttemptAt: &now,
	}); err != nil {
		t.Fatalf("failed to upsert message publish metadata: %v", err)
	}

	gotMsg, err = db.GetMessage(ctx, msg.ATURI)
	if err != nil {
		t.Fatalf("failed to re-get message: %v", err)
	}
	if gotMsg.PublishAttempts != 1 {
		t.Errorf("expected publish attempts 1, got %d", gotMsg.PublishAttempts)
	}
	if gotMsg.PublishError == "" {
		t.Errorf("expected publish_error to be stored")
	}
	if gotMsg.PublishedAt == nil {
		t.Errorf("expected published_at to be stored")
	}
	if gotMsg.LastPublishAttemptAt == nil {
		t.Errorf("expected last_publish_attempt_at to be stored")
	}

	totalAccounts, err := db.CountBridgedAccounts(ctx)
	if err != nil {
		t.Fatalf("failed to count accounts: %v", err)
	}
	if totalAccounts != 1 {
		t.Fatalf("expected 1 account, got %d", totalAccounts)
	}

	activeAccounts, err := db.CountActiveBridgedAccounts(ctx)
	if err != nil {
		t.Fatalf("failed to count active accounts: %v", err)
	}
	if activeAccounts != 1 {
		t.Fatalf("expected 1 active account, got %d", activeAccounts)
	}

	if err := db.AddBridgedAccount(ctx, BridgedAccount{
		ATDID:     "did:plc:inactive",
		SSBFeedID: "@inactive.ed25519",
		Active:    false,
	}); err != nil {
		t.Fatalf("failed to add inactive account: %v", err)
	}

	activeList, err := db.ListActiveBridgedAccounts(ctx)
	if err != nil {
		t.Fatalf("failed to list active accounts: %v", err)
	}
	if len(activeList) != 1 {
		t.Fatalf("expected 1 active account row, got %d", len(activeList))
	}
	if activeList[0].ATDID != acc.ATDID {
		t.Fatalf("expected active account DID %q, got %q", acc.ATDID, activeList[0].ATDID)
	}

	totalMessages, err := db.CountMessages(ctx)
	if err != nil {
		t.Fatalf("failed to count messages: %v", err)
	}
	if totalMessages != 1 {
		t.Fatalf("expected 1 message, got %d", totalMessages)
	}

	recentMessages, err := db.GetRecentMessages(ctx, 10)
	if err != nil {
		t.Fatalf("failed to get recent messages: %v", err)
	}
	if len(recentMessages) != 1 {
		t.Fatalf("expected 1 recent message, got %d", len(recentMessages))
	}
	if recentMessages[0].ATURI != msg.ATURI {
		t.Fatalf("expected recent message URI %q, got %q", msg.ATURI, recentMessages[0].ATURI)
	}

	if err := db.SetBridgeState(ctx, "firehose_seq", "12345"); err != nil {
		t.Fatalf("failed to set bridge state: %v", err)
	}
	stateVal, ok, err := db.GetBridgeState(ctx, "firehose_seq")
	if err != nil {
		t.Fatalf("failed to get bridge state: %v", err)
	}
	if !ok || stateVal != "12345" {
		t.Fatalf("expected bridge state 12345, got %q (ok=%v)", stateVal, ok)
	}

	if err := db.AddBlob(ctx, Blob{
		ATCID:      "bafyblob1",
		SSBBlobRef: "&blobref.sha256",
		Size:       12,
		MimeType:   "image/png",
	}); err != nil {
		t.Fatalf("failed to add blob: %v", err)
	}
	blob, err := db.GetBlob(ctx, "bafyblob1")
	if err != nil {
		t.Fatalf("failed to get blob: %v", err)
	}
	if blob == nil || blob.SSBBlobRef != "&blobref.sha256" {
		t.Fatalf("expected blob mapping to be stored")
	}

	blobCount, err := db.CountBlobs(ctx)
	if err != nil {
		t.Fatalf("failed to count blobs: %v", err)
	}
	if blobCount != 1 {
		t.Fatalf("expected 1 blob, got %d", blobCount)
	}

	failureCount, err := db.CountPublishFailures(ctx)
	if err != nil {
		t.Fatalf("failed to count publish failures: %v", err)
	}
	if failureCount != 1 {
		t.Fatalf("expected 1 publish failure, got %d", failureCount)
	}

	retryAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	if err := db.AddMessage(ctx, Message{
		ATURI:                "at://did:plc:123/app.bsky.feed.post/retry",
		ATCID:                "bafy-retry",
		SSBMsgRef:            "",
		ATDID:                "did:plc:123",
		Type:                 "app.bsky.feed.post",
		MessageState:         MessageStateFailed,
		RawATJson:            `{"text":"retry me"}`,
		RawSSBJson:           `{"type":"post","text":"retry me"}`,
		PublishError:         "publish failed",
		PublishAttempts:      2,
		LastPublishAttemptAt: &retryAt,
	}); err != nil {
		t.Fatalf("failed to add retry candidate: %v", err)
	}

	retryCandidates, err := db.GetRetryCandidates(ctx, 10, "did:plc:123", 3)
	if err != nil {
		t.Fatalf("failed to query retry candidates: %v", err)
	}
	if len(retryCandidates) != 1 {
		t.Fatalf("expected 1 retry candidate, got %d", len(retryCandidates))
	}
	if retryCandidates[0].ATURI != "at://did:plc:123/app.bsky.feed.post/retry" {
		t.Fatalf("unexpected retry candidate URI %q", retryCandidates[0].ATURI)
	}

	if err := db.AddMessage(ctx, Message{
		ATURI:              "at://did:plc:123/app.bsky.feed.like/deferred",
		ATCID:              "bafy-deferred",
		ATDID:              "did:plc:123",
		Type:               "app.bsky.feed.like",
		MessageState:       MessageStateDeferred,
		RawATJson:          `{"subject":{"uri":"at://missing"}}`,
		RawSSBJson:         `{"_atproto_subject":"at://missing"}`,
		DeferReason:        "_atproto_subject=at://missing",
		DeferAttempts:      1,
		LastDeferAttemptAt: &retryAt,
	}); err != nil {
		t.Fatalf("failed to add deferred message: %v", err)
	}
	deferredCount, err := db.CountDeferredMessages(ctx)
	if err != nil {
		t.Fatalf("failed to count deferred messages: %v", err)
	}
	if deferredCount != 1 {
		t.Fatalf("expected 1 deferred message, got %d", deferredCount)
	}
	reason, ok, err := db.GetLatestDeferredReason(ctx)
	if err != nil {
		t.Fatalf("failed to get latest deferred reason: %v", err)
	}
	if !ok || reason == "" {
		t.Fatalf("expected latest deferred reason")
	}

	seq := int64(99)
	if err := db.AddMessage(ctx, Message{
		ATURI:         "at://did:plc:123/app.bsky.feed.post/deleted",
		ATCID:         "bafy-deleted",
		ATDID:         "did:plc:123",
		Type:          "app.bsky.feed.post",
		MessageState:  MessageStateDeleted,
		RawATJson:     `{"op":"delete"}`,
		RawSSBJson:    `{"type":"bridge/tombstone"}`,
		DeletedAt:     &retryAt,
		DeletedSeq:    &seq,
		DeletedReason: "atproto_delete seq=99",
	}); err != nil {
		t.Fatalf("failed to add deleted message: %v", err)
	}
	deletedCount, err := db.CountDeletedMessages(ctx)
	if err != nil {
		t.Fatalf("failed to count deleted messages: %v", err)
	}
	if deletedCount != 1 {
		t.Fatalf("expected 1 deleted message, got %d", deletedCount)
	}

	deferredCandidates, err := db.GetDeferredCandidates(ctx, 10)
	if err != nil {
		t.Fatalf("failed to query deferred candidates: %v", err)
	}
	if len(deferredCandidates) != 1 || deferredCandidates[0].ATURI != "at://did:plc:123/app.bsky.feed.like/deferred" {
		t.Fatalf("unexpected deferred candidates: %+v", deferredCandidates)
	}
}

func TestGetAllBridgedAccounts(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.AddBridgedAccount(ctx, BridgedAccount{ATDID: "did:plc:a", SSBFeedID: "@a.ed25519", Active: true}); err != nil {
		t.Fatalf("add account a: %v", err)
	}
	if err := db.AddBridgedAccount(ctx, BridgedAccount{ATDID: "did:plc:b", SSBFeedID: "@b.ed25519", Active: false}); err != nil {
		t.Fatalf("add account b: %v", err)
	}

	accounts, err := db.GetAllBridgedAccounts(ctx)
	if err != nil {
		t.Fatalf("GetAllBridgedAccounts: %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accounts))
	}
}

func TestListBridgedAccountsWithStats(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.AddBridgedAccount(ctx, BridgedAccount{ATDID: "did:plc:stats", SSBFeedID: "@stats.ed25519", Active: true}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	accounts, err := db.ListBridgedAccountsWithStats(ctx)
	if err != nil {
		t.Fatalf("ListBridgedAccountsWithStats: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
}

func TestListRecentPublishedMessagesByDID(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.AddMessage(ctx, Message{
		ATURI:        "at://did:plc:alice/app.bsky.feed.post/msg1",
		ATCID:        "bafy1",
		SSBMsgRef:    "%msg1.sha256",
		ATDID:        "did:plc:alice",
		Type:         "app.bsky.feed.post",
		MessageState: MessageStatePublished,
		RawATJson:    `{"text":"hello"}`,
		RawSSBJson:   `{"type":"post","text":"hello"}`,
	}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	messages, err := db.ListRecentPublishedMessagesByDID(ctx, "did:plc:alice", 10)
	if err != nil {
		t.Fatalf("ListRecentPublishedMessagesByDID: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
}

func TestListMessages(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.AddMessage(ctx, Message{
		ATURI:        "at://did:plc:alice/app.bsky.feed.post/listtest",
		ATCID:        "bafylist",
		ATDID:        "did:plc:alice",
		Type:         "app.bsky.feed.post",
		MessageState: MessageStatePublished,
		RawATJson:    `{"text":"test"}`,
		RawSSBJson:   `{"type":"post","text":"test"}`,
	}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	messages, err := db.ListMessages(ctx, MessageListQuery{ATDID: "did:plc:alice"})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
}

func TestListMessageTypes(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.AddMessage(ctx, Message{
		ATURI:        "at://did:plc:alice/app.bsky.feed.post/type1",
		ATCID:        "bafytype1",
		ATDID:        "did:plc:alice",
		Type:         "app.bsky.feed.post",
		MessageState: MessageStatePublished,
		RawATJson:    `{"text":"test"}`,
		RawSSBJson:   `{"type":"post"}`,
	}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	types, err := db.ListMessageTypes(ctx)
	if err != nil {
		t.Fatalf("ListMessageTypes: %v", err)
	}
	if len(types) != 1 {
		t.Fatalf("expected 1 type, got %d", len(types))
	}
}

func TestCountPublishedMessages(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.AddMessage(ctx, Message{
		ATURI:        "at://did:plc:alice/app.bsky.feed.post/counted",
		ATCID:        "bafypub",
		SSBMsgRef:    "%counted.sha256",
		ATDID:        "did:plc:alice",
		Type:         "app.bsky.feed.post",
		MessageState: MessageStatePublished,
		RawATJson:    `{"text":"test"}`,
		RawSSBJson:   `{"type":"post"}`,
	}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	count, err := db.CountPublishedMessages(ctx)
	if err != nil {
		t.Fatalf("CountPublishedMessages: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 published message, got %d", count)
	}
}

func TestGetPublishFailures(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.AddMessage(ctx, Message{
		ATURI:           "at://did:plc:alice/app.bsky.feed.post/fail",
		ATCID:           "bafyfail",
		ATDID:           "did:plc:alice",
		Type:            "app.bsky.feed.post",
		MessageState:    MessageStateFailed,
		PublishError:    "test error",
		PublishAttempts: 1,
		RawATJson:       `{"text":"test"}`,
		RawSSBJson:      `{"type":"post"}`,
	}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	failures, err := db.GetPublishFailures(ctx, 10)
	if err != nil {
		t.Fatalf("GetPublishFailures: %v", err)
	}
	if len(failures) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(failures))
	}
}

func TestGetRecentBlobs(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.AddBlob(ctx, Blob{
		ATCID:      "bafyrecent",
		SSBBlobRef: "&recent.sha256",
		Size:       100,
		MimeType:   "image/png",
	}); err != nil {
		t.Fatalf("add blob: %v", err)
	}

	blobs, err := db.GetRecentBlobs(ctx, 10)
	if err != nil {
		t.Fatalf("GetRecentBlobs: %v", err)
	}
	if len(blobs) != 1 {
		t.Fatalf("expected 1 blob, got %d", len(blobs))
	}
}

func TestCheckBridgeHealth(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.SetBridgeState(ctx, "bridge_runtime_status", "live"); err != nil {
		t.Fatalf("set state: %v", err)
	}
	if err := db.SetBridgeState(ctx, "bridge_runtime_last_heartbeat_at", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("set heartbeat: %v", err)
	}

	health, err := db.CheckBridgeHealth(ctx, 60*time.Second)
	if err != nil {
		t.Fatalf("CheckBridgeHealth: %v", err)
	}
	if !health.Healthy {
		t.Fatalf("expected healthy")
	}
}

func TestCheckBridgeHealthUnhealthy(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	health, err := db.CheckBridgeHealth(ctx, 60*time.Second)
	if err != nil {
		t.Fatalf("CheckBridgeHealth: %v", err)
	}
	if health.Healthy {
		t.Fatalf("expected unhealthy when no state set")
	}
}

func TestGetAllBridgeState(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.SetBridgeState(ctx, "key1", "value1"); err != nil {
		t.Fatalf("set state: %v", err)
	}
	if err := db.SetBridgeState(ctx, "key2", "value2"); err != nil {
		t.Fatalf("set state: %v", err)
	}

	allState, err := db.GetAllBridgeState(ctx)
	if err != nil {
		t.Fatalf("GetAllBridgeState: %v", err)
	}
	if len(allState) != 2 {
		t.Fatalf("expected 2 state entries, got %d", len(allState))
	}
}

func TestNormalizeMessageLimit(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
		{0, 100},
		{-1, 100},
		{1, 1},
		{100, 100},
		{499, 499},
		{500, 500},
		{501, 500},
		{1000, 500},
	}
	for _, tt := range tests {
		got := normalizeMessageLimit(tt.input)
		if got != tt.want {
			t.Errorf("normalizeMessageLimit(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeMessageSort(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"oldest", "oldest"},
		{"attempts_desc", "attempts_desc"},
		{"attempts_asc", "attempts_asc"},
		{"type_asc", "type_asc"},
		{"type_desc", "type_desc"},
		{"state_asc", "state_asc"},
		{"state_desc", "state_desc"},
		{"invalid", "newest"},
		{"", "newest"},
		{"newest", "newest"},
	}
	for _, tt := range tests {
		got := normalizeMessageSort(tt.input)
		if got != tt.want {
			t.Errorf("normalizeMessageSort(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeMessageDirection(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"prev", "prev"},
		{"asc", "next"},
		{"desc", "next"},
		{"", "next"},
		{"invalid", "next"},
	}
	for _, tt := range tests {
		got := normalizeMessageDirection(tt.input)
		if got != tt.want {
			t.Errorf("normalizeMessageDirection(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeMessageListQuery(t *testing.T) {
	query := normalizeMessageListQuery(MessageListQuery{
		Search: "  hello  ",
		Type:   "  app.bsky.feed.post  ",
		State:  "  failed  ",
		Sort:   "invalid",
		Limit:  0,
		ATDID:  "  did:plc:alice  ",
	})
	if query.Search != "hello" {
		t.Errorf("expected trimmed search, got %q", query.Search)
	}
	if query.Sort != "newest" {
		t.Errorf("expected default sort, got %q", query.Sort)
	}
	if query.Limit != 100 {
		t.Errorf("expected default limit, got %d", query.Limit)
	}
}

func TestMessageOrderClause(t *testing.T) {
	tests := []struct {
		sort string
		want string
	}{
		{"oldest", "created_at ASC, at_uri ASC"},
		{"attempts_desc", "(publish_attempts + defer_attempts) DESC, created_at DESC, at_uri DESC"},
		{"attempts_asc", "(publish_attempts + defer_attempts) ASC, created_at DESC, at_uri DESC"},
		{"type_asc", "type ASC, created_at DESC, at_uri DESC"},
		{"type_desc", "type DESC, created_at DESC, at_uri DESC"},
		{"state_asc", "message_state ASC, created_at DESC, at_uri DESC"},
		{"state_desc", "message_state DESC, created_at DESC, at_uri DESC"},
		{"invalid", "created_at DESC, at_uri DESC"},
		{"", "created_at DESC, at_uri DESC"},
		{"newest", "created_at DESC, at_uri DESC"},
	}
	for _, tt := range tests {
		got := messageOrderClause(tt.sort)
		if got != tt.want {
			t.Errorf("messageOrderClause(%q) = %q, want %q", tt.sort, got, tt.want)
		}
	}
}

func TestAppendMessageListFilters(t *testing.T) {
	var builder strings.Builder
	var args []interface{}

	query := MessageListQuery{
		Search:   "test",
		Type:     "post",
		State:    "failed",
		ATDID:    "did:plc:alice",
		HasIssue: true,
	}
	appendMessageListFilters(&builder, &args, query)

	sql := builder.String()
	if !strings.Contains(sql, "LIKE") {
		t.Error("expected LIKE clause for search")
	}
	if !strings.Contains(sql, "type =") {
		t.Error("expected type filter")
	}
	if !strings.Contains(sql, "message_state =") {
		t.Error("expected state filter")
	}
	if !strings.Contains(sql, "at_did =") {
		t.Error("expected DID filter")
	}
	if len(args) < 6 {
		t.Errorf("expected at least 6 args, got %d", len(args))
	}
}

func TestAppendMessageListFiltersEmpty(t *testing.T) {
	var builder strings.Builder
	var args []interface{}
	appendMessageListFilters(&builder, &args, MessageListQuery{})
	if builder.Len() != 0 {
		t.Errorf("expected empty builder, got %q", builder.String())
	}
}

func TestEncodeMessageListCursor(t *testing.T) {
	cursor := messageListCursor{
		CreatedAt: time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
		ATURI:     "at://did:plc:test/app.bsky.feed.post/1",
	}
	encoded := encodeMessageListCursor(cursor)
	if encoded == "" {
		t.Error("expected non-empty encoding")
	}

	decoded, ok := decodeMessageListCursor(encoded)
	if !ok {
		t.Error("expected successful decode")
	}
	if decoded.CreatedAt.Unix() != cursor.CreatedAt.Unix() {
		t.Errorf("time mismatch: got %v, want %v", decoded.CreatedAt, cursor.CreatedAt)
	}
	if decoded.ATURI != cursor.ATURI {
		t.Errorf("uri mismatch: got %v, want %v", decoded.ATURI, cursor.ATURI)
	}
}

func TestDecodeMessageListCursorEmpty(t *testing.T) {
	_, ok := decodeMessageListCursor("")
	if ok {
		t.Error("expected false for empty string")
	}
	_, ok = decodeMessageListCursor("invalid")
	if ok {
		t.Error("expected false for invalid string")
	}
}

func TestSupportsMessageKeysetSort(t *testing.T) {
	if !supportsMessageKeysetSort("newest") {
		t.Error("newest should support keyset")
	}
	if !supportsMessageKeysetSort("oldest") {
		t.Error("oldest should support keyset")
	}
	if supportsMessageKeysetSort("attempts_desc") {
		t.Error("attempts_desc should not support keyset")
	}
}

func TestMessageKeysetClause(t *testing.T) {
	cursor := messageListCursor{
		CreatedAt: time.Now(),
		ATURI:     "at://did:plc:test/app.bsky.feed.post/1",
	}
	clause, _, _ := messageKeysetClause("newest", "next", cursor)
	if !strings.Contains(clause, "created_at") {
		t.Error("expected created_at in keyset clause")
	}
}

func TestAddBridgedAccountTwice(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	acc := BridgedAccount{ATDID: "did:plc:twice", SSBFeedID: "@twice.ed25519", Active: true}
	if err := db.AddBridgedAccount(ctx, acc); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := db.AddBridgedAccount(ctx, acc); err != nil {
		t.Fatalf("second add (upsert): %v", err)
	}
}

func TestAddMessageTwice(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	msg := Message{
		ATURI:        "at://did:plc:twice/app.bsky.feed.post/1",
		ATCID:        "bafy-twice",
		ATDID:        "did:plc:twice",
		Type:         "app.bsky.feed.post",
		MessageState: MessageStatePublished,
		RawATJson:    `{"text":"test"}`,
		RawSSBJson:   `{"type":"post"}`,
	}
	if err := db.AddMessage(ctx, msg); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := db.AddMessage(ctx, msg); err != nil {
		t.Fatalf("second add (upsert): %v", err)
	}
}

func TestAddBlobTwice(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	blob := Blob{ATCID: "bafyblob-twice", SSBBlobRef: "&twice.sha256", Size: 100, MimeType: "image/png"}
	if err := db.AddBlob(ctx, blob); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := db.AddBlob(ctx, blob); err != nil {
		t.Fatalf("second add (upsert): %v", err)
	}
}

func TestSetBridgeStateTwice(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.SetBridgeState(ctx, "key", "value1"); err != nil {
		t.Fatalf("first set: %v", err)
	}
	if err := db.SetBridgeState(ctx, "key", "value2"); err != nil {
		t.Fatalf("second set: %v", err)
	}
	val, _, err := db.GetBridgeState(ctx, "key")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if val != "value2" {
		t.Errorf("expected value2, got %q", val)
	}
}

func TestGetBridgedAccountNotFound(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	acc, err := db.GetBridgedAccount(ctx, "did:plc:notfound")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if acc != nil {
		t.Error("expected nil for not found")
	}
}

func TestGetMessageNotFound(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	msg, err := db.GetMessage(ctx, "at://did:plc:notfound/app.bsky.feed.post/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != nil {
		t.Error("expected nil for not found")
	}
}

func TestGetBlobNotFound(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	blob, err := db.GetBlob(ctx, "bafynotfound")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if blob != nil {
		t.Error("expected nil for not found")
	}
}

func TestGetBridgeStateNotFound(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	_, ok, err := db.GetBridgeState(ctx, "notfound")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected not found")
	}
}

func TestEncodeMessageListCursorEdgeCases(t *testing.T) {
	// Zero time should return empty string.
	got := encodeMessageListCursor(messageListCursor{ATURI: "at://x"})
	if got != "" {
		t.Errorf("expected empty for zero time, got %q", got)
	}
	// Empty ATURI should return empty string.
	got = encodeMessageListCursor(messageListCursor{CreatedAt: time.Now(), ATURI: ""})
	if got != "" {
		t.Errorf("expected empty for empty ATURI, got %q", got)
	}
	// Whitespace-only ATURI should return empty string.
	got = encodeMessageListCursor(messageListCursor{CreatedAt: time.Now(), ATURI: "   "})
	if got != "" {
		t.Errorf("expected empty for whitespace ATURI, got %q", got)
	}
}

func TestDecodeMessageListCursorEdgeCases(t *testing.T) {
	// Valid base64 but invalid JSON.
	_, ok := decodeMessageListCursor("bm90anNvbg")
	if ok {
		t.Error("expected false for non-JSON payload")
	}

	// Valid JSON but invalid time format.
	payload := `{"created_at":"not-a-time","at_uri":"at://x"}`
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	_, ok = decodeMessageListCursor(encoded)
	if ok {
		t.Error("expected false for bad time format")
	}

	// Valid JSON with empty ATURI.
	payload = `{"created_at":"2026-01-01T00:00:00Z","at_uri":""}`
	encoded = base64.RawURLEncoding.EncodeToString([]byte(payload))
	_, ok = decodeMessageListCursor(encoded)
	if ok {
		t.Error("expected false for empty ATURI in payload")
	}
}

func TestMessageKeysetClauseAllBranches(t *testing.T) {
	cursor := messageListCursor{
		CreatedAt: time.Now(),
		ATURI:     "at://did:plc:test/app.bsky.feed.post/1",
	}

	tests := []struct {
		sort      string
		direction string
		wantGT    bool // expect > in clause
		wantLT    bool // expect < in clause
		reverse   bool
	}{
		{"newest", "prev", true, false, true},
		{"oldest", "prev", false, true, true},
		{"newest", "next", false, true, false},
		{"oldest", "next", true, false, false},
	}

	for _, tt := range tests {
		clause, args, reverse := messageKeysetClause(tt.sort, tt.direction, cursor)
		if clause == "" {
			t.Errorf("sort=%s dir=%s: empty clause", tt.sort, tt.direction)
		}
		if len(args) != 3 {
			t.Errorf("sort=%s dir=%s: expected 3 args, got %d", tt.sort, tt.direction, len(args))
		}
		if reverse != tt.reverse {
			t.Errorf("sort=%s dir=%s: reverse=%v, want %v", tt.sort, tt.direction, reverse, tt.reverse)
		}
		if tt.wantGT && !strings.Contains(clause, ">") {
			t.Errorf("sort=%s dir=%s: expected > in clause", tt.sort, tt.direction)
		}
		if tt.wantLT && !strings.Contains(clause, "<") {
			t.Errorf("sort=%s dir=%s: expected < in clause", tt.sort, tt.direction)
		}
	}
}

func TestBotDirectoryOrderClauseAllBranches(t *testing.T) {
	tests := []struct {
		sort string
		want string
	}{
		{"newest", "ba.created_at DESC"},
		{"deferred_desc", "COALESCE(s.deferred_messages, 0) DESC, COALESCE(s.failed_messages, 0) DESC, ba.created_at DESC"},
		{"activity_desc", "COALESCE(s.total_messages, 0) DESC, COALESCE(s.published_messages, 0) DESC, ba.created_at DESC"},
		{"unknown", "COALESCE(s.total_messages, 0) DESC, COALESCE(s.published_messages, 0) DESC, ba.created_at DESC"},
	}
	for _, tt := range tests {
		got := botDirectoryOrderClause(tt.sort)
		if got != tt.want {
			t.Errorf("botDirectoryOrderClause(%q) = %q, want %q", tt.sort, got, tt.want)
		}
	}
}

func TestListRecentPublishedMessagesByDIDEdgeCases(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Empty DID returns nil immediately.
	messages, err := db.ListRecentPublishedMessagesByDID(ctx, "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if messages != nil {
		t.Errorf("expected nil for empty DID, got %v", messages)
	}

	// Whitespace DID returns nil.
	messages, err = db.ListRecentPublishedMessagesByDID(ctx, "   ", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if messages != nil {
		t.Errorf("expected nil for whitespace DID, got %v", messages)
	}

	// Default limit (0 -> 20).
	messages, err = db.ListRecentPublishedMessagesByDID(ctx, "did:plc:test", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(messages))
	}
}

func TestGetRetryCandidatesDefaults(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Add a failed message with no SSB ref.
	if err := db.AddMessage(ctx, Message{
		ATURI:           "at://did:plc:x/app.bsky.feed.post/r1",
		ATCID:           "bafy-r1",
		ATDID:           "did:plc:x",
		Type:            "app.bsky.feed.post",
		MessageState:    MessageStateFailed,
		PublishError:    "err",
		PublishAttempts: 1,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Default limit (0 -> 50) and default maxAttempts (0 -> 8), no DID filter.
	candidates, err := db.GetRetryCandidates(ctx, 0, "", 0)
	if err != nil {
		t.Fatalf("GetRetryCandidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
}

func TestGetDeferredCandidatesDefault(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Default limit (0 -> 50), empty DB.
	candidates, err := db.GetDeferredCandidates(ctx, 0)
	if err != nil {
		t.Fatalf("GetDeferredCandidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("expected 0, got %d", len(candidates))
	}
}

func TestGetPublishFailuresDefault(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Default limit (0 -> 50).
	failures, err := db.GetPublishFailures(ctx, 0)
	if err != nil {
		t.Fatalf("GetPublishFailures: %v", err)
	}
	if len(failures) != 0 {
		t.Fatalf("expected 0, got %d", len(failures))
	}
}

func TestGetLatestDeferredReasonEmpty(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// No deferred messages at all -> ErrNoRows path.
	reason, ok, err := db.GetLatestDeferredReason(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected not found")
	}
	if reason != "" {
		t.Errorf("expected empty reason, got %q", reason)
	}
}

func TestCheckBridgeHealthStaleHeartbeat(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Set status to "live" but with a very old heartbeat.
	if err := db.SetBridgeState(ctx, "bridge_runtime_status", "live"); err != nil {
		t.Fatalf("set status: %v", err)
	}
	staleTime := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	if err := db.SetBridgeState(ctx, "bridge_runtime_last_heartbeat_at", staleTime); err != nil {
		t.Fatalf("set heartbeat: %v", err)
	}

	health, err := db.CheckBridgeHealth(ctx, 60*time.Second)
	if err != nil {
		t.Fatalf("CheckBridgeHealth: %v", err)
	}
	if health.Healthy {
		t.Error("expected unhealthy for stale heartbeat")
	}
	if health.Status != "live" {
		t.Errorf("expected status 'live', got %q", health.Status)
	}
}

func TestCheckBridgeHealthNoMaxStale(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	if err := db.SetBridgeState(ctx, "bridge_runtime_status", "live"); err != nil {
		t.Fatalf("set status: %v", err)
	}
	if err := db.SetBridgeState(ctx, "bridge_runtime_last_heartbeat_at", time.Now().UTC().Add(-10*time.Minute).Format(time.RFC3339)); err != nil {
		t.Fatalf("set heartbeat: %v", err)
	}

	// maxStale=0 means skip heartbeat staleness check.
	health, err := db.CheckBridgeHealth(ctx, 0)
	if err != nil {
		t.Fatalf("CheckBridgeHealth: %v", err)
	}
	if !health.Healthy {
		t.Error("expected healthy when maxStale=0")
	}
}

func TestAddMessageDefaultState(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Add message with empty MessageState -> should default to "pending".
	if err := db.AddMessage(ctx, Message{
		ATURI:     "at://did:plc:x/app.bsky.feed.post/default-state",
		ATCID:     "bafy-ds",
		ATDID:     "did:plc:x",
		Type:      "app.bsky.feed.post",
		RawATJson: `{}`,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	msg, err := db.GetMessage(ctx, "at://did:plc:x/app.bsky.feed.post/default-state")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if msg.MessageState != MessageStatePending {
		t.Errorf("expected state 'pending', got %q", msg.MessageState)
	}
}

func TestGetMessageAllNullableFields(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	seq := int64(42)
	if err := db.AddMessage(ctx, Message{
		ATURI:                "at://did:plc:x/app.bsky.feed.post/full",
		ATCID:                "bafy-full",
		SSBMsgRef:            "%full.sha256",
		ATDID:                "did:plc:x",
		Type:                 "app.bsky.feed.post",
		MessageState:         MessageStateDeleted,
		RawATJson:            `{"text":"full"}`,
		RawSSBJson:           `{"type":"post"}`,
		PublishedAt:          &now,
		PublishError:         "some error",
		PublishAttempts:      3,
		LastPublishAttemptAt: &now,
		DeferReason:          "some reason",
		DeferAttempts:        2,
		LastDeferAttemptAt:   &now,
		DeletedAt:            &now,
		DeletedSeq:           &seq,
		DeletedReason:        "tombstone",
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	msg, err := db.GetMessage(ctx, "at://did:plc:x/app.bsky.feed.post/full")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if msg == nil {
		t.Fatal("expected message, got nil")
	}
	if msg.SSBMsgRef != "%full.sha256" {
		t.Errorf("ssb_msg_ref: got %q", msg.SSBMsgRef)
	}
	if msg.PublishedAt == nil {
		t.Error("expected published_at")
	}
	if msg.LastPublishAttemptAt == nil {
		t.Error("expected last_publish_attempt_at")
	}
	if msg.LastDeferAttemptAt == nil {
		t.Error("expected last_defer_attempt_at")
	}
	if msg.DeletedAt == nil {
		t.Error("expected deleted_at")
	}
	if msg.DeletedSeq == nil || *msg.DeletedSeq != 42 {
		t.Errorf("expected deleted_seq 42, got %v", msg.DeletedSeq)
	}
	if msg.DeletedReason != "tombstone" {
		t.Errorf("expected deleted_reason 'tombstone', got %q", msg.DeletedReason)
	}
}

func TestListMessagesPageNonKeysetSort(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Seed some messages.
	for i := 0; i < 5; i++ {
		uri := "at://did:plc:x/app.bsky.feed.post/nk" + string(rune('a'+i))
		if err := db.AddMessage(ctx, Message{
			ATURI:           uri,
			ATCID:           "cid-" + string(rune('a'+i)),
			ATDID:           "did:plc:x",
			Type:            "app.bsky.feed.post",
			MessageState:    MessageStateFailed,
			PublishError:    "err",
			PublishAttempts: i,
		}); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}

	// Use a non-keyset sort (attempts_desc) with small limit to trigger hasMore.
	page, err := db.ListMessagesPage(ctx, MessageListQuery{
		Sort:  "attempts_desc",
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("ListMessagesPage: %v", err)
	}
	if len(page.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(page.Messages))
	}
	if !page.HasNext {
		t.Error("expected HasNext for non-keyset sort with overflow")
	}
	if page.NextCursor == "" {
		t.Error("expected NextCursor to be set")
	}
}

func TestListMessagesPageInvalidCursor(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Keyset sort with an invalid cursor should treat it as no cursor.
	page, err := db.ListMessagesPage(ctx, MessageListQuery{
		Sort:      "newest",
		Limit:     10,
		Cursor:    "totally-invalid-cursor",
		Direction: "next",
	})
	if err != nil {
		t.Fatalf("ListMessagesPage: %v", err)
	}
	if len(page.Messages) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(page.Messages))
	}
}

func TestListMessagesPageOldestSort(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	for i := 0; i < 4; i++ {
		uri := "at://did:plc:x/app.bsky.feed.post/old" + string(rune('a'+i))
		if err := db.AddMessage(ctx, Message{
			ATURI:        uri,
			ATCID:        "cid-old" + string(rune('a'+i)),
			ATDID:        "did:plc:x",
			Type:         "app.bsky.feed.post",
			MessageState: MessageStatePublished,
		}); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}

	// First page: oldest sort.
	page1, err := db.ListMessagesPage(ctx, MessageListQuery{Sort: "oldest", Limit: 2})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Messages) != 2 {
		t.Fatalf("expected 2, got %d", len(page1.Messages))
	}
	if !page1.HasNext {
		t.Error("expected HasNext")
	}

	// Next page.
	page2, err := db.ListMessagesPage(ctx, MessageListQuery{
		Sort:      "oldest",
		Limit:     2,
		Cursor:    page1.NextCursor,
		Direction: "next",
	})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Messages) != 2 {
		t.Fatalf("expected 2, got %d", len(page2.Messages))
	}
	if !page2.HasPrev {
		t.Error("expected HasPrev")
	}

	// Prev from page2.
	back, err := db.ListMessagesPage(ctx, MessageListQuery{
		Sort:      "oldest",
		Limit:     2,
		Cursor:    page2.PrevCursor,
		Direction: "prev",
	})
	if err != nil {
		t.Fatalf("back: %v", err)
	}
	if len(back.Messages) != 2 {
		t.Fatalf("expected 2, got %d", len(back.Messages))
	}
}

func TestListTopDeferredReasonsDefault(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Default limit (0 -> 5), empty DB.
	reasons, err := db.ListTopDeferredReasons(ctx, 0)
	if err != nil {
		t.Fatalf("ListTopDeferredReasons: %v", err)
	}
	if len(reasons) != 0 {
		t.Fatalf("expected 0, got %d", len(reasons))
	}
}

func TestListTopIssueAccountsDefault(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Default limit (0 -> 5), empty DB.
	issues, err := db.ListTopIssueAccounts(ctx, 0)
	if err != nil {
		t.Fatalf("ListTopIssueAccounts: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected 0, got %d", len(issues))
	}
}

func TestGetRecentMessagesDefault(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Default limit (0 -> 50).
	messages, err := db.GetRecentMessages(ctx, 0)
	if err != nil {
		t.Fatalf("GetRecentMessages: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected 0, got %d", len(messages))
	}
}

func TestGetRecentBlobsDefault(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Default limit (0 -> 50).
	blobs, err := db.GetRecentBlobs(ctx, 0)
	if err != nil {
		t.Fatalf("GetRecentBlobs: %v", err)
	}
	if len(blobs) != 0 {
		t.Fatalf("expected 0, got %d", len(blobs))
	}
}

func TestParseNullableTimeFormats(t *testing.T) {
	tests := []struct {
		input   string
		wantNil bool
	}{
		// Invalid / empty.
		{"", true},
		{"   ", true},
		{"not-a-time", true},

		// RFC3339Nano
		{"2026-01-15T10:30:00.123456789Z", false},
		// RFC3339
		{"2026-01-15T10:30:00Z", false},
		// SQLite datetime with fractional seconds and timezone.
		{"2026-01-15 10:30:00.123456+00:00", false},
		// SQLite datetime with timezone.
		{"2026-01-15 10:30:00+00:00", false},
		// SQLite datetime without timezone.
		{"2026-01-15 10:30:00", false},
		// ISO8601 with T and Z.
		{"2026-01-15T10:30:00Z", false},
	}

	for _, tt := range tests {
		ns := sql.NullString{Valid: tt.input != "", String: tt.input}
		if tt.input == "   " {
			ns.Valid = true
		}
		got := parseNullableTime(ns)
		if tt.wantNil && got != nil {
			t.Errorf("parseNullableTime(%q) = %v, want nil", tt.input, got)
		}
		if !tt.wantNil && got == nil {
			t.Errorf("parseNullableTime(%q) = nil, want non-nil", tt.input)
		}
	}

	// Invalid NullString (Valid=false).
	got := parseNullableTime(sql.NullString{Valid: false})
	if got != nil {
		t.Error("expected nil for invalid NullString")
	}
}

func TestDBOpenInvalidPath(t *testing.T) {
	_, err := Open("/non/existent/path/that/cannot/be/created/db.sqlite")
	if err == nil {
		t.Fatal("expected error for invalid db path")
	}
}

func TestColumnExistsError(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	db.Close()
	_, err = db.columnExists("messages", "any")
	if err == nil {
		t.Fatal("expected error from closed db")
	}
}

func TestScanBridgedAccountStatsError(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	// Test scanBridgedAccountStats directly with failing scanner
	_, err = scanBridgedAccountStats(&failingScanner{})
	if err == nil {
		t.Error("expected scan error")
	}
}

type failingScanner struct{}

func (f *failingScanner) Scan(dest ...interface{}) error {
	return fmt.Errorf("scan failed")
}

func TestCountMessagesError(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	db.Close()
	_, err = db.CountMessages(context.Background())
	if err == nil {
		t.Error("expected error from closed db")
	}
}

func TestGetRecentMessagesError(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	db.Close()
	_, err = db.GetRecentMessages(context.Background(), 10)
	if err == nil {
		t.Error("expected error from closed db")
	}
}

func TestListMessagesError(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	db.Close()
	_, err = db.ListMessages(context.Background(), MessageListQuery{})
	if err == nil {
		t.Error("expected error from closed db")
	}
}

func TestCountPublishedMessagesError(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	db.Close()
	_, err = db.CountPublishedMessages(context.Background())
	if err == nil {
		t.Error("expected error from closed db")
	}
}

func TestCountPublishFailuresError(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	db.Close()
	_, err = db.CountPublishFailures(context.Background())
	if err == nil {
		t.Error("expected error from closed db")
	}
}

func TestCountDeferredMessagesError(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	db.Close()
	_, err = db.CountDeferredMessages(context.Background())
	if err == nil {
		t.Error("expected error from closed db")
	}
}

func TestCountDeletedMessagesError(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	db.Close()
	_, err = db.CountDeletedMessages(context.Background())
	if err == nil {
		t.Error("expected error from closed db")
	}
}

func TestGetPublishFailuresError(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	db.Close()
	_, err = db.GetPublishFailures(context.Background(), 10)
	if err == nil {
		t.Error("expected error from closed db")
	}
}

func TestGetRetryCandidatesError(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	db.Close()
	_, err = db.GetRetryCandidates(context.Background(), 10, "", 1)
	if err == nil {
		t.Error("expected error from closed db")
	}
}

func TestGetDeferredCandidatesError(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	db.Close()
	_, err = db.GetDeferredCandidates(context.Background(), 10)
	if err == nil {
		t.Error("expected error from closed db")
	}
}

func TestGetLatestDeferredReasonError(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	db.Close()
	_, _, err = db.GetLatestDeferredReason(context.Background())
	if err == nil {
		t.Error("expected error from closed db")
	}
}

func TestCountBlobsError(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	db.Close()
	_, err = db.CountBlobs(context.Background())
	if err == nil {
		t.Error("expected error from closed db")
	}
}

func TestGetRecentBlobsError(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	db.Close()
	_, err = db.GetRecentBlobs(context.Background(), 10)
	if err == nil {
		t.Error("expected error from closed db")
	}
}

func TestListMessagesSearchFilter(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	if err := db.AddMessage(ctx, Message{
		ATURI:        "at://did:plc:alice/app.bsky.feed.post/searchable",
		ATCID:        "bafy-search",
		ATDID:        "did:plc:alice",
		Type:         "app.bsky.feed.post",
		MessageState: MessageStatePublished,
		RawATJson:    `{"text":"unique_searchterm"}`,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	messages, err := db.ListMessages(ctx, MessageListQuery{Search: "unique_searchterm", Limit: 10})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	// Search is on at_uri, at_did, ssb_msg_ref, publish_error, defer_reason, deleted_reason - not raw JSON.
	// So searching for the DID should work.
	messages, err = db.ListMessages(ctx, MessageListQuery{Search: "did:plc:alice", Limit: 10})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
}

func TestListMessagesPageWithStateFilter(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	if err := db.AddMessage(ctx, Message{
		ATURI:        "at://did:plc:x/app.bsky.feed.post/sf1",
		ATCID:        "cid-sf1",
		ATDID:        "did:plc:x",
		Type:         "app.bsky.feed.post",
		MessageState: MessageStatePublished,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := db.AddMessage(ctx, Message{
		ATURI:        "at://did:plc:x/app.bsky.feed.post/sf2",
		ATCID:        "cid-sf2",
		ATDID:        "did:plc:x",
		Type:         "app.bsky.feed.post",
		MessageState: MessageStateFailed,
		PublishError: "err",
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	page, err := db.ListMessagesPage(ctx, MessageListQuery{
		Sort:  "newest",
		State: "published",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListMessagesPage: %v", err)
	}
	if len(page.Messages) != 1 {
		t.Fatalf("expected 1 published, got %d", len(page.Messages))
	}
}

func TestListMessagesPageWithTypeFilter(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	if err := db.AddMessage(ctx, Message{
		ATURI:        "at://did:plc:x/app.bsky.feed.post/tf1",
		ATCID:        "cid-tf1",
		ATDID:        "did:plc:x",
		Type:         "app.bsky.feed.post",
		MessageState: MessageStatePublished,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := db.AddMessage(ctx, Message{
		ATURI:        "at://did:plc:x/app.bsky.feed.like/tf2",
		ATCID:        "cid-tf2",
		ATDID:        "did:plc:x",
		Type:         "app.bsky.feed.like",
		MessageState: MessageStatePublished,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	page, err := db.ListMessagesPage(ctx, MessageListQuery{
		Sort:  "newest",
		Type:  "app.bsky.feed.like",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListMessagesPage: %v", err)
	}
	if len(page.Messages) != 1 {
		t.Fatalf("expected 1 like, got %d", len(page.Messages))
	}
}

// TestErrorPathsAfterClose verifies error paths by operating on a closed DB.
func TestErrorPathsAfterClose(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	db.Close()

	ctx := context.Background()

	if _, err := db.CountBridgedAccounts(ctx); err == nil {
		t.Error("expected error from CountBridgedAccounts on closed DB")
	}
	if _, err := db.CountActiveBridgedAccounts(ctx); err == nil {
		t.Error("expected error from CountActiveBridgedAccounts on closed DB")
	}
	if _, err := db.CountMessages(ctx); err == nil {
		t.Error("expected error from CountMessages on closed DB")
	}
	if _, err := db.CountPublishedMessages(ctx); err == nil {
		t.Error("expected error from CountPublishedMessages on closed DB")
	}
	if _, err := db.CountPublishFailures(ctx); err == nil {
		t.Error("expected error from CountPublishFailures on closed DB")
	}
	if _, err := db.CountDeferredMessages(ctx); err == nil {
		t.Error("expected error from CountDeferredMessages on closed DB")
	}
	if _, err := db.CountDeletedMessages(ctx); err == nil {
		t.Error("expected error from CountDeletedMessages on closed DB")
	}
	if _, err := db.CountBlobs(ctx); err == nil {
		t.Error("expected error from CountBlobs on closed DB")
	}
	if err := db.AddBridgedAccount(ctx, BridgedAccount{ATDID: "x", SSBFeedID: "y"}); err == nil {
		t.Error("expected error from AddBridgedAccount on closed DB")
	}
	if _, err := db.GetBridgedAccount(ctx, "x"); err == nil {
		t.Error("expected error from GetBridgedAccount on closed DB")
	}
	if err := db.AddMessage(ctx, Message{ATURI: "at://x", ATCID: "c", ATDID: "d", Type: "t", MessageState: "s"}); err == nil {
		t.Error("expected error from AddMessage on closed DB")
	}
	if _, err := db.GetMessage(ctx, "at://x"); err == nil {
		t.Error("expected error from GetMessage on closed DB")
	}
	if _, err := db.GetRecentMessages(ctx, 10); err == nil {
		t.Error("expected error from GetRecentMessages on closed DB")
	}
	if _, err := db.ListRecentPublishedMessagesByDID(ctx, "did:plc:x", 10); err == nil {
		t.Error("expected error from ListRecentPublishedMessagesByDID on closed DB")
	}
	if _, err := db.ListMessages(ctx, MessageListQuery{Sort: "newest"}); err == nil {
		t.Error("expected error from ListMessages on closed DB")
	}
	if _, err := db.ListMessagesPage(ctx, MessageListQuery{Sort: "newest", Limit: 10}); err == nil {
		t.Error("expected error from ListMessagesPage on closed DB")
	}
	if _, err := db.ListMessagesPage(ctx, MessageListQuery{Sort: "attempts_desc", Limit: 10}); err == nil {
		t.Error("expected error from ListMessagesPage non-keyset on closed DB")
	}
	if err := db.AddBlob(ctx, Blob{ATCID: "x", SSBBlobRef: "y"}); err == nil {
		t.Error("expected error from AddBlob on closed DB")
	}
	if _, err := db.GetBlob(ctx, "x"); err == nil {
		t.Error("expected error from GetBlob on closed DB")
	}
	if _, err := db.GetRecentBlobs(ctx, 10); err == nil {
		t.Error("expected error from GetRecentBlobs on closed DB")
	}
	if err := db.SetBridgeState(ctx, "k", "v"); err == nil {
		t.Error("expected error from SetBridgeState on closed DB")
	}
	if _, _, err := db.GetBridgeState(ctx, "k"); err == nil {
		t.Error("expected error from GetBridgeState on closed DB")
	}
	if _, err := db.CheckBridgeHealth(ctx, time.Minute); err == nil {
		t.Error("expected error from CheckBridgeHealth on closed DB")
	}
	if _, err := db.GetAllBridgeState(ctx); err == nil {
		t.Error("expected error from GetAllBridgeState on closed DB")
	}
	if _, err := db.GetAllBridgedAccounts(ctx); err == nil {
		t.Error("expected error from GetAllBridgedAccounts on closed DB")
	}
	if _, err := db.ListActiveBridgedAccounts(ctx); err == nil {
		t.Error("expected error from ListActiveBridgedAccounts on closed DB")
	}
	if _, err := db.ListActiveBridgedAccountsWithStats(ctx); err == nil {
		t.Error("expected error from ListActiveBridgedAccountsWithStats on closed DB")
	}
	if _, err := db.ListBridgedAccountsWithStats(ctx); err == nil {
		t.Error("expected error from ListBridgedAccountsWithStats on closed DB")
	}
	if _, err := db.ListActiveBridgedAccountsWithStatsSorted(ctx, "", "newest"); err == nil {
		t.Error("expected error from ListActiveBridgedAccountsWithStatsSorted on closed DB")
	}
	if _, err := db.GetActiveBridgedAccountWithStats(ctx, "x"); err == nil {
		t.Error("expected error from GetActiveBridgedAccountWithStats on closed DB")
	}
	if _, err := db.GetPublishFailures(ctx, 10); err == nil {
		t.Error("expected error from GetPublishFailures on closed DB")
	}
	if _, err := db.GetRetryCandidates(ctx, 10, "", 8); err == nil {
		t.Error("expected error from GetRetryCandidates on closed DB")
	}
	if _, err := db.GetDeferredCandidates(ctx, 10); err == nil {
		t.Error("expected error from GetDeferredCandidates on closed DB")
	}
	if _, _, err := db.GetLatestDeferredReason(ctx); err == nil {
		t.Error("expected error from GetLatestDeferredReason on closed DB")
	}
	if _, err := db.ListTopDeferredReasons(ctx, 5); err == nil {
		t.Error("expected error from ListTopDeferredReasons on closed DB")
	}
	if _, err := db.ListTopIssueAccounts(ctx, 5); err == nil {
		t.Error("expected error from ListTopIssueAccounts on closed DB")
	}
	if _, err := db.ListMessageTypes(ctx); err == nil {
		t.Error("expected error from ListMessageTypes on closed DB")
	}
}

func TestListMessagesPageNonKeysetNoOverflow(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Add exactly 2 messages.
	for i := 0; i < 2; i++ {
		uri := "at://did:plc:x/app.bsky.feed.post/nkno" + string(rune('a'+i))
		if err := db.AddMessage(ctx, Message{
			ATURI:           uri,
			ATCID:           "cid-nkno" + string(rune('a'+i)),
			ATDID:           "did:plc:x",
			Type:            "app.bsky.feed.post",
			MessageState:    MessageStateFailed,
			PublishError:    "err",
			PublishAttempts: i,
		}); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}

	// Non-keyset sort, limit=10 (more than available) -> no overflow.
	page, err := db.ListMessagesPage(ctx, MessageListQuery{
		Sort:  "attempts_desc",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListMessagesPage: %v", err)
	}
	if len(page.Messages) != 2 {
		t.Fatalf("expected 2, got %d", len(page.Messages))
	}
	if page.HasNext {
		t.Error("expected no HasNext")
	}
	if page.NextCursor != "" {
		t.Error("expected empty NextCursor")
	}
}
