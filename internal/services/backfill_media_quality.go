package services

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
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
	rows, err := pool.Query(ctx,
		`SELECT DISTINCT mv.item_id::text
		   FROM media_versions mv
		  WHERE mv.resolution IS NULL
		  ORDER BY mv.item_id::text`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			out = append(out, id)
		}
	}
	return out, nil
}

// processBackfillQualityTask 由 ScrapeWorker 调用:处理单个 item 下所有
// resolution IS NULL 的 media_versions。纯本地任务,不需要 TMDB。
func processBackfillQualityTask(ctx context.Context, pool *pgxpool.Pool, itemID string) error {
	rows, err := pool.Query(ctx,
		`SELECT id::text, name, mediainfo
		   FROM media_versions
		  WHERE item_id = $1::uuid AND resolution IS NULL`,
		itemID)
	if err != nil {
		return err
	}
	type row struct {
		id   string
		name string
		mi   map[string]interface{}
	}
	var batch []row
	for rows.Next() {
		var r row
		var miRaw *string
		if err := rows.Scan(&r.id, &r.name, &miRaw); err != nil {
			continue
		}
		if miRaw != nil && *miRaw != "" {
			_ = json.Unmarshal([]byte(*miRaw), &r.mi)
		}
		batch = append(batch, r)
	}
	rows.Close()

	for _, r := range batch {
		q, label := ComputeMediaVersionQuality(filepath.Base(r.name), r.mi)
		if q.Empty() && label == "" {
			_, _ = pool.Exec(ctx,
				`UPDATE media_versions SET resolution = 'unknown' WHERE id = $1::uuid AND resolution IS NULL`,
				r.id)
			continue
		}
		res := q.Resolution
		if res == "" {
			res = "unknown"
		}
		_, _ = pool.Exec(ctx,
			`UPDATE media_versions
			    SET resolution = $1, hdr_format = $2, video_codec = $3, audio_codec = $4,
			        source = $5, quality_label = $6
			  WHERE id = $7::uuid AND resolution IS NULL`,
			res, NullableStr(q.HDRFormat), NullableStr(q.VideoCodec),
			NullableStr(q.AudioCodec), NullableStr(q.Source), NullableStr(label),
			r.id)
	}
	return nil
}
