package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/atindex"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/backfill"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/mapper"
)

type fakeBridgeLifecycle struct {
	startErr   error
	stopErr    error
	startCalls int
	stopCalls  int
}

func (f *fakeBridgeLifecycle) Init(_ context.Context) error {
	return nil
}

func (f *fakeBridgeLifecycle) Start(_ context.Context) error {
	f.startCalls++
	return f.startErr
}

func (f *fakeBridgeLifecycle) Stop() error {
	f.stopCalls++
	return f.stopErr
}

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

func TestWaitForIndexedRepoStateNilIndexer(t *testing.T) {
	_, err := waitForIndexedRepoState(context.Background(), nil, "did:example:test", time.Second)
	if err == nil {
		t.Error("expected error for nil indexer")
	}
}

func TestWaitForIndexedRepoStateTimeout(t *testing.T) {
	database := setupTestDB(t)
	logger := log.New(io.Discard, "", 0)
	indexer := atindex.New(database, backfill.DIDPDSResolver{}, backfill.XRPCRepoFetcher{}, "", logger)

	ctx := context.Background()
	indexer.Start(ctx)

	_, err := waitForIndexedRepoState(ctx, indexer, "did:example:nonexistent", 100*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestWaitForReplayCursorNilDatabase(t *testing.T) {
	err := waitForReplayCursor(context.Background(), nil, 100, time.Second)
	if err == nil {
		t.Error("expected error for nil database")
	}
}

func TestWaitForReplayCursorZeroTarget(t *testing.T) {
	database := setupTestDB(t)
	err := waitForReplayCursor(context.Background(), database, 0, time.Second)
	if err != nil {
		t.Errorf("expected no error for zero target, got %v", err)
	}
}

func TestWaitForReplayCursorNegativeTarget(t *testing.T) {
	database := setupTestDB(t)
	err := waitForReplayCursor(context.Background(), database, -1, time.Second)
	if err != nil {
		t.Errorf("expected no error for negative target, got %v", err)
	}
}

func TestWaitForReplayCursorAlreadyMet(t *testing.T) {
	database := setupTestDB(t)

	ctx := context.Background()
	err := database.SetBridgeState(ctx, "atproto_event_cursor", "100")
	if err != nil {
		t.Fatalf("failed to set bridge state: %v", err)
	}

	err = waitForReplayCursor(ctx, database, 50, time.Second)
	if err != nil {
		t.Errorf("expected no error when cursor already met, got %v", err)
	}
}

func TestStartBridgeAppLifecycleStopsOnStartError(t *testing.T) {
	t.Parallel()

	startErr := errors.New("start failed")
	fake := &fakeBridgeLifecycle{startErr: startErr}

	err := startBridgeAppLifecycle(context.Background(), fake)
	if !errors.Is(err, startErr) {
		t.Fatalf("expected start error, got %v", err)
	}
	if fake.startCalls != 1 {
		t.Fatalf("expected start to be called once, got %d", fake.startCalls)
	}
	if fake.stopCalls != 1 {
		t.Fatalf("expected stop to be called once, got %d", fake.stopCalls)
	}
}

func TestStartBridgeAppLifecycleJoinsStartAndStopErrors(t *testing.T) {
	t.Parallel()

	startErr := errors.New("start failed")
	stopErr := errors.New("stop failed")
	fake := &fakeBridgeLifecycle{startErr: startErr, stopErr: stopErr}

	err := startBridgeAppLifecycle(context.Background(), fake)
	if !errors.Is(err, startErr) {
		t.Fatalf("expected joined error to include start err, got %v", err)
	}
	if !errors.Is(err, stopErr) {
		t.Fatalf("expected joined error to include stop err, got %v", err)
	}
	if fake.stopCalls != 1 {
		t.Fatalf("expected stop to be called once, got %d", fake.stopCalls)
	}
}

func TestStartBridgeAppLifecycleDoesNotDoubleStop(t *testing.T) {
	t.Parallel()

	fake := &fakeBridgeLifecycle{}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := startBridgeAppLifecycle(ctx, fake)
	if err != nil {
		t.Fatalf("unexpected lifecycle error: %v", err)
	}
	if fake.startCalls != 1 {
		t.Fatalf("expected start to be called once, got %d", fake.startCalls)
	}
	if fake.stopCalls != 1 {
		t.Fatalf("expected stop to be called once, got %d", fake.stopCalls)
	}
}

func TestCompositeBlobStoreGetWithNilPrimary(t *testing.T) {
	store := &compositeBlobStore{
		primary: nil,
		fsPath:  "",
	}

	_, err := store.Get([]byte("test"))
	if err == nil {
		t.Error("expected error when both primary and fsPath are empty")
	}
}

func TestCompositeBlobStoreGetWithEmptyFSPath(t *testing.T) {
	store := &compositeBlobStore{
		primary: nil,
		fsPath:  "",
	}

	_, err := store.Get([]byte{0x01, 0x02})
	if err == nil {
		t.Error("expected error with empty fsPath and nil primary")
	}
}

func TestSetBridgeStateBestEffortNilDB(t *testing.T) {
	setBridgeStateBestEffort(context.Background(), nil, "test_key", "test_value", log.New(io.Discard, "", 0))
}

func TestSetBridgeStateBestEffortEmptyKey(t *testing.T) {
	database := setupTestDB(t)
	setBridgeStateBestEffort(context.Background(), database, "", "test_value", log.New(io.Discard, "", 0))
}

func TestRunRuntimeHeartbeatSchedulerNilDB(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runRuntimeHeartbeatScheduler(ctx, nil, log.New(io.Discard, "", 0), time.Second)
}

func TestRunRetrySchedulerNilProcessor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runRetryScheduler(ctx, nil, log.New(io.Discard, "", 0))
}

func TestRunDeferredResolverSchedulerNilProcessor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDeferredResolverScheduler(ctx, nil, log.New(io.Discard, "", 0))
}

