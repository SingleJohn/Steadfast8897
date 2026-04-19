package scraper

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrBreakerOpen = errors.New("scraper: circuit breaker open")

// Gate 把限流与熔断打包成单一门面。
// 设计目标是不引入 golang.org/x/time/rate 依赖,内部用 token bucket 自实现;
// 熔断用一个 5 分钟的 ring buffer 统计成功/失败计数。
type Gate struct {
	mu sync.Mutex

	// token bucket
	rps      float64
	burst    float64
	tokens   float64
	lastFill time.Time
	minGap   time.Duration
	lastCall time.Time

	// breaker
	windowBuckets []gateBucket
	bucketSpan    time.Duration
	windowSpan    time.Duration
	minSamples    int
	errorRate     float64
	cooldown      time.Duration
	openedAt      time.Time
}

type gateBucket struct {
	ts      time.Time
	ok      int
	err     int
}

// GateConfig 指定速率(rps)与熔断窗口参数。minGap 作为硬下限,
// 用于豆瓣等明确要求间隔的源。
type GateConfig struct {
	RPS         float64
	Burst       float64
	MinGap      time.Duration
	WindowSpan  time.Duration
	BucketSpan  time.Duration
	MinSamples  int
	ErrorRate   float64
	Cooldown    time.Duration
}

// NewGate 构造一个 Gate;零值或不合理参数走合理默认。
func NewGate(cfg GateConfig) *Gate {
	if cfg.RPS <= 0 {
		cfg.RPS = 1
	}
	if cfg.Burst <= 0 {
		cfg.Burst = cfg.RPS
	}
	if cfg.WindowSpan <= 0 {
		cfg.WindowSpan = 5 * time.Minute
	}
	if cfg.BucketSpan <= 0 {
		cfg.BucketSpan = 30 * time.Second
	}
	if cfg.MinSamples <= 0 {
		cfg.MinSamples = 20
	}
	if cfg.ErrorRate <= 0 || cfg.ErrorRate > 1 {
		cfg.ErrorRate = 0.5
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = 10 * time.Minute
	}
	buckets := int(cfg.WindowSpan/cfg.BucketSpan) + 1
	return &Gate{
		rps:           cfg.RPS,
		burst:         cfg.Burst,
		tokens:        cfg.Burst,
		lastFill:      time.Now(),
		minGap:        cfg.MinGap,
		windowBuckets: make([]gateBucket, 0, buckets),
		bucketSpan:    cfg.BucketSpan,
		windowSpan:    cfg.WindowSpan,
		minSamples:    cfg.MinSamples,
		errorRate:     cfg.ErrorRate,
		cooldown:      cfg.Cooldown,
	}
}

// Wait 阻塞直到有可用 token。熔断开启时立即返回 ErrBreakerOpen。
// 对 ctx 取消敏感。
func (g *Gate) Wait(ctx context.Context) error {
	if g == nil {
		return nil
	}
	if g.isOpen() {
		return ErrBreakerOpen
	}
	for {
		wait := g.reserve()
		if wait <= 0 {
			return nil
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// Observe 记录一次调用结果,用于熔断判定。
func (g *Gate) Observe(success bool) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	g.trimLocked(now)
	if len(g.windowBuckets) == 0 || now.Sub(g.windowBuckets[len(g.windowBuckets)-1].ts) >= g.bucketSpan {
		g.windowBuckets = append(g.windowBuckets, gateBucket{ts: now})
	}
	b := &g.windowBuckets[len(g.windowBuckets)-1]
	if success {
		b.ok++
	} else {
		b.err++
	}
	g.evaluateLocked(now)
}

func (g *Gate) isOpen() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.openedAt.IsZero() {
		return false
	}
	if time.Since(g.openedAt) >= g.cooldown {
		g.openedAt = time.Time{}
		g.windowBuckets = g.windowBuckets[:0]
		return false
	}
	return true
}

func (g *Gate) reserve() time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	// token bucket 补充
	elapsed := now.Sub(g.lastFill).Seconds()
	if elapsed > 0 {
		g.tokens += elapsed * g.rps
		if g.tokens > g.burst {
			g.tokens = g.burst
		}
		g.lastFill = now
	}
	// 最小间隔
	if g.minGap > 0 && !g.lastCall.IsZero() {
		gap := now.Sub(g.lastCall)
		if gap < g.minGap {
			return g.minGap - gap
		}
	}
	if g.tokens < 1 {
		need := (1 - g.tokens) / g.rps
		return time.Duration(need * float64(time.Second))
	}
	g.tokens -= 1
	g.lastCall = now
	return 0
}

func (g *Gate) trimLocked(now time.Time) {
	cutoff := now.Add(-g.windowSpan)
	i := 0
	for i < len(g.windowBuckets) && g.windowBuckets[i].ts.Before(cutoff) {
		i++
	}
	if i > 0 {
		g.windowBuckets = g.windowBuckets[i:]
	}
}

func (g *Gate) evaluateLocked(now time.Time) {
	total, errs := 0, 0
	for _, b := range g.windowBuckets {
		total += b.ok + b.err
		errs += b.err
	}
	if total < g.minSamples {
		return
	}
	rate := float64(errs) / float64(total)
	if rate >= g.errorRate {
		g.openedAt = now
	}
}
