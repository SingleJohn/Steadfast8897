package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/models"
	"fyms/internal/services/scraper"
)

// extractShowNameFromEpisodes 从剧集文件名中归纳剧名(多数派胜出)。
// 用于 showPath 目录名本身没有剧名信息、只能从内部文件名推断的场景。
func extractShowNameFromEpisodes(showPath string) *string {
	entries, err := os.ReadDir(showPath)
	if err != nil {
		return nil
	}
	counts := make(map[string]int)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if !IsVideoExt(ext) {
			continue
		}
		p := scraper.Parse(name, scraper.ModeEpisode)
		candidate := p.Title
		if candidate == "" {
			candidate = p.OriginalTitle
		}
		if candidate != "" {
			counts[candidate]++
		}
	}
	if len(counts) == 0 {
		return nil
	}
	var best string
	var bestCount int
	for k, v := range counts {
		if v > bestCount || (v == bestCount && len(k) > len(best)) {
			best = k
			bestCount = v
		}
	}
	if best == "" {
		return nil
	}
	return &best
}

// ============ TV Show Scanning ============

var (
	seasonRE   = regexp.MustCompile(`(?i)[Ss](?:eason|taffel|aison|erie)?\s*(\d+)`)
	seasonCNRE = regexp.MustCompile(`第(\d+)季`)
)

type epFile struct {
	name string
	path string
	ext  string
}

var episodeScanLocks sync.Map

func withEpisodeScanLock(key string, fn func()) {
	lockAny, _ := episodeScanLocks.LoadOrStore(key, &sync.Mutex{})
	mu := lockAny.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	fn()
}

func isShowDir(path string) bool {
	// 目录名本身就是 Season/Specials/S01/第一季 这类 → 它是 season 目录,不是 show 根。
	// 否则 findShowRoot 从 episode 文件向上查找时会停在 Season 目录,
	// 把 "Season 1" 当作剧名创建错误 Series(scanOneShow 会用 dir basename 做 name)。
	if looksLikeSeasonDir(filepath.Base(path)) {
		return false
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if looksLikeSeasonDir(entry.Name()) {
				return true
			}
		} else {
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if IsVideoExt(ext) {
				return true
			}
		}
	}
	return false
}

