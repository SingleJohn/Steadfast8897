package services

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
)

// autoScrapeRunning 保证同一 library 同时只有一个 autoScrapeNewItems 扫表入队
// (防 ingest 高频触发下重复扫表做无用功)。Phase 2 后该函数不再自己消费,
// 只负责 SELECT → EnqueueBatch,实际刮削由 ScrapeWorker 消费 scrape_queue 完成。
var autoScrapeRunning sync.Map // libraryID -> *atomic.Bool

// autoScrapeLastRun 记录每个 library 最近一次成功触发扫表入队的时间。
// CAS 只挡并发不挡高频串行,ingest 密集 modify 事件会让同一 library 几十 ms
// 完成一次扫表再进下一次,日志和 DB 双压。加一个 cooldown 窗口,window 内
// 直接跳过 —— 反正 ON CONFLICT + failed 的 item 由 ScrapeWorker 的
// next_run_at(2→4→8→16→32 分钟)兜底重试,不需要 ingest 层高频重扫。
var autoScrapeLastRun sync.Map // libraryID -> time.Time

const autoScrapeCooldown = 30 * time.Second

func autoScrapeEnabled(ctx context.Context, pool *pgxpool.Pool) bool {
	autoEnabled, ok, err := repository.NewSystemConfigRepository(pool).GetString(ctx, "auto_scrape_enabled")
	return err == nil && ok && autoEnabled == "true"
}

// enqueueIdentifyIfEligible 把"新建即入队 identify"的资格判断集中到一处,与 autoScrape 的
// 过滤边界保持一致:
//   - 仅顶层 Movie/Series
//   - 仍缺 overview
//   - 未建立任何 item_external_ids
//   - 非 NFO 托管条目
func enqueueIdentifyIfEligible(ctx context.Context, pool *pgxpool.Pool, itemID string, priority int16, source string) bool {
	if !autoScrapeEnabled(ctx, pool) {
		return false
	}

	var itemType, overview, platformSource string
	var hasExternalIDs bool
	err := pool.QueryRow(ctx,
		`SELECT type,
		        COALESCE(overview, ''),
		        COALESCE(platform_scan_source, ''),
		        EXISTS (
		            SELECT 1 FROM item_external_ids e WHERE e.item_id = items.id
		        )
		   FROM items
		  WHERE id = $1::uuid
		    AND type IN ('Movie', 'Series')`,
		itemID,
	).Scan(&itemType, &overview, &platformSource, &hasExternalIDs)
	if err != nil {
		slog.Warn("[AutoScrape] Check item eligibility failed",
			"item", itemID, "source", source, "error", err)
		return false
	}
	if strings.TrimSpace(overview) != "" || hasExternalIDs || platformSource == "nfo" {
		return false
	}

	if err := NewScrapeQueue(pool).Enqueue(ctx, itemID, ScrapeTaskIdentify, priority); err != nil {
		slog.Warn("[AutoScrape] Enqueue identify failed",
			"item", itemID, "source", source, "error", err)
		return false
	}

	slog.Info("[AutoScrape] Enqueued identify task",
		"item", itemID, "type", itemType, "source", source, "priority", priority)
	return true
}

// autoScrapeNewItems 把本 library 里"还缺 overview"且"未在 cooldown"的
// Movie/Series 一批入队到 scrape_queue,task_type=identify。
// 入队是幂等的(UNIQUE(item_id, task_type)),重复触发不会放大工作量。
func autoScrapeNewItems(ctx context.Context, pool *pgxpool.Pool, libraryID string) {
	if lastAny, ok := autoScrapeLastRun.Load(libraryID); ok {
		if time.Since(lastAny.(time.Time)) < autoScrapeCooldown {
			slog.Debug("[AutoScrape] Within cooldown window, skip", "library", libraryID)
			return
		}
	}

	flagAny, _ := autoScrapeRunning.LoadOrStore(libraryID, &atomic.Bool{})
	flag := flagAny.(*atomic.Bool)
	if !flag.CompareAndSwap(false, true) {
		slog.Debug("[AutoScrape] Already enqueueing for library, skip", "library", libraryID)
		return
	}
	defer flag.Store(false)

	// 拿到执行锁就立即建立 cooldown 窗口,不管后面 SELECT/Enqueue 成败。
	// 即使 auto_scrape_enabled=false 也占用窗口 —— 否则高频触发会反复查 system_config。
	autoScrapeLastRun.Store(libraryID, time.Now())

	if !autoScrapeEnabled(ctx, pool) {
		return
	}

	// 仅扫没刮过的 Movie/Series。Phase 5 后不再过滤 identify_cooldown_until
	// —— 退避完全由 scrape_queue.next_run_at 接管,UNIQUE(item_id, task_type)
	// 防重复入队,worker.Claim 只取 next_run_at <= NOW() 的任务。
	//
	// 跳过条件(= 已识别或人工托管):
	//   - item_external_ids 有任何记录(tmdb/imdb/douban/bangumi/...)→ 身份已定
	//   - platform_scan_source='nfo'            → NFO 已带 studio,人工托管
	// 这两种都跳过,避免浪费 provider 配额。
	rows, err := pool.Query(ctx,
		`SELECT id::text FROM items
		  WHERE library_id = $1::uuid
		    AND type IN ('Movie', 'Series')
		    AND (overview IS NULL OR overview = '')
		    AND NOT EXISTS (
		        SELECT 1 FROM item_external_ids e WHERE e.item_id = items.id
		    )
		    AND platform_scan_source IS DISTINCT FROM 'nfo'
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

// EnqueueMissingScrapeIdentify 给用户手动"刮削全部缺失元数据"入口用:
// 扫全表 Movie/Series,把 overview 为空且未识别(item_external_ids 无任何记录,
// 且非 NFO 源)的以 refresh 优先级(0,最高)入队 identify,worker 自动消费。
// 与 autoScrapeNewItems 的过滤条件一致,但不受 library 边界和 auto_scrape_enabled 限制。
// 返回入队数量(ON CONFLICT 时 tag.RowsAffected 仍计入已存在行的 priority 更新)。
func EnqueueMissingScrapeIdentify(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	rows, err := pool.Query(ctx,
		`SELECT id::text FROM items
		  WHERE type IN ('Movie', 'Series')
		    AND (overview IS NULL OR overview = '')
		    AND NOT EXISTS (
		        SELECT 1 FROM item_external_ids e WHERE e.item_id = items.id
		    )
		    AND platform_scan_source IS DISTINCT FROM 'nfo'`)
	if err != nil {
		return 0, err
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
		return 0, nil
	}
	return NewScrapeQueue(pool).EnqueueBatch(ctx, ids, ScrapeTaskIdentify, ScrapePriorityRefresh)
}
