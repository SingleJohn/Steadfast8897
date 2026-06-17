package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	dbgen "fyms/internal/db/gen"
)

type ScrapeQueueRepository struct {
	queries *dbgen.Queries
}

type ScrapeQueueTask struct {
	ID         int64
	ItemID     string
	TaskType   string
	Priority   int16
	RetryCount int16
	NextRunAt  time.Time
	CreatedAt  time.Time
}

type QueueRecentTask struct {
	ID                int64
	ItemID            string
	ItemName          string
	ItemType          string
	FilePath          string
	SeriesName        string
	IndexNumber       *int32
	ParentIndexNumber *int32
	TaskType          string
	Scope             string
	Source            string
	Status            string
	Priority          int16
	RetryCount        int16
	LastError         string
	NextRunAt         time.Time
	UpdatedAt         time.Time
}

type ScrapeQueueTaskDetail struct {
	QueueRecentTask
	RequestURL     string
	ResponseStatus *int
	ResponseSample string
	DetailJSON     any
}

type QueueStats struct {
	Pending int64
	Running int64
	Done    int64
	Failed  int64
}

type ScrapeQueueFailure struct {
	Error          string
	RequestURL     string
	ResponseStatus int
	ResponseSample string
	DetailJSON     []byte
}

type AutoScrapeCandidate struct {
	ItemType       string
	Overview       string
	PlatformSource string
	HasExternalIDs bool
}

func NewScrapeQueueRepository(pool *pgxpool.Pool) *ScrapeQueueRepository {
	return &ScrapeQueueRepository{queries: dbgen.New(pool)}
}

func (r *ScrapeQueueRepository) Enqueue(ctx context.Context, itemID string, taskType string, priority int16) error {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return err
	}
	return r.queries.UpsertScrapeQueueTask(ctx, dbgen.UpsertScrapeQueueTaskParams{
		Column1:  toPGUUID(uid),
		TaskType: taskType,
		Priority: priority,
	})
}

func (r *ScrapeQueueRepository) EnqueueBatch(ctx context.Context, itemIDs []string, taskType string, priority int16) (int64, error) {
	if len(itemIDs) == 0 {
		return 0, nil
	}
	return r.queries.UpsertScrapeQueueTasks(ctx, dbgen.UpsertScrapeQueueTasksParams{
		Column1:  itemIDs,
		TaskType: taskType,
		Priority: priority,
	})
}

func (r *ScrapeQueueRepository) GetAutoScrapeCandidate(ctx context.Context, itemID string) (*AutoScrapeCandidate, error) {
	uid, err := uuid.Parse(itemID)
	if err != nil {
		return nil, err
	}
	row, err := r.queries.GetAutoScrapeCandidate(ctx, toPGUUID(uid))
	if err != nil {
		return nil, err
	}
	return &AutoScrapeCandidate{
		ItemType:       row.Type,
		Overview:       row.Overview,
		PlatformSource: row.PlatformScanSource,
		HasExternalIDs: row.HasExternalIds,
	}, nil
}

func (r *ScrapeQueueRepository) ListAutoScrapeCandidatesByLibrary(ctx context.Context, libraryID string) ([]string, error) {
	uid, err := uuid.Parse(libraryID)
	if err != nil {
		return nil, err
	}
	return r.queries.ListAutoScrapeCandidatesByLibrary(ctx, toPGUUID(uid))
}

func (r *ScrapeQueueRepository) ListMissingScrapeIdentifyCandidates(ctx context.Context) ([]string, error) {
	return r.queries.ListMissingScrapeIdentifyCandidates(ctx)
}

