package services

import (
	"context"
	"sync"
	"time"
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
}

type ScanProgressTracker struct {
	mu       sync.RWMutex
	progress map[string]*ScanProgress
}

func NewScanProgressTracker() *ScanProgressTracker {
	return &ScanProgressTracker{
		progress: make(map[string]*ScanProgress),
	}
}

func (t *ScanProgressTracker) StartScan(libraryID, libraryName string, totalItems int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.progress[libraryID] = &ScanProgress{
		LibraryID:   libraryID,
		LibraryName: libraryName,
		Status:      "scanning",
		TotalItems:  totalItems,
		StartedAt:   time.Now().UnixMilli(),
	}
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
	if p, ok := t.progress[libraryID]; ok {
		p.Status = "completed"
		p.Percentage = 100
		p.ProcessedItems = p.TotalItems
		p.CurrentItem = nil
		now := time.Now().UnixMilli()
		p.CompletedAt = &now
	}
	t.mu.Unlock()

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
	if p, ok := t.progress[libraryID]; ok {
		p.Status = "failed"
		p.Error = &errMsg
		now := time.Now().UnixMilli()
		p.CompletedAt = &now
	}
	t.mu.Unlock()

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
