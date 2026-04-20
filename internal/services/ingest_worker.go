package services

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ingestChannelBuffer = 10000
	ingestWorkers       = 4
)

// IngestWorker 是扫描/监控/Webhook 统一的事件消费端。
// 3 个事件产源(FileWatcher/webhook/Phase 3 scan)都通过 Submit 入队,
// N 个 consume goroutine 并发处理,落库走现有的 scanOneMovie/scanOneShow
// 以保证与手动全扫行为完全一致。
type IngestWorker struct {
	ch       chan IngestEvent
	overflow atomic.Int64
	pool     *pgxpool.Pool
	cache    *CacheService
	libs     *libraryIndex
	renames  *renameBuffer
	workers  int
}

func NewIngestWorker(pool *pgxpool.Pool, cache *CacheService) *IngestWorker {
	w := &IngestWorker{
		ch:      make(chan IngestEvent, ingestChannelBuffer),
		pool:    pool,
		cache:   cache,
		libs:    newLibraryIndex(pool),
		workers: ingestWorkers,
	}
	w.renames = newRenameBuffer(w.enqueue)
	return w
}

// Submit 事件入口。Delete 和 Create 会先走 renameBuffer 以尝试配对成 Rename。
// 非阻塞:channel 满则 overflow 计数 + warn 日志(每 100 条打一次,避免刷屏)。
func (w *IngestWorker) Submit(e IngestEvent) {
	switch e.Kind {
	case EventDelete:
		w.renames.OnDelete(e)
	case EventCreate:
		if !w.renames.OnCreate(e) {
			w.enqueue(e)
		}
	default:
		w.enqueue(e)
	}
}

func (w *IngestWorker) enqueue(e IngestEvent) {
	select {
	case w.ch <- e:
	default:
		prev := w.overflow.Add(1)
		if prev%100 == 1 {
			slog.Warn("[Ingest] channel overflow, event dropped",
				"total_dropped", prev, "kind", e.Kind.String(), "path", e.Path)
		}
	}
}

// OverflowCount 供观测/管理面板查询 channel 溢出总数(Phase 4 会接到 metrics)。
func (w *IngestWorker) OverflowCount() int64 { return w.overflow.Load() }

// Run 启动 Worker:加载 library 映射 + 启动定时刷新 + N 个消费 goroutine。
// 传入的 ctx 结束后所有 goroutine 停止。
func (w *IngestWorker) Run(ctx context.Context) {
	if err := w.libs.Refresh(ctx); err != nil {
		slog.Error("[Ingest] Library index refresh failed", "error", err)
	}
	w.libs.StartAutoRefresh(ctx, 60*time.Second)

	slog.Info("[Ingest] Worker started", "workers", w.workers, "buffer", ingestChannelBuffer)

	for i := 0; i < w.workers; i++ {
		go w.consume(ctx, i)
	}
	<-ctx.Done()
	slog.Info("[Ingest] Worker stopping")
}

func (w *IngestWorker) consume(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-w.ch:
			start := time.Now()
			if err := w.processEvent(ctx, e); err != nil {
				slog.Warn("[Ingest] processEvent failed",
					"worker", id, "kind", e.Kind.String(), "path", e.Path, "error", err)
			}
			w.cache.Del(ctx, "views:all")
			w.cache.DelPattern(ctx, "latest:*")
			if dur := time.Since(start); dur > 2*time.Second {
				slog.Info("[Ingest] Slow event",
					"worker", id, "kind", e.Kind.String(), "path", e.Path, "duration", dur)
			}
		}
	}
}

func (w *IngestWorker) processEvent(ctx context.Context, e IngestEvent) error {
	switch e.Kind {
	case EventCreate, EventModify:
		return w.processCreate(ctx, e)
	case EventDelete:
		return w.processDelete(ctx, e)
	case EventRename:
		return w.processRename(ctx, e)
	}
	return nil
}

func (w *IngestWorker) processCreate(ctx context.Context, e IngestEvent) error {
	libID, colType, ok := w.libs.Match(e.Path)
	if !ok {
		slog.Debug("[Ingest] Create skipped: path outside any library", "path", e.Path)
		return nil
	}
	switch colType {
	case "movies":
		return w.processMovieCreate(ctx, libID, e)
	case "tvshows":
		return w.processTvCreate(ctx, libID, e)
	}
	return nil
}

