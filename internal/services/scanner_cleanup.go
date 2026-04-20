package services

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
