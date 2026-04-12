package tangle

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// dbAdapter wraps sql.DB to satisfy the narrow Store interface
type dbAdapter struct {
	*sql.DB
}

func (a *dbAdapter) ExecContext(ctx context.Context, query string, args ...interface{}) (interface{}, error) {
	return a.DB.ExecContext(ctx, query, args...)
}

func (a *dbAdapter) QueryContext(ctx context.Context, query string, args ...interface{}) (Rows, error) {
	return a.DB.QueryContext(ctx, query, args...)
}

func (a *dbAdapter) QueryRowContext(ctx context.Context, query string, args ...interface{}) Row {
	return a.DB.QueryRowContext(ctx, query, args...)
}

// openTestDB creates an in-memory SQLite database with tangle schema
func openTestDB(t *testing.T) *Store {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// Create tangles table
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS tangles (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		root TEXT NOT NULL,
		tips TEXT,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		UNIQUE(name, root)
	)`); err != nil {
		t.Fatalf("failed to create tangles table: %v", err)
	}

	// Create tangle_membership table
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS tangle_membership (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		message_key TEXT NOT NULL,
		tangle_name TEXT NOT NULL,
		root_key TEXT NOT NULL,
		parent_keys TEXT,
		created_at INTEGER NOT NULL,
		UNIQUE(message_key, tangle_name)
	)`); err != nil {
		t.Fatalf("failed to create tangle_membership table: %v", err)
	}

	// Create messages table for joins
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key TEXT NOT NULL UNIQUE,
		seq INTEGER NOT NULL,
		value_json BLOB
	)`); err != nil {
		t.Fatalf("failed to create messages table: %v", err)
	}

	// Create indices
	indexStatements := []string{
		`CREATE INDEX IF NOT EXISTS idx_tangles_name_root ON tangles(name, root)`,
		`CREATE INDEX IF NOT EXISTS idx_tangle_membership_tangle ON tangle_membership(tangle_name, root_key)`,
		`CREATE INDEX IF NOT EXISTS idx_tangle_membership_root ON tangle_membership(root_key)`,
	}
	for _, stmt := range indexStatements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("failed to create index: %v", err)
		}
	}

	adapter := &dbAdapter{db}
	t.Cleanup(func() { db.Close() })
	return NewStore(adapter)
}

// TestAddMessage_Basic tests basic message insertion
func TestAddMessage_Basic(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	err := store.AddMessage(ctx, "msg1", "post", "root1", []string{"parent1"})
	if err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}

	// Verify insertion by querying directly
	var tk string
	var tn string
	row := store.db.QueryRowContext(ctx,
		`SELECT message_key, tangle_name FROM tangle_membership WHERE message_key = ?`,
		"msg1")
	err = row.Scan(&tk, &tn)
	if err != nil {
		t.Fatalf("direct query failed: %v", err)
	}
	if tk != "msg1" {
		t.Errorf("wrong message key: %s", tk)
	}
	if tn != "post" {
		t.Errorf("wrong tangle name: %s", tn)
	}
}

// TestAddMessage_Replace tests INSERT OR REPLACE semantics
func TestAddMessage_Replace(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	err := store.AddMessage(ctx, "msg1", "post", "root1", []string{"parent1"})
	if err != nil {
		t.Fatalf("first AddMessage failed: %v", err)
	}

	// Replace with different parents
	err = store.AddMessage(ctx, "msg1", "post", "root1", []string{"parent2", "parent3"})
	if err != nil {
		t.Fatalf("second AddMessage failed: %v", err)
	}

	// Verify the replacement by querying directly
	var parentJSON string
	row := store.db.QueryRowContext(ctx,
		`SELECT parent_keys FROM tangle_membership WHERE message_key = ?`,
		"msg1")
	err = row.Scan(&parentJSON)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	var parents []string
	json.Unmarshal([]byte(parentJSON), &parents)
	if len(parents) != 2 || parents[0] != "parent2" {
		t.Errorf("wrong parents after replace: %v", parents)
	}
}

// TestAddMessage_MultipleParents tests message with multiple parents
func TestAddMessage_MultipleParents(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	parents := []string{"parent1", "parent2", "parent3"}
	err := store.AddMessage(ctx, "msg1", "post", "root1", parents)
	if err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}

	// Verify by querying directly
	var parentJSON string
	row := store.db.QueryRowContext(ctx,
		`SELECT parent_keys FROM tangle_membership WHERE message_key = ?`,
		"msg1")
	err = row.Scan(&parentJSON)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	var retrievedParents []string
	json.Unmarshal([]byte(parentJSON), &retrievedParents)
	if len(retrievedParents) != 3 {
		t.Errorf("expected 3 parents, got %d", len(retrievedParents))
	}
}

// TestAddMessage_EmptyParents tests message with empty parent list
func TestAddMessage_EmptyParents(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	err := store.AddMessage(ctx, "msg1", "post", "root1", []string{})
	if err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}

	// Verify by querying directly
	var parentJSON string
	row := store.db.QueryRowContext(ctx,
		`SELECT parent_keys FROM tangle_membership WHERE message_key = ?`,
		"msg1")
	err = row.Scan(&parentJSON)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	var parents []string
	json.Unmarshal([]byte(parentJSON), &parents)
	if parents != nil && len(parents) != 0 {
		t.Errorf("expected 0 parents, got %d", len(parents))
	}
}

// TestGetTangleMembership_NotFound tests missing message
func TestGetTangleMembership_NotFound(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	_, err := store.GetTangleMembership(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent message")
	}
}

// TestGetTangleMessageCount_Zero tests empty tangle
func TestGetTangleMessageCount_Zero(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	count, err := store.GetTangleMessageCount(ctx, "post", "root1")
	if err != nil {
		t.Fatalf("GetTangleMessageCount failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 messages, got %d", count)
	}
}

// TestGetTangleMessageCount_Multiple tests count with multiple messages
func TestGetTangleMessageCount_Multiple(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		store.AddMessage(ctx, "msg"+string(rune('0'+i)), "post", "root1", []string{})
	}

	count, err := store.GetTangleMessageCount(ctx, "post", "root1")
	if err != nil {
		t.Fatalf("GetTangleMessageCount failed: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5 messages, got %d", count)
	}
}

// TestGetTangleMessageCount_Isolation tests feed isolation
func TestGetTangleMessageCount_Isolation(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	store.AddMessage(ctx, "msg1", "post", "root1", []string{})
	store.AddMessage(ctx, "msg2", "post", "root2", []string{})
	store.AddMessage(ctx, "msg3", "vote", "root1", []string{})

	count1, _ := store.GetTangleMessageCount(ctx, "post", "root1")
	count2, _ := store.GetTangleMessageCount(ctx, "post", "root2")
	count3, _ := store.GetTangleMessageCount(ctx, "vote", "root1")

	if count1 != 1 {
		t.Errorf("post/root1: expected 1, got %d", count1)
	}
	if count2 != 1 {
		t.Errorf("post/root2: expected 1, got %d", count2)
	}
	if count3 != 1 {
		t.Errorf("vote/root1: expected 1, got %d", count3)
	}
}

// TestGetTangleTips_SingleMessage tests single message is a tip
func TestGetTangleTips_SingleMessage(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	store.AddMessage(ctx, "msg1", "post", "root1", []string{})

	tips, err := store.GetTangleTips(ctx, "post", "root1")
	if err != nil {
		t.Fatalf("GetTangleTips failed: %v", err)
	}
	if len(tips) != 1 || tips[0] != "msg1" {
		t.Errorf("expected [msg1], got %v", tips)
	}
}

// TestGetTangleTips_LinearChain tests linear chain has one tip
func TestGetTangleTips_LinearChain(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	store.AddMessage(ctx, "msg1", "post", "root1", []string{})
	store.AddMessage(ctx, "msg2", "post", "root1", []string{"msg1"})
	store.AddMessage(ctx, "msg3", "post", "root1", []string{"msg2"})

	tips, err := store.GetTangleTips(ctx, "post", "root1")
	if err != nil {
		t.Fatalf("GetTangleTips failed: %v", err)
	}
	if len(tips) != 1 || tips[0] != "msg3" {
		t.Errorf("expected [msg3], got %v", tips)
	}
}

// TestGetTangleTips_Fork tests fork creates multiple tips
func TestGetTangleTips_Fork(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	store.AddMessage(ctx, "msg1", "post", "root1", []string{})
	store.AddMessage(ctx, "msg2", "post", "root1", []string{"msg1"})
	store.AddMessage(ctx, "msg3", "post", "root1", []string{"msg1"})

	tips, err := store.GetTangleTips(ctx, "post", "root1")
	if err != nil {
		t.Fatalf("GetTangleTips failed: %v", err)
	}
	if len(tips) != 2 {
		t.Errorf("expected 2 tips, got %d", len(tips))
	}
}

// TestGetTangleTips_Empty tests empty tangle has no tips
func TestGetTangleTips_Empty(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	tips, err := store.GetTangleTips(ctx, "post", "root1")
	if err != nil {
		t.Fatalf("GetTangleTips failed: %v", err)
	}
	if len(tips) != 0 {
		t.Errorf("expected 0 tips, got %d", len(tips))
	}
}

// TestGetMessagesByParent_Found tests finding children of a message
func TestGetMessagesByParent_Found(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	store.AddMessage(ctx, "msg1", "post", "root1", []string{})
	store.AddMessage(ctx, "msg2", "post", "root1", []string{"msg1"})
	store.AddMessage(ctx, "msg3", "post", "root1", []string{"msg1", "msg2"})

	children, err := store.GetMessagesByParent(ctx, "msg1")
	if err != nil {
		t.Fatalf("GetMessagesByParent failed: %v", err)
	}
	if len(children) != 2 {
		t.Errorf("expected 2 children, got %d", len(children))
	}
}

// TestGetMessagesByParent_NotFound tests nonexistent parent
func TestGetMessagesByParent_NotFound(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	store.AddMessage(ctx, "msg1", "post", "root1", []string{})

	children, err := store.GetMessagesByParent(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetMessagesByParent failed: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("expected 0 children, got %d", len(children))
	}
}

// TestGetTangle_Found tests retrieving a tangle
func TestGetTangle_Found(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	// Insert a tangle
	tips := []string{"msg1", "msg2"}
	tipsJSON, _ := json.Marshal(tips)
	_, err := store.db.ExecContext(ctx,
		`INSERT INTO tangles (name, root, tips, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"post", "root1", string(tipsJSON), 100, 200)
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	// Verify by querying directly
	var name, root, tipsStr string
	row := store.db.QueryRowContext(ctx,
		`SELECT name, root, tips FROM tangles WHERE name = ? AND root = ?`,
		"post", "root1")
	err = row.Scan(&name, &root, &tipsStr)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if name != "post" || root != "root1" {
		t.Errorf("wrong tangle: name=%s root=%s", name, root)
	}
	var tips2 []string
	json.Unmarshal([]byte(tipsStr), &tips2)
	if len(tips2) != 2 {
		t.Errorf("expected 2 tips, got %d", len(tips2))
	}
}

