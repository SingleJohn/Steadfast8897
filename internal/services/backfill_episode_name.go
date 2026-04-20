package services

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 脏特征正则 —— 用于 "当前标题很可能是文件名污染" 的判定。
// 仅当 name 同时命中(非占位符)且(扩展名 OR 画质 token OR 多段方括号 OR 连续点/下划线)时才改写。
const dirtyEpisodeNameWhere = `
  e.type = 'Episode'
  AND e.index_number IS NOT NULL
  AND e.name IS NOT NULL
  AND NOT (
      e.name ~ '^Episode [0-9]+$'
      OR e.name ~ '^Special [0-9]+$'
      OR e.name ~ '^第[0-9]+[集话回]$'
      OR e.name ~ '^S[0-9]{1,2}E[0-9]{1,3}$'
  )
  AND (
      e.name ~* '\.(mkv|mp4|avi|ts|m2ts|rmvb|flv|mov|wmv|webm|m4v|iso)$'
      OR e.name ~* '(1080p|2160p|720p|480p|576p|1440p|4k|webrip|web-?dl|bluray|bdrip|hdtv|dvdrip|remux|x26[45]|hevc|atmos|truehd|dts-?hd|h\.?26[45])'
      OR e.name ~ '\[.+\]\[.+\]'
      OR e.name ~ '[._]{2,}'
  )
`

// runEpisodeNameBackfill(Phase 2 改造版):
//  1. 清洗脏标题(纯 DB 操作,保持原逻辑)
//  2. 聚合 DISTINCT series_id → EnqueueBatch 到 scrape_queue,worker 异步调 TMDB。
// Progress 语义:Total=清洗计数 + 入队 series 数,Processed=已入队。
func (t *BackfillTask) runEpisodeNameBackfill(ctx context.Context, pool *pgxpool.Pool) error {
	// Step 1:清洗脏标题为占位符。
	seriesIDs, cleaned, err := cleanDirtyEpisodeNames(ctx, pool, t)
	if err != nil {
		return err
	}
	slog.Info("[Backfill] name stage: cleaned dirty names", "count", cleaned, "series", len(seriesIDs))

	// Step 2:把受影响 Series 入队,worker 调 scrapeEpisodeMetadata 覆盖占位符。
	total := int64(len(seriesIDs))
	t.setStageTotal(total)
	if total == 0 {
		return nil
	}

	queue := NewScrapeQueue(pool)
	ids := make([]string, 0, len(seriesIDs))
	for id := range seriesIDs {
		ids = append(ids, id)
	}
	const batch = 200
	var processed int64
	for i := 0; i < len(ids); i += batch {
		if t.shouldStop() {
			return nil
		}
		end := i + batch
		if end > len(ids) {
			end = len(ids)
		}
		if _, err := queue.EnqueueBatch(ctx, ids[i:end], ScrapeTaskBackfillEpisodeName, ScrapePriorityBackfill); err != nil {
			return err
		}
		processed += int64(end - i)
		t.advanceProgress(total, processed, "name_enqueued", int64(end-i))
	}
	slog.Info("[Backfill] name stage: enqueued series", "count", processed)
	return nil
}

// cleanDirtyEpisodeNames 批量把脏标题改为 "Episode N" / "Special N" 占位符,
// 返回受影响的 series_id 集合。
func cleanDirtyEpisodeNames(ctx context.Context, pool *pgxpool.Pool, t *BackfillTask) (map[string]struct{}, int64, error) {
	var total int64
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM items e WHERE `+dirtyEpisodeNameWhere,
	).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}

	seriesIDs := map[string]struct{}{}
	const batchSize = 500
	var processed int64
	var lastID string
	for {
		if t.shouldStop() {
			return seriesIDs, processed, nil
		}
		rows, err := pool.Query(ctx,
			`SELECT e.id::text, e.index_number, se.index_number, e.series_id::text
			 FROM items e
			 LEFT JOIN items se ON se.id = e.season_id
			 WHERE `+dirtyEpisodeNameWhere+`
			   AND e.id::text > $1
			 ORDER BY e.id
			 LIMIT $2`,
			lastID, batchSize)
		if err != nil {
			return nil, processed, err
		}
		type row struct {
			id        string
			epNum     int32
			seasonNum *int32
			seriesID  *string
		}
		var batch []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.epNum, &r.seasonNum, &r.seriesID); err != nil {
				continue
			}
			batch = append(batch, r)
		}
		rows.Close()
		if len(batch) == 0 {
			break
		}
		for _, r := range batch {
			if t.shouldStop() {
				return seriesIDs, processed, nil
			}
			newName := fmt.Sprintf("Episode %d", r.epNum)
			if r.seasonNum != nil && *r.seasonNum == 0 {
				newName = fmt.Sprintf("Special %d", r.epNum)
			}
			_, _ = pool.Exec(ctx,
				"UPDATE items SET name = $1, updated_at = NOW() WHERE id = $2::uuid",
				newName, r.id)
			if r.seriesID != nil && *r.seriesID != "" {
				seriesIDs[*r.seriesID] = struct{}{}
			}
			processed++
			lastID = r.id
		}
	}
	return seriesIDs, processed, nil
}

// processBackfillEpisodeNameTask 由 ScrapeWorker 调用:用 series 的 tmdb_id 调 TMDB
// 拉 season.episodes 覆盖占位符标题。没有 tmdb_id 的直接 done(等识别完成后再次入队即可)。
func processBackfillEpisodeNameTask(ctx context.Context, pool *pgxpool.Pool, client *TmdbClient, seriesID string) error {
	var tmdbID *int64
	if err := pool.QueryRow(ctx,
		"SELECT tmdb_id FROM items WHERE id = $1::uuid AND type = 'Series'",
		seriesID,
	).Scan(&tmdbID); err != nil {
		return err
	}
	if tmdbID == nil || *tmdbID <= 0 {
		return nil
	}
	scrapeEpisodeMetadata(ctx, pool, client, seriesID, *tmdbID)
	return nil
}
