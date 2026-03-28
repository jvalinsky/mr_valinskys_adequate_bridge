package logutil

import (
	"bytes"
	"context"
	"io"
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
