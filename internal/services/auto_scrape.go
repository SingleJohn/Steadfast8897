package services

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"
)

// autoScrapeRunning 保证同一 library 同时只有一个 autoScrapeNewItems 扫表入队
// (防 ingest 高频触发下重复扫表做无用功)。Phase 2 后该函数不再自己消费,
// 只负责 SELECT → EnqueueBatch,实际刮削由 ScrapeWorker 消费 scrape_queue 完成。
var autoScrapeRunning sync.Map // libraryID -> *atomic.Bool

// autoScrapeNewItems 把本 library 里"还缺 overview"且"未在 cooldown"的
// Movie/Series 一批入队到 scrape_queue,task_type=identify。
// 入队是幂等的(UNIQUE(item_id, task_type)),重复触发不会放大工作量。
func autoScrapeNewItems(ctx context.Context, pool *pgxpool.Pool, libraryID string) {
	flagAny, _ := autoScrapeRunning.LoadOrStore(libraryID, &atomic.Bool{})
	flag := flagAny.(*atomic.Bool)
	if !flag.CompareAndSwap(false, true) {
		slog.Debug("[AutoScrape] Already enqueueing for library, skip", "library", libraryID)
		return
	}
	defer flag.Store(false)

	var autoEnabled *string
	pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'auto_scrape_enabled'").Scan(&autoEnabled)
	if autoEnabled == nil || *autoEnabled != "true" {
		return
	}

	// 仅扫没刮过的 Movie/Series,且不在 identify_cooldown 内。
	// identify_cooldown_until 字段保留(Phase 4 删除),和 scrape_queue.next_run_at
	// 是两套退避机制:前者是"单次刮削失败后的整块冷却",后者是队列调度里的指数退避。
	rows, err := pool.Query(ctx,
		`SELECT id::text FROM items
		  WHERE library_id = $1::uuid
		    AND type IN ('Movie', 'Series')
		    AND (overview IS NULL OR overview = '')
		    AND (identify_cooldown_until IS NULL OR identify_cooldown_until < NOW())
		  ORDER BY created_at DESC`,
		libraryID)
	if err != nil {
		slog.Warn("[AutoScrape] Query failed", "library", libraryID, "error", err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	rows.Close()

	if len(ids) == 0 {
		return
	}

	queue := NewScrapeQueue(pool)
	enqueued, err := queue.EnqueueBatch(ctx, ids, ScrapeTaskIdentify, ScrapePriorityIdentify)
	if err != nil {
		slog.Warn("[AutoScrape] Enqueue failed", "library", libraryID, "error", err)
		return
	}
	slog.Info("[AutoScrape] Enqueued identify tasks", "library", libraryID, "count", enqueued)
}
