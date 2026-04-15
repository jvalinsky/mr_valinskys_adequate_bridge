package sbot

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/bfe"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/formats"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/bendy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
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
	slog.Debug("feed manager get feed seq", "author", author.String())
	l, err := f.store.Logs().Get(author.Ref())
	if err != nil {
		return -1, fmt.Errorf("feed manager: get log: %w", err)
	}
	seq, err := l.Seq()
	slog.Debug("feed manager get feed seq result", "author", author.String(), "seq", seq)
	return seq, nil
}

func (f *FeedManagerAdapter) GetMessage(author *refs.FeedRef, seq int64) ([]byte, error) {
	slog.Debug("feed manager get message", "author", author.String(), "seq", seq)
	l, err := f.store.Logs().Get(author.Ref())
	if err != nil {
		return nil, fmt.Errorf("feed manager: get log: %w", err)
	}

	msg, err := l.Get(seq)
	if err != nil {
		return nil, fmt.Errorf("feed manager: get message: %w", err)
	}

	switch formats.MessageFormat(msg.MessageFormat) {
	case "", formats.MessageSHA256:
		raw, err := feedlog.ClassicSignedMessageRaw(msg, author, seq)
		if err != nil {
			return nil, fmt.Errorf("feed manager: get classic message: %w", err)
		}
		slog.Debug("feed manager get message bytes", "author", author.String(), "seq", seq, "bytes", len(raw))
		return raw, nil
	case formats.MessageBendyButtV1:
		if len(msg.RawValue) == 0 {
			return nil, fmt.Errorf("feed manager: %w", formats.UnsupportedMessage(formats.MessageBendyButtV1, "ebt.replicate", "get_message_no_raw_value"))
		}
		return msg.RawValue, nil
	default:
		msgFormat := formats.MessageFormat(msg.MessageFormat)
		if msgFormat == "" {
			msgFormat = formats.MessageFromFeed(formats.FeedFormat(msg.FeedFormat))
		}
		return nil, fmt.Errorf("feed manager: %w", formats.UnsupportedMessage(msgFormat, "ebt.replicate", "get_message"))
	}
}

func (f *FeedManagerAdapter) AppendSignedMessage(raw []byte) (*refs.FeedRef, int64, error) {
	return f.AppendReplicatedMessage(raw)
}

func (f *FeedManagerAdapter) AppendReplicatedMessage(raw []byte) (*refs.FeedRef, int64, error) {
	msg, contentBytes, err := legacy.ParseSignedMessageJSON(raw)
	if err != nil {
		return f.appendBendyMessage(raw, err)
	}

	msgRef, err := legacy.SignedMessageRefFromJSON(raw)
	if err != nil {
		return nil, 0, fmt.Errorf("feed manager: derive message ref: %w", err)
	}

	metadata := &feedlog.Metadata{
		Author:           msg.Author.String(),
		Sequence:         msg.Sequence,
		Timestamp:        msg.Timestamp,
		Sig:              msg.Signature,
		Hash:             msgRef.String(),
		FeedFormat:       string(formats.FeedEd25519),
		MessageFormat:    string(formats.MessageSHA256),
		RawValue:         raw,
		CanonicalRef:     msgRef.String(),
		ValidationStatus: "validated",
	}
	if msg.Previous != nil {
		metadata.Previous = strings.TrimSpace(msg.Previous.String())
	}

	log, err := f.store.Logs().Get(msg.Author.String())
	if err == feedlog.ErrNotFound {
		log, err = f.store.Logs().Create(msg.Author.String())
	}
	if err != nil {
		return nil, 0, fmt.Errorf("feed manager: open log for %s: %w", msg.Author.String(), err)
	}

	currentSeq, err := log.Seq()
	if err != nil {
		return nil, 0, fmt.Errorf("feed manager: read seq for %s: %w", msg.Author.String(), err)
	}
	if msg.Sequence <= currentSeq {
		return &msg.Author, currentSeq, nil
	}
	if currentSeq >= 0 && msg.Sequence != currentSeq+1 {
		return nil, 0, fmt.Errorf("feed manager: sequence gap for %s: have=%d got=%d", msg.Author.String(), currentSeq, msg.Sequence)
	}

	if _, err := log.Append(contentBytes, metadata); err != nil {
		return nil, 0, fmt.Errorf("feed manager: append feed log for %s: %w", msg.Author.String(), err)
	}

	receiveLog, err := f.store.ReceiveLog()
	if err != nil {
		return nil, 0, fmt.Errorf("feed manager: open receive log: %w", err)
	}
	if _, err := receiveLog.Append(raw, metadata); err != nil {
		return nil, 0, fmt.Errorf("feed manager: append receive log for %s: %w", msg.Author.String(), err)
	}

	return &msg.Author, msg.Sequence, nil
}

