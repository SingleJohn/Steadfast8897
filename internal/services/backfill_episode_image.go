package services

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// runEpisodeImageBackfill 为 primary_image_path IS NULL 的 Episode 补分集封面。
// 流程:
//  1. 先做本地兜底 —— 扫描每集同目录的 <basename>-thumb.* / .thumb.* / 同名图;
//  2. 再按 (series_tmdb_id, season_number) 聚合,依次调 TMDB `/tv/{id}/season/{n}` 并下载 still_path。
// 受 system_config.episode_still_fetch 总开关控制(关 → 跳过 TMDB,仅做本地兜底)。
// 幂等:只处理 primary_image_path IS NULL。
func (t *BackfillTask) runEpisodeImageBackfill(ctx context.Context, pool *pgxpool.Pool) error {
	type candidate struct {
		id           string
		epNum        int32
		filePath     string
		seasonNum    int32
		seriesTmdbID int64
	}

	rows, err := pool.Query(ctx,
		`SELECT e.id::text,
		        e.index_number,
		        COALESCE(e.file_path, ''),
		        COALESCE(se.index_number, 1),
		        sr.tmdb_id
		 FROM items e
		 JOIN items se ON se.id = e.season_id
		 JOIN items sr ON sr.id = se.parent_id AND sr.type = 'Series'
		 WHERE e.type = 'Episode'
		   AND e.primary_image_path IS NULL
		   AND e.index_number IS NOT NULL
		   AND sr.tmdb_id IS NOT NULL AND sr.tmdb_id > 0
		 ORDER BY sr.tmdb_id, COALESCE(se.index_number, 1), e.index_number`,
	)
	if err != nil {
		return err
	}
	var all []candidate
	for rows.Next() {
		var c candidate
		var tmdbID *int64
		if err := rows.Scan(&c.id, &c.epNum, &c.filePath, &c.seasonNum, &tmdbID); err != nil {
			continue
		}
		if tmdbID == nil || *tmdbID <= 0 {
			continue
		}
		c.seriesTmdbID = *tmdbID
		all = append(all, c)
	}
	rows.Close()

	total := int64(len(all))
	t.setStageTotal(total)
	slog.Info("[Backfill] image stage start", "total", total)
	if total == 0 {
		return nil
	}

	// Step 1: 本地兜底。
	var processed int64
	dirCache := make(map[string]DirCache)
	remaining := make([]candidate, 0, len(all))
	for _, c := range all {
		if t.shouldStop() {
			return nil
		}
		if c.filePath != "" {
			dir := filepath.Dir(c.filePath)
			cache, ok := dirCache[dir]
			if !ok {
				cache = CacheDir(dir)
				dirCache[dir] = cache
			}
			if tp := FindEpisodeThumbCached(cache, filepath.Base(c.filePath)); tp != nil {
				tag := GenerateImageTag(*tp)
				if _, err := pool.Exec(ctx,
					"UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid AND primary_image_path IS NULL",
					*tp, tag, c.id); err == nil {
					processed++
					t.advanceProgress(total, processed, "image_local_hit", 1)
					continue
				}
			}
		}
		remaining = append(remaining, c)
	}

	// Step 2: 总开关关闭时只做本地兜底。
	if !readEpisodeStillFetchEnabled(ctx, pool) {
		slog.Info("[Backfill] image stage: TMDB still disabled", "processed", processed, "skipped_api", len(remaining))
		return nil
	}

	client := TmdbClientFromConfig(ctx, pool)
	if client == nil {
		slog.Info("[Backfill] image stage skip TMDB (no API key)", "processed", processed, "skipped_api", len(remaining))
		return nil
	}

	// Step 3: 按 (tmdbID, seasonNum) 聚合并逐组拉取。
	type groupKey struct {
		tmdbID    int64
		seasonNum int32
	}
	groups := make(map[groupKey][]candidate)
	keys := make([]groupKey, 0)
	for _, c := range remaining {
		k := groupKey{tmdbID: c.seriesTmdbID, seasonNum: c.seasonNum}
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], c)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].tmdbID != keys[j].tmdbID {
			return keys[i].tmdbID < keys[j].tmdbID
		}
		return keys[i].seasonNum < keys[j].seasonNum
	})

	for _, k := range keys {
		if t.shouldStop() {
			return nil
		}
		stills, err := fetchSeasonStills(ctx, client, k.tmdbID, k.seasonNum)
		time.Sleep(200 * time.Millisecond)
		if err != nil {
			slog.Debug("[Backfill] image fetch season failed", "tmdb", k.tmdbID, "season", k.seasonNum, "error", err)
			// 当前 season 失败则推进进度但记录失败计数。
			for range groups[k] {
				processed++
				t.advanceProgress(total, processed, "image_api_error", 1)
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}
		for _, c := range groups[k] {
			if t.shouldStop() {
				return nil
			}
			still, ok := stills[c.epNum]
			if !ok || still == "" {
				processed++
				t.advanceProgress(total, processed, "image_api_miss", 1)
				continue
			}
			savePath := fmt.Sprintf("data/metadata/%s/still.jpg", c.id)
			if client.DownloadImage(ctx, still, savePath, "w300") {
				tag := GenerateImageTag(savePath)
				_, _ = pool.Exec(ctx,
					"UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid AND primary_image_path IS NULL",
					savePath, tag, c.id)
				processed++
				t.advanceProgress(total, processed, "image_api_hit", 1)
			} else {
				processed++
				t.advanceProgress(total, processed, "image_api_download_failed", 1)
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	slog.Info("[Backfill] image stage done", "processed", processed)
	return nil
}

// fetchSeasonStills 返回 map[episodeNumber]stillPath。不动 name / overview —— 它由 A stage / 正常刮削负责。
func fetchSeasonStills(ctx context.Context, client *TmdbClient, tmdbID int64, seasonNum int32) (map[int32]string, error) {
	endpoint := fmt.Sprintf("%s/tv/%d/season/%d?api_key={API_KEY}&language=%s", TMDB_BASE, tmdbID, seasonNum, client.language)
	data, err := client.tmdbGet(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	episodesRaw, ok := data["episodes"].([]any)
	if !ok || len(episodesRaw) == 0 {
		return map[int32]string{}, nil
	}
	out := make(map[int32]string, len(episodesRaw))
	for _, raw := range episodesRaw {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ep, ok := jsonInt64(m, "episode_number")
		if !ok || ep <= 0 {
			continue
		}
		still, _ := m["still_path"].(string)
		still = strings.TrimSpace(still)
		if still == "" {
			continue
		}
		out[int32(ep)] = still
	}
	return out, nil
}
