package blobbridge

import (
	"context"
	"fmt"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/config"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
)

type GCConfig struct {
	MaxAgeDays    int
	MaxSizeGB     int64
	IntervalHours int
}

func DefaultGCConfig() GCConfig {
	return GCConfig{
		MaxAgeDays:    config.BlobMaxAgeDays,
		MaxSizeGB:     config.BlobMaxSizeGB * 1024 * 1024 * 1024,
		IntervalHours: config.BlobGCInterval,
	}
}

type GCMetrics struct {
	BlobsDeleted int64
	BytesFreed   int64
	Errors       int64
	LastRunAt    time.Time
}

func (b *Bridge) RunBlobGC(ctx context.Context, cfg GCConfig) (GCMetrics, error) {
	metrics := GCMetrics{
		LastRunAt: time.Now(),
	}

	oldBlobs, err := b.db.GetBlobsOlderThan(ctx, cfg.MaxAgeDays)
	if err != nil {
		metrics.Errors++
		return metrics, fmt.Errorf("get old blobs: %w", err)
	}

	for _, blob := range oldBlobs {
		if err := b.deleteBlob(ctx, blob); err != nil {
			metrics.Errors++
			continue
		}
		metrics.BlobsDeleted++
		metrics.BytesFreed += blob.Size
	}

	return metrics, nil
}

func (b *Bridge) deleteBlob(ctx context.Context, blob db.Blob) error {
	if err := b.db.DeleteBlob(ctx, blob.ATCID); err != nil {
		return err
	}
	return nil
}

func (b *Bridge) CheckBlobQuota(ctx context.Context, cfg GCConfig) (bool, error) {
	totalSize, err := b.db.CountBlobSize(ctx)
	if err != nil {
		return false, fmt.Errorf("count blob size: %w", err)
	}
	return totalSize > cfg.MaxSizeGB, nil
}
