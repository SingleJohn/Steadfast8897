package services

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"
)

const (
	// scrape worker 默认参数。TMDB 免费额度 40 req/10s(= 4 req/s),
	// 留一点余量给同步路径(用户手动 Identify、playback 自动刮削等)共享,
	// worker 用 3 req/s + burst 5,所有通过 TmdbClient.tmdbGet 走同一个 limiter。
	scrapeWorkerConcurrency = 4
	scrapeClaimBatch        = 8
	scrapeMaxRetry          = 5
	scrapeIdleSleep         = 5 * time.Second

	tmdbRatePerSec = 3
	tmdbRateBurst  = 5
)

// ScrapeWorker 消费 scrape_queue,按 task_type 分派到对应的单 item 处理函数。
// 处理函数实现在 auto_scrape.go / backfill_*.go,Worker 只负责调度 + 重试 + 限流。
type ScrapeWorker struct {
	queue   *ScrapeQueue
	pool    *pgxpool.Pool
	limiter *rate.Limiter
	workers int

	mu      sync.Mutex
	running bool

	// cachedClient 是 worker 生命周期内复用的 TmdbClient,配合 GetScrapeAggregator
	// 的 key=client 缓存命中 —— 原先每个任务都重建 aggregator + http.Transport,
	// 4 并发 worker 高频 identify 时开销显著。Admin 改 tmdb_* 配置后需重启生效。
	cachedClient atomic.Pointer[TmdbClient]
}

// NewScrapeWorker 构造。limiter 由 main 创建并设入 TmdbClient 共享,
// worker 这里拿一份引用主要是为了将来观测(Wait 次数等)。
func NewScrapeWorker(pool *pgxpool.Pool, queue *ScrapeQueue, limiter *rate.Limiter) *ScrapeWorker {
	return &ScrapeWorker{
		queue:   queue,
		pool:    pool,
		limiter: limiter,
		workers: scrapeWorkerConcurrency,
	}
}

// NewTmdbLimiter 返回共享给所有 TMDB 调用点的 rate.Limiter。
// 作为 package 函数方便 main.go 和测试注入。
func NewTmdbLimiter() *rate.Limiter {
	return rate.NewLimiter(rate.Limit(tmdbRatePerSec), tmdbRateBurst)
}

// Run 启动 worker:先 reconcile,再起 N 个 consume 循环。ctx 结束后全部停止。
func (w *ScrapeWorker) Run(ctx context.Context) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	if err := w.queue.ReconcileOnStartup(ctx); err != nil {
		slog.Warn("[ScrapeWorker] reconcile failed", "error", err)
	}

	slog.Info("[ScrapeWorker] started",
		"workers", w.workers, "claim_batch", scrapeClaimBatch,
		"max_retry", scrapeMaxRetry, "tmdb_rps", tmdbRatePerSec)

	var wg sync.WaitGroup
	for i := 0; i < w.workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			w.consume(ctx, id)
		}(i)
	}

	// 定期 prune done 任务(每 12h)
	wg.Add(1)
	go func() {
		defer wg.Done()
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

	<-ctx.Done()
	slog.Info("[ScrapeWorker] stopping")
}

func (w *ScrapeWorker) consume(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		tasks, err := w.queue.Claim(ctx, scrapeClaimBatch)
		if err != nil {
			slog.Warn("[ScrapeWorker] claim failed", "worker", id, "error", err)
			sleepOrCancel(ctx, scrapeIdleSleep)
			continue
		}
		if len(tasks) == 0 {
			sleepOrCancel(ctx, scrapeIdleSleep)
			continue
		}

		for _, t := range tasks {
			if ctx.Err() != nil {
				return
			}
			w.runTask(ctx, id, t)
		}
	}
}

func (w *ScrapeWorker) runTask(ctx context.Context, workerID int, t QueueTask) {
	start := time.Now()
	err := w.dispatch(ctx, t)
	dur := time.Since(start)

	if err != nil {
		slog.Info("[ScrapeWorker] task failed",
			"worker", workerID, "type", t.TaskType, "item", t.ItemID,
			"retry", t.RetryCount, "error", err, "duration", dur)
		w.queue.Fail(ctx, t.ID, t.RetryCount, scrapeMaxRetry, err.Error())
		return
	}

	w.queue.Done(ctx, t.ID)
	if dur > 3*time.Second {
		slog.Info("[ScrapeWorker] slow task",
			"worker", workerID, "type", t.TaskType, "item", t.ItemID, "duration", dur)
	}
}

// dispatch 按 task_type 路由到单 item 处理函数。
// 每个处理函数定义在对应的 auto_scrape.go / backfill_*.go,
// 保持调度与业务分离。
func (w *ScrapeWorker) dispatch(ctx context.Context, t QueueTask) error {
	switch t.TaskType {
	case ScrapeTaskIdentify, ScrapeTaskRefresh:
		client := w.tmdbClient(ctx)
		if client == nil {
			return fmt.Errorf("tmdb client unavailable (api key not configured)")
		}
		_, err := ScrapeItemWithClient(ctx, w.pool, t.ItemID, client)
		return err

	case ScrapeTaskBackfillQuality:
		return processBackfillQualityTask(ctx, w.pool, t.ItemID)

	case ScrapeTaskBackfillEpisodeName:
		client := w.tmdbClient(ctx)
		if client == nil {
			return fmt.Errorf("tmdb client unavailable")
		}
		return processBackfillEpisodeNameTask(ctx, w.pool, client, t.ItemID)

	case ScrapeTaskBackfillEpisodeImg:
		// 本地兜底在扫描入队阶段已处理;worker 走 TMDB 下载。
		// TMDB 开关关闭时直接标 done(不算失败)。
		if !readEpisodeStillFetchEnabled(ctx, w.pool) {
			return nil
		}
		client := w.tmdbClient(ctx)
		if client == nil {
			return fmt.Errorf("tmdb client unavailable")
		}
		return processBackfillEpisodeImageTask(ctx, w.pool, client, t.ItemID)
	}
	return fmt.Errorf("unknown task_type: %s", t.TaskType)
}

// tmdbClient 返回 worker 级缓存的 TmdbClient(lazy init)。
// 配合 GetScrapeAggregator 的 key=client 缓存,让同 worker 内所有 identify/name/image
// 任务共享同一个 Aggregator + http.Transport 连接池。
func (w *ScrapeWorker) tmdbClient(ctx context.Context) *TmdbClient {
	if c := w.cachedClient.Load(); c != nil {
		return c
	}
	c := TmdbClientFromConfig(ctx, w.pool)
	if c == nil {
		return nil
	}
	if !w.cachedClient.CompareAndSwap(nil, c) {
		// 被别的 goroutine 先赢 —— 用它设的值,丢弃自己 build 的
		return w.cachedClient.Load()
	}
	return c
}

// InvalidateCachedClient 让下一次 tmdbClient 重建,同时失效 Aggregator 缓存。
// Admin 改 tmdb_* 配置后调。
func (w *ScrapeWorker) InvalidateCachedClient() {
	w.cachedClient.Store(nil)
	InvalidateScrapeAggregator()
}

func sleepOrCancel(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
