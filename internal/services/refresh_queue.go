package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
	pool *pgxpool.Pool
}

func NewRefreshQueue(pool *pgxpool.Pool) *RefreshQueue {
	return &RefreshQueue{pool: pool}
}

func (q *RefreshQueue) Enqueue(ctx context.Context, itemID string, scope RefreshScope, source RefreshSource, priority int16, opts RefreshOptions) error {
	_, err := q.pool.Exec(ctx,
		`INSERT INTO refresh_queue (item_id, scope, source, priority, options_json, status, next_run_at)
		 VALUES ($1::uuid, $2, $3, $4, $5::jsonb, 'pending', NOW())
		 ON CONFLICT (item_id, scope) DO UPDATE SET
		   source       = EXCLUDED.source,
		   priority     = LEAST(refresh_queue.priority, EXCLUDED.priority),
		   options_json = EXCLUDED.options_json,
		   status       = CASE WHEN refresh_queue.status IN ('done', 'failed')
		                      THEN 'pending' ELSE refresh_queue.status END,
		   retry_count  = CASE WHEN refresh_queue.status IN ('done', 'failed')
		                      THEN 0 ELSE refresh_queue.retry_count END,
		   next_run_at  = CASE WHEN refresh_queue.status IN ('done', 'failed')
		                      THEN NOW() ELSE refresh_queue.next_run_at END,
		   last_error   = CASE WHEN refresh_queue.status IN ('done', 'failed')
		                      THEN NULL ELSE refresh_queue.last_error END,
		   updated_at   = NOW()`,
		itemID, string(scope), string(source), priority, opts.Marshal(),
	)
	return err
}

