package services

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/models"
)

// enqueueMovieNfoComplement 给 NFO 扫入的 Movie 入队 TMDB 补全任务。
// NFO 不带 cast image_url,而 autoScrapeNewItems 跳过 NFO 源,这里是兜底入口。
// UNIQUE(item_id, task_type) 会去重,重复扫不会放大工作量。
// Movie 没 Episode,所以只入队演员头像;poster/backdrop 通常已在本地目录(poster.jpg/fanart.jpg)。
func enqueueMovieNfoComplement(ctx context.Context, pool *pgxpool.Pool, itemID string) {
	var tmdbID *int64
	_ = pool.QueryRow(ctx,
		"SELECT tmdb_id FROM items WHERE id = $1::uuid AND type = 'Movie'",
		itemID,
	).Scan(&tmdbID)
	if tmdbID == nil || *tmdbID <= 0 {
		return
	}
	_ = NewScrapeQueue(pool).Enqueue(ctx, itemID, ScrapeTaskBackfillActorImg, ScrapePriorityScan)
	slog.Info("[Scan] NFO movie, enqueued actor image complement",
		"movie", itemID, "tmdb_id", *tmdbID)
}

// ============ Movie Scanning ============

type movieEntry struct {
	name     string
	fullPath string
	isDir    bool
	// videoPaths 在 isDir=true 时给出目录内所有视频文件的绝对路径(含 BDMV/STREAM),
	// 供 prune 阶段把 DB 里的 primary file_path 也识别为"本次扫到",避免被误删。
	videoPaths []string
}

func looksLikeSeasonDir(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "season") || strings.HasPrefix(lower, "specials") || lower == "extras" {
		return true
	}
	for _, prefix := range []string{"s0", "s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8", "s9"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return strings.Contains(lower, "第") && strings.Contains(lower, "季")
}

func looksLikeShowDir(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() && looksLikeSeasonDir(entry.Name()) {
			return true
		}
	}
	return false
}

// 结构化光盘目录名,这些目录名本身不应被当作电影/剧集的标题。
var structuralDiscDirNames = map[string]bool{
	"BDMV": true, "STREAM": true, "VIDEO_TS": true, "AUDIO_TS": true, "CERTIFICATE": true,
}

func isStructuralDiscDirName(name string) bool {
	return structuralDiscDirNames[strings.ToUpper(name)]
}

// isBdmvMovieDir 判断 dir 是否为 BDMV 布局的电影根(含 BDMV/STREAM 且内有视频文件)。
func isBdmvMovieDir(dir string) bool {
	entries, err := os.ReadDir(filepath.Join(dir, "BDMV", "STREAM"))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if IsVideoExt(strings.ToLower(filepath.Ext(e.Name()))) {
			return true
		}
	}
	return false
}

// collectBdmvVideos 返回 BDMV/STREAM 下的视频文件(DirCache 格式),按文件名排序。
func collectBdmvVideos(movieDir string) [][2]string {
	streamDir := filepath.Join(movieDir, "BDMV", "STREAM")
	entries, err := os.ReadDir(streamDir)
	if err != nil {
		return nil
	}
	var result [][2]string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !IsVideoExt(ext) {
			continue
		}
		result = append(result, [2]string{strings.ToLower(e.Name()), filepath.Join(streamDir, e.Name())})
	}
	return result
}

// findBdmvMovieRoot 从视频文件路径向上查找 BDMV 布局的电影根目录。
// 匹配结构: <root>/BDMV/STREAM/<file>;非 BDMV 布局返回空字符串。
func findBdmvMovieRoot(filePath string) string {
	cur := filepath.Dir(filePath)
	for depth := 0; depth < 4 && cur != filepath.Dir(cur); depth++ {
		if strings.EqualFold(filepath.Base(cur), "BDMV") {
			parent := filepath.Dir(cur)
			if isBdmvMovieDir(parent) {
				return parent
			}
		}
		cur = filepath.Dir(cur)
	}
	return ""
}

