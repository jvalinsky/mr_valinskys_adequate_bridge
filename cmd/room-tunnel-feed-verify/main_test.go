package main

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/mr_valinskys_adequate_bridge/internal/db"
)

func TestSplitCSV(t *testing.T) {
	got := splitCSV("  at://a ,at://b,at://a, ,at://c  ")
	want := []string{"at://a", "at://b", "at://c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitCSV mismatch: got=%v want=%v", got, want)
	}
}

func TestValidateSnapshot(t *testing.T) {
	snapshot := tunnelSnapshot{
		SourceFeed: "@" + "source.example" + ".ed25519",
		Entries: []bridgeEntry{
			{ATURI: "at://example/post/1", SSBMsgRef: "%msg1.sha256", Type: "post"},
			{ATURI: "at://example/post/2", SSBMsgRef: "%msg2.sha256", Type: "like"},
		},
	}

	if err := validateSnapshot(snapshot, snapshot.SourceFeed, []string{"at://example/post/1"}, 1); err != nil {
		t.Fatalf("validateSnapshot success path failed: %v", err)
	}

	if err := validateSnapshot(snapshot, "@"+"other.example"+".ed25519", nil, 1); err == nil {
		t.Fatalf("validateSnapshot expected source-feed mismatch error")
	}

	if err := validateSnapshot(snapshot, snapshot.SourceFeed, []string{"at://missing"}, 1); err == nil {
		t.Fatalf("validateSnapshot expected missing URI error")
	}

	bad := snapshot
	bad.Entries = []bridgeEntry{{ATURI: "at://example/post/1", SSBMsgRef: ""}}
	if err := validateSnapshot(bad, snapshot.SourceFeed, nil, 1); err == nil {
		t.Fatalf("validateSnapshot expected empty ssb_msg_ref error")
	}
}

func TestLoadPublishedEntries(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "bridge.sqlite")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	now := time.Now().UTC()
	later := now.Add(time.Second)

	insert := func(msg db.Message) {
		t.Helper()
		if err := database.AddMessage(ctx, msg); err != nil {
			t.Fatalf("insert message %s: %v", msg.ATURI, err)
		}
	}

	insert(db.Message{
		ATURI:        "at://did:plc:source/app.bsky.feed.post/1",
		ATCID:        "cid1",
		ATDID:        "did:plc:source",
		Type:         "post",
		MessageState: db.MessageStatePublished,
		SSBMsgRef:    "%published1.sha256",
		PublishedAt:  &now,
	})
	insert(db.Message{
		ATURI:        "at://did:plc:source/app.bsky.feed.post/2",
		ATCID:        "cid2",
		ATDID:        "did:plc:source",
		Type:         "repost",
		MessageState: db.MessageStateFailed,
		SSBMsgRef:    "",
		PublishedAt:  &later,
	})
	insert(db.Message{
		ATURI:        "at://did:plc:other/app.bsky.feed.post/9",
		ATCID:        "cid9",
		ATDID:        "did:plc:other",
		Type:         "post",
		MessageState: db.MessageStatePublished,
		SSBMsgRef:    "%published-other.sha256",
		PublishedAt:  &later,
	})

	entries, err := loadPublishedEntries(ctx, dbPath, "did:plc:source", []string{
		"at://did:plc:source/app.bsky.feed.post/1",
		"at://did:plc:source/app.bsky.feed.post/2",
		"at://did:plc:source/app.bsky.feed.post/missing",
	})
	if err != nil {
		t.Fatalf("loadPublishedEntries failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one published source entry, got %d", len(entries))
	}
	if entries[0].ATURI != "at://did:plc:source/app.bsky.feed.post/1" {
		t.Fatalf("unexpected AT URI: %q", entries[0].ATURI)
	}
	if entries[0].SSBMsgRef != "%published1.sha256" {
		t.Fatalf("unexpected ssb_msg_ref: %q", entries[0].SSBMsgRef)
	}
}
