package repo

import (
	"bytes"
	"context"
	"testing"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
)

// TestReadRepoFromCar tests reading a repo from a CAR file.
func TestReadRepoFromCar(t *testing.T) {
	// Create a simple repo
	bs := &memBlockstore{blocks: map[string]blocks.Block{}}
	writeRepo := NewRepo("did:plc:test123", bs)

	// Add a record
	testRecord := map[string]interface{}{
		"text":      "hello world",
		"createdAt": "2024-01-01T00:00:00Z",
	}
	_, _, err := writeRepo.CreateRecord(context.Background(), "app.bsky.feed.post", testRecord)
	if err != nil {
		t.Fatalf("CreateRecord failed: %v", err)
	}

	// Commit the repo
	_, _, err = writeRepo.Commit(context.Background(), nil)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Write to CAR
	var carBuf bytes.Buffer
	err = writeRepo.WriteCAR(&carBuf)
	if err != nil {
		t.Fatalf("WriteCAR failed: %v", err)
	}

	// Read from CAR
	repo, err := ReadRepoFromCar(context.Background(), &carBuf)
	if err != nil {
		t.Fatalf("ReadRepoFromCar failed: %v", err)
	}

	if repo.RepoDid() != "did:plc:test123" {
		t.Errorf("wrong DID: %s", repo.RepoDid())
	}
}

// TestGetRecordBytes tests retrieving a record by path.
func TestGetRecordBytes(t *testing.T) {
	writeRepo := NewRepo("did:plc:test456", &memBlockstore{blocks: map[string]blocks.Block{}})

	testRecord := map[string]interface{}{"text": "test message"}
	recordCID, recordPath, err := writeRepo.CreateRecord(context.Background(), "app.bsky.feed.post", testRecord)
	if err != nil {
		t.Fatalf("CreateRecord failed: %v", err)
	}

	_, _, err = writeRepo.Commit(context.Background(), nil)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Write and read back
	var carBuf bytes.Buffer
	writeRepo.WriteCAR(&carBuf)

	repo, err := ReadRepoFromCar(context.Background(), &carBuf)
	if err != nil {
		t.Fatalf("ReadRepoFromCar failed: %v", err)
	}

	retrievedCID, data, err := repo.GetRecordBytes(context.Background(), recordPath)
	if err != nil {
		t.Fatalf("GetRecordBytes failed: %v", err)
	}

	if retrievedCID != recordCID {
		t.Errorf("CID mismatch: got %s, want %s", retrievedCID, recordCID)
	}
	if data == nil || len(*data) == 0 {
		t.Error("record data is empty")
	}
}

// TestGetRecordBytesNotFound tests error when record doesn't exist.
func TestGetRecordBytesNotFound(t *testing.T) {
	writeRepo := NewRepo("did:plc:test789", &memBlockstore{blocks: map[string]blocks.Block{}})
	_, _, err := writeRepo.Commit(context.Background(), nil)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	var carBuf bytes.Buffer
	writeRepo.WriteCAR(&carBuf)

	repo, _ := ReadRepoFromCar(context.Background(), &carBuf)
	_, _, err = repo.GetRecordBytes(context.Background(), "app.bsky.feed.post/0000000000000001")
	if err == nil {
		t.Error("expected error for non-existent record")
	}
}

