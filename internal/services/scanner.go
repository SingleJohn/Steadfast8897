package services

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/models"
)

// ============ Scan Libraries ============

// ScanAllLibraries 对所有 library 并发触发 ScanLibrary。
func ScanAllLibraries(ctx context.Context, pool *pgxpool.Pool, cache *CacheService, tracker *ScanProgressTracker, ingest *IngestWorker) {
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
			ScanLibrary(ctx, pool, cache, tracker, ingest, lib.ID.String(), lib.CollectionType, lib.Paths, lib.Name)
		}(lib)
	}
	wg.Wait()
}

// ScanLibrary(Phase 3 改造版):不再直接落库。
//
// 流程:
//  1. 对每条 library path 做 os.Stat,收集 trustedRoots(成功的)——挂断的跳过,防误删
//  2. 遍历 trustedRoots 产 Create IngestEvent,Tag=libraryID 参与 Barrier 计数
//  3. 进度轮询:通过 ingest.InflightCount(libraryID) 推算 Processed
//  4. Barrier 等所有 Create 处理完
//  5. 差集:DB 里 file_path 落在 trustedRoots 内但本次没扫到的 → 产 Delete IngestEvent
//  6. Barrier 等 Delete 处理完
//  7. CompleteScan + 触发 backfillMediaVersions + autoScrapeNewItems(都是入队式,不等待)
func ScanLibrary(
	ctx context.Context,
	pool *pgxpool.Pool,
	cache *CacheService,
	tracker *ScanProgressTracker,
	ingest *IngestWorker,
	libraryID, collectionType string,
	paths []string,
	libraryName string,
) {
	if tracker.IsScanning(libraryID) {
		slog.Warn("Library is already scanning", "library", libraryName)
		return
	}
	if ingest == nil {
		slog.Error("[Scan] IngestWorker required but not provided", "library", libraryName)
		return
	}

	slog.Info("[Scan] Starting scan", "library", libraryName, "type", collectionType)
	cache.Del(ctx, "views:all")
	tracker.StartScan(libraryID, libraryName, 0)

	go func() {
		// 1. 可信根:os.Stat 成功的 library.path,挂断路径跳过。差集只对可信根下的 items 做。
		trustedRoots := make([]string, 0, len(paths))
		for _, p := range paths {
			if p == "" {
				continue
			}
			if _, err := os.Stat(p); err == nil {
				trustedRoots = append(trustedRoots, filepath.Clean(p))
			} else {
				slog.Warn("[Scan] Skipping unreadable path", "path", p, "error", err)
			}
		}
		if len(trustedRoots) == 0 {
			slog.Error("[Scan] No trusted paths, abort", "library", libraryName)
			tracker.FailScan(libraryID, "all library paths unreadable")
			return
		}

		// 2. 遍历 FS 产事件
		seenPaths := make(map[string]struct{})
		var total int64
		switch collectionType {
		case "tvshows":
			var showDirs [][2]string
			for _, p := range trustedRoots {
				collectShowDirs(p, &showDirs)
			}
			total = int64(len(showDirs))
			tracker.UpdateTotal(libraryID, total)
			slog.Info("[Scan] Collected entries", "library", libraryName, "shows", total)
			for _, sd := range showDirs {
				showPath := filepath.Clean(sd[1])
				seenPaths[showPath] = struct{}{}
				ingest.Submit(IngestEvent{
					Kind: EventCreate, Path: showPath, IsDir: true,
					Source: "scan", Tag: libraryID, DetectedAt: time.Now(),
				})
			}
		default:
			var entries []movieEntry
			for _, p := range trustedRoots {
				collectMovieEntries(p, &entries)
			}
			total = int64(len(entries))
			tracker.UpdateTotal(libraryID, total)
			slog.Info("[Scan] Collected entries", "library", libraryName, "movies", total)
			for _, e := range entries {
				full := filepath.Clean(e.fullPath)
				seenPaths[full] = struct{}{}
				ingest.Submit(IngestEvent{
					Kind: EventCreate, Path: full, IsDir: e.isDir,
					Source: "scan", Tag: libraryID, DetectedAt: time.Now(),
				})
			}
		}

		// 3. 进度轮询:基于 inflight 推算 Processed
		progressStop := make(chan struct{})
		var progressWg sync.WaitGroup
		progressWg.Add(1)
		go func() {
			defer progressWg.Done()
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-progressStop:
					return
				case <-ticker.C:
					inflight := ingest.InflightCount(libraryID)
					processed := total - inflight
					if processed < 0 {
						processed = 0
					}
					tracker.UpdateScan(libraryID, processed, nil)
				}
			}
		}()

		// 4. Barrier 等 Create 处理完
		ingest.Barrier(ctx, libraryID)

		// 5. 差集 → 产 Delete 事件
		pruned := pruneMissingPaths(ctx, pool, ingest, libraryID, collectionType, trustedRoots, seenPaths)
		if pruned > 0 {
			slog.Info("[Scan] Pruning enqueued", "library", libraryName, "count", pruned)
		}

		// 6. Barrier 等 Delete 处理完
		ingest.Barrier(ctx, libraryID)

		close(progressStop)
		progressWg.Wait()

		// 7. 完成 + 触发入队式后置任务
		slog.Info("[Scan] Completed", "library", libraryName)
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
