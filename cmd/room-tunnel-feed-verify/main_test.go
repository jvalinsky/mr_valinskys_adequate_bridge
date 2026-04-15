package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

// ---------- splitCSV tests ----------

func TestSplitCSV(t *testing.T) {
	got := splitCSV("  at://a ,at://b,at://a, ,at://c  ")
	want := []string{"at://a", "at://b", "at://c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitCSV mismatch: got=%v want=%v", got, want)
	}
}

func TestSplitCSVEmpty(t *testing.T) {
	if got := splitCSV(""); got != nil {
		t.Fatalf("expected nil for empty, got %v", got)
	}
	if got := splitCSV("   "); got != nil {
		t.Fatalf("expected nil for whitespace, got %v", got)
	}
}

func TestSplitCSVSingleEntry(t *testing.T) {
	got := splitCSV("at://x")
	want := []string{"at://x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitCSV single: got=%v want=%v", got, want)
	}
}

// ---------- containsString tests ----------

func TestContainsString(t *testing.T) {
	if !containsString([]string{"tunnel", "room1"}, "tunnel") {
		t.Fatal("expected containsString to find 'tunnel'")
	}
	if containsString([]string{"room1", "room2"}, "tunnel") {
		t.Fatal("expected containsString to not find 'tunnel'")
	}
	if containsString(nil, "anything") {
		t.Fatal("expected containsString with nil slice to return false")
	}
	// Trimming behavior
	if !containsString([]string{" tunnel "}, "tunnel") {
		t.Fatal("expected containsString to trim spaces")
	}
}

// ---------- parseAppKey tests ----------

func TestParseAppKeyValid(t *testing.T) {
	appKey, err := parseAppKey(defaultSHSCap)
	if err != nil {
		t.Fatalf("parseAppKey with default cap: %v", err)
	}
	if appKey == [32]byte{} {
		t.Fatal("expected non-zero app key")
	}
}

