package services

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
)

// runQualityBackfill(Phase 2 改造版):扫出所有 resolution IS NULL 的 media_versions 归属的
// items,一次性 EnqueueBatch 到 scrape_queue,让 ScrapeWorker 异步消费。
// Progress 语义:Total=受影响 items 数,Processed=已入队数(通常 = Total,扫完即 complete)。
func (t *BackfillTask) runQualityBackfill(ctx context.Context, pool *pgxpool.Pool) error {
	itemIDs, err := collectQualityCandidates(ctx, pool)
	if err != nil {
		return err
	}
	total := int64(len(itemIDs))
	t.setStageTotal(total)
	slog.Info("[Backfill] quality stage: candidates", "items", total)
	if total == 0 {
		return nil
	}

	queue := NewScrapeQueue(pool)
	const batch = 500
	var processed int64
	for i := 0; i < len(itemIDs); i += batch {
		if t.shouldStop() {
			return nil
		}
		end := i + batch
		if end > len(itemIDs) {
			end = len(itemIDs)
		}
		if _, err := queue.EnqueueBatch(ctx, itemIDs[i:end], ScrapeTaskBackfillQuality, ScrapePriorityBackfill); err != nil {
			return err
		}
		processed += int64(end - i)
		t.advanceProgress(total, processed, "quality_enqueued", int64(end-i))
	}
	slog.Info("[Backfill] quality stage: enqueued", "items", processed)
	return nil
}

// collectQualityCandidates 找出所有"有 resolution IS NULL 的 media_versions"的 items.
// 按 item 聚合而非 media_version,因为 scrape_queue.item_id 是 items.id。
func collectQualityCandidates(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	return repository.NewBackgroundTaskRepository(pool).ListQualityBackfillItemIDs(ctx)
}

// processBackfillQualityTask 由 ScrapeWorker 调用:处理单个 item 下所有
// resolution IS NULL 的 media_versions。纯本地任务,不需要 TMDB。
func processBackfillQualityTask(ctx context.Context, pool *pgxpool.Pool, itemID string) error {
	repo := repository.NewBackgroundTaskRepository(pool)
	batch, err := repo.ListQualityMediaVersions(ctx, itemID)
	if err != nil {
		return err
	}

	for _, r := range batch {
		q, label := ComputeMediaVersionQuality(filepath.Base(r.Name), r.MediaInfo)
		if q.Empty() && label == "" {
			_ = repo.MarkMediaVersionQualityUnknown(ctx, r.ID)
			continue
		}
		res := q.Resolution
		if res == "" {
			res = "unknown"
		}
		_ = repo.UpdateMediaVersionQuality(ctx, r.ID, res,
			stringPtrIfNotEmpty(q.HDRFormat),
			stringPtrIfNotEmpty(q.VideoCodec),
			stringPtrIfNotEmpty(q.AudioCodec),
			stringPtrIfNotEmpty(q.Source),
			stringPtrIfNotEmpty(label),
		)
	}
	return nil
}