func (q *RefreshQueue) EnqueueBatch(ctx context.Context, itemIDs []string, scope RefreshScope, source RefreshSource, priority int16, opts RefreshOptions) (int64, error) {
	if len(itemIDs) == 0 {
		return 0, nil
	}
	tag, err := q.pool.Exec(ctx,
		`INSERT INTO refresh_queue (item_id, scope, source, priority, options_json, status, next_run_at)
		 SELECT id::uuid, $2, $3, $4, $5::jsonb, 'pending', NOW()
		   FROM unnest($1::text[]) AS t(id)
		 ON CONFLICT (item_id, scope) DO UPDATE SET
		   source       = EXCLUDED.source,
		   priority     = LEAST(refresh_queue.priority, EXCLUDED.priority),
		   options_json = EXCLUDED.options_json,
		   status       = CASE WHEN refresh_queue.status IN ('done', 'failed')
		                      THEN 'pending' ELSE refresh_queue.status END,
		   retry_count  = CASE WHEN refresh_queue.status IN ('done', 'failed')
		                      THEN 0 ELSE refresh_queue.retry_count END,
		   next_run_at  = CASE WHEN refresh_queue.status IN ('done', 'failed')
		                      THEN NOW() ELSE refresh_queue.next_run_at END,
		   last_error   = CASE WHEN refresh_queue.status IN ('done', 'failed')
		                      THEN NULL ELSE refresh_queue.last_error END,
		   updated_at   = NOW()`,
		itemIDs, string(scope), string(source), priority, opts.Marshal(),
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *RefreshQueue) Claim(ctx context.Context, limit int) ([]RefreshTask, error) {
	rows, err := q.pool.Query(ctx,
		`WITH claimed AS (
		    SELECT id FROM refresh_queue
		     WHERE status = 'pending' AND next_run_at <= NOW()
		     ORDER BY priority, next_run_at
		     FOR UPDATE SKIP LOCKED
		     LIMIT $1
		 )
		 UPDATE refresh_queue q
		    SET status = 'running', updated_at = NOW()
		   FROM claimed
		  WHERE q.id = claimed.id
		 RETURNING q.id, q.item_id::text, q.scope, q.source, q.priority,
		           q.options_json::text, q.retry_count, q.next_run_at, q.created_at`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []RefreshTask
	for rows.Next() {
		var t RefreshTask
		var scope, source, rawOpts string
		if err := rows.Scan(&t.ID, &t.ItemID, &scope, &source, &t.Priority, &rawOpts, &t.RetryCount, &t.NextRunAt, &t.CreatedAt); err != nil {
			continue
		}
		t.Scope = RefreshScope(scope)
		t.Source = RefreshSource(source)
		t.Options = ParseRefreshOptions(rawOpts)
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func (q *RefreshQueue) Done(ctx context.Context, id int64) {
	_, _ = q.pool.Exec(ctx,
		`UPDATE refresh_queue
		    SET status = 'done', last_error = NULL, updated_at = NOW()
		  WHERE id = $1`,
		id)
}

func (q *RefreshQueue) Fail(ctx context.Context, id int64, retryCount int16, maxRetry int16, errMsg string) {
	if retryCount+1 >= maxRetry {
		_, _ = q.pool.Exec(ctx,
			`UPDATE refresh_queue
			    SET status = 'failed', retry_count = retry_count + 1,
			        last_error = $2, updated_at = NOW()
			  WHERE id = $1`,
			id, truncateRefreshError(errMsg))
		return
	}

	backoff := refreshRetryBackoff(retryCount + 1)
	_, _ = q.pool.Exec(ctx,
		`UPDATE refresh_queue
		    SET status = 'pending', retry_count = retry_count + 1,
		        last_error = $2, next_run_at = NOW() + $3::interval,
		        updated_at = NOW()
		  WHERE id = $1`,
		id, truncateRefreshError(errMsg), fmt.Sprintf("%d seconds", int(backoff.Seconds())))
}

func (q *RefreshQueue) ReconcileOnStartup(ctx context.Context) error {
	tag, err := q.pool.Exec(ctx,
		`UPDATE refresh_queue
		    SET status = 'pending', updated_at = NOW()
		  WHERE status = 'running'`)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		slog.Info("[RefreshQueue] Reconciled orphan running tasks at startup", "count", tag.RowsAffected())
	}
	return nil
}

func (q *RefreshQueue) ReconcileStaleRunning(ctx context.Context) error {
	tag, err := q.pool.Exec(ctx,
		`UPDATE refresh_queue
		    SET status = 'pending', updated_at = NOW()
		  WHERE status = 'running' AND updated_at < NOW() - INTERVAL '10 minutes'`)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		slog.Warn("[RefreshQueue] Reconciled stale running tasks during runtime", "count", tag.RowsAffected())
	}
	return nil
}

func (q *RefreshQueue) PruneDone(ctx context.Context) error {
	_, err := q.pool.Exec(ctx,
		`DELETE FROM refresh_queue WHERE status = 'done' AND updated_at < NOW() - INTERVAL '7 days'`)
	return err
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

	var where string
	switch status {
	case "failed":
		where = "rq.status = 'failed'"
	case "running":
		where = "rq.status = 'running'"
	case "pending":
		where = "rq.status = 'pending'"
	case "done":
		where = "rq.status = 'done'"
	default:
		where = "rq.status IN ('failed', 'running')"
	}

	rows, err := q.pool.Query(ctx,
		`SELECT rq.id, rq.item_id::text,
		        COALESCE(i.name, ''), COALESCE(i.type, ''),
		        COALESCE(i.file_path, ''),
		        COALESCE(i.series_name, ''),
		        i.index_number, i.parent_index_number,
		        rq.scope, rq.source, rq.status, rq.priority, rq.retry_count,
		        COALESCE(rq.last_error, ''), rq.next_run_at, rq.updated_at
		   FROM refresh_queue rq
		   LEFT JOIN items i ON i.id = rq.item_id
		  WHERE `+where+`
		  ORDER BY CASE rq.status WHEN 'failed' THEN 0 WHEN 'running' THEN 1 WHEN 'pending' THEN 2 ELSE 3 END,
		           rq.updated_at DESC
		  LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RefreshRecentTask
	for rows.Next() {
		var t RefreshRecentTask
		var scope, source string
		if err := rows.Scan(
			&t.ID, &t.ItemID, &t.ItemName, &t.ItemType,
			&t.FilePath, &t.SeriesName, &t.IndexNumber, &t.ParentIndexNumber,
			&scope, &source, &t.Status, &t.Priority, &t.RetryCount,
			&t.LastError, &t.NextRunAt, &t.UpdatedAt,
		); err != nil {
			continue
		}
		t.Scope = RefreshScope(scope)
		t.Source = RefreshSource(source)
		out = append(out, t)
	}
	return out, nil
}

func (q *RefreshQueue) RecentCount(ctx context.Context, status string) (int64, error) {
	var where string
	switch status {
	case "failed", "running", "pending", "done":
		where = "status = $1"
	default:
		where = "status IN ('failed', 'running')"
	}
	var n int64
	if where == "status = $1" {
		err := q.pool.QueryRow(ctx, `SELECT COUNT(*) FROM refresh_queue WHERE `+where, status).Scan(&n)
		return n, err
	}
	err := q.pool.QueryRow(ctx, `SELECT COUNT(*) FROM refresh_queue WHERE `+where).Scan(&n)
	return n, err
}

type RefreshTaskDetail struct {
	RefreshRecentTask
	Options RefreshOptions `json:"options"`
}

func (q *RefreshQueue) GetTaskDetail(ctx context.Context, id int64) (*RefreshTaskDetail, error) {
	var t RefreshTaskDetail
	var scope, source, rawOpts string
	err := q.pool.QueryRow(ctx,
		`SELECT rq.id, rq.item_id::text,
		        COALESCE(i.name, ''), COALESCE(i.type, ''),
		        COALESCE(i.file_path, ''),
		        COALESCE(i.series_name, ''),
		        i.index_number, i.parent_index_number,
		        rq.scope, rq.source, rq.status, rq.priority, rq.retry_count,
		        COALESCE(rq.last_error, ''), rq.next_run_at, rq.updated_at,
		        rq.options_json::text
		   FROM refresh_queue rq
		   LEFT JOIN items i ON i.id = rq.item_id
		  WHERE rq.id = $1`,
		id,
	).Scan(
		&t.ID, &t.ItemID, &t.ItemName, &t.ItemType,
		&t.FilePath, &t.SeriesName, &t.IndexNumber, &t.ParentIndexNumber,
		&scope, &source, &t.Status, &t.Priority, &t.RetryCount,
		&t.LastError, &t.NextRunAt, &t.UpdatedAt,
		&rawOpts,
	)
	if err != nil {
		return nil, err
	}
	t.Scope = RefreshScope(scope)
	t.Source = RefreshSource(source)
	t.Options = ParseRefreshOptions(rawOpts)
	return &t, nil
}

func (q *RefreshQueue) RetryTask(ctx context.Context, id int64) error {
	_, err := q.pool.Exec(ctx,
		`UPDATE refresh_queue
		    SET status = 'pending', next_run_at = NOW(), retry_count = 0,
		        last_error = NULL, updated_at = NOW()
		  WHERE id = $1 AND status = 'failed'`,
		id)
	return err
}

func (q *RefreshQueue) RetryAllFailed(ctx context.Context) (int64, error) {
	tag, err := q.pool.Exec(ctx,
		`UPDATE refresh_queue
		    SET status = 'pending', next_run_at = NOW(), retry_count = 0,
		        last_error = NULL, updated_at = NOW()
		  WHERE status = 'failed'`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *RefreshQueue) Stats(ctx context.Context) (QueueStats, error) {
	var s QueueStats
	rows, err := q.pool.Query(ctx,
		`SELECT status, COUNT(*) FROM refresh_queue GROUP BY status`)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			continue
		}
		switch status {
		case "pending":
			s.Pending = n
		case "running":
			s.Running = n
		case "done":
			s.Done = n
		case "failed":
			s.Failed = n
		}
	}
	return s, nil
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
