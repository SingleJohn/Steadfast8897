package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ScrapeTaskType 是 scrape_queue.task_type 的枚举。
type ScrapeTaskType string

const (
	ScrapeTaskIdentify            ScrapeTaskType = "identify"
	ScrapeTaskBackfillQuality     ScrapeTaskType = "backfill_quality"
	ScrapeTaskBackfillEpisodeName ScrapeTaskType = "backfill_episode_name"
	ScrapeTaskBackfillEpisodeImg  ScrapeTaskType = "backfill_episode_image"
	ScrapeTaskRefresh             ScrapeTaskType = "refresh"
)

// Priority 默认值约定(数值越小越优先):
//   0 = refresh(用户手动"重新刮削",最高)
//   1 = identify(ingest 新增 item 后自动入队)
//   3 = scan 触发的任务(Phase 3)
//   5 = backfill(BackfillTask 批量入队)
const (
	ScrapePriorityRefresh  = 0
	ScrapePriorityIdentify = 1
	ScrapePriorityScan     = 3
	ScrapePriorityBackfill = 5
)

// QueueTask 是从 scrape_queue Claim 到一个待处理任务。
// 命名避免与 tmdb.go 的 ScrapeTask(UI 刮削任务)冲突。
type QueueTask struct {
	ID         int64
	ItemID     string
	TaskType   ScrapeTaskType
	Priority   int16
	RetryCount int16
	NextRunAt  time.Time
	CreatedAt  time.Time
}

// ScrapeQueue 是对 scrape_queue 表的薄封装,提供入队 / 认领 / 完成 / 失败重试。
type ScrapeQueue struct {
	pool *pgxpool.Pool
}

func NewScrapeQueue(pool *pgxpool.Pool) *ScrapeQueue {
	return &ScrapeQueue{pool: pool}
}

// Enqueue 入队一条任务。UNIQUE(item_id, task_type) 会自动去重:
// 同 item 同类型已在队列(不论 pending/running/failed)就不重复入队,
// 但允许降低 priority(如手动 refresh 比 auto identify 优先)。
func (q *ScrapeQueue) Enqueue(ctx context.Context, itemID string, taskType ScrapeTaskType, priority int16) error {
	_, err := q.pool.Exec(ctx,
		`INSERT INTO scrape_queue (item_id, task_type, priority, status, next_run_at)
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
		   updated_at  = NOW()`,
		itemID, string(taskType), priority,
	)
	return err
}

// EnqueueBatch 一次入队多条(同 task_type / priority),比循环 Enqueue 少 N 次 round-trip。
func (q *ScrapeQueue) EnqueueBatch(ctx context.Context, itemIDs []string, taskType ScrapeTaskType, priority int16) (int64, error) {
	if len(itemIDs) == 0 {
		return 0, nil
	}
	tag, err := q.pool.Exec(ctx,
		`INSERT INTO scrape_queue (item_id, task_type, priority, status, next_run_at)
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
		   updated_at  = NOW()`,
		itemIDs, string(taskType), priority,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Claim 批量取出 limit 条待处理任务,原子地标记为 running。
// 使用 FOR UPDATE SKIP LOCKED 让多个 worker 并发 Claim 不互相阻塞。
func (q *ScrapeQueue) Claim(ctx context.Context, limit int) ([]QueueTask, error) {
	rows, err := q.pool.Query(ctx,
		`WITH claimed AS (
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
		 RETURNING q.id, q.item_id::text, q.task_type, q.priority, q.retry_count, q.next_run_at, q.created_at`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []QueueTask
	for rows.Next() {
		var t QueueTask
		var tt string
		if err := rows.Scan(&t.ID, &t.ItemID, &tt, &t.Priority, &t.RetryCount, &t.NextRunAt, &t.CreatedAt); err != nil {
			continue
		}
		t.TaskType = ScrapeTaskType(tt)
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// Done 标记成功完成。保留一段时间供审计,后续由 PruneDone 清理。
func (q *ScrapeQueue) Done(ctx context.Context, id int64) {
	_, _ = q.pool.Exec(ctx,
		`UPDATE scrape_queue SET status = 'done', last_error = NULL, updated_at = NOW() WHERE id = $1`,
		id)
}

// Fail 标记失败并按指数退避排下次运行。超过 maxRetry 就落成 failed。
func (q *ScrapeQueue) Fail(ctx context.Context, id int64, retryCount int16, maxRetry int16, errMsg string) {
	if retryCount+1 >= maxRetry {
		_, _ = q.pool.Exec(ctx,
			`UPDATE scrape_queue
			    SET status = 'failed', retry_count = retry_count + 1,
			        last_error = $2, updated_at = NOW()
			  WHERE id = $1`,
			id, truncateError(errMsg))
		return
	}
	backoff := retryBackoff(retryCount + 1)
	_, _ = q.pool.Exec(ctx,
		`UPDATE scrape_queue
		    SET status = 'pending', retry_count = retry_count + 1,
		        last_error = $2, next_run_at = NOW() + $3::interval,
		        updated_at = NOW()
		  WHERE id = $1`,
		id, truncateError(errMsg), fmt.Sprintf("%d seconds", int(backoff.Seconds())))
}

// ReconcileOnStartup 把崩溃前遗留的 running 任务(updated_at 超过 10 分钟)重置为 pending,
// 避免永久卡死。调用时机:main.go 启动 worker 前。
func (q *ScrapeQueue) ReconcileOnStartup(ctx context.Context) error {
	tag, err := q.pool.Exec(ctx,
		`UPDATE scrape_queue
		    SET status = 'pending', updated_at = NOW()
		  WHERE status = 'running' AND updated_at < NOW() - INTERVAL '10 minutes'`)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		slog.Info("[ScrapeQueue] Reconciled stale running tasks", "count", tag.RowsAffected())
	}
	return nil
}

// PruneDone 定期删除 done 状态超过 7 天的任务,防止表无限增长。
func (q *ScrapeQueue) PruneDone(ctx context.Context) error {
	_, err := q.pool.Exec(ctx,
		`DELETE FROM scrape_queue WHERE status = 'done' AND updated_at < NOW() - INTERVAL '7 days'`)
	return err
}

// QueueStats 给观测/管理面板用(Phase 4 的队列视图)。
type QueueStats struct {
	Pending int64
	Running int64
	Done    int64
	Failed  int64
}

func (q *ScrapeQueue) Stats(ctx context.Context) (QueueStats, error) {
	var s QueueStats
	rows, err := q.pool.Query(ctx,
		`SELECT status, COUNT(*) FROM scrape_queue GROUP BY status`)
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

// retryBackoff: 2^retry 分钟,上限 32 分钟。
// retry 1 → 2min, 2 → 4, 3 → 8, 4 → 16, 5 → 32
func retryBackoff(retryCount int16) time.Duration {
	if retryCount < 1 {
		retryCount = 1
	}
	if retryCount > 5 {
		retryCount = 5
	}
	mins := 1 << retryCount // 2, 4, 8, 16, 32
	return time.Duration(mins) * time.Minute
}

func truncateError(s string) string {
	const maxErr = 2000
	if len(s) > maxErr {
		return s[:maxErr] + "...[truncated]"
	}
	return s
}