func TestParseAppKeyInvalidBase64(t *testing.T) {
	_, err := parseAppKey("not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestParseAppKeyWrongLength(t *testing.T) {
	// Valid base64 but only 16 bytes
	_, err := parseAppKey("AAAAAAAAAAAAAAAAAAAAAA==")
	if err == nil {
		t.Fatal("expected error for wrong key length")
	}
}

// ---------- config validation tests ----------

func TestServeConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  serveConfig
	}{
		{"missing room-addr", serveConfig{RoomFeed: "x", KeyFile: "x", DBPath: "x", SourceDID: "x", Timeout: time.Second}},
		{"missing room-feed", serveConfig{RoomAddr: "x", KeyFile: "x", DBPath: "x", SourceDID: "x", Timeout: time.Second}},
		{"missing key-file", serveConfig{RoomAddr: "x", RoomFeed: "x", DBPath: "x", SourceDID: "x", Timeout: time.Second}},
		{"missing db", serveConfig{RoomAddr: "x", RoomFeed: "x", KeyFile: "x", SourceDID: "x", Timeout: time.Second}},
		{"missing source-did", serveConfig{RoomAddr: "x", RoomFeed: "x", KeyFile: "x", DBPath: "x", Timeout: time.Second}},
		{"bad timeout", serveConfig{RoomAddr: "x", RoomFeed: "x", KeyFile: "x", DBPath: "x", SourceDID: "x", Timeout: -1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	valid := serveConfig{RoomAddr: "x", RoomFeed: "x", KeyFile: "x", DBPath: "x", SourceDID: "x", Timeout: time.Second}
	if err := valid.validate(); err != nil {
		t.Fatalf("expected valid config: %v", err)
	}
}

func TestReadConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  readConfig
	}{
		{"missing room-addr", readConfig{RoomFeed: "x", KeyFile: "x", TargetFeed: "x", Timeout: time.Second, MinCount: 1}},
		{"missing room-feed", readConfig{RoomAddr: "x", KeyFile: "x", TargetFeed: "x", Timeout: time.Second, MinCount: 1}},
		{"missing key-file", readConfig{RoomAddr: "x", RoomFeed: "x", TargetFeed: "x", Timeout: time.Second, MinCount: 1}},
		{"missing target-feed", readConfig{RoomAddr: "x", RoomFeed: "x", KeyFile: "x", Timeout: time.Second, MinCount: 1}},
		{"bad timeout", readConfig{RoomAddr: "x", RoomFeed: "x", KeyFile: "x", TargetFeed: "x", Timeout: -1, MinCount: 1}},
		{"bad min-count", readConfig{RoomAddr: "x", RoomFeed: "x", KeyFile: "x", TargetFeed: "x", Timeout: time.Second, MinCount: 0}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	valid := readConfig{RoomAddr: "x", RoomFeed: "x", KeyFile: "x", TargetFeed: "x", Timeout: time.Second, MinCount: 1}
	if err := valid.validate(); err != nil {
		t.Fatalf("expected valid config: %v", err)
	}
}

func TestProbeConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  probeConfig
	}{
		{"missing room-addr", probeConfig{RoomFeed: "x", KeyFile: "x", TargetFeed: "x", Timeout: time.Second}},
		{"missing room-feed", probeConfig{RoomAddr: "x", KeyFile: "x", TargetFeed: "x", Timeout: time.Second}},
		{"missing key-file", probeConfig{RoomAddr: "x", RoomFeed: "x", TargetFeed: "x", Timeout: time.Second}},
		{"missing target-feed", probeConfig{RoomAddr: "x", RoomFeed: "x", KeyFile: "x", Timeout: time.Second}},
		{"bad timeout", probeConfig{RoomAddr: "x", RoomFeed: "x", KeyFile: "x", TargetFeed: "x", Timeout: 0}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	valid := probeConfig{RoomAddr: "x", RoomFeed: "x", KeyFile: "x", TargetFeed: "x", Timeout: time.Second}
	if err := valid.validate(); err != nil {
		t.Fatalf("expected valid config: %v", err)
	}
}

func TestHistoryConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  historyConfig
	}{
		{"missing room-addr", historyConfig{RoomFeed: "x", KeyFile: "x", TargetFeed: "x", Timeout: time.Second, MinCount: 1}},
		{"missing room-feed", historyConfig{RoomAddr: "x", KeyFile: "x", TargetFeed: "x", Timeout: time.Second, MinCount: 1}},
		{"missing key-file", historyConfig{RoomAddr: "x", RoomFeed: "x", TargetFeed: "x", Timeout: time.Second, MinCount: 1}},
		{"missing target-feed", historyConfig{RoomAddr: "x", RoomFeed: "x", KeyFile: "x", Timeout: time.Second, MinCount: 1}},
		{"bad timeout", historyConfig{RoomAddr: "x", RoomFeed: "x", KeyFile: "x", TargetFeed: "x", Timeout: 0, MinCount: 1}},
		{"bad min-count", historyConfig{RoomAddr: "x", RoomFeed: "x", KeyFile: "x", TargetFeed: "x", Timeout: time.Second, MinCount: 0}},
		{"bad limit", historyConfig{RoomAddr: "x", RoomFeed: "x", KeyFile: "x", TargetFeed: "x", Timeout: time.Second, MinCount: 1, Limit: -1}},
		{"bad sequence", historyConfig{RoomAddr: "x", RoomFeed: "x", KeyFile: "x", TargetFeed: "x", Timeout: time.Second, MinCount: 1, Sequence: -1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	valid := historyConfig{RoomAddr: "x", RoomFeed: "x", KeyFile: "x", TargetFeed: "x", Timeout: time.Second, MinCount: 1}
	if err := valid.validate(); err != nil {
		t.Fatalf("expected valid config: %v", err)
	}
}

func TestValidateClassicHistoryFrame(t *testing.T) {
	author, key, raw := signedHistoryPayload(t, 1)
	payload, err := json.Marshal(struct {
		Key       string          `json:"key"`
		Value     json.RawMessage `json:"value"`
		Timestamp int64           `json:"timestamp"`
	}{
		Key:       key,
		Value:     raw,
		Timestamp: 12346,
	})
	if err != nil {
		t.Fatalf("marshal wrapped frame: %v", err)
	}
	frame, err := validateClassicHistoryFrame(payload, author)
	if err != nil {
		t.Fatalf("validate wrapped frame: %v", err)
	}
	if frame.Key != key || frame.Author != author || frame.Sequence != 1 || frame.FeedFormat != "ed25519" || frame.MessageFormat != "sha256" || !frame.SignatureValid || frame.RawSHA256 == "" {
		t.Fatalf("unexpected frame summary: %+v", frame)
	}

	author, _, direct := signedHistoryPayload(t, 2)
	frame, err = validateClassicHistoryFrame(direct, author)
	if err != nil {
		t.Fatalf("validate direct frame: %v", err)
	}
	if frame.Key != "" || frame.Sequence != 2 || !frame.SignatureValid {
		t.Fatalf("unexpected direct frame summary: %+v", frame)
	}
}

func signedHistoryPayload(t *testing.T, sequence int64) (author string, key string, raw []byte) {
	t.Helper()
	kp, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	msg := legacy.Message{
		Previous:  nil,
		Author:    kp.FeedRef(),
		Sequence:  sequence,
		Timestamp: 12345 + sequence,
		Hash:      "sha256",
		Content: map[string]interface{}{
			"type": "post",
			"text": "hello",
		},
	}
	ref, sig, err := msg.Sign(kp)
	if err != nil {
		t.Fatalf("sign message: %v", err)
	}
	raw, err = msg.MarshalWithSignature(sig)
	if err != nil {
		t.Fatalf("marshal signed message: %v", err)
	}
	return kp.FeedRef().String(), ref.String(), raw
}

func TestValidateClassicHistoryFrameRejectsInvalid(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{"missing author", `{"sequence":1,"timestamp":1,"hash":"sha256","content":{},"signature":"x.sig.ed25519"}`},
		{"bad sequence", `{"author":"@a.ed25519","sequence":0,"timestamp":1,"hash":"sha256","content":{},"signature":"x.sig.ed25519"}`},
		{"missing content", `{"author":"@a.ed25519","sequence":1,"timestamp":1,"hash":"sha256","signature":"x.sig.ed25519"}`},
		{"bad signature", `{"author":"@a.ed25519","sequence":1,"timestamp":1,"hash":"sha256","content":{},"signature":"x"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateClassicHistoryFrame([]byte(tc.payload), ""); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if _, err := validateClassicHistoryFrame([]byte(`{"author":"@a.ed25519","sequence":1,"timestamp":1,"hash":"sha256","content":{},"signature":"x.sig.ed25519"}`), "@other.ed25519"); err == nil {
		t.Fatal("expected author mismatch")
	}
}

func TestValidateClassicHistoryFrameRejectsNonClassicFeed(t *testing.T) {
	author := refs.MustNewFeedRef([]byte("abcdefghijklmnopqrstuvwxyz123456"), refs.RefAlgoFeedBamboo).String()
	payload := []byte(strings.ReplaceAll(`{"previous":null,"author":"AUTHOR","sequence":1,"timestamp":1,"hash":"sha256","content":{},"signature":"x.sig.ed25519"}`, "AUTHOR", author))
	if _, err := validateClassicHistoryFrame(payload, ""); err == nil || !strings.Contains(err.Error(), "unsupported_feed_format") {
		t.Fatalf("expected structured unsupported format error, got %v", err)
	}
}

// ---------- ensureKeyPair tests ----------

func TestEnsureKeyPairEmptyPath(t *testing.T) {
	_, err := ensureKeyPair("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestEnsureKeyPairGeneratesAndLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "secret.json")

	// First call generates
	kp1, err := ensureKeyPair(path)
	if err != nil {
		t.Fatalf("ensureKeyPair generate: %v", err)
	}
	if kp1 == nil {
		t.Fatal("expected non-nil keypair")
	}

	// Second call loads existing
	kp2, err := ensureKeyPair(path)
	if err != nil {
		t.Fatalf("ensureKeyPair load: %v", err)
	}
	if kp2.FeedRef().String() != kp1.FeedRef().String() {
		t.Fatalf("expected same feed ref, got %q vs %q", kp1.FeedRef().String(), kp2.FeedRef().String())
	}
}

func TestEnsureKeyPairInvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ensureKeyPair(path)
	if err == nil {
		t.Fatal("expected error for invalid key file")
	}
}

// ---------- writeReadyFile tests ----------

func TestWriteReadyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "ready.json")

	kp, err := ensureKeyPair(filepath.Join(dir, "keys", "test.json"))
	if err != nil {
		t.Fatalf("create temp keypair: %v", err)
	}
	feed := kp.FeedRef()

	if err := writeReadyFile(path, feed); err != nil {
		t.Fatalf("writeReadyFile: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ready file: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode ready file: %v", err)
	}
	if payload["feed"] != feed.String() {
		t.Fatalf("ready file feed mismatch: got %q want %q", payload["feed"], feed.String())
	}
}

// ---------- roomConn.Close tests ----------

func TestRoomConnCloseNil(t *testing.T) {
	var rc *roomConn
	if err := rc.Close(); err != nil {
		t.Fatalf("nil roomConn.Close: %v", err)
	}
}

func TestRoomConnCloseEmpty(t *testing.T) {
	rc := &roomConn{}
	if err := rc.Close(); err != nil {
		t.Fatalf("empty roomConn.Close: %v", err)
	}
}

// ---------- whoamiHandler tests ----------

func TestWhoamiHandlerHandled(t *testing.T) {
	dir := t.TempDir()
	kp, err := ensureKeyPair(filepath.Join(dir, "keys.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := &whoamiHandler{keyPair: kp}

	if !h.Handled([]string{"whoami"}) {
		t.Fatal("expected whoami to be handled")
	}
	if h.Handled([]string{"other"}) {
		t.Fatal("expected other to not be handled")
	}
	if h.Handled([]string{"whoami", "extra"}) {
		t.Fatal("expected whoami/extra to not be handled")
	}
	if h.Handled(nil) {
		t.Fatal("expected nil to not be handled")
	}
}

// ---------- tunnelServeHandler.Handled tests ----------

func TestTunnelServeHandlerHandled(t *testing.T) {
	h := &tunnelServeHandler{}

	if !h.Handled([]string{"whoami"}) {
		t.Fatal("expected whoami to be handled")
	}
	if !h.Handled([]string{"tunnel", "connect"}) {
		t.Fatal("expected tunnel.connect to be handled")
	}
	if h.Handled([]string{"tunnel"}) {
		t.Fatal("expected bare tunnel to not be handled")
	}
	if h.Handled([]string{"tunnel", "announce"}) {
		t.Fatal("expected tunnel.announce to not be handled")
	}
	if h.Handled([]string{"other"}) {
		t.Fatal("expected other to not be handled")
	}
}

// ---------- validateSnapshot tests ----------

func TestValidateSnapshot(t *testing.T) {
	snapshot := tunnelSnapshot{
		SourceFeed: "@" + "source.example" + ".ed25519",
		Entries: []bridgeEntry{
			{ATURI: "at://example/post/1", SSBMsgRef: "%msg1.sha256", Type: "post"},
			{ATURI: "at://example/post/2", SSBMsgRef: "%msg2.sha256", Type: "like"},
		},
	}

	if err := validateSnapshot(snapshot, snapshot.SourceFeed, []string{"at://example/post/1"}, 1); err != nil {
		t.Fatalf("validateSnapshot success path failed: %v", err)
	}

	if err := validateSnapshot(snapshot, "@"+"other.example"+".ed25519", nil, 1); err == nil {
		t.Fatalf("validateSnapshot expected source-feed mismatch error")
	}

	if err := validateSnapshot(snapshot, snapshot.SourceFeed, []string{"at://missing"}, 1); err == nil {
		t.Fatalf("validateSnapshot expected missing URI error")
	}

	bad := snapshot
	bad.Entries = []bridgeEntry{{ATURI: "at://example/post/1", SSBMsgRef: ""}}
	if err := validateSnapshot(bad, snapshot.SourceFeed, nil, 1); err == nil {
		t.Fatalf("validateSnapshot expected empty ssb_msg_ref error")
	}
}

func TestValidateSnapshotMinCount(t *testing.T) {
	snapshot := tunnelSnapshot{
		Entries: []bridgeEntry{
			{ATURI: "at://a", SSBMsgRef: "%a.sha256"},
		},
	}
	if err := validateSnapshot(snapshot, "", nil, 5); err == nil {
		t.Fatal("expected min count error")
	}
}

func TestValidateSnapshotEmptyATURI(t *testing.T) {
	snapshot := tunnelSnapshot{
		Entries: []bridgeEntry{
			{ATURI: "", SSBMsgRef: "%a.sha256"},
		},
	}
	if err := validateSnapshot(snapshot, "", nil, 1); err == nil {
		t.Fatal("expected empty at_uri error")
	}
}

func TestValidateSnapshotNoSourceFeedCheck(t *testing.T) {
	snapshot := tunnelSnapshot{
		SourceFeed: "@anything.ed25519",
		Entries: []bridgeEntry{
			{ATURI: "at://x", SSBMsgRef: "%x.sha256"},
		},
	}
	// Empty expected source feed should skip the source feed check
	if err := validateSnapshot(snapshot, "", nil, 1); err != nil {
		t.Fatalf("expected no error with empty expected source feed: %v", err)
	}
}

// ---------- loadPublishedEntries tests ----------

func TestLoadPublishedEntries(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "bridge.sqlite")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	now := time.Now().UTC()
	later := now.Add(time.Second)

	insert := func(msg db.Message) {
		t.Helper()
		if err := database.AddMessage(ctx, msg); err != nil {
			t.Fatalf("insert message %s: %v", msg.ATURI, err)
		}
	}

	insert(db.Message{
		ATURI:        "at://did:plc:source/app.bsky.feed.post/1",
		ATCID:        "cid1",
		ATDID:        "did:plc:source",
		Type:         "post",
		MessageState: db.MessageStatePublished,
		SSBMsgRef:    "%published1.sha256",
		PublishedAt:  &now,
	})
	insert(db.Message{
		ATURI:        "at://did:plc:source/app.bsky.feed.post/2",
		ATCID:        "cid2",
		ATDID:        "did:plc:source",
		Type:         "repost",
		MessageState: db.MessageStateFailed,
		SSBMsgRef:    "",
		PublishedAt:  &later,
	})
	insert(db.Message{
		ATURI:        "at://did:plc:other/app.bsky.feed.post/9",
		ATCID:        "cid9",
		ATDID:        "did:plc:other",
		Type:         "post",
		MessageState: db.MessageStatePublished,
		SSBMsgRef:    "%published-other.sha256",
		PublishedAt:  &later,
	})

	entries, err := loadPublishedEntries(ctx, dbPath, "did:plc:source", []string{
		"at://did:plc:source/app.bsky.feed.post/1",
		"at://did:plc:source/app.bsky.feed.post/2",
		"at://did:plc:source/app.bsky.feed.post/missing",
	})
	if err != nil {
		t.Fatalf("loadPublishedEntries failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one published source entry, got %d", len(entries))
	}
	if entries[0].ATURI != "at://did:plc:source/app.bsky.feed.post/1" {
		t.Fatalf("unexpected AT URI: %q", entries[0].ATURI)
	}
	if entries[0].SSBMsgRef != "%published1.sha256" {
		t.Fatalf("unexpected ssb_msg_ref: %q", entries[0].SSBMsgRef)
	}
}

func TestLoadPublishedEntriesFallbackPath(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "bridge.sqlite")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	now := time.Now().UTC()

	for i := 0; i < 3; i++ {
		msg := db.Message{
			ATURI:        "at://did:plc:source/app.bsky.feed.post/" + string(rune('A'+i)),
			ATCID:        "cid" + string(rune('A'+i)),
			ATDID:        "did:plc:source",
			Type:         "post",
			MessageState: db.MessageStatePublished,
			SSBMsgRef:    "%msg" + string(rune('A'+i)) + ".sha256",
			PublishedAt:  &now,
		}
		if err := database.AddMessage(ctx, msg); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	// Add a non-matching DID entry
	if err := database.AddMessage(ctx, db.Message{
		ATURI:        "at://did:plc:other/app.bsky.feed.post/Z",
		ATCID:        "cidZ",
		ATDID:        "did:plc:other",
		Type:         "post",
		MessageState: db.MessageStatePublished,
		SSBMsgRef:    "%msgZ.sha256",
		PublishedAt:  &now,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// Add a published entry with empty SSBMsgRef (should be filtered)
	if err := database.AddMessage(ctx, db.Message{
		ATURI:        "at://did:plc:source/app.bsky.feed.post/noref",
		ATCID:        "cidNoRef",
		ATDID:        "did:plc:source",
		Type:         "post",
		MessageState: db.MessageStatePublished,
		SSBMsgRef:    "",
		PublishedAt:  &now,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Fallback path: empty expectedURIs
	entries, err := loadPublishedEntries(ctx, dbPath, "did:plc:source", nil)
	if err != nil {
		t.Fatalf("loadPublishedEntries fallback: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries in fallback path, got %d", len(entries))
	}
	// Verify sorted
	for i := 1; i < len(entries); i++ {
		if entries[i].ATURI < entries[i-1].ATURI {
			t.Fatalf("entries not sorted: %q < %q", entries[i].ATURI, entries[i-1].ATURI)
		}
	}
}

func TestLoadPublishedEntriesAllDIDs(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "bridge.sqlite")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	now := time.Now().UTC()
	for _, did := range []string{"did:plc:a", "did:plc:b"} {
		if err := database.AddMessage(ctx, db.Message{
			ATURI:        "at://" + did + "/app.bsky.feed.post/1",
			ATCID:        "cid",
			ATDID:        did,
			Type:         "post",
			MessageState: db.MessageStatePublished,
			SSBMsgRef:    "%msg.sha256",
			PublishedAt:  &now,
		}); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Empty DID should not filter
	entries, err := loadPublishedEntries(ctx, dbPath, "", nil)
	if err != nil {
		t.Fatalf("loadPublishedEntries all DIDs: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for all DIDs, got %d", len(entries))
	}
}

func TestLoadPublishedEntriesBadDB(t *testing.T) {
	ctx := context.Background()
	_, err := loadPublishedEntries(ctx, "/nonexistent/path.db", "did:plc:x", nil)
	if err == nil {
		t.Fatal("expected error for bad db path")
	}
}

func TestLoadPublishedEntriesPublishedAt(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "bridge.sqlite")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	now := time.Now().UTC()
	if err := database.AddMessage(ctx, db.Message{
		ATURI:        "at://did:plc:source/app.bsky.feed.post/withtime",
		ATCID:        "cid",
		ATDID:        "did:plc:source",
		Type:         "post",
		MessageState: db.MessageStatePublished,
		SSBMsgRef:    "%msg.sha256",
		PublishedAt:  &now,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	entries, err := loadPublishedEntries(ctx, dbPath, "did:plc:source", []string{"at://did:plc:source/app.bsky.feed.post/withtime"})
	if err != nil {
		t.Fatalf("loadPublishedEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].PublishedAt == "" {
		t.Fatalf("expected published_at to be populated, got %+v", entries)
	}
}
