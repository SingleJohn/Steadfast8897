package services

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// autoScrapeRunning 保证同一 library 同时只有一个 autoScrapeNewItems 在跑,
// 避免扫库频繁触发(file_watcher / 手动)时多个 goroutine 抢同一批未刮削 item,
// 造成 PG 重复 UPDATE / 事务回滚风暴。
var autoScrapeRunning sync.Map // libraryID -> *atomic.Bool

func autoScrapeNewItems(ctx context.Context, pool *pgxpool.Pool, libraryID string) {
	flagAny, _ := autoScrapeRunning.LoadOrStore(libraryID, &atomic.Bool{})
	flag := flagAny.(*atomic.Bool)
	if !flag.CompareAndSwap(false, true) {
		slog.Debug("[AutoScrape] Already running for library, skip", "library", libraryID)
		return
	}
	defer flag.Store(false)

	var autoEnabled *string
	pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'auto_scrape_enabled'").Scan(&autoEnabled)
	if autoEnabled == nil || *autoEnabled != "true" {
		return
	}

	client := TmdbClientFromConfig(ctx, pool)
	if client == nil {
		slog.Warn("[AutoScrape] TMDB API key not configured, skipping")
		return
	}

	type newItem struct {
		id   string
		name string
	}

	totalSuccess, totalFailed := 0, 0
	for batch := 0; ; batch++ {
		rows, err := pool.Query(ctx,
			"SELECT id::text, name FROM items WHERE library_id = $1::uuid AND type IN ('Movie', 'Series') "+
				"AND (overview IS NULL OR overview = '') "+
				"AND (identify_cooldown_until IS NULL OR identify_cooldown_until < NOW()) "+
				"ORDER BY created_at DESC LIMIT 50",
			libraryID)
		if err != nil {
			break
		}

		var items []newItem
		for rows.Next() {
			var item newItem
			if err := rows.Scan(&item.id, &item.name); err != nil {
				continue
			}
			items = append(items, item)
		}
		rows.Close()

		if len(items) == 0 {
			break
		}

		slog.Info("[AutoScrape] Batch start", "batch", batch+1, "count", len(items), "library", libraryID)

		success, failed := 0, 0
		for _, item := range items {
			_, err := ScrapeItemWithClient(ctx, pool, item.id, client)
			if err != nil {
				failed++
				slog.Debug("[AutoScrape] Failed", "name", item.name, "error", err)
			} else {
				success++
			}
			time.Sleep(300 * time.Millisecond)
		}

		totalSuccess += success
		totalFailed += failed
		slog.Info("[AutoScrape] Batch done", "batch", batch+1, "success", success, "failed", failed)

		// 如果全部失败说明 TMDB 不可达，停止
		if success == 0 {
			slog.Warn("[AutoScrape] All items in batch failed, stopping", "library", libraryID)
			break
		}
	}

	if totalSuccess > 0 || totalFailed > 0 {
		slog.Info("[AutoScrape] Done", "success", totalSuccess, "failed", totalFailed)
	}
}
