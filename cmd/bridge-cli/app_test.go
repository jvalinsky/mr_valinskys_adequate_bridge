package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestAppDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "bridge.sqlite")
}

func TestNewBridgeApp(t *testing.T) {
	dbPath := setupTestAppDB(t)
	cfg := AppConfig{
		DBPath:         dbPath,
		RepoPath:       t.TempDir(),
		BotSeed:        "test-seed-for-bridge-app",
		XRPCReadHost:   "https://bsky.social",
		PLCURL:         "https://plc.directory",
		RelayURL:       "wss://bsky.network",
		MCPListenAddr:  "",
		RoomEnable:     false,
		FirehoseEnable: false,
	}

	logger := log.New(os.Stderr, "test: ", log.LstdFlags)
	app := NewBridgeApp(cfg, logger)

	if app == nil {
		t.Fatal("NewBridgeApp returned nil")
	}
	if app.cfg.DBPath != cfg.DBPath {
		t.Errorf("expected DBPath %s, got %s", cfg.DBPath, app.cfg.DBPath)
	}
	if app.logger != logger {
		t.Error("logger not set correctly")
	}
}

func TestBridgeAppGetters(t *testing.T) {
	cfg := AppConfig{
		DBPath:         setupTestAppDB(t),
		RepoPath:       t.TempDir(),
		BotSeed:        "test-seed-for-bridge-app",
		XRPCReadHost:   "https://bsky.social",
		PLCURL:         "https://plc.directory",
		RelayURL:       "wss://bsky.network",
		MCPListenAddr:  "",
		RoomEnable:     false,
		FirehoseEnable: false,
	}

	logger := log.New(os.Stderr, "test: ", log.LstdFlags)
	app := NewBridgeApp(cfg, logger)

	// All getters should return nil before Init
	if app.DB() != nil {
		t.Error("DB() should return nil before Init")
	}
	if app.Processor() != nil {
		t.Error("Processor() should return nil before Init")
	}
	if app.Indexer() != nil {
		t.Error("Indexer() should return nil before Init")
	}
	if app.SSB() != nil {
		t.Error("SSB() should return nil before Init")
	}
	if app.Room() != nil {
		t.Error("Room() should return nil before Init")
	}
	if app.MCPServer() != nil {
		t.Error("MCPServer() should return nil before Init")
	}
}

