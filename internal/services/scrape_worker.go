package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyms/internal/services/scraper"

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

	// SetWorkerCount clamp 上限。TMDB 限流 3rps,再多 worker 也只是在 limiter 上排队,
	// 徒增上下文切换,16 足够覆盖"突发补量"场景。
	scrapeWorkerMax = 16

	tmdbRatePerSec = 3
	tmdbRateBurst  = 5
)

// ScrapeWorker 消费 scrape_queue,按 task_type 分派到对应的单 item 处理函数。
// 处理函数实现在 auto_scrape.go / backfill_*.go,Worker 只负责调度 + 重试 + 限流。
type ScrapeWorker struct {
	queue   *ScrapeQueue
	pool    *pgxpool.Pool
	limiter *rate.Limiter

	// runMu 保护 Run 幂等启动。workerMu 保护 consumers 切片。两把锁独立,
	// 避免 SetWorkerCount 与 Run 启停互相等待。
	runMu   sync.Mutex
	running bool

	// consumers:每个元素对应一个运行中的 consume goroutine 的 stop channel。
	// SetWorkerCount 用它动态增减,配合 system_config.scrape_worker_count。
	workerMu   sync.Mutex
	consumers  []chan struct{}
	workersCtx context.Context

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
	}
}

// NewTmdbLimiter 返回共享给所有 TMDB 调用点的 rate.Limiter。
// 作为 package 函数方便 main.go 和测试注入。初始值用默认常量,
// 启动后由 ApplyTmdbLimiterConfig 根据 system_config 覆盖。
func NewTmdbLimiter() *rate.Limiter {
	return rate.NewLimiter(rate.Limit(tmdbRatePerSec), tmdbRateBurst)
}

// TmdbLimiterSetting 描述限流的数值,仅供查看/监控。
type TmdbLimiterSetting struct {
	RatePerSec float64
	Burst      int
}

// CurrentTmdbLimiterSetting 返回 sharedTmdbLimiter 当前生效的数值。
func CurrentTmdbLimiterSetting() TmdbLimiterSetting {
	if sharedTmdbLimiter == nil {
		return TmdbLimiterSetting{RatePerSec: float64(tmdbRatePerSec), Burst: tmdbRateBurst}
	}
	return TmdbLimiterSetting{
		RatePerSec: float64(sharedTmdbLimiter.Limit()),
		Burst:      sharedTmdbLimiter.Burst(),
	}
}

// readTmdbLimiterConfig 读 system_config 里的速率配置,
// 缺失或非法回退到 tmdbRatePerSec / tmdbRateBurst 默认值。
func readTmdbLimiterConfig(ctx context.Context, pool *pgxpool.Pool) (float64, int) {
	rps := float64(tmdbRatePerSec)
	burst := tmdbRateBurst

	if v := strings.TrimSpace(readSystemConfigValue(ctx, pool, "tmdb_rate_per_sec")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 50 {
			rps = f
		}
	}
	if v := strings.TrimSpace(readSystemConfigValue(ctx, pool, "tmdb_rate_burst")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 100 {
			burst = n
		}
	}
	return rps, burst
}

// ApplyTmdbLimiterConfig 从 system_config 读速率参数,用 SetLimit/SetBurst
// 实时生效到 sharedTmdbLimiter。无需重启进程。
//
// 调用时机:
//   - 启动时(main.go 创建 limiter 后立刻调一次)
//   - postConfiguration handler 保存 tmdb_rate_* 配置后
func ApplyTmdbLimiterConfig(ctx context.Context, pool *pgxpool.Pool) {
	if sharedTmdbLimiter == nil {
		return
	}
	rps, burst := readTmdbLimiterConfig(ctx, pool)
	curRPS := float64(sharedTmdbLimiter.Limit())
	curBurst := sharedTmdbLimiter.Burst()
	if curRPS == rps && curBurst == burst {
		return
	}
	sharedTmdbLimiter.SetLimit(rate.Limit(rps))
	sharedTmdbLimiter.SetBurst(burst)
	slog.Info("[TMDB] rate limiter updated",
		"rps", rps, "burst", burst,
		"prev_rps", curRPS, "prev_burst", curBurst)
}

// Run 启动 worker:reconcile + 按 system_config 起 N 个 consume goroutine + 启 watchConfig
// 兜底轮询 + 12h PruneDone。ctx 结束后关闭所有 consumer 并返回。
func (w *ScrapeWorker) Run(ctx context.Context) {
	w.runMu.Lock()
	if w.running {
		w.runMu.Unlock()
		return
	}
	w.running = true
	w.runMu.Unlock()

	if err := w.queue.ReconcileOnStartup(ctx); err != nil {
		slog.Warn("[ScrapeWorker] reconcile failed", "error", err)
	}

	w.workersCtx = ctx
	n := w.loadDesiredCount(ctx)
	w.SetWorkerCount(n)
	slog.Info("[ScrapeWorker] started",
		"workers", n, "claim_batch", scrapeClaimBatch,
		"max_retry", scrapeMaxRetry, "tmdb_rps", tmdbRatePerSec)

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

	// 运行中兜底:每 5 分钟扫一次 "running > 10 分钟" 的任务,抢救 panic/deadlock
	// 造成的卡死。正常任务 runTask 结束都会走 Done/Fail 转出 running 状态,所以
	// 这个 ticker 绝大多数情况下啥也不做。
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
	slog.Info("[ScrapeWorker] stopping")
	w.workerMu.Lock()
	for _, ch := range w.consumers {
		close(ch)
	}
	w.consumers = nil
	w.workerMu.Unlock()
}

