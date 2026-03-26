package db

import (
	"context"
	"testing"
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
}
