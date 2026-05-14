package adapters

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/services"
	"fyms/internal/services/taskcenter"
)

// ProbeAdapter 包装 ffprobe 批量探测任务。
//
// 启动参数：params["threads"] int；未传或 0 时由 ProbeTask.Start 使用默认值。
// ProbeTask 用户主动 Stop 后内部状态回到 "idle"，tracker 靠 stopRequested flag
// 正确映射到 cancelled。
type ProbeAdapter struct {
	Task *services.ProbeTask
	DB   *pgxpool.Pool

	mu            sync.Mutex
	currentRunID  int64
	stopRequested atomic.Bool
}

func NewProbeAdapter(t *services.ProbeTask, db *pgxpool.Pool) *ProbeAdapter {
	return &ProbeAdapter{Task: t, DB: db}
}

func (a *ProbeAdapter) Kind() taskcenter.Kind { return taskcenter.KindProbe }

func (a *ProbeAdapter) Snapshot() taskcenter.Snapshot {
	p := a.Task.GetProgress()
	status := mapLegacyStatus(p.Status)

	// 与 handlers/library.go::buildEffectiveProbeProgress 行为对齐:
	// 任务非运行态时,从 DB 实时计算 missing / versionsTotal。
	// 否则 idle 状态下 counters 永远是 0,前端探测按钮的 disabled 判定
	// (missingCount === 0 && status === 'idle') 永远为真,导致无法点击。
	if status != taskcenter.StatusRunning && status != taskcenter.StatusStopping && a.DB != nil {
		ctx := context.Background()
		if cnt, err := services.GetMissingMediainfoCount(ctx, a.DB); err == nil {
			p.MissingCount = cnt
		}
		if total, err := services.GetTotalMediaVersionsCount(ctx, a.DB); err == nil {
			p.VersionsTotal = total
		}
	}

	snap := taskcenter.Snapshot{
		Kind:        taskcenter.KindProbe,
		Status:      status,
		Total:       p.TotalItems,
		Processed:   p.ProcessedItems,
		Success:     p.SuccessItems,
		Failed:      p.FailedItems,
		Percent:     p.Percentage,
		Current:     deref(p.CurrentItem),
		Error:       deref(p.Error),
		Cancellable: status == taskcenter.StatusRunning,
		Counters: map[string]int64{
			"threads":       int64(p.Threads),
			"missing":       p.MissingCount,
			"versionsTotal": p.VersionsTotal,
		},
	}
	a.mu.Lock()
	snap.RunID = a.currentRunID
	a.mu.Unlock()
	return snap
}

func (a *ProbeAdapter) Start(ctx context.Context, params taskcenter.StartParams, trigger taskcenter.Trigger) (int64, error) {
	a.mu.Lock()
	if a.currentRunID != 0 {
		rid := a.currentRunID
		a.mu.Unlock()
		return rid, nil
	}
	a.mu.Unlock()

	threads := paramInt(params, "threads", 0)
	if err := a.Task.Start(a.DB, threads); err != nil {
		return 0, err
	}

	runID, err := taskcenter.Begin(ctx, a.DB, taskcenter.BeginParams{
		Kind:    taskcenter.KindProbe,
		Trigger: trigger,
		Payload: map[string]any{"threads": threads},
	})
	if err != nil {
		slog.Warn("probe run begin failed; task running but untracked", "error", err)
		return 0, nil
	}

	a.mu.Lock()
	a.currentRunID = runID
	a.mu.Unlock()
	a.stopRequested.Store(false)

	go (&singleRunTracker{
		db:            a.DB,
		runID:         runID,
		snapshot:      a.Snapshot,
		stopRequested: &a.stopRequested,
		onDone: func() {
			a.mu.Lock()
			a.currentRunID = 0
			a.mu.Unlock()
		},
	}).run()

	return runID, nil
}

func (a *ProbeAdapter) Stop() error {
	a.stopRequested.Store(true)
	a.Task.Stop()
	return nil
}

// paramInt 从 StartParams 解析整数字段。JSON 反序列化后数字默认是 float64，
// 同时兼容直接传 int 的内部调用（chain 触发）。
func paramInt(p taskcenter.StartParams, key string, def int) int {
	if p == nil {
		return def
	}
	v, ok := p[key]
	if !ok {
		return def
	}
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return def
}
