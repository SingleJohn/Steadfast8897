package services

import (
	"context"
	"crypto/md5"
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
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/models"
)

const scanConcurrency = 10

var videoExtSet = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".wmv": true, ".flv": true,
	".webm": true, ".m4v": true, ".mov": true, ".ts": true, ".mpg": true,
	".mpeg": true, ".iso": true, ".bdmv": true, ".m2ts": true, ".vob": true,
	".rmvb": true, ".rm": true, ".3gp": true, ".ogv": true, ".strm": true,
}

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
	tags := []string{"title", "originaltitle", "plot", "tagline", "year", "rating", "tmdbid", "imdbid", "premiered"}
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

	return result
}

// ============ Apply NFO data to DB ============

func ApplyNfoData(ctx context.Context, pool *pgxpool.Pool, itemID string, nfo *NfoData) {
	setClauses := make([]string, 0, 10)
	args := make([]interface{}, 0, 10)
	argIdx := 1

	addClause := func(column, castSuffix string, value interface{}) {
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
		addClause("premiere_date", "::date", *nfo.Premiered)
	}
	if nfo.Year != nil {
		addClause("production_year", "", *nfo.Year)
	}
	if nfo.Title != nil {
		addClause("name", "", *nfo.Title)
		addClause("sort_name", "", strings.ToLower(*nfo.Title))
	}
	if nfo.Tagline != nil {
		addClause("tagline", "", *nfo.Tagline)
	}

	if len(setClauses) > 0 {
		setClauses = append(setClauses, "updated_at = NOW()")
		query := fmt.Sprintf("UPDATE items SET %s WHERE id = $%d::uuid",
			strings.Join(setClauses, ", "), argIdx)
		args = append(args, itemID)
		pool.Exec(ctx, query, args...)
	}

	if len(nfo.Genres) > 0 {
		pool.Exec(ctx, "DELETE FROM item_genres WHERE item_id = $1::uuid", itemID)
		for _, genre := range nfo.Genres {
			pool.Exec(ctx, "INSERT INTO genres (name) VALUES ($1) ON CONFLICT (name) DO NOTHING", genre)
			pool.Exec(ctx,
				"INSERT INTO item_genres (item_id, genre_id) SELECT $1::uuid, id FROM genres WHERE name = $2 ON CONFLICT DO NOTHING",
				itemID, genre)
		}
	}

	if len(nfo.Actors) > 0 || len(nfo.Directors) > 0 {
		pool.Exec(ctx, "DELETE FROM cast_members WHERE item_id = $1::uuid", itemID)
		for _, dir := range nfo.Directors {
			pool.Exec(ctx,
				"INSERT INTO cast_members (item_id, name, character, role, order_index) VALUES ($1::uuid, $2, '', 'Director', 0)",
				itemID, dir)
		}
		limit := len(nfo.Actors)
		if limit > 20 {
			limit = 20
		}
		for i := 0; i < limit; i++ {
			a := nfo.Actors[i]
			pool.Exec(ctx,
				"INSERT INTO cast_members (item_id, name, character, role, order_index, tmdb_id, image_url) VALUES ($1::uuid, $2, $3, 'Actor', $4, $5, $6)",
				itemID, a.Name, a.Role, int32(i), a.TmdbID, a.ImageURL)
		}
	}
}

// ============ Filename Parsing ============

type ParsedMovie struct {
	Name string
	Year *int32
}

var (
	movieRE1 = regexp.MustCompile(`^(.+?)\s*\((\d{4})\)`)
	movieRE2 = regexp.MustCompile(`^\[(.+?)\s+(\d{4})\]`)
	movieRE3 = regexp.MustCompile(`^(.+?)\s+(\d{4})(?:\s|$|\.)`)
)

func ParseMovieName(name string) ParsedMovie {
	if m := movieRE1.FindStringSubmatch(name); m != nil {
		year := parseYear(m[2])
		return ParsedMovie{Name: strings.TrimSpace(m[1]), Year: year}
	}
	if m := movieRE2.FindStringSubmatch(name); m != nil {
		year := parseYear(m[2])
		return ParsedMovie{Name: strings.TrimSpace(m[1]), Year: year}
	}
	if m := movieRE3.FindStringSubmatch(name); m != nil {
		year := parseYear(m[2])
		return ParsedMovie{Name: strings.TrimSpace(m[1]), Year: year}
	}
	return ParsedMovie{Name: name}
}

func parseYear(s string) *int32 {
	v, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return nil
	}
	i := int32(v)
	return &i
}

type ParsedEpisode struct {
	Season  int32
	Episode *int32
	Title   *string
}

var (
	epRE1 = regexp.MustCompile(`(?i)[Ss](\d+)[Ee](\d+)`)
	epRE2 = regexp.MustCompile(`(\d+)x(\d+)`)
	epRE3 = regexp.MustCompile(`(?i)[Ee](\d+)`)
)

