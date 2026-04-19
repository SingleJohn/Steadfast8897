package services

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/services/taskcenter"
)

type ScanProgress struct {
	LibraryID      string  `json:"LibraryId"`
	LibraryName    string  `json:"LibraryName"`
	Status         string  `json:"Status"`
	TotalItems     int64   `json:"TotalItems"`
	ProcessedItems int64   `json:"ProcessedItems"`
	CurrentItem    *string `json:"CurrentItem,omitempty"`
	StartedAt      int64   `json:"StartedAt"`
	CompletedAt    *int64  `json:"CompletedAt,omitempty"`
	Percentage     int     `json:"Percentage"`
	Error          *string `json:"Error,omitempty"`

	// RunID 是 task_runs 表里的这次扫描对应行。不暴露给 JSON，仅内部使用。
	RunID int64 `json:"-"`
}

// ScanProgressTracker 持有每个库的当前扫描进度。
//
// M6 起 tracker 也会向 task_runs 表写 Begin/End，让历史表能看到扫描记录。
// pool 为 nil 时降级为纯内存模式（测试或引导期）。
type ScanProgressTracker struct {
	mu       sync.RWMutex
	progress map[string]*ScanProgress
	pool     *pgxpool.Pool
}

func NewScanProgressTracker(pool *pgxpool.Pool) *ScanProgressTracker {
	return &ScanProgressTracker{
		progress: make(map[string]*ScanProgress),
		pool:     pool,
	}
}

func (t *ScanProgressTracker) StartScan(libraryID, libraryName string, totalItems int64) {
	t.mu.Lock()
	t.progress[libraryID] = &ScanProgress{
		LibraryID:   libraryID,
		LibraryName: libraryName,
		Status:      "scanning",
		TotalItems:  totalItems,
		StartedAt:   time.Now().UnixMilli(),
	}
	pool := t.pool
	t.mu.Unlock()

	if pool == nil {
		return
	}
	// Trigger 默认标记为 auto——file_watcher 和库编辑都属于自动触发。
	// 后续如果需要区分 manual，可以扩展签名传入 Trigger。
	runID, err := taskcenter.Begin(context.Background(), pool, taskcenter.BeginParams{
		Kind:    taskcenter.KindScan,
		Trigger: taskcenter.TriggerAuto,
		Total:   totalItems,
		Payload: map[string]any{
			"libraryId":   libraryID,
			"libraryName": libraryName,
		},
	})
	if err != nil {
		slog.Warn("scan: begin run failed", "libraryId", libraryID, "error", err)
		return
	}
	t.mu.Lock()
	if p, ok := t.progress[libraryID]; ok {
		p.RunID = runID
	}
	t.mu.Unlock()
}

func (t *ScanProgressTracker) UpdateTotal(libraryID string, total int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if p, ok := t.progress[libraryID]; ok {
		p.TotalItems = total
	}
}

func (t *ScanProgressTracker) UpdateScan(libraryID string, processedItems int64, currentItem *string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if p, ok := t.progress[libraryID]; ok {
		p.ProcessedItems = processedItems
		p.CurrentItem = currentItem
		if p.TotalItems > 0 {
			p.Percentage = int(float64(processedItems) / float64(p.TotalItems) * 100)
		}
	}
}

func (t *ScanProgressTracker) CompleteScan(libraryID string, cache *CacheService) {
	t.mu.Lock()
	var runID, finalTotal int64
	if p, ok := t.progress[libraryID]; ok {
		p.Status = "completed"
		p.Percentage = 100
		p.ProcessedItems = p.TotalItems
		p.CurrentItem = nil
		now := time.Now().UnixMilli()
		p.CompletedAt = &now
		runID = p.RunID
		finalTotal = p.TotalItems
	}
	pool := t.pool
	t.mu.Unlock()

	if pool != nil && runID > 0 {
		ctx := context.Background()
		_ = taskcenter.UpdateProgress(ctx, pool, runID, finalTotal, 0, 0, finalTotal, nil)
		_ = taskcenter.End(ctx, pool, runID, taskcenter.StatusSucceeded, "", "")
	}

	go func() {
		ctx := context.Background()
		cache.Del(ctx, "views:all")
		cache.DelPattern(ctx, "latest:"+libraryID+":*")
	}()

	go func() {
		time.Sleep(60 * time.Second)
		t.mu.Lock()
		defer t.mu.Unlock()
		if p, ok := t.progress[libraryID]; ok && p.Status == "completed" {
			delete(t.progress, libraryID)
		}
	}()
}

func (t *ScanProgressTracker) FailScan(libraryID, errMsg string) {
	t.mu.Lock()
	var runID, finalProcessed, finalTotal int64
	if p, ok := t.progress[libraryID]; ok {
		p.Status = "failed"
		p.Error = &errMsg
		now := time.Now().UnixMilli()
		p.CompletedAt = &now
		runID = p.RunID
		finalProcessed = p.ProcessedItems
		finalTotal = p.TotalItems
	}
	pool := t.pool
	t.mu.Unlock()

	if pool != nil && runID > 0 {
		ctx := context.Background()
		_ = taskcenter.UpdateProgress(ctx, pool, runID, finalProcessed, 0, 0, finalTotal, nil)
		_ = taskcenter.End(ctx, pool, runID, taskcenter.StatusFailed, "", errMsg)
	}

	go func() {
		time.Sleep(60 * time.Second)
		t.mu.Lock()
		defer t.mu.Unlock()
		if p, ok := t.progress[libraryID]; ok && p.Status == "failed" {
			delete(t.progress, libraryID)
		}
	}()
}

func (t *ScanProgressTracker) GetAll() []ScanProgress {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]ScanProgress, 0, len(t.progress))
	for _, p := range t.progress {
		result = append(result, *p)
	}
	return result
}

func (t *ScanProgressTracker) Get(libraryID string) *ScanProgress {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if p, ok := t.progress[libraryID]; ok {
		cp := *p
		return &cp
	}
	return nil
}

func (t *ScanProgressTracker) IsScanning(libraryID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	p, ok := t.progress[libraryID]
	return ok && p.Status == "scanning"
}

func (t *ScanProgressTracker) IsAnyScanning() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, p := range t.progress {
		if p.Status == "scanning" {
			return true
		}
	}
	return false
}
