package taskcenter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RunRow 对应 task_runs 表一行。所有时间字段用 UnixMilli，
// 0 表示 NULL（未完成的行 CompletedAt=0）。
type RunRow struct {
	ID          int64            `json:"id"`
	Kind        Kind             `json:"kind"`
	Stage       string           `json:"stage,omitempty"`
	ParentID    int64            `json:"parentId,omitempty"`
	Status      Status           `json:"status"`
	Trigger     Trigger          `json:"trigger"`
	Total       int64            `json:"total"`
	Processed   int64            `json:"processed"`
	Success     int64            `json:"success"`
	Failed      int64            `json:"failed"`
	Counters    map[string]int64 `json:"counters,omitempty"`
	Message     string           `json:"message,omitempty"`
	Error       string           `json:"error,omitempty"`
	Payload     map[string]any   `json:"payload,omitempty"`
	StartedAt   int64            `json:"startedAt"`
	CompletedAt int64            `json:"completedAt,omitempty"`
	DurationMs  int64            `json:"durationMs,omitempty"`
}

// BeginParams 是 Begin 的输入参数，避免参数列表太长。
type BeginParams struct {
	Kind     Kind
	Stage    string // 子 run 的 stage；父 run 或单行任务留空
	ParentID int64  // 0 表示无父
	Trigger  Trigger
	Total    int64          // 已知总量时填；未知填 0，进度回写阶段再更新
	Payload  map[string]any // 启动参数（threads/stages/channel 等）
}

// Begin 在 task_runs 表插入一条 running 记录，返回 run ID。
// 适配器在 Start 成功启动后（进入 goroutine 之前）调用。
func Begin(ctx context.Context, db *pgxpool.Pool, p BeginParams) (int64, error) {
	payloadJSON, err := marshalJSONB(p.Payload)
	if err != nil {
		return 0, fmt.Errorf("marshal payload: %w", err)
	}

	var parent any
	if p.ParentID > 0 {
		parent = p.ParentID
	}
	var stage any
	if p.Stage != "" {
		stage = p.Stage
	}

	var id int64
	err = db.QueryRow(ctx, `
		INSERT INTO task_runs (kind, stage, parent_id, status, trigger, total, payload)
		VALUES ($1, $2, $3, 'running', $4, $5, $6)
		RETURNING id
	`, string(p.Kind), stage, parent, string(p.Trigger), p.Total, payloadJSON).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert task_run: %w", err)
	}
	return id, nil
}

// BeginQueued 为 Backfill 那种"一次启动即预建多个 stage 行"的场景使用：
// 插入 status=queued 的记录，后续调用 MarkRunning 推进到 running。
func BeginQueued(ctx context.Context, db *pgxpool.Pool, p BeginParams) (int64, error) {
	payloadJSON, err := marshalJSONB(p.Payload)
	if err != nil {
		return 0, fmt.Errorf("marshal payload: %w", err)
	}
	var parent any
	if p.ParentID > 0 {
		parent = p.ParentID
	}
	var stage any
	if p.Stage != "" {
		stage = p.Stage
	}

	var id int64
	err = db.QueryRow(ctx, `
		INSERT INTO task_runs (kind, stage, parent_id, status, trigger, total, payload)
		VALUES ($1, $2, $3, 'queued', $4, $5, $6)
		RETURNING id
	`, string(p.Kind), stage, parent, string(p.Trigger), p.Total, payloadJSON).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert queued task_run: %w", err)
	}
	return id, nil
}

// MarkRunning 将 queued 行推进到 running 并重置开始时间。
func MarkRunning(ctx context.Context, db *pgxpool.Pool, runID int64) error {
	_, err := db.Exec(ctx, `
		UPDATE task_runs
		SET status = 'running', started_at = NOW()
		WHERE id = $1 AND status = 'queued'
	`, runID)
	return err
}

// UpdateProgress 定期回写进度（适配器可选择节流，如每 2 秒或每 100 项一次）。
// 不存在的 counters key 会被整体替换为传入的 map。
func UpdateProgress(ctx context.Context, db *pgxpool.Pool, runID int64, processed, success, failed, total int64, counters map[string]int64) error {
	countersJSON, err := marshalJSONB(counters)
	if err != nil {
		return fmt.Errorf("marshal counters: %w", err)
	}
	_, err = db.Exec(ctx, `
		UPDATE task_runs
		SET processed = $2, success = $3, failed = $4, total = $5, counters = $6
		WHERE id = $1
	`, runID, processed, success, failed, total, countersJSON)
	return err
}