func ParseEpisodeInfo(filename string) *ParsedEpisode {
	if m := epRE1.FindStringSubmatch(filename); m != nil {
		s := parseInt32(m[1], 1)
		e := parseInt32Ptr(m[2])
		return &ParsedEpisode{Season: s, Episode: e}
	}
	if m := epRE2.FindStringSubmatch(filename); m != nil {
		s := parseInt32(m[1], 1)
		e := parseInt32Ptr(m[2])
		return &ParsedEpisode{Season: s, Episode: e}
	}
	if m := epRE3.FindStringSubmatch(filename); m != nil {
		e := parseInt32Ptr(m[1])
		return &ParsedEpisode{Season: 1, Episode: e}
	}
	return nil
}

func parseInt32(s string, fallback int32) int32 {
	v, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return fallback
	}
	return int32(v)
}

func parseInt32Ptr(s string) *int32 {
	v, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return nil
	}
	i := int32(v)
	return &i
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
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
		if !IsVideoExt("." + ext) {
			continue
		}
		if loc := epRE1.FindStringIndex(name); loc != nil {
			before := name[:loc[0]]
			cleaned := strings.TrimSpace(strings.NewReplacer(".", " ", "-", " ").Replace(before))
			if cleaned != "" {
				return &cleaned
			}
		}
	}
	return nil
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

	// 立即创建进度条目，UI 可以立刻看到扫描已启动
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
	}()
}

func autoScrapeNewItems(ctx context.Context, pool *pgxpool.Pool, libraryID string) {
	var autoEnabled *string
	pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'auto_scrape_enabled'").Scan(&autoEnabled)
	if autoEnabled == nil || *autoEnabled != "true" {
		return
	}

	rows, err := pool.Query(ctx,
		"SELECT id::text, name FROM items WHERE library_id = $1::uuid AND type IN ('Movie', 'Series') "+
			"AND (overview IS NULL OR overview = '') ORDER BY created_at DESC LIMIT 50",
		libraryID)
	if err != nil {
		return
	}

	type newItem struct {
		id   string
		name string
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
		return
	}

	slog.Info("[AutoScrape] Scraping new items", "count", len(items), "library", libraryID)

	client := TmdbClientFromConfig(ctx, pool)
	if client == nil {
		slog.Warn("[AutoScrape] TMDB API key not configured, skipping")
		return
	}

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

	slog.Info("[AutoScrape] Done", "success", success, "failed", failed)
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

			pool.Exec(ctx,
				"INSERT INTO media_versions (item_id, name, file_path, container, is_primary, mediainfo, runtime_ticks, bitrate, size) "+
					"VALUES ($1, $2, $3, $4, TRUE, $5, $6, $7, $8) ON CONFLICT DO NOTHING",
				item.id, name, item.filePath, vfContainer, nullableJSON(miJSON), runtimeTicks, bitrate, size)
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
			return
		}

		poster := FindImageCached(dirCache, []string{"poster", "cover", "folder"})
		backdrop := FindImageCached(dirCache, []string{"fanart", "backdrop", "background"})
		mi := ReadMediainfoJSONCached(primaryPath, dirCache)
		sortName := strings.ToLower(parsed.Name)
		var runtimeTicks *int64
		if mi != nil {
			runtimeTicks = getJSONInt64(mi, "RunTimeTicks")
		}
		posterTag := ptrAndThen(poster, GenerateImageTag)
		backdropTag := ptrAndThen(backdrop, GenerateImageTag)

		var insertedID *uuid.UUID
		err := pool.QueryRow(ctx,
			"INSERT INTO items (library_id, type, name, sort_name, production_year, runtime_ticks, file_path, container, primary_image_path, primary_image_tag, backdrop_image_path, backdrop_image_tag) "+
				"VALUES ($1::uuid, 'Movie', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) "+
				"ON CONFLICT DO NOTHING RETURNING id",
			libraryID, parsed.Name, sortName, parsed.Year,
			runtimeTicks, primaryPath, ext,
			derefStr(poster), derefStr(posterTag),
			derefStr(backdrop), derefStr(backdropTag),
		).Scan(&insertedID)

		if err == nil && insertedID != nil {
			if nfoPath := FindNfoCached(dirCache); nfoPath != nil {
				if nfo := ParseNfo(*nfoPath); nfo != nil {
					ApplyNfoData(ctx, pool, insertedID.String(), nfo)
				}
			}
		}
	} else {
		ext := strings.ToLower(filepath.Ext(name))
		if !IsVideoExt(ext) {
			return
		}
		if existing[fullPath] {
			return
		}

		basename := strings.TrimSuffix(name, filepath.Ext(name))
		parsed := ParseMovieName(basename)
		parentDir := filepath.Dir(fullPath)
		parentCache := CacheDir(parentDir)
		mi := ReadMediainfoJSONCached(fullPath, parentCache)
		var runtimeTicks *int64
		if mi != nil {
			runtimeTicks = getJSONInt64(mi, "RunTimeTicks")
		}
		extStr := strings.TrimPrefix(ext, ".")

		pool.Exec(ctx,
			"INSERT INTO items (library_id, type, name, sort_name, production_year, runtime_ticks, file_path, container) "+
				"VALUES ($1::uuid, 'Movie', $2, $3, $4, $5, $6, $7) "+
				"ON CONFLICT DO NOTHING",
			libraryID, parsed.Name, strings.ToLower(parsed.Name),
			parsed.Year, runtimeTicks, fullPath, extStr)
	}
}