func collectMovieEntries(dir string, results *[]movieEntry) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") {
			continue
		}
		fullPath := filepath.Join(dir, name)
		if entry.IsDir() {
			if looksLikeSeasonDir(name) {
				continue
			}
			if looksLikeShowDir(fullPath) {
				continue
			}
			// BDMV 布局整目录作为一部电影,name 取电影根目录名,避免把 STREAM 当成电影名。
			if isBdmvMovieDir(fullPath) {
				vids := collectBdmvVideos(fullPath)
				paths := make([]string, 0, len(vids))
				for _, v := range vids {
					paths = append(paths, v[1])
				}
				*results = append(*results, movieEntry{name: name, fullPath: fullPath, isDir: true, videoPaths: paths})
				continue
			}
			var videoPaths []string
			subEntries, err := os.ReadDir(fullPath)
			if err == nil {
				for _, se := range subEntries {
					if se.IsDir() {
						continue
					}
					ext := strings.ToLower(filepath.Ext(se.Name()))
					if IsVideoExt(ext) {
						videoPaths = append(videoPaths, filepath.Join(fullPath, se.Name()))
					}
				}
			}
			if len(videoPaths) > 0 {
				*results = append(*results, movieEntry{name: name, fullPath: fullPath, isDir: true, videoPaths: videoPaths})
			} else {
				collectMovieEntries(fullPath, results)
			}
		} else {
			ext := strings.ToLower(filepath.Ext(name))
			if IsVideoExt(ext) {
				*results = append(*results, movieEntry{name: name, fullPath: fullPath, isDir: false})
			}
		}
	}
}

