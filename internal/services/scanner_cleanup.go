package services

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
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
	type pruneTarget struct {
		itemType   string
		isDirEvent bool
	}
	targets := []pruneTarget{{itemType: "Movie"}}
	switch collectionType {
	case libraryTypeTVShows:
		targets = []pruneTarget{
			{itemType: "Series", isDirEvent: true},
			{itemType: "Season", isDirEvent: true},
			{itemType: "Episode"},
		}
	case libraryTypeMixed:
		targets = []pruneTarget{
			{itemType: "Folder", isDirEvent: true},
			{itemType: "Movie"},
			{itemType: "Series", isDirEvent: true},
			{itemType: "Episode"},
		}
	}

	var count int
	repo := repository.NewScanIngestRepository(pool)
	for _, target := range targets {
		candidates, err := repo.ListPruneCandidatePaths(ctx, libraryID, target.itemType)
		if err != nil {
			slog.Warn("[Prune] query failed", "library", libraryID, "type", target.itemType, "error", err)
			continue
		}

		for _, c := range candidates {
			cleaned := filepath.Clean(c.FilePath)
			if !pathInTrustedRoots(cleaned, trustedRoots) {
				continue
			}
			if _, ok := seenPaths[cleaned]; ok {
				continue
			}
			ingest.Submit(IngestEvent{
				Kind: EventDelete, Path: cleaned, IsDir: target.isDirEvent,
				Source: "scan", Tag: libraryID,
				LibraryID: libraryID, CollectionType: collectionType, DetectedAt: time.Now(),
			})
			count++
		}
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

// backfillCatalogNumbers 给 catalog_number 为空的 Movie/Series 用名称/文件名兜底提取番号。
// NFO <num> 已在入库时写入,此处只填空值,不覆盖。
func backfillCatalogNumbers(ctx context.Context, pool *pgxpool.Pool) {
	repo := repository.NewScanIngestRepository(pool)
	pending, err := repo.ListCatalogNumberBackfillCandidates(ctx)
	if err != nil {
		return
	}

	updated := 0
	for _, r := range pending {
		num := ExtractCatalogNumber(r.Name)
		if num == "" && r.FilePath != "" {
			num = ExtractCatalogNumber(filepath.Base(r.FilePath))
		}
		if num == "" {
			continue
		}
		if n, err := repo.FillCatalogNumberIfEmpty(ctx, r.ID, num); err == nil && n > 0 {
			updated++
		}
	}
	if updated > 0 {
		slog.Info("[Scan] Backfilled catalog numbers", "count", updated)
	}
}

func backfillMediaVersions(ctx context.Context, pool *pgxpool.Pool) {
	items, err := repository.NewScanIngestRepository(pool).ListMediaVersionBackfillCandidates(ctx)
	if err != nil {
		return
	}

	if len(items) == 0 {
		return
	}
	slog.Info("[Backfill] Creating media_versions", "count", len(items))

	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup
	var count atomic.Int64

	for _, item := range items {
		wg.Add(1)
		go func(item repository.MediaVersionBackfillCandidate) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(item.FilePath), "."))
			var vfContainer string
			if ext == "strm" {
				if rp := ResolveStrmPath(item.FilePath); rp != nil {
					vfContainer = strings.TrimPrefix(filepath.Ext(*rp), ".")
				}
				if vfContainer == "" {
					vfContainer = item.Container
				}
			} else if item.Container != "" {
				vfContainer = item.Container
			} else {
				vfContainer = ext
			}

			name := strings.TrimSuffix(filepath.Base(item.FilePath), filepath.Ext(item.FilePath))
			if name == "" {
				name = "Unknown"
			}
			mi := ReadMediainfoJSON(item.FilePath)

			var runtimeTicks, bitrate, size *int64
			if mi != nil {
				runtimeTicks = getJSONInt64(mi, "RunTimeTicks")
				bitrate = getJSONInt64(mi, "Bitrate")
				size = getJSONInt64(mi, "Size")
			}

			q, qLabel := ComputeMediaVersionQuality(filepath.Base(item.FilePath), mi)
			_, _ = repository.NewPlaybackRepository(pool).UpsertMediaVersion(ctx, repository.MediaVersionUpsert{
				ItemID:       item.ID.String(),
				Name:         name,
				FilePath:     item.FilePath,
				Container:    vfContainer,
				IsPrimary:    true,
				MediaInfo:    mi,
				RuntimeTicks: runtimeTicks,
				Bitrate:      bitrate,
				Size:         size,
				Resolution:   stringPtrIfNotEmpty(q.Resolution),
				HDRFormat:    stringPtrIfNotEmpty(q.HDRFormat),
				VideoCodec:   stringPtrIfNotEmpty(q.VideoCodec),
				AudioCodec:   stringPtrIfNotEmpty(q.AudioCodec),
				Source:       stringPtrIfNotEmpty(q.Source),
				QualityLabel: stringPtrIfNotEmpty(qLabel),
			})
			count.Add(1)
		}(item)
	}
	wg.Wait()
	slog.Info("[Backfill] media_versions created", "count", count.Load())
}