func (w *IngestWorker) processMovieCreate(ctx context.Context, libID string, e IngestEvent) error {
	name := filepath.Base(e.Path)
	existing := map[string]bool{}
	scanOneMovie(ctx, w.pool, libID, name, e.Path, e.IsDir, existing)
	go autoScrapeNewItems(ctx, w.pool, libID)
	return nil
}

func (w *IngestWorker) processTvCreate(ctx context.Context, libID string, e IngestEvent) error {
	showPath := w.findShowRoot(libID, e.Path)
	if showPath == "" {
		slog.Debug("[Ingest] Tv create skipped: no show dir found", "path", e.Path)
		return nil
	}
	existing := map[string]bool{}
	scanOneShow(ctx, w.pool, libID, filepath.Base(showPath), showPath, existing)
	go autoScrapeNewItems(ctx, w.pool, libID)
	return nil
}

// findShowRoot 从文件路径向上找第一个 isShowDir()==true 的目录;
// 若 filePath 本身就是目录(IsDir=true)且是 show 根,也返回它。
func (w *IngestWorker) findShowRoot(libID, filePath string) string {
	roots := w.libs.libraryRoots(libID)
	cleaned := filepath.Clean(filePath)

	for _, root := range roots {
		rel, err := filepath.Rel(root, cleaned)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		cur := cleaned
		for cur != root && cur != filepath.Dir(cur) {
			if isShowDir(cur) {
				return cur
			}
			cur = filepath.Dir(cur)
		}
	}
	return ""
}

// processDelete 统一删除逻辑,修 file_watcher.go:190 bug:
// 目录删除要匹配 file_path = <path> 或 file_path LIKE '<path>/%',
// 否则整个目录被删后库里残留。清理完再补空 Season/Series 的回收。
func (w *IngestWorker) processDelete(ctx context.Context, e IngestEvent) error {
	norm := filepath.Clean(e.Path)
	prefix := norm + string(filepath.Separator) + "%"

	tag, err := w.pool.Exec(ctx,
		"DELETE FROM items WHERE file_path = $1 OR file_path LIKE $2",
		norm, prefix)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		slog.Info("[Ingest] Delete removed items", "count", tag.RowsAffected(), "path", norm)
		_ = cleanupEmptyParents(ctx, w.pool)
	}
	return nil
}

// processRename 保留 item 身份(播放进度/merge 关系/media_versions),只改 file_path。
// 若旧路径在库里找不到(ingest 漏了之前的 Create),fallback 按 Create 处理新路径。
func (w *IngestWorker) processRename(ctx context.Context, e IngestEvent) error {
	oldPath := filepath.Clean(e.OldPath)
	newPath := filepath.Clean(e.Path)

	if e.IsDir {
		rows, err := w.pool.Query(ctx,
			"SELECT id, file_path FROM items WHERE file_path = $1 OR file_path LIKE $2",
			oldPath, oldPath+string(filepath.Separator)+"%")
		if err != nil {
			return err
		}
		type upd struct {
			id uuid.UUID
			fp string
		}
		var updates []upd
		for rows.Next() {
			var u upd
			if rows.Scan(&u.id, &u.fp) == nil {
				updates = append(updates, u)
			}
		}
		rows.Close()

		for _, u := range updates {
			var nfp string
			if u.fp == oldPath {
				nfp = newPath
			} else {
				nfp = newPath + u.fp[len(oldPath):]
			}
			w.pool.Exec(ctx, "UPDATE items SET file_path = $1, updated_at = NOW() WHERE id = $2", nfp, u.id)
			w.pool.Exec(ctx, "UPDATE media_versions SET file_path = $1 WHERE file_path = $2", nfp, u.fp)
		}
		if len(updates) > 0 {
			slog.Info("[Ingest] Rename updated items", "count", len(updates), "from", oldPath, "to", newPath)
		}
		return nil
	}

	tag, err := w.pool.Exec(ctx,
		"UPDATE items SET file_path = $1, updated_at = NOW() WHERE file_path = $2",
		newPath, oldPath)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		w.pool.Exec(ctx, "UPDATE media_versions SET file_path = $1 WHERE file_path = $2", newPath, oldPath)
		slog.Info("[Ingest] Rename", "from", oldPath, "to", newPath)
		return nil
	}
	// 老路径不在库里:当作新建处理
	return w.processCreate(ctx, IngestEvent{
		Kind:       EventCreate,
		Path:       newPath,
		IsDir:      false,
		Source:     e.Source,
		DetectedAt: time.Now(),
	})
}
