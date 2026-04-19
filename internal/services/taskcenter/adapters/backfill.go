package adapters

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/services"
	"fyms/internal/services/taskcenter"
)

// BackfillAdapter 包装三阶段串行回填（quality / name / image）。
//
// 一次 Start 在 task_runs 里建：
//   - 1 条父 run（kind=backfill, stage=NULL, status=running）
//   - N 条子 run（按实际 stages 长度，stage=quality/name/image，status=queued）
//
// trackBackfill goroutine 监视 Snapshot.Stage 的推进，在 stage 切换时
// 把上一个子 run 标 succeeded、把当前子 run 从 queued 推进到 running；
// 终态出现时关闭最后一个 stage 子 run 与父 run，剩余未执行的子 run 标 cancelled。
type BackfillAdapter struct {
	Task *services.BackfillTask
	DB   *pgxpool.Pool

	mu              sync.Mutex
	currentParentID int64
	childRunIDs     map[services.BackfillStage]int64
	stages          []services.BackfillStage
	stopRequested   atomic.Bool
}

func NewBackfillAdapter(t *services.BackfillTask, db *pgxpool.Pool) *BackfillAdapter {
	return &BackfillAdapter{Task: t, DB: db}
}

func (a *BackfillAdapter) Kind() taskcenter.Kind { return taskcenter.KindBackfill }

func (a *BackfillAdapter) Snapshot() taskcenter.Snapshot {
	p := a.Task.GetProgress()
	status := mapLegacyStatus(p.Status)
	snap := taskcenter.Snapshot{
		Kind:        taskcenter.KindBackfill,
		Status:      status,
		Stage:       string(p.Stage),
		Total:       p.Total,
		Processed:   p.Processed,
		Percent:     pctFromCount(p.Processed, p.Total),
		Error:       p.LastError,
		Counters:    copyCounters(p.Counters),
		Cancellable: status == taskcenter.StatusRunning,
	}
	if p.StartedAt != nil {
		snap.StartedAt = p.StartedAt.UnixMilli()
	}
	if p.CompletedAt != nil {
		snap.CompletedAt = p.CompletedAt.UnixMilli()
	}
	a.mu.Lock()
	snap.RunID = a.currentParentID
	a.mu.Unlock()
	return snap
}

func (a *BackfillAdapter) Start(ctx context.Context, params taskcenter.StartParams, trigger taskcenter.Trigger) (int64, error) {
	a.mu.Lock()
	if a.currentParentID != 0 {
		pid := a.currentParentID
		a.mu.Unlock()
		return pid, nil
	}
	a.mu.Unlock()

	stages := parseBackfillStages(params)
	if len(stages) == 0 {
		stages = services.DefaultBackfillStages
	}

	if err := a.Task.Start(ctx, a.DB, stages); err != nil {
		return 0, err
	}

	parentID, err := taskcenter.Begin(ctx, a.DB, taskcenter.BeginParams{
		Kind:    taskcenter.KindBackfill,
		Trigger: trigger,
		Payload: map[string]any{"stages": stagesAsStrings(stages)},
	})
	if err != nil {
		slog.Warn("backfill run begin failed; task running but untracked", "error", err)
		return 0, nil
	}

	children := make(map[services.BackfillStage]int64, len(stages))
	for _, st := range stages {
		id, qErr := taskcenter.BeginQueued(ctx, a.DB, taskcenter.BeginParams{
			Kind:     taskcenter.KindBackfill,
			Stage:    string(st),
			ParentID: parentID,
			Trigger:  trigger,
		})
		if qErr != nil {
			slog.Warn("backfill child run begin failed", "stage", st, "error", qErr)
			continue
		}
		children[st] = id
	}

	a.mu.Lock()
	a.currentParentID = parentID
	a.childRunIDs = children
	a.stages = stages
	a.mu.Unlock()
	a.stopRequested.Store(false)

	go a.trackBackfill()
	return parentID, nil
}

func (a *BackfillAdapter) Stop() error {
	a.stopRequested.Store(true)
	a.Task.Stop()
	return nil
}

