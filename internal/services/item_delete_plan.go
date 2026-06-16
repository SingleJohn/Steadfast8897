package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ItemDeletePlan struct {
	ItemID       string
	ItemName     string
	ItemType     string
	FilePaths    []string
	DirFilePaths []string
	DirPaths     []string
	LibraryRoots []string
}

type ItemDeleteResult struct {
	DeletedFiles int
	MissingFiles int
	DeletedDirs  int
	SkippedPaths int
}

type deleteItemPathRow struct {
	ID               string
	Type             string
	Name             string
	FilePath         *string
	PrimaryImagePath *string
	BackdropPath     *string
	LocalTrailerPath *string
}

func BuildItemDeletePlan(ctx context.Context, pool *pgxpool.Pool, itemID string) (*ItemDeletePlan, error) {
	items, roots, err := loadItemDeleteRows(ctx, pool, itemID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}

	plan := &ItemDeletePlan{
		ItemID:       items[0].ID,
		ItemName:     items[0].Name,
		ItemType:     items[0].Type,
		LibraryRoots: roots,
	}
	files := make(map[string]struct{})
	dirFiles := make(map[string]struct{})
	dirs := make(map[string]struct{})

	for _, item := range items {
		addLocalPath(files, item.FilePath)
		addLocalPath(files, item.PrimaryImagePath)
		addLocalPath(files, item.BackdropPath)
		addLocalPath(files, item.LocalTrailerPath)
		addDerivedSidecars(files, dirFiles, dirs, item, roots)
	}

	addDBPaths(ctx, pool, files, "SELECT file_path FROM media_versions WHERE item_id::text = ANY($1)", itemIDs(items))
	addDBPaths(ctx, pool, files, "SELECT file_path FROM external_subtitles WHERE item_id::text = ANY($1)", itemIDs(items))
	addDBPaths(ctx, pool, files, "SELECT path FROM item_images WHERE item_id::text = ANY($1)", itemIDs(items))

	for p := range files {
		delete(dirFiles, p)
	}
	plan.FilePaths = sortedPaths(files)
	plan.DirFilePaths = sortedPaths(dirFiles)
	plan.DirPaths = sortedPaths(dirs)
	sort.Slice(plan.DirPaths, func(i, j int) bool {
		return pathDepth(plan.DirPaths[i]) > pathDepth(plan.DirPaths[j])
	})
	return plan, nil
}

func ExecuteItemDeletePlan(plan *ItemDeletePlan) (ItemDeleteResult, error) {
	var result ItemDeleteResult
	if plan == nil {
		return result, nil
	}

	parentDirs := make(map[string]struct{})
	for _, p := range plan.FilePaths {
		clean, ok := safeDeletePath(p)
		if !ok {
			result.SkippedPaths++
			continue
		}
		info, err := os.Lstat(clean)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				result.MissingFiles++
				continue
			}
			return result, fmt.Errorf("stat delete target %s: %w", clean, err)
		}
		if info.IsDir() {
			result.SkippedPaths++
			continue
		}
		if err := os.Remove(clean); err != nil {
			return result, fmt.Errorf("delete file %s: %w", clean, err)
		}
		result.DeletedFiles++
		addParentDirs(parentDirs, clean, plan.LibraryRoots)
	}

	for _, p := range plan.DirFilePaths {
		clean, ok := safeDeletePath(p)
		if !ok {
			result.SkippedPaths++
			continue
		}
		info, err := os.Lstat(clean)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				result.MissingFiles++
				continue
			}
			return result, fmt.Errorf("stat delete target %s: %w", clean, err)
		}
		if info.IsDir() {
			continue
		}
		if err := os.Remove(clean); err != nil {
			return result, fmt.Errorf("delete file %s: %w", clean, err)
		}
		result.DeletedFiles++
		addParentDirs(parentDirs, clean, plan.LibraryRoots)
	}

	for _, d := range plan.DirPaths {
		clean, ok := safeDeletePath(d)
		if !ok {
			result.SkippedPaths++
			continue
		}
		deleted, err := removeEmptyDir(clean, plan.LibraryRoots)
		if err != nil {
			return result, err
		}
		if deleted {
			result.DeletedDirs++
			addParentDirs(parentDirs, clean, plan.LibraryRoots)
		}
	}

	for _, d := range sortedDirsDeepFirst(parentDirs) {
		deleted, err := removeEmptyDir(d, plan.LibraryRoots)
		if err != nil {
			return result, err
		}
		if deleted {
			result.DeletedDirs++
		}
	}

	return result, nil
}

func DeleteItemRecord(ctx context.Context, pool *pgxpool.Pool, itemID string) (bool, error) {
	ct, err := pool.Exec(ctx, "DELETE FROM items WHERE id = $1::uuid", itemID)
	if err != nil {
		return false, err
	}
	if ct.RowsAffected() > 0 {
		if err := CleanupEmptyParents(ctx, pool); err != nil {
			slog.Warn("[DeleteItem] cleanup empty parents failed", "itemId", itemID, "error", err)
		}
		return true, nil
	}
	return false, nil
}

