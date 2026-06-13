package services

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
	for _, prefix := range prefixes {
		if p := findImageByStemCached(cache, prefix); p != nil {
			return p
		}
	}
	for _, entry := range cache {
		name, path := entry[0], entry[1]
		ext := strings.TrimPrefix(filepath.Ext(name), ".")
		if !isSupportedImageExt(ext) {
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

func FindMovieNfoCached(cache DirCache, videoBasename string, allowGeneric bool) *string {
	stem := movieSidecarStem(videoBasename)
	if stem != "" {
		want := stem + ".nfo"
		for _, entry := range cache {
			if entry[0] == want {
				return &entry[1]
			}
		}
	}
	if allowGeneric {
		return FindNfoCached(cache)
	}
	return nil
}

func FindMovieImageCached(cache DirCache, videoBasename string, prefixes []string, allowGeneric bool) *string {
	stem := movieSidecarStem(videoBasename)
	if stem != "" {
		if hasImagePrefix(prefixes, "poster") {
			if p := findImageByStemCached(cache, stem); p != nil {
				return p
			}
		}
		for _, prefix := range prefixes {
			for _, sep := range []string{"-", ".", "_"} {
				if p := findImageByStemCached(cache, stem+sep+prefix); p != nil {
					return p
				}
			}
		}
	}
	if allowGeneric {
		return FindImageCached(cache, prefixes)
	}
	return nil
}

func findImageByStemCached(cache DirCache, wantStem string) *string {
	wantStem = strings.ToLower(strings.TrimSpace(wantStem))
	if wantStem == "" {
		return nil
	}
	for _, entry := range cache {
		name, path := entry[0], entry[1]
		ext := strings.TrimPrefix(filepath.Ext(name), ".")
		if !isSupportedImageExt(ext) {
			continue
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		if stem == wantStem {
			return &path
		}
	}
	return nil
}

func movieSidecarStem(videoBasename string) string {
	videoBasename = strings.ToLower(strings.TrimSpace(filepath.Base(videoBasename)))
	if videoBasename == "" {
		return ""
	}
	return strings.TrimSuffix(videoBasename, filepath.Ext(videoBasename))
}

func hasImagePrefix(prefixes []string, want string) bool {
	for _, prefix := range prefixes {
		if prefix == want {
			return true
		}
	}
	return false
}

func allowGenericMovieSidecars(cache DirCache) bool {
	return countVideoFilesInCache(cache) <= 1
}

func countVideoFilesInCache(cache DirCache) int {
	var count int
	for _, entry := range cache {
		if IsVideoExt(filepath.Ext(entry[0])) {
			count++
		}
	}
	return count
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
	candidates := []string{
		stem + "-thumb",
		stem + ".thumb",
		stem,
	}
	for _, want := range candidates {
		for _, entry := range cache {
			name, path := entry[0], entry[1]
			ext := strings.TrimPrefix(filepath.Ext(name), ".")
			if !isSupportedImageExt(ext) {
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

// FindExtraFanart 返回 <dir>/extrafanart/ 下所有受支持的图片绝对路径,按文件名内嵌数字
// 自然排序(fanart1 < fanart2 < ... < fanart10)。这些图会作为额外 Backdrop(idx 1..N)入库,
// 对齐 Emby:extrafanart = 多张 Backdrop,客户端在 BackdropImageTags 数组里取。
func FindExtraFanart(dir string) []string {
	extraDir := filepath.Join(dir, "extrafanart")
	entries, err := os.ReadDir(extraDir)
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "._") || strings.HasPrefix(name, ".") {
			continue
		}
		if !isSupportedImageExt(filepath.Ext(name)) {
			continue
		}
		paths = append(paths, filepath.Join(extraDir, name))
	}
	sort.Slice(paths, func(i, j int) bool {
		ni, nj := trailingNumber(paths[i]), trailingNumber(paths[j])
		if ni != nj {
			return ni < nj
		}
		return paths[i] < paths[j]
	})
	return paths
}

// FindLocalTrailer 返回电影目录下的本地预告片绝对路径,无则返回 ""。
// 约定(对齐 Emby):优先 <dir>/trailers/ 下第一个视频;其次同目录 <basename>-trailer.<ext>。
func FindLocalTrailer(dir string) string {
	// 1) trailers/ 子目录
	trailerDir := filepath.Join(dir, "trailers")
	if entries, err := os.ReadDir(trailerDir); err == nil {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if IsVideoExt(filepath.Ext(e.Name())) {
				names = append(names, e.Name())
			}
		}
		if len(names) > 0 {
			sort.Strings(names)
			return filepath.Join(trailerDir, names[0])
		}
	}
	// 2) 同目录 <stem>-trailer.<ext>
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
			if strings.HasSuffix(stem, "-trailer") && IsVideoExt(filepath.Ext(name)) {
				return filepath.Join(dir, name)
			}
		}
	}
	return ""
}

// setLocalTrailer 把本地预告片路径写入 items.local_trailer_path(空路径则置空,幂等)。
func setLocalTrailer(ctx context.Context, pool *pgxpool.Pool, itemID uuid.UUID, trailerPath string) {
	var val interface{}
	if trailerPath != "" {
		val = trailerPath
	}
	if _, err := pool.Exec(ctx,
		"UPDATE items SET local_trailer_path = $1 WHERE id = $2::uuid", val, itemID); err != nil {
		slog.Warn("[Scan] Failed to set local trailer", "itemId", itemID, "error", err)
	}
}

// trailingNumber 提取文件茎里末尾的数字(fanart10.jpg -> 10),无数字返回 0。
func trailingNumber(path string) int {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	i := len(stem)
	for i > 0 && stem[i-1] >= '0' && stem[i-1] <= '9' {
		i--
	}
	if i == len(stem) {
		return 0
	}
	n, _ := strconv.Atoi(stem[i:])
	return n
}

// syncItemExtraBackdrops 把 item 的额外 Backdrop(extrafanart)全量重写进 item_images。
// 幂等:每次先删后插,重复扫不会放大。paths 为空时仅清理旧记录。
func syncItemExtraBackdrops(ctx context.Context, pool *pgxpool.Pool, itemID uuid.UUID, paths []string) {
	if _, err := pool.Exec(ctx,
		"DELETE FROM item_images WHERE item_id = $1::uuid AND image_type = 'Backdrop'", itemID); err != nil {
		slog.Warn("[Scan] Failed to clear extra backdrops", "itemId", itemID, "error", err)
		return
	}
	for i, p := range paths {
		tag := GenerateImageTag(p)
		if tag == nil {
			continue
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO item_images (item_id, image_type, idx, path, tag)
			 VALUES ($1::uuid, 'Backdrop', $2, $3, $4)
			 ON CONFLICT (item_id, image_type, idx) DO UPDATE SET path = EXCLUDED.path, tag = EXCLUDED.tag`,
			itemID, i+1, p, *tag); err != nil {
			slog.Warn("[Scan] Failed to insert extra backdrop", "itemId", itemID, "path", p, "error", err)
		}
	}
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

func isSupportedImageExt(ext string) bool {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "jpg", "jpeg", "png", "webp":
		return true
	default:
		return false
	}
}

func normalizeImageComparePath(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return ""
	}
	if os.PathSeparator == '\\' {
		return strings.ToLower(path)
	}
	return path
}

func isManagedNamedImagePath(currentPath string, dir string, prefixes []string) bool {
	currentPath = strings.TrimSpace(currentPath)
	dir = strings.TrimSpace(dir)
	if currentPath == "" || dir == "" {
		return false
	}
	if normalizeImageComparePath(filepath.Dir(currentPath)) != normalizeImageComparePath(dir) {
		return false
	}
	base := strings.ToLower(filepath.Base(currentPath))
	if !isSupportedImageExt(filepath.Ext(base)) {
		return false
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	for _, prefix := range prefixes {
		if strings.HasPrefix(stem, prefix) || strings.HasSuffix(stem, prefix) {
			return true
		}
	}
	return false
}

func isManagedEpisodeThumbPath(currentPath string, dir string, videoBasename string) bool {
	currentPath = strings.TrimSpace(currentPath)
	dir = strings.TrimSpace(dir)
	videoBasename = strings.TrimSpace(videoBasename)
	if currentPath == "" || dir == "" || videoBasename == "" {
		return false
	}
	if normalizeImageComparePath(filepath.Dir(currentPath)) != normalizeImageComparePath(dir) {
		return false
	}
	base := strings.ToLower(filepath.Base(currentPath))
	if !isSupportedImageExt(filepath.Ext(base)) {
		return false
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	videoStem := strings.ToLower(strings.TrimSuffix(videoBasename, filepath.Ext(videoBasename)))
	switch stem {
	case videoStem, videoStem + "-thumb", videoStem + ".thumb":
		return true
	default:
		return false
	}
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
	if err := syncItemArtworkWithClear(ctx, pool, itemID, poster, posterTag, false, backdrop, backdropTag, false); err != nil {
		slog.Warn("[Scan] Failed to sync artwork", "itemId", itemID, "error", err)
	}
}

func syncItemArtworkWithClear(
	ctx context.Context,
	pool *pgxpool.Pool,
	itemID uuid.UUID,
	poster *string,
	posterTag *string,
	clearPoster bool,
	backdrop *string,
	backdropTag *string,
	clearBackdrop bool,
) error {
	if (poster == nil || *poster == "") &&
		(backdrop == nil || *backdrop == "") &&
		!clearPoster && !clearBackdrop {
		return nil
	}

	_, err := pool.Exec(ctx,
		`UPDATE items
		 SET primary_image_path = CASE
		                            WHEN $4 THEN NULL
		                            WHEN NULLIF($2, '') IS NOT NULL THEN $2
		                            ELSE primary_image_path
		                          END,
		     primary_image_tag = CASE
		                           WHEN $4 THEN NULL
		                           WHEN NULLIF($3, '') IS NOT NULL THEN $3
		                           ELSE primary_image_tag
		                         END,
		     backdrop_image_path = CASE
		                             WHEN $7 THEN NULL
		                             WHEN NULLIF($5, '') IS NOT NULL THEN $5
		                             ELSE backdrop_image_path
		                           END,
		     backdrop_image_tag = CASE
		                            WHEN $7 THEN NULL
		                            WHEN NULLIF($6, '') IS NOT NULL THEN $6
		                            ELSE backdrop_image_tag
		                          END,
		     updated_at = NOW()
		 WHERE id = $1::uuid`,
		itemID,
		derefStr(poster),
		derefStr(posterTag),
		clearPoster,
		derefStr(backdrop),
		derefStr(backdropTag),
		clearBackdrop,
	)
	return err
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
