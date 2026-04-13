package bridge

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
)

// ---------- utility function tests ----------

func TestExtractReplyRefsFromFlatContent(t *testing.T) {
	content := map[string]any{
		"root":   "%root.sha256",
		"branch": "%parent.sha256",
	}
	root, parent := extractReplyRefs(content)
	if root != "%root.sha256" {
		t.Errorf("expected root=%%root.sha256, got %q", root)
	}
	if parent != "%parent.sha256" {
		t.Errorf("expected parent=%%parent.sha256, got %q", parent)
	}
}

func TestExtractReplyRefsFromTangles(t *testing.T) {
	content := map[string]any{
		"tangles": map[string]any{
			"comment": map[string]any{
				"root":     "%tangle-root.sha256",
				"previous": []any{"%tangle-prev1.sha256", "%tangle-prev2.sha256"},
			},
		},
	}
	root, parent := extractReplyRefs(content)
	if root != "%tangle-root.sha256" {
		t.Errorf("expected root from tangles, got %q", root)
	}
	if parent != "%tangle-prev2.sha256" {
		t.Errorf("expected parent from tangles previous (last), got %q", parent)
	}
}

func TestExtractReplyRefsTanglesPreviousAsStringSlice(t *testing.T) {
	content := map[string]any{
		"tangles": map[string]any{
			"comment": map[string]any{
				"root":     "%tangle-root.sha256",
				"previous": []string{"%prev1.sha256", "%prev2.sha256"},
			},
		},
	}
	root, parent := extractReplyRefs(content)
	if root != "%tangle-root.sha256" {
		t.Errorf("unexpected root: %q", root)
	}
	if parent != "%prev2.sha256" {
		t.Errorf("expected last string previous, got %q", parent)
	}
}

func TestExtractReplyRefsFallbackSymmetry(t *testing.T) {
	// When only root is set, parent should copy root
	content := map[string]any{"root": "%only-root.sha256"}
	root, parent := extractReplyRefs(content)
	if root != "%only-root.sha256" || parent != "%only-root.sha256" {
		t.Errorf("expected symmetry: root=%q parent=%q", root, parent)
	}

	// When only branch is set, root should copy parent
	content2 := map[string]any{"branch": "%only-branch.sha256"}
	root2, parent2 := extractReplyRefs(content2)
	if root2 != "%only-branch.sha256" || parent2 != "%only-branch.sha256" {
		t.Errorf("expected symmetry: root=%q parent=%q", root2, parent2)
	}
}

func TestExtractReplyRefsEmpty(t *testing.T) {
	root, parent := extractReplyRefs(map[string]any{"type": "post", "text": "hello"})
	if root != "" || parent != "" {
		t.Errorf("expected both empty: root=%q parent=%q", root, parent)
	}
}

func TestStringValue(t *testing.T) {
	tests := []struct {
		input    any
		expected string
	}{
		{"hello", "hello"},
		{nil, ""},
		{42, "42"},
		{3.14, "3.14"},
	}
	for _, tc := range tests {
		got := stringValue(tc.input)
		if got != tc.expected {
			t.Errorf("stringValue(%v): got %q want %q", tc.input, got, tc.expected)
		}
	}
}

func TestBoolValue(t *testing.T) {
	tests := []struct {
		input    any
		expected bool
	}{
		{true, true},
		{false, false},
		{"true", true},
		{"TRUE", true},
		{"  true  ", true},
		{"false", false},
		{nil, false},
		{42, false},
	}
	for _, tc := range tests {
		got := boolValue(tc.input)
		if got != tc.expected {
			t.Errorf("boolValue(%v): got %v want %v", tc.input, got, tc.expected)
		}
	}
}

func TestFloatValue(t *testing.T) {
	tests := []struct {
		input    any
		expected float64
	}{
		{float64(1.5), 1.5},
		{float32(2.5), 2.5},
		{int(3), 3.0},
		{int64(4), 4.0},
		{nil, 0},
		{"abc", 0},
	}
	for _, tc := range tests {
		got := floatValue(tc.input)
		if got != tc.expected {
			t.Errorf("floatValue(%v): got %v want %v", tc.input, got, tc.expected)
		}
	}
}

