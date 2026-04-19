package adapters

import (
	"context"
	"errors"
	"fmt"

	"fyms/internal/services"
	"fyms/internal/services/taskcenter"
)

// ScanAdapter 聚合 ScanProgressTracker（多库并行 map）为单个 taskcenter.Task。
//
// 聚合规则：
//   - 顶层 Snapshot.Status：任一库 scanning → Running；全部 completed/failed → Succeeded/Failed；空 → Idle。
//   - Total / Processed：所有库求和。
//   - Children：每个库一个子 Snapshot，便于前端展开看单库进度。
//
// Start/Stop：Scanner 由文件监听和库编辑操作触发，没有一个"全局扫描"的入口。
// 本适配器暂不暴露 Start/Stop（返回 ErrNotSupported），M5 任务链时再考虑是否需要
// 暴露"扫描所有库"的快捷入口。
type ScanAdapter struct {
	Tracker *services.ScanProgressTracker
}

var ErrScanStartNotSupported = errors.New("scan task must be triggered via library scan; task center start not supported")

func NewScanAdapter(t *services.ScanProgressTracker) *ScanAdapter {
	return &ScanAdapter{Tracker: t}
}

func (a *ScanAdapter) Kind() taskcenter.Kind { return taskcenter.KindScan }

func (a *ScanAdapter) Snapshot() taskcenter.Snapshot {
	all := a.Tracker.GetAll()

	agg := taskcenter.Snapshot{
		Kind:        taskcenter.KindScan,
		Status:      taskcenter.StatusIdle,
		Cancellable: false,
	}
	if len(all) == 0 {
		return agg
	}

	var running, failed, completed int
	var earliestStart, latestComplete int64
	children := make([]taskcenter.Snapshot, 0, len(all))

	for _, p := range all {
		child := taskcenter.Snapshot{
			Kind:      taskcenter.KindScan,
			Status:    mapLegacyStatus(p.Status),
			Phase:     p.LibraryName,
			Message:   fmt.Sprintf("library=%s", p.LibraryID),
			Total:     p.TotalItems,
			Processed: p.ProcessedItems,
			Percent:   p.Percentage,
			Current:   deref(p.CurrentItem),
			Error:     deref(p.Error),
			StartedAt: p.StartedAt,
		}
		if p.CompletedAt != nil {
			child.CompletedAt = *p.CompletedAt
			if *p.CompletedAt > latestComplete {
				latestComplete = *p.CompletedAt
			}
		}
		if earliestStart == 0 || p.StartedAt < earliestStart {
			earliestStart = p.StartedAt
		}

		agg.Total += child.Total
		agg.Processed += child.Processed

		switch child.Status {
		case taskcenter.StatusRunning:
			running++
		case taskcenter.StatusFailed:
			failed++
		case taskcenter.StatusSucceeded:
			completed++
		}
		children = append(children, child)
	}

	agg.Children = children
	agg.StartedAt = earliestStart
	agg.Percent = pctFromCount(agg.Processed, agg.Total)
	agg.Message = fmt.Sprintf("%d running / %d completed / %d failed", running, completed, failed)

	switch {
	case running > 0:
		agg.Status = taskcenter.StatusRunning
	case failed > 0 && completed == 0:
		agg.Status = taskcenter.StatusFailed
		agg.CompletedAt = latestComplete
	case completed > 0:
		agg.Status = taskcenter.StatusSucceeded
		agg.CompletedAt = latestComplete
	}
	return agg
}

func (a *ScanAdapter) Start(_ context.Context, _ taskcenter.StartParams, _ taskcenter.Trigger) (int64, error) {
	return 0, ErrScanStartNotSupported
}

func (a *ScanAdapter) Stop() error {
	// Scanner 目前不支持中途取消，保持 no-op。
	return nil
}