func scanOneShow(
	ctx context.Context,
	pool *pgxpool.Pool,
	libraryID string,
	showNameRaw string,
	showPath string,
) {
	parsed := ParseMovieName(showNameRaw)
	showCache := CacheDir(showPath)

	var nfoTitle *string
	var nfoData *NfoData
	for _, entry := range showCache {
		if entry[0] == "tvshow.nfo" {
			nfoData = ParseNfo(entry[1])
			if nfoData != nil {
				nfoTitle = nfoData.Title
			}
			break
		}
	}

	finalShowName := parsed.Name
	if nfoTitle != nil {
		finalShowName = *nfoTitle
	}

	poster := FindImageCached(showCache, posterImagePrefixes)
	backdrop := FindImageCached(showCache, backdropImagePrefixes)
	posterTag := ptrAndThen(poster, GenerateImageTag)
	backdropTag := ptrAndThen(backdrop, GenerateImageTag)

	// 查找已有 Series:
	// 1) 优先按 Show 目录路径(file_path)定位 — 唯一、防重名错挂
	// 2) 未命中再按 name 兜底(兼容历史数据:老 Series 的 file_path 可能为 NULL)
	//    只有在找到的 Series.file_path 为 NULL 时才复用并惰性回填;若 file_path
	//    已有值且 ≠ 当前 showPath,视为"同名不同目录"的另一部剧,不复用。
	var seriesID string
	createdSeries := false
	findExistingByPath := func(path string) string {
		var id uuid.UUID
		if err := pool.QueryRow(ctx,
			"SELECT id FROM items WHERE library_id = $1::uuid AND type = 'Series' AND file_path = $2 LIMIT 1",
			libraryID, path).Scan(&id); err == nil {
			return id.String()
		}
		return ""
	}
	findExistingByName := func(name string) (string, bool) {
		var id uuid.UUID
		var fp *string
		if err := pool.QueryRow(ctx,
			"SELECT id, file_path FROM items WHERE library_id = $1::uuid AND type = 'Series' AND name = $2 LIMIT 1",
			libraryID, name).Scan(&id, &fp); err != nil {
			return "", false
		}
		// file_path 已绑定到别的 Show 目录 → 不是同一部剧
		if fp != nil && *fp != "" && *fp != showPath {
			return "", false
		}
		return id.String(), fp == nil || *fp == ""
	}

	seriesID = findExistingByPath(showPath)
	if seriesID == "" {
		if id, needBackfill := findExistingByName(finalShowName); id != "" {
			seriesID = id
			if needBackfill {
				pool.Exec(ctx,
					"UPDATE items SET file_path = $1, updated_at = NOW() WHERE id = $2::uuid AND file_path IS NULL",
					showPath, seriesID)
			}
		} else if finalShowName != parsed.Name {
			if id, needBackfill := findExistingByName(parsed.Name); id != "" {
				seriesID = id
				// 找到旧的目录名 Series,更新为 NFO 中文名并回填 file_path
				updates := "name = $1, sort_name = $2, updated_at = NOW()"
				args := []interface{}{finalShowName, strings.ToLower(finalShowName)}
				if needBackfill {
					updates += ", file_path = $3"
					args = append(args, showPath)
				}
				args = append(args, seriesID)
				pool.Exec(ctx,
					fmt.Sprintf("UPDATE items SET %s WHERE id = $%d::uuid", updates, len(args)),
					args...)
			}
		}
	}

	if seriesID != "" {
		uid, _ := uuid.Parse(seriesID)
		syncItemArtwork(ctx, pool, uid, poster, posterTag, backdrop, backdropTag)
	} else {
		var insertedID *uuid.UUID
		err := pool.QueryRow(ctx,
			"INSERT INTO items (library_id, type, name, sort_name, production_year, file_path, primary_image_path, primary_image_tag, backdrop_image_path, backdrop_image_tag) "+
				"VALUES ($1::uuid, 'Series', $2, $3, $4, $5, $6, $7, $8, $9) "+
				"ON CONFLICT DO NOTHING RETURNING id",
			libraryID, finalShowName, strings.ToLower(finalShowName), parsed.Year, showPath,
			derefStr(poster), derefStr(posterTag),
			derefStr(backdrop), derefStr(backdropTag),
		).Scan(&insertedID)
		if err == nil && insertedID != nil {
			seriesID = insertedID.String()
			createdSeries = true
		} else {
			// ON CONFLICT(多半是并发同路径插入) → 再按 file_path 查一次拿 UUID
			seriesID = findExistingByPath(showPath)
			if seriesID == "" {
				return
			}
		}
	}

	if nfoData != nil {
		ApplyNfoDataWithPlatformSource(ctx, pool, seriesID, nfoData, models.PlatformScanSourceNFO)
	}
	if createdSeries {
		enqueueIdentifyIfEligible(ctx, pool, seriesID, ScrapePriorityIdentify, "scan.series.create")
	}

	entries, err := os.ReadDir(showPath)
	if err != nil {
		return
	}

	// Collect season directories
	type seasonDir struct {
		path      string
		seasonNum int32
	}
	var seasonDirs []seasonDir
	hasVideoInRoot := false
	for _, se := range entries {
		if se.IsDir() {
			dirName := se.Name()
			seasonNum := extractSeasonNumber(dirName)
			if seasonNum >= 0 {
				seasonDirs = append(seasonDirs, seasonDir{path: filepath.Join(showPath, dirName), seasonNum: seasonNum})
			}
		} else if IsVideoExt(filepath.Ext(se.Name())) {
			hasVideoInRoot = true
		}
	}
	// If no season subdirectories found but root has video files, treat root as Season 1
	if len(seasonDirs) == 0 && hasVideoInRoot {
		seasonDirs = append(seasonDirs, seasonDir{path: showPath, seasonNum: 1})
	}

	// 已刮削 Series 下新增 Episode 的增量补全:收集本次新建 Episode 所在的 seasonID。
	// scanOneShow 结束时,若 Series.tmdb_id 存在,就针对这些 season 入队
	// backfill_episode_name / backfill_episode_image,避免新集停在占位符名/无缩略图状态。
	newEpisodeSeasonIDs := map[string]struct{}{}

	for _, sd := range seasonDirs {
		seasonNum := sd.seasonNum
		seasonPath := sd.path
		seasonCache := CacheDir(seasonPath)
		seasonPoster := FindImageCached(seasonCache, posterImagePrefixes)
		seasonPosterTag := ptrAndThen(seasonPoster, GenerateImageTag)

		var seasonID string
		var seasonInsertedID *uuid.UUID
		err := pool.QueryRow(ctx,
			"INSERT INTO items (library_id, parent_id, type, name, sort_name, index_number, series_id, series_name, primary_image_path, primary_image_tag) "+
				"VALUES ($1::uuid, $2::uuid, 'Season', $3, $4, $5, $6::uuid, $7, $8, $9) "+
				"ON CONFLICT DO NOTHING RETURNING id",
			libraryID, seriesID,
			fmt.Sprintf("Season %d", seasonNum), fmt.Sprintf("season %04d", seasonNum),
			seasonNum, seriesID, finalShowName,
			derefStr(seasonPoster), derefStr(seasonPosterTag),
		).Scan(&seasonInsertedID)

		if err == nil && seasonInsertedID != nil {
			seasonID = seasonInsertedID.String()
		} else {
			var existingSeasonID uuid.UUID
			err = pool.QueryRow(ctx,
				"SELECT id FROM items WHERE parent_id = $1::uuid AND type = 'Season' AND index_number = $2 LIMIT 1",
				seriesID, seasonNum).Scan(&existingSeasonID)
			if err != nil {
				continue
			}
			syncItemArtwork(ctx, pool, existingSeasonID, seasonPoster, seasonPosterTag, nil, nil)
			seasonID = existingSeasonID.String()
		}

		// Group episodes by episode number
		epGroups := make(map[int32][]epFile)
		for _, entry := range seasonCache {
			fname, fpath := entry[0], entry[1]
			ext := strings.TrimPrefix(filepath.Ext(fname), ".")
			if !IsVideoExt("." + ext) {
				continue
			}
			epInfo := ParseEpisodeInfo(fname)
			epNum := int32(0)
			if epInfo != nil && epInfo.Episode != nil {
				epNum = *epInfo.Episode
			}
			epGroups[epNum] = append(epGroups[epNum], epFile{name: fname, path: fpath, ext: ext})
		}

		// Sort episode numbers for deterministic ordering
		epNums := make([]int32, 0, len(epGroups))
		for k := range epGroups {
			epNums = append(epNums, k)
		}
		sort.Slice(epNums, func(i, j int) bool { return epNums[i] < epNums[j] })

		for _, epNum := range epNums {
			files := epGroups[epNum]
			if len(files) == 0 {
				continue
			}
			primary := files[0]

			lockKey := fmt.Sprintf("%s|%s|%d|%d", libraryID, strings.ToLower(finalShowName), seasonNum, epNum)
			withEpisodeScanLock(lockKey, func() {
				itemID, createdEpisode := ensureCanonicalEpisodeItem(ctx, pool, libraryID, seasonID, seriesID, finalShowName, seasonNum, epNum, primary, seasonCache)
				if itemID == uuid.Nil {
					return
				}

				if createdEpisode {
					newEpisodeSeasonIDs[seasonID] = struct{}{}
					nfoStem := strings.TrimSuffix(primary.name, filepath.Ext(primary.name))
					epNfoName := nfoStem + ".nfo"
					for _, entry := range seasonCache {
						if entry[0] == epNfoName {
							if nfo := ParseNfo(entry[1]); nfo != nil {
								ApplyNfoDataWithPlatformSource(ctx, pool, itemID.String(), nfo, models.PlatformScanSourceNFO)
							}
							break
						}
					}
				}

				ensureEpisodeMediaVersions(ctx, pool, itemID, files, seasonCache)
			})
		}

		pool.Exec(ctx, "UPDATE items SET updated_at = NOW() WHERE id = $1::uuid AND type = 'Series'", seriesID)
	}

	// Series 有 tmdb_id(identify 成功或 NFO 带 tmdbid)时才能做 TMDB 补全。
	// autoScrapeNewItems 不会把已识别 / NFO 源的 item 重入队,BackfillTask 又只在用户手动
	// 触发或启动 24h 后跑,所以这里是常规 rescrape 路径外的主动补全入口。
	var seriesTmdbID *int64
	_ = pool.QueryRow(ctx,
		"SELECT tmdb_id FROM items WHERE id = $1::uuid AND type = 'Series'",
		seriesID,
	).Scan(&seriesTmdbID)
	seriesScraped := seriesTmdbID != nil && *seriesTmdbID > 0

	// 分支 1:已刮削 Series 下新增 Episode → 增量补 name/image,避免新集停在占位符。
	if seriesScraped && len(newEpisodeSeasonIDs) > 0 {
		seasonIDs := make([]string, 0, len(newEpisodeSeasonIDs))
		for sid := range newEpisodeSeasonIDs {
			seasonIDs = append(seasonIDs, sid)
		}
		if err := enqueueSeriesSubtreeRemote(ctx, NewScrapeQueue(pool), seriesID, seasonIDs, ScrapePriorityScan, seriesSubtreeRemotePlan{
			EnqueueEpisodeNames:  true,
			EnqueueEpisodeImages: true,
		}); err != nil {
			slog.Warn("[Scan] enqueue subtree complement for new episodes failed",
				"series", seriesID, "show", finalShowName, "error", err)
		} else {
			slog.Info("[Scan] New episodes under scraped series, enqueued subtree complement",
				"series", seriesID, "show", finalShowName, "seasons", len(seasonIDs))
		}
	}

	// 分支 2:NFO 扫入且 Series 有 tmdb_id → 补 TMDB 独有字段。
	// NFO 不带 cast image_url,也不带分集 still,而 auto_scrape 明确跳过 NFO 源,
	// 这里是唯一的兜底入口。UNIQUE(item_id,task_type) 会去重,重扫不会放大工作量。
	// 包含所有现存季(不限 new),下游任务按 "字段为空才覆盖" 语义自检,不会覆盖已有数据。
	if seriesScraped && nfoData != nil {
		allSeasonIDs, err := loadSeriesSeasonIDs(ctx, pool, seriesID)
		if err != nil {
			slog.Warn("[Scan] load series seasons for subtree complement failed",
				"series", seriesID, "show", finalShowName, "error", err)
		}
		if err := enqueueSeriesSubtreeRemote(ctx, NewScrapeQueue(pool), seriesID, allSeasonIDs, ScrapePriorityScan, seriesSubtreeRemotePlan{
			EnqueueEpisodeNames:  true,
			EnqueueEpisodeImages: true,
			EnqueueActorImages:   true,
		}); err != nil {
			slog.Warn("[Scan] enqueue NFO subtree complement failed",
				"series", seriesID, "show", finalShowName, "error", err)
		} else {
			slog.Info("[Scan] NFO series, enqueued TMDB subtree complement",
				"series", seriesID, "show", finalShowName,
				"tmdb_id", *seriesTmdbID, "seasons", len(allSeasonIDs))
		}
	}
}

