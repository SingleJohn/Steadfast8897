package adapters

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/services/taskcenter"
)

// trackerTick 是单 run tracker 的轮询周期。
// 比前端 SSE 节流稍密，避免结束事件被拖长；对 DB 压力可忽略（单条 UPDATE）。
const trackerTick = time.Second

// singleRunTracker 监视一个单 run 任务（Scrape/Probe/Update.apply）的终态，
// 并在终态出现时回写 processed/success/failed + 最终 status。
//
// 语义要点：
//   - 必须先见过一次 Running / Stopping 才允许认定终态，避免启动前的 idle 瞬态误判。
//   - idle 终态按 stopRequested flag 映射：true → cancelled，false → succeeded。
//   - 每轮如果 processed 有变化，节流写一次 UpdateProgress，让历史表最终一致。
type singleRunTracker struct {
	db            *pgxpool.Pool
	runID         int64
	snapshot      func() taskcenter.Snapshot
	stopRequested *atomic.Bool
	onDone        func() // 给适配器重置 currentRunID 用
}

func (t *singleRunTracker) run() {
	ctx := context.Background()
	ticker := time.NewTicker(trackerTick)
	defer ticker.Stop()

	var seenRunning bool
	var lastProcessed int64 = -1

	for range ticker.C {
		s := t.snapshot()

		if s.Status.Running() {
			seenRunning = true
			if s.Processed != lastProcessed {
				_ = taskcenter.UpdateProgress(ctx, t.db, t.runID,
					s.Processed, s.Success, s.Failed, s.Total, s.Counters)
				lastProcessed = s.Processed
			}
			continue
		}

		// 未见过 running 前的 idle 可能只是启动尚未触达的瞬态；继续等待。
		if !seenRunning {
			continue
		}

		final := s.Status
		if final == taskcenter.StatusIdle {
			if t.stopRequested.Load() {
				final = taskcenter.StatusCancelled
			} else {
				final = taskcenter.StatusSucceeded
			}
		}

		if err := taskcenter.UpdateProgress(ctx, t.db, t.runID,
			s.Processed, s.Success, s.Failed, s.Total, s.Counters); err != nil {
			slog.Debug("tracker UpdateProgress on end", "runId", t.runID, "error", err)
		}
		if err := taskcenter.End(ctx, t.db, t.runID, final, s.Message, s.Error); err != nil {
			slog.Warn("tracker End failed", "runId", t.runID, "status", final, "error", err)
		}

		if t.onDone != nil {
			t.onDone()
		}
		return
	}
}