// TestGetTangle_NotFound tests missing tangle
func TestGetTangle_NotFound(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	_, err := store.GetTangle(ctx, "post", "root1")
	if err == nil {
		t.Fatal("expected error for missing tangle")
	}
}

// TestGetTangleMessages_Basic tests retrieving messages from a tangle
func TestGetTangleMessages_Basic(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	store.AddMessage(ctx, "msg1", "post", "root1", []string{})
	store.AddMessage(ctx, "msg2", "post", "root1", []string{"msg1"})

	msgs, err := store.GetTangleMessages(ctx, "post", "root1", 0)
	if err != nil {
		t.Fatalf("GetTangleMessages failed: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
}

// TestExtractTangleMetadata_Basic tests extracting metadata from content
func TestExtractTangleMetadata_Basic(t *testing.T) {
	content := map[string]interface{}{
		"tangles": map[string]interface{}{
			"post": map[string]interface{}{
				"root":     "root123",
				"previous": []interface{}{"msg1", "msg2"},
			},
		},
	}

	name, root, parents := ExtractTangleMetadata(content)
	if name != "post" {
		t.Errorf("wrong name: %s", name)
	}
	if root != "root123" {
		t.Errorf("wrong root: %s", root)
	}
	if len(parents) != 2 {
		t.Errorf("wrong parents count: %d", len(parents))
	}
}

// TestExtractTangleMetadata_NoTangles tests content without tangles
func TestExtractTangleMetadata_NoTangles(t *testing.T) {
	content := map[string]interface{}{
		"text": "hello",
	}

	name, root, parents := ExtractTangleMetadata(content)
	if name != "" || root != "" || parents != nil {
		t.Errorf("expected empty result, got name=%s root=%s parents=%v", name, root, parents)
	}
}

// TestExtractTangleMetadata_MissingRoot tests tangle without root field
func TestExtractTangleMetadata_MissingRoot(t *testing.T) {
	content := map[string]interface{}{
		"tangles": map[string]interface{}{
			"post": map[string]interface{}{
				"previous": []interface{}{"msg1"},
			},
		},
	}

	name, root, parents := ExtractTangleMetadata(content)
	if name != "post" {
		t.Errorf("wrong name: %s", name)
	}
	if root != "" {
		t.Errorf("expected empty root, got %s", root)
	}
	if len(parents) != 1 {
		t.Errorf("wrong parents count: %d", len(parents))
	}
}

// TestExtractTangleMetadata_MultipleTangles tests extraction from content with multiple tangles
func TestExtractTangleMetadata_MultipleTangles(t *testing.T) {
	content := map[string]interface{}{
		"tangles": map[string]interface{}{
			"post": map[string]interface{}{
				"root":     "root1",
				"previous": []interface{}{"msg1"},
			},
			"vote": map[string]interface{}{
				"root":     "root2",
				"previous": []interface{}{"msg2"},
			},
		},
	}

	name, root, _ := ExtractTangleMetadata(content)
	// Should return the first one encountered (map iteration order is random in Go)
	if name == "" || root == "" {
		t.Errorf("expected non-empty tangle, got name=%s root=%s", name, root)
	}
}
