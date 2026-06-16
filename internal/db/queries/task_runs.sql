-- name: CreateTaskRun :one
INSERT INTO task_runs (kind, stage, parent_id, status, trigger, total, payload)
VALUES ($1, $2, $3, 'running', $4, $5, $6::jsonb)
RETURNING id;

-- name: CreateQueuedTaskRun :one
INSERT INTO task_runs (kind, stage, parent_id, status, trigger, total, payload)
VALUES ($1, $2, $3, 'queued', $4, $5, $6::jsonb)
RETURNING id;

-- name: MarkTaskRunRunning :exec
UPDATE task_runs
   SET status = 'running', started_at = NOW()
 WHERE id = $1 AND status = 'queued';

-- name: UpdateTaskRunProgress :exec
UPDATE task_runs
   SET processed = $2, success = $3, failed = $4, total = $5, counters = $6::jsonb
 WHERE id = $1;

-- name: EndTaskRun :exec
UPDATE task_runs
   SET status       = $2,
       message      = NULLIF($3, ''),
       error        = NULLIF($4, ''),
       completed_at = NOW(),
       duration_ms  = GREATEST(0, EXTRACT(EPOCH FROM (NOW() - started_at))::BIGINT * 1000)
 WHERE id = $1 AND status IN ('queued','running','stopping');

-- name: ListTaskRunHistory :many
SELECT id, kind, stage, parent_id, status, trigger,
       total, processed, success, failed, counters,
       message, error, payload, started_at, completed_at, duration_ms
  FROM task_runs
 ORDER BY started_at DESC
 LIMIT $1;

-- name: ListTaskRunHistoryByKind :many
SELECT id, kind, stage, parent_id, status, trigger,
       total, processed, success, failed, counters,
       message, error, payload, started_at, completed_at, duration_ms
  FROM task_runs
 WHERE kind = $2
 ORDER BY started_at DESC
 LIMIT $1;

-- name: ListTaskRunHistoryTopLevel :many
SELECT id, kind, stage, parent_id, status, trigger,
       total, processed, success, failed, counters,
       message, error, payload, started_at, completed_at, duration_ms
  FROM task_runs
 WHERE parent_id IS NULL
 ORDER BY started_at DESC
 LIMIT $1;

-- name: ListTaskRunHistoryByKindTopLevel :many
SELECT id, kind, stage, parent_id, status, trigger,
       total, processed, success, failed, counters,
       message, error, payload, started_at, completed_at, duration_ms
  FROM task_runs
 WHERE kind = $2 AND parent_id IS NULL
 ORDER BY started_at DESC
 LIMIT $1;

-- name: ListTaskRunHistoryByParent :many
SELECT id, kind, stage, parent_id, status, trigger,
       total, processed, success, failed, counters,
       message, error, payload, started_at, completed_at, duration_ms
  FROM task_runs
 WHERE parent_id = $2
 ORDER BY started_at DESC
 LIMIT $1;

-- name: ListTaskRunHistoryByKindAndParent :many
SELECT id, kind, stage, parent_id, status, trigger,
       total, processed, success, failed, counters,
       message, error, payload, started_at, completed_at, duration_ms
  FROM task_runs
 WHERE kind = $2 AND parent_id = $3
 ORDER BY started_at DESC
 LIMIT $1;

-- name: ReconcileTaskRunsOnStartup :exec
UPDATE task_runs
   SET status       = 'cancelled',
       error        = COALESCE(error, 'interrupted by server restart'),
       completed_at = NOW(),
       duration_ms  = GREATEST(0, EXTRACT(EPOCH FROM (NOW() - started_at))::BIGINT * 1000)
 WHERE status IN ('queued','running','stopping')
   AND kind <> 'update';

-- name: ReconcileUpdateTaskRunsOnStartup :exec
UPDATE task_runs
   SET status       = $1,
       message      = NULLIF($2, ''),
       error        = NULLIF($3, ''),
       completed_at = NOW(),
       duration_ms  = GREATEST(0, EXTRACT(EPOCH FROM (NOW() - started_at))::BIGINT * 1000)
 WHERE kind = 'update'
   AND status IN ('queued','running','stopping');
