package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
)

const (
	RefreshPriorityManual = 0
	RefreshPriorityFS     = 2
	RefreshPriorityScan   = 3
)

type RefreshTask struct {
	ID         int64
	ItemID     string
	Scope      RefreshScope
	Source     RefreshSource
	Priority   int16
	Options    RefreshOptions
	RetryCount int16
	NextRunAt  time.Time
	CreatedAt  time.Time
}

type RefreshQueue struct {
	repo *repository.RefreshQueueRepository
}

func NewRefreshQueue(pool *pgxpool.Pool) *RefreshQueue {
	return &RefreshQueue{repo: repository.NewRefreshQueueRepository(pool)}
}

func (q *RefreshQueue) Enqueue(ctx context.Context, itemID string, scope RefreshScope, source RefreshSource, priority int16, opts RefreshOptions) error {
	return q.repo.Enqueue(ctx, itemID, string(scope), string(source), priority, []byte(opts.Marshal()))
}

func (q *RefreshQueue) EnqueueBatch(ctx context.Context, itemIDs []string, scope RefreshScope, source RefreshSource, priority int16, opts RefreshOptions) (int64, error) {
	if len(itemIDs) == 0 {
		return 0, nil
	}
	return q.repo.EnqueueBatch(ctx, itemIDs, string(scope), string(source), priority, []byte(opts.Marshal()))
}

func (q *RefreshQueue) Claim(ctx context.Context, limit int) ([]RefreshTask, error) {
	rows, err := q.repo.Claim(ctx, limit)
	if err != nil {
		return nil, err
	}
	tasks := make([]RefreshTask, 0, len(rows))
	for _, row := range rows {
		tasks = append(tasks, RefreshTask{
			ID:         row.ID,
			ItemID:     row.ItemID,
			Scope:      RefreshScope(row.Scope),
			Source:     RefreshSource(row.Source),
			Priority:   row.Priority,
			Options:    ParseRefreshOptions(row.OptionsRaw),
			RetryCount: row.RetryCount,
			NextRunAt:  row.NextRunAt,
			CreatedAt:  row.CreatedAt,
		})
	}
	return tasks, nil
}

func (q *RefreshQueue) Done(ctx context.Context, id int64) {
	_ = q.repo.Done(ctx, id)
}

func (q *RefreshQueue) Fail(ctx context.Context, id int64, retryCount int16, maxRetry int16, errMsg string) {
	if retryCount+1 >= maxRetry {
		_ = q.repo.FailFatal(ctx, id, truncateRefreshError(errMsg))
		return
	}

	backoff := refreshRetryBackoff(retryCount + 1)
	_ = q.repo.FailRetry(ctx, id, backoff, truncateRefreshError(errMsg))
}

func (q *RefreshQueue) ReconcileOnStartup(ctx context.Context) error {
	count, err := q.repo.ReconcileOnStartup(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		slog.Info("[RefreshQueue] Reconciled orphan running tasks at startup", "count", count)
	}
	return nil
}

func (q *RefreshQueue) ReconcileStaleRunning(ctx context.Context) error {
	count, err := q.repo.ReconcileStaleRunning(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		slog.Warn("[RefreshQueue] Reconciled stale running tasks during runtime", "count", count)
	}
	return nil
}

func (q *RefreshQueue) PruneDone(ctx context.Context) error {
	return q.repo.PruneDone(ctx)
}

type RefreshRecentTask struct {
	ID                int64         `json:"id"`
	ItemID            string        `json:"item_id"`
	ItemName          string        `json:"item_name"`
	ItemType          string        `json:"item_type"`
	FilePath          string        `json:"file_path,omitempty"`
	SeriesName        string        `json:"series_name,omitempty"`
	IndexNumber       *int32        `json:"index_number,omitempty"`
	ParentIndexNumber *int32        `json:"parent_index_number,omitempty"`
	Scope             RefreshScope  `json:"scope"`
	Source            RefreshSource `json:"source"`
	Status            string        `json:"status"`
	Priority          int16         `json:"priority"`
	RetryCount        int16         `json:"retry_count"`
	LastError         string        `json:"last_error,omitempty"`
	NextRunAt         time.Time     `json:"next_run_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

func (q *RefreshQueue) Recent(ctx context.Context, status string, limit, offset int) ([]RefreshRecentTask, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	repoRows, err := q.repo.Recent(ctx, status, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]RefreshRecentTask, 0, len(repoRows))
	for _, row := range repoRows {
		out = append(out, RefreshRecentTask{
			ID:                row.ID,
			ItemID:            row.ItemID,
			ItemName:          row.ItemName,
			ItemType:          row.ItemType,
			FilePath:          row.FilePath,
			SeriesName:        row.SeriesName,
			IndexNumber:       row.IndexNumber,
			ParentIndexNumber: row.ParentIndexNumber,
			Scope:             RefreshScope(row.Scope),
			Source:            RefreshSource(row.Source),
			Status:            row.Status,
			Priority:          row.Priority,
			RetryCount:        row.RetryCount,
			LastError:         row.LastError,
			NextRunAt:         row.NextRunAt,
			UpdatedAt:         row.UpdatedAt,
		})
	}
	return out, nil
}

func (q *RefreshQueue) RecentCount(ctx context.Context, status string) (int64, error) {
	return q.repo.RecentCount(ctx, status)
}

type RefreshTaskDetail struct {
	RefreshRecentTask
	Options RefreshOptions `json:"options"`
}

func (q *RefreshQueue) GetTaskDetail(ctx context.Context, id int64) (*RefreshTaskDetail, error) {
	row, err := q.repo.GetTaskDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	return &RefreshTaskDetail{
		RefreshRecentTask: RefreshRecentTask{
			ID:                row.ID,
			ItemID:            row.ItemID,
			ItemName:          row.ItemName,
			ItemType:          row.ItemType,
			FilePath:          row.FilePath,
			SeriesName:        row.SeriesName,
			IndexNumber:       row.IndexNumber,
			ParentIndexNumber: row.ParentIndexNumber,
			Scope:             RefreshScope(row.Scope),
			Source:            RefreshSource(row.Source),
			Status:            row.Status,
			Priority:          row.Priority,
			RetryCount:        row.RetryCount,
			LastError:         row.LastError,
			NextRunAt:         row.NextRunAt,
			UpdatedAt:         row.UpdatedAt,
		},
		Options: ParseRefreshOptions(row.OptionsRaw),
	}, nil
}

func (q *RefreshQueue) RetryTask(ctx context.Context, id int64) error {
	return q.repo.RetryTask(ctx, id)
}

func (q *RefreshQueue) RetryAllFailed(ctx context.Context) (int64, error) {
	return q.repo.RetryAllFailed(ctx)
}

func (q *RefreshQueue) Stats(ctx context.Context) (QueueStats, error) {
	stats, err := q.repo.Stats(ctx)
	if err != nil {
		return QueueStats{}, err
	}
	return QueueStats(stats), nil
}

func refreshRetryBackoff(retryCount int16) time.Duration {
	if retryCount < 1 {
		retryCount = 1
	}
	if retryCount > 5 {
		retryCount = 5
	}
	return time.Duration(1<<retryCount) * time.Minute
}

func truncateRefreshError(s string) string {
	const maxErr = 2000
	if len(s) > maxErr {
		return s[:maxErr] + "...[truncated]"
	}
	return s
}
