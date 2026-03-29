package db

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDB(t *testing.T) {
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
	db, err := Open(":memory:")
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
