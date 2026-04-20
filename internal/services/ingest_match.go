package services

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type libraryMatch struct {
	libID          string
	collectionType string
	path           string // filepath.Clean 之后的绝对路径
}

// libraryIndex 把 libraries 表里的 paths 映射缓存到内存,提供 path→library 归属判断。
// 修复 file_watcher.go:122 的 bug:原代码用 strings.HasPrefix 会把 /media/a 错匹配
// 到 /media/abc/file;新实现用 filepath.Rel,避免边界越界。
type libraryIndex struct {
	mu      sync.RWMutex
	entries []libraryMatch
	pool    *pgxpool.Pool
	loaded  atomic.Bool
}

func newLibraryIndex(pool *pgxpool.Pool) *libraryIndex {
	return &libraryIndex{pool: pool}
}

// Refresh 从 DB 重新加载 libraries → paths 映射。
func (li *libraryIndex) Refresh(ctx context.Context) error {
	rows, err := li.pool.Query(ctx, "SELECT id, collection_type, paths FROM libraries")
	if err != nil {
		return err
	}
	defer rows.Close()

	var entries []libraryMatch
	for rows.Next() {
		var id uuid.UUID
		var ct string
		var paths []string
		if err := rows.Scan(&id, &ct, &paths); err != nil {
			continue
		}
		for _, p := range paths {
			if p == "" {
				continue
			}
			entries = append(entries, libraryMatch{
				libID:          id.String(),
				collectionType: ct,
				path:           filepath.Clean(p),
			})
		}
	}

	li.mu.Lock()
	li.entries = entries
	li.mu.Unlock()
	li.loaded.Store(true)
	return nil
}

// Match 判断 filePath 归属哪个 library,选最长路径匹配(防嵌套 library 取错父级)。
// 用 filepath.Rel 而非 strings.HasPrefix:
//   - rel == "." 说明 filePath 就是 library 根
//   - rel 不以 ".." 开头说明 filePath 在 library 下
//   - 跨卷/跨 drive 时 filepath.Rel 直接返回错误,自动排除
func (li *libraryIndex) Match(filePath string) (libID, collectionType string, ok bool) {
	cleaned := filepath.Clean(filePath)
	li.mu.RLock()
	defer li.mu.RUnlock()

	var best libraryMatch
	bestLen := -1
	for _, e := range li.entries {
		rel, err := filepath.Rel(e.path, cleaned)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if len(e.path) > bestLen {
			best = e
			bestLen = len(e.path)
		}
	}
	if bestLen < 0 {
		return "", "", false
	}
	return best.libID, best.collectionType, true
}

// libraryRoots 返回指定 library 的所有根路径(clean 后)。
func (li *libraryIndex) libraryRoots(libID string) []string {
	li.mu.RLock()
	defer li.mu.RUnlock()
	var roots []string
	for _, e := range li.entries {
		if e.libID == libID {
			roots = append(roots, e.path)
		}
	}
	return roots
}

// StartAutoRefresh 后台周期性刷新,避免 Admin 改 library 路径后 ingest 还用老映射。
func (li *libraryIndex) StartAutoRefresh(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = li.Refresh(ctx)
			}
		}
	}()
}
