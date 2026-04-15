package formats

import (
	"errors"
	"strings"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

func TestRegistrySupportMatrix(t *testing.T) {
	tests := []struct {
		name   string
		feed   FeedFormat
		status SupportStatus
	}{
		{"classic", FeedEd25519, StatusSupported},
		{"bendy", FeedBendyButtV1, StatusPartial},
		{"gabby", FeedGabbyGroveV1, StatusUnsupported},
		{"bamboo", FeedBamboo, StatusUnsupported},
		{"indexed", FeedIndexedV1, StatusUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DefaultRegistry.FeedStatus(tt.feed); got != tt.status {
				t.Fatalf("FeedStatus(%q) = %q, want %q", tt.feed, got, tt.status)
			}
			msgFormat := MessageFromFeed(tt.feed)
			if msgFormat == "" {
				t.Fatalf("MessageFromFeed(%q) returned empty format", tt.feed)
			}
			if _, ok := DefaultRegistry.Message(msgFormat); !ok {
				t.Fatalf("message format %q is not registered", msgFormat)
			}
		})
	}
}

func TestUnsupportedFormatErrorClassification(t *testing.T) {
	err := UnsupportedFeed(FeedBamboo, "ebt.replicate", "append")
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("unsupported feed error does not wrap ErrUnsupportedFormat")
	}
	if got := ErrorKind(err); got != "unsupported_feed_format" {
		t.Fatalf("ErrorKind(feed) = %q", got)
	}
	if !strings.Contains(err.Error(), "feed") || !strings.Contains(err.Error(), "bamboo") {
		t.Fatalf("feed error lacks useful fields: %v", err)
	}

	err = UnsupportedMessage(MessageIndexedV1, "createHistoryStream", "history")
	if got := ErrorKind(err); got != "unsupported_message_format" {
		t.Fatalf("ErrorKind(message) = %q", got)
	}
	if !strings.Contains(err.Error(), "indexed-v1") || !strings.Contains(err.Error(), "createHistoryStream") {
		t.Fatalf("message error lacks useful fields: %v", err)
	}
}

func TestFormatFromRefs(t *testing.T) {
	feed := refs.MustNewFeedRef(make([]byte, 32), refs.RefAlgoFeedBamboo)
	if got := FeedFromRef(feed); got != FeedBamboo {
		t.Fatalf("FeedFromRef = %q", got)
	}
	msg := refs.MustNewMessageRef(make([]byte, 32), refs.RefAlgoMessageIndexed)
	if got := MessageFromRef(msg); got != MessageIndexedV1 {
		t.Fatalf("MessageFromRef = %q", got)
	}
	if !IsClassic(FeedEd25519, MessageSHA256) {
		t.Fatal("classic format pair not recognized")
	}
}
