-- name: UpsertScrapeQueueTask :exec
INSERT INTO scrape_queue (item_id, task_type, priority, status, next_run_at)
VALUES ($1::uuid, $2, $3, 'pending', NOW())
ON CONFLICT (item_id, task_type) DO UPDATE SET
  priority    = LEAST(scrape_queue.priority, EXCLUDED.priority),
  status      = CASE WHEN scrape_queue.status IN ('done', 'failed')
                     THEN 'pending' ELSE scrape_queue.status END,
  next_run_at = CASE WHEN scrape_queue.status IN ('done', 'failed')
                     THEN NOW() ELSE scrape_queue.next_run_at END,
  retry_count = CASE WHEN scrape_queue.status IN ('done', 'failed')
                     THEN 0 ELSE scrape_queue.retry_count END,
  last_error  = CASE WHEN scrape_queue.status IN ('done', 'failed')
                     THEN NULL ELSE scrape_queue.last_error END,
  detail_json = CASE WHEN scrape_queue.status IN ('done', 'failed')
                     THEN NULL ELSE scrape_queue.detail_json END,
  updated_at  = NOW();

-- name: UpsertScrapeQueueTasks :execrows
INSERT INTO scrape_queue (item_id, task_type, priority, status, next_run_at)
SELECT id::uuid, $2, $3, 'pending', NOW() FROM unnest($1::text[]) AS t(id)
ON CONFLICT (item_id, task_type) DO UPDATE SET
  priority    = LEAST(scrape_queue.priority, EXCLUDED.priority),
  status      = CASE WHEN scrape_queue.status IN ('done', 'failed')
                     THEN 'pending' ELSE scrape_queue.status END,
  next_run_at = CASE WHEN scrape_queue.status IN ('done', 'failed')
                     THEN NOW() ELSE scrape_queue.next_run_at END,
  retry_count = CASE WHEN scrape_queue.status IN ('done', 'failed')
                     THEN 0 ELSE scrape_queue.retry_count END,
  last_error  = CASE WHEN scrape_queue.status IN ('done', 'failed')
                     THEN NULL ELSE scrape_queue.last_error END,
  detail_json = CASE WHEN scrape_queue.status IN ('done', 'failed')
                     THEN NULL ELSE scrape_queue.detail_json END,
  updated_at  = NOW();

-- name: ClaimScrapeQueueTasks :many
WITH claimed AS (
    SELECT id FROM scrape_queue
    WHERE status = 'pending' AND next_run_at <= NOW()
    ORDER BY priority, next_run_at
    FOR UPDATE SKIP LOCKED
    LIMIT $1
)
UPDATE scrape_queue q
   SET status = 'running', updated_at = NOW()
  FROM claimed
 WHERE q.id = claimed.id
RETURNING q.id, q.item_id::text, q.task_type, q.priority, q.retry_count, q.next_run_at, q.created_at;

-- name: MarkScrapeQueueDone :exec
UPDATE scrape_queue
   SET status = 'done', last_error = NULL,
       request_url = NULL, response_status = NULL, response_sample = NULL, detail_json = NULL,
       updated_at = NOW()
 WHERE id = $1;

-- name: MarkScrapeQueueFailedFatal :exec
UPDATE scrape_queue
   SET status = 'failed', retry_count = retry_count + 1,
       last_error = $2,
       request_url = $3, response_status = $4, response_sample = $5, detail_json = $6::jsonb,
       updated_at = NOW()
 WHERE id = $1;

-- name: MarkScrapeQueueFailedRetry :exec
UPDATE scrape_queue
   SET status = 'pending', retry_count = retry_count + 1,
       last_error = $2, next_run_at = NOW() + $3::interval,
       request_url = $4, response_status = $5, response_sample = $6, detail_json = $7::jsonb,
       updated_at = NOW()
 WHERE id = $1;

