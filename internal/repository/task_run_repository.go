package repository

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	dbgen "fyms/internal/db/gen"
)

type TaskRunRepository struct {
	queries *dbgen.Queries
}

type TaskRun struct {
	ID          int64
	Kind        string
	Stage       string
	ParentID    int64
	Status      string
	Trigger     string
	Total       int64
	Processed   int64
	Success     int64
	Failed      int64
	Counters    map[string]int64
	Message     string
	Error       string
	Payload     map[string]any
	StartedAt   int64
	CompletedAt int64
	DurationMs  int64
}

type TaskRunCreate struct {
	Kind     string
	Stage    string
	ParentID int64
	Trigger  string
	Total    int64
	Payload  []byte
}

type TaskRunHistoryFilter struct {
	Kind     string
	ParentID *int64
	Limit    int
}

func NewTaskRunRepository(pool *pgxpool.Pool) *TaskRunRepository {
	return &TaskRunRepository{queries: dbgen.New(pool)}
}

func (r *TaskRunRepository) Begin(ctx context.Context, p TaskRunCreate) (int64, error) {
	return r.queries.CreateTaskRun(ctx, taskRunCreateParams(p))
}

func (r *TaskRunRepository) BeginQueued(ctx context.Context, p TaskRunCreate) (int64, error) {
	return r.queries.CreateQueuedTaskRun(ctx, dbgen.CreateQueuedTaskRunParams{
		Kind:     p.Kind,
		Stage:    nullablePGText(p.Stage),
		ParentID: nullablePGInt8(p.ParentID),
		Trigger:  p.Trigger,
		Total:    p.Total,
		Column6:  p.Payload,
	})
}

func (r *TaskRunRepository) MarkRunning(ctx context.Context, runID int64) error {
	return r.queries.MarkTaskRunRunning(ctx, runID)
}

func (r *TaskRunRepository) UpdateProgress(ctx context.Context, runID int64, processed, success, failed, total int64, counters []byte) error {
	return r.queries.UpdateTaskRunProgress(ctx, dbgen.UpdateTaskRunProgressParams{
		ID:        runID,
		Processed: processed,
		Success:   success,
		Failed:    failed,
		Total:     total,
		Column6:   counters,
	})
}

func (r *TaskRunRepository) End(ctx context.Context, runID int64, status, message, errMsg string) error {
	return r.queries.EndTaskRun(ctx, dbgen.EndTaskRunParams{
		ID:      runID,
		Status:  status,
		Column3: message,
		Column4: errMsg,
	})
}

func (r *TaskRunRepository) History(ctx context.Context, f TaskRunHistoryFilter) ([]TaskRun, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	lim := int32(limit)

	var (
		rows []dbgen.TaskRun
		err  error
	)
	switch {
	case f.Kind != "" && f.ParentID != nil && *f.ParentID == 0:
		rows, err = r.queries.ListTaskRunHistoryByKindTopLevel(ctx, dbgen.ListTaskRunHistoryByKindTopLevelParams{Limit: lim, Kind: f.Kind})
	case f.Kind != "" && f.ParentID != nil:
		rows, err = r.queries.ListTaskRunHistoryByKindAndParent(ctx, dbgen.ListTaskRunHistoryByKindAndParentParams{
			Limit:    lim,
			Kind:     f.Kind,
			ParentID: pgtype.Int8{Int64: *f.ParentID, Valid: true},
		})
	case f.Kind != "":
		rows, err = r.queries.ListTaskRunHistoryByKind(ctx, dbgen.ListTaskRunHistoryByKindParams{Limit: lim, Kind: f.Kind})
	case f.ParentID != nil && *f.ParentID == 0:
		rows, err = r.queries.ListTaskRunHistoryTopLevel(ctx, lim)
	case f.ParentID != nil:
		rows, err = r.queries.ListTaskRunHistoryByParent(ctx, dbgen.ListTaskRunHistoryByParentParams{
			Limit:    lim,
			ParentID: pgtype.Int8{Int64: *f.ParentID, Valid: true},
		})
	default:
		rows, err = r.queries.ListTaskRunHistory(ctx, lim)
	}
	if err != nil {
		return nil, err
	}
	return mapTaskRuns(rows), nil
}

func (r *TaskRunRepository) ReconcileOnStartup(ctx context.Context) error {
	return r.queries.ReconcileTaskRunsOnStartup(ctx)
}

func (r *TaskRunRepository) ReconcileUpdateOnStartup(ctx context.Context, status, message, errMsg string) error {
	return r.queries.ReconcileUpdateTaskRunsOnStartup(ctx, dbgen.ReconcileUpdateTaskRunsOnStartupParams{
		Status:  status,
		Column2: message,
		Column3: errMsg,
	})
}

func taskRunCreateParams(p TaskRunCreate) dbgen.CreateTaskRunParams {
	return dbgen.CreateTaskRunParams{
		Kind:     p.Kind,
		Stage:    nullablePGText(p.Stage),
		ParentID: nullablePGInt8(p.ParentID),
		Trigger:  p.Trigger,
		Total:    p.Total,
		Column6:  p.Payload,
	}
}

func nullablePGText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func nullablePGInt8(v int64) pgtype.Int8 {
	if v == 0 {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: v, Valid: true}
}

func mapTaskRuns(rows []dbgen.TaskRun) []TaskRun {
	out := make([]TaskRun, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapTaskRun(row))
	}
	return out
}

func mapTaskRun(row dbgen.TaskRun) TaskRun {
	r := TaskRun{
		ID:        row.ID,
		Kind:      row.Kind,
		Status:    row.Status,
		Trigger:   row.Trigger,
		Total:     row.Total,
		Processed: row.Processed,
		Success:   row.Success,
		Failed:    row.Failed,
		StartedAt: row.StartedAt.Time.UnixMilli(),
	}
	if row.Stage.Valid {
		r.Stage = row.Stage.String
	}
	if row.ParentID.Valid {
		r.ParentID = row.ParentID.Int64
	}
	if row.Message.Valid {
		r.Message = row.Message.String
	}
	if row.Error.Valid {
		r.Error = row.Error.String
	}
	if row.CompletedAt.Valid {
		r.CompletedAt = row.CompletedAt.Time.UnixMilli()
	}
	if row.DurationMs.Valid {
		r.DurationMs = row.DurationMs.Int64
	}
	if len(row.Counters) > 0 {
		_ = json.Unmarshal(row.Counters, &r.Counters)
	}
	if len(row.Payload) > 0 {
		_ = json.Unmarshal(row.Payload, &r.Payload)
	}
	return r
}
