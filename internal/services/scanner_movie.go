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
	if !tmdbConfigured(ctx, pool) {
		slog.Debug("[Scan] Skip NFO movie TMDB complement: api key not configured", "movie", itemID)
		return
	}
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

// dirVideosAreDistinctMovies 判断同一目录下的多个视频是「各自独立的影片」还是
// 「同一部电影的多个版本」。
// 做法:把每个文件名解析成片名,只要出现 >=2 个不同的非空片名,就认定为合集目录,
// 应把每个视频拆成独立电影;否则(片名一致或无法区分)按多版本目录合并处理。
// 真·多版本(同名不同清晰度,如 X.1080p / X.2160p)解析后片名相同 → 返回 false 维持合并;
// 即便此处误拆,后续 TMDB 识别到相同 tmdb_id 时 MergeMultiVersionItems 仍会自动合并回多版本。
func dirVideosAreDistinctMovies(videoPaths []string) bool {
	if catalogs, ok := dirVideoCatalogNumbers(videoPaths); ok {
		return len(catalogs) >= 2
	}

	titles := make(map[string]struct{}, len(videoPaths))
	for _, p := range videoPaths {
		title := strings.ToLower(strings.TrimSpace(ParseMovieName(filepath.Base(p)).Name))
		if title == "" {
			continue
		}
		titles[title] = struct{}{}
		if len(titles) >= 2 {
			return true
		}
	}
	return false
}

func dirVideoCatalogNumbers(videoPaths []string) (map[string]struct{}, bool) {
	if len(videoPaths) == 0 {
		return nil, false
	}
	catalogs := make(map[string]struct{}, len(videoPaths))
	for _, p := range videoPaths {
		num := ExtractCatalogNumber(filepath.Base(p))
		if num == "" {
			return nil, false
		}
		catalogs[num] = struct{}{}
	}
	return catalogs, true
}

