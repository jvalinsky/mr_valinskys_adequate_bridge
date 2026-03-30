package sbot

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/replication"
)

type FeedManagerAdapter struct {
	store *feedlog.StoreImpl
}

func NewFeedManagerAdapter(store *feedlog.StoreImpl) *FeedManagerAdapter {
	return &FeedManagerAdapter{store: store}
}

func (f *FeedManagerAdapter) GetFeedSeq(author *refs.FeedRef) (int64, error) {
	log.Printf("[EBT DEBUG] FeedManagerAdapter.GetFeedSeq: author=%s", author.String())
	l, err := f.store.Logs().Get(author.Ref())
	if err != nil {
		return -1, fmt.Errorf("feed manager: get log: %w", err)
	}
	seq, err := l.Seq()
	log.Printf("[EBT DEBUG] FeedManagerAdapter.GetFeedSeq: author=%s seq=%d", author.String(), seq)
	return seq, nil
}

func (f *FeedManagerAdapter) GetMessage(author *refs.FeedRef, seq int64) ([]byte, error) {
	log.Printf("[EBT DEBUG] FeedManagerAdapter.GetMessage: author=%s seq=%d", author.String(), seq)
	l, err := f.store.Logs().Get(author.Ref())
	if err != nil {
		return nil, fmt.Errorf("feed manager: get log: %w", err)
	}

	msg, err := l.Get(seq)
	if err != nil {
		return nil, fmt.Errorf("feed manager: get message: %w", err)
	}

	var content interface{}
	if err := json.Unmarshal(msg.Value, &content); err != nil {
		content = string(msg.Value)
	}

	var previous interface{}
	if msg.Metadata.Previous != "" {
		previous = msg.Metadata.Previous
	}

	value := map[string]interface{}{
		"author":    msg.Metadata.Author,
		"sequence":  msg.Metadata.Sequence,
		"previous":  previous,
		"signature": fmt.Sprintf("%x", msg.Metadata.Sig),
		"content":   content,
	}

	msgData := map[string]interface{}{
		"key":       msg.Metadata.Hash,
		"value":     value,
		"timestamp": msg.Metadata.Timestamp,
	}

	data, err := json.Marshal(msgData)
	if err != nil {
		return nil, fmt.Errorf("feed manager: marshal message: %w", err)
	}

	log.Printf("[EBT DEBUG] FeedManagerAdapter.GetMessage: author=%s seq=%d msg_bytes=%d", author.String(), seq, len(data))
	return data, nil
}

var _ replication.FeedManager = (*FeedManagerAdapter)(nil)
