// Package logutil provides logging helpers shared across bridge packages.
package logutil

import (
	"context"
	"fmt"
	"io"
	stdlog "log"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otlploggrpc "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	otlploghttp "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

const (
	defaultProtocol = "grpc"
	defaultOutput   = "text"
)

// Config controls logger runtime initialization.
type Config struct {
	Endpoint    string
	Protocol    string
	Insecure    bool
	ServiceName string
	CommandName string
	LocalOutput string
	LocalWriter io.Writer
}

// Runtime provides component-scoped stdlib loggers and optional OTLP export.
type Runtime struct {
	provider    *sdklog.LoggerProvider
	emitter     otellog.Logger
	commandName string

	localOutput string
	localWriter io.Writer

	mu sync.Mutex
}

// ValidateConfig validates and normalizes a logging config.
func ValidateConfig(cfg Config) (Config, error) {
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.ServiceName = strings.TrimSpace(cfg.ServiceName)
	cfg.CommandName = strings.TrimSpace(cfg.CommandName)
	cfg.Protocol = strings.ToLower(strings.TrimSpace(cfg.Protocol))
	cfg.LocalOutput = strings.ToLower(strings.TrimSpace(cfg.LocalOutput))

	if cfg.Protocol == "" {
		cfg.Protocol = defaultProtocol
	}
	if cfg.Protocol != "grpc" && cfg.Protocol != "http" {
		return cfg, fmt.Errorf("invalid otel logs protocol %q: expected grpc or http", cfg.Protocol)
	}

	if cfg.LocalOutput == "" {
		cfg.LocalOutput = defaultOutput
	}
	if cfg.LocalOutput != "text" && cfg.LocalOutput != "none" {
		return cfg, fmt.Errorf("invalid local log output %q: expected text or none", cfg.LocalOutput)
	}

	if cfg.ServiceName == "" {
		return cfg, fmt.Errorf("otel service name must not be empty")
	}

	if cfg.LocalWriter == nil {
		cfg.LocalWriter = os.Stdout
	}

	return cfg, nil
}

// NewRuntime creates a runtime that emits OTLP logs when configured.
// OTLP exporter setup failures are fail-open: local logging continues.
func NewRuntime(cfg Config) (*Runtime, error) {
	cfg, err := ValidateConfig(cfg)
	if err != nil {
		return nil, err
	}

	rt := &Runtime{
		commandName: cfg.CommandName,
		localOutput: cfg.LocalOutput,
		localWriter: cfg.LocalWriter,
	}

	if cfg.Endpoint == "" {
		return rt, nil
	}

	exporter, err := newOTLPExporter(cfg)
	if err != nil {
		rt.warnf("event=otel_logs_exporter_init_failed endpoint=%q protocol=%s err=%v", cfg.Endpoint, cfg.Protocol, err)
		return rt, nil
	}

	resAttrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceInstanceID(serviceInstanceID()),
	}
	if v := buildVersion(); v != "" {
		resAttrs = append(resAttrs, semconv.ServiceVersion(v))
	}

	res, err := resource.New(context.Background(), resource.WithAttributes(resAttrs...))
	if err != nil {
		rt.warnf("event=otel_logs_resource_init_failed err=%v", err)
		return rt, nil
	}

	provider := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	)
	rt.provider = provider
	rt.emitter = provider.Logger("github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil")

	return rt, nil
}

func newOTLPExporter(cfg Config) (sdklog.Exporter, error) {
	ctx := context.Background()
	if cfg.Protocol == "http" {
		opts := []otlploghttp.Option{otlploghttp.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			opts = append(opts, otlploghttp.WithInsecure())
		}
		return otlploghttp.New(ctx, opts...)
	}

	opts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(cfg.Endpoint)}
	if cfg.Insecure {
		opts = append(opts, otlploggrpc.WithInsecure())
	}
	return otlploggrpc.New(ctx, opts...)
}

// Logger creates a component logger that preserves existing stdlib formatting.
func (r *Runtime) Logger(component string) *stdlog.Logger {
	component = strings.TrimSpace(component)
	if component == "" {
		component = "app"
	}
	prefix := component + ": "
	return stdlog.New(&runtimeWriter{rt: r, component: component, prefix: prefix}, prefix, stdlog.LstdFlags)
}

// Shutdown flushes and shuts down OTLP exporter state.
func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil || r.provider == nil {
		return nil
	}
	return r.provider.Shutdown(ctx)
}

func (r *Runtime) warnf(format string, args ...interface{}) {
	if r == nil || r.localOutput != "text" || r.localWriter == nil {
		return
	}
	line := fmt.Sprintf("logutil: "+format+"\n", args...)
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = io.WriteString(r.localWriter, line)
}

// NewTextLogger creates a plain text stdout logger for fallback paths.
func NewTextLogger(component string) *stdlog.Logger {
	component = strings.TrimSpace(component)
	if component == "" {
		component = "app"
	}
	return stdlog.New(os.Stdout, component+": ", stdlog.LstdFlags)
}

type runtimeWriter struct {
	rt        *Runtime
	component string
	prefix    string
}