// resolveMovieDirTarget 把一个视频文件的「实时事件」解析成应入库的电影单元,
// 与手动全扫 collectMovieEntries 的逐子目录判定保持一致 —— 否则实时入库逐文件落库,
// 会把同一部电影的多 part(X-cd1 / X-cd2)拆成两条 Movie,且目录级 poster.jpg/fanart.jpg
// 因「目录含多个视频/多条 Movie」被防合集护栏挡掉,导致没有封面。
//
// 判定规则(完全对齐 collectMovieEntries 第 188~198 行):
//   - 父目录就是库根 → 平铺单片,逐文件入库(isDir=false)
//   - 父目录非库根、且同级出现 >=2 个不同片名(合集平铺,如 .../9总全国探花/*.strm)
//     → 仍逐文件入库
//   - 其余(目录内单片,或多 part 同名)→ 整个父目录当一部电影(isDir=true),
//     交给 scanOneMovie 把多 part 归并成同一 item 的多个 media_versions
//
// 单片也归到目录级是刻意的:collectMovieEntries 对「子目录含 1 个视频」同样产出 isDir=true,
// 这样目录级的 poster.jpg/fanart.jpg 才能被 FindImageCached 直接命中。
func resolveMovieDirTarget(filePath string, libraryRoots []string) (string, bool) {
	parent := filepath.Clean(filepath.Dir(filePath))
	for _, root := range libraryRoots {
		if filepath.Clean(root) == parent {
			return filePath, false
		}
	}
	var siblingVideos []string
	if entries, err := os.ReadDir(parent); err == nil {
		for _, se := range entries {
			if se.IsDir() {
				continue
			}
			if IsVideoExt(strings.ToLower(filepath.Ext(se.Name()))) {
				siblingVideos = append(siblingVideos, filepath.Join(parent, se.Name()))
			}
		}
	}
	if len(siblingVideos) >= 2 && dirVideosAreDistinctMovies(siblingVideos) {
		return filePath, false
	}
	return parent, true
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
			if IsExtrasDirName(name) {
				continue
			}
			// 电影库语义:每个含视频文件的目录都是一部独立电影,不含直接视频的目录递归进去。
			// 不在这里做任何剧集/季判定 —— 否则:
			//   1. 分组目录(如 .../独立创作者/)只要里面有一部名字含"第X季"的电影,
			//      就会被 looksLikeShowDir 误判为剧集,导致整层连同所有电影被跳过;
			//   2. 名字恰好含"第X季"/以季编号开头的单部电影目录会被 looksLikeSeasonDir 单独跳过。
			// 季/剧集识别只属于 tvshows / mixed 库(各自走 collectShowEntries / collectMixedEntries)。
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
				// 合集目录(一个目录里平铺多部各自独立的影片,如 .../9总全国探花/*.strm)
				// 不能当成"一部电影的多个版本",否则目录名会变成唯一片名、各影片沦为它的版本。
				// 解析文件名判定:出现 >=2 个不同片名即逐个拆成独立电影;同名(真·多版本)才合并。
				if len(videoPaths) >= 2 && dirVideosAreDistinctMovies(videoPaths) {
					for _, vp := range videoPaths {
						*results = append(*results, movieEntry{name: filepath.Base(vp), fullPath: vp, isDir: false})
					}
				} else {
					*results = append(*results, movieEntry{name: name, fullPath: fullPath, isDir: true, videoPaths: videoPaths})
				}
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
				setLocalTrailer(ctx, pool, itemID, FindLocalTrailer(fullPath))
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

		// 多 part 乱序到达兜底:某个非主分段(如 cd2)可能在主文件(字母序最靠前的 cd1)
		// 落盘前就被实时事件先建了条目。此时直接 INSERT 主文件不会冲突 → 产生重复条目。
		// 先把已建的同目录条目重指到当前主文件,随后的 INSERT 命中 ON CONFLICT 走同步分支。
		if len(videoFiles) > 1 {
			repointDirMovieToPrimary(ctx, pool, libraryID, primaryPath, videoFiles)
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
			setLocalTrailer(ctx, pool, *insertedID, FindLocalTrailer(fullPath))
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
				setLocalTrailer(ctx, pool, conflictID, FindLocalTrailer(fullPath))
				ensureMovieMediaVersions(ctx, pool, conflictID, videoFiles, dirCache)
			} else if existingID := findExistingMovieItem(ctx, pool, libraryID, parsed.Name, parsed.Year, primaryPath); existingID != nil {
				syncItemArtwork(ctx, pool, *existingID, poster, posterTag, backdrop, backdropTag)
				syncItemExtraBackdrops(ctx, pool, *existingID, FindExtraFanart(fullPath))
				setLocalTrailer(ctx, pool, *existingID, FindLocalTrailer(fullPath))
				ensureMovieMediaVersions(ctx, pool, *existingID, videoFiles, dirCache)
			}
		}
	} else {
		ext := strings.ToLower(filepath.Ext(name))
		if !IsVideoExt(ext) {
			return
		}
		// extras/trailers 目录里的视频不当独立影片(兜底,主拦截在 ingest processCreate)。
		if IsInExtrasFolder(fullPath) {
			return
		}
		parentDir := filepath.Dir(fullPath)
		parentCache := CacheDir(parentDir)
		videoBasename := filepath.Base(fullPath)
		allowGenericSidecars := allowGenericMovieSidecars(parentCache)
		poster := FindMovieImageCached(parentCache, videoBasename, posterImagePrefixes, allowGenericSidecars)
		backdrop := FindMovieImageCached(parentCache, videoBasename, backdropImagePrefixes, allowGenericSidecars)
		posterTag := ptrAndThen(poster, GenerateImageTag)
		backdropTag := ptrAndThen(backdrop, GenerateImageTag)
		nfoPath := FindMovieNfoCached(parentCache, videoBasename, allowGenericSidecars)
		if existing[fullPath] {
			var itemID uuid.UUID
			if err := pool.QueryRow(ctx,
				"SELECT id FROM items WHERE library_id = $1::uuid AND type = 'Movie' AND file_path = $2 LIMIT 1",
				libraryID, fullPath).Scan(&itemID); err == nil {
				syncItemArtwork(ctx, pool, itemID, poster, posterTag, backdrop, backdropTag)
				ensureMovieMediaVersions(ctx, pool, itemID, [][2]string{{strings.ToLower(filepath.Base(fullPath)), fullPath}}, parentCache)
				// race 场景兜底:同名 NFO 比 video 晚到时,再次触发扫描可补 apply 一次。
				// 多电影平铺目录只接受 <视频 basename>.nfo,避免串用目录里的第一个 NFO。
				if nfoPath != nil {
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
			// 单文件布局首次新建:与目录布局新建分支对称,apply 对应 NFO。
			// 不加这段时,NFO 里的 tmdbid/plot/cast 要等下次扫描命中 existing 分支才被补上,
			// 首扫期间 TMDB identify 会基于文件名再识别一轮,容易误判 + 覆盖 NFO 手工编辑。
			if nfoPath != nil {
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

// repointDirMovieToPrimary 在「目录级电影」入库前收敛多 part 乱序到达的重复条目:
// 当主文件(videoFiles[0])尚未入库、但同目录其它分段已先建条目时,
// 把已建条目重指到主文件,使随后的 INSERT 命中 ON CONFLICT 而走同步分支(补齐各分段为多版本),
// 从而避免同一部多 part 电影被拆成多条 Movie。主文件已存在则无需处理(正常顺序到达)。
func repointDirMovieToPrimary(ctx context.Context, pool *pgxpool.Pool, libraryID, primaryPath string, videoFiles [][2]string) {
	var exists bool
	if err := pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM items WHERE library_id = $1::uuid AND type = 'Movie' AND file_path = $2)",
		libraryID, primaryPath).Scan(&exists); err != nil || exists {
		return
	}
	others := make([]string, 0, len(videoFiles))
	for _, f := range videoFiles {
		if f[1] != primaryPath {
			others = append(others, f[1])
		}
	}
	if len(others) == 0 {
		return
	}
	var id uuid.UUID
	if err := pool.QueryRow(ctx,
		"SELECT id FROM items WHERE library_id = $1::uuid AND type = 'Movie' AND file_path = ANY($2) ORDER BY created_at ASC LIMIT 1",
		libraryID, others).Scan(&id); err != nil {
		return
	}
	pool.Exec(ctx,
		"UPDATE items SET file_path = $1, updated_at = NOW() WHERE id = $2",
		primaryPath, id)
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

		var mvID uuid.UUID
		err := pool.QueryRow(ctx,
			`INSERT INTO media_versions (item_id, name, file_path, container, is_primary, mediainfo, runtime_ticks, bitrate, size, resolution, hdr_format, video_codec, audio_codec, source, quality_label)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			 ON CONFLICT (item_id, file_path) DO UPDATE SET
			 	name = EXCLUDED.name,
			 	container = EXCLUDED.container,
			 	is_primary = EXCLUDED.is_primary,
			 	mediainfo = COALESCE(EXCLUDED.mediainfo, media_versions.mediainfo),
			 	runtime_ticks = COALESCE(EXCLUDED.runtime_ticks, media_versions.runtime_ticks),
			 	bitrate = COALESCE(EXCLUDED.bitrate, media_versions.bitrate),
			 	size = COALESCE(EXCLUDED.size, media_versions.size),
			 	resolution = COALESCE(EXCLUDED.resolution, media_versions.resolution),
			 	hdr_format = COALESCE(EXCLUDED.hdr_format, media_versions.hdr_format),
			 	video_codec = COALESCE(EXCLUDED.video_codec, media_versions.video_codec),
			 	audio_codec = COALESCE(EXCLUDED.audio_codec, media_versions.audio_codec),
			 	source = COALESCE(EXCLUDED.source, media_versions.source),
			 	quality_label = COALESCE(EXCLUDED.quality_label, media_versions.quality_label)
			 RETURNING id`,
			itemID, verName, fpath, container, isPrimary, nullableJSON(miJSON), runtimeTicks, bitrate, size,
			NullableStr(q.Resolution), NullableStr(q.HDRFormat), NullableStr(q.VideoCodec),
			NullableStr(q.AudioCodec), NullableStr(q.Source), NullableStr(qLabel)).Scan(&mvID)
		if err != nil {
			continue
		}
		SyncExternalSubtitles(ctx, pool, itemID, mvID, fpath, dirCache)
	}
}
