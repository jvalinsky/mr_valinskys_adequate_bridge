package feedlog

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/formats"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
)

func TestStore(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "feedlog-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.sqlite")
	store, err := NewStore(Config{
		DBPath:     dbPath,
		RepoPath:   tempDir,
		BlobSubdir: "blobs",
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Test MultiLog
	logs := store.Logs()
	author := "@alice.ed25519"

	has, err := logs.Has(author)
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("expected author not to exist yet")
	}

	l, err := logs.Create(author)
	if err != nil {
		t.Fatalf("failed to create log: %v", err)
	}

	has, err = logs.Has(author)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("expected author to exist")
	}

	list, err := logs.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0] != author {
		t.Errorf("unexpected list: %v", list)
	}

	// Test Log operations
	seq, err := l.Seq()
	if err != nil {
		t.Fatal(err)
	}
	if seq != -1 {
		t.Errorf("expected -1 for empty log, got %d", seq)
	}

	content := []byte(`{"type":"post","text":"hello"}`)
	metadata := &Metadata{
		Author:    author,
		Sequence:  1,
		Timestamp: 123456789,
		Hash:      "%hash1",
		Sig:       []byte("signature"),
	}

	newSeq, err := l.Append(content, metadata)
	if err != nil {
		t.Fatalf("failed to append: %v", err)
	}
	if newSeq != 1 {
		t.Errorf("expected seq 1, got %d", newSeq)
	}

	seq, err = l.Seq()
	if err != nil {
		t.Fatal(err)
	}
	if seq != 1 {
		t.Errorf("expected seq 1, got %d", seq)
	}

	msg, err := l.Get(1)
	if err != nil {
		t.Fatalf("failed to get msg: %v", err)
	}
	if !bytes.Equal(msg.Value, content) {
		t.Errorf("unexpected content: %s", msg.Value)
	}
	if msg.Metadata.Author != author {
		t.Errorf("unexpected author: %s", msg.Metadata.Author)
	}

	// Test ReceiveLog
	rl, err := store.ReceiveLog()
	if err != nil {
		t.Fatal(err)
	}

	rlSeq, err := rl.Seq()
	if err != nil {
		t.Fatal(err)
	}
	if rlSeq != -1 {
		t.Errorf("expected -1 for empty receive log, got %d", rlSeq)
	}

	rlNewSeq, err := rl.Append(content, metadata)
	if err != nil {
		t.Fatal(err)
	}
	if rlNewSeq != 1 {
		t.Errorf("expected seq 1, got %d", rlNewSeq)
	}

	rlMsg, err := rl.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rlMsg.Value, content) {
		t.Errorf("unexpected receive log content: %s", rlMsg.Value)
	}

	// Test BlobStore
	bs := store.Blobs()
	blobContent := []byte("fake blob data")
	hash, err := bs.Put(bytes.NewReader(blobContent))
	if err != nil {
		t.Fatalf("failed to put blob: %v", err)
	}

	hasBlob, err := bs.Has(hash)
	if err != nil {
		t.Fatal(err)
	}
	if !hasBlob {
		t.Fatal("expected blob to exist")
	}

	size, err := bs.Size(hash)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(blobContent)) {
		t.Errorf("expected size %d, got %d", len(blobContent), size)
	}

	rc, err := bs.Get(hash)
	if err != nil {
		t.Fatal(err)
	}
	gotBlob, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBlob, blobContent) {
		t.Errorf("unexpected blob content: %s", gotBlob)
	}

	err = bs.Delete(hash)
	if err != nil {
		t.Fatal(err)
	}
	hasBlob, _ = bs.Has(hash)
	if hasBlob {
		t.Fatal("expected blob to be deleted")
	}
}

func TestMessageFormatMetadataRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	store, err := NewStore(Config{
		DBPath:     filepath.Join(tempDir, "test.sqlite"),
		RepoPath:   tempDir,
		BlobSubdir: "blobs",
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	author := "@alice.bendybutt-v1"
	log, err := store.Logs().Create(author)
	if err != nil {
		t.Fatalf("create log: %v", err)
	}

	raw := []byte("d5:bendy4:rawe")
	content := []byte(`{"type":"post","text":"projection"}`)
	metadata := &Metadata{
		Author:           author,
		Sequence:         1,
		Timestamp:        123,
		Hash:             "%abc.bendybutt-v1",
		FeedFormat:       string(formats.FeedBendyButtV1),
		MessageFormat:    string(formats.MessageBendyButtV1),
		RawValue:         raw,
		CanonicalRef:     "%abc.bendybutt-v1",
		ValidationStatus: "validated",
	}
	if _, err := log.Append(content, metadata); err != nil {
		t.Fatalf("append: %v", err)
	}

	got, err := log.Get(1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.FeedFormat != string(formats.FeedBendyButtV1) || got.MessageFormat != string(formats.MessageBendyButtV1) {
		t.Fatalf("unexpected formats: %+v", got)
	}
	if got.CanonicalRef != "%abc.bendybutt-v1" || got.Key != "%abc.bendybutt-v1" {
		t.Fatalf("unexpected canonical ref/key: %+v", got)
	}
	if !bytes.Equal(got.RawValue, raw) || !bytes.Equal(got.Metadata.RawValue, raw) {
		t.Fatalf("raw bytes were not preserved")
	}
}

func TestFeedlogMigratesClassicRowsToFormatDefaults(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "legacy.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE schema_version (version INTEGER PRIMARY KEY);
		INSERT INTO schema_version (version) VALUES (13);
		CREATE TABLE feeds (id INTEGER PRIMARY KEY AUTOINCREMENT, addr BLOB UNIQUE NOT NULL, created_at INTEGER NOT NULL);
		CREATE INDEX idx_feeds_addr ON feeds(addr);
		CREATE TABLE messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			feed_id INTEGER NOT NULL,
			seq INTEGER NOT NULL,
			key TEXT NOT NULL,
			value_json BLOB NOT NULL,
			created_at INTEGER NOT NULL,
			FOREIGN KEY (feed_id) REFERENCES feeds(id),
			UNIQUE(feed_id, seq)
		);
		CREATE INDEX idx_messages_feed_seq ON messages(feed_id, seq);
		CREATE TABLE messages_key_idx (key TEXT UNIQUE NOT NULL, feed_id INTEGER NOT NULL, seq INTEGER NOT NULL);
		CREATE INDEX idx_messages_key ON messages_key_idx(key);
		CREATE TABLE receive_log (id INTEGER PRIMARY KEY, seq INTEGER NOT NULL, key TEXT NOT NULL, value_json BLOB NOT NULL, created_at INTEGER NOT NULL);
		CREATE TABLE blobs (id INTEGER PRIMARY KEY AUTOINCREMENT, hash BLOB UNIQUE NOT NULL, size INTEGER NOT NULL, created_at INTEGER NOT NULL);
		CREATE INDEX idx_blobs_hash ON blobs(hash);
		CREATE TABLE tangles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			root TEXT NOT NULL,
			tips TEXT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(name, root)
		);
		CREATE INDEX idx_tangles_name_root ON tangles(name, root);
		CREATE TABLE tangle_membership (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_key TEXT NOT NULL,
			tangle_name TEXT NOT NULL,
			root_key TEXT NOT NULL,
			parent_keys TEXT,
			created_at INTEGER NOT NULL,
			UNIQUE(message_key, tangle_name)
		);
		CREATE INDEX idx_tangle_membership_tangle ON tangle_membership(tangle_name, root_key);
		CREATE INDEX idx_tangle_membership_root ON tangle_membership(root_key);
	`)
	if err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO feeds (id, addr, created_at) VALUES (?, ?, ?)`, 1, []byte("@alice.ed25519"), 1); err != nil {
		t.Fatalf("insert legacy feed: %v", err)
	}
	wrapper, err := json.Marshal(&storedMessageWrapper{
		Content: []byte(`{"type":"post"}`),
		Metadata: &Metadata{
			Author:    "@alice.ed25519",
			Sequence:  1,
			Timestamp: 1,
			Hash:      "%legacy.sha256",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO messages (feed_id, seq, key, value_json, created_at) VALUES (1, 1, '%legacy.sha256', ?, 1)`, wrapper); err != nil {
		t.Fatalf("insert legacy message: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(Config{DBPath: dbPath, RepoPath: tempDir})
	if err != nil {
		t.Fatalf("migrate legacy store: %v", err)
	}
	defer store.Close()
	log, err := store.Logs().Get("@alice.ed25519")
	if err != nil {
		t.Fatalf("get migrated log: %v", err)
	}
	msg, err := log.Get(1)
	if err != nil {
		t.Fatalf("get migrated message: %v", err)
	}
	if msg.FeedFormat != string(formats.FeedEd25519) || msg.MessageFormat != string(formats.MessageSHA256) {
		t.Fatalf("unexpected migrated formats: %+v", msg)
	}
}

func TestSignatureVerification(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sig-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.sqlite")
	store, err := NewStore(Config{
		DBPath:   dbPath,
		RepoPath: tempDir,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	kp, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}

	authorRef := kp.FeedRef()

	msg := &legacy.Message{
		Author:    authorRef,
		Sequence:  1,
		Timestamp: 1234567890,
		Hash:      legacy.HashAlgorithm,
		Content:   map[string]interface{}{"type": "post", "text": "hello from signature test"},
	}

	msgRef, sig, err := msg.Sign(kp)
	if err != nil {
		t.Fatal(err)
	}

	signedJSON, err := msg.MarshalWithSignature(sig)
	if err != nil {
		t.Fatal(err)
	}

	metadata := &Metadata{
		Author:    authorRef.String(),
		Sequence:  1,
		Timestamp: 1234567890,
		Hash:      msgRef.String(),
		Sig:       sig,
	}

	store.SetSignatureVerifier(&DefaultSignatureVerifier{})

	var sigLogEntries []struct {
		author string
		valid  bool
	}
	var sigLogMu sync.Mutex
	store.SetSignatureLogger(func(author string, seq int64, key string, valid bool, err error) {
		sigLogMu.Lock()
		defer sigLogMu.Unlock()
		sigLogEntries = append(sigLogEntries, struct {
			author string
			valid  bool
		}{author: author, valid: valid})
	})

	rl, err := store.ReceiveLog()
	if err != nil {
		t.Fatal(err)
	}

	seq, err := rl.Append(signedJSON, metadata)
	if err != nil {
		t.Fatalf("failed to append with valid signature: %v", err)
	}
	if seq != 1 {
		t.Errorf("expected seq 1, got %d", seq)
	}

	sigLogMu.Lock()
	if len(sigLogEntries) != 1 {
		t.Errorf("expected 1 signature log entry, got %d", len(sigLogEntries))
	} else if !sigLogEntries[0].valid {
		t.Error("expected valid signature to be logged as valid")
	}
	sigLogMu.Unlock()
}

func TestNoSignatureVerifier(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nosig-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.sqlite")
	store, err := NewStore(Config{
		DBPath:   dbPath,
		RepoPath: tempDir,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	content := []byte(`{"type":"post","text":"no verifier"}`)
	metadata := &Metadata{
		Author:    "@test.ed25519",
		Sequence:  1,
		Timestamp: 123456789,
		Hash:      "%hash1",
		Sig:       nil,
	}

	rl, err := store.ReceiveLog()
	if err != nil {
		t.Fatal(err)
	}

	seq, err := rl.Append(content, metadata)
	if err != nil {
		t.Fatalf("failed to append without verifier: %v", err)
	}
	if seq != 1 {
		t.Errorf("expected seq 1, got %d", seq)
	}
}

func createTestSignedMessage(t *testing.T, kp *keys.KeyPair, seq int64) ([]byte, *Metadata) {
	t.Helper()

	authorRef := kp.FeedRef()

	msg := &legacy.Message{
		Author:    authorRef,
		Sequence:  seq,
		Timestamp: 1234567890,
		Hash:      legacy.HashAlgorithm,
		Content:   map[string]interface{}{"type": "post", "text": "test message"},
	}

	msgRef, sig, err := msg.Sign(kp)
	if err != nil {
		t.Fatal(err)
	}

	signedJSON, err := msg.MarshalWithSignature(sig)
	if err != nil {
		t.Fatal(err)
	}

	metadata := &Metadata{
		Author:    authorRef.String(),
		Sequence:  seq,
		Timestamp: 1234567890,
		Hash:      msgRef.String(),
		Sig:       sig,
	}

	return signedJSON, metadata
}

func TestDefaultSignatureVerifierRejectsWhitespaceMutations(t *testing.T) {
	kp, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}
	raw, metadata := createTestSignedMessage(t, kp, 1)

	mutated := bytes.Replace(raw, []byte(`"author": "`), []byte(`"author" : "`), 1)
	if bytes.Equal(mutated, raw) {
		t.Fatal("expected whitespace mutation to change payload")
	}

	verifier := &DefaultSignatureVerifier{}
	if err := verifier.Verify(mutated, metadata); err == nil {
		t.Fatal("expected whitespace-mutated message to fail strict verification")
	}
}

func TestDefaultSignatureVerifierRejectsOuterWhitespaceMutations(t *testing.T) {
	kp, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}
	raw, metadata := createTestSignedMessage(t, kp, 1)

	cases := []struct {
		name    string
		mutated []byte
	}{
		{
			name:    "leading space",
			mutated: append([]byte(" "), raw...),
		},
		{
			name:    "trailing newline",
			mutated: append(append([]byte(nil), raw...), '\n'),
		},
	}

	verifier := &DefaultSignatureVerifier{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := verifier.Verify(tc.mutated, metadata); err == nil {
				t.Fatalf("expected %s mutation to fail strict verification", tc.name)
			}
		})
	}
}

func TestDefaultSignatureVerifierRejectsTopLevelOrderMutations(t *testing.T) {
	kp, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}
	raw, metadata := createTestSignedMessage(t, kp, 1)

	var parsed struct {
		Author    string          `json:"author"`
		Sequence  int64           `json:"sequence"`
		Timestamp int64           `json:"timestamp"`
		Hash      string          `json:"hash"`
		Content   json.RawMessage `json:"content"`
		Signature string          `json:"signature"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal signed message: %v", err)
	}

	mutated := []byte(fmt.Sprintf(`{"previous":null,"sequence":%d,"author":"%s","timestamp":%d,"hash":"%s","content":%s,"signature":"%s"}`,
		parsed.Sequence,
		parsed.Author,
		parsed.Timestamp,
		parsed.Hash,
		parsed.Content,
		parsed.Signature,
	))

	verifier := &DefaultSignatureVerifier{}
	if err := verifier.Verify(mutated, metadata); err == nil {
		t.Fatal("expected top-level-order-mutated message to fail strict verification")
	}
}

func TestDefaultSignatureVerifierAcceptsFixtureSignedMessage(t *testing.T) {
	const fixture = `{"previous":null,"author":"@6iF2pmL9+jpnM515551HTgVVOGCUZ9qfE8Y3DmdFz7w=.ed25519","sequence":1,"timestamp":1775622534000,"hash":"sha256","content":{"type":"contact","contact":"@HY3zOj73zbLT5wG76eUZXIKTMB4to/voRbYWESXyVtA=.ed25519","following":true},"signature":"IFefnN3fb4bEpWfFtMD2lyn30yQXtmSPVCB0JQQv05WkHVADzz675PiMAf5JLXosTUPfP02IvTeKHdQd1JGPAw==.sig.ed25519"}`
	prettyFixture, err := legacy.PrettyPrint([]byte(fixture))
	if err != nil {
		t.Fatalf("pretty print fixture: %v", err)
	}

	msg, _, err := legacy.ParseSignedMessageJSON(prettyFixture)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	msgRef, err := legacy.SignedMessageRefFromJSON(prettyFixture)
	if err != nil {
		t.Fatalf("fixture ref: %v", err)
	}

	metadata := &Metadata{
		Author:    msg.Author.String(),
		Sequence:  msg.Sequence,
		Timestamp: msg.Timestamp,
		Hash:      msgRef.String(),
		Sig:       msg.Signature,
	}

	verifier := &DefaultSignatureVerifier{}
	if err := verifier.Verify(prettyFixture, metadata); err != nil {
		t.Fatalf("fixture must verify: %v", err)
	}

	gotSig := base64.StdEncoding.EncodeToString(msg.Signature) + ".sig.ed25519"
	if !bytes.Contains([]byte(fixture), []byte(gotSig)) {
		t.Fatalf("fixture signature suffix mismatch: %s", gotSig)
	}
}

// createTestStore is a helper that creates a temporary store for testing
func createTestStore(t *testing.T) *StoreImpl {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "feedlog-key-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	store, err := NewStore(Config{
		DBPath:   filepath.Join(tempDir, "test.sqlite"),
		RepoPath: tempDir,
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// TestMessageKeyUniqueness verifies that two distinct messages produce different SQL keys
func TestMessageKeyUniqueness(t *testing.T) {
	store := createTestStore(t)

	logs := store.Logs()
	l, err := logs.Create("@alice.ed25519")
	if err != nil {
		t.Fatal(err)
	}

	msg1 := []byte(`{"type":"post","text":"message one"}`)
	msg2 := []byte(`{"type":"post","text":"message two"}`)
	meta1 := &Metadata{Author: "@alice.ed25519", Sequence: 1, Hash: "%h1"}
	meta2 := &Metadata{Author: "@alice.ed25519", Sequence: 2, Hash: "%h2"}

	if _, err := l.Append(msg1, meta1); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(msg2, meta2); err != nil {
		t.Fatal(err)
	}

	// Query the SQL key column directly to verify uniqueness
	rows, err := store.db.Query("SELECT key FROM messages ORDER BY seq ASC")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
	}

	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	if keys[0] == keys[1] {
		t.Errorf("distinct messages produced identical SQL keys: %s", keys[0])
	}
}

// TestMessageKeyCollisionPrevention is a regression test for the truncated-hex
// key bug. Two messages sharing an identical 16-byte prefix must produce
// distinct keys under SHA-256 hashing.
func TestMessageKeyCollisionPrevention(t *testing.T) {
	store := createTestStore(t)

	logs := store.Logs()
	l, err := logs.Create("@bob.ed25519")
	if err != nil {
		t.Fatal(err)
	}

	// These two messages share the same first 30+ characters (the JSON prefix),
	// which would collide under the old truncated-hex scheme.
	msg1 := []byte(`{"type":"post","text":"aaa","seq":1}`)
	msg2 := []byte(`{"type":"post","text":"aaa","seq":2}`)
	meta1 := &Metadata{Author: "@bob.ed25519", Sequence: 1, Hash: "%h1"}
	meta2 := &Metadata{Author: "@bob.ed25519", Sequence: 2, Hash: "%h2"}

	if _, err := l.Append(msg1, meta1); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(msg2, meta2); err != nil {
		t.Fatal(err)
	}

	// Verify we have 2 distinct entries in messages_key_idx
	var count int
	err = store.db.QueryRow("SELECT COUNT(DISTINCT key) FROM messages_key_idx").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected 2 distinct keys in index, got %d (collision detected)", count)
	}
}

