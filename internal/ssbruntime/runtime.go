// Package ssbruntime manages local SSB storage and publishing dependencies.
package ssbruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"go.cryptoscope.co/luigi"
	"go.cryptoscope.co/margaret"
	librarian "go.cryptoscope.co/margaret/indexes"
	"go.cryptoscope.co/margaret/multilog"
	"go.cryptoscope.co/ssb"
	ssbmultilogs "go.cryptoscope.co/ssb/multilogs"
	ssbrepo "go.cryptoscope.co/ssb/repo"

	"github.com/mr_valinskys_adequate_bridge/internal/bots"
)

// Runtime owns local SSB dependencies needed for deterministic per-DID publishing.
type Runtime struct {
	logger        *log.Logger
	receiveLog    margaret.Log
	userFeeds     multilog.MultiLog
	userFeedIndex librarian.SinkIndex
	blobStore     ssb.BlobStore
	manager       *bots.Manager
}

// Open initializes an SSB runtime rooted at repoPath.
func Open(repoPath string, masterSeed []byte, hmacKey *[32]byte, logger *log.Logger) (*Runtime, error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	if len(masterSeed) == 0 {
		return nil, fmt.Errorf("master seed must not be empty")
	}

	if err := os.MkdirAll(repoPath, 0o700); err != nil {
		return nil, fmt.Errorf("create repo path: %w", err)
	}

	repo := ssbrepo.New(repoPath)

	rxLog, err := ssbrepo.OpenLog(repo)
	if err != nil {
		return nil, fmt.Errorf("open receive log: %w", err)
	}

	userFeeds, userFeedIndex, err := ssbrepo.OpenStandaloneMultiLog(repo, "userFeeds", ssbmultilogs.UserFeedsUpdate)
	if err != nil {
		return nil, fmt.Errorf("open user feeds index: %w", err)
	}

	blobStore, err := ssbrepo.OpenBlobStore(repo)
	if err != nil {
		return nil, fmt.Errorf("open blob store: %w", err)
	}

	rt := &Runtime{
		logger:        logger,
		receiveLog:    rxLog,
		userFeeds:     userFeeds,
		userFeedIndex: userFeedIndex,
		blobStore:     blobStore,
		manager:       bots.NewManager(masterSeed, rxLog, userFeeds, hmacKey),
	}

	if err := rt.refreshUserFeeds(context.Background()); err != nil {
		_ = rt.Close()
		return nil, err
	}

	return rt, nil
}

// Publish signs and appends one mapped message for atDID.
func (r *Runtime) Publish(ctx context.Context, atDID string, content map[string]interface{}) (string, error) {
	if err := r.refreshUserFeeds(ctx); err != nil {
		return "", err
	}

	pub, err := r.manager.GetPublisher(atDID)
	if err != nil {
		return "", fmt.Errorf("get publisher for %s: %w", atDID, err)
	}

	msgRef, err := pub.Publish(content)
	if err != nil {
		return "", fmt.Errorf("publish: %w", err)
	}

	// Keep author feed index current so follow-up records can resolve references.
	if err := r.refreshUserFeeds(ctx); err != nil {
		r.logger.Printf("event=warn step=refresh_userfeeds_after_publish did=%s err=%v", atDID, err)
	}

	return msgRef.Ref(), nil
}

// BlobStore returns the underlying SSB blob store used by this runtime.
func (r *Runtime) BlobStore() ssb.BlobStore {
	return r.blobStore
}

func (r *Runtime) refreshUserFeeds(ctx context.Context) error {
	src, err := r.receiveLog.Query(r.userFeedIndex.QuerySpec())
	if err != nil {
		return fmt.Errorf("query user feed index source: %w", err)
	}

	if err := luigi.Pump(ctx, r.userFeedIndex, src); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("pump user feed index: %w", err)
	}
	return nil
}

// Close releases runtime indexes and logs.
func (r *Runtime) Close() error {
	var errs []error

	if closer, ok := r.userFeedIndex.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close user feed index: %w", err))
		}
	}
	if closer, ok := r.userFeeds.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close user feeds multilog: %w", err))
		}
	}
	if closer, ok := r.receiveLog.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close receive log: %w", err))
		}
	}

	return errors.Join(errs...)
}
