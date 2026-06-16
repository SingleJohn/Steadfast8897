package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
)

// ScrapeTaskType 是 scrape_queue.task_type 的枚举。
type ScrapeTaskType string

const (
	ScrapeTaskIdentify            ScrapeTaskType = "identify"
	ScrapeTaskBackfillQuality     ScrapeTaskType = "backfill_quality"
	ScrapeTaskBackfillEpisodeName ScrapeTaskType = "backfill_episode_name"
	ScrapeTaskBackfillEpisodeImg  ScrapeTaskType = "backfill_episode_image"
	// ScrapeTaskBackfillActorImg:给 NFO 扫入的 Series/Movie 补演员头像 URL。
	// 只填 cast_members.image_url IS NULL 的行,不覆盖已有 URL。
	ScrapeTaskBackfillActorImg ScrapeTaskType = "backfill_actor_images"
	ScrapeTaskRefresh          ScrapeTaskType = "refresh"
)

// Priority 默认值约定(数值越小越优先):
//
//	0 = refresh(用户手动"重新刮削",最高)
//	1 = identify(ingest 新增 item 后自动入队)
//	3 = scan 触发的任务(Phase 3)
//	5 = backfill(BackfillTask 批量入队)
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
	repo *repository.ScrapeQueueRepository
}

func NewScrapeQueue(pool *pgxpool.Pool) *ScrapeQueue {
	return &ScrapeQueue{repo: repository.NewScrapeQueueRepository(pool)}
}

// Enqueue 入队一条任务。UNIQUE(item_id, task_type) 会自动去重:
// 同 item 同类型已在队列(不论 pending/running/failed)就不重复入队,
// 但允许降低 priority(如手动 refresh 比 auto identify 优先)。
func (q *ScrapeQueue) Enqueue(ctx context.Context, itemID string, taskType ScrapeTaskType, priority int16) error {
	return q.repo.Enqueue(ctx, itemID, string(taskType), priority)
}

// EnqueueBatch 一次入队多条(同 task_type / priority),比循环 Enqueue 少 N 次 round-trip。
func (q *ScrapeQueue) EnqueueBatch(ctx context.Context, itemIDs []string, taskType ScrapeTaskType, priority int16) (int64, error) {
	if len(itemIDs) == 0 {
		return 0, nil
	}
	return q.repo.EnqueueBatch(ctx, itemIDs, string(taskType), priority)
}

// Claim 批量取出 limit 条待处理任务,原子地标记为 running。
// 使用 FOR UPDATE SKIP LOCKED 让多个 worker 并发 Claim 不互相阻塞。
func (q *ScrapeQueue) Claim(ctx context.Context, limit int) ([]QueueTask, error) {
	rows, err := q.repo.Claim(ctx, limit)
	if err != nil {
		return nil, err
	}
	tasks := make([]QueueTask, 0, len(rows))
	for _, row := range rows {
		tasks = append(tasks, QueueTask{
			ID:         row.ID,
			ItemID:     row.ItemID,
			TaskType:   ScrapeTaskType(row.TaskType),
			Priority:   row.Priority,
			RetryCount: row.RetryCount,
			NextRunAt:  row.NextRunAt,
			CreatedAt:  row.CreatedAt,
		})
	}
	return tasks, nil
}

// Done 标记成功完成。保留一段时间供审计,后续由 PruneDone 清理。
// 同时清空 HTTP 诊断列和结构化 detail_json,避免前端看到过时信息。
func (q *ScrapeQueue) Done(ctx context.Context, id int64) {
	_ = q.repo.Done(ctx, id)
}

// FailFatal 标记任务为终态 failed,不走退避重试。
// 用于明确不可能靠重试解决的错误(no match / 非 TMDB 源无法映射 / 类型不支持等)。
// 节省 worker 资源和代理配额,避免 Pending 被一堆注定失败的 item 占住。
func (q *ScrapeQueue) FailFatal(ctx context.Context, id int64, errMsg string, diag *ScrapeDiag) {
	var reqURL, respBody interface{}
	var respStatus interface{}
	var detailJSON interface{}
	if diag != nil && diag.Attempts > 0 {
		reqURL = diag.URL
		if diag.Status > 0 {
			respStatus = diag.Status
		}
		if diag.Body != "" {
			respBody = diag.Body
		}
	}
	if diag != nil && diag.Detail != "" {
		detailJSON = diag.Detail
	}
	_ = q.repo.FailFatal(ctx, id, repository.ScrapeQueueFailure{
		Error:          truncateError(errMsg),
		RequestURL:     stringFromAny(reqURL),
		ResponseStatus: intFromAny(respStatus),
		ResponseSample: stringFromAny(respBody),
		DetailJSON:     bytesFromAny(detailJSON),
	})
}

