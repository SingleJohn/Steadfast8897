package taskcenter

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Registry 是全局任务单例表。进程启动时由 main.go 创建一次，
// 各适配器（ScanAdapter 等）通过 Register 注册自身。
//
// 同时作为 SSE 的发布总线：Subscribe 返回一个 channel，
// 适配器在 Snapshot 变化时调用 Publish，订阅者全部收到同一份快照。
// 订阅者消费速率跟不上时，该订阅者会被丢一条（非阻塞），不影响其他订阅者。
type Registry struct {
	mu    sync.RWMutex
	tasks map[Kind]Task

	pubMu   sync.Mutex
	subs    map[int64]chan Snapshot
	nextSub atomic.Int64
}

func NewRegistry() *Registry {
	return &Registry{
		tasks: make(map[Kind]Task),
		subs:  make(map[int64]chan Snapshot),
	}
}

// Register 注册一个任务适配器。同 Kind 重复注册会覆盖（便于测试）。
func (r *Registry) Register(t Task) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks[t.Kind()] = t
}

// Get 根据 Kind 查任务；不存在返回 nil。
func (r *Registry) Get(kind Kind) Task {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tasks[kind]
}

// Kinds 返回所有已注册的 Kind（顺序不保证）。
func (r *Registry) Kinds() []Kind {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Kind, 0, len(r.tasks))
	for k := range r.tasks {
		out = append(out, k)
	}
	return out
}

// SnapshotAll 返回所有任务的当前快照。顺序按 Kind 常量定义的稳定顺序，
// 让前端 /Tasks 列表展示位置不跳动。
func (r *Registry) SnapshotAll() []Snapshot {
	order := []Kind{KindScan, KindScrape, KindProbe, KindBackfill, KindUpdate, KindCleanup}
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Snapshot, 0, len(order))
	for _, k := range order {
		if t, ok := r.tasks[k]; ok {
			out = append(out, t.Snapshot())
		}
	}
	// 后注册的、不在标准顺序里的 Kind 追加在末尾。
	for k, t := range r.tasks {
		if !slices.Contains(order, k) {
			out = append(out, t.Snapshot())
		}
	}
	return out
}

// Publish 向所有订阅者广播一份 Snapshot 副本。非阻塞：channel 满则丢弃。
// 适配器应在内存状态变化后调用（建议节流，避免每条进度都推）。
func (r *Registry) Publish(s Snapshot) {
	r.pubMu.Lock()
	defer r.pubMu.Unlock()
	for _, ch := range r.subs {
		select {
		case ch <- s:
		default:
			// 订阅者处理不过来就丢本条，后续快照会自动补齐最新态。
		}
	}
}

// Subscribe 返回一个订阅 channel 和一个取消函数。
// 调用 cancel 后 channel 会被关闭，不要再读取。
// 订阅者通常由 SSE handler 调用，页面关闭时 defer cancel()。
func (r *Registry) Subscribe() (<-chan Snapshot, func()) {
	id := r.nextSub.Add(1)
	ch := make(chan Snapshot, 64)

	r.pubMu.Lock()
	r.subs[id] = ch
	r.pubMu.Unlock()

	cancel := func() {
		r.pubMu.Lock()
		if c, ok := r.subs[id]; ok {
			delete(r.subs, id)
			close(c)
		}
		r.pubMu.Unlock()
	}
	return ch, cancel
}

// StartBroadcaster 每 interval 扫描一次 SnapshotAll，与上一轮比较，
// 对发生关键字段变化的任务调用 Publish。进程生命期只启动一次。
//
// 选择"轮询 + 差异判定"而不是"每次状态变化即时推送"：
//   - 无需侵入各适配器或 tracker 代码
//   - 差异判定只覆盖关键字段，频繁的 current 文本变化不会打满 SSE
//   - 1s 粒度对用户观感足够，且已与 tracker 周期对齐
func (r *Registry) StartBroadcaster(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		prev := map[Kind]Snapshot{}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for _, s := range r.SnapshotAll() {
					old, hadPrev := prev[s.Kind]
					if !hadPrev || !snapshotEqual(old, s) {
						r.Publish(s)
						prev[s.Kind] = s
					}
				}
			}
		}
	}()
}

// snapshotEqual 比较两次快照的"值得推送"字段。Current（当前处理项名称）
// 变化频率高，也纳入比较以保证进度流畅；Counters 的局部变化不单独推送，
// Processed 变化必然触发 Publish，Counters 会随其一起带出去。
func snapshotEqual(a, b Snapshot) bool {
	return a.Status == b.Status &&
		a.Stage == b.Stage &&
		a.Phase == b.Phase &&
		a.Total == b.Total &&
		a.Processed == b.Processed &&
		a.Success == b.Success &&
		a.Failed == b.Failed &&
		a.Percent == b.Percent &&
		a.Current == b.Current &&
		a.Error == b.Error &&
		a.RunID == b.RunID
}
