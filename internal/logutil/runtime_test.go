package logutil

import (
	"bytes"
	"context"
	"io"
	stdlog "log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	collogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"
)

func TestValidateConfig(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cfg, err := ValidateConfig(Config{ServiceName: "bridge-cli"})
		if err != nil {
			t.Fatalf("ValidateConfig: %v", err)
		}
		if cfg.Protocol != "grpc" {
			t.Fatalf("expected default protocol grpc, got %q", cfg.Protocol)
		}
		if cfg.LocalOutput != "text" {
			t.Fatalf("expected default output text, got %q", cfg.LocalOutput)
		}
		if cfg.LocalWriter == nil {
			t.Fatalf("expected default local writer")
		}
	})

	t.Run("invalid protocol", func(t *testing.T) {
		_, err := ValidateConfig(Config{ServiceName: "bridge-cli", Protocol: "udp"})
		if err == nil {
			t.Fatalf("expected protocol validation error")
		}
	})

	t.Run("invalid local output", func(t *testing.T) {
		_, err := ValidateConfig(Config{ServiceName: "bridge-cli", LocalOutput: "json"})
		if err == nil {
			t.Fatalf("expected local output validation error")
		}
	})

	t.Run("missing service name", func(t *testing.T) {
		_, err := ValidateConfig(Config{})
		if err == nil {
			t.Fatalf("expected service name validation error")
		}
	})
}

func TestParseBodyAttributesAndSeverity(t *testing.T) {
	body := `event=ui_request method=GET path="/state?token=REDACTED" status=204 duration_ms=13 remote="127.0.0.1:1234"`
	attrs, values := parseBodyAttributes(body)
	if len(attrs) == 0 {
		t.Fatalf("expected parsed attributes")
	}
	if values["event"] != "ui_request" {
		t.Fatalf("expected event parsed, got %q", values["event"])
	}
	if values["path"] != "/state?token=REDACTED" {
		t.Fatalf("expected quoted value decode, got %q", values["path"])
	}
	if values["status"] != "204" {
		t.Fatalf("expected numeric value preserved, got %q", values["status"])
	}

	if sev := inferSeverity(`event=publish_failed err=boom`, map[string]string{"event": "publish_failed", "err": "boom"}); sev != 17 {
		t.Fatalf("expected error severity, got %v", sev)
	}
	if sev := inferSeverity("Connecting to firehose", map[string]string{}); sev != 9 {
		t.Fatalf("expected info severity, got %v", sev)
	}
}