-- name: ListRecentScrapeQueueTasks :many
SELECT sq.id, sq.item_id::text,
       COALESCE(i.name, ''), COALESCE(i.type, ''),
       COALESCE(i.file_path, ''),
       COALESCE(i.series_name, ''),
       i.index_number, i.parent_index_number,
       sq.task_type, sq.status, sq.priority, sq.retry_count,
       COALESCE(sq.last_error, ''), sq.next_run_at, sq.updated_at
  FROM scrape_queue sq
  LEFT JOIN items i ON i.id = sq.item_id
 WHERE sq.status = $3
 ORDER BY CASE sq.status WHEN 'failed' THEN 0 WHEN 'running' THEN 1 ELSE 2 END,
          sq.updated_at DESC
 LIMIT $1 OFFSET $2;

-- name: ListRecentScrapeQueueActiveTasks :many
SELECT sq.id, sq.item_id::text,
       COALESCE(i.name, ''), COALESCE(i.type, ''),
       COALESCE(i.file_path, ''),
       COALESCE(i.series_name, ''),
       i.index_number, i.parent_index_number,
       sq.task_type, sq.status, sq.priority, sq.retry_count,
       COALESCE(sq.last_error, ''), sq.next_run_at, sq.updated_at
  FROM scrape_queue sq
  LEFT JOIN items i ON i.id = sq.item_id
 WHERE sq.status IN ('failed', 'running')
 ORDER BY CASE sq.status WHEN 'failed' THEN 0 WHEN 'running' THEN 1 ELSE 2 END,
          sq.updated_at DESC
 LIMIT $1 OFFSET $2;

-- name: CountScrapeQueueTasksByStatus :one
SELECT COUNT(*) FROM scrape_queue WHERE status = $1;

-- name: CountScrapeQueueActiveTasks :one
SELECT COUNT(*) FROM scrape_queue WHERE status IN ('failed', 'running');

-- name: RetryScrapeQueueTask :exec
UPDATE scrape_queue
   SET status = 'pending', next_run_at = NOW(), retry_count = 0,
       last_error = NULL,
       request_url = NULL, response_status = NULL, response_sample = NULL, detail_json = NULL,
       updated_at = NOW()
 WHERE id = $1 AND status = 'failed';

-- name: RetryAllFailedScrapeQueueTasks :execrows
UPDATE scrape_queue
   SET status = 'pending', next_run_at = NOW(), retry_count = 0,
       last_error = NULL,
       request_url = NULL, response_status = NULL, response_sample = NULL, detail_json = NULL,
       updated_at = NOW()
 WHERE status = 'failed';

-- name: GetScrapeQueueTaskDetail :one
SELECT sq.id, sq.item_id::text, COALESCE(i.name, ''), COALESCE(i.type, ''),
       COALESCE(i.file_path, ''), COALESCE(i.series_name, ''),
       i.index_number, i.parent_index_number,
       sq.task_type, sq.status, sq.priority, sq.retry_count,
       COALESCE(sq.last_error, ''), sq.next_run_at, sq.updated_at,
       sq.request_url, sq.response_status, sq.response_sample, sq.detail_json
  FROM scrape_queue sq
  LEFT JOIN items i ON i.id = sq.item_id
 WHERE sq.id = $1;

-- name: ReconcileScrapeQueueRunning :execrows
UPDATE scrape_queue
   SET status = 'pending', updated_at = NOW()
 WHERE status = 'running';

-- name: ReconcileStaleScrapeQueueRunning :execrows
UPDATE scrape_queue
   SET status = 'pending', updated_at = NOW()
 WHERE status = 'running' AND updated_at < NOW() - INTERVAL '10 minutes';

-- name: PruneDoneScrapeQueueTasks :exec
DELETE FROM scrape_queue WHERE status = 'done' AND updated_at < NOW() - INTERVAL '7 days';

-- name: CountScrapeQueueTasksByStatusGroup :many
SELECT status, COUNT(*) FROM scrape_queue GROUP BY status;
