// Package adapters 把现有任务对象（services.ScrapeTask / ProbeTask / ...）
// 包装成 taskcenter.Task 接口。本包只做字段映射 + 方法转发，不引入新业务。
//
// M1 阶段：Snapshot 只读映射 + Start/Stop 转发；不写 task_runs 表。
// M2 阶段再在 Start/Stop 里嵌入 runs.Begin / runs.End。
package adapters

import (
	"maps"

	"fyms/internal/services/taskcenter"
)

// mapLegacyStatus 把各任务的旧状态字符串映射为统一的 taskcenter.Status。
// 未识别的字符串回退到 idle，避免前端拿到乱码。
func mapLegacyStatus(s string) taskcenter.Status {
	switch s {
	case "idle", "":
		return taskcenter.StatusIdle
	case "queued":
		return taskcenter.StatusQueued
	case "running", "scanning", "checking", "updating":
		return taskcenter.StatusRunning
	case "stopping":
		return taskcenter.StatusStopping
	case "completed", "succeeded", "available":
		return taskcenter.StatusSucceeded
	case "failed", "error":
		return taskcenter.StatusFailed
	case "stopped", "cancelled":
		return taskcenter.StatusCancelled
	default:
		return taskcenter.StatusIdle
	}
}

// pctFromCount 保护性计算百分比，避免 total=0 除零。
func pctFromCount(processed, total int64) int {
	if total <= 0 {
		return 0
	}
	p := int(processed * 100 / total)
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

// deref 安全解引用 *string，nil 返回空串。
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// copyCounters 深拷 map，避免 Snapshot 接收者修改影响适配器内部状态。
func copyCounters(src map[string]int64) map[string]int64 {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]int64, len(src))
	maps.Copy(out, src)
	return out
}
