package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	dbgen "fyms/internal/db/gen"
)

type RefreshQueueRepository struct {
	queries *dbgen.Queries
}

type RefreshQueueTask struct {
	ID         int64
	ItemID     string
	Scope      string
	Source     string
	Priority   int16
	OptionsRaw string
	RetryCount int16
	NextRunAt  time.Time
	CreatedAt  time.Time
}

type RefreshQueueTaskDetail struct {
	QueueRecentTask
	OptionsRaw string
}

func NewRefreshQueueRepository(pool *pgxpool.Pool) *RefreshQueueRepository {
	return &RefreshQueueRepository{queries: dbgen.New(pool)}
}

func (r *RefreshQueueRepository) Enqueue(ctx context.Context, itemID, scope, source string, priority int16, optionsJSON []byte) error {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return err
	}
	return r.queries.UpsertRefreshQueueTask(ctx, dbgen.UpsertRefreshQueueTaskParams{
		Column1:  toPGUUID(uid),
		Scope:    scope,
		Source:   source,
		Priority: priority,
		Column5:  optionsJSON,
	})
}

func (r *RefreshQueueRepository) EnqueueBatch(ctx context.Context, itemIDs []string, scope, source string, priority int16, optionsJSON []byte) (int64, error) {
	if len(itemIDs) == 0 {
		return 0, nil
	}
	return r.queries.UpsertRefreshQueueTasks(ctx, dbgen.UpsertRefreshQueueTasksParams{
		Column1:  itemIDs,
		Scope:    scope,
		Source:   source,
		Priority: priority,
		Column5:  optionsJSON,
	})
}

func (r *RefreshQueueRepository) Claim(ctx context.Context, limit int) ([]RefreshQueueTask, error) {
	rows, err := r.queries.ClaimRefreshQueueTasks(ctx, int32(limit))
	if err != nil {
		return nil, err
	}
	out := make([]RefreshQueueTask, 0, len(rows))
	for _, row := range rows {
		out = append(out, RefreshQueueTask{
			ID:         row.ID,
			ItemID:     row.QItemID,
			Scope:      row.Scope,
			Source:     row.Source,
			Priority:   row.Priority,
			OptionsRaw: row.QOptionsJson,
			RetryCount: row.RetryCount,
			NextRunAt:  row.NextRunAt.Time,
			CreatedAt:  row.CreatedAt.Time,
		})
	}
	return out, nil
}

func (r *RefreshQueueRepository) Done(ctx context.Context, id int64) error {
	return r.queries.MarkRefreshQueueDone(ctx, id)
}

func (r *RefreshQueueRepository) FailFatal(ctx context.Context, id int64, errMsg string) error {
	return r.queries.MarkRefreshQueueFailedFatal(ctx, dbgen.MarkRefreshQueueFailedFatalParams{
		ID:        id,
		LastError: textValue(errMsg),
	})
}

func (r *RefreshQueueRepository) FailRetry(ctx context.Context, id int64, backoff time.Duration, errMsg string) error {
	return r.queries.MarkRefreshQueueFailedRetry(ctx, dbgen.MarkRefreshQueueFailedRetryParams{
		ID:        id,
		LastError: textValue(errMsg),
		Column3:   intervalFromDuration(backoff),
	})
}

func (r *RefreshQueueRepository) ReconcileOnStartup(ctx context.Context) (int64, error) {
	return r.queries.ReconcileRefreshQueueRunning(ctx)
}

func (r *RefreshQueueRepository) ReconcileStaleRunning(ctx context.Context) (int64, error) {
	return r.queries.ReconcileStaleRefreshQueueRunning(ctx)
}

func (r *RefreshQueueRepository) PruneDone(ctx context.Context) error {
	return r.queries.PruneDoneRefreshQueueTasks(ctx)
}

func (r *RefreshQueueRepository) Recent(ctx context.Context, status string, limit, offset int) ([]QueueRecentTask, error) {
	if status == "failed" || status == "running" || status == "pending" || status == "done" {
		rows, err := r.queries.ListRecentRefreshQueueTasks(ctx, dbgen.ListRecentRefreshQueueTasksParams{
			Limit:  int32(limit),
			Offset: int32(offset),
			Status: status,
		})
		if err != nil {
			return nil, err
		}
		out := make([]QueueRecentTask, 0, len(rows))
		for _, row := range rows {
			out = append(out, mapRefreshRecentTask(row))
		}
		return out, nil
	}
	rows, err := r.queries.ListRecentRefreshQueueActiveTasks(ctx, dbgen.ListRecentRefreshQueueActiveTasksParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]QueueRecentTask, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapRefreshActiveRecentTask(row))
	}
	return out, nil
}