func (r *ScrapeQueueRepository) Claim(ctx context.Context, limit int) ([]ScrapeQueueTask, error) {
	rows, err := r.queries.ClaimScrapeQueueTasks(ctx, int32(limit))
	if err != nil {
		return nil, err
	}
	out := make([]ScrapeQueueTask, 0, len(rows))
	for _, row := range rows {
		out = append(out, ScrapeQueueTask{
			ID:         row.ID,
			ItemID:     row.QItemID,
			TaskType:   row.TaskType,
			Priority:   row.Priority,
			RetryCount: row.RetryCount,
			NextRunAt:  row.NextRunAt.Time,
			CreatedAt:  row.CreatedAt.Time,
		})
	}
	return out, nil
}

func (r *ScrapeQueueRepository) Done(ctx context.Context, id int64) error {
	return r.queries.MarkScrapeQueueDone(ctx, id)
}

func (r *ScrapeQueueRepository) FailFatal(ctx context.Context, id int64, failure ScrapeQueueFailure) error {
	return r.queries.MarkScrapeQueueFailedFatal(ctx, dbgen.MarkScrapeQueueFailedFatalParams{
		ID:             id,
		LastError:      textValue(failure.Error),
		RequestUrl:     nullableText(failure.RequestURL),
		ResponseStatus: nullableInt32(failure.ResponseStatus),
		ResponseSample: nullableText(failure.ResponseSample),
		Column6:        nullableBytes(failure.DetailJSON),
	})
}

func (r *ScrapeQueueRepository) FailRetry(ctx context.Context, id int64, backoff time.Duration, failure ScrapeQueueFailure) error {
	return r.queries.MarkScrapeQueueFailedRetry(ctx, dbgen.MarkScrapeQueueFailedRetryParams{
		ID:             id,
		LastError:      textValue(failure.Error),
		Column3:        intervalFromDuration(backoff),
		RequestUrl:     nullableText(failure.RequestURL),
		ResponseStatus: nullableInt32(failure.ResponseStatus),
		ResponseSample: nullableText(failure.ResponseSample),
		Column7:        nullableBytes(failure.DetailJSON),
	})
}

func (r *ScrapeQueueRepository) Recent(ctx context.Context, status string, limit, offset int) ([]QueueRecentTask, error) {
	if status == "failed" || status == "running" || status == "pending" {
		rows, err := r.queries.ListRecentScrapeQueueTasks(ctx, dbgen.ListRecentScrapeQueueTasksParams{
			Limit:  int32(limit),
			Offset: int32(offset),
			Status: status,
		})
		if err != nil {
			return nil, err
		}
		out := make([]QueueRecentTask, 0, len(rows))
		for _, row := range rows {
			out = append(out, mapScrapeRecentTask(row))
		}
		return out, nil
	}
	rows, err := r.queries.ListRecentScrapeQueueActiveTasks(ctx, dbgen.ListRecentScrapeQueueActiveTasksParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]QueueRecentTask, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapScrapeActiveRecentTask(row))
	}
	return out, nil
}

func (r *ScrapeQueueRepository) RecentCount(ctx context.Context, status string) (int64, error) {
	if status == "failed" || status == "running" || status == "pending" {
		return r.queries.CountScrapeQueueTasksByStatus(ctx, status)
	}
	return r.queries.CountScrapeQueueActiveTasks(ctx)
}

func (r *ScrapeQueueRepository) RetryTask(ctx context.Context, id int64) error {
	return r.queries.RetryScrapeQueueTask(ctx, id)
}

func (r *ScrapeQueueRepository) RetryAllFailed(ctx context.Context) (int64, error) {
	return r.queries.RetryAllFailedScrapeQueueTasks(ctx)
}