func ensureCanonicalEpisodeItem(
	ctx context.Context,
	pool *pgxpool.Pool,
	libraryID string,
	seasonID string,
	seriesID string,
	finalShowName string,
	seasonNum int32,
	epNum int32,
	primary epFile,
	seasonCache DirCache,
) (uuid.UUID, bool) {
	type episodeCandidate struct {
		ID       uuid.UUID
		FilePath *string
	}

	rows, err := pool.Query(ctx,
		`SELECT id, file_path
		 FROM items
		 WHERE season_id = $1::uuid
		   AND type = 'Episode'
		   AND index_number = $2
		 ORDER BY CASE WHEN file_path = $3 THEN 0 ELSE 1 END, created_at ASC, id ASC`,
		seasonID, epNum, primary.path,
	)
	if err != nil {
		return uuid.Nil, false
	}
	defer rows.Close()

	var candidates []episodeCandidate
	for rows.Next() {
		var c episodeCandidate
		if rows.Scan(&c.ID, &c.FilePath) == nil {
			candidates = append(candidates, c)
		}
	}
	if len(candidates) > 0 {
		canonical := candidates[0]
		canonicalID := canonical.ID
		if canonical.FilePath == nil || *canonical.FilePath == "" || !scanPathExists(*canonical.FilePath) {
			mi := ReadMediainfoJSONCached(primary.path, seasonCache)
			var runtimeTicks *int64
			if mi != nil {
				runtimeTicks = getJSONInt64(mi, "RunTimeTicks")
			}
			pool.Exec(ctx,
				`UPDATE items
				    SET file_path = $1,
				        container = $2,
				        runtime_ticks = COALESCE($3::bigint, runtime_ticks),
				        updated_at = NOW()
				  WHERE id = $4::uuid`,
				primary.path, primary.ext, runtimeTicks, canonicalID)
		}
		for _, dup := range candidates[1:] {
			mergeDuplicateEpisodeIntoCanonical(ctx, pool, canonicalID, dup.ID)
		}
		return canonicalID, false
	}

	// M7.1:写入占位符标题,避免文件名污染;后续 scrapeEpisodeMetadata 会识别为占位符并用 TMDB 真实标题覆盖。
	epTitle := fmt.Sprintf("Episode %d", epNum)
	if seasonNum == 0 {
		epTitle = fmt.Sprintf("Special %d", epNum)
	}
	mi := ReadMediainfoJSONCached(primary.path, seasonCache)
	var runtimeTicks *int64
	if mi != nil {
		runtimeTicks = getJSONInt64(mi, "RunTimeTicks")
	}

	// M7.2:新扫时兜底本地分集封面(<basename>-thumb.jpg 等)。
	var thumbPath, thumbTag *string
	if tp := FindEpisodeThumbCached(seasonCache, primary.name); tp != nil {
		thumbPath = tp
		thumbTag = GenerateImageTag(*tp)
	}

	var insertedEpID *uuid.UUID
	err = pool.QueryRow(ctx,
		"INSERT INTO items (library_id, parent_id, type, name, sort_name, index_number, parent_index_number, runtime_ticks, file_path, container, series_id, series_name, season_id, primary_image_path, primary_image_tag, created_at) "+
			"VALUES ($1::uuid, $2::uuid, 'Episode', $3, $4, $5, $6, $7, $8, $9, $10::uuid, $11, $12::uuid, $13, $14, COALESCE($15, NOW())) "+
			"ON CONFLICT DO NOTHING RETURNING id",
		libraryID, seasonID, epTitle,
		fmt.Sprintf("episode %04d", epNum),
		epNum, seasonNum, runtimeTicks,
		primary.path, primary.ext,
		seriesID, finalShowName, seasonID,
		derefStr(thumbPath), derefStr(thumbTag),
		fileMtimeOrNil(primary.path),
	).Scan(&insertedEpID)
	if err == nil && insertedEpID != nil {
		return *insertedEpID, true
	}

	rows, err = pool.Query(ctx,
		`SELECT id
		 FROM items
		 WHERE season_id = $1::uuid
		   AND type = 'Episode'
		   AND index_number = $2
		 ORDER BY CASE WHEN file_path = $3 THEN 0 ELSE 1 END, created_at ASC, id ASC`,
		seasonID, epNum, primary.path,
	)
	if err != nil {
		return uuid.Nil, false
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			return id, false
		}
	}
	return uuid.Nil, false
}

func scanPathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func mergeDuplicateEpisodeIntoCanonical(ctx context.Context, pool *pgxpool.Pool, canonicalID uuid.UUID, duplicateID uuid.UUID) {
	if canonicalID == duplicateID {
		return
	}

	pool.Exec(ctx,
		`INSERT INTO media_versions (item_id, name, file_path, container, is_primary, mediainfo, runtime_ticks, bitrate, size, resolution, hdr_format, video_codec, audio_codec, source, quality_label)
		 SELECT $1, name, file_path, container, is_primary, mediainfo, runtime_ticks, bitrate, size, resolution, hdr_format, video_codec, audio_codec, source, quality_label
		 FROM media_versions
		 WHERE item_id = $2
		 ON CONFLICT (item_id, file_path) DO NOTHING`,
		canonicalID, duplicateID,
	)

	pool.Exec(ctx,
		`INSERT INTO user_item_data (user_id, item_id, playback_position_ticks, play_count, is_favorite, played, last_played_date)
		 SELECT user_id, $1, playback_position_ticks, play_count, is_favorite, played, last_played_date
		 FROM user_item_data
		 WHERE item_id = $2
		 ON CONFLICT (user_id, item_id) DO UPDATE SET
		 	playback_position_ticks = GREATEST(user_item_data.playback_position_ticks, EXCLUDED.playback_position_ticks),
		 	play_count = GREATEST(user_item_data.play_count, EXCLUDED.play_count),
		 	is_favorite = user_item_data.is_favorite OR EXCLUDED.is_favorite,
		 	played = user_item_data.played OR EXCLUDED.played,
		 	last_played_date = GREATEST(
		 		COALESCE(user_item_data.last_played_date, TIMESTAMP 'epoch'),
		 		COALESCE(EXCLUDED.last_played_date, TIMESTAMP 'epoch')
		 	)`,
		canonicalID, duplicateID,
	)

	pool.Exec(ctx, "DELETE FROM items WHERE id = $1::uuid", duplicateID)
}