func scanOneMovie(
	ctx context.Context,
	pool *pgxpool.Pool,
	libraryID string,
	name string,
	fullPath string,
	isDir bool,
	existing map[string]bool,
) {
	if isDir {
		parsed := ParseMovieName(name)
		dirCache := CacheDir(fullPath)
		poster := FindImageCached(dirCache, posterImagePrefixes)
		backdrop := FindImageCached(dirCache, backdropImagePrefixes)
		posterTag := ptrAndThen(poster, GenerateImageTag)
		backdropTag := ptrAndThen(backdrop, GenerateImageTag)

		var videoFiles [][2]string
		for _, entry := range dirCache {
			ext := filepath.Ext(entry[0])
			if IsVideoExt(ext) {
				videoFiles = append(videoFiles, entry)
			}
		}
		// BDMV 布局的电影根目录自身没有视频,视频在 BDMV/STREAM 下。
		if len(videoFiles) == 0 {
			if vids := collectBdmvVideos(fullPath); len(vids) > 0 {
				videoFiles = vids
			}
		}
		if len(videoFiles) == 0 {
			return
		}

		primaryPath := videoFiles[0][1]
		primaryName := videoFiles[0][0]
		ext := strings.TrimPrefix(filepath.Ext(primaryName), ".")
		if ext == "" {
			ext = "mkv"
		}
		sortName := strings.ToLower(parsed.Name)

		if existing[primaryPath] {
			var itemID uuid.UUID
			var existingName string
			if err := pool.QueryRow(ctx,
				"SELECT id, name FROM items WHERE library_id = $1::uuid AND type = 'Movie' AND file_path = $2 LIMIT 1",
				libraryID, primaryPath).Scan(&itemID, &existingName); err == nil {
				// 旧数据 name 被识别成 STREAM/BDMV 等结构化目录名时,一次性修正为当前电影名。
				if isStructuralDiscDirName(existingName) && existingName != parsed.Name {
					pool.Exec(ctx,
						"UPDATE items SET name = $1, sort_name = $2, production_year = COALESCE($3, production_year), updated_at = NOW() WHERE id = $4",
						parsed.Name, sortName, parsed.Year, itemID)
				}
				syncItemArtwork(ctx, pool, itemID, poster, posterTag, backdrop, backdropTag)
				syncItemExtraBackdrops(ctx, pool, itemID, FindExtraFanart(fullPath))
				ensureMovieMediaVersions(ctx, pool, itemID, videoFiles, dirCache)
				// race 场景兜底:NFO 比 video 晚到时,首次 Create 事件没读到 nfo 就 INSERT 了,
				// 等到 nfo 的 Create 再次触发时走到这里,补一次 ApplyNfoData(幂等)。
				if nfoPath := FindNfoCached(dirCache); nfoPath != nil {
					if nfo := ParseNfo(*nfoPath); nfo != nil {
						ApplyNfoDataWithPlatformSource(ctx, pool, itemID.String(), nfo, models.PlatformScanSourceNFO)
						enqueueMovieNfoComplement(ctx, pool, itemID.String())
					}
				}
			}
			return
		}

		mi := ReadMediainfoJSONCached(primaryPath, dirCache)
		var runtimeTicks *int64
		if mi != nil {
			runtimeTicks = getJSONInt64(mi, "RunTimeTicks")
		}

		var insertedID *uuid.UUID
		err := pool.QueryRow(ctx,
			"INSERT INTO items (library_id, type, name, sort_name, production_year, runtime_ticks, file_path, container, primary_image_path, primary_image_tag, backdrop_image_path, backdrop_image_tag, created_at) "+
				"VALUES ($1::uuid, 'Movie', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, COALESCE($12, NOW())) "+
				"ON CONFLICT DO NOTHING RETURNING id",
			libraryID, parsed.Name, sortName, parsed.Year,
			runtimeTicks, primaryPath, ext,
			derefStr(poster), derefStr(posterTag),
			derefStr(backdrop), derefStr(backdropTag),
			fileMtimeOrNil(primaryPath),
		).Scan(&insertedID)

		if err == nil && insertedID != nil {
			ensureMovieMediaVersions(ctx, pool, *insertedID, videoFiles, dirCache)
			syncItemExtraBackdrops(ctx, pool, *insertedID, FindExtraFanart(fullPath))
			if nfoPath := FindNfoCached(dirCache); nfoPath != nil {
				if nfo := ParseNfo(*nfoPath); nfo != nil {
					ApplyNfoDataWithPlatformSource(ctx, pool, insertedID.String(), nfo, models.PlatformScanSourceNFO)
					enqueueMovieNfoComplement(ctx, pool, insertedID.String())
				}
			}
			enqueueIdentifyIfEligible(ctx, pool, insertedID.String(), ScrapePriorityIdentify, "scan.movie.create")
		} else if err == pgx.ErrNoRows {
			// CONFLICT 唯一来源是 idx_items_filepath_unique(同 file_path),优先按 file_path 查;
			// 同时一次性修正旧数据里被识别为 STREAM/BDMV 的错误 name。
			var conflictID uuid.UUID
			var conflictName string
			if err := pool.QueryRow(ctx,
				"SELECT id, name FROM items WHERE file_path = $1 LIMIT 1",
				primaryPath).Scan(&conflictID, &conflictName); err == nil {
				if isStructuralDiscDirName(conflictName) && conflictName != parsed.Name {
					pool.Exec(ctx,
						"UPDATE items SET name = $1, sort_name = $2, production_year = COALESCE($3, production_year), updated_at = NOW() WHERE id = $4",
						parsed.Name, sortName, parsed.Year, conflictID)
				}
				syncItemArtwork(ctx, pool, conflictID, poster, posterTag, backdrop, backdropTag)
				syncItemExtraBackdrops(ctx, pool, conflictID, FindExtraFanart(fullPath))
				ensureMovieMediaVersions(ctx, pool, conflictID, videoFiles, dirCache)
			} else if existingID := findExistingMovieItem(ctx, pool, libraryID, parsed.Name, parsed.Year, primaryPath); existingID != nil {
				syncItemArtwork(ctx, pool, *existingID, poster, posterTag, backdrop, backdropTag)
				syncItemExtraBackdrops(ctx, pool, *existingID, FindExtraFanart(fullPath))
				ensureMovieMediaVersions(ctx, pool, *existingID, videoFiles, dirCache)
			}
		}
	} else {
		ext := strings.ToLower(filepath.Ext(name))
		if !IsVideoExt(ext) {
			return
		}
		parentDir := filepath.Dir(fullPath)
		parentCache := CacheDir(parentDir)
		poster := FindImageCached(parentCache, posterImagePrefixes)
		backdrop := FindImageCached(parentCache, backdropImagePrefixes)
		posterTag := ptrAndThen(poster, GenerateImageTag)
		backdropTag := ptrAndThen(backdrop, GenerateImageTag)
		if existing[fullPath] {
			var itemID uuid.UUID
			if err := pool.QueryRow(ctx,
				"SELECT id FROM items WHERE library_id = $1::uuid AND type = 'Movie' AND file_path = $2 LIMIT 1",
				libraryID, fullPath).Scan(&itemID); err == nil {
				syncItemArtwork(ctx, pool, itemID, poster, posterTag, backdrop, backdropTag)
				ensureMovieMediaVersions(ctx, pool, itemID, [][2]string{{strings.ToLower(filepath.Base(fullPath)), fullPath}}, parentCache)
				// race 场景兜底:同目录下若有独立 movie.nfo,补 apply 一次。单文件布局的 NFO
				// 通常与视频同名(如 xxx.mkv + xxx.nfo),FindNfoCached 也能识别。
				if nfoPath := FindNfoCached(parentCache); nfoPath != nil {
					if nfo := ParseNfo(*nfoPath); nfo != nil {
						ApplyNfoDataWithPlatformSource(ctx, pool, itemID.String(), nfo, models.PlatformScanSourceNFO)
						enqueueMovieNfoComplement(ctx, pool, itemID.String())
					}
				}
			}
			return
		}

		basename := strings.TrimSuffix(name, filepath.Ext(name))
		parsed := ParseMovieName(basename)
		mi := ReadMediainfoJSONCached(fullPath, parentCache)
		var runtimeTicks *int64
		if mi != nil {
			runtimeTicks = getJSONInt64(mi, "RunTimeTicks")
		}
		extStr := strings.TrimPrefix(ext, ".")

		var insertedID *uuid.UUID
		err := pool.QueryRow(ctx,
			"INSERT INTO items (library_id, type, name, sort_name, production_year, runtime_ticks, file_path, container, primary_image_path, primary_image_tag, backdrop_image_path, backdrop_image_tag, created_at) "+
				"VALUES ($1::uuid, 'Movie', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, COALESCE($12, NOW())) "+
				"ON CONFLICT DO NOTHING RETURNING id",
			libraryID, parsed.Name, strings.ToLower(parsed.Name),
			parsed.Year, runtimeTicks, fullPath, extStr,
			derefStr(poster), derefStr(posterTag),
			derefStr(backdrop), derefStr(backdropTag),
			fileMtimeOrNil(fullPath),
		).Scan(&insertedID)
		if err == nil && insertedID != nil {
			ensureMovieMediaVersions(ctx, pool, *insertedID, [][2]string{{strings.ToLower(filepath.Base(fullPath)), fullPath}}, parentCache)
			// 单文件布局首次新建:与目录布局新建分支对称,apply 同目录的 NFO。
			// 不加这段时,NFO 里的 tmdbid/plot/cast 要等下次扫描命中 existing 分支才被补上,
			// 首扫期间 TMDB identify 会基于文件名再识别一轮,容易误判 + 覆盖 NFO 手工编辑。
			if nfoPath := FindNfoCached(parentCache); nfoPath != nil {
				if nfo := ParseNfo(*nfoPath); nfo != nil {
					ApplyNfoDataWithPlatformSource(ctx, pool, insertedID.String(), nfo, models.PlatformScanSourceNFO)
					enqueueMovieNfoComplement(ctx, pool, insertedID.String())
				}
			}
			enqueueIdentifyIfEligible(ctx, pool, insertedID.String(), ScrapePriorityIdentify, "scan.movie.create")
		} else if err == pgx.ErrNoRows {
			if existingID := findExistingMovieItem(ctx, pool, libraryID, parsed.Name, parsed.Year, fullPath); existingID != nil {
				syncItemArtwork(ctx, pool, *existingID, poster, posterTag, backdrop, backdropTag)
				ensureMovieMediaVersions(ctx, pool, *existingID, [][2]string{{strings.ToLower(filepath.Base(fullPath)), fullPath}}, parentCache)
			}
		}
	}
}

