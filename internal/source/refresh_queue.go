package source

import (
	"context"
	"time"

	"fyms/internal/repository"
)

// 在线源刷新任务类型。
const (
	RefreshTaskCatalogFetch  = "catalog_fetch"  // 遍历 provider 分类把内容批量入库，填充虚拟库
	RefreshTaskDetailRefresh = "detail_refresh" // 重拉连载剧 detail，追更集数
)

// 任务目标维度。
const (
	RefreshTargetProvider = "provider"
	RefreshTargetItem     = "item"
)

// 优先级（数值越小越优先）。
const (
	RefreshPriorityManual    = 0 // 用户手动触发
	RefreshPriorityScheduled = 5 // 定时调度
)

const (
	sourceRefreshMaxRetry = 5
	// 单次 catalog_fetch 每个分类抓取的页数上限（控制源站压力与库膨胀）。
	sourceCatalogFetchPagesPerCategory = 2
	// 单次 catalog_fetch 遍历的分类数上限。
	sourceCatalogFetchMaxCategories = 30
)

// SourceRefreshQueue 是对 source_refresh_queue 表的薄封装。
type SourceRefreshQueue struct {
	repo *repository.SourceRepository
}

func NewSourceRefreshQueue(repo *repository.SourceRepository) *SourceRefreshQueue {
	return &SourceRefreshQueue{repo: repo}
}

func (q *SourceRefreshQueue) EnqueueCatalogFetch(ctx context.Context, providerID int64, priority int16) error {
	return q.repo.EnqueueSourceRefresh(ctx, RefreshTaskCatalogFetch, RefreshTargetProvider, providerID, priority, nil)
}

func (q *SourceRefreshQueue) EnqueueDetailRefresh(ctx context.Context, itemID int64, priority int16) error {
	return q.repo.EnqueueSourceRefresh(ctx, RefreshTaskDetailRefresh, RefreshTargetItem, itemID, priority, nil)
}

func (q *SourceRefreshQueue) Claim(ctx context.Context, limit int) ([]repository.SourceRefreshTask, error) {
	return q.repo.ClaimSourceRefresh(ctx, limit)
}

func (q *SourceRefreshQueue) Done(ctx context.Context, id int64) {
	_ = q.repo.DoneSourceRefresh(ctx, id)
}

// Fail 按退避重试，超过上限落 failed。
func (q *SourceRefreshQueue) Fail(ctx context.Context, id int64, retryCount int16, errMsg string) {
	if retryCount+1 >= sourceRefreshMaxRetry {
		_ = q.repo.FailFatalSourceRefresh(ctx, id, truncateRefreshError(errMsg))
		return
	}
	_ = q.repo.FailSourceRefresh(ctx, id, sourceRefreshBackoff(retryCount+1), truncateRefreshError(errMsg))
}

func (q *SourceRefreshQueue) Stats(ctx context.Context) (map[string]int64, error) {
	return q.repo.SourceRefreshQueueStats(ctx)
}

// sourceRefreshBackoff: 2^retry 分钟，上限 32 分钟。
func sourceRefreshBackoff(retryCount int16) time.Duration {
	if retryCount < 1 {
		retryCount = 1
	}
	if retryCount > 5 {
		retryCount = 5
	}
	return time.Duration(1<<retryCount) * time.Minute
}

func truncateRefreshError(s string) string {
	const maxErr = 1000
	if len(s) > maxErr {
		return s[:maxErr] + "...[truncated]"
	}
	return s
}
