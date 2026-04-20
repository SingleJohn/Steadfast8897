package services

import (
	"context"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/models"
)

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

	cache.Del(ctx, "views:all")

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
