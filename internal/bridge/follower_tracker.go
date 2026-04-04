package bridge

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/xrpc"
)

type FollowerTracker struct {
	db         *db.DB
	xrpcClient *xrpc.Client
	pdsURL     string
	botDID     string
	botSSBFeed string

	announceCh   chan refs.FeedRef
	debounceDur  time.Duration
	rateLimitDur time.Duration
	maxPerWindow int

	mu          sync.Mutex
	pending     map[string]time.Time
	followCount int
	windowStart time.Time
	stopCh      chan struct{}
	wg          sync.WaitGroup
}

type FollowerTrackerConfig struct {
	DB            *db.DB
	XRPCClient    *xrpc.Client
	PDSURL        string
	BotDID        string
	BotSSBFeed    string
	DebounceDelay time.Duration
	RateLimitDur  time.Duration
	MaxFollowsPer int
}

func NewFollowerTracker(cfg FollowerTrackerConfig) *FollowerTracker {
	if cfg.DebounceDelay == 0 {
		cfg.DebounceDelay = 5 * time.Second
	}
	if cfg.RateLimitDur == 0 {
		cfg.RateLimitDur = 60 * time.Second
	}
	if cfg.MaxFollowsPer == 0 {
		cfg.MaxFollowsPer = 10
	}

	return &FollowerTracker{
		db:           cfg.DB,
		xrpcClient:   cfg.XRPCClient,
		pdsURL:       cfg.PDSURL,
		botDID:       cfg.BotDID,
		botSSBFeed:   cfg.BotSSBFeed,
		announceCh:   make(chan refs.FeedRef, 100),
		debounceDur:  cfg.DebounceDelay,
		rateLimitDur: cfg.RateLimitDur,
		maxPerWindow: cfg.MaxFollowsPer,
		pending:      make(map[string]time.Time),
		stopCh:       make(chan struct{}),
	}
}

func (ft *FollowerTracker) Announce(feed refs.FeedRef) {
	select {
	case ft.announceCh <- feed:
	default:
	}
}

func (ft *FollowerTracker) Start(ctx context.Context, logger *log.Logger) {
	ft.wg.Add(2)
	go ft.processLoop(ctx, logger)
	go ft.rateLimitResetLoop()
}

func (ft *FollowerTracker) Stop() {
	close(ft.stopCh)
	ft.wg.Wait()
}

func (ft *FollowerTracker) processLoop(ctx context.Context, logger *log.Logger) {
	defer ft.wg.Done()

	debouncer := time.NewTimer(ft.debounceDur)
	defer debouncer.Stop()

	for {
		select {
		case <-ft.stopCh:
			return
		case <-ctx.Done():
			return
		case feed := <-ft.announceCh:
			ft.mu.Lock()
			ft.pending[feed.String()] = time.Now()
			ft.mu.Unlock()

			select {
			case <-debouncer.C:
			default:
			}
			debouncer.Reset(ft.debounceDur)

		case <-debouncer.C:
			ft.mu.Lock()
			pending := make(map[string]time.Time)
			for k, v := range ft.pending {
				pending[k] = v
			}
			ft.pending = make(map[string]time.Time)
			ft.mu.Unlock()

			for feedStr := range pending {
				feed, err := refs.ParseFeedRef(feedStr)
				if err != nil {
					logger.Printf("follower-tracker: invalid feed ref: %v", err)
					continue
				}

				if err := ft.syncFollower(ctx, logger, *feed); err != nil {
					logger.Printf("follower-tracker: failed to sync follower %s: %v", feedStr, err)
				}
			}
		}
	}
}

func (ft *FollowerTracker) rateLimitResetLoop() {
	defer ft.wg.Done()

	ticker := time.NewTicker(ft.rateLimitDur)
	defer ticker.Stop()

	for {
		select {
		case <-ft.stopCh:
			return
		case <-ticker.C:
			ft.mu.Lock()
			ft.followCount = 0
			ft.windowStart = time.Now()
			ft.mu.Unlock()
		}
	}
}

func (ft *FollowerTracker) syncFollower(ctx context.Context, logger *log.Logger, followerFeed refs.FeedRef) error {
	followerSSBFeed := followerFeed.String()

	synced, err := ft.db.HasFollowerSync(ctx, ft.botDID, followerSSBFeed)
	if err != nil {
		return err
	}
	if synced {
		return nil
	}

	if ft.botSSBFeed == followerSSBFeed {
		return nil
	}

	ft.mu.Lock()
	if ft.followCount >= ft.maxPerWindow {
		ft.mu.Unlock()
		logger.Printf("follower-tracker: rate limited, skipping follow for %s", followerSSBFeed)
		return nil
	}
	ft.followCount++
	ft.mu.Unlock()

	followerDID, err := ft.resolveSSBFeedToDID(ctx, followerFeed)
	if err != nil {
		logger.Printf("follower-tracker: could not resolve %s to DID: %v", followerSSBFeed, err)
		return nil
	}

	if followerDID == "" {
		logger.Printf("follower-tracker: no DID found for %s", followerSSBFeed)
		return nil
	}

	if err := ft.publishATProtoFollow(ctx, followerDID); err != nil {
		logger.Printf("follower-tracker: failed to publish follow for %s: %v", followerDID, err)
		return err
	}

	if err := ft.db.AddFollowerSync(ctx, ft.botDID, followerSSBFeed); err != nil {
		logger.Printf("follower-tracker: failed to record sync: %v", err)
	}

	logger.Printf("follower-tracker: followed back %s (DID: %s)", followerSSBFeed, followerDID)
	return nil
}

func (ft *FollowerTracker) resolveSSBFeedToDID(ctx context.Context, feed refs.FeedRef) (string, error) {
	if ft.xrpcClient == nil {
		return "", nil
	}

	var result struct {
		Did string `json:"did"`
	}

	err := ft.xrpcClient.LexDo(ctx, "query", "", "com.atproto.identity.resolveHandle", map[string]interface{}{
		"handle": feed.String(),
	}, nil, &result)
	if err == nil && result.Did != "" {
		return result.Did, nil
	}

	return "", nil
}

func (ft *FollowerTracker) publishATProtoFollow(ctx context.Context, targetDID string) error {
	if ft.xrpcClient == nil || ft.botDID == "" {
		return nil
	}

	var result struct {
		URI string `json:"uri"`
	}

	record := map[string]interface{}{
		"$type":     "app.bsky.graph.follow",
		"subject":   targetDID,
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}

	return ft.xrpcClient.LexDo(ctx, "procedure", "application/json", "com.atproto.repo.createRecord", map[string]interface{}{
		"repo":       ft.botDID,
		"collection": "app.bsky.graph.follow",
		"record":     record,
	}, record, &result)
}
