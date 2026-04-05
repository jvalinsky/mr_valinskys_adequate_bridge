package sbot

import (
	"fmt"
	"log/slog"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

type FeedReplicator struct {
	store *feedlog.StoreImpl
}

func NewFeedReplicator(store *feedlog.StoreImpl) *FeedReplicator {
	return &FeedReplicator{store: store}
}

func (f *FeedReplicator) ListFeeds() ([]refs.FeedRef, error) {
	addrs, err := f.store.Logs().List()
	if err != nil {
		return nil, fmt.Errorf("feed replicator: list feeds: %w", err)
	}

	var feeds []refs.FeedRef
	for _, addr := range addrs {
		feedRef, err := refs.ParseFeedRef(addr)
		if err != nil {
			continue
		}
		feeds = append(feeds, *feedRef)
	}
	slog.Debug("feed replicator list feeds", "total", len(feeds), "feeds", fmt.Sprintf("%v", feeds))
	return feeds, nil
}