func loadItemDeleteRows(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]deleteItemPathRow, []string, error) {
	rows, err := pool.Query(ctx, `
WITH RECURSIVE target AS (
	SELECT id, parent_id, type, name, file_path, primary_image_path, backdrop_image_path, local_trailer_path, library_id, 0 AS depth
	  FROM items
	 WHERE id = $1::uuid
	UNION ALL
	SELECT child.id, child.parent_id, child.type, child.name, child.file_path, child.primary_image_path, child.backdrop_image_path,
	       child.local_trailer_path, child.library_id, target.depth + 1
	  FROM items child
	  JOIN target ON child.parent_id = target.id
)
SELECT target.id::text, target.type, target.name, target.file_path,
       target.primary_image_path, target.backdrop_image_path, target.local_trailer_path, l.paths
  FROM target
  JOIN libraries l ON l.id = target.library_id
 ORDER BY target.depth ASC, target.type ASC, target.name ASC`, itemID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var out []deleteItemPathRow
	rootSet := make(map[string]struct{})
	for rows.Next() {
		var row deleteItemPathRow
		var roots []string
		if err := rows.Scan(&row.ID, &row.Type, &row.Name, &row.FilePath, &row.PrimaryImagePath, &row.BackdropPath, &row.LocalTrailerPath, &roots); err != nil {
			return nil, nil, err
		}
		out = append(out, row)
		for _, root := range roots {
			if clean, ok := safeDeletePath(root); ok {
				rootSet[clean] = struct{}{}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return out, sortedPaths(rootSet), nil
}

func addDBPaths(ctx context.Context, pool *pgxpool.Pool, files map[string]struct{}, query string, ids []string) {
	if len(ids) == 0 {
		return
	}
	rows, err := pool.Query(ctx, query, ids)
	if err != nil {
		slog.Warn("[DeleteItem] collect DB paths failed", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var p *string
		if rows.Scan(&p) == nil {
			addLocalPath(files, p)
		}
	}
}

func addDerivedSidecars(files map[string]struct{}, dirFiles map[string]struct{}, dirs map[string]struct{}, item deleteItemPathRow, roots []string) {
	if item.FilePath == nil {
		return
	}
	filePath, ok := safeDeletePath(*item.FilePath)
	if !ok {
		return
	}

	if isOwnedItemDir(item.Type, filePath, roots) {
		addDirTree(dirFiles, dirs, filePath)
		return
	}

	switch item.Type {
	case "Movie":
		addMovieSidecars(files, dirFiles, dirs, filePath)
	case "Series":
		addSeriesSidecars(files, dirFiles, dirs, filePath)
	case "Season":
		addSeasonSidecars(files, filePath)
	case "Episode":
		addEpisodeSidecars(files, filePath)
	}
}

func isOwnedItemDir(itemType, path string, roots []string) bool {
	switch itemType {
	case "Movie", "Series", "Season":
	default:
		return false
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	return !isLibraryRoot(path, roots)
}

func addMovieSidecars(files map[string]struct{}, dirFiles map[string]struct{}, dirs map[string]struct{}, path string) {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		cache := CacheDir(path)
		addMatchingImage(files, cache, posterImagePrefixes)
		addMatchingImage(files, cache, backdropImagePrefixes)
		addFirstNfo(files, cache)
		addDirTree(dirFiles, dirs, filepath.Join(path, "extrafanart"))
		for name := range extrasDirNames {
			addDirTree(dirFiles, dirs, filepath.Join(path, name))
		}
		addDirIfExists(dirs, path)
		return
	}

	dir := filepath.Dir(path)
	cache := CacheDir(dir)
	base := filepath.Base(path)
	allowGeneric := allowGenericMovieSidecars(cache)
	addLocalPath(files, FindMovieNfoCached(cache, base, allowGeneric))
	addLocalPath(files, FindMovieImageCached(cache, base, posterImagePrefixes, allowGeneric))
	addLocalPath(files, FindMovieImageCached(cache, base, backdropImagePrefixes, allowGeneric))
	addSiblingByStem(files, dir, base, []string{"mediainfo", "mediaInfo"}, []string{".json"})
	addSiblingByStem(files, dir, base, []string{"trailer"}, videoExtensions())
	addExternalSubtitles(files, path, cache)
}

func addSeriesSidecars(files map[string]struct{}, dirFiles map[string]struct{}, dirs map[string]struct{}, path string) {
	cache := CacheDir(path)
	addMatchingImage(files, cache, posterImagePrefixes)
	addMatchingImage(files, cache, backdropImagePrefixes)
	addFirstNfo(files, cache)
	addDirTree(dirFiles, dirs, filepath.Join(path, "extrafanart"))
	addDirIfExists(dirs, path)
}

func addSeasonSidecars(files map[string]struct{}, path string) {
	cache := CacheDir(path)
	addMatchingImage(files, cache, posterImagePrefixes)
}

func addEpisodeSidecars(files map[string]struct{}, path string) {
	dir := filepath.Dir(path)
	cache := CacheDir(dir)
	base := filepath.Base(path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	addLocalPath(files, FindEpisodeThumbCached(cache, base))
	addLocalPath(files, pathWithExistingExt(dir, stem, []string{".nfo"}))
	addSiblingByStem(files, dir, base, []string{"mediainfo", "mediaInfo"}, []string{".json"})
	addExternalSubtitles(files, path, cache)
}

func addExternalSubtitles(files map[string]struct{}, videoPath string, cache DirCache) {
	for _, sub := range findExternalSubtitlesCached(videoPath, cache) {
		addLocalStringPath(files, sub.Path)
	}
}

func addMatchingImage(files map[string]struct{}, cache DirCache, prefixes []string) {
	addLocalPath(files, FindImageCached(cache, prefixes))
}

func addFirstNfo(files map[string]struct{}, cache DirCache) {
	addLocalPath(files, FindNfoCached(cache))
}

func addSiblingByStem(files map[string]struct{}, dir, videoBase string, suffixes []string, exts []string) {
	stem := strings.TrimSuffix(videoBase, filepath.Ext(videoBase))
	for _, suffix := range suffixes {
		for _, ext := range exts {
			addLocalPath(files, pathWithExistingExt(dir, stem+"-"+suffix, []string{ext}))
			addLocalPath(files, pathWithExistingExt(dir, stem+"."+suffix, []string{ext}))
		}
	}
}

func pathWithExistingExt(dir, stem string, exts []string) *string {
	for _, ext := range exts {
		p := filepath.Join(dir, stem+ext)
		if _, err := os.Stat(p); err == nil {
			return &p
		}
	}
	return nil
}

func videoExtensions() []string {
	exts := make([]string, 0, len(videoExtSet))
	for ext := range videoExtSet {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	return exts
}

func addDirIfExists(dirs map[string]struct{}, p string) {
	clean, ok := safeDeletePath(p)
	if !ok {
		return
	}
	if info, err := os.Stat(clean); err == nil && info.IsDir() {
		dirs[clean] = struct{}{}
	}
}

func addDirTree(files map[string]struct{}, dirs map[string]struct{}, dir string) {
	clean, ok := safeDeletePath(dir)
	if !ok {
		return
	}
	info, err := os.Stat(clean)
	if err != nil || !info.IsDir() {
		return
	}
	_ = filepath.WalkDir(clean, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		pathClean, ok := safeDeletePath(path)
		if !ok {
			return nil
		}
		if d.IsDir() {
			dirs[pathClean] = struct{}{}
			return nil
		}
		files[pathClean] = struct{}{}
		return nil
	})
}

func addLocalPath(paths map[string]struct{}, p *string) {
	if p == nil {
		return
	}
	addLocalStringPath(paths, *p)
}

func addLocalStringPath(paths map[string]struct{}, p string) {
	clean, ok := safeDeletePath(p)
	if !ok {
		return
	}
	paths[clean] = struct{}{}
}

func safeDeletePath(p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", false
	}
	if strings.HasPrefix(p, "\\\\") || filepath.IsAbs(p) {
		clean := filepath.Clean(p)
		if clean == "." || clean == string(filepath.Separator) {
			return "", false
		}
		return clean, true
	}
	if u, err := url.Parse(p); err == nil && u.Scheme != "" && u.Scheme != "file" {
		return "", false
	}
	clean := filepath.Clean(p)
	if clean == "." || clean == string(filepath.Separator) {
		return "", false
	}
	return clean, true
}

func addParentDirs(dirs map[string]struct{}, p string, roots []string) {
	dir := filepath.Dir(p)
	if !filepath.IsAbs(p) && !strings.HasPrefix(p, "\\\\") {
		if dir != "" && dir != "." {
			dirs[dir] = struct{}{}
		}
		return
	}
	for dir != "" && dir != "." && !isLibraryRoot(dir, roots) {
		dirs[dir] = struct{}{}
		next := filepath.Dir(dir)
		if next == dir {
			return
		}
		dir = next
	}
}

func removeEmptyDir(dir string, roots []string) (bool, error) {
	if dir == "" || dir == "." || isLibraryRoot(dir, roots) {
		return false, nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat dir %s: %w", dir, err)
	}
	if !info.IsDir() {
		return false, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("read dir %s: %w", dir, err)
	}
	if len(entries) > 0 {
		return false, nil
	}
	if err := os.Remove(dir); err != nil {
		return false, fmt.Errorf("remove empty dir %s: %w", dir, err)
	}
	return true, nil
}

func isLibraryRoot(dir string, roots []string) bool {
	norm := normalizeImageComparePath(dir)
	for _, root := range roots {
		if norm == normalizeImageComparePath(root) {
			return true
		}
	}
	return false
}

func itemIDs(items []deleteItemPathRow) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func sortedPaths(paths map[string]struct{}) []string {
	out := make([]string, 0, len(paths))
	for p := range paths {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func sortedDirsDeepFirst(dirs map[string]struct{}) []string {
	out := sortedPaths(dirs)
	sort.Slice(out, func(i, j int) bool {
		return pathDepth(out[i]) > pathDepth(out[j])
	})
	return out
}

func pathDepth(path string) int {
	return strings.Count(filepath.Clean(path), string(filepath.Separator))
}
