package services

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyms/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	refreshWorkerConcurrency = 1
	refreshClaimBatch        = 4
	refreshMaxRetry          = 5
	refreshIdleSleep         = 5 * time.Second
	refreshWorkerMax         = 8
)

type RefreshWorker struct {
	queue       *RefreshQueue
	scrapeQueue *ScrapeQueue
	pool        *pgxpool.Pool

	runMu   sync.Mutex
	running bool

	workerMu   sync.Mutex
	consumers  []chan struct{}
	workersCtx context.Context
}

func NewRefreshWorker(pool *pgxpool.Pool, queue *RefreshQueue, scrapeQueue *ScrapeQueue) *RefreshWorker {
	return &RefreshWorker{
		queue:       queue,
		scrapeQueue: scrapeQueue,
		pool:        pool,
	}
}

func (w *RefreshWorker) Run(ctx context.Context) {
	w.runMu.Lock()
	if w.running {
		w.runMu.Unlock()
		return
	}
	w.running = true
	w.runMu.Unlock()

	if err := w.queue.ReconcileOnStartup(ctx); err != nil {
		slog.Warn("[RefreshWorker] reconcile failed", "error", err)
	}

	w.workersCtx = ctx
	n := w.loadDesiredCount(ctx)
	w.SetWorkerCount(n)
	slog.Info("[RefreshWorker] started", "workers", n, "claim_batch", refreshClaimBatch)

	go w.watchConfig(ctx, 60*time.Second)

	go func() {
		ticker := time.NewTicker(12 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = w.queue.PruneDone(ctx)
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = w.queue.ReconcileStaleRunning(ctx)
			}
		}
	}()

	<-ctx.Done()
	slog.Info("[RefreshWorker] stopping")
	w.workerMu.Lock()
	for _, ch := range w.consumers {
		close(ch)
	}
	w.consumers = nil
	w.workerMu.Unlock()
}

func (w *RefreshWorker) WorkerCount() int {
	w.workerMu.Lock()
	defer w.workerMu.Unlock()
	return len(w.consumers)
}

func (w *RefreshWorker) SetWorkerCount(n int) {
	if n < 1 {
		n = 1
	}
	if n > refreshWorkerMax {
		n = refreshWorkerMax
	}

	w.workerMu.Lock()
	defer w.workerMu.Unlock()

	cur := len(w.consumers)
	if n == cur {
		return
	}
	if n > cur {
		for i := cur; i < n; i++ {
			stopCh := make(chan struct{})
			w.consumers = append(w.consumers, stopCh)
			go w.consume(w.workersCtx, i, stopCh)
		}
		slog.Info("[RefreshWorker] Resized workers up", "from", cur, "to", n)
		return
	}
	for i := n; i < cur; i++ {
		close(w.consumers[i])
	}
	w.consumers = w.consumers[:n]
	slog.Info("[RefreshWorker] Resized workers down", "from", cur, "to", n)
}

func (w *RefreshWorker) loadDesiredCount(ctx context.Context) int {
	raw := readSystemConfigValue(ctx, w.pool, "refresh_worker_count")
	if raw == "" {
		return refreshWorkerConcurrency
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return refreshWorkerConcurrency
	}
	return n
}

func (w *RefreshWorker) watchConfig(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n := w.loadDesiredCount(ctx); n != w.WorkerCount() {
				w.SetWorkerCount(n)
			}
		}
	}
}

func (w *RefreshWorker) consume(ctx context.Context, workerID int, stopCh chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		default:
		}

		tasks, err := w.queue.Claim(ctx, refreshClaimBatch)
		if err != nil {
			slog.Warn("[RefreshWorker] claim failed", "worker", workerID, "error", err)
			if !sleepOrStop(ctx, stopCh, refreshIdleSleep) {
				return
			}
			continue
		}
		if len(tasks) == 0 {
			if !sleepOrStop(ctx, stopCh, refreshIdleSleep) {
				return
			}
			continue
		}
		for _, t := range tasks {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			default:
			}
			w.runTask(ctx, workerID, t)
		}
	}
}

func (w *RefreshWorker) runTask(ctx context.Context, workerID int, t RefreshTask) {
	start := time.Now()
	err := w.dispatch(ctx, t)
	dur := time.Since(start)
	if err != nil {
		slog.Info("[RefreshWorker] task failed",
			"worker", workerID, "scope", t.Scope, "item", t.ItemID,
			"retry", t.RetryCount, "error", err, "duration", dur)
		w.queue.Fail(ctx, t.ID, t.RetryCount, refreshMaxRetry, err.Error())
		return
	}
	w.queue.Done(ctx, t.ID)
	if dur > 3*time.Second {
		slog.Info("[RefreshWorker] slow task", "worker", workerID, "scope", t.Scope, "item", t.ItemID, "duration", dur)
	}
}