// End 将 run 标为终止状态（succeeded/failed/cancelled），记录 completed_at 与 duration。
// 已终止的行再次调用为 no-op（WHERE 过滤）。
func End(ctx context.Context, db *pgxpool.Pool, runID int64, status Status, message, errMsg string) error {
	if !status.Terminal() {
		return fmt.Errorf("End called with non-terminal status: %s", status)
	}
	_, err := db.Exec(ctx, `
		UPDATE task_runs
		SET status       = $2,
		    message      = NULLIF($3, ''),
		    error        = NULLIF($4, ''),
		    completed_at = NOW(),
		    duration_ms  = GREATEST(0, EXTRACT(EPOCH FROM (NOW() - started_at))::BIGINT * 1000)
		WHERE id = $1 AND status IN ('queued','running','stopping')
	`, runID, string(status), message, errMsg)
	return err
}

// HistoryFilter 用于 /Tasks/history 查询。
type HistoryFilter struct {
	Kind     Kind   // 可空
	ParentID *int64 // 非 nil 时精确匹配（0 表示只查顶层）
	Limit    int    // 默认 100，上限 1000
}

// History 查询历史运行记录，按 started_at DESC 排序。
func History(ctx context.Context, db *pgxpool.Pool, f HistoryFilter) ([]RunRow, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	q := `SELECT id, kind, stage, parent_id, status, trigger,
	             total, processed, success, failed, counters,
	             message, error, payload, started_at, completed_at, duration_ms
	      FROM task_runs WHERE 1=1`
	args := []any{}
	if f.Kind != "" {
		args = append(args, string(f.Kind))
		q += fmt.Sprintf(" AND kind = $%d", len(args))
	}
	if f.ParentID != nil {
		if *f.ParentID == 0 {
			q += " AND parent_id IS NULL"
		} else {
			args = append(args, *f.ParentID)
			q += fmt.Sprintf(" AND parent_id = $%d", len(args))
		}
	}
	args = append(args, limit)
	q += fmt.Sprintf(" ORDER BY started_at DESC LIMIT $%d", len(args))

	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]RunRow, 0, limit)
	for rows.Next() {
		var (
			r            RunRow
			stage        *string
			parent       *int64
			message      *string
			errMsg       *string
			completedAt  *time.Time
			durationMs   *int64
			startedAt    time.Time
			kindStr      string
			statusStr    string
			triggerStr   string
			countersJSON []byte
			payloadJSON  []byte
		)
		if err := rows.Scan(
			&r.ID, &kindStr, &stage, &parent, &statusStr, &triggerStr,
			&r.Total, &r.Processed, &r.Success, &r.Failed, &countersJSON,
			&message, &errMsg, &payloadJSON, &startedAt, &completedAt, &durationMs,
		); err != nil {
			return nil, err
		}
		r.Kind = Kind(kindStr)
		r.Status = Status(statusStr)
		r.Trigger = Trigger(triggerStr)
		if stage != nil {
			r.Stage = *stage
		}
		if parent != nil {
			r.ParentID = *parent
		}
		if message != nil {
			r.Message = *message
		}
		if errMsg != nil {
			r.Error = *errMsg
		}
		r.StartedAt = startedAt.UnixMilli()
		if completedAt != nil {
			r.CompletedAt = completedAt.UnixMilli()
		}
		if durationMs != nil {
			r.DurationMs = *durationMs
		}
		if len(countersJSON) > 0 {
			_ = json.Unmarshal(countersJSON, &r.Counters)
		}
		if len(payloadJSON) > 0 {
			_ = json.Unmarshal(payloadJSON, &r.Payload)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReconcileOnStartup 服务启动时把上次没关闭的 running/stopping/queued 行
// 标为 cancelled，避免历史表里一堆"永远在跑"的僵尸记录。
// 进程崩溃或容器重启会触发此路径。
func ReconcileOnStartup(ctx context.Context, db *pgxpool.Pool) error {
	_, err := db.Exec(ctx, `
		UPDATE task_runs
		SET status       = 'cancelled',
		    error        = COALESCE(error, 'interrupted by server restart'),
		    completed_at = NOW(),
		    duration_ms  = GREATEST(0, EXTRACT(EPOCH FROM (NOW() - started_at))::BIGINT * 1000)
		WHERE status IN ('queued','running','stopping')
		  AND kind <> 'update'
	`)
	return err
}

func ReconcileUpdateOnStartup(ctx context.Context, db *pgxpool.Pool, status Status, message, errMsg string) error {
	if !status.Terminal() {
		return fmt.Errorf("ReconcileUpdateOnStartup called with non-terminal status: %s", status)
	}
	_, err := db.Exec(ctx, `
		UPDATE task_runs
		SET status       = $1,
		    message      = NULLIF($2, ''),
		    error        = NULLIF($3, ''),
		    completed_at = NOW(),
		    duration_ms  = GREATEST(0, EXTRACT(EPOCH FROM (NOW() - started_at))::BIGINT * 1000)
		WHERE kind = 'update'
		  AND status IN ('queued','running','stopping')
	`, string(status), message, errMsg)
	return err
}

func marshalJSONB(v any) ([]byte, error) {
	if v == nil {
		return []byte("{}"), nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return []byte("{}"), nil
	}
	return b, nil
}