// Fail 标记失败并按指数退避排下次运行。超过 maxRetry 就落成 failed。
// diag 允许为 nil(非 HTTP 任务或上游没注入时三列写 NULL)。
func (q *ScrapeQueue) Fail(ctx context.Context, id int64, retryCount int16, maxRetry int16, errMsg string, diag *ScrapeDiag) {
	var reqURL, respBody interface{}
	var respStatus interface{}
	var detailJSON interface{}
	if diag != nil && diag.Attempts > 0 {
		reqURL = diag.URL
		if diag.Status > 0 {
			respStatus = diag.Status
		}
		if diag.Body != "" {
			respBody = diag.Body
		}
	}
	if diag != nil && diag.Detail != "" {
		detailJSON = diag.Detail
	}

	if retryCount+1 >= maxRetry {
		_ = q.repo.FailFatal(ctx, id, repository.ScrapeQueueFailure{
			Error:          truncateError(errMsg),
			RequestURL:     stringFromAny(reqURL),
			ResponseStatus: intFromAny(respStatus),
			ResponseSample: stringFromAny(respBody),
			DetailJSON:     bytesFromAny(detailJSON),
		})
		return
	}
	backoff := retryBackoff(retryCount + 1)
	_ = q.repo.FailRetry(ctx, id, backoff, repository.ScrapeQueueFailure{
		Error:          truncateError(errMsg),
		RequestURL:     stringFromAny(reqURL),
		ResponseStatus: intFromAny(respStatus),
		ResponseSample: stringFromAny(respBody),
		DetailJSON:     bytesFromAny(detailJSON),
	})
}