func (w *RefreshWorker) dispatch(ctx context.Context, t RefreshTask) error {
	if t.Options.AllowRemote && !t.Options.ValidateOnly && t.Scope == RefreshScopeMetadata && w.scrapeQueue != nil {
		if err := w.scrapeQueue.Enqueue(ctx, t.ItemID, ScrapeTaskRefresh, ScrapePriorityRefresh); err != nil {
			return err
		}
		return nil
	}

	switch t.Scope {
	case RefreshScopeMetadata:
		return refreshItemLocalMetadata(ctx, w.pool, t.ItemID, t.Options)
	case RefreshScopeImages:
		return refreshItemLocalImages(ctx, w.pool, t.ItemID, t.Options)
	case RefreshScopeSubtree:
		if err := refreshItemLocalMetadata(ctx, w.pool, t.ItemID, t.Options); err != nil {
			return err
		}
		if err := refreshItemLocalImages(ctx, w.pool, t.ItemID, t.Options); err != nil {
			return err
		}
		return RefreshSeriesSubtree(ctx, w.pool, w.queue, w.scrapeQueue, t.ItemID, t.Source, t.Priority, t.Options)
	default:
		return fmt.Errorf("unknown refresh scope: %s", t.Scope)
	}
}

type refreshItemInfo struct {
	ID                uuid.UUID
	Type              string
	FilePath          *string
	PrimaryImagePath  *string
	BackdropImagePath *string
	ParentID          *uuid.UUID
	SeasonID          *uuid.UUID
	IndexNumber       *int32
	ParentIndexNumber *int32
}

