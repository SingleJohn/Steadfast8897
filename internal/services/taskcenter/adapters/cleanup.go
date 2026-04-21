package adapters

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/models"
	"fyms/internal/services/taskcenter"
)

// CleanupAdapter 聚合所有"媒体库软删除后的后台清理"为单个 taskcenter.Task。
//
// 外部通过 Enqueue(libraryID, name) 触发：立即启动一个 goroutine 分批 DELETE
// 该库的 items，完成后 DELETE libraries 行本身。每个库一个 run，多个库可并行。
//
// Snapshot 聚合规则和 ScanAdapter 类似：
//   - Children 按库一个子 Snapshot。
//   - Status：任一 running → Running；否则任一最近失败 → Failed；否则最近全成功 → Succeeded；空 → Idle。
//   - 已终止的子 run 保留 retainFinished 时长，便于前端 SSE 捕获 succeeded 事件后再清理视图。
//
// Task.Start() 不支持——清理由 delete library 路由直接触发。
type CleanupAdapter struct {
	DB *pgxpool.Pool

	mu   sync.Mutex
	runs map[uuid.UUID]*cleanupRun
}

type cleanupRun struct {
	libraryID   uuid.UUID
	libraryName string
	runID       int64
	status      taskcenter.Status
	total       int64
	processed   int64
	startedAt   int64
	completedAt int64
	errMsg      string
	message     string
}

var ErrCleanupStartNotSupported = errors.New("cleanup task is triggered by library delete; task center start not supported")

// retainFinished 是终止状态的 child 保留时长。略长于 SSE broadcaster 节流周期
// (1s) 的 60 倍,保证前端 toast 不会因为切页/断连而漏掉。
const retainFinished = 60 * time.Second

// batchSize 每轮 DELETE 限制的行数。PG 设定下 500 通常 20~80ms 一轮,WAL 压力小。
const batchSize = 500

// batchSleep 两轮之间的喘息,给其他事务腾空间;过大会拉长总时长。
const batchSleep = 50 * time.Millisecond

func NewCleanupAdapter(db *pgxpool.Pool) *CleanupAdapter {
	return &CleanupAdapter{
		DB:   db,
		runs: make(map[uuid.UUID]*cleanupRun),
	}
}

func (a *CleanupAdapter) Kind() taskcenter.Kind { return taskcenter.KindCleanup }

// Enqueue 启动一个库的后台清理。调用方应在 MarkLibraryDeleted 成功后调用。
// 重复入队同一 libraryID 会复用已在跑的 run，不重复启动。
func (a *CleanupAdapter) Enqueue(libraryID uuid.UUID, name string) {
	a.mu.Lock()
	if existing, ok := a.runs[libraryID]; ok {
		if existing.status.Running() {
			a.mu.Unlock()
			return
		}
		// 之前跑失败过，删掉旧记录重新跑。
		delete(a.runs, libraryID)
	}
	run := &cleanupRun{
		libraryID:   libraryID,
		libraryName: name,
		status:      taskcenter.StatusRunning,
		startedAt:   time.Now().UnixMilli(),
	}
	a.runs[libraryID] = run
	a.mu.Unlock()

	go a.execute(run)
}

// ResumeAfterRestart 启动时接管上次进程遗留的待清理库。
// 调用方从 models.ListDeletedLibraryIDs 拿到 id 列表后传入。
func (a *CleanupAdapter) ResumeAfterRestart(ctx context.Context) {
	ids, err := models.ListDeletedLibraryIDs(ctx, a.DB)
	if err != nil {
		slog.Warn("cleanup: list pending on startup failed", "error", err)
		return
	}
	for _, id := range ids {
		name, _ := models.GetLibraryNameIncludingDeleted(ctx, a.DB, id)
		slog.Info("cleanup: resume pending library", "libraryId", id, "name", name)
		a.Enqueue(id, name)
	}
}

