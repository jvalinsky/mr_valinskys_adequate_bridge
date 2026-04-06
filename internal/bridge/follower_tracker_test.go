package bridge

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/xrpc"
)

func makeFeedRef(t *testing.T, seed byte) refs.FeedRef {
	t.Helper()
	id := make([]byte, 32)
	for i := range id {
		id[i] = seed
	}
	ref, err := refs.NewFeedRef(id, refs.RefAlgoFeedSSB1)
	if err != nil {
		t.Fatalf("NewFeedRef: %v", err)
	}
	return *ref
}

func TestNewFollowerTrackerDefaults(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ft := NewFollowerTracker(FollowerTrackerConfig{
		DB: database,
	})

	if ft.debounceDur != 5*time.Second {
		t.Errorf("expected debounce 5s, got %v", ft.debounceDur)
	}
	if ft.rateLimitDur != 60*time.Second {
		t.Errorf("expected rate limit 60s, got %v", ft.rateLimitDur)
	}
	if ft.maxPerWindow != 10 {
		t.Errorf("expected max per window 10, got %d", ft.maxPerWindow)
	}
}

func TestAnnounceDoesNotBlockOnFullBuffer(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ft := NewFollowerTracker(FollowerTrackerConfig{
		DB: database,
	})

	feed := makeFeedRef(t, 0x01)
	for i := 0; i < 100; i++ {
		ft.Announce(feed)
	}

	done := make(chan struct{})
	go func() {
		ft.Announce(feed)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Announce blocked on full buffer")
	}
}

func TestFollowerTrackerSkipAlreadySynced(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	feed := makeFeedRef(t, 0x02)
	feedStr := feed.String()

	if err := database.AddFollowerSync(ctx, "did:plc:bot", feedStr); err != nil {
		t.Fatalf("AddFollowerSync: %v", err)
	}

	ft := NewFollowerTracker(FollowerTrackerConfig{
		DB:         database,
		BotDID:     "did:plc:bot",
		BotSSBFeed: makeFeedRef(t, 0xFF).String(),
	})
	ft.debounceDur = 50 * time.Millisecond

	startCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := log.New(io.Discard, "", 0)
	ft.Start(startCtx, logger)

	ft.Announce(feed)

	time.Sleep(200 * time.Millisecond)
	ft.Stop()

	has, err := database.HasFollowerSync(ctx, "did:plc:bot", feedStr)
	if err != nil {
		t.Fatalf("HasFollowerSync: %v", err)
	}
	if !has {
		t.Error("expected existing sync to still be there")
	}
}

func TestFollowerTrackerSkipsSelf(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	botFeed := makeFeedRef(t, 0x03)
	botFeedStr := botFeed.String()

	ft := NewFollowerTracker(FollowerTrackerConfig{
		DB:         database,
		BotDID:     "did:plc:bot",
		BotSSBFeed: botFeedStr,
	})
	ft.debounceDur = 50 * time.Millisecond

	startCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := log.New(io.Discard, "", 0)
	ft.Start(startCtx, logger)

	ft.Announce(botFeed)

	time.Sleep(200 * time.Millisecond)
	ft.Stop()

	ctx := context.Background()
	has, err := database.HasFollowerSync(ctx, "did:plc:bot", botFeedStr)
	if err != nil {
		t.Fatalf("HasFollowerSync: %v", err)
	}
	if has {
		t.Error("expected no self-sync")
	}
}

func TestFollowerTrackerRateLimiting(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	var createCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/xrpc/com.atproto.identity.resolveHandle" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"did": "did:plc:resolved"})
			return
		}
		if r.URL.Path == "/xrpc/com.atproto.repo.createRecord" {
			createCount++
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"uri": "at://did:plc:bot/app.bsky.graph.follow/1"})
	}))
	defer server.Close()

	xrpcClient := &xrpc.Client{Host: server.URL, Client: server.Client()}
	botFeed := makeFeedRef(t, 0xFF).String()

	ft := NewFollowerTracker(FollowerTrackerConfig{
		DB:            database,
		XRPCClient:    xrpcClient,
		PDSURL:        server.URL,
		BotDID:        "did:plc:bot",
		BotSSBFeed:    botFeed,
		MaxFollowsPer: 2,
		RateLimitDur:  10 * time.Second,
	})
	ft.debounceDur = 50 * time.Millisecond

	startCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := log.New(io.Discard, "", 0)
	ft.Start(startCtx, logger)

	for i := 0; i < 4; i++ {
		feed := makeFeedRef(t, byte(0x10+i))
		ft.Announce(feed)
	}

	time.Sleep(300 * time.Millisecond)
	ft.Stop()

	if createCount > 2 {
		t.Errorf("expected at most 2 createRecord calls, got %d", createCount)
	}
}

func TestFollowerTrackerDeduplicatesWithinDebounceWindow(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	var createCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/xrpc/com.atproto.identity.resolveHandle" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"did": "did:plc:resolved"})
			return
		}
		if r.URL.Path == "/xrpc/com.atproto.repo.createRecord" {
			createCount++
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"uri": "at://did:plc:bot/app.bsky.graph.follow/1"})
	}))
	defer server.Close()

	xrpcClient := &xrpc.Client{Host: server.URL, Client: server.Client()}
	botFeed := makeFeedRef(t, 0xFF).String()

	ft := NewFollowerTracker(FollowerTrackerConfig{
		DB:            database,
		XRPCClient:    xrpcClient,
		PDSURL:        server.URL,
		BotDID:        "did:plc:bot",
		BotSSBFeed:    botFeed,
		MaxFollowsPer: 10,
		RateLimitDur:  10 * time.Second,
	})
	ft.debounceDur = 100 * time.Millisecond

	startCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := log.New(io.Discard, "", 0)
	ft.Start(startCtx, logger)

	feed := makeFeedRef(t, 0x20)
	for i := 0; i < 5; i++ {
		ft.Announce(feed)
	}

	time.Sleep(300 * time.Millisecond)
	ft.Stop()

	if createCount != 1 {
		t.Errorf("expected 1 createRecord call after dedup, got %d", createCount)
	}
}

func TestFollowerTrackerStop(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ft := NewFollowerTracker(FollowerTrackerConfig{
		DB:         database,
		BotDID:     "did:plc:bot",
		BotSSBFeed: makeFeedRef(t, 0xFF).String(),
	})

	startCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := log.New(io.Discard, "", 0)
	ft.Start(startCtx, logger)

	done := make(chan struct{})
	go func() {
		ft.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return in time")
	}
}

func TestFollowerTrackerNilXRPCClient(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ft := NewFollowerTracker(FollowerTrackerConfig{
		DB:         database,
		BotDID:     "did:plc:bot",
		BotSSBFeed: makeFeedRef(t, 0xFF).String(),
	})
	ft.debounceDur = 50 * time.Millisecond

	startCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := log.New(io.Discard, "", 0)
	ft.Start(startCtx, logger)

	feed := makeFeedRef(t, 0x30)
	ft.Announce(feed)

	time.Sleep(200 * time.Millisecond)
	ft.Stop()

	ctx := context.Background()
	has, err := database.HasFollowerSync(ctx, "did:plc:bot", feed.String())
	if err != nil {
		t.Fatalf("HasFollowerSync: %v", err)
	}
	if has {
		t.Error("expected no sync when xrpc client is nil")
	}
}
