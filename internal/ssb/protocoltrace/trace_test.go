package protocoltrace

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/formats"
)

func TestSanitizeFieldRedactsSensitiveKeys(t *testing.T) {
	tests := []string{"private_key", "secret", "password", "token", "hmac_key", "bot_seed", "shs_cap"}
	for _, key := range tests {
		if got := SanitizeField(key, "sensitive"); got != "[redacted]" {
			t.Fatalf("SanitizeField(%q) = %q, want redacted", key, got)
		}
	}
	if got := SanitizeField("feed", " @abc.ed25519 "); got != "@abc.ed25519" {
		t.Fatalf("feed sanitize = %q", got)
	}
}

func TestEmitIncludesCorrelationFields(t *testing.T) {
	var buf bytes.Buffer
	Configure(true, "test-run", log.New(&buf, "", 0))
	defer Configure(false, "", nil)

	connID := NewConnID("room")
	streamID := NewStreamID()
	Emit(Event{
		Phase:          "call_out",
		Direction:      "out",
		ConnID:         connID,
		StreamID:       streamID,
		Feed:           "@peer.ed25519",
		PeerFeed:       "@peer.ed25519",
		ReplicatedFeed: "@replicated.ed25519",
		FeedFormat:     "ed25519",
		MessageFormat:  "sha256",
		HistoryFormat:  "classic",
		BlobRef:        "&blob.sha256",
		Transport:      "room_tunnel",
		Method:         "tunnel.connect",
		Req:            7,
		Origin:         "@origin.ed25519",
		Portal:         "@portal.ed25519",
		Target:         "@target.ed25519",
		Bytes:          123,
	})

	line := buf.String()
	for _, want := range []string{
		"event=ssb_protocol_trace",
		"run_id=test-run",
		"phase=call_out",
		"direction=out",
		"conn_id=" + connID,
		"stream_id=" + streamID,
		"feed=@peer.ed25519",
		"peer_feed=@peer.ed25519",
		"replicated_feed=@replicated.ed25519",
		"feed_format=ed25519",
		"message_format=sha256",
		"history_format=classic",
		"blob_ref=&blob.sha256",
		"transport=room_tunnel",
		"method=tunnel.connect",
		"req=7",
		"origin=@origin.ed25519",
		"portal=@portal.ed25519",
		"target=@target.ed25519",
		"bytes=123",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("trace line missing %q:\n%s", want, line)
		}
	}
}

func TestErrKind(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"canceled", context.Canceled, "context_canceled"},
		{"deadline", context.DeadlineExceeded, "timeout"},
		{"eof", io.EOF, "closed"},
		{"sqlite", errors.New("database is locked"), "sqlite_busy"},
		{"handshake", errors.New("tunnel.connect inner SHS handshake: EOF"), "handshake"},
		{"decode", errors.New("decode history envelope: nope"), "decode"},
		{"membership", errors.New("room.attendants: membership required"), "membership"},
		{"unsupported feed", formats.UnsupportedFeed(formats.FeedBamboo, "ebt.replicate", "append"), "unsupported_feed_format"},
		{"unsupported message", formats.UnsupportedMessage(formats.MessageIndexedV1, "createHistoryStream", "history"), "unsupported_message_format"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ErrKind(tt.err); got != tt.want {
				t.Fatalf("ErrKind(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