func findExistingMovieItem(ctx context.Context, pool *pgxpool.Pool, libraryID, name string, year *int32, filePath string) *uuid.UUID {
	var itemID uuid.UUID
	err := pool.QueryRow(ctx,
		`SELECT id
		 FROM items
		 WHERE library_id = $1::uuid
		   AND type = 'Movie'
		   AND name = $2
		   AND COALESCE(production_year, 0) = COALESCE($3, 0)
		 ORDER BY CASE WHEN file_path = $4 THEN 0 ELSE 1 END, created_at ASC
		 LIMIT 1`,
		libraryID, name, year, filePath,
	).Scan(&itemID)
	if err != nil {
		return nil
	}
	return &itemID
}

func ensureMovieMediaVersions(ctx context.Context, pool *pgxpool.Pool, itemID uuid.UUID, videoFiles [][2]string, dirCache DirCache) {
	for i, f := range videoFiles {
		fpath := f[1]
		verName := strings.TrimSuffix(filepath.Base(fpath), filepath.Ext(fpath))
		if verName == "" {
			verName = "Unknown"
		}
		mi := ReadMediainfoJSONCached(fpath, dirCache)
		isPrimary := i == 0

		container := strings.TrimPrefix(strings.ToLower(filepath.Ext(fpath)), ".")
		if container == "strm" {
			if rp := ResolveStrmPath(fpath); rp != nil {
				resolved := strings.TrimPrefix(filepath.Ext(*rp), ".")
				if resolved != "" {
					container = resolved
				}
			}
		}
		if container == "" {
			container = "mkv"
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

		q, qLabel := ComputeMediaVersionQuality(filepath.Base(fpath), mi)

		pool.Exec(ctx,
			"INSERT INTO media_versions (item_id, name, file_path, container, is_primary, mediainfo, runtime_ticks, bitrate, size, resolution, hdr_format, video_codec, audio_codec, source, quality_label) "+
				"VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) ON CONFLICT DO NOTHING",
			itemID, verName, fpath, container, isPrimary, nullableJSON(miJSON), runtimeTicks, bitrate, size,
			NullableStr(q.Resolution), NullableStr(q.HDRFormat), NullableStr(q.VideoCodec),
			NullableStr(q.AudioCodec), NullableStr(q.Source), NullableStr(qLabel))
	}
}