func TestIsHTTPURL(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"https://example.com", true},
		{"http://localhost:8080", true},
		{"HTTP://EXAMPLE.COM", true},
		{"  https://example.com  ", true},
		{"ftp://example.com", false},
		{"not a url", false},
		{"", false},
	}
	for _, tc := range tests {
		got := isHTTPURL(tc.input)
		if got != tc.expected {
			t.Errorf("isHTTPURL(%q): got %v want %v", tc.input, got, tc.expected)
		}
	}
}

func TestFirstNonBlank(t *testing.T) {
	if r := firstNonBlank("", "  ", "hello", "world"); r != "hello" {
		t.Errorf("expected 'hello', got %q", r)
	}
	if r := firstNonBlank("", "", ""); r != "" {
		t.Errorf("expected empty, got %q", r)
	}
	if r := firstNonBlank("first"); r != "first" {
		t.Errorf("expected 'first', got %q", r)
	}
}

func TestInt64Ptr(t *testing.T) {
	if p := int64Ptr(0); p != nil {
		t.Error("expected nil for 0")
	}
	if p := int64Ptr(-1); p != nil {
		t.Error("expected nil for -1")
	}
	if p := int64Ptr(5); p == nil || *p != 5 {
		t.Errorf("expected *p=5, got %v", p)
	}
}

func TestReverseCreatedAt(t *testing.T) {
	seq := int64(42)
	ts := reverseCreatedAt(&seq)
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Errorf("reverseCreatedAt returned invalid RFC3339: %q", ts)
	}
	ts2 := reverseCreatedAt(nil)
	if _, err := time.Parse(time.RFC3339, ts2); err != nil {
		t.Errorf("reverseCreatedAt(nil) returned invalid RFC3339: %q", ts2)
	}
}

// ---------- normalizeSSBContentMap tests ----------

