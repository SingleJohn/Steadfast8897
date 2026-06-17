package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
)

type ProbeProgress struct {
	Status         string  `json:"status"`
	TotalItems     int64   `json:"totalItems"`
	ProcessedItems int64   `json:"processedItems"`
	SuccessItems   int64   `json:"successItems"`
	FailedItems    int64   `json:"failedItems"`
	CurrentItem    *string `json:"currentItem,omitempty"`
	Percentage     int     `json:"percentage"`
	Threads        int     `json:"threads"`
	MissingCount   int64   `json:"missingCount"`
	VersionsTotal  int64   `json:"versionsTotal"`
	Error          *string `json:"error,omitempty"`
}

type ProbeTask struct {
	mu       sync.Mutex
	progress ProbeProgress
	stopFlag atomic.Bool
}

func NewProbeTask() *ProbeTask {
	return &ProbeTask{
		progress: ProbeProgress{Status: "idle", Threads: 5},
	}
}

func (pt *ProbeTask) GetProgress() ProbeProgress {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	return pt.progress
}

func (pt *ProbeTask) Stop() {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if pt.progress.Status == "running" {
		pt.stopFlag.Store(true)
		pt.progress.Status = "stopping"
		slog.Info("[Probe] Stop requested")
	}
}

func (pt *ProbeTask) Start(pool *pgxpool.Pool, threads int) error {
	pt.mu.Lock()
	if pt.progress.Status == "running" || pt.progress.Status == "stopping" {
		pt.mu.Unlock()
		return fmt.Errorf("probe task is already running")
	}
	pt.mu.Unlock()

	if threads < 1 {
		threads = 1
	}
	if threads > 20 {
		threads = 20
	}
	pt.stopFlag.Store(false)

	ctx := context.Background()
	repo := repository.NewBackgroundTaskRepository(pool)
	items, err := repo.ListProbeTargets(ctx)
	if err != nil {
		return err
	}

	if len(items) == 0 {
		pt.mu.Lock()
		pt.progress = ProbeProgress{Status: "completed", Percentage: 100, Threads: int(threads)}
		pt.mu.Unlock()
		return nil
	}

	mappings := getProbePathMappings(ctx, pool)

	pt.mu.Lock()
	pt.progress = ProbeProgress{
		Status:     "running",
		TotalItems: int64(len(items)),
		Threads:    threads,
	}
	pt.mu.Unlock()

	slog.Info("[Probe] Starting", "items", len(items), "threads", threads)

	go func() {
		sem := make(chan struct{}, threads)
		var wg sync.WaitGroup
		var processed, success, failed atomic.Int64

		for _, item := range items {
			if pt.stopFlag.Load() {
				break
			}

			sem <- struct{}{}
			wg.Add(1)

			go func(pi repository.ProbeTarget) {
				defer func() { <-sem; wg.Done() }()
				if pt.stopFlag.Load() {
					return
				}

				err := probeOneItem(ctx, pool, pi.MediaVersionID.String(), pi.ItemID.String(), pi.FilePath, pi.Name, mappings)
				p := processed.Add(1)
				if err != nil {
					failed.Add(1)
				} else {
					success.Add(1)
				}

				pt.mu.Lock()
				pt.progress.ProcessedItems = p
				pt.progress.SuccessItems = success.Load()
				pt.progress.FailedItems = failed.Load()
				pt.progress.CurrentItem = &pi.Name
				if pt.progress.TotalItems > 0 {
					pt.progress.Percentage = int(float64(p) / float64(pt.progress.TotalItems) * 100)
				}
				pt.mu.Unlock()
			}(item)
		}

		wg.Wait()

		pt.mu.Lock()
		if pt.stopFlag.Load() {
			pt.progress.Status = "idle"
			slog.Info("[Probe] Stopped", "processed", pt.progress.ProcessedItems, "total", pt.progress.TotalItems)
		} else {
			pt.progress.Status = "completed"
			slog.Info("[Probe] Completed", "success", pt.progress.SuccessItems, "failed", pt.progress.FailedItems)
		}
		pt.mu.Unlock()
	}()

	return nil
}

