package source

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"fyms/internal/repository"
)

const (
	sourceRefreshWorkers     = 4
	sourceRefreshClaimEvery  = 5 * time.Second
	sourceRefreshMaintEvery  = 5 * time.Minute
	sourceRefreshTaskTimeout = 5 * time.Minute
	sourceRefreshClaimBatch  = sourceRefreshWorkers
)

// SourceRefreshWorker 消费 source_refresh_queue：并发执行 catalog_fetch / detail_refresh，
// 失败退避重试，启动/运行期清理孤儿 running，定期清理 done。
type SourceRefreshWorker struct {
	queue    *SourceRefreshQueue
	executor *sourceRefreshExecutor
	repo     *repository.SourceRepository
	logger   *slog.Logger
}

func NewSourceRefreshWorker(repo *repository.SourceRepository, client *http.Client, js *JSRuntimeManager, csp *CSPRuntimeManager) *SourceRefreshWorker {
	return &SourceRefreshWorker{
		queue:    NewSourceRefreshQueue(repo),
		executor: &sourceRefreshExecutor{repo: repo, client: client, js: js, csp: csp},
		repo:     repo,
		logger:   SourceLogger("refresh"),
	}
}

func (w *SourceRefreshWorker) Run(ctx context.Context) {
	if n, err := w.repo.ReconcileSourceRefreshOnStartup(ctx); err != nil {
		w.logger.Warn("[SourceRefresh] reconcile on startup failed", "log_target", "refresh", "error", err)
	} else if n > 0 {
		w.logger.Info("[SourceRefresh] reconciled orphan running tasks", "log_target", "refresh", "count", n)
	}

	claimTicker := time.NewTicker(sourceRefreshClaimEvery)
	defer claimTicker.Stop()
	maintTicker := time.NewTicker(sourceRefreshMaintEvery)
	defer maintTicker.Stop()

	w.drain(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-claimTicker.C:
			w.drain(ctx)
		case <-maintTicker.C:
			if n, err := w.repo.ReconcileStaleSourceRefresh(ctx); err == nil && n > 0 {
				w.logger.Warn("[SourceRefresh] reconciled stale running tasks", "log_target", "refresh", "count", n)
			}
			_ = w.repo.PruneDoneSourceRefresh(ctx)
		}
	}
}

// drain 持续认领并发处理，直到队列没有待处理任务。
func (w *SourceRefreshWorker) drain(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		tasks, err := w.queue.Claim(ctx, sourceRefreshClaimBatch)
		if err != nil {
			w.logger.Warn("[SourceRefresh] claim failed", "log_target", "refresh", "error", err)
			return
		}
		if len(tasks) == 0 {
			return
		}
		var wg sync.WaitGroup
		for _, task := range tasks {
			wg.Add(1)
			go func(t repository.SourceRefreshTask) {
				defer wg.Done()
				w.process(ctx, t)
			}(task)
		}
		wg.Wait()
	}
}

func (w *SourceRefreshWorker) process(ctx context.Context, task repository.SourceRefreshTask) {
	taskCtx, cancel := context.WithTimeout(ctx, sourceRefreshTaskTimeout)
	defer cancel()

	var err error
	switch task.TaskType {
	case RefreshTaskCatalogFetch:
		err = w.executor.runCatalogFetch(taskCtx, task.TargetID)
	case RefreshTaskDetailRefresh:
		err = w.executor.runDetailRefresh(taskCtx, task.TargetID)
	default:
		_ = w.repo.FailFatalSourceRefresh(ctx, task.ID, "unknown task type: "+task.TaskType)
		return
	}

	if err != nil {
		w.queue.Fail(ctx, task.ID, task.RetryCount, err.Error())
		w.logger.Warn("[SourceRefresh] task failed",
			"log_target", "refresh",
			"task_type", task.TaskType,
			"target_id", task.TargetID,
			"retry", task.RetryCount,
			"error_type", ErrorType(err))
		return
	}
	w.queue.Done(ctx, task.ID)
}
