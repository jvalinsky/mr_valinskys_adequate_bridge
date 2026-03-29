package db

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)


func TestOpenError(t *testing.T) {
	// 1. Invalid path (directory that doesn't exist)
	tmpDir, err := os.MkdirTemp("", "db_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "nonexistent", "bridge.db")
	_, err = Open(dbPath)
	if err == nil {
		t.Error("expected error for invalid path, got nil")
	}

	// 2. Database that fails initSchema
	invalidDB := filepath.Join(tmpDir, "invalid.txt")
	if err := os.WriteFile(invalidDB, []byte("this is not a sqlite database"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err = Open(invalidDB)
	if err == nil {
		t.Error("expected error for non-database file, got nil")
	}
}

func TestInitSchemaErrors(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Force ensureColumn to fail by creating a table with a conflicting name
	// but this is tricky because ensureColumn checks if table exists.
	// Let's try to add a column that already exists but with a different definition?
	// SQLite ALTER TABLE ADD COLUMN is quite limited.
	// Actually, let's test ensureColumn directly for error paths.
}

func TestEnsureColumn(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// 1. Column exists (should be no-op)
	if err := db.ensureColumn("messages", "at_uri", "TEXT"); err != nil {
		t.Errorf("expected no-op for existing column, got error: %v", err)
	}

	// 2. Column doesn't exist (should add it)
	if err := db.ensureColumn("messages", "test_new_col", "TEXT"); err != nil {
		t.Errorf("failed to add new column: %v", err)
	}

	// 3. Error: Invalid table name
	if err := db.ensureColumn("nonexistent_table", "col", "TEXT"); err == nil {
		t.Error("expected error for nonexistent table, got nil")
	}

	// 4. Error: Invalid column definition
	if err := db.ensureColumn("messages", "bad_col", "INVALID_TYPE!!!"); err == nil {
		t.Error("expected error for invalid column definition, got nil")
	}
}

func TestCursorEdgeCases(t *testing.T) {
	// 1. Empty URI -> returns ""
	c1 := messageListCursor{CreatedAt: time.Now(), ATURI: ""}
	enc1 := encodeMessageListCursor(c1)
	if enc1 != "" {
		t.Errorf("expected empty string for empty URI, got %q", enc1)
	}

	// 2. Special characters in URI
	c2 := messageListCursor{CreatedAt: time.Now(), ATURI: "at://did:plc:123/!@#$%^&*()"}
	enc2 := encodeMessageListCursor(c2)
	dec2, ok := decodeMessageListCursor(enc2)
	if !ok || dec2.ATURI != c2.ATURI {
		t.Errorf("failed special char URI cursor test")
	}

	// 3. Zero time -> returns ""
	c3 := messageListCursor{CreatedAt: time.Time{}, ATURI: "uri"}
	enc3 := encodeMessageListCursor(c3)
	if enc3 != "" {
		t.Errorf("expected empty string for zero time, got %q", enc3)
	}

	// 4. Decode invalid base64
	_, ok = decodeMessageListCursor("!!!not-base64!!!")
	if ok {
		t.Error("expected failure for invalid base64")
	}

	// 5. Decode invalid JSON
	encInvalidJSON := base64.RawURLEncoding.EncodeToString([]byte("{invalid-json}"))
	_, ok = decodeMessageListCursor(encInvalidJSON)
	if ok {
		t.Error("expected failure for invalid JSON")
	}

	// 6. Decode invalid time format
	encInvalidTime := base64.RawURLEncoding.EncodeToString([]byte(`{"created_at":"not-a-time","at_uri":"uri"}`))
	_, ok = decodeMessageListCursor(encInvalidTime)
	if ok {
		t.Error("expected failure for invalid time format")
	}

	// 7. Decode empty ATURI in JSON
	encEmptyURI := base64.RawURLEncoding.EncodeToString([]byte(`{"created_at":"2026-01-01T00:00:00Z","at_uri":" "}`))
	_, ok = decodeMessageListCursor(encEmptyURI)
	if ok {
		t.Error("expected failure for empty ATURI in decoded JSON")
	}
}

func TestScanners(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	// Add some data to exercise scanners
	acc := BridgedAccount{ATDID: "did:plc:scanner", SSBFeedID: "@scan.ed25519", Active: true}
	db.AddBridgedAccount(ctx, acc)
	db.AddMessage(ctx, Message{ATURI: "at://scan/1", ATDID: acc.ATDID, Type: "test", MessageState: MessageStatePublished})
	db.AddBlob(ctx, Blob{ATCID: "bafy-scan", SSBBlobRef: "&scan", Size: 100})
	db.SetBridgeState(ctx, "scan-key", "scan-val")

	// Exercise all scanners via their high-level methods
	if _, err := db.ListMessageTypes(ctx); err != nil {
		t.Error(err)
	}
	if _, err := db.ListTopIssueAccounts(ctx, 10); err != nil {
		t.Error(err)
	}
	if _, err := db.GetRecentBlobs(ctx, 10); err != nil {
		t.Error(err)
	}
	if _, err := db.GetAllBridgeState(ctx); err != nil {
		t.Error(err)
	}
}

func TestMoreErrorPaths(t *testing.T) {
	db, _ := Open(":memory:?parseTime=true")
	defer db.Close()

	// 1. ensureColumn error path (invalid syntax)
	// SQLite is very permissive, but this should fail:
	err := db.ensureColumn("messages", "bad_col", "REFERS TO NOTHING")
	if err == nil {
		t.Error("expected error for invalid column definition, got nil")
	}

	// 2. decodeMessageListCursor edge cases
	// Invalid base64
	_, ok := decodeMessageListCursor("not-base64!")
	if ok {
		t.Error("expected failure for invalid base64")
	}

	// 3. columnExists error path (invalid table name)
	_, err = db.columnExists("`", "col")
	if err == nil {
		t.Error("expected error for invalid table name in columnExists")
	}
}

func TestScannerDirectErrors(t *testing.T) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	db.conn.SetMaxOpenConns(1)
	defer db.Close()

	ctx := context.Background()
	// Add one message so we have a row to scan
	if err := db.AddMessage(ctx, Message{ATURI: "at://scan/1", ATDID: "did:plc:1", Type: "test"}); err != nil {
		t.Fatalf("failed to add message: %v", err)
	}

	// 1. scanMessageTypeRow expects 1 column. Give it 2.
	rows1, err := db.conn.Query("SELECT at_uri, at_cid FROM messages LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	if rows1.Next() {
		_, err = scanMessageTypeRow(rows1)
		if err == nil {
			t.Error("expected error for scanMessageTypeRow with 2 columns, got nil")
		}
	}
	rows1.Close()

	// 2. scanDeferredReasonCountRow expects 2 columns. Give it 1.
	rows2, err := db.conn.Query("SELECT at_uri FROM messages LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	if rows2.Next() {
		_, err = scanDeferredReasonCountRow(rows2)
		if err == nil {
			t.Error("expected error for scanDeferredReasonCountRow with 1 column, got nil")
		}
	}
	rows2.Close()

	// 3. scanBridgeStateRow expects 3 columns. Give it 1.
	rows3, err := db.conn.Query("SELECT at_uri FROM messages LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	if rows3.Next() {
		_, err = scanBridgeStateRow(rows3)
		if err == nil {
			t.Error("expected error for scanBridgeStateRow with 1 column, got nil")
		}
	}
	rows3.Close()

	// 4. scanAccountIssueSummaryRow (expects 8 columns)
	rows4, err := db.conn.Query("SELECT at_uri FROM messages LIMIT 1")
	if err == nil && rows4.Next() {
		_, err = scanAccountIssueSummaryRow(rows4)
		if err == nil {
			t.Error("expected error for scanAccountIssueSummaryRow with 1 column, got nil")
		}
	}
	rows4.Close()

	// 5. scanBlobRow (expects 5 columns)
	rows5, err := db.conn.Query("SELECT at_uri FROM messages LIMIT 1")
	if err == nil && rows5.Next() {
		_, err = scanBlobRow(rows5)
		if err == nil {
			t.Error("expected error for scanBlobRow with 1 column, got nil")
		}
	}
	rows5.Close()

	// 6. scanMessagesRows (force scanMessageRow to fail)
	rows6, err := db.conn.Query("SELECT at_uri FROM messages LIMIT 1")
	if err == nil {
		_, err = scanMessagesRows(rows6)
		if err == nil {
			t.Error("expected error for scanMessagesRows with 1 column, got nil")
		}
	}
	rows6.Close()
}

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

func TestDBOpenInitSchemaError(t *testing.T) {
	// Creating a directory where the DB file should be will cause a failure to open/init
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "mydb")
	err := os.Mkdir(dbPath, 0755)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Open(dbPath)
	if err == nil {
		t.Fatal("expected error when opening a directory as a database")
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

func TestOpenInvalidPath(t *testing.T) {
	// A path that is definitely not a valid SQLite database should fail.
	_, err := Open("/dev/null/nonexistent/path/db.sqlite")
	if err == nil {
		t.Fatal("expected error opening invalid path")
	}
}

func TestOpenReadOnlyFS(t *testing.T) {
	// Open on a path where we can connect but initSchema fails because
	// the database is read-only.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "readonly.sqlite")

	// Create a valid empty file that SQLite can open but not write to.
	f, err := os.Create(dbPath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	f.Close()
	// Make it read-only.
	if err := os.Chmod(dbPath, 0o444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dbPath, 0o644) })

	// This should fail at initSchema because it can't create tables.
	_, err = Open(dbPath + "?mode=ro")
	if err == nil {
		t.Fatal("expected error opening read-only database")
	}
}

func TestOpenCorruptFile(t *testing.T) {
	// Write garbage to a file and try to Open it. Ping should fail
	// or initSchema should fail.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "corrupt.sqlite")
	if err := os.WriteFile(dbPath, []byte("this is not a sqlite database"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := Open(dbPath)
	if err == nil {
		t.Fatal("expected error opening corrupt file")
	}
}

func TestInitSchemaReopen(t *testing.T) {
	// Opening a file-backed DB twice exercises initSchema's ensureColumn
	// paths where columns already exist (the "exists" branch).
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "reopen.sqlite")

	db1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	ctx := context.Background()
	// Seed some data so the DB is non-empty.
	if err := db1.AddBridgedAccount(ctx, BridgedAccount{
		ATDID: "did:plc:reopen", SSBFeedID: "@reopen.ed25519", Active: true,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}
	db1.Close()

	// Second open calls initSchema again; all ensureColumn calls should
	// find columns already present and return nil (no-op).
	db2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer db2.Close()

	acc, err := db2.GetBridgedAccount(ctx, "did:plc:reopen")
	if err != nil {
		t.Fatalf("get account after reopen: %v", err)
	}
	if acc == nil {
		t.Fatal("expected account to persist after reopen")
	}
}

func TestEnsureColumnAlreadyExists(t *testing.T) {
	// Directly test ensureColumn when a column already exists.
	database, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	// "at_uri" already exists on messages. Calling ensureColumn should be a no-op.
	if err := database.ensureColumn("messages", "at_uri", "TEXT"); err != nil {
		t.Fatalf("ensureColumn existing: %v", err)
	}
}

func TestEnsureColumnAddsNew(t *testing.T) {
	// Test ensureColumn when a column does NOT exist; it should add it.
	database, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	// Add a brand new column.
	if err := database.ensureColumn("messages", "test_coverage_col", "TEXT"); err != nil {
		t.Fatalf("ensureColumn new: %v", err)
	}

	// Verify the column exists by inserting a row that uses it.
	_, err = database.conn.Exec(`INSERT INTO messages (at_uri, at_cid, at_did, type, test_coverage_col) VALUES ('at://test', 'cid', 'did', 'type', 'val')`)
	if err != nil {
		t.Fatalf("insert with new column: %v", err)
	}
}

func TestEnsureColumnInvalidTable(t *testing.T) {
	// Test ensureColumn with a non-existent table; should not error on PRAGMA
	// but will fail on ALTER TABLE.
	database, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	// PRAGMA table_info on a non-existent table returns empty rows, so
	// columnExists returns false. Then ALTER TABLE on a non-existent table
	// should fail.
	err = database.ensureColumn("nonexistent_table", "col", "TEXT")
	if err == nil {
		t.Fatal("expected error for ensureColumn on nonexistent table")
	}
}

func TestColumnExists(t *testing.T) {
	database, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	// Existing column.
	exists, err := database.columnExists("messages", "at_uri")
	if err != nil {
		t.Fatalf("columnExists: %v", err)
	}
	if !exists {
		t.Error("expected at_uri to exist")
	}

	// Non-existing column.
	exists, err = database.columnExists("messages", "nonexistent_col_xyz")
	if err != nil {
		t.Fatalf("columnExists: %v", err)
	}
	if exists {
		t.Error("expected nonexistent_col_xyz to not exist")
	}

	// Non-existing table (returns empty result set from PRAGMA).
	exists, err = database.columnExists("nonexistent_table", "col")
	if err != nil {
		t.Fatalf("columnExists nonexistent table: %v", err)
	}
	if exists {
		t.Error("expected false for nonexistent table")
	}
}

func TestInitSchemaWithPartialSchema(t *testing.T) {
	// Create a DB with only a partial schema (messages table without
	// migration columns), then open it normally. This tests the ensureColumn
	// path where columns genuinely need to be added.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "partial.sqlite")

	// Open a raw SQLite connection and create a minimal messages table
	// missing the migration columns.
	rawConn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	// Include message_state since schema.sql creates an index on it, but
	// leave out migration-only columns (published_at, publish_error, etc.)
	// so ensureColumn will add them.
	_, err = rawConn.Exec(`
		CREATE TABLE IF NOT EXISTS bridged_accounts (
			at_did TEXT PRIMARY KEY,
			ssb_feed_id TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			active BOOLEAN DEFAULT 1
		);
		CREATE TABLE IF NOT EXISTS messages (
			at_uri TEXT PRIMARY KEY,
			at_cid TEXT NOT NULL,
			ssb_msg_ref TEXT,
			at_did TEXT NOT NULL,
			type TEXT NOT NULL,
			message_state TEXT NOT NULL DEFAULT 'pending',
			raw_at_json TEXT,
			raw_ssb_json TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(at_did) REFERENCES bridged_accounts(at_did)
		);
		CREATE INDEX IF NOT EXISTS idx_messages_at_did ON messages(at_did);
		CREATE INDEX IF NOT EXISTS idx_messages_type ON messages(type);
		CREATE INDEX IF NOT EXISTS idx_messages_state ON messages(message_state);
		CREATE TABLE IF NOT EXISTS blobs (
			at_cid TEXT PRIMARY KEY,
			ssb_blob_ref TEXT NOT NULL,
			size INTEGER,
			mime_type TEXT,
			downloaded_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS bridge_state (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		t.Fatalf("create partial schema: %v", err)
	}
	rawConn.Close()

	// Now open with db.Open which runs initSchema. It should add all the
	// missing migration columns via ensureColumn.
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open with partial schema: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	// Verify we can add a full message with all fields.
	now := time.Now().UTC().Truncate(time.Second)
	seq := int64(5)
	if err := database.AddMessage(ctx, Message{
		ATURI:                "at://did:plc:partial/app.bsky.feed.post/1",
		ATCID:                "bafy-partial",
		ATDID:                "did:plc:partial",
		Type:                 "app.bsky.feed.post",
		MessageState:         MessageStateFailed,
		PublishError:         "fail",
		PublishAttempts:      1,
		LastPublishAttemptAt: &now,
		DeferReason:          "reason",
		DeferAttempts:        1,
		LastDeferAttemptAt:   &now,
		DeletedAt:            &now,
		DeletedSeq:           &seq,
		DeletedReason:        "gone",
	}); err != nil {
		t.Fatalf("add message on migrated schema: %v", err)
	}

	msg, err := database.GetMessage(ctx, "at://did:plc:partial/app.bsky.feed.post/1")
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if msg == nil {
		t.Fatal("expected message after migration")
	}
	if msg.PublishError != "fail" {
		t.Errorf("expected publish_error 'fail', got %q", msg.PublishError)
	}
	if msg.DeletedSeq == nil || *msg.DeletedSeq != 5 {
		t.Errorf("expected deleted_seq 5, got %v", msg.DeletedSeq)
	}
}

func TestCheckBridgeHealthBadHeartbeatFormat(t *testing.T) {
	// When heartbeat is not a valid RFC3339 string, the parse silently fails
	// and healthy stays true (based on status alone).
	database, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.SetBridgeState(ctx, "bridge_runtime_status", "live"); err != nil {
		t.Fatalf("set status: %v", err)
	}
	if err := database.SetBridgeState(ctx, "bridge_runtime_last_heartbeat_at", "not-a-time"); err != nil {
		t.Fatalf("set heartbeat: %v", err)
	}

	health, err := database.CheckBridgeHealth(ctx, 60*time.Second)
	if err != nil {
		t.Fatalf("CheckBridgeHealth: %v", err)
	}
	// Status is "live", heartbeat parse fails -> healthy remains true.
	if !health.Healthy {
		t.Error("expected healthy when heartbeat parse fails")
	}
}

func TestCheckBridgeHealthEmptyHeartbeat(t *testing.T) {
	// Status is "live" but no heartbeat key at all.
	database, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.SetBridgeState(ctx, "bridge_runtime_status", "live"); err != nil {
		t.Fatalf("set status: %v", err)
	}

	health, err := database.CheckBridgeHealth(ctx, 60*time.Second)
	if err != nil {
		t.Fatalf("CheckBridgeHealth: %v", err)
	}
	// Status is "live", no heartbeat -> healthy=true (no staleness to check).
	if !health.Healthy {
		t.Error("expected healthy when heartbeat is absent")
	}
}

func TestCheckBridgeHealthNonLiveStatus(t *testing.T) {
	database, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.SetBridgeState(ctx, "bridge_runtime_status", "stopping"); err != nil {
		t.Fatalf("set status: %v", err)
	}
	if err := database.SetBridgeState(ctx, "bridge_runtime_last_heartbeat_at", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("set heartbeat: %v", err)
	}

	health, err := database.CheckBridgeHealth(ctx, 60*time.Second)
	if err != nil {
		t.Fatalf("CheckBridgeHealth: %v", err)
	}
	if health.Healthy {
		t.Error("expected unhealthy for non-live status")
	}
	if health.Status != "stopping" {
		t.Errorf("expected status 'stopping', got %q", health.Status)
	}
}

func TestMessageKeysetOrderAllBranches(t *testing.T) {
	tests := []struct {
		sort    string
		reverse bool
		want    string
	}{
		{"newest", false, "created_at DESC, at_uri DESC"},
		{"newest", true, "created_at ASC, at_uri ASC"},
		{"oldest", false, "created_at ASC, at_uri ASC"},
		{"oldest", true, "created_at DESC, at_uri DESC"},
	}
	for _, tt := range tests {
		got := messageKeysetOrder(tt.sort, tt.reverse)
		if got != tt.want {
			t.Errorf("messageKeysetOrder(%q, %v) = %q, want %q", tt.sort, tt.reverse, got, tt.want)
		}
	}
}

func TestReverseMessages(t *testing.T) {
	msgs := []Message{
		{ATURI: "a"},
		{ATURI: "b"},
		{ATURI: "c"},
	}
	reverseMessages(msgs)
	if msgs[0].ATURI != "c" || msgs[1].ATURI != "b" || msgs[2].ATURI != "a" {
		t.Errorf("unexpected order after reverse: %v", []string{msgs[0].ATURI, msgs[1].ATURI, msgs[2].ATURI})
	}

	// Empty and single-element slices should not panic.
	reverseMessages(nil)
	reverseMessages([]Message{{ATURI: "x"}})
}

func TestNormalizeBotDirectorySort(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"newest", "newest"},
		{"deferred_desc", "deferred_desc"},
		{"activity_desc", "activity_desc"},
		{"invalid", "activity_desc"},
		{"", "activity_desc"},
		{"  newest  ", "newest"},
	}
	for _, tt := range tests {
		got := normalizeBotDirectorySort(tt.input)
		if got != tt.want {
			t.Errorf("normalizeBotDirectorySort(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestListMessagesPagePrevDirection(t *testing.T) {
	database, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	// Seed 5 messages.
	for i := 0; i < 5; i++ {
		uri := fmt.Sprintf("at://did:plc:x/app.bsky.feed.post/prev%d", i)
		if err := database.AddMessage(ctx, Message{
			ATURI:        uri,
			ATCID:        fmt.Sprintf("cid-prev%d", i),
			ATDID:        "did:plc:x",
			Type:         "app.bsky.feed.post",
			MessageState: MessageStatePublished,
		}); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}

	// First page: newest, limit 2.
	page1, err := database.ListMessagesPage(ctx, MessageListQuery{Sort: "newest", Limit: 2})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Messages) != 2 {
		t.Fatalf("expected 2, got %d", len(page1.Messages))
	}

	// Next page.
	page2, err := database.ListMessagesPage(ctx, MessageListQuery{
		Sort:      "newest",
		Limit:     2,
		Cursor:    page1.NextCursor,
		Direction: "next",
	})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}

	// Go back with "prev" direction.
	prevPage, err := database.ListMessagesPage(ctx, MessageListQuery{
		Sort:      "newest",
		Limit:     2,
		Cursor:    page2.PrevCursor,
		Direction: "prev",
	})
	if err != nil {
		t.Fatalf("prev: %v", err)
	}
	if len(prevPage.Messages) != 2 {
		t.Fatalf("expected 2 on prev, got %d", len(prevPage.Messages))
	}
	// When going "prev", HasNext should be true (we came from a later page).
	if !prevPage.HasNext {
		t.Error("expected HasNext when going prev with cursor")
	}
}

func TestOpenFileBacked(t *testing.T) {
	// Test Open with a file-backed DB to cover the Ping path.
	dbPath := filepath.Join(t.TempDir(), "test.sqlite")
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open file-backed: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := database.SetBridgeState(ctx, "test_key", "test_val"); err != nil {
		t.Fatalf("SetBridgeState: %v", err)
	}
	val, ok, err := database.GetBridgeState(ctx, "test_key")
	if err != nil {
		t.Fatalf("GetBridgeState: %v", err)
	}
	if !ok || val != "test_val" {
		t.Errorf("expected test_val, got %q ok=%v", val, ok)
	}
}

func TestAddMessageWithExplicitCreatedAt(t *testing.T) {
	database, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	// Non-zero CreatedAt triggers the else branch in AddMessage.
	customTime := time.Date(2025, 6, 15, 12, 0, 0, 123456789, time.UTC)
	if err := database.AddMessage(ctx, Message{
		ATURI:        "at://did:plc:x/app.bsky.feed.post/custom-time",
		ATCID:        "bafy-ct",
		ATDID:        "did:plc:x",
		Type:         "app.bsky.feed.post",
		MessageState: MessageStatePublished,
		CreatedAt:    customTime,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	msg, err := database.GetMessage(ctx, "at://did:plc:x/app.bsky.feed.post/custom-time")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if msg == nil {
		t.Fatal("expected message")
	}
	// CreatedAt should be truncated to millisecond and in UTC.
	expected := customTime.Truncate(time.Millisecond).UTC()
	if !msg.CreatedAt.Equal(expected) {
		t.Errorf("expected created_at %v, got %v", expected, msg.CreatedAt)
	}
}

func TestListMessagesPageEmptyResultsNoCursors(t *testing.T) {
	database, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	// Keyset sort, no messages in DB.
	page, err := database.ListMessagesPage(ctx, MessageListQuery{
		Sort:  "newest",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListMessagesPage: %v", err)
	}
	if len(page.Messages) != 0 {
		t.Fatalf("expected 0, got %d", len(page.Messages))
	}
	if page.HasNext || page.HasPrev {
		t.Error("expected no pagination flags on empty result")
	}
	if page.NextCursor != "" || page.PrevCursor != "" {
		t.Error("expected empty cursors on empty result")
	}
}

func TestListTopDeferredReasonsWithData(t *testing.T) {
	database, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	// Add deferred messages with different reasons.
	for i, reason := range []string{"missing_parent", "missing_parent", "rate_limited"} {
		if err := database.AddMessage(ctx, Message{
			ATURI:        fmt.Sprintf("at://did:plc:x/app.bsky.feed.post/defer%d", i),
			ATCID:        fmt.Sprintf("cid-defer%d", i),
			ATDID:        "did:plc:x",
			Type:         "app.bsky.feed.post",
			MessageState: MessageStateDeferred,
			DeferReason:  reason,
		}); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}

	reasons, err := database.ListTopDeferredReasons(ctx, 10)
	if err != nil {
		t.Fatalf("ListTopDeferredReasons: %v", err)
	}
	if len(reasons) != 2 {
		t.Fatalf("expected 2 reasons, got %d", len(reasons))
	}
	// "missing_parent" should be first (count=2).
	if reasons[0].Reason != "missing_parent" || reasons[0].Count != 2 {
		t.Errorf("expected missing_parent:2, got %s:%d", reasons[0].Reason, reasons[0].Count)
	}
}

func TestListTopIssueAccountsWithData(t *testing.T) {
	database, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	if err := database.AddBridgedAccount(ctx, BridgedAccount{
		ATDID: "did:plc:issue1", SSBFeedID: "@issue1.ed25519", Active: true,
	}); err != nil {
		t.Fatalf("add account: %v", err)
	}

	// Add failed and deferred messages.
	if err := database.AddMessage(ctx, Message{
		ATURI:        "at://did:plc:issue1/app.bsky.feed.post/f1",
		ATCID:        "cid-f1",
		ATDID:        "did:plc:issue1",
		Type:         "app.bsky.feed.post",
		MessageState: MessageStateFailed,
		PublishError: "err",
	}); err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if err := database.AddMessage(ctx, Message{
		ATURI:        "at://did:plc:issue1/app.bsky.feed.post/d1",
		ATCID:        "cid-d1",
		ATDID:        "did:plc:issue1",
		Type:         "app.bsky.feed.post",
		MessageState: MessageStateDeferred,
		DeferReason:  "reason",
	}); err != nil {
		t.Fatalf("add deferred: %v", err)
	}

	issues, err := database.ListTopIssueAccounts(ctx, 10)
	if err != nil {
		t.Fatalf("ListTopIssueAccounts: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 account, got %d", len(issues))
	}
	if issues[0].IssueMessages != 2 {
		t.Errorf("expected 2 issue messages, got %d", issues[0].IssueMessages)
	}
	if issues[0].FailedMessages != 1 {
		t.Errorf("expected 1 failed, got %d", issues[0].FailedMessages)
	}
}

func TestScanMessageRowMinimalNulls(t *testing.T) {
	// Exercise scanMessageRow via GetRecentMessages with messages that have
	// all nullable fields as NULL.
	database, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	// Add a minimal message with only required fields.
	if err := database.AddMessage(ctx, Message{
		ATURI:        "at://did:plc:x/app.bsky.feed.post/minimal",
		ATCID:        "bafy-min",
		ATDID:        "did:plc:x",
		Type:         "app.bsky.feed.post",
		MessageState: MessageStatePending,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	messages, err := database.GetRecentMessages(ctx, 10)
	if err != nil {
		t.Fatalf("GetRecentMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	msg := messages[0]
	if msg.SSBMsgRef != "" {
		t.Errorf("expected empty ssb_msg_ref, got %q", msg.SSBMsgRef)
	}
	if msg.PublishedAt != nil {
		t.Error("expected nil published_at")
	}
	if msg.LastPublishAttemptAt != nil {
		t.Error("expected nil last_publish_attempt_at")
	}
	if msg.LastDeferAttemptAt != nil {
		t.Error("expected nil last_defer_attempt_at")
	}
	if msg.DeletedAt != nil {
		t.Error("expected nil deleted_at")
	}
	if msg.DeletedSeq != nil {
		t.Error("expected nil deleted_seq")
	}
}

func TestScanMessageRowAllFieldsPopulated(t *testing.T) {
	// Exercise scanMessageRow through GetRecentMessages with all nullable
	// fields populated.
	database, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	seq := int64(42)
	if err := database.AddMessage(ctx, Message{
		ATURI:                "at://did:plc:x/app.bsky.feed.post/allfields",
		ATCID:                "bafy-all",
		SSBMsgRef:            "%all.sha256",
		ATDID:                "did:plc:x",
		Type:                 "app.bsky.feed.post",
		MessageState:         MessageStateDeleted,
		RawATJson:            `{"text":"all"}`,
		RawSSBJson:           `{"type":"post"}`,
		PublishedAt:          &now,
		PublishError:         "error",
		PublishAttempts:      2,
		LastPublishAttemptAt: &now,
		DeferReason:          "reason",
		DeferAttempts:        1,
		LastDeferAttemptAt:   &now,
		DeletedAt:            &now,
		DeletedSeq:           &seq,
		DeletedReason:        "gone",
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	messages, err := database.GetRecentMessages(ctx, 10)
	if err != nil {
		t.Fatalf("GetRecentMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1, got %d", len(messages))
	}
	msg := messages[0]
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
}

func TestGetLatestDeferredReasonValidButEmpty(t *testing.T) {
	// Test the path where defer_reason is Valid but empty after trim.
	database, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	// Add a deferred message with a whitespace-only defer reason.
	// Since AddMessage stores it as-is, we need to use raw SQL.
	_, err = database.conn.ExecContext(ctx,
		`INSERT INTO messages (at_uri, at_cid, at_did, type, message_state, defer_reason)
		 VALUES ('at://empty-reason', 'cid', 'did', 'type', 'deferred', '   ')`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// The query filters out empty/whitespace reasons, so this returns not found.
	reason, ok, err := database.GetLatestDeferredReason(ctx)
	if err != nil {
		t.Fatalf("GetLatestDeferredReason: %v", err)
	}
	if ok {
		t.Errorf("expected not found for whitespace reason, got %q", reason)
	}
}

func TestListActiveBridgedAccountsWithStatsSortedSearchFilter(t *testing.T) {
	database, err := Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	if err := database.AddBridgedAccount(ctx, BridgedAccount{
		ATDID: "did:plc:sorted1", SSBFeedID: "@s1.ed25519", Active: true,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := database.AddBridgedAccount(ctx, BridgedAccount{
		ATDID: "did:plc:sorted2", SSBFeedID: "@s2.ed25519", Active: true,
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Test with search filter.
	results, err := database.ListActiveBridgedAccountsWithStatsSorted(ctx, "sorted1", "newest")
	if err != nil {
		t.Fatalf("sorted with search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}

	// Test without search.
	results, err = database.ListActiveBridgedAccountsWithStatsSorted(ctx, "", "activity_desc")
	if err != nil {
		t.Fatalf("sorted without search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
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
