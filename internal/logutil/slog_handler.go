package logutil

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"
)

// otlpSlogHandler implements slog.Handler, converting slog.Record directly to
// structured key-value output. Also writes to local output.
type otlpSlogHandler struct {
	level slog.Leveler
	local io.Writer
	attrs []slog.Attr
	group string
}

// Enabled reports whether the handler handles records at the given level.
func (h *otlpSlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.level != nil {
		minLevel = h.level.Level()
	}
	return level >= minLevel
}

// Handle converts slog.Record to structured output and writes to local output.
func (h *otlpSlogHandler) Handle(ctx context.Context, r slog.Record) error {
	// Skip if below minimum level
	if !h.Enabled(ctx, r.Level) {
		return nil
	}

	// Build attributes from record attrs + handler attrs
	attrs := append([]slog.Attr{}, h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})

	// Convert slog.Attr to key=value map
	kvMap := attrsToMap(attrs)

	// Write to local output if configured
	if h.local != nil {
		timestamp := r.Time.Format(time.RFC3339)
		level := r.Level.String()
		msg := r.Message
		var kvStr string
		for k, v := range kvMap {
			kvStr += fmt.Sprintf(" %s=%v", k, v)
		}
		fmt.Fprintf(h.local, "%s %s %s%s\n", timestamp, level, msg, kvStr)
	}

	return nil
}

// WithAttrs returns a new handler with the given attributes added.
func (h *otlpSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := append([]slog.Attr{}, h.attrs...)
	newAttrs = append(newAttrs, attrs...)
	return &otlpSlogHandler{
		level: h.level,
		local: h.local,
		attrs: newAttrs,
		group: h.group,
	}
}

// WithGroup returns a new handler with the given group added to the group path.
func (h *otlpSlogHandler) WithGroup(name string) slog.Handler {
	newGroup := h.group
	if newGroup != "" {
		newGroup += "."
	}
	newGroup += name
	return &otlpSlogHandler{
		level: h.level,
		local: h.local,
		attrs: h.attrs,
		group: newGroup,
	}
}

// attrsToMap converts slog.Attr slice to a string key-value map.
func attrsToMap(attrs []slog.Attr) map[string]interface{} {
	m := make(map[string]interface{})
	for _, a := range attrs {
		m[a.Key] = a.Value.Any()
	}
	return m
}
