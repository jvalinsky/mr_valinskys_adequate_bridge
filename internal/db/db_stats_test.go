package db

import (
	"path/filepath"
	"testing"
	"time"
)

func TestListActiveBridgedAccountsWithStats(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := t.Context()

	// Create two active accounts and one inactive.
	if err := database.AddBridgedAccount(ctx, BridgedAccount{ATDID: "did:plc:active1", SSBFeedID: "@a1.ed25519", Active: true}); err != nil {
		t.Fatalf("add active1: %v", err)
	}
	if err := database.AddBridgedAccount(ctx, BridgedAccount{ATDID: "did:plc:active2", SSBFeedID: "@a2.ed25519", Active: true}); err != nil {
		t.Fatalf("add active2: %v", err)
	}
	if err := database.AddBridgedAccount(ctx, BridgedAccount{ATDID: "did:plc:inactive", SSBFeedID: "@in.ed25519", Active: false}); err != nil {
		t.Fatalf("add inactive: %v", err)
	}

	// Add messages for active1.
	now := time.Now().Truncate(time.Second)
	for i, state := range []string{MessageStatePublished, MessageStatePublished, MessageStateFailed, MessageStateDeferred, MessageStatePending} {
		msg := Message{
			ATURI:        "at://did:plc:active1/app.bsky.feed.post/" + string(rune('a'+i)),
			ATCID:        "cid" + string(rune('a'+i)),
			ATDID:        "did:plc:active1",
			Type:         "app.bsky.feed.post",
			MessageState: state,
		}
		if state == MessageStatePublished {
			pubTime := now.Add(-time.Duration(i) * time.Minute)
			msg.PublishedAt = &pubTime
		}
		if err := database.AddMessage(ctx, msg); err != nil {
			t.Fatalf("add message %d: %v", i, err)
		}
	}

	// active2 has no messages.

	accounts, err := database.ListActiveBridgedAccountsWithStats(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(accounts) != 2 {
		t.Fatalf("expected 2 active accounts, got %d", len(accounts))
	}

	// Find active1 in results.
	var found1, found2 bool
	for _, acc := range accounts {
		switch acc.ATDID {
		case "did:plc:active1":
			found1 = true
			if acc.TotalMessages != 5 {
				t.Errorf("active1 total: want 5, got %d", acc.TotalMessages)
			}
			if acc.PublishedMessages != 2 {
				t.Errorf("active1 published: want 2, got %d", acc.PublishedMessages)
			}
			if acc.FailedMessages != 1 {
				t.Errorf("active1 failed: want 1, got %d", acc.FailedMessages)
			}
			if acc.DeferredMessages != 1 {
				t.Errorf("active1 deferred: want 1, got %d", acc.DeferredMessages)
			}
			if acc.LastPublishedAt == nil {
				t.Error("active1 last published at should not be nil")
			}
		case "did:plc:active2":
			found2 = true
			if acc.TotalMessages != 0 {
				t.Errorf("active2 total: want 0, got %d", acc.TotalMessages)
			}
			if acc.LastPublishedAt != nil {
				t.Error("active2 last published at should be nil")
			}

		case "did:plc:inactive":
			t.Fatal("inactive account should not appear in active list")
		}
	}
	if !found1 || !found2 {
		t.Fatalf("missing expected accounts found1=%v found2=%v", found1, found2)
	}
}

func TestGetActiveBridgedAccountWithStats(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := t.Context()

	if err := database.AddBridgedAccount(ctx, BridgedAccount{ATDID: "did:plc:bot1", SSBFeedID: "@b1.ed25519", Active: true}); err != nil {
		t.Fatalf("add bot1: %v", err)
	}
	if err := database.AddBridgedAccount(ctx, BridgedAccount{ATDID: "did:plc:inactive-bot", SSBFeedID: "@ib.ed25519", Active: false}); err != nil {
		t.Fatalf("add inactive: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	pubTime := now
	if err := database.AddMessage(ctx, Message{
		ATURI:        "at://did:plc:bot1/app.bsky.feed.post/m1",
		ATCID:        "cid-m1",
		ATDID:        "did:plc:bot1",
		Type:         "app.bsky.feed.post",
		MessageState: MessageStatePublished,
		PublishedAt:  &pubTime,
	}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	// Fetch existing active account.
	acc, err := database.GetActiveBridgedAccountWithStats(ctx, "did:plc:bot1")
	if err != nil {
		t.Fatalf("get bot1: %v", err)
	}
	if acc == nil {
		t.Fatal("expected bot1 to be returned")
	}
	if acc.TotalMessages != 1 || acc.PublishedMessages != 1 {
		t.Errorf("bot1 stats: total=%d published=%d", acc.TotalMessages, acc.PublishedMessages)
	}
	if acc.LastPublishedAt == nil {
		t.Error("bot1 last published at should not be nil")
	}

	// Fetch inactive account should return nil.
	acc, err = database.GetActiveBridgedAccountWithStats(ctx, "did:plc:inactive-bot")
	if err != nil {
		t.Fatalf("get inactive: %v", err)
	}
	if acc != nil {
		t.Fatal("inactive account should not be returned")
	}

	// Fetch non-existing account should return nil.
	acc, err = database.GetActiveBridgedAccountWithStats(ctx, "did:plc:nonexistent")
	if err != nil {
		t.Fatalf("get nonexistent: %v", err)
	}
	if acc != nil {
		t.Fatal("nonexistent account should not be returned")
	}
}
