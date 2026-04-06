package blobbridge

import (
	"context"
	"database/sql"
	"io"
	"log"
	"path/filepath"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
)

func seedOldBlob(t *testing.T, rawDB *sql.DB, atCID, ssbRef string, size int64, daysAgo int) {
	t.Helper()
	_, err := rawDB.Exec(
		`INSERT INTO blobs (at_cid, ssb_blob_ref, size, mime_type, downloaded_at)
		 VALUES (?, ?, ?, 'image/png', datetime('now', '-' || ? || ' days'))`,
		atCID, ssbRef, size, daysAgo,
	)
	if err != nil {
		t.Fatalf("insert old blob: %v", err)
	}
}

func TestRunBlobGCDeletesOldBlobs(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gc-test.sqlite")

	// Open raw to seed old blobs.
	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	// Run schema.
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	database.Close()

	seedOldBlob(t, rawDB, "bafy-old-a", "&old-a", 100, 30)
	seedOldBlob(t, rawDB, "bafy-old-b", "&old-b", 100, 30)
	seedOldBlob(t, rawDB, "bafy-old-c", "&old-c", 100, 30)
	rawDB.Close()

	database, err = db.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	bridge := New(database, newTestBlobStore(), nil, log.New(io.Discard, "", 0))

	metrics, err := bridge.RunBlobGC(ctx, GCConfig{MaxAgeDays: 7})
	if err != nil {
		t.Fatalf("RunBlobGC: %v", err)
	}
	if metrics.BlobsDeleted != 3 {
		t.Errorf("expected 3 blobs deleted, got %d", metrics.BlobsDeleted)
	}
	if metrics.BytesFreed != 300 {
		t.Errorf("expected 300 bytes freed, got %d", metrics.BytesFreed)
	}
	if metrics.Errors != 0 {
		t.Errorf("expected 0 errors, got %d", metrics.Errors)
	}
}

func TestRunBlobGCSkipsNewBlobs(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	if err := database.AddBlob(ctx, db.Blob{
		ATCID:      "bafy-recent",
		SSBBlobRef: "&recent",
		Size:       50,
		MimeType:   "image/png",
	}); err != nil {
		t.Fatalf("add blob: %v", err)
	}

	bridge := New(database, newTestBlobStore(), nil, log.New(io.Discard, "", 0))

	metrics, err := bridge.RunBlobGC(ctx, GCConfig{MaxAgeDays: 7})
	if err != nil {
		t.Fatalf("RunBlobGC: %v", err)
	}
	if metrics.BlobsDeleted != 0 {
		t.Errorf("expected 0 blobs deleted, got %d", metrics.BlobsDeleted)
	}
}

func TestRunBlobGCErrorOnGetOldBlobs(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	database.Close()

	ctx := context.Background()
	bridge := New(database, newTestBlobStore(), nil, log.New(io.Discard, "", 0))

	_, err = bridge.RunBlobGC(ctx, GCConfig{MaxAgeDays: 7})
	if err == nil {
		t.Fatal("expected error from closed DB")
	}
}

func TestCheckBlobQuotaUnderLimit(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	if err := database.AddBlob(ctx, db.Blob{
		ATCID:      "bafy-small",
		SSBBlobRef: "&small",
		Size:       100,
	}); err != nil {
		t.Fatalf("add blob: %v", err)
	}

	bridge := New(database, newTestBlobStore(), nil, log.New(io.Discard, "", 0))

	exceeded, err := bridge.CheckBlobQuota(ctx, GCConfig{MaxSizeGB: 1024 * 1024 * 1024})
	if err != nil {
		t.Fatalf("CheckBlobQuota: %v", err)
	}
	if exceeded {
		t.Error("expected quota not exceeded for small blob")
	}
}

func TestCheckBlobQuotaExceedsLimit(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	if err := database.AddBlob(ctx, db.Blob{
		ATCID:      "bafy-quota",
		SSBBlobRef: "&quota",
		Size:       1000,
	}); err != nil {
		t.Fatalf("add blob: %v", err)
	}

	bridge := New(database, newTestBlobStore(), nil, log.New(io.Discard, "", 0))

	exceeded, err := bridge.CheckBlobQuota(ctx, GCConfig{MaxSizeGB: 0})
	if err != nil {
		t.Fatalf("CheckBlobQuota: %v", err)
	}
	if !exceeded {
		t.Error("expected quota exceeded")
	}
}

func TestDefaultGCConfig(t *testing.T) {
	cfg := DefaultGCConfig()
	if cfg.MaxAgeDays <= 0 {
		t.Error("expected positive MaxAgeDays")
	}
	if cfg.MaxSizeGB <= 0 {
		t.Error("expected positive MaxSizeGB")
	}
	if cfg.IntervalHours <= 0 {
		t.Error("expected positive IntervalHours")
	}
}

func TestGetBlobsOlderThan(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gc-older.sqlite")

	rawDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	database.Close()

	seedOldBlob(t, rawDB, "bafy-old-gc", "&old-gc", 50, 10)
	rawDB.Close()

	database, err = db.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer database.Close()

	// Also add a new blob.
	ctx := context.Background()
	if err := database.AddBlob(ctx, db.Blob{
		ATCID:      "bafy-new-gc",
		SSBBlobRef: "&new-gc",
		Size:       50,
		MimeType:   "image/png",
	}); err != nil {
		t.Fatalf("add new blob: %v", err)
	}

	blobs, err := database.GetBlobsOlderThan(ctx, 7)
	if err != nil {
		t.Fatalf("GetBlobsOlderThan: %v", err)
	}
	if len(blobs) != 1 {
		t.Fatalf("expected 1 old blob, got %d", len(blobs))
	}
	if blobs[0].ATCID != "bafy-old-gc" {
		t.Errorf("expected bafy-old-gc, got %q", blobs[0].ATCID)
	}
}

func TestDeleteBlob(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	if err := database.AddBlob(ctx, db.Blob{
		ATCID:      "bafy-to-delete",
		SSBBlobRef: "&to-delete",
		Size:       10,
	}); err != nil {
		t.Fatalf("add blob: %v", err)
	}

	if err := database.DeleteBlob(ctx, "bafy-to-delete"); err != nil {
		t.Fatalf("DeleteBlob: %v", err)
	}

	blob, err := database.GetBlob(ctx, "bafy-to-delete")
	if err != nil {
		t.Fatalf("GetBlob after delete: %v", err)
	}
	if blob != nil {
		t.Error("expected blob to be deleted")
	}
}

func TestCountBlobSize(t *testing.T) {
	database, err := db.Open(":memory:?parseTime=true")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	total, err := database.CountBlobSize(ctx)
	if err != nil {
		t.Fatalf("CountBlobSize empty: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0, got %d", total)
	}

	for _, size := range []int64{100, 200, 300} {
		if err := database.AddBlob(ctx, db.Blob{
			ATCID: "bafy-size-" + string(rune(size)),
			Size:  size,
		}); err != nil {
			t.Fatalf("add blob: %v", err)
		}
	}

	total, err = database.CountBlobSize(ctx)
	if err != nil {
		t.Fatalf("CountBlobSize: %v", err)
	}
	if total != 600 {
		t.Errorf("expected 600, got %d", total)
	}
}