func getProbePathMappings(ctx context.Context, pool *pgxpool.Pool) [][2]string {
	val, ok, err := repository.NewSystemConfigRepository(pool).GetString(ctx, "probe_path_mappings")
	if err != nil || !ok || val == "" {
		return nil
	}

	var arr []map[string]string
	if err := json.Unmarshal([]byte(val), &arr); err != nil {
		return nil
	}

	var mappings [][2]string
	for _, m := range arr {
		from, ok1 := m["from"]
		to, ok2 := m["to"]
		if ok1 && ok2 {
			mappings = append(mappings, [2]string{from, to})
		}
	}
	return mappings
}

func applyPathMappings(path string, mappings [][2]string) string {
	for _, m := range mappings {
		if len(path) >= len(m[0]) && path[:len(m[0])] == m[0] {
			return m[1] + path[len(m[0]):]
		}
	}
	return path
}

func probeOneItem(ctx context.Context, pool *pgxpool.Pool, mvID, itemID, filePath, name string, mappings [][2]string) error {
	realPath := filePath
	if filepath.Ext(filePath) == ".strm" {
		resolved := ResolveStrmPath(filePath)
		if resolved == nil {
			return fmt.Errorf("cannot resolve strm: %s", filePath)
		}
		realPath = *resolved
	}

	remote := strings.HasPrefix(realPath, "http://") || strings.HasPrefix(realPath, "https://")

	// 本地文件:应用路径映射并校验存在;远程直链:直接交给 ffprobe(支持 http)。
	var fileSize *int64
	if !remote {
		realPath = applyPathMappings(realPath, mappings)
		fi, err := os.Stat(realPath)
		if err != nil {
			return fmt.Errorf("file not found: %s", realPath)
		}
		s := fi.Size()
		fileSize = &s
	}

	doneCh := make(chan *ProbeResult, 1)
	errCh := make(chan error, 1)
	go func() {
		var (
			result *ProbeResult
			err    error
		)
		if remote {
			result, err = ProbeRemote(realPath)
		} else {
			result, err = ProbeFile(realPath)
		}
		if err != nil {
			errCh <- err
		} else {
			doneCh <- result
		}
	}()

	var result *ProbeResult
	select {
	case result = <-doneCh:
	case err := <-errCh:
		return err
	case <-time.After(40 * time.Second):
		return fmt.Errorf("probe timeout")
	}

	var streams []map[string]interface{}
	for _, s := range result.Streams {
		streams = append(streams, map[string]interface{}{
			"Codec": s.Codec, "Type": s.StreamType, "Index": s.Index,
			"IsDefault": s.IsDefault, "IsForced": s.IsForced,
			"Width": s.Width, "Height": s.Height, "BitRate": s.BitRate,
			"Channels": s.Channels, "SampleRate": s.SampleRate,
			"Language": s.Language, "Title": s.Title, "DisplayTitle": s.DisplayTitle,
		})
	}

	// 远程无法 os.Stat,size 留空;有大小且有时长才算码率。
	var bitrate *int64
	if fileSize != nil && result.DurationTicks > 0 {
		durSec := float64(result.DurationTicks) / 10_000_000.0
		b := int64(float64(*fileSize) * 8.0 / durSec)
		bitrate = &b
	}

	dbInfo := map[string]interface{}{
		"Name": name, "Size": fileSize, "RunTimeTicks": result.DurationTicks,
		"Bitrate": bitrate, "Container": result.Container, "MediaStreams": streams,
	}
	dbInfoJSON, _ := json.Marshal(dbInfo)

	chaptersSlice := result.Chapters
	if chaptersSlice == nil {
		chaptersSlice = []ProbeChapter{}
	}
	chaptersJSON, _ := json.Marshal(chaptersSlice)

	// 用 COALESCE 保护 size/bitrate:远程探测传 nil 时不覆盖已有列值。
	// chapters 每次探测都更新（空数组表示"探过但无章节"，区分 NULL "未探"）。
	repo := repository.NewBackgroundTaskRepository(pool)
	if err := repo.UpdateProbeMediaVersion(ctx, mvID, string(dbInfoJSON), result.DurationTicks, bitrate, fileSize, string(chaptersJSON)); err != nil {
		return err
	}

	return repo.FillItemRuntimeTicksIfEmpty(ctx, itemID, result.DurationTicks)
}

func GetMissingMediainfoCount(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	return repository.NewBackgroundTaskRepository(pool).GetMissingMediainfoCount(ctx)
}

func GetTotalMediaVersionsCount(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	return repository.NewBackgroundTaskRepository(pool).GetTotalMediaVersionsCount(ctx)
}
