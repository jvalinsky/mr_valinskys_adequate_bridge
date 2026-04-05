package sbot

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

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

	var content interface{}
	if err := json.Unmarshal(msg.Value, &content); err != nil {
		content = string(msg.Value)
	}

	buf := &bytes.Buffer{}
	buf.WriteString("{\n")

	// NOTE: Do NOT include "key" field here. EBT messages are raw message
	// objects (previous, author, sequence, timestamp, hash, content, signature).
	// The "key" field is only used in createHistoryStream key-value format.
	// Including it breaks TF's signature verification and message ID computation.

	buf.WriteString(`  "previous": `)
	if msg.Metadata.Previous != "" {
		buf.WriteString(`"` + msg.Metadata.Previous + `"`)
	} else {
		buf.WriteString("null")
	}
	buf.WriteString(",\n")

	buf.WriteString(`  "author": "`)
	buf.WriteString(msg.Metadata.Author)
	buf.WriteString(`",` + "\n")

	buf.WriteString(`  "sequence": `)
	buf.WriteString(strconv.FormatInt(msg.Metadata.Sequence, 10))
	buf.WriteString(",\n")

	buf.WriteString(`  "timestamp": `)
	buf.WriteString(strconv.FormatInt(msg.Metadata.Timestamp, 10))
	buf.WriteString(",\n")

	buf.WriteString(`  "hash": "sha256",` + "\n")

	buf.WriteString(`  "content": `)
	// Content must be indented to match JSON.stringify(msg, null, 2) semantics.
	contentBytes, err := json.MarshalIndent(content, "  ", "  ")
	if err != nil {
		return nil, fmt.Errorf("feed manager: marshal content: %w", err)
	}
	buf.Write(contentBytes)
	buf.WriteString(",\n")

	buf.WriteString(`  "signature": "`)
	buf.WriteString(base64.StdEncoding.EncodeToString(msg.Metadata.Sig))
	buf.WriteString(`.sig.ed25519"` + "\n")

	buf.WriteString("}")

	msgBytes := buf.Bytes()
	slog.Debug("feed manager get message full msg", "seq", seq, "msg", string(msgBytes))
	slog.Debug("feed manager get message bytes", "author", author.String(), "seq", seq, "bytes", len(msgBytes))
	return msgBytes, nil
}

var _ replication.FeedManager = (*FeedManagerAdapter)(nil)
