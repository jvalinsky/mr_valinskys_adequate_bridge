// Package ssbruntime manages local SSB storage and publishing dependencies.
package ssbruntime

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/mr_valinskys_adequate_bridge/internal/logutil"
	"go.cryptoscope.co/margaret"
	librarian "go.cryptoscope.co/margaret/indexes"
	"go.cryptoscope.co/margaret/multilog"
	"go.cryptoscope.co/ssb"
	"go.cryptoscope.co/ssb/sbot"
	refs "go.mindeco.de/ssb-refs"

	"github.com/mr_valinskys_adequate_bridge/internal/bots"
)

// Runtime owns local SSB dependencies needed for deterministic per-DID publishing.
// It embeds a full sbot daemon to serve messages via EBT.
type Runtime struct {
	logger        *log.Logger
	sbotNode      *sbot.Sbot
	receiveLog    margaret.Log
	userFeeds     multilog.MultiLog
	userFeedIndex librarian.SinkIndex
	blobStore     ssb.BlobStore
	manager       *bots.Manager
}

// Config configures the embedded sbot.
type Config struct {
	RepoPath   string
	ListenAddr string
	MasterSeed []byte
	HMACKey    *[32]byte
}

// Open initializes an SSB runtime rooted at repoPath.
func Open(ctx context.Context, cfg Config, logger *log.Logger) (*Runtime, error) {
	logger = logutil.Ensure(logger)
	if len(cfg.MasterSeed) == 0 {
		return nil, fmt.Errorf("master seed must not be empty")
	}

	if err := os.MkdirAll(cfg.RepoPath, 0o700); err != nil {
		return nil, fmt.Errorf("create repo path: %w", err)
	}

	opts := []sbot.Option{
		sbot.WithContext(ctx),
		sbot.WithRepoPath(cfg.RepoPath),
		sbot.WithListenAddr(cfg.ListenAddr),
		sbot.WithPromisc(true),
		sbot.DisableEBT(false),
	}
	if cfg.HMACKey != nil {
		opts = append(opts, sbot.WithHMACSigning(cfg.HMACKey[:]))
		opts = append(opts, sbot.WithAppKey(cfg.HMACKey[:]))
	}

	// Disable standard bot stuff we don't need or want to deal with right now
	opts = append(opts, sbot.DisableEBT(false)) // keep ebt

	node, err := sbot.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("initialize sbot: %w", err)
	}

	rxLog := node.ReceiveLog
	userFeeds := node.Users
	blobStore := node.BlobStore

	// Start the network listener in the background
	go func() {
		err := node.Network.Serve(ctx)
		if err != nil && err != context.Canceled && err != ssb.ErrShuttingDown {
			logger.Printf("unit=ssbruntime event=network_serve_failed err=%v", err)
		}
	}()

	rt := &Runtime{
		logger:     logger,
		sbotNode:   node,
		receiveLog: rxLog,
		userFeeds:  userFeeds,
		blobStore:  blobStore,
		manager:    bots.NewManager(cfg.MasterSeed, rxLog, userFeeds, cfg.HMACKey),
	}

	// Register existing feeds for replication so they are advertised to peers via EBT/gossip
	existingFeeds, err := userFeeds.List()
	if err == nil {
		for _, f := range existingFeeds {
			feedRef, err := refs.ParseFeedRef(string(f))
			if err == nil {
				node.Replicate(feedRef)
			}
		}
		logger.Printf("unit=ssbruntime event=replication_started count=%d", len(existingFeeds))
	}

	logger.Printf("unit=ssbruntime event=runtime_started repo_path=%s listen_addr=%s", cfg.RepoPath, cfg.ListenAddr)
	return rt, nil
}

// Node returns the embedded sbot daemon.
func (r *Runtime) Node() *sbot.Sbot {
	return r.sbotNode
}

// Publish signs and appends one mapped message for atDID.
func (r *Runtime) Publish(ctx context.Context, atDID string, content map[string]interface{}) (string, error) {
	pub, err := r.manager.GetPublisher(atDID)
	if err != nil {
		return "", fmt.Errorf("ssbruntime: failed to get publisher for %s: %w", atDID, err)
	}

	feedRef, err := r.manager.GetFeedID(atDID)
	if err == nil {
		// Ensure our sbot is following this bot identity so it advertises it via EBT
		r.logger.Printf("unit=ssbruntime event=publishing_as_managed_bot feed=%s", feedRef)
		r.sbotNode.Replicate(feedRef)
	}

	msgRef, err := pub.Publish(content)
	if err != nil {
		return "", fmt.Errorf("ssbruntime: failed to publish message for %s: %w", atDID, err)
	}

	return msgRef.Ref(), nil
}

// ResolveFeed returns the deterministic SSB feed ref for atDID without creating a DB account row.
func (r *Runtime) ResolveFeed(_ context.Context, atDID string) (string, error) {
	if r == nil || r.manager == nil {
		return "", fmt.Errorf("runtime manager is nil")
	}

	feedRef, err := r.manager.GetFeedID(atDID)
	if err != nil {
		return "", fmt.Errorf("get feed id for %s: %w", atDID, err)
	}
	return feedRef.Ref(), nil
}

// BlobStore returns the underlying SSB blob store used by this runtime.
func (r *Runtime) BlobStore() ssb.BlobStore {
	return r.blobStore
}

// Close releases runtime indexes and logs by stopping sbot.
func (r *Runtime) Close() error {
	if r.sbotNode != nil {
		r.sbotNode.Shutdown()
		return r.sbotNode.Close()
	}
	return nil
}
