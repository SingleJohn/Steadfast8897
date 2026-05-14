package services

import (
	"context"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// autoProbeHook 在扫描结束时,如果 probe_on_ingest 开关打开且有未探测的
// media_version,自动触发一次 ProbeTask。复用单例 ProbeTask,避免多次扫描
// 并发起多个探测;ProbeTask 内部 running 时 Start 直接返回 nil 不冲突。
type autoProbeHook struct {
	mu   sync.Mutex
	task *ProbeTask
}

var autoProbe = &autoProbeHook{}

// RegisterAutoProbeTask 由 main.go 在初始化 AppState 时调用,把全局
// ProbeTask 注册进 hook。未注册时 MaybeTriggerAutoProbe 直接 no-op。
func RegisterAutoProbeTask(t *ProbeTask) {
	autoProbe.mu.Lock()
	defer autoProbe.mu.Unlock()
	autoProbe.task = t
}

// MaybeTriggerAutoProbe 检查 probe_on_ingest 配置和 missing 计数,
// 满足条件时调 ProbeTask.Start。失败 / 已在跑都安静跳过(只记 Debug)。
// 设计为可在 goroutine 内调用,不返回错误。
func MaybeTriggerAutoProbe(ctx context.Context, pool *pgxpool.Pool) {
	autoProbe.mu.Lock()
	t := autoProbe.task
	autoProbe.mu.Unlock()
	if t == nil {
		return
	}

	var enabled string
	_ = pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'probe_on_ingest'").Scan(&enabled)
	if enabled != "true" {
		return
	}

	cnt, err := GetMissingMediainfoCount(ctx, pool)
	if err != nil || cnt == 0 {
		return
	}

	if err := t.Start(pool, 0); err != nil {
		slog.Debug("[Scan] Auto-probe skipped", "reason", err)
		return
	}
	slog.Info("[Scan] Auto-probe triggered", "missing", cnt)
}
