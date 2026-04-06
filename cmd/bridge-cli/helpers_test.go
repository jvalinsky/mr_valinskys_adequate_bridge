package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
)

func testContext() context.Context {
	return context.Background()
}

func setupTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return database
}

func TestParseSlogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"unknown", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"  debug  ", slog.LevelDebug},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseSlogLevel(tt.input)
			if result != tt.expected {
				t.Errorf("parseSlogLevel(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseHMACKey(t *testing.T) {
	// Test empty input
	key, err := parseHMACKey("")
	if err != nil {
		t.Errorf("empty key should not error: %v", err)
	}
	if key != nil {
		t.Error("empty key should return nil")
	}

	// Test whitespace-only input
	key, err = parseHMACKey("   ")
	if err != nil {
		t.Errorf("whitespace key should not error: %v", err)
	}
	if key != nil {
		t.Error("whitespace key should return nil")
	}

	// Test valid base64 32-byte key
	validBase64 := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	key, err = parseHMACKey(validBase64)
	if err != nil {
		t.Errorf("valid base64 key should not error: %v", err)
	}
	if key == nil {
		t.Error("valid base64 key should return non-nil")
	}

	// Test valid hex 32-byte key (64 hex chars)
	validHex := "0000000000000000000000000000000000000000000000000000000000000000"
	key, err = parseHMACKey(validHex)
	if err != nil {
		t.Errorf("valid hex key should not error: %v", err)
	}
	if key == nil {
		t.Error("valid hex key should return non-nil")
	}

	// Test invalid length (not 32 bytes)
	invalidKey := "tooshort"
	key, err = parseHMACKey(invalidKey)
	if err == nil {
		t.Error("invalid length key should error")
	}
	if key != nil {
		t.Error("invalid length key should return nil")
	}
}

func TestFallbackValue(t *testing.T) {
	tests := []struct {
		value    string
		fallback string
		expected string
	}{
		{"hello", "default", "hello"},
		{"", "default", "default"},
		{"   ", "default", "default"},
		{"  value  ", "default", "  value  "},
	}

	for _, tt := range tests {
		t.Run(tt.value+"|"+tt.fallback, func(t *testing.T) {
			result := fallbackValue(tt.value, tt.fallback)
			if result != tt.expected {
				t.Errorf("fallbackValue(%q, %q) = %q, want %q", tt.value, tt.fallback, result, tt.expected)
			}
		})
	}
}

func TestDedupeStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "no duplicates",
			input:    []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "with duplicates",
			input:    []string{"a", "b", "a", "c", "b"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "with whitespace",
			input:    []string{"  a  ", "a", "  b"},
			expected: []string{"a", "b"},
		},
		{
			name:     "with empty strings",
			input:    []string{"a", "", "b", "", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "only whitespace and empty",
			input:    []string{"", "  ", "   "},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dedupeStrings(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("dedupeStrings(%v) length = %d, want %d", tt.input, len(result), len(tt.expected))
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("dedupeStrings(%v)[%d] = %q, want %q", tt.input, i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestResolveLiveBlobHostResolver(t *testing.T) {
	// Test with explicit host
	resolver, err := resolveLiveBlobHostResolver("https://example.com", "https://plc.directory", false)
	if err != nil {
		t.Errorf("explicit host should not error: %v", err)
	}
	if resolver == nil {
		t.Error("explicit_host should return non-nil resolver")
	}

	// Test with empty host (should use DIDPDSResolver)
	resolver, err = resolveLiveBlobHostResolver("", "https://plc.directory", false)
	if err != nil {
		t.Errorf("empty host should not error: %v", err)
	}
	if resolver == nil {
		t.Error("empty host should return non-nil resolver")
	}

	// Test with insecure flag
	resolver, err = resolveLiveBlobHostResolver("", "https://plc.directory", true)
	if err != nil {
		t.Errorf("insecure flag should not error: %v", err)
	}
	if resolver == nil {
		t.Error("insecure flag should return non-nil resolver")
	}
}

func TestReadFirehoseCursor(t *testing.T) {
	ctx := testContext()

	// Test with empty database (no cursor set)
	database := setupTestDB(t)
	seq, ok, err := readFirehoseCursor(ctx, database)
	if err != nil {
		t.Errorf("empty db should not error: %v", err)
	}
	if ok {
		t.Error("empty db should return ok=false")
	}
	if seq != 0 {
		t.Errorf("empty db should return seq=0, got %d", seq)
	}
}
