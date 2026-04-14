package main

import (
	"context"
	"io"
	"log"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/backfill"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
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
	pdsResolver, err := resolveLivePDSHostResolver("https://example.com", "https://plc.directory", false)
	if err != nil {
		t.Fatalf("explicit host should not error: %v", err)
	}
	if _, ok := pdsResolver.(backfill.FixedHostResolver); !ok {
		t.Fatalf("explicit host should return FixedHostResolver, got %T", pdsResolver)
	}

	pdsResolver, err = resolveLivePDSHostResolver("", "https://plc.directory", false)
	if err != nil {
		t.Fatalf("empty host should not error: %v", err)
	}
	if _, ok := pdsResolver.(backfill.DIDPDSResolver); !ok {
		t.Fatalf("empty host should return DIDPDSResolver, got %T", pdsResolver)
	}

	// Test with explicit host
	resolver, err := resolveLiveBlobHostResolver("https://example.com", "https://plc.directory", false)
	if err != nil {
		t.Errorf("explicit host should not error: %v", err)
	}
	if resolver == nil {
		t.Error("explicit_host should return non-nil resolver")
	}
	if _, ok := resolver.(backfill.FixedHostResolver); !ok {
		t.Errorf("explicit host should return FixedHostResolver, got %T", resolver)
	}

	// Test with empty host (should use DIDPDSResolver)
	resolver, err = resolveLiveBlobHostResolver("", "https://plc.directory", false)
	if err != nil {
		t.Errorf("empty host should not error: %v", err)
	}
	if resolver == nil {
		t.Error("empty host should return non-nil resolver")
	}
	if _, ok := resolver.(backfill.DIDPDSResolver); !ok {
		t.Errorf("empty host should return DIDPDSResolver, got %T", resolver)
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

type mockFeedReplicator struct {
	feeds []string
}

func (m *mockFeedReplicator) Replicate(feed interface{}) {
	switch v := feed.(type) {
	case refs.FeedRef:
		m.feeds = append(m.feeds, v.String())
	case *refs.FeedRef:
		if v != nil {
			m.feeds = append(m.feeds, v.String())
		}
	case string:
		m.feeds = append(m.feeds, v)
	}
}

func TestSyncReverseReplicatedFeeds(t *testing.T) {
	ctx := context.Background()
	database := setupTestDB(t)

	activeFeed := refs.MustNewFeedRef(testFeedBytes(1), refs.RefAlgoFeedSSB1).String()
	inactiveFeed := refs.MustNewFeedRef(testFeedBytes(2), refs.RefAlgoFeedSSB1).String()
	noActionFeed := refs.MustNewFeedRef(testFeedBytes(3), refs.RefAlgoFeedSSB1).String()

	if err := database.AddReverseIdentityMapping(ctx, db.ReverseIdentityMapping{
		SSBFeedID:    activeFeed,
		ATDID:        "did:plc:active",
		Active:       true,
		AllowPosts:   true,
		AllowReplies: true,
		AllowFollows: true,
	}); err != nil {
		t.Fatalf("add active mapping: %v", err)
	}
	if err := database.AddReverseIdentityMapping(ctx, db.ReverseIdentityMapping{
		SSBFeedID:    inactiveFeed,
		ATDID:        "did:plc:inactive",
		Active:       false,
		AllowPosts:   true,
		AllowReplies: true,
		AllowFollows: true,
	}); err != nil {
		t.Fatalf("add inactive mapping: %v", err)
	}
	if err := database.AddReverseIdentityMapping(ctx, db.ReverseIdentityMapping{
		SSBFeedID:    noActionFeed,
		ATDID:        "did:plc:noaction",
		Active:       true,
		AllowPosts:   false,
		AllowReplies: false,
		AllowFollows: false,
	}); err != nil {
		t.Fatalf("add no-action mapping: %v", err)
	}

	replicator := &mockFeedReplicator{}
	syncReverseReplicatedFeeds(ctx, database, replicator, log.New(io.Discard, "", 0))

	if len(replicator.feeds) != 1 {
		t.Fatalf("expected 1 replicated feed, got %d (%v)", len(replicator.feeds), replicator.feeds)
	}
	if replicator.feeds[0] != activeFeed {
		t.Fatalf("expected active mapped feed %s, got %s", activeFeed, replicator.feeds[0])
	}
}

func testFeedBytes(fill byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = fill
	}
	return out
}
