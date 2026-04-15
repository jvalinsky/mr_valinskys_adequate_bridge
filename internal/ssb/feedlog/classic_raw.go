package feedlog

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/formats"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

// ClassicSignedMessageRaw returns the exact signed JSON payload for classic
// SSB messages. It prefers stored raw bytes and only rebuilds when older rows
// are missing RawValue.
func ClassicSignedMessageRaw(msg *StoredMessage, fallbackAuthor *refs.FeedRef, fallbackSeq int64) ([]byte, error) {
	if msg == nil {
		return nil, fmt.Errorf("feedlog: nil stored message")
	}

	msgFormat := formats.MessageFormat(strings.TrimSpace(msg.MessageFormat))
	if msgFormat != "" && msgFormat != formats.MessageSHA256 {
		return nil, fmt.Errorf("feedlog: %w", formats.UnsupportedMessage(msgFormat, "classic_raw", "derive"))
	}

	if len(msg.RawValue) > 0 {
		return cloneBytes(msg.RawValue), nil
	}

	if msg.Metadata == nil {
		return nil, fmt.Errorf("feedlog: classic message missing metadata")
	}
	if len(msg.Metadata.Sig) == 0 {
		return nil, fmt.Errorf("feedlog: classic message missing signature")
	}

	author, err := resolveClassicAuthor(msg.Metadata.Author, fallbackAuthor)
	if err != nil {
		return nil, err
	}

	seq := msg.Metadata.Sequence
	if seq <= 0 {
		seq = fallbackSeq
	}
	if seq <= 0 {
		return nil, fmt.Errorf("feedlog: classic message missing sequence")
	}

	var previous *refs.MessageRef
	if prevStr := strings.TrimSpace(msg.Metadata.Previous); prevStr != "" {
		prevRef, err := refs.ParseMessageRef(prevStr)
		if err != nil {
			return nil, fmt.Errorf("feedlog: parse previous ref: %w", err)
		}
		previous = prevRef
	}

	var content interface{}
	if err := json.Unmarshal(msg.Value, &content); err != nil {
		content = string(msg.Value)
	}

	legacyMsg := &legacy.Message{
		Previous:  previous,
		Author:    author,
		Sequence:  seq,
		Timestamp: msg.Metadata.Timestamp,
		Hash:      legacy.HashAlgorithm,
		Content:   content,
	}
	raw, err := legacyMsg.MarshalWithSignature(msg.Metadata.Sig)
	if err != nil {
		return nil, fmt.Errorf("feedlog: marshal classic message: %w", err)
	}

	return raw, nil
}

func resolveClassicAuthor(author string, fallback *refs.FeedRef) (refs.FeedRef, error) {
	author = strings.TrimSpace(author)
	if author != "" {
		parsed, err := refs.ParseFeedRef(author)
		if err != nil {
			return refs.FeedRef{}, fmt.Errorf("feedlog: parse author ref: %w", err)
		}
		return *parsed, nil
	}
	if fallback == nil {
		return refs.FeedRef{}, fmt.Errorf("feedlog: classic message missing author")
	}
	return *fallback, nil
}
