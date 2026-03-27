package db

import (
	"context"
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
		ATURI:      "at://did:plc:123/app.bsky.feed.post/456",
		ATCID:      "bafy123",
		SSBMsgRef:  "%msg123.sha256",
		ATDID:      "did:plc:123",
		Type:       "app.bsky.feed.post",
		RawATJson:  `{"text":"hello"}`,
		RawSSBJson: `{"type":"post","text":"hello"}`,
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
}
