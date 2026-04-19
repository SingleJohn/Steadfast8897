package services

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/models"
	"fyms/internal/services/scraper"
)

const scanConcurrency = 10

var videoExtSet = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".wmv": true, ".flv": true,
	".webm": true, ".m4v": true, ".mov": true, ".ts": true, ".mpg": true,
	".mpeg": true, ".iso": true, ".bdmv": true, ".m2ts": true, ".vob": true,
	".rmvb": true, ".rm": true, ".3gp": true, ".ogv": true, ".strm": true,
}

var (
	posterImagePrefixes   = []string{"poster", "cover", "folder", "thumb"}
	backdropImagePrefixes = []string{"fanart", "backdrop", "background", "landscape"}
)

func IsVideoExt(ext string) bool {
	return videoExtSet[strings.ToLower(ext)]
}

// ============ NFO Parser ============

type nfoTagPair struct {
	cdata *regexp.Regexp
	plain *regexp.Regexp
}

var nfoTagRegexes map[string]nfoTagPair

var (
	nfoGenreRe = regexp.MustCompile(`(?i)<genre>([^<]*)</genre>`)
	nfoActorRe = regexp.MustCompile(`(?is)<actor>([\s\S]*?)</actor>`)
	nfoNameRe  = regexp.MustCompile(`(?i)<name>([^<]*)</name>`)
	nfoRoleRe  = regexp.MustCompile(`(?i)<role>([^<]*)</role>`)
	nfoTypeRe  = regexp.MustCompile(`(?i)<type>([^<]*)</type>`)
	nfoTmdbRe  = regexp.MustCompile(`(?i)<tmdbid>([^<]*)</tmdbid>`)
)

func init() {
	tags := []string{"title", "originaltitle", "plot", "tagline", "year", "rating", "tmdbid", "imdbid", "premiered", "studio"}
	nfoTagRegexes = make(map[string]nfoTagPair, len(tags))
	for _, name := range tags {
		nfoTagRegexes[name] = nfoTagPair{
			cdata: regexp.MustCompile(`(?is)<` + name + `><!\[CDATA\[([\s\S]*?)\]\]></` + name + `>`),
			plain: regexp.MustCompile(`(?i)<` + name + `>([^<]*)</` + name + `>`),
		}
	}
}

func nfoTag(xml, name string) *string {
	pair, ok := nfoTagRegexes[name]
	if !ok {
		return nil
	}
	if m := pair.cdata.FindStringSubmatch(xml); m != nil {
		s := strings.TrimSpace(m[1])
		return &s
	}
	if m := pair.plain.FindStringSubmatch(xml); m != nil {
		s := strings.TrimSpace(m[1])
		return &s
	}
	return nil
}

type NfoData struct {
	Title         *string
	OriginalTitle *string
	Plot          *string
	Year          *int32
	Rating        *float64
	TmdbID        *int32
	ImdbID        *string
	Genres        []string
	Actors        []NfoActor
	Directors     []string
	Premiered     *string
	Tagline       *string
	Studio        *string
}

type NfoActor struct {
	Name     string
	Role     string
	TmdbID   *int32
	ImageURL *string
}

func ParseNfo(nfoPath string) *NfoData {
	data, err := os.ReadFile(nfoPath)
	if err != nil {
		return nil
	}
	xml := string(data)
	if strings.HasPrefix(xml, "\uFEFF") {
		xml = xml[3:]
	}

	result := &NfoData{}

	result.Title = nfoTag(xml, "title")
	result.OriginalTitle = nfoTag(xml, "originaltitle")
	result.Plot = nfoTag(xml, "plot")
	result.Tagline = nfoTag(xml, "tagline")
	if s := nfoTag(xml, "year"); s != nil {
		if v, err := strconv.ParseInt(*s, 10, 32); err == nil {
			i := int32(v)
			result.Year = &i
		}
	}
	if s := nfoTag(xml, "rating"); s != nil {
		if v, err := strconv.ParseFloat(*s, 64); err == nil {
			result.Rating = &v
		}
	}
	if s := nfoTag(xml, "tmdbid"); s != nil {
		if v, err := strconv.ParseInt(*s, 10, 32); err == nil {
			i := int32(v)
			result.TmdbID = &i
		}
	}
	result.ImdbID = nfoTag(xml, "imdbid")
	result.Premiered = nfoTag(xml, "premiered")

	for _, m := range nfoGenreRe.FindAllStringSubmatch(xml, -1) {
		g := strings.TrimSpace(m[1])
		if g != "" {
			result.Genres = append(result.Genres, g)
		}
	}

	for _, m := range nfoActorRe.FindAllStringSubmatch(xml, -1) {
		block := m[1]
		nameMatch := nfoNameRe.FindStringSubmatch(block)
		if nameMatch == nil {
			continue
		}
		name := strings.TrimSpace(nameMatch[1])

		role := ""
		if rm := nfoRoleRe.FindStringSubmatch(block); rm != nil {
			role = strings.TrimSpace(rm[1])
		}
		atype := "Actor"
		if tm := nfoTypeRe.FindStringSubmatch(block); tm != nil {
			atype = strings.TrimSpace(tm[1])
		}
		var tmdbID *int32
		if tm := nfoTmdbRe.FindStringSubmatch(block); tm != nil {
			if v, err := strconv.ParseInt(strings.TrimSpace(tm[1]), 10, 32); err == nil {
				i := int32(v)
				tmdbID = &i
			}
		}

		if atype == "Director" {
			result.Directors = append(result.Directors, name)
		} else {
			result.Actors = append(result.Actors, NfoActor{Name: name, Role: role, TmdbID: tmdbID})
		}
	}

	dirRe := regexp.MustCompile(`(?i)<director>([^<]*)</director>`)
	for _, m := range dirRe.FindAllStringSubmatch(xml, -1) {
		d := strings.TrimSpace(m[1])
		if d == "" {
			continue
		}
		found := false
		for _, existing := range result.Directors {
			if existing == d {
				found = true
				break
			}
		}
		if !found {
			result.Directors = append(result.Directors, d)
		}
	}

	// Extract first <studio> tag
	result.Studio = nfoTag(xml, "studio")

	return result
}

