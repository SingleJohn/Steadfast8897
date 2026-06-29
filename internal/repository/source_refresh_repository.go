package repository

import (
	"context"
	"encoding/json"
	"time"
)

// SourceRefreshTask 是从 source_refresh_queue 认领到的一条待处理任务。
type SourceRefreshTask struct {
	ID         int64
	TaskType   string
	TargetKind string
	TargetID   int64
	Payload    json.RawMessage
	Priority   int16
	RetryCount int16
	NextRunAt  time.Time
	CreatedAt  time.Time
}

// EnqueueSourceRefresh 入队一条刷新任务。UNIQUE(task_type,target_kind,target_id) 去重：
// 已存在时取更高优先级(数值更小)；非 running 的旧任务(done/failed/pending)重新激活为 pending 立即重跑，
// 正在 running 的不打断。
func (r *SourceRepository) EnqueueSourceRefresh(ctx context.Context, taskType, targetKind string, targetID int64, priority int16, payload []byte) error {
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO source_refresh_queue (task_type, target_kind, target_id, priority, payload, status, next_run_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, 'pending', NOW())
		ON CONFLICT (task_type, target_kind, target_id) DO UPDATE SET
			priority    = LEAST(source_refresh_queue.priority, EXCLUDED.priority),
			payload     = EXCLUDED.payload,
			status      = CASE WHEN source_refresh_queue.status = 'running' THEN source_refresh_queue.status ELSE 'pending' END,
			retry_count = CASE WHEN source_refresh_queue.status = 'running' THEN source_refresh_queue.retry_count ELSE 0 END,
			next_run_at = CASE WHEN source_refresh_queue.status = 'running' THEN source_refresh_queue.next_run_at ELSE NOW() END,
			last_error  = CASE WHEN source_refresh_queue.status = 'running' THEN source_refresh_queue.last_error ELSE NULL END,
			updated_at  = NOW()
	`, taskType, targetKind, targetID, priority, payload)
	return err
}

// ClaimSourceRefresh 批量认领待处理任务并原子标记 running，FOR UPDATE SKIP LOCKED 支持多 worker 并发。
func (r *SourceRepository) ClaimSourceRefresh(ctx context.Context, limit int) ([]SourceRefreshTask, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := r.pool.Query(ctx, `
		UPDATE source_refresh_queue SET status = 'running', updated_at = NOW()
		WHERE id IN (
			SELECT id FROM source_refresh_queue
			WHERE status = 'pending' AND next_run_at <= NOW()
			ORDER BY priority, next_run_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, task_type, target_kind, target_id, payload, priority, retry_count, next_run_at, created_at`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SourceRefreshTask{}
	for rows.Next() {
		var t SourceRefreshTask
		if err := rows.Scan(&t.ID, &t.TaskType, &t.TargetKind, &t.TargetID, &t.Payload, &t.Priority, &t.RetryCount, &t.NextRunAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DoneSourceRefresh 标记任务成功完成。
func (r *SourceRepository) DoneSourceRefresh(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE source_refresh_queue SET status = 'done', last_error = NULL, updated_at = NOW() WHERE id = $1`, id)
	return err
}

// FailSourceRefresh 失败后按退避时间排下次重试(回到 pending)。
func (r *SourceRepository) FailSourceRefresh(ctx context.Context, id int64, backoff time.Duration, errMsg string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE source_refresh_queue
		   SET status = 'pending', retry_count = retry_count + 1, last_error = $2,
		       next_run_at = NOW() + make_interval(secs => $3), updated_at = NOW()
		 WHERE id = $1`, id, errMsg, backoff.Seconds())
	return err
}

// FailFatalSourceRefresh 标记终态 failed，不再重试。
func (r *SourceRepository) FailFatalSourceRefresh(ctx context.Context, id int64, errMsg string) error {
	_, err := r.pool.Exec(ctx, `UPDATE source_refresh_queue SET status = 'failed', last_error = $2, updated_at = NOW() WHERE id = $1`, id, errMsg)
	return err
}

// ReconcileSourceRefreshOnStartup 启动时把所有 running 任务重置为 pending(孤儿清理)。
func (r *SourceRepository) ReconcileSourceRefreshOnStartup(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE source_refresh_queue SET status = 'pending', updated_at = NOW() WHERE status = 'running'`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ReconcileStaleSourceRefresh 运行期兜底：把卡住超过 15 分钟的 running 重置为 pending。
func (r *SourceRepository) ReconcileStaleSourceRefresh(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE source_refresh_queue SET status = 'pending', updated_at = NOW() WHERE status = 'running' AND updated_at < NOW() - INTERVAL '15 minutes'`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// PruneDoneSourceRefresh 删除 done 超过 3 天的任务，防止表无限增长。
func (r *SourceRepository) PruneDoneSourceRefresh(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM source_refresh_queue WHERE status = 'done' AND updated_at < NOW() - INTERVAL '3 days'`)
	return err
}

// ListStaleSeriesItemIDs 列出需要追更的连载剧：detail 已加载且超过 TTL（或从未追更过）。
func (r *SourceRepository) ListStaleSeriesItemIDs(ctx context.Context, ttl time.Duration, limit int) ([]int64, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id FROM source_items
		 WHERE LOWER(item_type) = 'series'
		   AND detail_loaded = TRUE
		   AND (detail_refreshed_at IS NULL OR detail_refreshed_at < NOW() - make_interval(secs => $1))
		 ORDER BY detail_refreshed_at ASC NULLS FIRST, updated_at DESC
		 LIMIT $2`, ttl.Seconds(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SourceItemDetailRefreshedAt 读取单个 item 的最后追更时间，供 detail TTL 判断(避免改动共享的 SourceItem 扫描)。
func (r *SourceRepository) SourceItemDetailRefreshedAt(ctx context.Context, id int64) (*time.Time, error) {
	var ts *time.Time
	if err := r.pool.QueryRow(ctx, `SELECT detail_refreshed_at FROM source_items WHERE id = $1`, id).Scan(&ts); err != nil {
		return nil, err
	}
	return ts, nil
}

// MarkSourceItemDetailRefreshed 在成功重拉 detail 后记录追更时间。
func (r *SourceRepository) MarkSourceItemDetailRefreshed(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE source_items SET detail_refreshed_at = NOW() WHERE id = $1`, id)
	return err
}

// SourceRefreshQueueStats 给前端/观测用：各状态计数。
func (r *SourceRepository) SourceRefreshQueueStats(ctx context.Context) (map[string]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT status, COUNT(*) FROM source_refresh_queue GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{"pending": 0, "running": 0, "done": 0, "failed": 0}
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		out[status] = count
	}
	return out, rows.Err()
}