func (f *FeedManagerAdapter) appendBendyMessage(raw []byte, classicErr error) (*refs.FeedRef, int64, error) {
	msg, err := bendy.FromStoredMessage(raw)
	if err != nil {
		return nil, 0, fmt.Errorf("feed manager: parse replicated message: classic=%v bendy=%w", classicErr, err)
	}
	if err := msg.Verify(); err != nil {
		return nil, 0, fmt.Errorf("feed manager: verify bendybutt message: %w", err)
	}

	algo, pubKey, err := bfe.DecodeFeed(msg.Author)
	if err != nil {
		return nil, 0, fmt.Errorf("feed manager: decode bendybutt author: %w", err)
	}
	if algo != string(formats.FeedBendyButtV1) {
		return nil, 0, fmt.Errorf("feed manager: %w", formats.UnsupportedFeed(formats.FeedFormat(algo), "ebt.replicate", "append"))
	}
	author, err := refs.NewFeedRef(pubKey, refs.RefAlgoFeedBendyButt)
	if err != nil {
		return nil, 0, fmt.Errorf("feed manager: author ref: %w", err)
	}
	msgRef, err := msg.ToRefsMessage()
	if err != nil {
		return nil, 0, fmt.Errorf("feed manager: derive bendybutt message ref: %w", err)
	}
	previous := ""
	if len(msg.Previous) > 0 {
		if prevAlgo, prevHash, err := bfe.DecodeMessage(msg.Previous); err == nil {
			if ref, err := refs.NewMessageRef(prevHash, refs.RefAlgoMessage(prevAlgo)); err == nil {
				previous = ref.String()
			}
		}
	}

	contentBytes, err := json.Marshal(msg.ContentSection[0])
	if err != nil {
		contentBytes = raw
	}
	metadata := &feedlog.Metadata{
		Author:           author.String(),
		Sequence:         msg.Sequence,
		Previous:         previous,
		Timestamp:        msg.Timestamp,
		Hash:             msgRef.String(),
		FeedFormat:       string(formats.FeedBendyButtV1),
		MessageFormat:    string(formats.MessageBendyButtV1),
		RawValue:         raw,
		CanonicalRef:     msgRef.String(),
		ValidationStatus: "validated",
	}

	log, err := f.store.Logs().Get(author.String())
	if err == feedlog.ErrNotFound {
		log, err = f.store.Logs().Create(author.String())
	}
	if err != nil {
		return nil, 0, fmt.Errorf("feed manager: open log for %s: %w", author.String(), err)
	}

	currentSeq, err := log.Seq()
	if err != nil {
		return nil, 0, fmt.Errorf("feed manager: read seq for %s: %w", author.String(), err)
	}
	if msg.Sequence <= currentSeq {
		return author, currentSeq, nil
	}
	if currentSeq >= 0 && msg.Sequence != currentSeq+1 {
		return nil, 0, fmt.Errorf("feed manager: sequence gap for %s: have=%d got=%d", author.String(), currentSeq, msg.Sequence)
	}

	if _, err := log.Append(contentBytes, metadata); err != nil {
		return nil, 0, fmt.Errorf("feed manager: append feed log for %s: %w", author.String(), err)
	}

	receiveLog, err := f.store.ReceiveLog()
	if err != nil {
		return nil, 0, fmt.Errorf("feed manager: open receive log: %w", err)
	}
	if _, err := receiveLog.Append(raw, metadata); err != nil {
		return nil, 0, fmt.Errorf("feed manager: append receive log for %s: %w", author.String(), err)
	}

	return author, msg.Sequence, nil
}

var _ replication.FeedManager = (*FeedManagerAdapter)(nil)