// TestMessageKeyIsSHA256 verifies the key column contains the full SHA-256 hex
// of the stored data, not a truncated prefix.
func TestMessageKeyIsSHA256(t *testing.T) {
	store := createTestStore(t)

	logs := store.Logs()
	l, err := logs.Create("@charlie.ed25519")
	if err != nil {
		t.Fatal(err)
	}

	content := []byte(`{"type":"post","text":"hash me"}`)
	meta := &Metadata{Author: "@charlie.ed25519", Sequence: 1, Hash: "%h1"}

	if _, err := l.Append(content, meta); err != nil {
		t.Fatal(err)
	}

	// Read back the key from SQL
	var sqlKey string
	err = store.db.QueryRow("SELECT key FROM messages WHERE seq = 1").Scan(&sqlKey)
	if err != nil {
		t.Fatal(err)
	}

	// The key should be 64 hex chars (32 bytes SHA-256)
	if len(sqlKey) != 64 {
		t.Errorf("expected 64-char hex key, got %d chars: %s", len(sqlKey), sqlKey)
	}

	// Recompute: the stored data is json.Marshal of storedMessageWrapper
	wrapper := &storedMessageWrapper{Content: content, Metadata: meta}
	data, _ := json.Marshal(wrapper)
	expected := fmt.Sprintf("%x", sha256.Sum256(data))
	if sqlKey != expected {
		t.Errorf("key mismatch:\n  got:    %s\n  expect: %s", sqlKey, expected)
	}
}