func (w *runtimeWriter) Write(p []byte) (int, error) {
	if w.rt == nil {
		return len(p), nil
	}
	if w.rt.localOutput == "text" && w.rt.localWriter != nil {
		w.rt.mu.Lock()
		_, _ = w.rt.localWriter.Write(p)
		w.rt.mu.Unlock()
	}
	if w.rt.emitter != nil {
		w.rt.emit(w.component, w.prefix, p)
	}
	return len(p), nil
}

func (r *Runtime) emit(component, prefix string, payload []byte) {
	rawLine := strings.TrimRight(string(payload), "\r\n")
	timestamp, body := stripStdPrefix(rawLine, prefix)
	if strings.TrimSpace(body) == "" {
		return
	}

	attrs, kv := parseBodyAttributes(body)
	attrs = append(attrs, otellog.String("component", component))
	if r.commandName != "" {
		attrs = append(attrs, otellog.String("command", r.commandName))
	}

	var record otellog.Record
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	record.SetTimestamp(timestamp)
	record.SetObservedTimestamp(time.Now())

	severity := inferSeverity(body, kv)
	record.SetSeverity(severity)
	record.SetSeverityText(severity.String())
	record.SetBody(otellog.StringValue(body))
	if ev := strings.TrimSpace(kv["event"]); ev != "" {
		record.SetEventName(ev)
	}
	if len(attrs) > 0 {
		record.AddAttributes(attrs...)
	}
	r.emitter.Emit(context.Background(), record)
}

func stripStdPrefix(line, prefix string) (time.Time, string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return time.Time{}, ""
	}

	prefixStripped := false
	if strings.HasPrefix(line, prefix) {
		line = strings.TrimSpace(line[len(prefix):])
		prefixStripped = true
	}

	if ts, rest, ok := stripTimestampPrefix(line); ok {
		line = rest
		if !prefixStripped && strings.HasPrefix(line, prefix) {
			line = strings.TrimSpace(line[len(prefix):])
		}
		return ts, line
	}

	return time.Time{}, line
}

func stripTimestampPrefix(line string) (time.Time, string, bool) {
	const layout = "2006/01/02 15:04:05"
	if len(line) < len(layout) {
		return time.Time{}, line, false
	}
	candidate := line[:len(layout)]
	ts, err := time.ParseInLocation(layout, candidate, time.Local)
	if err != nil {
		return time.Time{}, line, false
	}
	rest := strings.TrimSpace(line[len(layout):])
	return ts, rest, true
}

func parseBodyAttributes(body string) ([]otellog.KeyValue, map[string]string) {
	pairs := parsePairs(body)
	if len(pairs) == 0 {
		return nil, map[string]string{}
	}
	attrs := make([]otellog.KeyValue, 0, len(pairs))
	values := make(map[string]string, len(pairs))
	for _, p := range pairs {
		values[p.key] = p.value
		attrs = append(attrs, toLogKeyValue(p.key, p.value))
	}
	return attrs, values
}

type pair struct {
	key   string
	value string
}

func parsePairs(s string) []pair {
	var out []pair
	i := 0
	for i < len(s) {
		for i < len(s) && s[i] == ' ' {
			i++
		}
		if i >= len(s) {
			break
		}

		keyStart := i
		for i < len(s) && s[i] != '=' && s[i] != ' ' {
			i++
		}
		if i >= len(s) || s[i] != '=' {
			break
		}
		key := strings.TrimSpace(s[keyStart:i])
		i++

		if key == "" {
			break
		}

		if i < len(s) && s[i] == '"' {
			j := i + 1
			escaped := false
			for j < len(s) {
				if s[j] == '\\' {
					escaped = !escaped
					j++
					continue
				}
				if s[j] == '"' && !escaped {
					break
				}
				escaped = false
				j++
			}
			if j >= len(s) {
				break
			}
			raw := s[i : j+1]
			decoded, err := strconv.Unquote(raw)
			if err != nil {
				decoded = strings.Trim(raw, "\"")
			}
			out = append(out, pair{key: key, value: decoded})
			i = j + 1
			continue
		}

		valueStart := i
		for i < len(s) && s[i] != ' ' {
			i++
		}
		value := strings.TrimSpace(s[valueStart:i])
		if value != "" || (i < len(s) && s[i] == ' ') {
			out = append(out, pair{key: key, value: value})
		}
	}
	return out
}

func inferSeverity(body string, attrs map[string]string) otellog.Severity {
	if errValue := strings.TrimSpace(attrs["err"]); errValue != "" {
		return otellog.SeverityError
	}
	event := strings.TrimSpace(attrs["event"])
	if strings.HasSuffix(event, "_failed") || strings.HasSuffix(event, "_error") {
		return otellog.SeverityError
	}
	return otellog.SeverityInfo
}

func toLogKeyValue(key, value string) otellog.KeyValue {
	value = strings.TrimSpace(value)
	if v, err := strconv.ParseBool(value); err == nil {
		return otellog.Bool(key, v)
	}
	if v, err := strconv.ParseInt(value, 10, 64); err == nil {
		return otellog.Int64(key, v)
	}
	if v, err := strconv.ParseFloat(value, 64); err == nil {
		return otellog.Float64(key, v)
	}
	return otellog.String(key, value)
}

func serviceInstanceID() string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

func buildVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	v := strings.TrimSpace(bi.Main.Version)
	if v == "" || v == "(devel)" {
		return ""
	}
	return v
}
