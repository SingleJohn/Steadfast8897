package services

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/models"
)

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