func TestNormalizeSSBContentMapDirect(t *testing.T) {
	input := map[string]any{"type": "post", "text": "hello"}
	result, err := normalizeSSBContentMap(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["type"] != "post" {
		t.Errorf("unexpected type: %v", result["type"])
	}
}

func TestNormalizeSSBContentMapFromRawMessage(t *testing.T) {
	raw := json.RawMessage(`{"type":"vote"}`)
	result, err := normalizeSSBContentMap(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["type"] != "vote" {
		t.Errorf("unexpected type: %v", result["type"])
	}
}

func TestNormalizeSSBContentMapFromInterface(t *testing.T) {
	// Use an interface{} value that json.Marshal can handle
	type custom struct {
		Name string `json:"name"`
	}
	result, err := normalizeSSBContentMap(custom{Name: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["name"] != "test" {
		t.Errorf("unexpected name: %v", result["name"])
	}
}

// ---------- decodeSSBContent tests ----------

func TestDecodeSSBContentEmpty(t *testing.T) {
	_, err := decodeSSBContent(nil)
	if err == nil {
		t.Fatal("expected error for nil input")
	}
	_, err = decodeSSBContent([]byte("  "))
	if err == nil {
		t.Fatal("expected error for whitespace input")
	}
}

func TestDecodeSSBContentFlatJSON(t *testing.T) {
	result, err := decodeSSBContent([]byte(`{"type":"post","text":"hello"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["type"] != "post" {
		t.Errorf("unexpected type: %v", result["type"])
	}
}

func TestDecodeSSBContentEnvelope(t *testing.T) {
	result, err := decodeSSBContent([]byte(`{"content":{"type":"vote","vote":{"link":"%x.sha256","value":1}}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["type"] != "vote" {
		t.Errorf("unexpected type: %v", result["type"])
	}
}

func TestDecodeSSBContentInvalidJSON(t *testing.T) {
	_, err := decodeSSBContent([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid json")
	}
}

// ---------- normalizedReverseMentions tests ----------

func TestNormalizedReverseMentionsNil(t *testing.T) {
	if result := normalizedReverseMentions(nil); result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestNormalizedReverseMentionsDirectSlice(t *testing.T) {
	input := []map[string]any{{"link": "@a.ed25519"}}
	result := normalizedReverseMentions(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 mention, got %d", len(result))
	}
}

func TestNormalizedReverseMentionsAnySlice(t *testing.T) {
	input := []any{
		map[string]any{"link": "@a.ed25519"},
		"not a map",
		map[string]interface{}{"link": "@b.ed25519"},
	}
	result := normalizedReverseMentions(input)
	if len(result) != 2 {
		t.Fatalf("expected 2 mentions from []any, got %d", len(result))
	}
}

func TestNormalizedReverseMentionsUnsupported(t *testing.T) {
	if result := normalizedReverseMentions("unsupported"); result != nil {
		t.Errorf("expected nil for string input, got %v", result)
	}
}

// ---------- LoadReverseCredentials tests ----------

func TestLoadReverseCredentialsEmptyPath(t *testing.T) {
	creds, err := LoadReverseCredentials("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(creds) != 0 {
		t.Fatalf("expected empty map, got %v", creds)
	}
}

func TestLoadReverseCredentialsMissingFile(t *testing.T) {
	_, err := LoadReverseCredentials("/nonexistent/path.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadReverseCredentialsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadReverseCredentials(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadReverseCredentialsValid(t *testing.T) {
	content := `{
		"did:plc:alice": {
			"identifier": "alice.bsky.social",
			"pds_host": "https://bsky.social/",
			"password_env": "ALICE_PASSWORD"
		}
	}`
	path := filepath.Join(t.TempDir(), "creds.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	creds, err := LoadReverseCredentials(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry, ok := creds["did:plc:alice"]
	if !ok {
		t.Fatal("expected did:plc:alice in map")
	}
	if entry.Identifier != "alice.bsky.social" {
		t.Errorf("unexpected identifier: %q", entry.Identifier)
	}
	// PDSHost should have trailing slash trimmed
	if entry.PDSHost != "https://bsky.social" {
		t.Errorf("expected trailing slash trimmed, got %q", entry.PDSHost)
	}
}

func TestLoadReverseCredentialsNullJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "null.json")
	if err := os.WriteFile(path, []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	creds, err := LoadReverseCredentials(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(creds) != 0 {
		t.Fatalf("expected empty map for null JSON, got %v", creds)
	}
}

// ---------- ReverseProcessor.Enabled tests ----------

func TestReverseProcessorEnabledNilReceiver(t *testing.T) {
	var p *ReverseProcessor
	if p.Enabled() {
		t.Fatal("expected nil receiver to return false")
	}
}

func TestReverseProcessorEnabledFalse(t *testing.T) {
	p := NewReverseProcessor(ReverseProcessorConfig{Enabled: false})
	if p.Enabled() {
		t.Fatal("expected disabled processor to return false")
	}
}

func TestReverseProcessorEnabledTrue(t *testing.T) {
	p := NewReverseProcessor(ReverseProcessorConfig{Enabled: true})
	if !p.Enabled() {
		t.Fatal("expected enabled processor to return true")
	}
}

// ---------- CredentialStatus tests ----------

func TestCredentialStatusNilProcessor(t *testing.T) {
	var p *ReverseProcessor
	status := p.CredentialStatus("did:plc:x")
	if status.Configured {
		t.Fatal("expected unconfigured for nil processor")
	}
	if status.Reason != "reverse_sync_unavailable" {
		t.Errorf("unexpected reason: %q", status.Reason)
	}
}

func TestCredentialStatusMissingEntry(t *testing.T) {
	p := NewReverseProcessor(ReverseProcessorConfig{
		Credentials: map[string]ReverseCredentialFileEntry{},
	})
	status := p.CredentialStatus("did:plc:unknown")
	if status.Configured {
		t.Fatal("expected unconfigured")
	}
	if status.Reason != "missing_credentials_entry" {
		t.Errorf("unexpected reason: %q", status.Reason)
	}
}

func TestCredentialStatusMissingIdentifier(t *testing.T) {
	p := NewReverseProcessor(ReverseProcessorConfig{
		Credentials: map[string]ReverseCredentialFileEntry{
			"did:plc:x": {Identifier: "", PasswordEnv: "PWD", PDSHost: "https://pds.test"},
		},
	})
	status := p.CredentialStatus("did:plc:x")
	if status.Configured {
		t.Fatal("expected unconfigured")
	}
	if status.Reason != "missing_identifier" {
		t.Errorf("unexpected reason: %q", status.Reason)
	}
}

func TestCredentialStatusMissingPasswordEnv(t *testing.T) {
	p := NewReverseProcessor(ReverseProcessorConfig{
		Credentials: map[string]ReverseCredentialFileEntry{
			"did:plc:x": {Identifier: "user", PasswordEnv: "", PDSHost: "https://pds.test"},
		},
	})
	status := p.CredentialStatus("did:plc:x")
	if status.Configured {
		t.Fatal("expected unconfigured")
	}
	if status.Reason != "missing_password_env" {
		t.Errorf("unexpected reason: %q", status.Reason)
	}
}

func TestCredentialStatusPasswordEnvUnset(t *testing.T) {
	p := NewReverseProcessor(ReverseProcessorConfig{
		Credentials: map[string]ReverseCredentialFileEntry{
			"did:plc:x": {Identifier: "user", PasswordEnv: "BRIDGE_TEST_NONEXISTENT_cred_7861", PDSHost: "https://pds.test"},
		},
	})
	os.Unsetenv("BRIDGE_TEST_NONEXISTENT_cred_7861")
	status := p.CredentialStatus("did:plc:x")
	if status.Configured {
		t.Fatal("expected unconfigured when env var unset")
	}
	if status.Reason != "password_env_unset" {
		t.Errorf("unexpected reason: %q", status.Reason)
	}
}

func TestCredentialStatusConfigured(t *testing.T) {
	const envKey = "BRIDGE_TEST_CRED_PWD_001"
	os.Setenv(envKey, "secret")
	t.Cleanup(func() { os.Unsetenv(envKey) })

	p := NewReverseProcessor(ReverseProcessorConfig{
		Credentials: map[string]ReverseCredentialFileEntry{
			"did:plc:x": {Identifier: "user", PasswordEnv: envKey, PDSHost: "https://pds.test"},
		},
	})
	status := p.CredentialStatus("did:plc:x")
	if !status.Configured {
		t.Fatal("expected configured")
	}
	if status.Reason != "configured" {
		t.Errorf("unexpected reason: %q", status.Reason)
	}
}

func TestCredentialStatusConfiguredViaPLC(t *testing.T) {
	const envKey = "BRIDGE_TEST_CRED_PWD_002"
	os.Setenv(envKey, "secret")
	t.Cleanup(func() { os.Unsetenv(envKey) })

	p := NewReverseProcessor(ReverseProcessorConfig{
		Credentials: map[string]ReverseCredentialFileEntry{
			"did:plc:x": {Identifier: "user", PasswordEnv: envKey, PDSHost: ""},
		},
	})
	status := p.CredentialStatus("did:plc:x")
	if !status.Configured {
		t.Fatal("expected configured via PLC")
	}
	if status.Reason != "configured_via_plc" {
		t.Errorf("unexpected reason: %q", status.Reason)
	}
}

// ---------- stripEmbeddedBlobMarkdown tests ----------

func TestStripEmbeddedBlobMarkdownNoRefs(t *testing.T) {
	result := stripEmbeddedBlobMarkdown("Hello world", nil)
	if result != "Hello world" {
		t.Errorf("unexpected: %q", result)
	}
}

func TestStripEmbeddedBlobMarkdownEmptyText(t *testing.T) {
	result := stripEmbeddedBlobMarkdown("", map[string]struct{}{"&blob.sha256": {}})
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

// ---------- normalizeReversePostText tests ----------

func TestNormalizeReversePostText(t *testing.T) {
	input := "  hello   world  \n\n\n\nfoo  "
	result := normalizeReversePostText(input)
	if strings.Contains(result, "   ") {
		t.Errorf("expected multi-space collapsed: %q", result)
	}
	if strings.Contains(result, "\n\n\n") {
		t.Errorf("expected multi-blank-lines collapsed: %q", result)
	}
}

func TestNormalizeReversePostTextEmpty(t *testing.T) {
	if result := normalizeReversePostText(""); result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

// ---------- processDecodedMessage - skip paths ----------

func TestProcessDecodedMessageSkipsEmptyAuthor(t *testing.T) {
	database, _ := db.Open(":memory:?parseTime=true")
	defer database.Close()

	writer := &stubReverseWriter{}
	proc := newTestReverseProcessor(t, database, writer)

	err := proc.processDecodedMessage(context.Background(), 1, "%ref.sha256", "", int64Ptr(1), []byte(`{"type":"post","text":"x"}`), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(writer.createCalls) != 0 {
		t.Fatal("expected no create calls for empty author")
	}
}

func TestProcessDecodedMessageSkipsEmptyRef(t *testing.T) {
	database, _ := db.Open(":memory:?parseTime=true")
	defer database.Close()

	writer := &stubReverseWriter{}
	proc := newTestReverseProcessor(t, database, writer)

	err := proc.processDecodedMessage(context.Background(), 1, "", "@alice.ed25519", int64Ptr(1), []byte(`{"type":"post","text":"x"}`), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(writer.createCalls) != 0 {
		t.Fatal("expected no create calls for empty ref")
	}
}

func TestProcessDecodedMessageSkipsUnmappedAuthor(t *testing.T) {
	database, _ := db.Open(":memory:?parseTime=true")
	defer database.Close()

	writer := &stubReverseWriter{}
	proc := newTestReverseProcessor(t, database, writer)

	// No identity mapping for @unknown.ed25519
	err := proc.processDecodedMessage(context.Background(), 1, "%ref.sha256", "@unknown.ed25519", int64Ptr(1), []byte(`{"type":"post","text":"x"}`), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(writer.createCalls) != 0 {
		t.Fatal("expected no create calls for unmapped author")
	}
}

func TestProcessDecodedMessageDecodeFailurePersists(t *testing.T) {
	database, _ := db.Open(":memory:?parseTime=true")
	defer database.Close()

	ctx := context.Background()
	if err := database.AddReverseIdentityMapping(ctx, db.ReverseIdentityMapping{
		SSBFeedID: "@alice.ed25519", ATDID: "did:plc:alice", Active: true,
		AllowPosts: true, AllowReplies: true, AllowFollows: true,
	}); err != nil {
		t.Fatal(err)
	}

	writer := &stubReverseWriter{}
	proc := newTestReverseProcessor(t, database, writer)

	// The processor tries to persist a failed event with an empty action,
	// which the DB rejects. This bubbles up as an error.
	err := proc.processDecodedMessage(ctx, 1, "%badjson.sha256", "@alice.ed25519", int64Ptr(1), []byte(`not json at all`), false)
	if err == nil {
		t.Fatal("expected error for bad JSON that fails to persist")
	}
	if !strings.Contains(err.Error(), "persist reverse decode failure") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProcessDecodedMessageSkipsAlreadyProcessedEvent(t *testing.T) {
	database, _ := db.Open(":memory:?parseTime=true")
	defer database.Close()

	ctx := context.Background()
	if err := database.AddReverseIdentityMapping(ctx, db.ReverseIdentityMapping{
		SSBFeedID: "@alice.ed25519", ATDID: "did:plc:alice", Active: true,
		AllowPosts: true, AllowReplies: true, AllowFollows: true,
	}); err != nil {
		t.Fatal(err)
	}

	writer := &stubReverseWriter{}
	proc := newTestReverseProcessor(t, database, writer)

	// First call - processes
	err := proc.processDecodedMessage(ctx, 1, "%dup.sha256", "@alice.ed25519", int64Ptr(1), []byte(`{"type":"post","text":"first"}`), false)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(writer.createCalls) != 1 {
		t.Fatal("expected 1 create call on first processing")
	}

	// Second call with same ref - should be skipped (already published)
	err = proc.processDecodedMessage(ctx, 2, "%dup.sha256", "@alice.ed25519", int64Ptr(2), []byte(`{"type":"post","text":"second"}`), false)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(writer.createCalls) != 1 {
		t.Fatalf("expected still 1 create call (skip), got %d", len(writer.createCalls))
	}
}

// ---------- NewReverseProcessor defaults ----------

func TestNewReverseProcessorDefaults(t *testing.T) {
	p := NewReverseProcessor(ReverseProcessorConfig{
		Logger: log.New(io.Discard, "", 0),
	})
	if p == nil {
		t.Fatal("expected non-nil processor")
	}
	if p.writer == nil {
		t.Fatal("expected default writer")
	}
	if p.credentials == nil {
		t.Fatal("expected non-nil credentials map")
	}
}

// ---------- findFirstNonOverlappingToken ----------

func TestFindFirstNonOverlappingToken(t *testing.T) {
	text := "Hello @alice how are you @bob"
	start, end, ok := findFirstNonOverlappingToken(text, "@alice", nil)
	if !ok || text[start:end] != "@alice" {
		t.Errorf("expected to find @alice: ok=%v start=%d end=%d", ok, start, end)
	}

	// Now occupy that range and find @bob
	occupied := []reverseByteRange{{start: start, end: end}}
	start2, end2, ok2 := findFirstNonOverlappingToken(text, "@bob", occupied)
	if !ok2 || text[start2:end2] != "@bob" {
		t.Errorf("expected to find @bob: ok=%v start=%d end=%d", ok2, start2, end2)
	}

	// Empty token
	_, _, ok3 := findFirstNonOverlappingToken(text, "", nil)
	if ok3 {
		t.Error("expected false for empty token")
	}

	// Missing token
	_, _, ok4 := findFirstNonOverlappingToken(text, "@charlie", nil)
	if ok4 {
		t.Error("expected false for missing token")
	}
}

func TestRangeOverlaps(t *testing.T) {
	occupied := []reverseByteRange{{start: 5, end: 10}}
	if !rangeOverlaps(7, 12, occupied) {
		t.Error("expected overlap")
	}
	if !rangeOverlaps(3, 7, occupied) {
		t.Error("expected overlap")
	}
	if rangeOverlaps(10, 15, occupied) {
		t.Error("expected no overlap (adjacent)")
	}
	if rangeOverlaps(0, 5, occupied) {
		t.Error("expected no overlap (adjacent)")
	}
	// Invalid range
	if !rangeOverlaps(10, 5, nil) {
		t.Error("expected overlap for invalid range (end <= start)")
	}
}

// ---------- resolveReplyTargets ----------

func TestResolveReplyTargetsMissingRefs(t *testing.T) {
	database, _ := db.Open(":memory:?parseTime=true")
	defer database.Close()

	proc := newTestReverseProcessor(t, database, &stubReverseWriter{})
	_, _, reason, err := proc.resolveReplyTargets(context.Background(), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "missing_reply_refs" {
		t.Errorf("expected missing_reply_refs, got %q", reason)
	}
}

func TestResolveReplyTargetsSymmetricalFill(t *testing.T) {
	database, _ := db.Open(":memory:?parseTime=true")
	defer database.Close()

	ctx := context.Background()
	if err := database.AddMessage(ctx, db.Message{
		ATURI: "at://did:plc:x/app.bsky.feed.post/a", ATCID: "cid-a",
		ATDID: "did:plc:x", Type: "app.bsky.feed.post",
		MessageState: db.MessageStatePublished, SSBMsgRef: "%a.sha256",
	}); err != nil {
		t.Fatal(err)
	}

	proc := newTestReverseProcessor(t, database, &stubReverseWriter{})

	// Only root provided - parent should also use root
	root, parent, reason, err := proc.resolveReplyTargets(ctx, "%a.sha256", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "" {
		t.Errorf("expected no defer reason, got %q", reason)
	}
	if root == nil || parent == nil {
		t.Fatal("expected both root and parent resolved")
	}
	if root.ATURI != parent.ATURI {
		t.Errorf("expected root==parent when only root provided: root=%q parent=%q", root.ATURI, parent.ATURI)
	}
}