// WorkerCount 返回当前运行中的 consume goroutine 数。
func (w *ScrapeWorker) WorkerCount() int {
	w.workerMu.Lock()
	defer w.workerMu.Unlock()
	return len(w.consumers)
}

// SetWorkerCount 动态调整 consume goroutine 数量。n<cur 时关闭多余的 stopCh;
// n>cur 时 spawn 新 goroutine。取值范围 [1, scrapeWorkerMax],超出自动 clamp。
// 幂等:n==cur 时直接返回。TMDB 共享 limiter 3rps,加 worker 只能加 burst,不能突破速率上限。
func (w *ScrapeWorker) SetWorkerCount(n int) {
	if n < 1 {
		n = 1
	}
	if n > scrapeWorkerMax {
		n = scrapeWorkerMax
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
		slog.Info("[ScrapeWorker] Resized workers up", "from", cur, "to", n)
		return
	}
	for i := n; i < cur; i++ {
		close(w.consumers[i])
	}
	w.consumers = w.consumers[:n]
	slog.Info("[ScrapeWorker] Resized workers down", "from", cur, "to", n)
}

// loadDesiredCount 从 system_config.scrape_worker_count 读目标数量;默认 scrapeWorkerConcurrency。
func (w *ScrapeWorker) loadDesiredCount(ctx context.Context) int {
	raw := readSystemConfigValue(ctx, w.pool, "scrape_worker_count")
	if raw == "" {
		return scrapeWorkerConcurrency
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return scrapeWorkerConcurrency
	}
	return n
}

// watchConfig 周期性对齐 system_config.scrape_worker_count(兜底:Admin handler 也会直接调 SetWorkerCount)。
func (w *ScrapeWorker) watchConfig(ctx context.Context, interval time.Duration) {
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

func (w *ScrapeWorker) consume(ctx context.Context, id int, stopCh chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		default:
		}

		tasks, err := w.queue.Claim(ctx, scrapeClaimBatch)
		if err != nil {
			slog.Warn("[ScrapeWorker] claim failed", "worker", id, "error", err)
			if !sleepOrStop(ctx, stopCh, scrapeIdleSleep) {
				return
			}
			continue
		}
		if len(tasks) == 0 {
			if !sleepOrStop(ctx, stopCh, scrapeIdleSleep) {
				return
			}
			continue
		}

		for _, t := range tasks {
			if ctx.Err() != nil {
				return
			}
			select {
			case <-stopCh:
				return
			default:
			}
			w.runTask(ctx, id, t)
		}
	}
}

func (w *ScrapeWorker) runTask(ctx context.Context, workerID int, t QueueTask) {
	ctx, diag := WithDiag(ctx)
	start := time.Now()
	err := w.dispatch(ctx, t)
	dur := time.Since(start)

	if err != nil {
		fatal := isScrapeFatalError(err)
		slog.Info("[ScrapeWorker] task failed",
			"worker", workerID, "type", t.TaskType, "item", t.ItemID,
			"retry", t.RetryCount, "error", err, "duration", dur, "fatal", fatal)
		if fatal {
			// 不可重试错误:直接落终态 failed,不走退避。
			// 避免同一批 "no match" / 非 TMDB 源无法映射 的 item 在 pending 里
			// 循环 5 次退避(最长 32 分钟)空耗代理和 worker。
			w.queue.FailFatal(ctx, t.ID, err.Error(), diag)
		} else {
			w.queue.Fail(ctx, t.ID, t.RetryCount, scrapeMaxRetry, err.Error(), diag)
		}
		return
	}

	w.queue.Done(ctx, t.ID)
	if dur > 3*time.Second {
		slog.Info("[ScrapeWorker] slow task",
			"worker", workerID, "type", t.TaskType, "item", t.ItemID, "duration", dur)
	}
}

// isScrapeFatalError 判断错误是否"重试也没用",应直接归入 failed 而不退避。
//
// 准则:**如果代码或数据没变,重试一模一样的操作拿到相同错误的概率接近 100%**
// 就是 fatal。临时性失败(HTTP / 超时 / 代理抖动 / 限流)**不在此列**,应走退避。
func isScrapeFatalError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, scraper.ErrNoMatch) {
		return true
	}
	s := err.Error()
	// 已入人工确认队列,不需要再 worker 重跑
	if strings.Contains(s, "identify queued for manual confirmation") {
		return true
	}
	// 类型不支持,代码层面就不会成功
	if strings.Contains(s, "cannot scrape type") {
		return true
	}
	// TMDB 404 在 identify 路径上通常意味着 tmdb_id 错误/TMDB 下架,重试依然 404
	if strings.Contains(s, "HTTP 404") {
		return true
	}
	return false
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

// sleepOrStop 比 sleepOrCancel 多监听 stopCh;返回 false 代表需要退出 consume。
// 动态缩容时老 worker 应当在下一次 idle/claim 失败后尽快感知到 stopCh 关闭。
func sleepOrStop(ctx context.Context, stopCh chan struct{}, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-stopCh:
		return false
	case <-time.After(d):
		return true
	}
}