// trackBackfill 监视原任务状态推进子 run 与父 run。
func (a *BackfillAdapter) trackBackfill() {
	ctx := context.Background()
	ticker := time.NewTicker(trackerTick)
	defer ticker.Stop()

	var (
		seenRunning  bool
		prevStage    services.BackfillStage
		prevSnapshot taskcenter.Snapshot
	)

	// finalizeChild 以给定 status 结束一个 stage 子 run 并移出 map。
	// progressSnap 不为 nil 时同步回写最终进度到 task_runs。
	finalizeChild := func(stage services.BackfillStage, status taskcenter.Status, progressSnap *taskcenter.Snapshot) {
		a.mu.Lock()
		id, ok := a.childRunIDs[stage]
		if ok {
			delete(a.childRunIDs, stage)
		}
		a.mu.Unlock()
		if !ok {
			return
		}
		if progressSnap != nil {
			_ = taskcenter.UpdateProgress(ctx, a.DB, id,
				progressSnap.Processed, progressSnap.Success, progressSnap.Failed,
				progressSnap.Total, progressSnap.Counters)
		}
		errMsg := ""
		if progressSnap != nil {
			errMsg = progressSnap.Error
		}
		_ = taskcenter.End(ctx, a.DB, id, status, "", errMsg)
	}

	for range ticker.C {
		s := a.Snapshot()
		cur := services.BackfillStage(s.Stage)

		if s.Status.Running() {
			seenRunning = true

			if prevStage != cur {
				// 前一 stage 顺利结束（能切换到新 stage 说明 quality/name 跑完了）。
				if prevStage != "" {
					snap := prevSnapshot
					finalizeChild(prevStage, taskcenter.StatusSucceeded, &snap)
				}
				if cur != "" {
					a.mu.Lock()
					id, ok := a.childRunIDs[cur]
					a.mu.Unlock()
					if ok {
						_ = taskcenter.MarkRunning(ctx, a.DB, id)
					}
				}
				prevStage = cur
			}

			// 中途进度回写（只更当前 stage 的子 run）。
			if cur != "" {
				a.mu.Lock()
				id, ok := a.childRunIDs[cur]
				a.mu.Unlock()
				if ok {
					_ = taskcenter.UpdateProgress(ctx, a.DB, id,
						s.Processed, s.Success, s.Failed, s.Total, s.Counters)
				}
			}

			prevSnapshot = s
			continue
		}

		if !seenRunning {
			continue
		}

		// 进入终态。映射 idle → stopRequested ? cancelled : succeeded。
		final := s.Status
		if final == taskcenter.StatusIdle {
			if a.stopRequested.Load() {
				final = taskcenter.StatusCancelled
			} else {
				final = taskcenter.StatusSucceeded
			}
		}

		// 关闭最后一个处于 running 的 stage 子 run。
		last := cur
		if last == "" {
			last = prevStage
		}
		if last != "" {
			finalizeChild(last, final, &s)
		}

		// 剩余未执行的 queued 子 run：按前置结果标记 cancelled。
		a.mu.Lock()
		remaining := a.childRunIDs
		a.childRunIDs = nil
		parentID := a.currentParentID
		a.currentParentID = 0
		a.stages = nil
		a.mu.Unlock()

		reason := "skipped"
		switch final {
		case taskcenter.StatusFailed:
			reason = "previous stage failed"
		case taskcenter.StatusCancelled:
			reason = "cancelled by user"
		}
		for _, id := range remaining {
			_ = taskcenter.End(ctx, a.DB, id, taskcenter.StatusCancelled, reason, "")
		}

		_ = taskcenter.End(ctx, a.DB, parentID, final, "", s.Error)
		return
	}
}

// parseBackfillStages 从 StartParams["stages"] 解析 []string → []BackfillStage。
func parseBackfillStages(p taskcenter.StartParams) []services.BackfillStage {
	if p == nil {
		return nil
	}
	raw, ok := p["stages"]
	if !ok {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]services.BackfillStage, 0, len(list))
	for _, v := range list {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, services.BackfillStage(s))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stagesAsStrings(stages []services.BackfillStage) []string {
	out := make([]string, len(stages))
	for i, s := range stages {
		out[i] = string(s)
	}
	return out
}