// TestForEachIteration tests iterating over records.
func TestForEachIteration(t *testing.T) {
	writeRepo := NewRepo("did:plc:test999", &memBlockstore{blocks: map[string]blocks.Block{}})

	// Add multiple records
	for i := 0; i < 3; i++ {
		testRecord := map[string]interface{}{"index": i}
		_, _, err := writeRepo.CreateRecord(context.Background(), "app.bsky.feed.post", testRecord)
		if err != nil {
			t.Fatalf("CreateRecord failed: %v", err)
		}
	}

	_, _, err := writeRepo.Commit(context.Background(), nil)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	var carBuf bytes.Buffer
	writeRepo.WriteCAR(&carBuf)

	repo, _ := ReadRepoFromCar(context.Background(), &carBuf)

	count := 0
	err = repo.ForEach(context.Background(), "", func(k string, v cid.Cid) error {
		count++
		// Verify key and CID are valid
		if k == "" || v == cid.Undef {
			t.Error("invalid record in iteration")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ForEach failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 records, got %d", count)
	}
}

// TestForEachWithPrefix tests ForEach with a prefix filter.
func TestForEachWithPrefix(t *testing.T) {
	writeRepo := NewRepo("did:plc:prefix123", &memBlockstore{blocks: map[string]blocks.Block{}})

	// Add records with different paths
	for i := 0; i < 2; i++ {
		writeRepo.CreateRecord(context.Background(), "app.bsky.feed.post", map[string]interface{}{})
		writeRepo.CreateRecord(context.Background(), "app.bsky.actor.profile", map[string]interface{}{})
	}

	writeRepo.Commit(context.Background(), nil)

	var carBuf bytes.Buffer
	writeRepo.WriteCAR(&carBuf)

	repo, _ := ReadRepoFromCar(context.Background(), &carBuf)

	count := 0
	repo.ForEach(context.Background(), "app.bsky.feed", func(k string, v cid.Cid) error {
		count++
		if !bytes.HasPrefix([]byte(k), []byte("app.bsky.feed")) {
			t.Errorf("wrong prefix: %s", k)
		}
		return nil
	})

	if count == 0 {
		t.Error("no records with prefix found")
	}
}

// TestForEachContextCancellation tests ForEach respects context cancellation.
func TestForEachContextCancellation(t *testing.T) {
	writeRepo := NewRepo("did:plc:cancel123", &memBlockstore{blocks: map[string]blocks.Block{}})

	for i := 0; i < 5; i++ {
		writeRepo.CreateRecord(context.Background(), "app.bsky.feed.post", map[string]interface{}{})
	}

	writeRepo.Commit(context.Background(), nil)

	var carBuf bytes.Buffer
	writeRepo.WriteCAR(&carBuf)

	repo, _ := ReadRepoFromCar(context.Background(), &carBuf)

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := repo.ForEach(ctx, "", func(k string, v cid.Cid) error {
		return nil
	})
	if err == nil {
		t.Error("expected context error")
	}
}

// TestCreateRecord tests record creation.
func TestCreateRecord(t *testing.T) {
	writeRepo := NewRepo("did:plc:create123", &memBlockstore{blocks: map[string]blocks.Block{}})

	testRecord := map[string]interface{}{
		"text":      "hello",
		"createdAt": "2024-01-01T00:00:00Z",
	}

	recordCID, recordPath, err := writeRepo.CreateRecord(context.Background(), "app.bsky.feed.post", testRecord)
	if err != nil {
		t.Fatalf("CreateRecord failed: %v", err)
	}

	if recordCID == cid.Undef {
		t.Error("invalid record CID")
	}
	if recordPath == "" {
		t.Error("empty record path")
	}
	if !bytes.HasPrefix([]byte(recordPath), []byte("app.bsky.feed.post/")) {
		t.Errorf("wrong path prefix: %s", recordPath)
	}
}

// TestWriteRepoCommit tests committing a repo.
func TestWriteRepoCommit(t *testing.T) {
	writeRepo := NewRepo("did:plc:commit123", &memBlockstore{blocks: map[string]blocks.Block{}})

	writeRepo.CreateRecord(context.Background(), "app.bsky.feed.post", map[string]interface{}{})

	commitCID, rev, err := writeRepo.Commit(context.Background(), nil)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	if commitCID == cid.Undef {
		t.Error("invalid commit CID")
	}
	// rev can be empty for unsigned commits
	_ = rev
}

// TestWriteRepoCAR tests writing repo to CAR format.
func TestWriteRepoCAR(t *testing.T) {
	writeRepo := NewRepo("did:plc:car123", &memBlockstore{blocks: map[string]blocks.Block{}})

	writeRepo.CreateRecord(context.Background(), "app.bsky.feed.post", map[string]interface{}{})
	writeRepo.Commit(context.Background(), nil)

	var buf bytes.Buffer
	err := writeRepo.WriteCAR(&buf)
	if err != nil {
		t.Fatalf("WriteCAR failed: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("CAR output is empty")
	}

	// Verify it can be read back
	repo, err := ReadRepoFromCar(context.Background(), &buf)
	if err != nil {
		t.Fatalf("read back failed: %v", err)
	}
	if repo.RepoDid() != "did:plc:car123" {
		t.Errorf("DID mismatch after round-trip: %s", repo.RepoDid())
	}
}

// TestEmptyRepo tests creating and committing an empty repo.
func TestEmptyRepo(t *testing.T) {
	writeRepo := NewRepo("did:plc:empty123", &memBlockstore{blocks: map[string]blocks.Block{}})

	_, _, err := writeRepo.Commit(context.Background(), nil)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	var carBuf bytes.Buffer
	writeRepo.WriteCAR(&carBuf)

	repo, err := ReadRepoFromCar(context.Background(), &carBuf)
	if err != nil {
		t.Fatalf("ReadRepoFromCar failed: %v", err)
	}

	// Empty repo should have zero records
	count := 0
	repo.ForEach(context.Background(), "", func(k string, v cid.Cid) error {
		count++
		return nil
	})
	if count != 0 {
		t.Errorf("expected 0 records in empty repo, got %d", count)
	}
}

// TestRepoDid tests RepoDid accessor.
func TestRepoDid(t *testing.T) {
	did := "did:plc:specific123"
	writeRepo := NewRepo(did, &memBlockstore{blocks: map[string]blocks.Block{}})

	_, _, err := writeRepo.Commit(context.Background(), nil)
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	var carBuf bytes.Buffer
	writeRepo.WriteCAR(&carBuf)

	repo, _ := ReadRepoFromCar(context.Background(), &carBuf)
	if repo.RepoDid() != did {
		t.Errorf("RepoDid: got %s, want %s", repo.RepoDid(), did)
	}
}

// TestSignedCommit tests SignedCommit accessor.
func TestSignedCommit(t *testing.T) {
	writeRepo := NewRepo("did:plc:commit123", &memBlockstore{blocks: map[string]blocks.Block{}})

	writeRepo.CreateRecord(context.Background(), "app.bsky.feed.post", map[string]interface{}{})
	writeRepo.Commit(context.Background(), nil)

	var carBuf bytes.Buffer
	writeRepo.WriteCAR(&carBuf)

	repo, _ := ReadRepoFromCar(context.Background(), &carBuf)
	commit := repo.SignedCommit()

	if commit.Did != "did:plc:commit123" {
		t.Errorf("commit DID: got %s, want %s", commit.Did, "did:plc:commit123")
	}
	if commit.Version != 3 {
		t.Errorf("commit version: got %d, want 3", commit.Version)
	}
}

// TestGenerateRkey tests record key generation.
func TestGenerateRkey(t *testing.T) {
	writeRepo := NewRepo("did:plc:rkey123", &memBlockstore{blocks: map[string]blocks.Block{}})

	keys := make(map[string]bool)
	for i := 0; i < 5; i++ {
		_, path, _ := writeRepo.CreateRecord(context.Background(), "app.bsky.feed.post", map[string]interface{}{})
		keys[path] = true
	}

	if len(keys) != 5 {
		t.Errorf("expected 5 unique keys, got %d", len(keys))
	}

	for key := range keys {
		if len(key) == 0 {
			t.Error("empty rkey generated")
		}
	}
}

// TestMultipleRecordTypes tests creating different record types.
func TestMultipleRecordTypes(t *testing.T) {
	writeRepo := NewRepo("did:plc:multi123", &memBlockstore{blocks: map[string]blocks.Block{}})

	types := []string{
		"app.bsky.feed.post",
		"app.bsky.actor.profile",
		"app.bsky.feed.like",
	}

	for _, recType := range types {
		_, _, err := writeRepo.CreateRecord(context.Background(), recType, map[string]interface{}{})
		if err != nil {
			t.Errorf("CreateRecord failed for %s: %v", recType, err)
		}
	}

	writeRepo.Commit(context.Background(), nil)

	var carBuf bytes.Buffer
	writeRepo.WriteCAR(&carBuf)

	repo, _ := ReadRepoFromCar(context.Background(), &carBuf)

	recordTypes := make(map[string]bool)
	repo.ForEach(context.Background(), "", func(k string, v cid.Cid) error {
		// Extract type from path (format: "type/rkey")
		parts := bytes.Split([]byte(k), []byte("/"))
		if len(parts) > 0 {
			recordTypes[string(parts[0])] = true
		}
		return nil
	})

	if len(recordTypes) != len(types) {
		t.Errorf("expected %d record types, got %d", len(types), len(recordTypes))
	}
}

// BenchmarkCreateRecord benchmarks record creation.
func BenchmarkCreateRecord(b *testing.B) {
	writeRepo := NewRepo("did:plc:bench123", &memBlockstore{blocks: map[string]blocks.Block{}})
	record := map[string]interface{}{"text": "test"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		writeRepo.CreateRecord(context.Background(), "app.bsky.feed.post", record)
	}
}

// BenchmarkCommit benchmarks commit operation.
func BenchmarkCommit(b *testing.B) {
	writeRepo := NewRepo("did:plc:bench456", &memBlockstore{blocks: map[string]blocks.Block{}})
	writeRepo.CreateRecord(context.Background(), "app.bsky.feed.post", map[string]interface{}{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		writeRepo.Commit(context.Background(), nil)
	}
}

// BenchmarkForEach benchmarks iteration.
func BenchmarkForEach(b *testing.B) {
	writeRepo := NewRepo("did:plc:bench789", &memBlockstore{blocks: map[string]blocks.Block{}})
	for i := 0; i < 100; i++ {
		writeRepo.CreateRecord(context.Background(), "app.bsky.feed.post", map[string]interface{}{})
	}
	writeRepo.Commit(context.Background(), nil)

	var carBuf bytes.Buffer
	writeRepo.WriteCAR(&carBuf)
	repo, _ := ReadRepoFromCar(context.Background(), &carBuf)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		repo.ForEach(context.Background(), "", func(k string, v cid.Cid) error {
			return nil
		})
	}
}
