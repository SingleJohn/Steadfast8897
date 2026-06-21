package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
)

const (
	sourceItemsGCRetentionKey = "source_items_gc_retention_days"
	sourceItemsGCIntervalKey  = "source_items_gc_interval_hours"
	sourceItemsGCBatchKey     = "source_items_gc_batch_size"

	sourceItemsGCRetentionDays = 90
	sourceItemsGCIntervalHours = 24
	sourceItemsGCBatchSize     = 500
)

type sourceItemsGCConfig struct {
	Retention time.Duration
	Interval  time.Duration
	BatchSize int64
}

func StartSourceItemsGC(ctx context.Context, pool *pgxpool.Pool, repo *repository.SourceRepository) {
	if pool == nil || repo == nil {
		return
	}
	logger := slog.With("log_target", "source", "component", "source_items_gc")
	go func() {
		logger.Info("[SourceGC] started")
		for {
			cfg := readSourceItemsGCConfig(ctx, pool)
			runSourceItemsGCTick(ctx, repo, cfg, logger)

			timer := time.NewTimer(cfg.Interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func runSourceItemsGCTick(ctx context.Context, repo *repository.SourceRepository, cfg sourceItemsGCConfig, logger *slog.Logger) {
	start := time.Now()
	cutoff := start.Add(-cfg.Retention)
	deleted, err := repo.DeleteExpiredSourceItems(ctx, cutoff, cfg.BatchSize)
	if err != nil {
		logger.Warn("[SourceGC] sweep failed",
			"action", "source_items_gc",
			"status", "error",
			"error_type", errorType(err),
			"retention_days", int(cfg.Retention.Hours()/24),
			"batch_size", cfg.BatchSize,
			"latency_ms", time.Since(start).Milliseconds(),
			"error", err)
		return
	}
	logger.Info("[SourceGC] sweep",
		"action", "source_items_gc",
		"status", "ok",
		"retention_days", int(cfg.Retention.Hours()/24),
		"batch_size", cfg.BatchSize,
		"deleted", deleted,
		"latency_ms", time.Since(start).Milliseconds())
}

func readSourceItemsGCConfig(ctx context.Context, pool *pgxpool.Pool) sourceItemsGCConfig {
	retentionDays := ReadIntSystemConfig(ctx, pool, sourceItemsGCRetentionKey, sourceItemsGCRetentionDays)
	if retentionDays < 7 {
		retentionDays = 7
	}
	intervalHours := ReadIntSystemConfig(ctx, pool, sourceItemsGCIntervalKey, sourceItemsGCIntervalHours)
	if intervalHours < 1 {
		intervalHours = 1
	}
	batchSize := ReadIntSystemConfig(ctx, pool, sourceItemsGCBatchKey, sourceItemsGCBatchSize)
	if batchSize < 1 {
		batchSize = sourceItemsGCBatchSize
	}
	if batchSize > 5000 {
		batchSize = 5000
	}
	return sourceItemsGCConfig{
		Retention: time.Duration(retentionDays) * 24 * time.Hour,
		Interval:  time.Duration(intervalHours) * time.Hour,
		BatchSize: int64(batchSize),
	}
}

func errorType(err error) string {
	if err == nil {
		return ""
	}
	return "error"
}