func TestRuntimeOTLPHTTPExport(t *testing.T) {
	var reqCh = make(chan *collogsv1.ExportLogsServiceRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/logs" {
			t.Errorf("expected /v1/logs, got %s", r.URL.Path)
		}
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		var req collogsv1.ExportLogsServiceRequest
		if err := proto.Unmarshal(payload, &req); err != nil {
			t.Errorf("unmarshal export request: %v", err)
		}
		select {
		case reqCh <- &req:
		default:
		}

		respBytes, _ := proto.Marshal(&collogsv1.ExportLogsServiceResponse{})
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(respBytes)
	}))
	defer server.Close()

	endpoint := endpointHostPort(t, server.URL)
	var local bytes.Buffer
	rt, err := NewRuntime(Config{
		Endpoint:    endpoint,
		Protocol:    "http",
		Insecure:    true,
		ServiceName: "bridge-cli",
		CommandName: "start",
		LocalOutput: "text",
		LocalWriter: &local,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	logger := rt.Logger("bridge")
	logger.Printf("event=published did=%s seq=%d", "did:plc:abc", 7)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rt.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case req := <-reqCh:
		record := findLogRecordByBody(req, "event=published")
		if record == nil {
			t.Fatalf("expected exported record with event=published")
		}
		if got := record.GetBody().GetStringValue(); !strings.Contains(got, "event=published") {
			t.Fatalf("expected log body to contain event, got %q", got)
		}
		attrs := flattenAttrs(record.GetAttributes())
		if attrs["event"] != "published" {
			t.Fatalf("expected event attribute, got %q (body=%q attrs=%v)", attrs["event"], record.GetBody().GetStringValue(), attrs)
		}
		if attrs["component"] != "bridge" {
			t.Fatalf("expected component=bridge, got %q", attrs["component"])
		}
		if attrs["command"] != "start" {
			t.Fatalf("expected command=start, got %q", attrs["command"])
		}
		if attrs["seq"] != "7" {
			t.Fatalf("expected seq attr value 7, got %q", attrs["seq"])
		}
		if got := local.String(); !strings.Contains(got, "event=published") {
			t.Fatalf("expected local output to retain log line, got %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for OTLP export")
	}
}

func TestRuntimeFailOpenWhenEndpointUnavailable(t *testing.T) {
	var local bytes.Buffer
	rt, err := NewRuntime(Config{
		Endpoint:    "127.0.0.1:1",
		Protocol:    "http",
		Insecure:    true,
		ServiceName: "bridge-cli",
		CommandName: "start",
		LocalOutput: "text",
		LocalWriter: &local,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	logger := rt.Logger("bridge")
	logger.Printf("event=runtime_live status=%s", "ok")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = rt.Shutdown(ctx)

	if got := local.String(); !strings.Contains(got, "event=runtime_live") {
		t.Fatalf("expected local output despite endpoint failure, got %q", got)
	}
}

func TestRuntimeFailOpenWhenExporterInitFails(t *testing.T) {
	var local bytes.Buffer
	rt, err := NewRuntime(Config{
		Endpoint:    "bad\nhost:4318",
		Protocol:    "http",
		Insecure:    true,
		ServiceName: "bridge-cli",
		CommandName: "start",
		LocalOutput: "text",
		LocalWriter: &local,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	logger := rt.Logger("bridge")
	logger.Printf("event=runtime_started")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = rt.Shutdown(ctx)

	out := local.String()
	if !strings.Contains(out, "otel_logs_exporter_init_failed") {
		t.Fatalf("expected exporter init warning, got %q", out)
	}
	if !strings.Contains(out, "event=runtime_started") {
		t.Fatalf("expected local log line after init failure, got %q", out)
	}
}

func TestEnsure(t *testing.T) {
	if l := Ensure(nil); l == nil {
		t.Fatal("expected non-nil logger for nil input")
	}
	custom := stdlog.New(io.Discard, "test: ", 0)
	if l := Ensure(custom); l != custom {
		t.Fatal("expected same logger returned")
	}
}

func TestNewTextLogger(t *testing.T) {
	l := NewTextLogger("")
	if l == nil {
		t.Fatal("expected non-nil logger")
	}
	l2 := NewTextLogger("mycomp")
	if l2 == nil {
		t.Fatal("expected non-nil logger for component")
	}
}

func TestLoggerEmptyComponent(t *testing.T) {
	var buf bytes.Buffer
	rt, _ := NewRuntime(Config{ServiceName: "test", LocalOutput: "text", LocalWriter: &buf})
	l := rt.Logger("")
	l.Printf("event=test_msg")
	if !strings.Contains(buf.String(), "app:") {
		t.Fatalf("expected default 'app' component, got %q", buf.String())
	}
}

func TestRuntimeNoEndpoint(t *testing.T) {
	var buf bytes.Buffer
	rt, err := NewRuntime(Config{ServiceName: "test", LocalOutput: "text", LocalWriter: &buf})
	if err != nil {
		t.Fatal(err)
	}
	// No OTLP endpoint — shutdown should be no-op
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownNilRuntime(t *testing.T) {
	var rt *Runtime
	if err := rt.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeWriterNilRuntime(t *testing.T) {
	w := &runtimeWriter{rt: nil}
	n, err := w.Write([]byte("test"))
	if err != nil || n != 4 {
		t.Fatalf("expected (4, nil), got (%d, %v)", n, err)
	}
}

func TestRuntimeLocalOutputNone(t *testing.T) {
	var buf bytes.Buffer
	rt, err := NewRuntime(Config{ServiceName: "test", LocalOutput: "none", LocalWriter: &buf})
	if err != nil {
		t.Fatal(err)
	}
	l := rt.Logger("test")
	l.Printf("event=should_not_appear")
	if buf.String() != "" {
		t.Fatalf("expected no local output with 'none', got %q", buf.String())
	}
}

func TestWarnfSilentWhenNone(t *testing.T) {
	var buf bytes.Buffer
	rt := &Runtime{localOutput: "none", localWriter: &buf}
	rt.warnf("test %s", "warning")
	if buf.String() != "" {
		t.Fatal("expected no output from warnf when localOutput=none")
	}
}

func TestWarnfSilentWhenNilWriter(t *testing.T) {
	rt := &Runtime{localOutput: "text", localWriter: nil}
	rt.warnf("test %s", "warning") // should not panic
}

func TestStripStdPrefixEmptyLine(t *testing.T) {
	ts, body := stripStdPrefix("", "prefix: ")
	if !ts.IsZero() || body != "" {
		t.Fatalf("expected zero time and empty body, got %v, %q", ts, body)
	}
}

func TestStripStdPrefixWithTimestampAndPrefix(t *testing.T) {
	// Timestamp present, prefix after timestamp
	line := "2025/03/28 12:00:00 bridge: event=test"
	ts, body := stripStdPrefix(line, "bridge: ")
	if ts.IsZero() {
		t.Fatal("expected non-zero timestamp")
	}
	if body != "event=test" {
		t.Fatalf("expected body after prefix strip, got %q", body)
	}
}

func TestStripTimestampPrefixShortLine(t *testing.T) {
	ts, rest, ok := stripTimestampPrefix("short")
	if ok || !ts.IsZero() || rest != "short" {
		t.Fatal("expected no match for short line")
	}
}

func TestStripTimestampPrefixInvalid(t *testing.T) {
	ts, rest, ok := stripTimestampPrefix("not-a-timestamp-at-all!!")
	if ok || !ts.IsZero() || rest != "not-a-timestamp-at-all!!" {
		t.Fatal("expected no match for invalid timestamp")
	}
}

func TestParsePairsEdgeCases(t *testing.T) {
	// Empty string
	if pairs := parsePairs(""); len(pairs) != 0 {
		t.Fatalf("expected 0 pairs for empty, got %d", len(pairs))
	}

	// Key without equals
	if pairs := parsePairs("justkey"); len(pairs) != 0 {
		t.Fatalf("expected 0 pairs for bare key, got %d", len(pairs))
	}

	// Key with space before equals (space terminates key)
	if pairs := parsePairs("key =value"); len(pairs) != 0 {
		t.Fatalf("expected 0 pairs when space before =, got %d", len(pairs))
	}

	// Empty key (=value)
	if pairs := parsePairs("=value"); len(pairs) != 0 {
		t.Fatalf("expected 0 pairs for empty key, got %d", len(pairs))
	}

	// Quoted value with escape
	pairs := parsePairs(`msg="hello \"world\""`)
	if len(pairs) != 1 || pairs[0].value != `hello "world"` {
		t.Fatalf("expected unescaped quoted value, got %v", pairs)
	}

	// Unterminated quote
	if pairs := parsePairs(`msg="unterminated`); len(pairs) != 0 {
		t.Fatalf("expected 0 pairs for unterminated quote, got %d", len(pairs))
	}

	// Invalid unquote (edge case)
	pairs = parsePairs(`msg="bad\qescape"`)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair with fallback decode, got %d", len(pairs))
	}
}

func TestInferSeverityErrorEvent(t *testing.T) {
	if sev := inferSeverity("event=db_error", map[string]string{"event": "db_error"}); sev != 17 {
		t.Fatalf("expected error severity for _error event suffix, got %v", sev)
	}
}

func TestToLogKeyValue(t *testing.T) {
	// Bool
	kv := toLogKeyValue("flag", "true")
	if kv.Key != "flag" {
		t.Fatal("expected key=flag")
	}

	// Int
	kv = toLogKeyValue("count", "42")
	if kv.Key != "count" {
		t.Fatal("expected key=count")
	}

	// Float
	kv = toLogKeyValue("rate", "3.14")
	if kv.Key != "rate" {
		t.Fatal("expected key=rate")
	}

	// String
	kv = toLogKeyValue("name", "hello")
	if kv.Key != "name" {
		t.Fatal("expected key=name")
	}
}

func TestEmitEmptyBody(t *testing.T) {
	var buf bytes.Buffer
	rt, _ := NewRuntime(Config{
		Endpoint:    "127.0.0.1:1",
		Protocol:    "http",
		Insecure:    true,
		ServiceName: "test",
		LocalOutput: "text",
		LocalWriter: &buf,
	})
	// emit with empty body should be a no-op (after stripping)
	rt.emit("comp", "comp: ", []byte("comp: \n"))
}

func TestNewOTLPExporterGRPCInsecure(t *testing.T) {
	_, err := newOTLPExporter(Config{Protocol: "grpc", Endpoint: "127.0.0.1:4317", Insecure: true})
	if err != nil {
		t.Fatalf("expected no error for grpc insecure, got %v", err)
	}
}

func TestNewOTLPExporterHTTPSecure(t *testing.T) {
	_, err := newOTLPExporter(Config{Protocol: "http", Endpoint: "127.0.0.1:4318"})
	if err != nil {
		t.Fatalf("expected no error for http secure, got %v", err)
	}
}

func TestParsePairsMoreEdgeCases(t *testing.T) {
	// Trailing spaces
	if pairs := parsePairs("key=val  "); len(pairs) != 1 || pairs[0].value != "val" {
		t.Fatalf("expected 1 pair for trailing spaces, got %v", pairs)
	}
	// Key with no value at end
	if pairs := parsePairs("key="); len(pairs) != 0 {
		t.Fatalf("expected 0 pairs for empty key= at end, got %v", pairs)
	}
}

func TestRuntimeNewRuntimeValidationError(t *testing.T) {
	_, err := NewRuntime(Config{}) // missing ServiceName
	if err == nil {
		t.Fatal("expected error from ValidateConfig")
	}
}

func TestRuntimeShutdownNilProvider(t *testing.T) {
	rt := &Runtime{}
	err := rt.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("expected nil error for nil provider, got %v", err)
	}
}

func TestValidateConfigMoreErrors(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"invalid protocol", Config{Protocol: "ftp", ServiceName: "s"}},
		{"invalid output", Config{LocalOutput: "pdf", ServiceName: "s"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateConfig(tt.cfg)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

func endpointHostPort(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return u.Host
}

func findLogRecordByBody(req *collogsv1.ExportLogsServiceRequest, contains string) *logsv1.LogRecord {
	if req == nil {
		return nil
	}
	for _, resourceLogs := range req.ResourceLogs {
		for _, scopeLogs := range resourceLogs.ScopeLogs {
			for _, record := range scopeLogs.LogRecords {
				if strings.Contains(record.GetBody().GetStringValue(), contains) {
					return record
				}
			}
		}
	}
	return nil
}

func flattenAttrs(attrs []*commonv1.KeyValue) map[string]string {
	out := make(map[string]string, len(attrs))
	for _, kv := range attrs {
		if kv == nil {
			continue
		}
		out[kv.Key] = anyValueToString(kv.Value)
	}
	return out
}

func anyValueToString(v *commonv1.AnyValue) string {
	if v == nil {
		return ""
	}
	switch val := v.Value.(type) {
	case *commonv1.AnyValue_StringValue:
		return val.StringValue
	case *commonv1.AnyValue_BoolValue:
		if val.BoolValue {
			return "true"
		}
		return "false"
	case *commonv1.AnyValue_IntValue:
		return strconv.FormatInt(val.IntValue, 10)
	case *commonv1.AnyValue_DoubleValue:
		return strconv.FormatFloat(val.DoubleValue, 'f', -1, 64)
	default:
		return ""
	}
}
