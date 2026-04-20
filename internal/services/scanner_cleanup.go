package services

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ============ Pruning (Phase 3 差集) ============

// pruneMissingPaths 对比 DB 里本 library 的 items.file_path 和本次扫到的 seenPaths,
// 对每个"DB 里有但本次没见"的 item 产一条 Delete IngestEvent(由 ingest worker 处理级联)。
//
// 误删保护:只对 trustedRoots 下的 items 做差集——os.Stat 失败的 library.path
// 下属 items 不动,防止挂断导致整库记录被误删。
func pruneMissingPaths(
	ctx context.Context,
	pool *pgxpool.Pool,
	ingest *IngestWorker,
	libraryID string,
	collectionType string,
	trustedRoots []string,
	seenPaths map[string]struct{},
) int {
	itemType := "Movie"
	isDirEvent := false
	if collectionType == "tvshows" {
		itemType = "Series"
		isDirEvent = true
	}

	rows, err := pool.Query(ctx,
		`SELECT id::text, file_path FROM items
		  WHERE library_id = $1::uuid AND type = $2 AND file_path IS NOT NULL`,
		libraryID, itemType)
	if err != nil {
		slog.Warn("[Prune] query failed", "library", libraryID, "error", err)
		return 0
	}
	type row struct{ id, fp string }
	var candidates []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.id, &r.fp) == nil {
			candidates = append(candidates, r)
		}
	}
	rows.Close()

	var count int
	for _, c := range candidates {
		cleaned := filepath.Clean(c.fp)
		if !pathInTrustedRoots(cleaned, trustedRoots) {
			continue
		}
		if _, ok := seenPaths[cleaned]; ok {
			continue
		}
		ingest.Submit(IngestEvent{
			Kind: EventDelete, Path: cleaned, IsDir: isDirEvent,
			Source: "scan", Tag: libraryID, DetectedAt: time.Now(),
		})
		count++
	}
	return count
}

// pathInTrustedRoots 判断 cleaned 路径是否落在任一 trustedRoot 之下。
func pathInTrustedRoots(cleaned string, trustedRoots []string) bool {
	for _, tr := range trustedRoots {
		rel, err := filepath.Rel(tr, cleaned)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return true
	}
	return false
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
