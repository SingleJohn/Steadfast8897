package services

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// runEpisodeImageBackfill(Phase 2 改造版):
//  1. 对每个 primary_image_path IS NULL 的 Episode,先做本地兜底(<basename>-thumb.* 等);
//  2. 剩余按 season_id 聚合,EnqueueBatch 到 scrape_queue;worker 调 TMDB 拉 season 里所有 stills。
// Progress 语义:Total=本地兜底候选数 + 入队 season 数,Processed=已处理 + 已入队。
func (t *BackfillTask) runEpisodeImageBackfill(ctx context.Context, pool *pgxpool.Pool) error {
	type candidate struct {
		id       string
		epNum    int32
		filePath string
		seasonID string
	}

	rows, err := pool.Query(ctx,
		`SELECT e.id::text,
		        e.index_number,
		        COALESCE(e.file_path, ''),
		        e.season_id::text
		 FROM items e
		 JOIN items se ON se.id = e.season_id
		 JOIN items sr ON sr.id = se.parent_id AND sr.type = 'Series'
		 WHERE e.type = 'Episode'
		   AND e.primary_image_path IS NULL
		   AND e.index_number IS NOT NULL
		   AND sr.tmdb_id IS NOT NULL AND sr.tmdb_id > 0
		 ORDER BY e.season_id, e.index_number`)
	if err != nil {
		return err
	}
	var all []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.epNum, &c.filePath, &c.seasonID); err != nil {
			continue
		}
		all = append(all, c)
	}
	rows.Close()

	total := int64(len(all))
	t.setStageTotal(total)
	slog.Info("[Backfill] image stage: candidates", "items", total)
	if total == 0 {
		return nil
	}

	// Step 1:本地兜底(同目录 <basename>-thumb.* 等)。
	var processed int64
	dirCache := make(map[string]DirCache)
	seasonsToEnqueue := map[string]struct{}{}
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
					`UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW()
					  WHERE id = $3::uuid AND primary_image_path IS NULL`,
					*tp, tag, c.id); err == nil {
					processed++
					t.advanceProgress(total, processed, "image_local_hit", 1)
					continue
				}
			}
		}
		seasonsToEnqueue[c.seasonID] = struct{}{}
	}

	// Step 2:剩余按 season 入队;TMDB 关了也入队(worker 会判断后跳过)。
	if len(seasonsToEnqueue) == 0 {
		return nil
	}
	ids := make([]string, 0, len(seasonsToEnqueue))
	for id := range seasonsToEnqueue {
		ids = append(ids, id)
	}
	queue := NewScrapeQueue(pool)
	const batch = 200
	for i := 0; i < len(ids); i += batch {
		if t.shouldStop() {
			return nil
		}
		end := i + batch
		if end > len(ids) {
			end = len(ids)
		}
		if _, err := queue.EnqueueBatch(ctx, ids[i:end], ScrapeTaskBackfillEpisodeImg, ScrapePriorityBackfill); err != nil {
			return err
		}
		processed += int64(end - i)
		t.advanceProgress(total, processed, "image_enqueued_season", int64(end-i))
	}
	slog.Info("[Backfill] image stage: enqueued seasons", "count", len(ids))
	return nil
}

// processBackfillEpisodeImageTask 由 ScrapeWorker 调用:处理单个 Season。
// 查 season.index + series.tmdb_id,调 TMDB 拉 season.episodes 的 still_path,下载分发。
func processBackfillEpisodeImageTask(ctx context.Context, pool *pgxpool.Pool, client *TmdbClient, seasonID string) error {
	var seasonNum *int32
	var seriesTmdbID *int64
	err := pool.QueryRow(ctx,
		`SELECT se.index_number, sr.tmdb_id
		   FROM items se
		   JOIN items sr ON sr.id = se.parent_id AND sr.type = 'Series'
		  WHERE se.id = $1::uuid AND se.type = 'Season'`,
		seasonID,
	).Scan(&seasonNum, &seriesTmdbID)
	if err != nil {
		return err
	}
	if seasonNum == nil || seriesTmdbID == nil || *seriesTmdbID <= 0 {
		return nil
	}

	stills, err := fetchSeasonStills(ctx, client, *seriesTmdbID, *seasonNum)
	if err != nil {
		return err
	}
	if len(stills) == 0 {
		return nil
	}

	// 查该 season 下仍缺 still 的 episodes
	rows, err := pool.Query(ctx,
		`SELECT id::text, index_number
		   FROM items
		  WHERE season_id = $1::uuid AND type = 'Episode'
		    AND primary_image_path IS NULL
		    AND index_number IS NOT NULL`,
		seasonID)
	if err != nil {
		return err
	}
	type epRow struct {
		id    string
		epNum int32
	}
	var eps []epRow
	for rows.Next() {
		var r epRow
		if err := rows.Scan(&r.id, &r.epNum); err == nil {
			eps = append(eps, r)
		}
	}
	rows.Close()

	for _, ep := range eps {
		still, ok := stills[ep.epNum]
		if !ok || still == "" {
			continue
		}
		savePath := fmt.Sprintf("data/metadata/%s/still.jpg", ep.id)
		if client.DownloadImage(ctx, still, savePath, "w300") {
			tag := GenerateImageTag(savePath)
			_, _ = pool.Exec(ctx,
				`UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW()
				  WHERE id = $3::uuid AND primary_image_path IS NULL`,
				savePath, tag, ep.id)
		}
	}
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