// ============ Apply NFO data to DB ============

func ApplyNfoData(ctx context.Context, pool *pgxpool.Pool, itemID string, nfo *NfoData) {
	ApplyNfoDataWithType(ctx, pool, itemID, "", nfo, "")
}

func ApplyNfoDataWithPlatformSource(ctx context.Context, pool *pgxpool.Pool, itemID string, nfo *NfoData, source models.PlatformScanSource) {
	ApplyNfoDataWithType(ctx, pool, itemID, "", nfo, source)
}

// ApplyNfoDataWithType 单 item 元数据落库。整个 Apply 包一个事务,
// 避免原先 20~40 次独立 pool.Exec 带来的 round-trip + WAL sync 风暴。
// itemType 为空时内部会 fallback 查一次 items.type(仅影响 sort_name 是否写入);
// 调用方已知 itemType(比如 applyMergedDetails)应直接传入,省掉这次往返。
func ApplyNfoDataWithType(ctx context.Context, pool *pgxpool.Pool, itemID string, itemType string, nfo *NfoData, source models.PlatformScanSource) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		slog.Warn("[ApplyNfo] begin tx failed", "item_id", itemID, "error", err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	setClauses := make([]string, 0, 10)
	args := make([]any, 0, 10)
	argIdx := 1

	addClause := func(column, castSuffix string, value any) {
		if castSuffix != "" {
			setClauses = append(setClauses, fmt.Sprintf("%s = $%d%s", column, argIdx, castSuffix))
		} else {
			setClauses = append(setClauses, fmt.Sprintf("%s = $%d", column, argIdx))
		}
		args = append(args, value)
		argIdx++
	}

	if nfo.Plot != nil {
		addClause("overview", "", *nfo.Plot)
	}
	if nfo.Rating != nil && *nfo.Rating > 1.0 {
		addClause("community_rating", "", float32(*nfo.Rating))
	}
	if nfo.TmdbID != nil {
		addClause("tmdb_id", "", *nfo.TmdbID)
	}
	if nfo.ImdbID != nil {
		addClause("imdb_id", "", *nfo.ImdbID)
	}
	if nfo.Premiered != nil {
		if premiered := strings.TrimSpace(*nfo.Premiered); premiered != "" {
			addClause("premiere_date", "::date", premiered)
		}
	}
	if nfo.Year != nil {
		addClause("production_year", "", *nfo.Year)
	}
	if nfo.Title != nil {
		addClause("name", "", *nfo.Title)
		effType := itemType
		if effType == "" {
			_ = tx.QueryRow(ctx, "SELECT type FROM items WHERE id = $1::uuid", itemID).Scan(&effType)
		}
		if effType != "Episode" {
			addClause("sort_name", "", strings.ToLower(*nfo.Title))
		}
	}
	if nfo.Tagline != nil {
		addClause("tagline", "", *nfo.Tagline)
	}
	if nfo.Studio != nil {
		studio := strings.TrimSpace(*nfo.Studio)
		if studio != "" {
			addClause("studio", "", studio)
			addClause("platform_scan_status", "", string(models.PlatformScanMatched))
			if source != "" {
				addClause("platform_scan_source", "", string(source))
			}
			addClause("platform_scan_error", "", nil)
			setClauses = append(setClauses, "platform_scanned_at = NOW()")
		}
	}

	if len(setClauses) > 0 {
		setClauses = append(setClauses, "updated_at = NOW()")
		query := fmt.Sprintf("UPDATE items SET %s WHERE id = $%d::uuid",
			strings.Join(setClauses, ", "), argIdx)
		args = append(args, itemID)
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				// 唯一约束冲突(同库重名等):回滚主事务,用独立连接把 item
				// 打成 error 状态,让未匹配/异常面板可见。
				_ = tx.Rollback(ctx)
				committed = true
				_, markErr := pool.Exec(ctx,
					`UPDATE items
					    SET platform_scan_status = 'error',
					        platform_scan_error  = $1,
					        platform_scanned_at  = NOW(),
					        updated_at           = NOW()
					  WHERE id = $2::uuid`,
					fmt.Sprintf("元数据写入冲突: %s", pgErr.Detail), itemID)
				if markErr != nil {
					slog.Warn("[ApplyNfo] mark error status failed", "item_id", itemID, "error", markErr)
				}
				slog.Warn("[ApplyNfo] unique constraint conflict",
					"item_id", itemID, "constraint", pgErr.ConstraintName, "detail", pgErr.Detail)
				return
			}
			slog.Warn("[ApplyNfo] update items failed", "item_id", itemID, "error", err)
			return
		}
	}

	if len(nfo.Genres) > 0 {
		if _, err := tx.Exec(ctx, "DELETE FROM item_genres WHERE item_id = $1::uuid", itemID); err != nil {
			slog.Warn("[ApplyNfo] delete item_genres failed", "item_id", itemID, "error", err)
			return
		}
		if _, err := tx.Exec(ctx,
			"INSERT INTO genres (name) SELECT unnest($1::text[]) ON CONFLICT (name) DO NOTHING",
			nfo.Genres); err != nil {
			slog.Warn("[ApplyNfo] upsert genres failed", "item_id", itemID, "error", err)
			return
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO item_genres (item_id, genre_id)
			   SELECT $1::uuid, id FROM genres WHERE name = ANY($2::text[])
			 ON CONFLICT DO NOTHING`,
			itemID, nfo.Genres); err != nil {
			slog.Warn("[ApplyNfo] link item_genres failed", "item_id", itemID, "error", err)
			return
		}
	}

	if len(nfo.Actors) > 0 || len(nfo.Directors) > 0 {
		existingImages := make(map[string]string)
		rows, qerr := tx.Query(ctx,
			"SELECT name, role, image_url FROM cast_members WHERE item_id = $1::uuid AND image_url IS NOT NULL AND image_url <> ''",
			itemID)
		if qerr == nil {
			for rows.Next() {
				var name, role, imageURL string
				if rows.Scan(&name, &role, &imageURL) == nil {
					existingImages[name+"|"+role] = imageURL
				}
			}
			rows.Close()
		}

		if _, err := tx.Exec(ctx, "DELETE FROM cast_members WHERE item_id = $1::uuid", itemID); err != nil {
			slog.Warn("[ApplyNfo] delete cast_members failed", "item_id", itemID, "error", err)
			return
		}

		itemUUID, perr := uuid.Parse(itemID)
		if perr != nil {
			slog.Warn("[ApplyNfo] parse item uuid failed", "item_id", itemID, "error", perr)
			return
		}

		type castRow struct {
			name, character, role string
			orderIndex            int32
			tmdbID                *int32
			imageURL              *string
		}
		actorLimit := len(nfo.Actors)
		if actorLimit > 20 {
			actorLimit = 20
		}
		castRows := make([]castRow, 0, len(nfo.Directors)+actorLimit)
		for _, dir := range nfo.Directors {
			castRows = append(castRows, castRow{name: dir, role: "Director"})
		}
		for i := 0; i < actorLimit; i++ {
			a := nfo.Actors[i]
			imageURL := a.ImageURL
			if imageURL == nil || *imageURL == "" {
				if existing := existingImages[a.Name+"|Actor"]; existing != "" {
					imageURL = &existing
				}
			}
			castRows = append(castRows, castRow{
				name: a.Name, character: a.Role, role: "Actor",
				orderIndex: int32(i), tmdbID: a.TmdbID, imageURL: imageURL,
			})
		}

		if len(castRows) > 0 {
			if _, err := tx.CopyFrom(ctx,
				pgx.Identifier{"cast_members"},
				[]string{"item_id", "name", "character", "role", "order_index", "tmdb_id", "image_url"},
				pgx.CopyFromSlice(len(castRows), func(i int) ([]any, error) {
					r := castRows[i]
					return []any{itemUUID, r.name, r.character, r.role, r.orderIndex, r.tmdbID, r.imageURL}, nil
				}),
			); err != nil {
				slog.Warn("[ApplyNfo] copy cast_members failed", "item_id", itemID, "error", err)
				return
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		slog.Warn("[ApplyNfo] commit failed", "item_id", itemID, "error", err)
		return
	}
	committed = true
}

// ============ Filename Parsing ============

type ParsedMovie struct {
	Name string
	Year *int32
}

func ParseMovieName(name string) ParsedMovie {
	p := scraper.Parse(name, scraper.ModeMovie)
	title := preferTitle(p, name)
	return ParsedMovie{Name: title, Year: p.Year}
}

// preferTitle 在 Title 为空时回落到 OriginalTitle 或原始名。
func preferTitle(p scraper.ParsedName, raw string) string {
	if p.Title != "" {
		return p.Title
	}
	if p.OriginalTitle != "" {
		return p.OriginalTitle
	}
	return raw
}

type ParsedEpisode struct {
	Season  int32
	Episode *int32
	Title   *string
}

func ParseEpisodeInfo(filename string) *ParsedEpisode {
	p := scraper.Parse(filename, scraper.ModeEpisode)
	if p.Episode == nil {
		return nil
	}
	season := int32(1)
	if p.Season != nil {
		season = *p.Season
	}
	return &ParsedEpisode{Season: season, Episode: p.Episode}
}

// ============ Directory Utilities ============

type DirCache = [][2]string // [name_lowercase, full_path]

func CacheDir(dir string) DirCache {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	result := make(DirCache, 0, len(entries))
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if strings.HasPrefix(name, "._") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		result = append(result, [2]string{name, path})
	}
	return result
}

func FindImage(dir string, prefixes []string) *string {
	return FindImageCached(CacheDir(dir), prefixes)
}

func FindImageCached(cache DirCache, prefixes []string) *string {
	imageExts := map[string]bool{"jpg": true, "jpeg": true, "png": true, "webp": true}
	for _, entry := range cache {
		name, path := entry[0], entry[1]
		ext := strings.TrimPrefix(filepath.Ext(name), ".")
		if !imageExts[ext] {
			continue
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		for _, prefix := range prefixes {
			if strings.HasPrefix(stem, prefix) || strings.HasSuffix(stem, prefix) {
				return &path
			}
		}
	}
	return nil
}

// FindEpisodeThumbCached 在同目录(seasonCache)内查找 Episode 的本地分集封面,
// 约定顺序(参考 Emby / Jellyfin 文件命名):
//  1. <basename>-thumb.(jpg|png|webp|jpeg)
//  2. <basename>.thumb.(jpg|png|webp|jpeg)
//  3. <basename>.(jpg|png|webp|jpeg)(要求文件茎必须等于视频名,避免误命中 poster/fanart)
func FindEpisodeThumbCached(cache DirCache, videoBasename string) *string {
	if cache == nil || videoBasename == "" {
		return nil
	}
	stem := strings.ToLower(strings.TrimSuffix(videoBasename, filepath.Ext(videoBasename)))
	if stem == "" {
		return nil
	}
	imageExts := map[string]bool{"jpg": true, "jpeg": true, "png": true, "webp": true}
	candidates := []string{
		stem + "-thumb",
		stem + ".thumb",
		stem,
	}
	for _, want := range candidates {
		for _, entry := range cache {
			name, path := entry[0], entry[1]
			ext := strings.TrimPrefix(filepath.Ext(name), ".")
			if !imageExts[ext] {
				continue
			}
			s := strings.TrimSuffix(name, filepath.Ext(name))
			if s == want {
				return &path
			}
		}
	}
	return nil
}

func FindNfo(dir string) *string {
	return FindNfoCached(CacheDir(dir))
}

func FindNfoCached(cache DirCache) *string {
	for _, entry := range cache {
		if strings.HasSuffix(entry[0], ".nfo") {
			return &entry[1]
		}
	}
	return nil
}

func GenerateImageTag(filePath string) *string {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil
	}
	mtime := info.ModTime().Unix()
	input := fmt.Sprintf("%s:%d", filePath, mtime)
	digest := md5.Sum([]byte(input))
	tag := fmt.Sprintf("%x", digest)
	return &tag
}

func syncItemArtwork(
	ctx context.Context,
	pool *pgxpool.Pool,
	itemID uuid.UUID,
	poster *string,
	posterTag *string,
	backdrop *string,
	backdropTag *string,
) {
	if (poster == nil || *poster == "") && (backdrop == nil || *backdrop == "") {
		return
	}

	_, err := pool.Exec(ctx,
		`UPDATE items
		 SET primary_image_path = CASE WHEN NULLIF($2, '') IS NOT NULL THEN $2 ELSE primary_image_path END,
		     primary_image_tag = CASE WHEN NULLIF($3, '') IS NOT NULL THEN $3 ELSE primary_image_tag END,
		     backdrop_image_path = CASE WHEN NULLIF($4, '') IS NOT NULL THEN $4 ELSE backdrop_image_path END,
		     backdrop_image_tag = CASE WHEN NULLIF($5, '') IS NOT NULL THEN $5 ELSE backdrop_image_tag END,
		     updated_at = NOW()
		 WHERE id = $1::uuid`,
		itemID,
		derefStr(poster),
		derefStr(posterTag),
		derefStr(backdrop),
		derefStr(backdropTag),
	)
	if err != nil {
		slog.Warn("[Scan] Failed to sync artwork", "itemId", itemID, "error", err)
	}
}

func ReadMediainfoJSON(filePath string) map[string]interface{} {
	stem := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	dir := filepath.Dir(filePath)
	jsonPath := filepath.Join(dir, stem+"-mediainfo.json")
	return readMediainfoJSONFromPath(jsonPath)
}

func ReadMediainfoJSONCached(filePath string, cache DirCache) map[string]interface{} {
	stem := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	jsonName := strings.ToLower(stem + "-mediainfo.json")

	for _, entry := range cache {
		if entry[0] == jsonName {
			return readMediainfoJSONFromPath(entry[1])
		}
	}
	for _, entry := range cache {
		if strings.HasSuffix(entry[0], "-mediainfo.json") {
			return readMediainfoJSONFromPath(entry[1])
		}
	}
	return nil
}

func readMediainfoJSONFromPath(jsonPath string) map[string]interface{} {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil
	}

	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}

	if arr, ok := raw.([]interface{}); ok {
		if len(arr) == 0 {
			return nil
		}
		entry, ok := arr[0].(map[string]interface{})
		if !ok {
			return nil
		}
		if msi, ok := entry["MediaSourceInfo"].(map[string]interface{}); ok {
			return msi
		}
		return entry
	}

	if obj, ok := raw.(map[string]interface{}); ok {
		if msi, ok := obj["MediaSourceInfo"].(map[string]interface{}); ok {
			return msi
		}
		return obj
	}
	return nil
}

func ResolveStrmPath(filePath string) *string {
	if !strings.HasSuffix(filePath, ".strm") {
		return nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}
	lines := strings.SplitN(string(data), "\n", 2)
	if len(lines) == 0 {
		return nil
	}
	line := strings.TrimSpace(lines[0])
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}

	resolved := line
	if !strings.HasPrefix(resolved, "http") && strings.HasPrefix(resolved, "/") {
		if _, err := os.Stat(resolved); err != nil {
			mnt := "/mnt" + resolved
			if _, err := os.Stat(mnt); err == nil {
				resolved = mnt
			} else {
				fixed := strings.Replace(resolved, "/CloudNAS", "/mnt/CloudNAS", 1)
				if fixed != resolved {
					if _, err := os.Stat(fixed); err == nil {
						resolved = fixed
					}
				}
			}
		}
	}
	return &resolved
}

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

// ============ Scan Libraries ============

func ScanAllLibraries(ctx context.Context, pool *pgxpool.Pool, cache *CacheService, tracker *ScanProgressTracker) {
	libs, err := models.GetAllLibraries(ctx, pool)
	if err != nil {
		slog.Error("Failed to get libraries", "error", err)
		return
	}

	var wg sync.WaitGroup
	for _, lib := range libs {
		wg.Add(1)
		go func(lib models.Library) {
			defer wg.Done()
			ScanLibrary(ctx, pool, cache, tracker, lib.ID.String(), lib.CollectionType, lib.Paths, lib.Name)
		}(lib)
	}
	wg.Wait()
}

func ScanLibrary(
	ctx context.Context,
	pool *pgxpool.Pool,
	cache *CacheService,
	tracker *ScanProgressTracker,
	libraryID, collectionType string,
	paths []string,
	libraryName string,
) {
	if tracker.IsScanning(libraryID) {
		slog.Warn("Library is already scanning", "library", libraryName)
		return
	}

	slog.Info("[Scan] Starting scan", "library", libraryName, "type", collectionType)

	cache.Del(ctx, "views:all")

	tracker.StartScan(libraryID, libraryName, 0)

	go func() {
		// NFS 遍历收集文件列表（在 goroutine 中，不阻塞调用方）
		var movieEntries []movieEntry
		var showDirs [][2]string
		if collectionType == "tvshows" {
			for _, p := range paths {
				collectShowDirs(p, &showDirs)
			}
			tracker.UpdateTotal(libraryID, int64(len(showDirs)))
			slog.Info("[Scan] Collected entries", "library", libraryName, "shows", len(showDirs))
		} else {
			for _, p := range paths {
				collectMovieEntries(p, &movieEntries)
			}
			tracker.UpdateTotal(libraryID, int64(len(movieEntries)))
			slog.Info("[Scan] Collected entries", "library", libraryName, "movies", len(movieEntries))
		}

		var scanErr error
		if collectionType == "tvshows" {
			scanErr = scanTvShowsWithEntries(ctx, pool, libraryID, showDirs, tracker)
		} else {
			scanErr = scanMoviesWithEntries(ctx, pool, libraryID, movieEntries, tracker)
		}

		if scanErr != nil {
			slog.Error("[Scan] Failed", "library", libraryName, "error", scanErr)
			tracker.FailScan(libraryID, scanErr.Error())
			return
		}

		slog.Info("[Scan] Completed", "library", libraryName)
		cleanupMissingItems(ctx, pool, libraryID)
		tracker.CompleteScan(libraryID, cache)

		go backfillMediaVersions(ctx, pool)

		go autoScrapeNewItems(ctx, pool, libraryID)

		go func() {
			merged, merr := models.MergeMultiVersionItems(ctx, pool)
			if merr != nil {
				slog.Error("[Scan] MergeVersions failed", "error", merr)
			} else if merged > 0 {
				slog.Info("[Scan] MergeVersions completed", "merged", merged)
			}
		}()
	}()
}

// autoScrapeRunning 保证同一 library 同时只有一个 autoScrapeNewItems 在跑,
// 避免扫库频繁触发(file_watcher / 手动)时多个 goroutine 抢同一批未刮削 item,
// 造成 PG 重复 UPDATE / 事务回滚风暴。
var autoScrapeRunning sync.Map // libraryID -> *atomic.Bool

func autoScrapeNewItems(ctx context.Context, pool *pgxpool.Pool, libraryID string) {
	flagAny, _ := autoScrapeRunning.LoadOrStore(libraryID, &atomic.Bool{})
	flag := flagAny.(*atomic.Bool)
	if !flag.CompareAndSwap(false, true) {
		slog.Debug("[AutoScrape] Already running for library, skip", "library", libraryID)
		return
	}
	defer flag.Store(false)

	var autoEnabled *string
	pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'auto_scrape_enabled'").Scan(&autoEnabled)
	if autoEnabled == nil || *autoEnabled != "true" {
		return
	}

	client := TmdbClientFromConfig(ctx, pool)
	if client == nil {
		slog.Warn("[AutoScrape] TMDB API key not configured, skipping")
		return
	}

	type newItem struct {
		id   string
		name string
	}

	totalSuccess, totalFailed := 0, 0
	for batch := 0; ; batch++ {
		rows, err := pool.Query(ctx,
			"SELECT id::text, name FROM items WHERE library_id = $1::uuid AND type IN ('Movie', 'Series') "+
				"AND (overview IS NULL OR overview = '') "+
				"AND (identify_cooldown_until IS NULL OR identify_cooldown_until < NOW()) "+
				"ORDER BY created_at DESC LIMIT 50",
			libraryID)
		if err != nil {
			break
		}

		var items []newItem
		for rows.Next() {
			var item newItem
			if err := rows.Scan(&item.id, &item.name); err != nil {
				continue
			}
			items = append(items, item)
		}
		rows.Close()

		if len(items) == 0 {
			break
		}

		slog.Info("[AutoScrape] Batch start", "batch", batch+1, "count", len(items), "library", libraryID)

		success, failed := 0, 0
		for _, item := range items {
			_, err := ScrapeItemWithClient(ctx, pool, item.id, client)
			if err != nil {
				failed++
				slog.Debug("[AutoScrape] Failed", "name", item.name, "error", err)
			} else {
				success++
			}
			time.Sleep(300 * time.Millisecond)
		}

		totalSuccess += success
		totalFailed += failed
		slog.Info("[AutoScrape] Batch done", "batch", batch+1, "success", success, "failed", failed)

		// 如果全部失败说明 TMDB 不可达，停止
		if success == 0 {
			slog.Warn("[AutoScrape] All items in batch failed, stopping", "library", libraryID)
			break
		}
	}

	if totalSuccess > 0 || totalFailed > 0 {
		slog.Info("[AutoScrape] Done", "success", totalSuccess, "failed", totalFailed)
	}
}

// ============ Cleanup ============

func cleanupMissingItems(ctx context.Context, pool *pgxpool.Pool, libraryID string) {
	rows, err := pool.Query(ctx,
		"SELECT id, type, file_path FROM items WHERE library_id = $1::uuid AND file_path IS NOT NULL AND type IN ('Movie', 'Episode')",
		libraryID)
	if err != nil {
		return
	}
	defer rows.Close()

	type itemRow struct {
		id       uuid.UUID
		itemType string
		filePath string
	}
	var items []itemRow
	for rows.Next() {
		var r itemRow
		if err := rows.Scan(&r.id, &r.itemType, &r.filePath); err != nil {
			continue
		}
		items = append(items, r)
	}
	rows.Close()

	var removed int64
	for _, item := range items {
		if _, err := os.Stat(item.filePath); os.IsNotExist(err) {
			pool.Exec(ctx, "DELETE FROM items WHERE id = $1", item.id)
			removed++
		}
	}

	if removed == 0 {
		return
	}
	slog.Info("[Cleanup] Removed items with missing files", "count", removed, "library", libraryID)

	// Remove empty Seasons
	seasonRows, err := pool.Query(ctx,
		"SELECT s.id FROM items s WHERE s.library_id = $1::uuid AND s.type = 'Season' "+
			"AND NOT EXISTS (SELECT 1 FROM items e WHERE e.parent_id = s.id AND e.type = 'Episode')",
		libraryID)
	if err == nil {
		var emptySeasons []uuid.UUID
		for seasonRows.Next() {
			var id uuid.UUID
			if seasonRows.Scan(&id) == nil {
				emptySeasons = append(emptySeasons, id)
			}
		}
		seasonRows.Close()
		for _, id := range emptySeasons {
			pool.Exec(ctx, "DELETE FROM items WHERE id = $1", id)
		}
		if len(emptySeasons) > 0 {
			slog.Info("[Cleanup] Removed empty seasons", "count", len(emptySeasons))
		}
	}

	// Remove empty Series
	seriesRows, err := pool.Query(ctx,
		"SELECT s.id FROM items s WHERE s.library_id = $1::uuid AND s.type = 'Series' "+
			"AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = s.id)",
		libraryID)
	if err == nil {
		var emptySeries []uuid.UUID
		for seriesRows.Next() {
			var id uuid.UUID
			if seriesRows.Scan(&id) == nil {
				emptySeries = append(emptySeries, id)
			}
		}
		seriesRows.Close()
		for _, id := range emptySeries {
			pool.Exec(ctx, "DELETE FROM items WHERE id = $1", id)
		}
		if len(emptySeries) > 0 {
			slog.Info("[Cleanup] Removed empty series", "count", len(emptySeries))
		}
	}
}

// ============ Backfill ============

func backfillMediaVersions(ctx context.Context, pool *pgxpool.Pool) {
	rows, err := pool.Query(ctx,
		"SELECT i.id, i.file_path, i.container FROM items i "+
			"WHERE i.type IN ('Movie', 'Episode') AND i.file_path IS NOT NULL "+
			"AND NOT EXISTS (SELECT 1 FROM media_versions mv WHERE mv.item_id = i.id) "+
			"ORDER BY i.created_at DESC")
	if err != nil {
		return
	}
	defer rows.Close()

	type backfillRow struct {
		id        uuid.UUID
		filePath  string
		container string
	}
	var items []backfillRow
	for rows.Next() {
		var r backfillRow
		var fp, ct *string
		if err := rows.Scan(&r.id, &fp, &ct); err != nil {
			continue
		}
		if fp != nil {
			r.filePath = *fp
		}
		if ct != nil {
			r.container = *ct
		}
		items = append(items, r)
	}
	rows.Close()

	if len(items) == 0 {
		return
	}
	slog.Info("[Backfill] Creating media_versions", "count", len(items))

	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup
	var count atomic.Int64

	for _, item := range items {
		wg.Add(1)
		go func(item backfillRow) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(item.filePath), "."))
			var vfContainer string
			if ext == "strm" {
				if rp := ResolveStrmPath(item.filePath); rp != nil {
					vfContainer = strings.TrimPrefix(filepath.Ext(*rp), ".")
				}
				if vfContainer == "" {
					vfContainer = item.container
				}
			} else if item.container != "" {
				vfContainer = item.container
			} else {
				vfContainer = ext
			}

			name := strings.TrimSuffix(filepath.Base(item.filePath), filepath.Ext(item.filePath))
			if name == "" {
				name = "Unknown"
			}
			mi := ReadMediainfoJSON(item.filePath)
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

			q, qLabel := ComputeMediaVersionQuality(filepath.Base(item.filePath), mi)

			pool.Exec(ctx,
				"INSERT INTO media_versions (item_id, name, file_path, container, is_primary, mediainfo, runtime_ticks, bitrate, size, resolution, hdr_format, video_codec, audio_codec, source, quality_label) "+
					"VALUES ($1, $2, $3, $4, TRUE, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) ON CONFLICT DO NOTHING",
				item.id, name, item.filePath, vfContainer, nullableJSON(miJSON), runtimeTicks, bitrate, size,
				NullableStr(q.Resolution), NullableStr(q.HDRFormat), NullableStr(q.VideoCodec),
				NullableStr(q.AudioCodec), NullableStr(q.Source), NullableStr(qLabel))
			count.Add(1)
		}(item)
	}
	wg.Wait()
	slog.Info("[Backfill] media_versions created", "count", count.Load())
}

// ============ Movie Scanning ============

type movieEntry struct {
	name     string
	fullPath string
	isDir    bool
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
			hasVideo := false
			subEntries, err := os.ReadDir(fullPath)
			if err == nil {
				for _, se := range subEntries {
					ext := strings.ToLower(filepath.Ext(se.Name()))
					if IsVideoExt(ext) {
						hasVideo = true
						break
					}
				}
			}
			if hasVideo {
				*results = append(*results, movieEntry{name: name, fullPath: fullPath, isDir: true})
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

func scanMoviesWithEntries(
	ctx context.Context,
	pool *pgxpool.Pool,
	libraryID string,
	allEntries []movieEntry,
	tracker *ScanProgressTracker,
) error {
	existing := make(map[string]bool)
	rows, err := pool.Query(ctx,
		"SELECT file_path FROM items WHERE library_id = $1::uuid AND type = 'Movie'", libraryID)
	if err != nil {
		return fmt.Errorf("load existing: %w", err)
	}
	for rows.Next() {
		var fp string
		if rows.Scan(&fp) == nil {
			existing[fp] = true
		}
	}
	rows.Close()

	sem := make(chan struct{}, scanConcurrency)
	var wg sync.WaitGroup
	var processed atomic.Int64

	for _, entry := range allEntries {
		wg.Add(1)
		go func(entry movieEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			scanOneMovie(ctx, pool, libraryID, entry.name, entry.fullPath, entry.isDir, existing)
			p := processed.Add(1)
			tracker.UpdateScan(libraryID, p, &entry.name)
		}(entry)
	}
	wg.Wait()
	return nil
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
		if len(videoFiles) == 0 {
			return
		}

		primaryPath := videoFiles[0][1]
		primaryName := videoFiles[0][0]
		ext := strings.TrimPrefix(filepath.Ext(primaryName), ".")
		if ext == "" {
			ext = "mkv"
		}

		if existing[primaryPath] {
			var itemID uuid.UUID
			if err := pool.QueryRow(ctx,
				"SELECT id FROM items WHERE library_id = $1::uuid AND type = 'Movie' AND file_path = $2 LIMIT 1",
				libraryID, primaryPath).Scan(&itemID); err == nil {
				syncItemArtwork(ctx, pool, itemID, poster, posterTag, backdrop, backdropTag)
				ensureMovieMediaVersions(ctx, pool, itemID, videoFiles, dirCache)
			}
			return
		}

		mi := ReadMediainfoJSONCached(primaryPath, dirCache)
		sortName := strings.ToLower(parsed.Name)
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
			if nfoPath := FindNfoCached(dirCache); nfoPath != nil {
				if nfo := ParseNfo(*nfoPath); nfo != nil {
					ApplyNfoDataWithPlatformSource(ctx, pool, insertedID.String(), nfo, models.PlatformScanSourceNFO)
				}
			}
		} else if err == pgx.ErrNoRows {
			if existingID := findExistingMovieItem(ctx, pool, libraryID, parsed.Name, parsed.Year, primaryPath); existingID != nil {
				syncItemArtwork(ctx, pool, *existingID, poster, posterTag, backdrop, backdropTag)
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

func collectShowDirs(dir string, results *[][2]string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") {
			continue
		}
		if !entry.IsDir() {
			continue
		}
		fullPath := filepath.Join(dir, name)
		if isShowDir(fullPath) {
			*results = append(*results, [2]string{name, fullPath})
		} else {
			collectShowDirs(fullPath, results)
		}
	}
}

func scanTvShowsWithEntries(
	ctx context.Context,
	pool *pgxpool.Pool,
	libraryID string,
	showDirs [][2]string,
	tracker *ScanProgressTracker,
) error {
	existingEps := make(map[string]bool)
	rows, err := pool.Query(ctx,
		"SELECT file_path FROM items WHERE library_id = $1::uuid AND type = 'Episode' AND file_path IS NOT NULL",
		libraryID)
	if err != nil {
		return fmt.Errorf("load existing episodes: %w", err)
	}
	for rows.Next() {
		var fp string
		if rows.Scan(&fp) == nil {
			existingEps[fp] = true
		}
	}
	rows.Close()

	sem := make(chan struct{}, scanConcurrency)
	var wg sync.WaitGroup
	var processed atomic.Int64

	for _, sd := range showDirs {
		wg.Add(1)
		go func(showNameRaw, showPath string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			scanOneShow(ctx, pool, libraryID, showNameRaw, showPath, existingEps)

			p := processed.Add(1)
			tracker.UpdateScan(libraryID, p, &showNameRaw)
		}(sd[0], sd[1])
	}
	wg.Wait()
	return nil
}

func scanOneShow(
	ctx context.Context,
	pool *pgxpool.Pool,
	libraryID string,
	showNameRaw string,
	showPath string,
	existingEps map[string]bool,
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
		canonicalID := candidates[0].ID
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

// ============ Helpers ============

func getJSONInt64(m map[string]interface{}, key string) *int64 {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch n := v.(type) {
	case float64:
		i := int64(n)
		return &i
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return &i
		}
	}
	return nil
}

func nullableJSON(data []byte) interface{} {
	if data == nil {
		return nil
	}
	return string(data)
}

// NullableStr 为空字符串时返回 nil,保证对应列写入 NULL。
func NullableStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// ComputeMediaVersionQuality 组合 mediainfo(优先)与文件名 NameParser(兜底)推导 QualityTags,
// 并给出短标签(如 "4K HDR BluRay")。是所有 media_versions INSERT 路径的共用入口。
func ComputeMediaVersionQuality(fileName string, mi map[string]interface{}) (scraper.QualityTags, string) {
	q := scraper.MergeQualityTags(
		scraper.QualityFromMediainfo(mi),
		scraper.QualityFromParsed(scraper.Parse(fileName, scraper.ModeEpisode)),
	)
	return q, scraper.QualityLabel(q)
}

func ptrAndThen(p *string, f func(string) *string) *string {
	if p == nil {
		return nil
	}
	return f(*p)
}

func derefStr(p *string) interface{} {
	if p == nil {
		return nil
	}
	return *p
}

// fileMtimeOrNil returns the mtime of the file at path, or nil if stat fails.
// Used as the created_at timestamp for new items so FYMS's "latest" list
// mirrors Emby's DateCreated (= file mtime on disk).
func fileMtimeOrNil(path string) interface{} {
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	return info.ModTime().UTC()
}
