package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

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

// runEpisodeNameBackfill 清洗存量 Episode 脏标题,并对关联 Series 重跑 scrapeEpisodeMetadata。
// 1) 将脏标题改为 "Episode N"(或 Season 0 下 "Special N"),变成占位符;
// 2) 聚合 DISTINCT series_id,有 tmdb_id 的 Series 调 scrapeEpisodeMetadata(幂等,内部只覆盖占位符/空 overview)。
func (t *BackfillTask) runEpisodeNameBackfill(ctx context.Context, pool *pgxpool.Pool) error {
	var total int64
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM items e WHERE `+dirtyEpisodeNameWhere,
	).Scan(&total); err != nil {
		return err
	}
	t.setStageTotal(total)
	slog.Info("[Backfill] name stage start", "total", total)

	// Step 1:清洗标题。
	seriesIDs := map[string]struct{}{}
	if total > 0 {
		const batchSize = 500
		var processed int64
		var lastID string
		for {
			if t.shouldStop() {
				return nil
			}
			rows, err := pool.Query(ctx,
				`SELECT e.id::text, e.index_number, se.index_number, e.series_id::text
				 FROM items e
				 LEFT JOIN items se ON se.id = e.season_id
				 WHERE `+dirtyEpisodeNameWhere+`
				   AND e.id::text > $1
				 ORDER BY e.id
				 LIMIT $2`,
				lastID, batchSize,
			)
			if err != nil {
				return err
			}
			type row struct {
				id        string
				epNum     int32
				seasonNum *int32
				seriesID  *string
			}
			batch := make([]row, 0, batchSize)
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
					return nil
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
				t.advanceProgress(total, processed, "name_cleaned", 1)
				lastID = r.id
			}
		}
	}

	// Step 2:对受影响 Series 触发 scrapeEpisodeMetadata(仅对有 tmdb_id 的)。
	if len(seriesIDs) == 0 {
		return nil
	}
	client := TmdbClientFromConfig(ctx, pool)
	if client == nil {
		// 没有配 TMDB API key,就到此为止 —— 占位符后续用户配上 API 再自动覆盖。
		slog.Info("[Backfill] name stage skip TMDB refresh (no API key)", "series_count", len(seriesIDs))
		return nil
	}
	for seriesID := range seriesIDs {
		if t.shouldStop() {
			return nil
		}
		var tmdbID *int64
		if err := pool.QueryRow(ctx,
			"SELECT tmdb_id FROM items WHERE id = $1::uuid AND type = 'Series'",
			seriesID,
		).Scan(&tmdbID); err != nil || tmdbID == nil || *tmdbID <= 0 {
			continue
		}
		scrapeEpisodeMetadata(ctx, pool, client, seriesID, *tmdbID)
		t.mu.Lock()
		t.progress.Counters["name_series_refreshed"]++
		t.mu.Unlock()
		time.Sleep(500 * time.Millisecond)
	}
	slog.Info("[Backfill] name stage done", "series", len(seriesIDs))
	return nil
}
