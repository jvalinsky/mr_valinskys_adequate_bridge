// Package protocoltrace provides opt-in, metadata-only tracing for SSB protocol
// debugging. It deliberately avoids logging raw payloads, keys, or SHS secrets.
package protocoltrace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/formats"
)

type Tracer struct {
	runID string
	log   *log.Logger

	nextConn   atomic.Uint64
	nextStream atomic.Uint64
}

type Event struct {
	Phase          string
	Direction      string
	ConnID         string
	StreamID       string
	Feed           string
	PeerFeed       string
	ReplicatedFeed string
	FeedFormat     string
	MessageFormat  string
	HistoryFormat  string
	BlobRef        string
	Transport      string
	Method         string
	Req            int32
	Origin         string
	Portal         string
	Target         string
	Bytes          int
	ErrKind        string
	Duration       time.Duration
}

var current atomic.Pointer[Tracer]

func Configure(enabled bool, runID string, logger *log.Logger) {
	if !enabled {
		current.Store(nil)
		return
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		runID = strings.TrimSpace(os.Getenv("MVAB_TEST_RUN_ID"))
	}
	if runID == "" {
		runID = fmt.Sprintf("run-%d", time.Now().UTC().UnixNano())
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	current.Store(&Tracer{
		runID: runID,
		log:   logger,
	})
}

func Current() *Tracer {
	return current.Load()
}

func Enabled() bool {
	return Current() != nil
}

func NewConnID(origin string) string {
	t := Current()
	if t == nil {
		return ""
	}
	return t.NewConnID(origin)
}

func NewStreamID() string {
	t := Current()
	if t == nil {
		return ""
	}
	return t.NewStreamID()
}

func Emit(ev Event) {
	if t := Current(); t != nil {
		t.Emit(ev)
	}
}

func (t *Tracer) NewConnID(origin string) string {
	origin = sanitizeToken(origin)
	if origin == "" {
		origin = "conn"
	}
	return fmt.Sprintf("%s-%d", origin, t.nextConn.Add(1))
}

func (t *Tracer) NewStreamID() string {
	return fmt.Sprintf("stream-%d", t.nextStream.Add(1))
}

func (t *Tracer) Emit(ev Event) {
	if t == nil || t.log == nil {
		return
	}
	fields := []string{"event=ssb_protocol_trace", "run_id=" + formatValue(t.runID)}
	appendKV := func(key, value string) {
		value = SanitizeField(key, value)
		if strings.TrimSpace(value) == "" {
			return
		}
		fields = append(fields, key+"="+formatValue(value))
	}

	appendKV("phase", ev.Phase)
	appendKV("direction", ev.Direction)
	appendKV("conn_id", ev.ConnID)
	appendKV("stream_id", ev.StreamID)
	appendKV("feed", ev.Feed)
	appendKV("peer_feed", ev.PeerFeed)
	appendKV("replicated_feed", ev.ReplicatedFeed)
	appendKV("feed_format", ev.FeedFormat)
	appendKV("message_format", ev.MessageFormat)
	appendKV("history_format", ev.HistoryFormat)
	appendKV("blob_ref", ev.BlobRef)
	appendKV("transport", ev.Transport)
	appendKV("method", ev.Method)
	if ev.Req != 0 {
		fields = append(fields, "req="+strconv.FormatInt(int64(ev.Req), 10))
	}
	appendKV("origin", ev.Origin)
	appendKV("portal", ev.Portal)
	appendKV("target", ev.Target)
	if ev.Bytes > 0 {
		fields = append(fields, "bytes="+strconv.Itoa(ev.Bytes))
	}
	appendKV("err_kind", ev.ErrKind)
	if ev.Duration > 0 {
		fields = append(fields, "duration_ms="+strconv.FormatInt(ev.Duration.Milliseconds(), 10))
	}

	t.log.Print(strings.Join(fields, " "))
}

func SanitizeField(key, value string) string {
	lower := strings.ToLower(strings.TrimSpace(key))
	switch {
	case strings.Contains(lower, "secret"),
		strings.Contains(lower, "private"),
		strings.Contains(lower, "password"),
		strings.Contains(lower, "token"),
		strings.Contains(lower, "hmac"),
		strings.Contains(lower, "seed"),
		strings.Contains(lower, "cap"):
		return "[redacted]"
	default:
		return strings.TrimSpace(value)
	}
}

func ErrKind(err error) string {
	if err == nil {
		return ""
	}
	if kind := formats.ErrorKind(err); kind != "" {
		return kind
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case errors.Is(err, context.DeadlineExceeded), isNetTimeout(err):
		return "timeout"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrClosedPipe), errors.Is(err, net.ErrClosed):
		return "closed"
	}

	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "sqlite_busy"),
		strings.Contains(lower, "database is locked"),
		strings.Contains(lower, "database table is locked"):
		return "sqlite_busy"
	case strings.Contains(lower, "handshake"):
		return "handshake"
	case strings.Contains(lower, "muxrpc remote error"),
		strings.Contains(lower, "name:error"):
		return "remote_error"
	case strings.Contains(lower, "parse"),
		strings.Contains(lower, "decode"),
		strings.Contains(lower, "unmarshal"):
		return "decode"
	case strings.Contains(lower, "membership"):
		return "membership"
	case strings.Contains(lower, "exceeds max size"),
		strings.Contains(lower, "too large"):
		return "too_large"
	case strings.Contains(lower, "not announced"),
		strings.Contains(lower, "unavailable"):
		return "unavailable"
	default:
		return "other"
	}
}

func isNetTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func formatValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return `""`
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '"'
	}) >= 0 {
		return strconv.Quote(value)
	}
	return value
}

func sanitizeToken(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, value)
	return strings.Trim(value, "-")
}
