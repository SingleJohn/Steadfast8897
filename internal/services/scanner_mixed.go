package services

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	libraryTypeMovies  = "movies"
	libraryTypeTVShows = "tvshows"
	libraryTypeMixed   = "mixed"
)

type showEntry struct {
	name        string
	fullPath    string
	videoPaths  []string
	seasonPaths []string
}

type mixedScanEntries struct {
	folders []folderEntry
	movies  []movieEntry
	shows   []showEntry
}

type folderEntry struct {
	fullPath string
}

var mixedExplicitEpisodeRE = regexp.MustCompile(`(?i)(s\d{1,2}\s*e\d{1,3}|第\s*\d+\s*[集话]|(?:^|[\s._-])ep?\s*\d{1,3}(?:[\s._-]|$))`)

func collectShowEntries(dir string, results *[]showEntry) {
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
			*results = append(*results, showEntry{
				name:        name,
				fullPath:    fullPath,
				videoPaths:  collectShowVideoPaths(fullPath),
				seasonPaths: collectShowSeasonPaths(fullPath),
			})
		} else {
			collectShowEntries(fullPath, results)
		}
	}
}

func collectShowDirs(dir string, results *[][2]string) {
	var shows []showEntry
	collectShowEntries(dir, &shows)
	for _, show := range shows {
		*results = append(*results, [2]string{show.name, show.fullPath})
	}
}

func collectShowVideoPaths(showPath string) []string {
	var paths []string
	collectShowVideoPathsRecursive(showPath, &paths)
	return paths
}

func collectShowSeasonPaths(showPath string) []string {
	scans := collectTVSeasonScans(showPath)
	paths := make([]string, 0, len(scans))
	cleanShowPath := filepath.Clean(showPath)
	for _, scan := range scans {
		cleanSeasonPath := filepath.Clean(scan.path)
		if cleanSeasonPath != "" && cleanSeasonPath != cleanShowPath {
			paths = append(paths, scan.path)
		}
	}
	return paths
}

func collectShowVideoPathsRecursive(dir string, paths *[]string) {
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
			collectShowVideoPathsRecursive(fullPath, paths)
			continue
		}
		if IsVideoExt(filepath.Ext(name)) {
			*paths = append(*paths, fullPath)
		}
	}
}

func collectMixedEntries(dir string, results *mixedScanEntries) {
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
			if IsExtrasDirName(name) || looksLikeSeasonDir(name) {
				continue
			}
			if mixedLooksLikeShowDir(fullPath) {
				results.shows = append(results.shows, showEntry{
					name:        name,
					fullPath:    fullPath,
					videoPaths:  collectShowVideoPaths(fullPath),
					seasonPaths: collectShowSeasonPaths(fullPath),
				})
				continue
			}
			if movie, ok := mixedMovieEntryForDir(name, fullPath); ok {
				results.movies = append(results.movies, movie)
				continue
			}
			results.folders = append(results.folders, folderEntry{fullPath: fullPath})
			collectMixedEntries(fullPath, results)
			continue
		}
		if IsVideoExt(filepath.Ext(name)) && !IsInExtrasFolder(fullPath) {
			results.movies = append(results.movies, movieEntry{name: name, fullPath: fullPath, isDir: false})
		}
	}
}

func mixedLooksLikeShowDir(path string) bool {
	if looksLikeSeasonDir(filepath.Base(path)) || isBdmvMovieDir(path) {
		return false
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	directVideos := 0
	episodeLike := 0
	for _, entry := range entries {
		name := entry.Name()
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, ".") || strings.HasPrefix(lower, "@") {
			continue
		}
		if entry.IsDir() {
			if looksLikeSeasonDir(name) {
				return true
			}
			continue
		}
		if lower == "tvshow.nfo" {
			return true
		}
		if lower == "movie.nfo" {
			return false
		}
		if !IsVideoExt(filepath.Ext(name)) {
			continue
		}
		directVideos++
		if mixedHasExplicitEpisodeToken(name) {
			episodeLike++
		}
	}
	return directVideos > 0 && episodeLike >= 2 && directVideos == episodeLike
}

func mixedMovieEntryForDir(name, fullPath string) (movieEntry, bool) {
	if isBdmvMovieDir(fullPath) {
		vids := collectBdmvVideos(fullPath)
		paths := make([]string, 0, len(vids))
		for _, v := range vids {
			paths = append(paths, v[1])
		}
		return movieEntry{name: name, fullPath: fullPath, isDir: true, videoPaths: paths}, true
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return movieEntry{}, false
	}
	hasMovieNfo := false
	var videoPaths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		entryName := entry.Name()
		lower := strings.ToLower(entryName)
		if lower == "movie.nfo" {
			hasMovieNfo = true
			continue
		}
		if IsVideoExt(filepath.Ext(entryName)) {
			videoPaths = append(videoPaths, filepath.Join(fullPath, entryName))
		}
	}
	if len(videoPaths) == 0 {
		return movieEntry{}, false
	}
	if hasMovieNfo {
		return movieEntry{name: name, fullPath: fullPath, isDir: true, videoPaths: videoPaths}, true
	}
	if len(videoPaths) >= 2 && dirVideosAreDistinctMovies(videoPaths) {
		return movieEntry{}, false
	}
	for _, p := range videoPaths {
		if mixedHasExplicitEpisodeToken(filepath.Base(p)) {
			return movieEntry{}, false
		}
	}
	return movieEntry{name: name, fullPath: fullPath, isDir: true, videoPaths: videoPaths}, true
}

