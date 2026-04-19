package services

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FileWatcher struct {
	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
}

type watchPath struct {
	path           string
	libraryID      string
	collectionType string
}

func NewFileWatcher() *FileWatcher {
	return &FileWatcher{}
}

func (fw *FileWatcher) Start(ctx context.Context, pool *pgxpool.Pool, cache *CacheService) {
	var enabled *string
	pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'file_watcher_enabled'").Scan(&enabled)
	if enabled != nil && *enabled == "false" {
		slog.Info("[FileWatcher] Disabled by config")
		return
	}

	rows, err := pool.Query(ctx, "SELECT id, name, collection_type, paths FROM libraries ORDER BY name")
	if err != nil {
		slog.Error("[FileWatcher] Failed to get libraries", "error", err)
		return
	}

	type libInfo struct {
		id             uuid.UUID
		name           string
		collectionType string
		paths          []string
	}
	var libs []libInfo
	for rows.Next() {
		var l libInfo
		if err := rows.Scan(&l.id, &l.name, &l.collectionType, &l.paths); err != nil {
			continue
		}
		libs = append(libs, l)
	}
	rows.Close()

	if len(libs) == 0 {
		slog.Info("[FileWatcher] No libraries to watch")
		return
	}

	var wps []watchPath
	for _, lib := range libs {
		for _, p := range lib.paths {
			if p == "" {
				continue
			}
			if _, err := os.Stat(p); err != nil {
				continue
			}
			if isRemoteMount(p) {
				slog.Info("[FileWatcher] Skipping remote mount", "path", p)
				continue
			}
			wps = append(wps, watchPath{p, lib.id.String(), lib.collectionType})
		}
	}

	if len(wps) == 0 {
		slog.Info("[FileWatcher] No local paths to watch")
		return
	}

	fw.mu.Lock()
	fw.stopCh = make(chan struct{})
	fw.running = true
	fw.mu.Unlock()

	stopCh := fw.stopCh

	go func() {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			slog.Error("[FileWatcher] Failed to create watcher", "error", err)
			return
		}
		defer watcher.Close()

		watched := 0
		for _, wp := range wps {
			if err := addRecursive(watcher, wp.path); err != nil {
				slog.Warn("[FileWatcher] Cannot watch", "path", wp.path, "error", err)
			} else {
				watched++
			}
		}
		slog.Info("[FileWatcher] Watching paths for changes", "count", watched)

		pending := make(map[string]time.Time)
		var pendingMu sync.Mutex
		debounceInterval := 500 * time.Millisecond

		findLibrary := func(filePath string) *watchPath {
			for i := range wps {
				if strings.HasPrefix(filePath, wps[i].path) {
					return &wps[i]
				}
			}
			return nil
		}

		go func() {
			ticker := time.NewTicker(debounceInterval)
			defer ticker.Stop()
			for {
				select {
				case <-stopCh:
					return
				case <-ticker.C:
					pendingMu.Lock()
					now := time.Now()
					var ready []string
					for p, t := range pending {
						if now.Sub(t) >= debounceInterval {
							ready = append(ready, p)
						}
					}
					for _, p := range ready {
						delete(pending, p)
					}
					pendingMu.Unlock()

					libsToScan := make(map[string]*watchPath)
					for _, p := range ready {
						wp := findLibrary(p)
						if wp == nil {
							continue
						}
						libsToScan[wp.libraryID] = wp
					}

					for _, wp := range libsToScan {
						slog.Info("[FileWatcher] Triggering scan for library", "library", wp.libraryID)
						ct := wp.collectionType
						lid := wp.libraryID
						go func(libraryID, colType string) {
							var name string
							var paths []string
							err := pool.QueryRow(context.Background(),
								"SELECT name, paths FROM libraries WHERE id = $1::uuid", libraryID).Scan(&name, &paths)
							if err != nil {
								slog.Warn("[FileWatcher] Cannot get library info", "id", libraryID, "error", err)
								return
							}
							tracker := NewScanProgressTracker(pool)
							ScanLibrary(context.Background(), pool, cache, tracker, libraryID, colType, paths, name)
						}(lid, ct)
					}
				}
			}
		}()

		for {
			select {
			case <-stopCh:
				slog.Info("[FileWatcher] Stop signal received")
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Has(fsnotify.Remove) {
					handleFileRemoved(ctx, pool, event.Name)
					cache.Del(ctx, "views:all")
					cache.DelPattern(ctx, "latest:*")
				}

				if event.Has(fsnotify.Create) {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						_ = addRecursive(watcher, event.Name)
					}
					pendingMu.Lock()
					pending[event.Name] = time.Now()
					pendingMu.Unlock()
				}

				if event.Has(fsnotify.Rename) {
					if _, err := os.Stat(event.Name); err == nil {
						pendingMu.Lock()
						pending[event.Name] = time.Now()
						pendingMu.Unlock()
					} else {
						handleFileRemoved(ctx, pool, event.Name)
					}
					cache.Del(ctx, "views:all")
					cache.DelPattern(ctx, "latest:*")
				}

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.Warn("[FileWatcher] Error", "error", err)
			}
		}
	}()
}

func addRecursive(watcher *fsnotify.Watcher, path string) error {
	return filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := filepath.Base(p)
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") {
				return filepath.SkipDir
			}
			return watcher.Add(p)
		}
		return nil
	})
}

func handleFileRemoved(ctx context.Context, pool *pgxpool.Pool, filePath string) {
	var id uuid.UUID
	var itemType string
	err := pool.QueryRow(ctx,
		"SELECT id, type FROM items WHERE file_path = $1 LIMIT 1", filePath).Scan(&id, &itemType)
	if err != nil {
		return
	}

	pool.Exec(ctx, "DELETE FROM items WHERE id = $1", id)
	slog.Info("[FileWatcher] Removed from DB", "type", itemType, "path", filePath)

	if itemType == "Episode" {
		pool.Exec(ctx,
			"DELETE FROM items WHERE type = 'Season' AND NOT EXISTS (SELECT 1 FROM items e WHERE e.parent_id = items.id)")
		pool.Exec(ctx,
			"DELETE FROM items WHERE type = 'Series' AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = items.id)")
	}
}

func (fw *FileWatcher) Stop() {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if fw.running && fw.stopCh != nil {
		close(fw.stopCh)
		fw.running = false
		slog.Info("[FileWatcher] Stopped")
	}
}

func (fw *FileWatcher) Restart(ctx context.Context, pool *pgxpool.Pool, cache *CacheService) {
	fw.Stop()
	fw.Start(ctx, pool, cache)
}

func isRemoteMount(dirPath string) bool {
	cmd := exec.Command("df", "-T", dirPath)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return false
	}
	parts := strings.Fields(lines[1])
	if len(parts) < 2 {
		return false
	}
	fsType := strings.ToLower(parts[1])
	remoteTypes := []string{"fuse", "nfs", "nfs4", "cifs", "smb", "smbfs", "9p", "sshfs"}
	for _, rt := range remoteTypes {
		if strings.HasPrefix(fsType, rt) || fsType == rt {
			return true
		}
	}
	return false
}
