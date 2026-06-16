package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// FileChangeEvent 是 Webhook 对外 API 的事件格式,保留不变以兼容外部推送方。
// 内部会翻译成统一的 IngestEvent 送入 IngestWorker。
type FileChangeEvent struct {
	Action          string  `json:"action"`
	IsDir           bool    `json:"is_dir"`
	SourceFile      string  `json:"source_file"`
	DestinationFile *string `json:"destination_file,omitempty"`
}

// HandleFileChangeEvents 是 Webhook 的入口:把 payload 里每条事件翻译成 IngestEvent
// 并 Submit 到 ingest 流。Phase 1 前的实现直接落库(与 scanner 重复),现在统一
// 走 worker,与 fsnotify / Phase 3 的全扫共享同一条消费路径。
func HandleFileChangeEvents(ctx context.Context, worker *IngestWorker, events []FileChangeEvent) {
	if worker == nil {
		return
	}
	mappings := getWebhookPathMappings(ctx, worker.pool)
	now := time.Now()

	for _, ev := range events {
		src := applyWebhookPathMappings(ev.SourceFile, mappings)
		var dst string
		if ev.DestinationFile != nil {
			dst = applyWebhookPathMappings(*ev.DestinationFile, mappings)
		}

		switch strings.ToLower(ev.Action) {
		case "create", "add":
			worker.Submit(IngestEvent{
				Kind: EventCreate, Path: src, IsDir: ev.IsDir,
				Source: "webhook", DetectedAt: now,
			})
		case "modify", "change":
			worker.Submit(IngestEvent{
				Kind: EventModify, Path: src, IsDir: ev.IsDir,
				Source: "webhook", DetectedAt: now,
			})
		case "delete", "remove":
			worker.Submit(IngestEvent{
				Kind: EventDelete, Path: src, IsDir: ev.IsDir,
				Source: "webhook", DetectedAt: now,
			})
		case "rename", "move":
			if dst != "" {
				worker.Submit(IngestEvent{
					Kind: EventRename, OldPath: src, Path: dst, IsDir: ev.IsDir,
					Source: "webhook", DetectedAt: now,
				})
			}
		}
	}
}

func getWebhookPathMappings(ctx context.Context, pool *pgxpool.Pool) [][2]string {
	var val *string
	pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'webhook_path_mappings'").Scan(&val)
	if val == nil {
		return nil
	}

	var arr []map[string]string
	if err := json.Unmarshal([]byte(*val), &arr); err != nil {
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

func applyWebhookPathMappings(path string, mappings [][2]string) string {
	for _, m := range mappings {
		if strings.HasPrefix(path, m[0]) {
			return m[1] + path[len(m[0]):]
		}
	}
	return path
}

// cleanupEmptyParents 在批量 Delete 后回收孤儿 Season/Series。
// 由 IngestWorker.processDelete 调用,放在这里是因为它是跨事件的"结构清理"
// 而非单事件处理。
func cleanupEmptyParents(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx,
		`DELETE FROM items WHERE type = 'Season' AND id NOT IN (
			SELECT DISTINCT parent_id FROM items WHERE parent_id IS NOT NULL AND type = 'Episode'
		) AND type = 'Season'`)
	if err != nil {
		return fmt.Errorf("cleanup seasons: %w", err)
	}

	_, err = pool.Exec(ctx,
		`DELETE FROM items WHERE type = 'Series' AND id NOT IN (
			SELECT DISTINCT parent_id FROM items WHERE parent_id IS NOT NULL AND type = 'Season'
		) AND type = 'Series'`)
	if err != nil {
		return fmt.Errorf("cleanup series: %w", err)
	}
	return nil
}

func CleanupEmptyParents(ctx context.Context, pool *pgxpool.Pool) error {
	return cleanupEmptyParents(ctx, pool)
}