func loadRefreshItemInfo(ctx context.Context, pool *pgxpool.Pool, itemID string) (*refreshItemInfo, error) {
	var info refreshItemInfo
	err := pool.QueryRow(ctx,
		`SELECT id, type, file_path, primary_image_path, backdrop_image_path,
		        parent_id, season_id, index_number, parent_index_number
		   FROM items
		  WHERE id = $1::uuid`,
		itemID,
	).Scan(
		&info.ID, &info.Type, &info.FilePath, &info.PrimaryImagePath, &info.BackdropImagePath,
		&info.ParentID, &info.SeasonID, &info.IndexNumber, &info.ParentIndexNumber,
	)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func refreshItemLocalMetadata(ctx context.Context, pool *pgxpool.Pool, itemID string, opts RefreshOptions) error {
	info, err := loadRefreshItemInfo(ctx, pool, itemID)
	if err != nil {
		return err
	}

	switch info.Type {
	case "Movie":
		dir := mediaDirFromItemPath(info.FilePath)
		if dir == "" {
			return nil
		}
		cache := CacheDir(dir)
		videoBasename := ""
		if info.FilePath != nil {
			videoBasename = filepath.Base(*info.FilePath)
		}
		allowGenericSidecars := allowGenericMovieRefreshSidecars(ctx, pool, dir)
		if nfoPath := FindMovieNfoCached(cache, videoBasename, allowGenericSidecars); nfoPath != nil {
			if nfo := ParseNfo(*nfoPath); nfo != nil && !opts.ValidateOnly {
				ApplyNfoDataWithPlatformSource(ctx, pool, itemID, nfo, models.PlatformScanSourceNFO)
			}
		}
	case "Series":
		if info.FilePath == nil || strings.TrimSpace(*info.FilePath) == "" {
			return nil
		}
		cache := CacheDir(*info.FilePath)
		for _, entry := range cache {
			if entry[0] == "tvshow.nfo" {
				if nfo := ParseNfo(entry[1]); nfo != nil && !opts.ValidateOnly {
					ApplyNfoDataWithPlatformSource(ctx, pool, itemID, nfo, models.PlatformScanSourceNFO)
				}
				break
			}
		}
	case "Episode":
		if info.FilePath == nil || strings.TrimSpace(*info.FilePath) == "" {
			return nil
		}
		dir := filepath.Dir(*info.FilePath)
		stem := strings.TrimSuffix(strings.ToLower(filepath.Base(*info.FilePath)), filepath.Ext(*info.FilePath))
		cache := CacheDir(dir)
		for _, entry := range cache {
			if entry[0] == stem+".nfo" {
				if nfo := ParseNfo(entry[1]); nfo != nil && !opts.ValidateOnly {
					ApplyNfoDataWithPlatformSource(ctx, pool, itemID, nfo, models.PlatformScanSourceNFO)
				}
				break
			}
		}
	}
	return nil
}

func refreshItemLocalImages(ctx context.Context, pool *pgxpool.Pool, itemID string, opts RefreshOptions) error {
	info, err := loadRefreshItemInfo(ctx, pool, itemID)
	if err != nil {
		return err
	}
	if opts.ValidateOnly {
		return nil
	}

	switch info.Type {
	case "Movie":
		dir := mediaDirFromItemPath(info.FilePath)
		if dir == "" {
			return nil
		}
		cache := CacheDir(dir)
		videoBasename := ""
		if info.FilePath != nil {
			videoBasename = filepath.Base(*info.FilePath)
		}
		allowGenericSidecars := allowGenericMovieRefreshSidecars(ctx, pool, dir)
		poster := FindMovieImageCached(cache, videoBasename, posterImagePrefixes, allowGenericSidecars)
		backdrop := FindMovieImageCached(cache, videoBasename, backdropImagePrefixes, allowGenericSidecars)
		clearPoster := poster == nil && info.PrimaryImagePath != nil &&
			isManagedNamedImagePath(*info.PrimaryImagePath, dir, posterImagePrefixes)
		clearBackdrop := backdrop == nil && info.BackdropImagePath != nil &&
			isManagedNamedImagePath(*info.BackdropImagePath, dir, backdropImagePrefixes)
		return syncItemArtworkWithClear(
			ctx, pool, info.ID,
			poster, ptrAndThen(poster, GenerateImageTag), clearPoster,
			backdrop, ptrAndThen(backdrop, GenerateImageTag), clearBackdrop,
		)
	case "Series":
		if info.FilePath == nil || strings.TrimSpace(*info.FilePath) == "" {
			return nil
		}
		cache := CacheDir(*info.FilePath)
		poster := FindImageCached(cache, posterImagePrefixes)
		backdrop := FindImageCached(cache, backdropImagePrefixes)
		clearPoster := poster == nil && info.PrimaryImagePath != nil &&
			isManagedNamedImagePath(*info.PrimaryImagePath, *info.FilePath, posterImagePrefixes)
		clearBackdrop := backdrop == nil && info.BackdropImagePath != nil &&
			isManagedNamedImagePath(*info.BackdropImagePath, *info.FilePath, backdropImagePrefixes)
		return syncItemArtworkWithClear(
			ctx, pool, info.ID,
			poster, ptrAndThen(poster, GenerateImageTag), clearPoster,
			backdrop, ptrAndThen(backdrop, GenerateImageTag), clearBackdrop,
		)
	case "Season":
		dir, err := seasonDirFromSeasonID(ctx, pool, itemID)
		if err != nil || dir == "" {
			return err
		}
		cache := CacheDir(dir)
		poster := FindImageCached(cache, posterImagePrefixes)
		clearPoster := poster == nil && info.PrimaryImagePath != nil &&
			isManagedNamedImagePath(*info.PrimaryImagePath, dir, posterImagePrefixes)
		return syncItemArtworkWithClear(
			ctx, pool, info.ID,
			poster, ptrAndThen(poster, GenerateImageTag), clearPoster,
			nil, nil, false,
		)
	case "Episode":
		if info.FilePath == nil || strings.TrimSpace(*info.FilePath) == "" {
			return nil
		}
		dir := filepath.Dir(*info.FilePath)
		cache := CacheDir(dir)
		base := filepath.Base(*info.FilePath)
		thumb := FindEpisodeThumbCached(cache, base)
		clearPoster := thumb == nil && info.PrimaryImagePath != nil &&
			isManagedEpisodeThumbPath(*info.PrimaryImagePath, dir, base)
		return syncItemArtworkWithClear(
			ctx, pool, info.ID,
			thumb, ptrAndThen(thumb, GenerateImageTag), clearPoster,
			nil, nil, false,
		)
	}
	return nil
}

func allowGenericMovieRefreshSidecars(ctx context.Context, pool *pgxpool.Pool, dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return true
	}
	var count int
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*)
		   FROM items
		  WHERE type = 'Movie'
		    AND file_path IS NOT NULL
		    AND (file_path = $1 OR file_path LIKE $2)`,
		dir, dir+string(filepath.Separator)+"%").Scan(&count)
	if err != nil {
		return true
	}
	return count <= 1
}

func mediaDirFromItemPath(filePath *string) string {
	if filePath == nil {
		return ""
	}
	p := strings.TrimSpace(*filePath)
	if p == "" {
		return ""
	}
	if info, err := os.Stat(p); err == nil && info.IsDir() {
		return p
	}
	return filepath.Dir(p)
}

func seasonDirFromSeasonID(ctx context.Context, pool *pgxpool.Pool, seasonID string) (string, error) {
	var episodePath *string
	err := pool.QueryRow(ctx,
		`SELECT file_path
		   FROM items
		  WHERE season_id = $1::uuid
		    AND type = 'Episode'
		    AND file_path IS NOT NULL
		  ORDER BY created_at ASC
		  LIMIT 1`,
		seasonID,
	).Scan(&episodePath)
	if err != nil || episodePath == nil || strings.TrimSpace(*episodePath) == "" {
		return "", err
	}
	return filepath.Dir(*episodePath), nil
}
