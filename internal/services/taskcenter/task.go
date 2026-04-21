// Package taskcenter 统一抽象所有后台异步任务（scan/scrape/probe/backfill/update）。
//
// 设计要点：
//   - Task 接口：每种任务对外暴露相同的 Start / Stop / Snapshot 形状，供 Registry 聚合。
//   - Snapshot：只读内存态，字段覆盖现有 5 个任务的并集（total/processed/stage/counters 等）。
//     读写统一用 UnixMilli 时间戳，避免 *time.Time / 字符串混用。
//   - task_runs 表：只记录历史（启动/结束），不追每秒进度，由 runs.go 管理。
package taskcenter

import (
	"context"
)

// Kind 是任务种类枚举。字符串常量，用于路由和 DB 记录。
type Kind string

const (
	KindScan     Kind = "scan"
	KindScrape   Kind = "scrape"
	KindProbe    Kind = "probe"
	KindBackfill Kind = "backfill"
	KindUpdate   Kind = "update"
	KindCleanup  Kind = "cleanup" // 媒体库软删除后的后台清理
)

// Status 是统一的任务状态枚举。各适配器内部旧状态（如 "scanning" / "idle"）
// 由适配器层映射到这套标准状态。
type Status string

const (
	StatusIdle      Status = "idle"
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusStopping  Status = "stopping"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Trigger 记录任务因何启动，用于历史表排查。
type Trigger string

const (
	TriggerManual  Trigger = "manual"  // 用户在 UI 点触发
	TriggerAuto    Trigger = "auto"    // 扫描器/文件监听自动触发
	TriggerStartup Trigger = "startup" // 服务启动时的自动回填
	TriggerChain   Trigger = "chain"   // 任务链联动（M5）
)

// StartParams 是启动参数的自由容器。由各适配器内部解析需要的字段
// （如 probe 的 threads、backfill 的 stages、update 的 channel）。
type StartParams map[string]any

// Snapshot 是任务当前运行态的只读视图，SSE 推送和 /Tasks 接口都用它。
// 所有时间字段使用 UnixMilli（0 表示未设置）。
type Snapshot struct {
	Kind        Kind             `json:"kind"`
	RunID       int64            `json:"runId,omitempty"`   // 对应 task_runs.id；idle 时为 0
	Status      Status           `json:"status"`
	Stage       string           `json:"stage,omitempty"`   // Backfill: quality/name/image；其他为空
	Phase       string           `json:"phase,omitempty"`   // 自由文本阶段（如 scan 的 walking/matching）
	Total       int64            `json:"total"`
	Processed   int64            `json:"processed"`
	Success     int64            `json:"success,omitempty"`
	Failed      int64            `json:"failed,omitempty"`
	Percent     int              `json:"percent"`
	Current     string           `json:"current,omitempty"` // 当前处理项名称
	Counters    map[string]int64 `json:"counters,omitempty"`
	Message     string           `json:"message,omitempty"`
	Error       string           `json:"error,omitempty"`
	StartedAt   int64            `json:"startedAt,omitempty"`
	CompletedAt int64            `json:"completedAt,omitempty"`
	Cancellable bool             `json:"cancellable"`
	Children    []Snapshot       `json:"children,omitempty"` // scan 多库 / backfill 多 stage 聚合视图
}

// Task 是每个适配器必须实现的接口。适配器内部仍调用原服务对象，
// 本接口只负责统一对外形状。
type Task interface {
	Kind() Kind

	// Snapshot 返回当前内存态，必须无锁快照（或内部自行加锁），不阻塞。
	Snapshot() Snapshot

	// Start 异步启动任务。若已在运行，应返回当前 runID 而非错误（幂等）。
	// ctx 用于取消；params 由适配器解析。
	Start(ctx context.Context, params StartParams, trigger Trigger) (runID int64, err error)

	// Stop 请求停止。若任务不可取消或已结束，返回 nil 即可。
	Stop() error
}

// Running 是 Status 的辅助判断：是否处于活跃状态（会消耗资源）。
func (s Status) Running() bool {
	return s == StatusQueued || s == StatusRunning || s == StatusStopping
}

// Terminal 是否为终止状态。
func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusCancelled
}