func (a *CleanupAdapter) execute(run *cleanupRun) {
	ctx := context.Background()

	total, err := models.CountLibraryItems(ctx, a.DB, run.libraryID)
	if err != nil {
		a.finish(run, taskcenter.StatusFailed, "", "count items: "+err.Error())
		return
	}
	a.mu.Lock()
	run.total = total
	a.mu.Unlock()

	runID, err := taskcenter.Begin(ctx, a.DB, taskcenter.BeginParams{
		Kind:    taskcenter.KindCleanup,
		Trigger: taskcenter.TriggerManual,
		Total:   total,
		Payload: map[string]any{
			"libraryId":   run.libraryID.String(),
			"libraryName": run.libraryName,
		},
	})
	if err != nil {
		slog.Warn("cleanup: begin run failed; proceeding without history row",
			"libraryId", run.libraryID, "error", err)
	}
	a.mu.Lock()
	run.runID = runID
	a.mu.Unlock()

	var lastProgressPush time.Time
	for {
		n, err := models.DeleteLibraryItemsBatch(ctx, a.DB, run.libraryID, batchSize)
		if err != nil {
			a.finish(run, taskcenter.StatusFailed, "", "delete items batch: "+err.Error())
			return
		}
		if n == 0 {
			break
		}
		a.mu.Lock()
		run.processed += n
		processed := run.processed
		a.mu.Unlock()

		// 节流写 task_runs.progress,避免每批一次 UPDATE。
		if runID > 0 && time.Since(lastProgressPush) > time.Second {
			_ = taskcenter.UpdateProgress(ctx, a.DB, runID, processed, processed, 0, total, nil)
			lastProgressPush = time.Now()
		}
		time.Sleep(batchSleep)
	}

	if err := models.FinalizeLibraryDeletion(ctx, a.DB, run.libraryID); err != nil {
		a.finish(run, taskcenter.StatusFailed, "", "finalize: "+err.Error())
		return
	}
	a.finish(run, taskcenter.StatusSucceeded,
		fmt.Sprintf("cleaned %d items", run.processed), "")
}

func (a *CleanupAdapter) finish(run *cleanupRun, status taskcenter.Status, message, errMsg string) {
	ctx := context.Background()
	a.mu.Lock()
	run.status = status
	run.completedAt = time.Now().UnixMilli()
	run.message = message
	run.errMsg = errMsg
	processed := run.processed
	total := run.total
	runID := run.runID
	a.mu.Unlock()

	if runID > 0 {
		_ = taskcenter.UpdateProgress(ctx, a.DB, runID, processed, processed, 0, total, nil)
		_ = taskcenter.End(ctx, a.DB, runID, status, message, errMsg)
	}

	if status == taskcenter.StatusFailed {
		slog.Warn("cleanup: library cleanup failed",
			"libraryId", run.libraryID, "name", run.libraryName, "error", errMsg)
	} else {
		slog.Info("cleanup: library cleanup succeeded",
			"libraryId", run.libraryID, "name", run.libraryName, "items", processed)
	}
}

// Snapshot 返回聚合快照。同步做终止 run 的过期清理(已终止 > retainFinished)。
func (a *CleanupAdapter) Snapshot() taskcenter.Snapshot {
	now := time.Now().UnixMilli()
	cutoff := now - retainFinished.Milliseconds()

	a.mu.Lock()
	// 过期清理
	for id, r := range a.runs {
		if r.status.Terminal() && r.completedAt > 0 && r.completedAt < cutoff {
			delete(a.runs, id)
		}
	}

	agg := taskcenter.Snapshot{
		Kind:        taskcenter.KindCleanup,
		Status:      taskcenter.StatusIdle,
		Cancellable: false,
	}
	if len(a.runs) == 0 {
		a.mu.Unlock()
		return agg
	}

	children := make([]taskcenter.Snapshot, 0, len(a.runs))
	var running, failed, succeeded int
	var earliestStart, latestComplete int64
	for _, r := range a.runs {
		child := taskcenter.Snapshot{
			Kind:        taskcenter.KindCleanup,
			RunID:       r.runID,
			Status:      r.status,
			Phase:       r.libraryName,
			Message:     fmt.Sprintf("library=%s", r.libraryID),
			Total:       r.total,
			Processed:   r.processed,
			Percent:     pctFromCount(r.processed, r.total),
			Current:     r.libraryName,
			Error:       r.errMsg,
			StartedAt:   r.startedAt,
			CompletedAt: r.completedAt,
		}
		if earliestStart == 0 || r.startedAt < earliestStart {
			earliestStart = r.startedAt
		}
		if r.completedAt > latestComplete {
			latestComplete = r.completedAt
		}
		switch r.status {
		case taskcenter.StatusRunning, taskcenter.StatusQueued, taskcenter.StatusStopping:
			running++
		case taskcenter.StatusFailed:
			failed++
		case taskcenter.StatusSucceeded:
			succeeded++
		}
		agg.Total += r.total
		agg.Processed += r.processed
		children = append(children, child)
	}
	a.mu.Unlock()

	agg.Children = children
	agg.StartedAt = earliestStart
	agg.Percent = pctFromCount(agg.Processed, agg.Total)
	agg.Message = fmt.Sprintf("%d running / %d done / %d failed", running, succeeded, failed)

	switch {
	case running > 0:
		agg.Status = taskcenter.StatusRunning
	case failed > 0 && succeeded == 0:
		agg.Status = taskcenter.StatusFailed
		agg.CompletedAt = latestComplete
	case succeeded > 0:
		agg.Status = taskcenter.StatusSucceeded
		agg.CompletedAt = latestComplete
	}
	return agg
}

func (a *CleanupAdapter) Start(_ context.Context, _ taskcenter.StartParams, _ taskcenter.Trigger) (int64, error) {
	return 0, ErrCleanupStartNotSupported
}

func (a *CleanupAdapter) Stop() error { return nil }