func TestRunDeferredExpirySchedulerNilProcessor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runDeferredExpiryScheduler(ctx, nil, log.New(io.Discard, "", 0))
}

func TestRunATProtoTrackSchedulerNilDB(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runATProtoTrackScheduler(ctx, nil, nil, log.New(io.Discard, "", 0))
}

func TestRunATProtoTrackSchedulerNilIndexer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	database := setupTestDB(t)

	runATProtoTrackScheduler(ctx, database, nil, log.New(io.Discard, "", 0))
}

func TestTrackActiveRepos(t *testing.T) {
	database := setupTestDB(t)
	logger := log.New(io.Discard, "", 0)

	ctx := context.Background()
	trackActiveRepos(ctx, database, nil, logger)
}

func TestShutdownLogRuntimeNil(t *testing.T) {
	shutdownLogRuntime(nil)
}

func TestParseHMACKeyInvalidLength(t *testing.T) {
	_, err := parseHMACKey("tooshort")
	if err == nil {
		t.Error("expected error for invalid length key")
	}
}

func TestParseHMACKeyValidBase64(t *testing.T) {
	key, err := parseHMACKey("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatalf("expected no error for valid base64 key, got %v", err)
	}
	if key == nil {
		t.Error("expected non-nil key for valid base64")
	}
}

func TestParseHMACKeyValidHex(t *testing.T) {
	key, err := parseHMACKey("0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("expected no error for valid hex key, got %v", err)
	}
	if key == nil {
		t.Error("expected non-nil key for valid hex")
	}
}

func TestFallbackValueEmpty(t *testing.T) {
	result := fallbackValue("", "default")
	if result != "default" {
		t.Errorf("expected 'default', got %q", result)
	}
}

func TestFallbackValueWhitespace(t *testing.T) {
	result := fallbackValue("   ", "default")
	if result != "default" {
		t.Errorf("expected 'default', got %q", result)
	}
}

func TestFallbackValueNonEmpty(t *testing.T) {
	result := fallbackValue("value", "default")
	if result != "value" {
		t.Errorf("expected 'value', got %q", result)
	}
}