func ensureEpisodeMediaVersions(ctx context.Context, pool *pgxpool.Pool, itemID uuid.UUID, files []epFile, seasonCache DirCache) {
	for i, f := range files {
		verName := strings.TrimSuffix(filepath.Base(f.name), filepath.Ext(f.name))
		if verName == "" {
			verName = "Unknown"
		}
		mi := ReadMediainfoJSONCached(f.path, seasonCache)
		isPrimary := i == 0

		container := f.ext
		if f.ext == "strm" {
			if rp := ResolveStrmPath(f.path); rp != nil {
				resolved := strings.TrimPrefix(filepath.Ext(*rp), ".")
				if resolved != "" {
					container = resolved
				}
			}
		}

		var miJSON []byte
		if mi != nil {
			miJSON, _ = json.Marshal(mi)
		}
		var runtimeTicks, bitrate, size *int64
		if mi != nil {
			runtimeTicks = getJSONInt64(mi, "RunTimeTicks")
			bitrate = getJSONInt64(mi, "Bitrate")
			size = getJSONInt64(mi, "Size")
		}

		q, qLabel := ComputeMediaVersionQuality(f.name, mi)

		pool.Exec(ctx,
			"INSERT INTO media_versions (item_id, name, file_path, container, is_primary, mediainfo, runtime_ticks, bitrate, size, resolution, hdr_format, video_codec, audio_codec, source, quality_label) "+
				"VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) ON CONFLICT DO NOTHING",
			itemID, verName, f.path, container, isPrimary,
			nullableJSON(miJSON), runtimeTicks, bitrate, size,
			NullableStr(q.Resolution), NullableStr(q.HDRFormat), NullableStr(q.VideoCodec),
			NullableStr(q.AudioCodec), NullableStr(q.Source), NullableStr(qLabel))
	}
}

func extractSeasonNumber(dirName string) int32 {
	lower := strings.ToLower(dirName)
	// Specials / SP / 特别篇 → Season 0
	if lower == "specials" || lower == "sp" || lower == "special" || strings.Contains(dirName, "特别篇") || strings.Contains(dirName, "番外") {
		return 0
	}
	if m := seasonRE.FindStringSubmatch(dirName); m != nil {
		if v, err := strconv.ParseInt(m[1], 10, 32); err == nil {
			return int32(v)
		}
	}
	if m := seasonCNRE.FindStringSubmatch(dirName); m != nil {
		if v, err := strconv.ParseInt(m[1], 10, 32); err == nil {
			return int32(v)
		}
	}
	return -1
}
