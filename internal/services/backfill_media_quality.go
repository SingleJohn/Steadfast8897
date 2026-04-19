package services

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
)

// runQualityBackfill 填充 media_versions.resolution / hdr_format / video_codec / audio_codec / source / quality_label。
// 幂等:只处理 resolution IS NULL 的行;优先用 mediainfo,缺失字段用文件名 NameParser 兜底。
// 纯本地,无 API 调用。
func (t *BackfillTask) runQualityBackfill(ctx context.Context, pool *pgxpool.Pool) error {
	var total int64
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM media_versions WHERE resolution IS NULL").Scan(&total); err != nil {
		return err
	}
	t.setStageTotal(total)
	slog.Info("[Backfill] quality stage start", "total", total)
	if total == 0 {
		return nil
	}

	const batchSize = 200
	var processed int64
	var lastID string

	for {
		if t.shouldStop() {
			return nil
		}
		rows, err := pool.Query(ctx,
			`SELECT id::text, name, mediainfo
			 FROM media_versions
			 WHERE resolution IS NULL
			   AND id::text > $1
			 ORDER BY id
			 LIMIT $2`,
			lastID, batchSize,
		)
		if err != nil {
			return err
		}

		type row struct {
			id   string
			name string
			mi   map[string]interface{}
		}
		batch := make([]row, 0, batchSize)

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

		if len(batch) == 0 {
			break
		}

		for _, r := range batch {
			if t.shouldStop() {
				return nil
			}
			q, label := ComputeMediaVersionQuality(filepath.Base(r.name), r.mi)
			if q.Empty() && label == "" {
				// 即便没任何标签,仍需写入一个 resolution 占位以跳出 NULL 筛选,
				// 避免下次运行再处理同一条 —— 用 "unknown" 占位。
				_, _ = pool.Exec(ctx,
					`UPDATE media_versions SET resolution = 'unknown' WHERE id = $1::uuid AND resolution IS NULL`,
					r.id)
			} else {
				res := q.Resolution
				if res == "" {
					res = "unknown"
				}
				_, _ = pool.Exec(ctx,
					`UPDATE media_versions
					 SET resolution = $1, hdr_format = $2, video_codec = $3, audio_codec = $4, source = $5, quality_label = $6
					 WHERE id = $7::uuid AND resolution IS NULL`,
					res, NullableStr(q.HDRFormat), NullableStr(q.VideoCodec),
					NullableStr(q.AudioCodec), NullableStr(q.Source), NullableStr(label),
					r.id)
			}
			processed++
			t.advanceProgress(total, processed, "quality_updated", 1)
			lastID = r.id
		}
	}
	slog.Info("[Backfill] quality stage done", "processed", processed)
	return nil
}