func mixedHasExplicitEpisodeToken(name string) bool {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	return mixedExplicitEpisodeRE.MatchString(stem)
}

func ensureMixedParentFolder(ctx context.Context, pool *pgxpool.Pool, libraryID string, roots []string, mediaPath string) *string {
	parentPath := filepath.Dir(filepath.Clean(mediaPath))
	return ensureMixedFolderTree(ctx, pool, libraryID, roots, parentPath)
}

func ensureMixedFolderTree(ctx context.Context, pool *pgxpool.Pool, libraryID string, roots []string, folderPath string) *string {
	cleaned := filepath.Clean(folderPath)
	for _, root := range roots {
		root = filepath.Clean(root)
		rel, err := filepath.Rel(root, cleaned)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		parts := splitPathParts(rel)
		cur := root
		var parentID *string
		for _, part := range parts {
			cur = filepath.Join(cur, part)
			id := ensureMixedFolder(ctx, pool, libraryID, parentID, cur)
			if id == "" {
				return parentID
			}
			parentID = &id
		}
		return parentID
	}
	return nil
}

func splitPathParts(rel string) []string {
	raw := strings.Split(rel, string(filepath.Separator))
	parts := make([]string, 0, len(raw))
	for _, p := range raw {
		if p != "" && p != "." {
			parts = append(parts, p)
		}
	}
	return parts
}

func ensureMixedFolder(ctx context.Context, pool *pgxpool.Pool, libraryID string, parentID *string, folderPath string) string {
	name := filepath.Base(folderPath)
	sortName := strings.ToLower(name)
	parentArg := nullableParentID(parentID)
	var id string
	err := pool.QueryRow(ctx,
		`INSERT INTO items (library_id, parent_id, type, name, sort_name, file_path, created_at)
		 VALUES ($1::uuid, $2::uuid, 'Folder', $3, $4, $5, COALESCE($6, NOW()))
		 ON CONFLICT DO NOTHING
		 RETURNING id::text`,
		libraryID, parentArg, name, sortName, filepath.Clean(folderPath), fileMtimeOrNil(folderPath),
	).Scan(&id)
	if err == nil {
		return id
	}
	err = pool.QueryRow(ctx,
		`SELECT id::text FROM items
		  WHERE library_id = $1::uuid AND type = 'Folder' AND file_path = $2
		  LIMIT 1`,
		libraryID, filepath.Clean(folderPath),
	).Scan(&id)
	if err != nil {
		return ""
	}
	_, _ = pool.Exec(ctx,
		`UPDATE items
		    SET parent_id = $1::uuid, name = $2, sort_name = $3, updated_at = NOW()
		  WHERE id = $4::uuid`,
		parentArg, name, sortName, id,
	)
	return id
}

func setMixedItemParent(ctx context.Context, pool *pgxpool.Pool, libraryID, itemType, filePath string, parentID *string) {
	_, _ = pool.Exec(ctx,
		`UPDATE items
		    SET parent_id = $1::uuid, updated_at = NOW()
		  WHERE library_id = $2::uuid AND type = $3 AND file_path = $4`,
		nullableParentID(parentID), libraryID, itemType, filepath.Clean(filePath),
	)
}

func nullableParentID(parentID *string) interface{} {
	if parentID == nil || strings.TrimSpace(*parentID) == "" {
		return nil
	}
	return *parentID
}

func scanMixedMovie(ctx context.Context, pool *pgxpool.Pool, libraryID string, roots []string, movie movieEntry) {
	parentID := ensureMixedParentFolder(ctx, pool, libraryID, roots, movie.fullPath)
	scanOneMovie(ctx, pool, libraryID, movie.name, movie.fullPath, movie.isDir, map[string]bool{})
	keyPath := filepath.Clean(movie.fullPath)
	if movie.isDir && len(movie.videoPaths) > 0 {
		keyPath = filepath.Clean(movie.videoPaths[0])
	}
	setMixedItemParent(ctx, pool, libraryID, "Movie", keyPath, parentID)
}

func scanMixedShow(ctx context.Context, pool *pgxpool.Pool, libraryID string, roots []string, show showEntry) {
	parentID := ensureMixedParentFolder(ctx, pool, libraryID, roots, show.fullPath)
	scanOneShow(ctx, pool, libraryID, show.name, show.fullPath)
	setMixedItemParent(ctx, pool, libraryID, "Series", show.fullPath, parentID)
}
