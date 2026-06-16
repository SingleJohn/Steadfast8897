-- name: UpsertRefreshQueueTask :exec
INSERT INTO refresh_queue (item_id, scope, source, priority, options_json, status, next_run_at)
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
  updated_at   = NOW();

-- name: UpsertRefreshQueueTasks :execrows
INSERT INTO refresh_queue (item_id, scope, source, priority, options_json, status, next_run_at)
SELECT id::uuid, $2, $3, $4, $5::jsonb, 'pending'
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
  updated_at   = NOW();

-- name: ClaimRefreshQueueTasks :many
WITH claimed AS (
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
          q.options_json::text, q.retry_count, q.next_run_at, q.created_at;

-- name: MarkRefreshQueueDone :exec
UPDATE refresh_queue
   SET status = 'done', last_error = NULL, updated_at = NOW()
 WHERE id = $1;

-- name: MarkRefreshQueueFailedFatal :exec
UPDATE refresh_queue
   SET status = 'failed', retry_count = retry_count + 1,
       last_error = $2, updated_at = NOW()
 WHERE id = $1;

-- name: MarkRefreshQueueFailedRetry :exec
UPDATE refresh_queue
   SET status = 'pending', retry_count = retry_count + 1,
       last_error = $2, next_run_at = NOW() + $3::interval,
       updated_at = NOW()
 WHERE id = $1;

-- name: ReconcileRefreshQueueRunning :execrows
UPDATE refresh_queue
   SET status = 'pending', updated_at = NOW()
 WHERE status = 'running';

-- name: ReconcileStaleRefreshQueueRunning :execrows
UPDATE refresh_queue
   SET status = 'pending', updated_at = NOW()
 WHERE status = 'running' AND updated_at < NOW() - INTERVAL '10 minutes';

-- name: PruneDoneRefreshQueueTasks :exec
DELETE FROM refresh_queue WHERE status = 'done' AND updated_at < NOW() - INTERVAL '7 days';

-- name: ListRecentRefreshQueueTasks :many
SELECT rq.id, rq.item_id::text,
       COALESCE(i.name, ''), COALESCE(i.type, ''),
       COALESCE(i.file_path, ''),
       COALESCE(i.series_name, ''),
       i.index_number, i.parent_index_number,
       rq.scope, rq.source, rq.status, rq.priority, rq.retry_count,
       COALESCE(rq.last_error, ''), rq.next_run_at, rq.updated_at
  FROM refresh_queue rq
  LEFT JOIN items i ON i.id = rq.item_id
 WHERE rq.status = $3
 ORDER BY CASE rq.status WHEN 'failed' THEN 0 WHEN 'running' THEN 1 WHEN 'pending' THEN 2 ELSE 3 END,
          rq.updated_at DESC
 LIMIT $1 OFFSET $2;

-- name: ListRecentRefreshQueueActiveTasks :many
SELECT rq.id, rq.item_id::text,
       COALESCE(i.name, ''), COALESCE(i.type, ''),
       COALESCE(i.file_path, ''),
       COALESCE(i.series_name, ''),
       i.index_number, i.parent_index_number,
       rq.scope, rq.source, rq.status, rq.priority, rq.retry_count,
       COALESCE(rq.last_error, ''), rq.next_run_at, rq.updated_at
  FROM refresh_queue rq
  LEFT JOIN items i ON i.id = rq.item_id
 WHERE rq.status IN ('failed', 'running')
 ORDER BY CASE rq.status WHEN 'failed' THEN 0 WHEN 'running' THEN 1 WHEN 'pending' THEN 2 ELSE 3 END,
          rq.updated_at DESC
 LIMIT $1 OFFSET $2;

-- name: CountRefreshQueueTasksByStatus :one
SELECT COUNT(*) FROM refresh_queue WHERE status = $1;

-- name: CountRefreshQueueActiveTasks :one
SELECT COUNT(*) FROM refresh_queue WHERE status IN ('failed', 'running');

-- name: GetRefreshQueueTaskDetail :one
SELECT rq.id, rq.item_id::text,
       COALESCE(i.name, ''), COALESCE(i.type, ''),
       COALESCE(i.file_path, ''),
       COALESCE(i.series_name, ''),
       i.index_number, i.parent_index_number,
       rq.scope, rq.source, rq.status, rq.priority, rq.retry_count,
       COALESCE(rq.last_error, ''), rq.next_run_at, rq.updated_at,
       rq.options_json::text
  FROM refresh_queue rq
  LEFT JOIN items i ON i.id = rq.item_id
 WHERE rq.id = $1;

-- name: RetryRefreshQueueTask :exec
UPDATE refresh_queue
   SET status = 'pending', next_run_at = NOW(), retry_count = 0,
       last_error = NULL, updated_at = NOW()
 WHERE id = $1 AND status = 'failed';

-- name: RetryAllFailedRefreshQueueTasks :execrows
UPDATE refresh_queue
   SET status = 'pending', next_run_at = NOW(), retry_count = 0,
       last_error = NULL, updated_at = NOW()
 WHERE status = 'failed';

-- name: CountRefreshQueueTasksByStatusGroup :many
SELECT status, COUNT(*) FROM refresh_queue GROUP BY status;