func TestBridgeAppInitBasic(t *testing.T) {
	cfg := AppConfig{
		DBPath:         setupTestAppDB(t),
		RepoPath:       t.TempDir(),
		BotSeed:        "test-seed-for-bridge-app",
		XRPCReadHost:   "https://bsky.social",
		PLCURL:         "https://plc.directory",
		RelayURL:       "wss://bsky.network",
		MCPListenAddr:  "",
		RoomEnable:     false,
		FirehoseEnable: false,
	}

	logger := log.New(os.Stderr, "test: ", log.LstdFlags)
	app := NewBridgeApp(cfg, logger)

	ctx := context.Background()
	err := app.Init(ctx)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Verify components are initialized
	if app.DB() == nil {
		t.Error("DB() should not be nil after Init")
	}
	if app.Processor() == nil {
		t.Error("Processor() should not be nil after Init")
	}
	if app.Indexer() == nil {
		t.Error("Indexer() should not be nil after Init")
	}
	if app.SSB() == nil {
		t.Error("SSB() should not be nil after Init")
	}
	if app.MCPServer() == nil {
		t.Error("MCPServer() should not be nil after Init")
	}
	if app.Room() != nil {
		t.Error("Room() should be nil when RoomEnable is false")
	}

	// Clean up
	err = app.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestBridgeAppInitWithFirehose(t *testing.T) {
	cfg := AppConfig{
		DBPath:              ":memory:",
		RepoPath:            t.TempDir(),
		BotSeed:             "test-seed-for-bridge-app",
		XRPCReadHost:        "https://bsky.social",
		PLCURL:              "https://plc.directory",
		RelayURL:            "wss://bsky.network",
		MCPListenAddr:       "",
		RoomEnable:          false,
		FirehoseEnable:      true,
		MaxMsgsPerDIDPerMin: 60,
	}

	logger := log.New(os.Stderr, "test: ", log.LstdFlags)
	app := NewBridgeApp(cfg, logger)

	ctx := context.Background()
	err := app.Init(ctx)
	if err != nil {
		t.Fatalf("Init with firehose failed: %v", err)
	}

	// Firehose should be initialized but we can't test it directly
	// Just verify the app initialized successfully
	if app.DB() == nil {
		t.Error("DB should be initialized")
	}

	err = app.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestBridgeAppStartStopLifecycle(t *testing.T) {
	cfg := AppConfig{
		DBPath:              ":memory:",
		RepoPath:            t.TempDir(),
		BotSeed:             "test-seed-for-bridge-app",
		XRPCReadHost:        "https://bsky.social",
		PLCURL:              "https://plc.directory",
		RelayURL:            "wss://bsky.network",
		MCPListenAddr:       "",
		RoomEnable:          false,
		FirehoseEnable:      false,
		MaxMsgsPerDIDPerMin: 60,
	}

	logger := log.New(os.Stderr, "test: ", log.LstdFlags)
	app := NewBridgeApp(cfg, logger)

	ctx := context.Background()
	err := app.Init(ctx)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Start with a cancellable context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	err = app.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Give some time for goroutines to start
	time.Sleep(100 * time.Millisecond)

	// Stop should complete without error
	err = app.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestBridgeAppStartStopWithTimeout(t *testing.T) {
	cfg := AppConfig{
		DBPath:              setupTestAppDB(t),
		RepoPath:            t.TempDir(),
		BotSeed:             "test-seed-for-bridge-app",
		XRPCReadHost:        "https://bsky.social",
		PLCURL:              "https://plc.directory",
		RelayURL:            "wss://bsky.network",
		MCPListenAddr:       "",
		RoomEnable:          false,
		FirehoseEnable:      false,
		MaxMsgsPerDIDPerMin: 60,
	}

	logger := log.New(os.Stderr, "test: ", log.LstdFlags)
	app := NewBridgeApp(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := app.Init(ctx)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// StartIndexerPipeline first to set up necessary state
	err = app.StartIndexerPipeline(ctx)
	if err != nil {
		t.Fatalf("StartIndexerPipeline failed: %v", err)
	}

	err = app.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	err = app.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestReadATProtoEventCursor(t *testing.T) {
	ctx := context.Background()

	// Test with nil database
	cursor, err := readATProtoEventCursor(ctx, nil)
	if err != nil {
		t.Errorf("expected no error with nil db, got %v", err)
	}
	if cursor != 0 {
		t.Errorf("expected cursor 0 with nil db, got %d", cursor)
	}

	// Test with empty database (no cursor set)
	// We need a real DB for this test
	// This would require setting up a test database
	// For now, we'll skip this part as it requires db package integration
}

func TestBridgeAppStartIndexerPipeline(t *testing.T) {
	cfg := AppConfig{
		DBPath:              setupTestAppDB(t),
		RepoPath:            t.TempDir(),
		BotSeed:             "test-seed-for-bridge-app",
		XRPCReadHost:        "https://bsky.social",
		PLCURL:              "https://plc.directory",
		RelayURL:            "wss://bsky.network",
		MCPListenAddr:       "",
		RoomEnable:          false,
		FirehoseEnable:      false,
		MaxMsgsPerDIDPerMin: 60,
	}

	logger := log.New(os.Stderr, "test: ", log.LstdFlags)
	app := NewBridgeApp(cfg, logger)

	ctx := context.Background()
	err := app.Init(ctx)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// StartIndexerPipeline should succeed
	err = app.StartIndexerPipeline(ctx)
	if err != nil {
		t.Fatalf("StartIndexerPipeline failed: %v", err)
	}

	err = app.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestBridgeAppInitWithInsecureFlag(t *testing.T) {
	cfg := AppConfig{
		DBPath:          ":memory:",
		RepoPath:        t.TempDir(),
		BotSeed:         "test-seed-for-bridge-app",
		XRPCReadHost:    "https://bsky.social",
		PLCURL:          "https://plc.directory",
		RelayURL:        "wss://bsky.network",
		MCPListenAddr:   "",
		RoomEnable:      false,
		FirehoseEnable:  false,
		AtprotoInsecure: true,
	}

	logger := log.New(os.Stderr, "test: ", log.LstdFlags)
	app := NewBridgeApp(cfg, logger)

	ctx := context.Background()
	err := app.Init(ctx)
	if err != nil {
		t.Fatalf("Init with insecure flag failed: %v", err)
	}

	err = app.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}