func (r *ScrapeQueueRepository) GetTaskDetail(ctx context.Context, id int64) (*ScrapeQueueTaskDetail, error) {
	row, err := r.queries.GetScrapeQueueTaskDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	detail := &ScrapeQueueTaskDetail{
		QueueRecentTask: QueueRecentTask{
			ID:                row.ID,
			ItemID:            row.SqItemID,
			ItemName:          row.Name,
			ItemType:          row.Type,
			FilePath:          row.FilePath,
			SeriesName:        row.SeriesName,
			IndexNumber:       ptrInt32FromPG(row.IndexNumber),
			ParentIndexNumber: ptrInt32FromPG(row.ParentIndexNumber),
			TaskType:          row.TaskType,
			Status:            row.Status,
			Priority:          row.Priority,
			RetryCount:        row.RetryCount,
			LastError:         row.LastError,
			NextRunAt:         row.NextRunAt.Time,
			UpdatedAt:         row.UpdatedAt.Time,
		},
		RequestURL:     textOrEmpty(row.RequestUrl),
		ResponseStatus: ptrIntFromPG(row.ResponseStatus),
		ResponseSample: textOrEmpty(row.ResponseSample),
	}
	if len(row.DetailJson) > 0 {
		var parsed any
		if err := json.Unmarshal(row.DetailJson, &parsed); err == nil {
			detail.DetailJSON = parsed
		}
	}
	return detail, nil
}

func (r *ScrapeQueueRepository) ReconcileOnStartup(ctx context.Context) (int64, error) {
	return r.queries.ReconcileScrapeQueueRunning(ctx)
}

func (r *ScrapeQueueRepository) ReconcileStaleRunning(ctx context.Context) (int64, error) {
	return r.queries.ReconcileStaleScrapeQueueRunning(ctx)
}

func (r *ScrapeQueueRepository) PruneDone(ctx context.Context) error {
	return r.queries.PruneDoneScrapeQueueTasks(ctx)
}

func (r *ScrapeQueueRepository) Stats(ctx context.Context) (QueueStats, error) {
	var stats QueueStats
	rows, err := r.queries.CountScrapeQueueTasksByStatusGroup(ctx)
	if err != nil {
		return stats, err
	}
	for _, row := range rows {
		applyQueueStatusCount(&stats, row.Status, row.Count)
	}
	return stats, nil
}

func mapScrapeRecentTask(row dbgen.ListRecentScrapeQueueTasksRow) QueueRecentTask {
	return QueueRecentTask{
		ID:                row.ID,
		ItemID:            row.SqItemID,
		ItemName:          row.Name,
		ItemType:          row.Type,
		FilePath:          row.FilePath,
		SeriesName:        row.SeriesName,
		IndexNumber:       ptrInt32FromPG(row.IndexNumber),
		ParentIndexNumber: ptrInt32FromPG(row.ParentIndexNumber),
		TaskType:          row.TaskType,
		Status:            row.Status,
		Priority:          row.Priority,
		RetryCount:        row.RetryCount,
		LastError:         row.LastError,
		NextRunAt:         row.NextRunAt.Time,
		UpdatedAt:         row.UpdatedAt.Time,
	}
}

func mapScrapeActiveRecentTask(row dbgen.ListRecentScrapeQueueActiveTasksRow) QueueRecentTask {
	return QueueRecentTask{
		ID:                row.ID,
		ItemID:            row.SqItemID,
		ItemName:          row.Name,
		ItemType:          row.Type,
		FilePath:          row.FilePath,
		SeriesName:        row.SeriesName,
		IndexNumber:       ptrInt32FromPG(row.IndexNumber),
		ParentIndexNumber: ptrInt32FromPG(row.ParentIndexNumber),
		TaskType:          row.TaskType,
		Status:            row.Status,
		Priority:          row.Priority,
		RetryCount:        row.RetryCount,
		LastError:         row.LastError,
		NextRunAt:         row.NextRunAt.Time,
		UpdatedAt:         row.UpdatedAt.Time,
	}
}

func textOrEmpty(v pgtype.Text) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func applyQueueStatusCount(stats *QueueStats, status string, count int64) {
	switch status {
	case "pending":
		stats.Pending = count
	case "running":
		stats.Running = count
	case "done":
		stats.Done = count
	case "failed":
		stats.Failed = count
	}
}