// RecentTask 给前端队列面板用:最近失败/运行中任务。
// SeriesName / IndexNumber / ParentIndexNumber 用于 Episode/Season 的上下文展示
// (例如 "某剧 S01E05"),Movie/Series 顶层时为空。FilePath 帮助定位物理文件。
type RecentTask struct {
	ID                int64          `json:"id"`
	ItemID            string         `json:"item_id"`
	ItemName          string         `json:"item_name"`
	ItemType          string         `json:"item_type"`
	FilePath          string         `json:"file_path,omitempty"`
	SeriesName        string         `json:"series_name,omitempty"`
	IndexNumber       *int32         `json:"index_number,omitempty"`
	ParentIndexNumber *int32         `json:"parent_index_number,omitempty"`
	TaskType          ScrapeTaskType `json:"task_type"`
	Status            string         `json:"status"`
	Priority          int16          `json:"priority"`
	RetryCount        int16          `json:"retry_count"`
	LastError         string         `json:"last_error,omitempty"`
	NextRunAt         time.Time      `json:"next_run_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

// Recent 返回 scrape_queue 中的任务。
//
//	status = "" 表示 failed+running 合并(failed 优先);否则按该状态过滤
//	limit/offset 分页(limit 1-500,offset >=0)
//
// SQL JOIN items 带出 file_path / series_name / index 等,前端展示时用来
// 定位到"哪个剧集的哪一集"以及"物理路径",方便排查刮削失败。
func (q *ScrapeQueue) Recent(ctx context.Context, status string, limit, offset int) ([]RecentTask, error) {
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
	out := make([]RecentTask, 0, len(repoRows))
	for _, row := range repoRows {
		out = append(out, RecentTask{
			ID:                row.ID,
			ItemID:            row.ItemID,
			ItemName:          row.ItemName,
			ItemType:          row.ItemType,
			FilePath:          row.FilePath,
			SeriesName:        row.SeriesName,
			IndexNumber:       row.IndexNumber,
			ParentIndexNumber: row.ParentIndexNumber,
			TaskType:          ScrapeTaskType(row.TaskType),
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

// RecentCount 给分页器用,返回指定 status 的总数。status="" 时按 failed+running 合并计。
func (q *ScrapeQueue) RecentCount(ctx context.Context, status string) (int64, error) {
	return q.repo.RecentCount(ctx, status)
}

// RetryTask 把单个 failed 任务重置为 pending(立即重试)。
// 同时清空诊断字段,下次失败时由 worker 重新写入。
func (q *ScrapeQueue) RetryTask(ctx context.Context, id int64) error {
	return q.repo.RetryTask(ctx, id)
}

// RetryAllFailed 批量把所有 failed 任务重置为 pending。返回被重置的数量。
func (q *ScrapeQueue) RetryAllFailed(ctx context.Context) (int64, error) {
	return q.repo.RetryAllFailed(ctx)
}

// TaskDetail 是 Recent 单行 + HTTP/结构化诊断字段,详情接口专用。
// response_sample 可能几十 KB,列表接口不返回这个,只在点开时按 id 拉取。
type TaskDetail struct {
	RecentTask
	RequestURL     string `json:"request_url,omitempty"`
	ResponseStatus *int   `json:"response_status,omitempty"`
	ResponseSample string `json:"response_sample,omitempty"`
	DetailJSON     any    `json:"detail_json,omitempty"`
}

// GetTaskDetail 按 id 拉一条任务的完整信息(含 response_sample)。
func (q *ScrapeQueue) GetTaskDetail(ctx context.Context, id int64) (*TaskDetail, error) {
	row, err := q.repo.GetTaskDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	return &TaskDetail{
		RecentTask: RecentTask{
			ID:                row.ID,
			ItemID:            row.ItemID,
			ItemName:          row.ItemName,
			ItemType:          row.ItemType,
			FilePath:          row.FilePath,
			SeriesName:        row.SeriesName,
			IndexNumber:       row.IndexNumber,
			ParentIndexNumber: row.ParentIndexNumber,
			TaskType:          ScrapeTaskType(row.TaskType),
			Status:            row.Status,
			Priority:          row.Priority,
			RetryCount:        row.RetryCount,
			LastError:         row.LastError,
			NextRunAt:         row.NextRunAt,
			UpdatedAt:         row.UpdatedAt,
		},
		RequestURL:     row.RequestURL,
		ResponseStatus: row.ResponseStatus,
		ResponseSample: row.ResponseSample,
		DetailJSON:     row.DetailJSON,
	}, nil
}

// ReconcileOnStartup 启动时无条件把所有 running 任务重置为 pending。
// 启动时刻新的 worker goroutine 还没起来,不可能有"合法运行中"的任务 —— 旧进程
// 留下的 running 全部是孤儿,哪怕 updated_at 是 1 秒前的也一样救不回来。
// 之前用 `updated_at < NOW() - 10 min` 过滤会漏掉"重启前刚刚 Claim 的任务",
// 让它们永久卡在 running 状态(前端"运行中"计数虚高,实际永不执行)。
func (q *ScrapeQueue) ReconcileOnStartup(ctx context.Context) error {
	count, err := q.repo.ReconcileOnStartup(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		slog.Info("[ScrapeQueue] Reconciled orphan running tasks at startup", "count", count)
	}
	return nil
}

// ReconcileStaleRunning 运行中的兜底清理:针对 updated_at > 10 分钟的 running 任务。
// 这种情况只可能来自 goroutine panic / deadlock 导致 task 永远没走到 Done/Fail。
// 由 ScrapeWorker 每 5 分钟调一次,跟启动清理互补。
func (q *ScrapeQueue) ReconcileStaleRunning(ctx context.Context) error {
	count, err := q.repo.ReconcileStaleRunning(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		slog.Warn("[ScrapeQueue] Reconciled stale running tasks during runtime",
			"count", count,
			"hint", "goroutine may have panicked or deadlocked without updating status")
	}
	return nil
}

// PruneDone 定期删除 done 状态超过 7 天的任务,防止表无限增长。
func (q *ScrapeQueue) PruneDone(ctx context.Context) error {
	return q.repo.PruneDone(ctx)
}

// QueueStats 给观测/管理面板用(Phase 4 的队列视图)。
type QueueStats struct {
	Pending int64
	Running int64
	Done    int64
	Failed  int64
}

func (q *ScrapeQueue) Stats(ctx context.Context) (QueueStats, error) {
	stats, err := q.repo.Stats(ctx)
	if err != nil {
		return QueueStats{}, err
	}
	return QueueStats(stats), nil
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

func stringFromAny(v any) string {
	s, _ := v.(string)
	return s
}

func intFromAny(v any) int {
	n, _ := v.(int)
	return n
}

func bytesFromAny(v any) []byte {
	switch raw := v.(type) {
	case []byte:
		return raw
	case string:
		return []byte(raw)
	default:
		return nil
	}
}
