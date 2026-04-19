// Package sysmetrics 周期采样 CPU / 内存 / 本进程网络出口，并通过订阅机制把最新样本广播给 SSE。
//
// 设计要点：
//   - 采样由单 goroutine + time.Ticker 驱动；无订阅者也持续采样，保证 Overview 打开时立即有历史。
//   - CPU / 内存使用 gopsutil，容器内会自动读 cgroup；额外再探测 cgroup 限额覆盖展示口径。
//   - 网络出口不取 OS 网卡计数，而是：
//       1) Direct：gin 中间件累加 c.Writer.Size()，在每次 tick 算差分得 bytes/s
//       2) Redirect：由外部注入的 RedirectEstimator 返回活跃会话码率合计（302 转发估算）
//   - 历史 ring buffer 固定 60 个样本（2 分钟）。
package sysmetrics

import (
	"context"
	"encoding/json"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

const historySize = 60

// RedirectEstimator 返回当前 302 转发的总码率估算（bytes/s）与活跃会话数。
type RedirectEstimator func() (bitrateBps uint64, activeSessions int)

type Collector struct {
	interval  time.Duration
	estimator RedirectEstimator

	mu      sync.RWMutex
	history []Snapshot
	subs    map[chan Snapshot]struct{}

	txBytes  atomic.Uint64 // gin 中间件累计写出字节
	lastTx   uint64
	lastTick time.Time

	env      string
	cores    int
	memLimit uint64  // 0 表示无限制
	cpuLimit float64 // 0 表示无限制

	stopCh chan struct{}
	once   sync.Once
}

// NewCollector 创建采集器；interval<=0 时默认 2s，estimator 为 nil 时走空实现。
func NewCollector(interval time.Duration, estimator RedirectEstimator) *Collector {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if estimator == nil {
		estimator = func() (uint64, int) { return 0, 0 }
	}
	c := &Collector{
		interval:  interval,
		estimator: estimator,
		history:   make([]Snapshot, 0, historySize),
		subs:      make(map[chan Snapshot]struct{}),
		stopCh:    make(chan struct{}),
	}
	c.env = detectEnv()
	c.cores = runtime.NumCPU()
	c.memLimit = detectMemLimit()
	c.cpuLimit = detectCPUQuota()
	if c.cpuLimit > 0 {
		// quota 向上取整作为展示核数：--cpus=1.5 → 2
		rounded := int(c.cpuLimit + 0.999)
		if rounded > 0 && rounded < c.cores {
			c.cores = rounded
		}
	}
	return c
}

// Start 启动后台采样 goroutine。ctx 用于外部关停。
func (c *Collector) Start(ctx context.Context) {
	// 预热一次 CPU 读取（gopsutil 首次调用返回 0）
	_, _ = cpu.Percent(0, false)
	go c.loop(ctx)
}

// Stop 停止采样。可多次调用。
func (c *Collector) Stop() {
	c.once.Do(func() { close(c.stopCh) })
}

func (c *Collector) loop(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case now := <-ticker.C:
			s := c.sample(now)
			c.publish(s)
		}
	}
}

func (c *Collector) sample(now time.Time) Snapshot {
	s := Snapshot{
		Env:       c.env,
		CPUCores:  c.cores,
		Timestamp: now.UnixMilli(),
	}

	if pct, err := cpu.Percent(0, false); err == nil && len(pct) > 0 {
		s.CPUPercent = pct[0]
	}

	if v, err := mem.VirtualMemory(); err == nil {
		s.MemTotal = v.Total
		s.MemUsed = v.Used
		s.MemPercent = v.UsedPercent
	}
	// 容器限额更小时覆盖为限额口径
	if c.memLimit > 0 && (s.MemTotal == 0 || c.memLimit < s.MemTotal) {
		s.MemTotal = c.memLimit
		if s.MemTotal > 0 {
			s.MemPercent = float64(s.MemUsed) / float64(s.MemTotal) * 100
			if s.MemPercent > 100 {
				s.MemPercent = 100
			}
		}
	}

	// Direct 出口：累计值差分
	cur := c.txBytes.Load()
	if !c.lastTick.IsZero() {
		dt := now.Sub(c.lastTick).Seconds()
		if dt > 0 && cur >= c.lastTx {
			s.DirectTxBps = uint64(float64(cur-c.lastTx) / dt)
		}
	}
	c.lastTx = cur
	c.lastTick = now

	// Redirect 估算
	bps, sessions := c.estimator()
	s.RedirectBpsEst = bps
	s.ActiveSessions = sessions

	c.mu.Lock()
	c.history = append(c.history, s)
	if len(c.history) > historySize {
		c.history = c.history[len(c.history)-historySize:]
	}
	c.mu.Unlock()

	return s
}

// publish 非阻塞发送；慢订阅者会丢样本而不是阻塞采集循环。
func (c *Collector) publish(s Snapshot) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for ch := range c.subs {
		select {
		case ch <- s:
		default:
		}
	}
}

// Snapshot 返回最近一个样本（若从未采样过，返回空壳但 env/cores 已填）。
func (c *Collector) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.history) == 0 {
		return Snapshot{
			Env:       c.env,
			CPUCores:  c.cores,
			Timestamp: time.Now().UnixMilli(),
		}
	}
	return c.history[len(c.history)-1]
}

// History 返回历史样本副本。
func (c *Collector) History() []Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Snapshot, len(c.history))
	copy(out, c.history)
	return out
}

// Subscribe 返回一个接收通道与取消函数。取消后 map 中移除，此后不再收到；通道不 close，
// 订阅方按自身 context 退出即可（避免 close 后 publisher 写入 panic）。
func (c *Collector) Subscribe() (<-chan Snapshot, func()) {
	ch := make(chan Snapshot, 4)
	c.mu.Lock()
	c.subs[ch] = struct{}{}
	c.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			c.mu.Lock()
			delete(c.subs, ch)
			c.mu.Unlock()
		})
	}
}

// ByteCountMiddleware 返回一个 gin 中间件，累加每个响应已写入的字节数到 txBytes。
// 必须尽早挂载（在业务 handler 之前）以覆盖全部响应。
func (c *Collector) ByteCountMiddleware() gin.HandlerFunc {
	return func(gc *gin.Context) {
		gc.Next()
		size := gc.Writer.Size()
		if size > 0 {
			c.txBytes.Add(uint64(size))
		}
	}
}

// MarshalHistory 序列化 {current, history}，供 HTTP handler 直接写回。
func (c *Collector) MarshalHistory() ([]byte, error) {
	type resp struct {
		Current Snapshot   `json:"current"`
		History []Snapshot `json:"history"`
	}
	return json.Marshal(resp{Current: c.Snapshot(), History: c.History()})
}
