package sqlite

import (
	"database/sql"
	"encoding/json"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// openTestDB creates an in-memory SQLite database for testing
func openTestDB(t *testing.T) (*sql.DB, int64) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// Create the feeds table first (for foreign key constraint)
	feedSchema := `
	CREATE TABLE IF NOT EXISTS feeds (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		created_at INTEGER NOT NULL
	);
	`
	if _, err := db.Exec(feedSchema); err != nil {
		t.Fatalf("failed to create feeds schema: %v", err)
	}

	// Insert a test feed
	result, err := db.Exec("INSERT INTO feeds (name, created_at) VALUES (?, ?)", "test-feed", 0)
	if err != nil {
		t.Fatalf("failed to insert test feed: %v", err)
	}
	feedID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("failed to get feed ID: %v", err)
	}

	// Create the messages table
	schema := `
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		feed_id INTEGER NOT NULL,
		seq INTEGER NOT NULL,
		key TEXT NOT NULL,
		value_json BLOB NOT NULL,
		created_at INTEGER NOT NULL,
		FOREIGN KEY (feed_id) REFERENCES feeds(id),
		UNIQUE(feed_id, seq)
	);

	CREATE INDEX IF NOT EXISTS idx_messages_key ON messages(key);
	CREATE INDEX IF NOT EXISTS idx_messages_feed_seq ON messages(feed_id, seq);

	CREATE TABLE IF NOT EXISTS messages_key_idx (
		key TEXT UNIQUE NOT NULL,
		feed_id INTEGER NOT NULL,
		seq INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_messages_key_idx ON messages_key_idx(key);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	return db, feedID
}

// TestLogAppendRoundTrip tests that a message can be stored and retrieved
func TestLogAppendRoundTrip(t *testing.T) {
	db, feedID := openTestDB(t)
	defer db.Close()

	log := &Log{
		db:     db,
		feedID: feedID,
	}

	testMessage := map[string]interface{}{
		"author":    "@alice",
		"content":   "hello world",
		"timestamp": 1234567890,
	}
	data, _ := json.Marshal(testMessage)

	// Append message
	seq, err := log.Append(data)
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Retrieve message
	retrieved, err := log.Get(seq)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved == nil {
		t.Error("retrieved message is nil")
	}
}

// TestLogAppendMultipleMessages tests appending multiple messages
func TestLogAppendMultipleMessages(t *testing.T) {
	db, feedID := openTestDB(t)
	defer db.Close()

	log := &Log{
		db:     db,
		feedID: feedID,
	}

	// Append 3 messages
	for i := 1; i <= 3; i++ {
		msg := map[string]interface{}{
			"index": i,
		}
		data, _ := json.Marshal(msg)

		seq, err := log.Append(data)
		if err != nil {
			t.Fatalf("Append failed for message %d: %v", i, err)
		}

		if seq != int64(i) {
			t.Errorf("expected seq %d, got %d", i, seq)
		}
	}

	// Verify all messages exist
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM messages WHERE feed_id = ?", feedID).Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 messages, got %d", count)
	}
}

// TestLogAppendDifferentFeeds tests that feeds are independent
func TestLogAppendDifferentFeeds(t *testing.T) {
	db, feedID1 := openTestDB(t)
	defer db.Close()

	// Create a second feed
	result, err := db.Exec("INSERT INTO feeds (name, created_at) VALUES (?, ?)", "test-feed-2", 0)
	if err != nil {
		t.Fatalf("failed to insert second feed: %v", err)
	}
	feedID2, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("failed to get second feed ID: %v", err)
	}

	log1 := &Log{db: db, feedID: feedID1}
	log2 := &Log{db: db, feedID: feedID2}

	msg1 := map[string]interface{}{"feed": "1"}
	msg2 := map[string]interface{}{"feed": "2"}

	data1, _ := json.Marshal(msg1)
	data2, _ := json.Marshal(msg2)

	seq1, err := log1.Append(data1)
	if err != nil {
		t.Fatalf("log1.Append failed: %v", err)
	}

	seq2, err := log2.Append(data2)
	if err != nil {
		t.Fatalf("log2.Append failed: %v", err)
	}

	// Both should have seq 1 (sequences are per-feed)
	if seq1 != 1 || seq2 != 1 {
		t.Errorf("expected both seqs to be 1, got %d and %d", seq1, seq2)
	}

	// Verify we have 2 messages total
	var count int
	db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 messages total, got %d", count)
	}
}

// TestLogAppendSeqIncrement tests that sequence numbers increment correctly
func TestLogAppendSeqIncrement(t *testing.T) {
	db, feedID := openTestDB(t)
	defer db.Close()

	log := &Log{db: db, feedID: feedID}

	for i := 1; i <= 5; i++ {
		msg := map[string]interface{}{"seq_test": i}
		data, _ := json.Marshal(msg)

		seq, err := log.Append(data)
		if err != nil {
			t.Fatalf("Append %d failed: %v", i, err)
		}

		if seq != int64(i) {
			t.Errorf("seq %d: expected %d, got %d", i, i, seq)
		}
	}
}

// TestLogGetNotFound tests Get returns error for nonexistent sequence
func TestLogGetNotFound(t *testing.T) {
	db, feedID := openTestDB(t)
	defer db.Close()

	log := &Log{db: db, feedID: feedID}

	// Try to get nonexistent message
	_, err := log.Get(999)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TestLogGetFromDifferentFeed tests that Get only returns from current feed
func TestLogGetFromDifferentFeed(t *testing.T) {
	db, feedID1 := openTestDB(t)
	defer db.Close()

	// Create a second feed
	result, err := db.Exec("INSERT INTO feeds (name, created_at) VALUES (?, ?)", "test-feed-2", 0)
	if err != nil {
		t.Fatalf("failed to insert second feed: %v", err)
	}
	feedID2, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("failed to get second feed ID: %v", err)
	}

	log1 := &Log{db: db, feedID: feedID1}
	log2 := &Log{db: db, feedID: feedID2}

	msg1 := map[string]interface{}{"feed": "1"}
	data1, _ := json.Marshal(msg1)

	// Append to log1
	seq, err := log1.Append(data1)
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Try to get from log2 (should fail)
	_, err = log2.Get(seq)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound when getting from different feed, got %v", err)
	}

	// Should succeed from log1
	_, err = log1.Get(seq)
	if err != nil {
		t.Errorf("Get from same feed failed: %v", err)
	}
}

// TestLogMessagesKeyIndexIntegrity tests that messages_key_idx stays in sync
func TestLogMessagesKeyIndexIntegrity(t *testing.T) {
	db, feedID := openTestDB(t)
	defer db.Close()

	log := &Log{db: db, feedID: feedID}

	// Append some messages
	for i := 1; i <= 3; i++ {
		msg := map[string]interface{}{"index": i}
		data, _ := json.Marshal(msg)
		_, err := log.Append(data)
		if err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	// Verify messages_key_idx has the same count as messages
	var msgCount, idxCount int
	db.QueryRow("SELECT COUNT(*) FROM messages WHERE feed_id = ?", feedID).Scan(&msgCount)
	db.QueryRow("SELECT COUNT(*) FROM messages_key_idx WHERE feed_id = ?", feedID).Scan(&idxCount)

	if msgCount != idxCount {
		t.Errorf("message count mismatch: messages=%d, idx=%d", msgCount, idxCount)
	}
}

// TestLogAppendEmptyData tests appending empty JSON
func TestLogAppendEmptyData(t *testing.T) {
	db, feedID := openTestDB(t)
	defer db.Close()

	log := &Log{db: db, feedID: feedID}

	data := []byte("{}")

	seq, err := log.Append(data)
	if err != nil {
		t.Fatalf("Append empty failed: %v", err)
	}

	if seq != 1 {
		t.Errorf("expected seq 1, got %d", seq)
	}

	// Verify we can retrieve it
	retrieved, err := log.Get(seq)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved == nil {
		t.Error("retrieved message is nil")
	}
}

// TestLogAppendLargeData tests appending large JSON data
func TestLogAppendLargeData(t *testing.T) {
	db, feedID := openTestDB(t)
	defer db.Close()

	log := &Log{db: db, feedID: feedID}

	// Create a large message
	largeMsg := map[string]interface{}{
		"text": string(make([]byte, 10000)), // 10KB text field
	}
	data, _ := json.Marshal(largeMsg)

	seq, err := log.Append(data)
	if err != nil {
		t.Fatalf("Append large failed: %v", err)
	}

	// Verify we can retrieve it
	retrieved, err := log.Get(seq)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved == nil {
		t.Error("retrieved message is nil")
	}
}

// TestLogAppendUnicode tests appending messages with unicode characters
func TestLogAppendUnicode(t *testing.T) {
	db, feedID := openTestDB(t)
	defer db.Close()

	log := &Log{db: db, feedID: feedID}

	msg := map[string]interface{}{
		"text": "Hello 世界 🌍 مرحبا",
	}
	data, _ := json.Marshal(msg)

	seq, err := log.Append(data)
	if err != nil {
		t.Fatalf("Append unicode failed: %v", err)
	}

	retrieved, err := log.Get(seq)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved == nil {
		t.Error("retrieved message is nil")
	}
}

// BenchmarkLogAppend benchmarks message append operations
func BenchmarkLogAppend(b *testing.B) {
	db, feedID := openTestDB(&testing.T{})
	defer db.Close()

	log := &Log{db: db, feedID: feedID}

	msg := map[string]interface{}{
		"text": "benchmark message",
	}
	data, _ := json.Marshal(msg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		log.Append(data)
	}
}

// BenchmarkLogGet benchmarks message retrieval
func BenchmarkLogGet(b *testing.B) {
	db, feedID := openTestDB(&testing.T{})
	defer db.Close()

	log := &Log{db: db, feedID: feedID}

	msg := map[string]interface{}{"text": "test"}
	data, _ := json.Marshal(msg)

	log.Append(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		log.Get(1)
	}
}