func (r *RefreshQueueRepository) RecentCount(ctx context.Context, status string) (int64, error) {
	if status == "failed" || status == "running" || status == "pending" || status == "done" {
		return r.queries.CountRefreshQueueTasksByStatus(ctx, status)
	}
	return r.queries.CountRefreshQueueActiveTasks(ctx)
}

func (r *RefreshQueueRepository) GetTaskDetail(ctx context.Context, id int64) (*RefreshQueueTaskDetail, error) {
	row, err := r.queries.GetRefreshQueueTaskDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	return &RefreshQueueTaskDetail{
		QueueRecentTask: QueueRecentTask{
			ID:                row.ID,
			ItemID:            row.RqItemID,
			ItemName:          row.Name,
			ItemType:          row.Type,
			FilePath:          row.FilePath,
			SeriesName:        row.SeriesName,
			IndexNumber:       ptrInt32FromPG(row.IndexNumber),
			ParentIndexNumber: ptrInt32FromPG(row.ParentIndexNumber),
			Scope:             row.Scope,
			Source:            row.Source,
			Status:            row.Status,
			Priority:          row.Priority,
			RetryCount:        row.RetryCount,
			LastError:         row.LastError,
			NextRunAt:         row.NextRunAt.Time,
			UpdatedAt:         row.UpdatedAt.Time,
		},
		OptionsRaw: row.RqOptionsJson,
	}, nil
}

func (r *RefreshQueueRepository) RetryTask(ctx context.Context, id int64) error {
	return r.queries.RetryRefreshQueueTask(ctx, id)
}

func (r *RefreshQueueRepository) RetryAllFailed(ctx context.Context) (int64, error) {
	return r.queries.RetryAllFailedRefreshQueueTasks(ctx)
}

func (r *RefreshQueueRepository) Stats(ctx context.Context) (QueueStats, error) {
	var stats QueueStats
	rows, err := r.queries.CountRefreshQueueTasksByStatusGroup(ctx)
	if err != nil {
		return stats, err
	}
	for _, row := range rows {
		applyQueueStatusCount(&stats, row.Status, row.Count)
	}
	return stats, nil
}

func mapRefreshRecentTask(row dbgen.ListRecentRefreshQueueTasksRow) QueueRecentTask {
	return QueueRecentTask{
		ID:                row.ID,
		ItemID:            row.RqItemID,
		ItemName:          row.Name,
		ItemType:          row.Type,
		FilePath:          row.FilePath,
		SeriesName:        row.SeriesName,
		IndexNumber:       ptrInt32FromPG(row.IndexNumber),
		ParentIndexNumber: ptrInt32FromPG(row.ParentIndexNumber),
		Scope:             row.Scope,
		Source:            row.Source,
		Status:            row.Status,
		Priority:          row.Priority,
		RetryCount:        row.RetryCount,
		LastError:         row.LastError,
		NextRunAt:         row.NextRunAt.Time,
		UpdatedAt:         row.UpdatedAt.Time,
	}
}

func mapRefreshActiveRecentTask(row dbgen.ListRecentRefreshQueueActiveTasksRow) QueueRecentTask {
	return QueueRecentTask{
		ID:                row.ID,
		ItemID:            row.RqItemID,
		ItemName:          row.Name,
		ItemType:          row.Type,
		FilePath:          row.FilePath,
		SeriesName:        row.SeriesName,
		IndexNumber:       ptrInt32FromPG(row.IndexNumber),
		ParentIndexNumber: ptrInt32FromPG(row.ParentIndexNumber),
		Scope:             row.Scope,
		Source:            row.Source,
		Status:            row.Status,
		Priority:          row.Priority,
		RetryCount:        row.RetryCount,
		LastError:         row.LastError,
		NextRunAt:         row.NextRunAt.Time,
		UpdatedAt:         row.UpdatedAt.Time,
	}
}
