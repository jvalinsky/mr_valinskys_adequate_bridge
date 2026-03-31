// Package ssbruntime manages local SSB storage and publishing dependencies.
package ssbruntime

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bots"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/sbot"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/handlers"
)

type Runtime struct {
	logger        *log.Logger
	sbotNode      *sbot.Sbot
	receiveLog    feedlog.Log
	userFeeds     feedlog.MultiLog
	blobStore     feedlog.BlobStore
	manager       *bots.Manager
	followedFeeds sync.Map
}

type Config struct {
	RepoPath   string
	ListenAddr string
	MasterSeed []byte
	HMACKey    *[32]byte
	KeyPair    *keys.KeyPair
	AppKey     string
}

func Open(ctx context.Context, cfg Config, logger *log.Logger) (*Runtime, error) {
	logger = logutil.Ensure(logger)
	if len(cfg.MasterSeed) == 0 {
		return nil, fmt.Errorf("master seed must not be empty")
	}

	if err := os.MkdirAll(cfg.RepoPath, 0o700); err != nil {
		return nil, fmt.Errorf("create repo path: %w", err)
	}

	appKey := cfg.AppKey
	if cfg.HMACKey != nil {
		appKey = string(cfg.HMACKey[:])
	}

	node, err := sbot.New(sbot.Options{
		RepoPath:   cfg.RepoPath,
		ListenAddr: cfg.ListenAddr,
		KeyPair:    cfg.KeyPair,
		AppKey:     appKey,
		EnableEBT:  true,
		Hops:       2,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize sbot: %w", err)
	}

	rxLog, err := node.Store().ReceiveLog()
	if err != nil {
		return nil, fmt.Errorf("get receive log: %w", err)
	}

	userFeeds := node.Store().Logs()
	blobStore := node.Store().Blobs()

	go func() {
		if err := node.Serve(); err != nil && err != context.Canceled {
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

	existingFeeds, err := userFeeds.List()
	if err == nil {
		registered := 0
		for _, f := range existingFeeds {
			sublog, err := userFeeds.Get(f)
			if err != nil {
				continue
			}
			seq, err := sublog.Seq()
			if err != nil || seq < 0 {
				continue
			}
			registered++
		}
		logger.Printf("unit=ssbruntime event=replication_started total=%d registered=%d", len(existingFeeds), registered)
	}

	logger.Printf("unit=ssbruntime event=runtime_started repo_path=%s listen_addr=%s", cfg.RepoPath, cfg.ListenAddr)
	return rt, nil
}

func (r *Runtime) Node() *sbot.Sbot {
	return r.sbotNode
}

func (r *Runtime) Publish(ctx context.Context, atDID string, content map[string]interface{}) (string, error) {
	pub, err := r.manager.GetPublisher(atDID)
	if err != nil {
		return "", fmt.Errorf("ssbruntime: failed to get publisher for %s: %w", atDID, err)
	}

	feedRef, err := r.manager.GetFeedID(atDID)
	if err == nil {
		feedKey := feedRef.String()
		if _, alreadyFollowed := r.followedFeeds.LoadOrStore(feedKey, true); !alreadyFollowed {
			r.logger.Printf("unit=ssbruntime event=publishing_ebt_follow feed=%s", feedRef)
		}
	}

	msgRef, err := pub.PublishJSON(content)
	if err != nil {
		return "", fmt.Errorf("ssbruntime: failed to publish message for %s: %w", atDID, err)
	}

	if err == nil && feedRef.Ref() != "" {
		seq, err := pub.Seq()
		if err == nil {
			r.sbotNode.NotifyFeedSeq(&feedRef, seq)
		}
	}

	return msgRef.String(), nil
}

func (r *Runtime) ResolveFeed(_ context.Context, atDID string) (string, error) {
	if r == nil || r.manager == nil {
		return "", fmt.Errorf("runtime manager is nil")
	}

	feedRef, err := r.manager.GetFeedID(atDID)
	if err != nil {
		return "", fmt.Errorf("get feed id for %s: %w", atDID, err)
	}
	return feedRef.String(), nil
}

func (r *Runtime) BlobStore() feedlog.BlobStore {
	return r.blobStore
}

func (r *Runtime) GetPeers() []handlers.PeerStatus {
	if r.sbotNode == nil {
		return nil
	}
	peers := r.sbotNode.Peers()
	res := make([]handlers.PeerStatus, 0, len(peers))
	for _, p := range peers {
		res = append(res, handlers.PeerStatus{
			Addr: p.Conn.RemoteAddr().String(),
			Feed: p.ID.String(),
		})
	}
	return res
}

func (r *Runtime) GetEBTState() map[string]map[string]int64 {
	if r.sbotNode == nil || r.sbotNode.StateMatrix() == nil {
		return nil
	}
	return r.sbotNode.StateMatrix().Export()
}

func (r *Runtime) Close() error {
	if r.sbotNode != nil {
		return r.sbotNode.Shutdown()
	}
	return nil
}
