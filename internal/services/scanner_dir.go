package services

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
