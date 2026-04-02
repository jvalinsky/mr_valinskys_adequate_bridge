package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/mapper"
)

func TestAccountCommands(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.sqlite")
	ctx := context.Background()
	botSeed := "test-seed"

	// Test Account Add
	did := "did:plc:alice"
	if err := runAccountAdd(ctx, dbPath, botSeed, did); err != nil {
		t.Fatalf("runAccountAdd failed: %v", err)
	}

	// Test Account List
	if err := runAccountList(ctx, dbPath); err != nil {
		t.Fatalf("runAccountList failed: %v", err)
	}

	// Test Account Remove
	if err := runAccountRemove(ctx, dbPath, did); err != nil {
		t.Fatalf("runAccountRemove failed: %v", err)
	}

	// Verify deactivation
	database, _ := db.Open(dbPath)
	defer database.Close()
	acc, _ := database.GetBridgedAccount(ctx, did)
	if acc.Active {
		t.Error("expected account to be inactive")
	}
}

func TestStatsCommand(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test-stats.sqlite")
	ctx := context.Background()

	database, _ := db.Open(dbPath)
	eventCursor, err := database.AppendATProtoEvent(ctx, db.ATProtoRecordEvent{
		DID:        "did:plc:alice",
		Collection: mapper.RecordTypePost,
		RKey:       "post1",
		ATURI:      "at://did:plc:alice/app.bsky.feed.post/post1",
		ATCID:      "bafyrecord",
		Action:     "upsert",
		Rev:        "rev1",
		RecordJSON: `{"$type":"app.bsky.feed.post","text":"hello"}`,
	})
	if err != nil {
		t.Fatalf("append atproto event: %v", err)
	}
	if err := database.SetBridgeState(ctx, "atproto_event_cursor", "41"); err != nil {
		t.Fatalf("set replay cursor: %v", err)
	}
	if err := database.SetBridgeState(ctx, "firehose_seq", "17"); err != nil {
		t.Fatalf("set legacy cursor: %v", err)
	}
	if err := database.UpsertATProtoSource(ctx, db.ATProtoSource{
		SourceKey: "default-relay",
		RelayURL:  "wss://relay.example.test",
		LastSeq:   55,
	}); err != nil {
		t.Fatalf("upsert atproto source: %v", err)
	}
	database.AddBridgedAccount(ctx, db.BridgedAccount{ATDID: "did:plc:alice", Active: true})
	database.AddMessage(ctx, db.Message{
		ATURI:        "at://alice/post/1",
		ATCID:        "c1",
		ATDID:        "did:plc:alice",
		Type:         mapper.RecordTypePost,
		MessageState: db.MessageStatePublished,
	})
	database.Close()

	output := captureStdout(t, func() {
		if err := runStats(ctx, dbPath); err != nil {
			t.Fatalf("runStats failed: %v", err)
		}
	})
	if !strings.Contains(output, "Bridge replay cursor: 41") {
		t.Fatalf("stats output missing bridge replay cursor: %s", output)
	}
	if !strings.Contains(output, "Relay source cursor: 55") {
		t.Fatalf("stats output missing relay source cursor: %s", output)
	}
	if !strings.Contains(output, "ATProto event-log head: "+strconv.FormatInt(eventCursor, 10)) {
		t.Fatalf("stats output missing event-log head cursor: %s", output)
	}
	if !strings.Contains(output, "Legacy firehose cursor: 17") {
		t.Fatalf("stats output missing legacy firehose cursor: %s", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	os.Stdout = oldStdout

	output := <-done
	if err := r.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}
	return output
}
