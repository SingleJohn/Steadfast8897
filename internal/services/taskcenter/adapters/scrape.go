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

// ScrapeAdapter 包装 TMDB 元数据刮削任务（services.ScrapeTask）。
//
// M2 起 Start 会把 run 写入 task_runs：Begin → 启动监视 goroutine → 终态时 End。
// 若已在运行，返回 currentRunID（幂等），不重复建 run。
type ScrapeAdapter struct {
	Task *services.ScrapeTask
	DB   *pgxpool.Pool

	mu            sync.Mutex
	currentRunID  int64
	stopRequested atomic.Bool
}

func NewScrapeAdapter(t *services.ScrapeTask, db *pgxpool.Pool) *ScrapeAdapter {
	return &ScrapeAdapter{Task: t, DB: db}
}

func (a *ScrapeAdapter) Kind() taskcenter.Kind { return taskcenter.KindScrape }

func (a *ScrapeAdapter) Snapshot() taskcenter.Snapshot {
	p := a.Task.GetProgress()
	status := mapLegacyStatus(p.Status)
	snap := taskcenter.Snapshot{
		Kind:        taskcenter.KindScrape,
		Status:      status,
		Total:       p.TotalItems,
		Processed:   p.ProcessedItems,
		Success:     p.SuccessItems,
		Failed:      p.FailedItems,
		Percent:     p.Percentage,
		Current:     deref(p.CurrentItem),
		Error:       deref(p.LastError),
		Cancellable: status == taskcenter.StatusRunning,
		Counters: map[string]int64{
			"missing":    p.MissingCount,
			"itemsTotal": p.ItemsTotal,
		},
	}
	a.mu.Lock()
	snap.RunID = a.currentRunID
	a.mu.Unlock()
	return snap
}

func (a *ScrapeAdapter) Start(ctx context.Context, _ taskcenter.StartParams, trigger taskcenter.Trigger) (int64, error) {
	// 幂等：已在跑就返回当前 runID，不新建 run。
	a.mu.Lock()
	if a.currentRunID != 0 {
		rid := a.currentRunID
		a.mu.Unlock()
		return rid, nil
	}
	a.mu.Unlock()

	if err := a.Task.Start(ctx, a.DB); err != nil {
		return 0, err
	}

	runID, err := taskcenter.Begin(ctx, a.DB, taskcenter.BeginParams{
		Kind:    taskcenter.KindScrape,
		Trigger: trigger,
	})
	if err != nil {
		// 任务已启动但 DB 记录失败：不阻塞业务，只记 warn。
		slog.Warn("scrape run begin failed; task running but untracked", "error", err)
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

func (a *ScrapeAdapter) Stop() error {
	a.stopRequested.Store(true)
	a.Task.Stop()
	return nil
}