// ============ TV Show Scanning ============

var (
	seasonRE   = regexp.MustCompile(`(?i)[Ss](?:eason|taffel|aison|erie)?\s*(\d+)`)
	seasonCNRE = regexp.MustCompile(`第(\d+)季`)
)

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

	poster := FindImageCached(showCache, []string{"poster", "cover", "folder"})
	backdrop := FindImageCached(showCache, []string{"fanart", "backdrop", "background"})
	posterTag := ptrAndThen(poster, GenerateImageTag)
	backdropTag := ptrAndThen(backdrop, GenerateImageTag)

	var seriesID string
	var insertedID *uuid.UUID
	err := pool.QueryRow(ctx,
		"INSERT INTO items (library_id, type, name, sort_name, production_year, primary_image_path, primary_image_tag, backdrop_image_path, backdrop_image_tag) "+
			"VALUES ($1::uuid, 'Series', $2, $3, $4, $5, $6, $7, $8) "+
			"ON CONFLICT DO NOTHING RETURNING id",
		libraryID, finalShowName, strings.ToLower(finalShowName), parsed.Year,
		derefStr(poster), derefStr(posterTag),
		derefStr(backdrop), derefStr(backdropTag),
	).Scan(&insertedID)

	if err == nil && insertedID != nil {
		seriesID = insertedID.String()
	} else {
		var existingID uuid.UUID
		err = pool.QueryRow(ctx,
			"SELECT id FROM items WHERE library_id = $1::uuid AND type = 'Series' AND name = $2 LIMIT 1",
			libraryID, finalShowName).Scan(&existingID)
		if err != nil {
			return
		}
		seriesID = existingID.String()
	}

	if nfoData != nil {
		ApplyNfoData(ctx, pool, seriesID, nfoData)
	}

	entries, err := os.ReadDir(showPath)
	if err != nil {
		return
	}
	for _, se := range entries {
		if !se.IsDir() {
			continue
		}
		dirName := se.Name()
		seasonNum := extractSeasonNumber(dirName)
		if seasonNum < 0 {
			continue
		}

		seasonPath := filepath.Join(showPath, dirName)
		seasonCache := CacheDir(seasonPath)
		seasonPoster := FindImageCached(seasonCache, []string{"poster", "cover", "folder"})
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
			seasonID = existingSeasonID.String()
		}

		// Group episodes by episode number
		type epFile struct {
			name string
			path string
			ext  string
		}
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

			var itemID uuid.UUID
			if existingEps[primary.path] {
				err := pool.QueryRow(ctx, "SELECT id FROM items WHERE file_path = $1 LIMIT 1", primary.path).Scan(&itemID)
				if err != nil {
					continue
				}
			} else {
				epTitle := strings.TrimSuffix(filepath.Base(primary.name), filepath.Ext(primary.name))
				if epTitle == "" {
					epTitle = "Episode"
				}
				mi := ReadMediainfoJSONCached(primary.path, seasonCache)
				var runtimeTicks *int64
				if mi != nil {
					runtimeTicks = getJSONInt64(mi, "RunTimeTicks")
				}

				var insertedEpID *uuid.UUID
				err := pool.QueryRow(ctx,
					"INSERT INTO items (library_id, parent_id, type, name, sort_name, index_number, parent_index_number, runtime_ticks, file_path, container, series_id, series_name, season_id) "+
						"VALUES ($1::uuid, $2::uuid, 'Episode', $3, $4, $5, $6, $7, $8, $9, $10::uuid, $11, $12::uuid) "+
						"ON CONFLICT DO NOTHING RETURNING id",
					libraryID, seasonID, epTitle,
					fmt.Sprintf("episode %04d", epNum),
					epNum, seasonNum, runtimeTicks,
					primary.path, primary.ext,
					seriesID, finalShowName, seasonID,
				).Scan(&insertedEpID)

				if err != nil || insertedEpID == nil {
					continue
				}
				itemID = *insertedEpID

				nfoStem := strings.TrimSuffix(primary.name, filepath.Ext(primary.name))
				epNfoName := nfoStem + ".nfo"
				for _, entry := range seasonCache {
					if entry[0] == epNfoName {
						if nfo := ParseNfo(entry[1]); nfo != nil {
							ApplyNfoData(ctx, pool, itemID.String(), nfo)
						}
						break
					}
				}
			}

			// Create media_versions for all files of this episode
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

				pool.Exec(ctx,
					"INSERT INTO media_versions (item_id, name, file_path, container, is_primary, mediainfo, runtime_ticks, bitrate, size) "+
						"VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) ON CONFLICT DO NOTHING",
					itemID, verName, f.path, container, isPrimary,
					nullableJSON(miJSON), runtimeTicks, bitrate, size)
			}
		}

		pool.Exec(ctx, "UPDATE items SET updated_at = NOW() WHERE id = $1::uuid AND type = 'Series'", seriesID)
	}
}

func extractSeasonNumber(dirName string) int32 {
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
