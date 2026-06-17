package services

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
)

// FileWatcher 是 ingest 事件流的 fsnotify producer。
// Phase 1 之前它自己 debounce 后调 ScanLibrary 触发全库扫,大库一次改名要几分钟;
// 现在它只把 fsnotify 原始事件翻译成 IngestEvent 丢给 IngestWorker,
// 500ms 窗口的 Rename 合并、目录级 Delete、路径归属判断都在 worker 侧做。
type FileWatcher struct {
	mu        sync.Mutex
	running   bool
	stopCh    chan struct{}
	ingest    *IngestWorker
	refreshes *RefreshScheduler
}

func NewFileWatcher(ingest *IngestWorker, refreshes *RefreshScheduler) *FileWatcher {
	return &FileWatcher{ingest: ingest, refreshes: refreshes}
}

func (fw *FileWatcher) Start(ctx context.Context, pool *pgxpool.Pool, cache *CacheService) {
	enabled, ok, _ := repository.NewSystemConfigRepository(pool).GetString(ctx, "file_watcher_enabled")
	if ok && enabled == "false" {
		slog.Info("[FileWatcher] Disabled by config")
		return
	}

	libs, err := repository.NewLibraryRepository(pool).ListLibrariesForWatcher(ctx)
	if err != nil {
		slog.Error("[FileWatcher] Failed to get libraries", "error", err)
		return
	}

	if len(libs) == 0 {
		slog.Info("[FileWatcher] No libraries to watch")
		return
	}

	var watchRoots []string
	for _, lib := range libs {
		for _, p := range lib.Paths {
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
			watchRoots = append(watchRoots, p)
		}
	}

	if len(watchRoots) == 0 {
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
		for _, p := range watchRoots {
			if err := addRecursive(watcher, p); err != nil {
				slog.Warn("[FileWatcher] Cannot watch", "path", p, "error", err)
			} else {
				watched++
			}
		}
		slog.Info("[FileWatcher] Watching paths for changes", "count", watched)

		for {
			select {
			case <-stopCh:
				slog.Info("[FileWatcher] Stop signal received")
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				fw.handle(watcher, event)
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.Warn("[FileWatcher] Error", "error", err)
			}
		}
	}()
}

// handle 把一次 fsnotify 原始事件翻译成 1~2 条 IngestEvent。
// fsnotify 的 Op 是位掩码(Create|Write|Remove|Rename|Chmod),一次 Event 可能触发多种
// 语义,分开 Submit。
func (fw *FileWatcher) handle(watcher *fsnotify.Watcher, event fsnotify.Event) {
	if fw.ingest == nil && fw.refreshes == nil {
		return
	}
	path := event.Name
	now := time.Now()

	isDir := false
	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) || event.Has(fsnotify.Rename) {
		if info, err := os.Stat(path); err == nil {
			isDir = info.IsDir()
		}
	}

	if event.Has(fsnotify.Create) {
		if isDir {
			_ = addRecursive(watcher, path)
		}
		if !isDir && fw.refreshes != nil && classifySidecarPath(path) != "" {
			fw.refreshes.OnSidecarChange(path)
			return
		}
		fw.ingest.Submit(IngestEvent{
			Kind: EventCreate, Path: path, IsDir: isDir,
			Source: "fsnotify", DetectedAt: now,
		})
	}
	if event.Has(fsnotify.Write) {
		if !isDir && fw.refreshes != nil && classifySidecarPath(path) != "" {
			fw.refreshes.OnSidecarChange(path)
			return
		}
		fw.ingest.Submit(IngestEvent{
			Kind: EventModify, Path: path, IsDir: isDir,
			Source: "fsnotify", DetectedAt: now,
		})
	}
	if event.Has(fsnotify.Remove) {
		if fw.refreshes != nil && classifySidecarPath(path) != "" {
			fw.refreshes.OnSidecarChange(path)
			return
		}
		// 删除时文件已不存在,无法确定 isDir;processDelete 用 `= $1 OR LIKE $2/%`
		// 兜底目录级删除,与 isDir 无关。
		fw.ingest.Submit(IngestEvent{
			Kind: EventDelete, Path: path, IsDir: false,
			Source: "fsnotify", DetectedAt: now,
		})
	}
	if event.Has(fsnotify.Rename) {
		if fw.refreshes != nil && classifySidecarPath(path) != "" {
			fw.refreshes.OnSidecarChange(path)
			return
		}
		// fsnotify 的 Rename 语义:原路径被改名。新路径通常以 Create 紧随其后。
		// 跨目录 mv → 先 Rename(旧) 后 Create(新),renameBuffer 在 500ms 内合并成 Rename。
		// 若 stat 原路径还在(Windows 某些场景),按 Create 处理。
		if _, err := os.Stat(path); err == nil {
			fw.ingest.Submit(IngestEvent{
				Kind: EventCreate, Path: path, IsDir: isDir,
				Source: "fsnotify", DetectedAt: now,
			})
		} else {
			fw.ingest.Submit(IngestEvent{
				Kind: EventDelete, Path: path, IsDir: false,
				Source: "fsnotify", DetectedAt: now,
			})
		}
	}
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

// isRemoteMount 检测路径是否落在远程挂载上,避免 fsnotify 去 watch 网盘(不支持且慢)。
// Unix: 调 df -T 解析 fstype;Windows: 检查 UNC 路径 + 通过 'net use' 判断映射盘。
func isRemoteMount(dirPath string) bool {
	if runtime.GOOS == "windows" {
		return isRemoteMountWindows(dirPath)
	}
	return isRemoteMountUnix(dirPath)
}

func isRemoteMountUnix(dirPath string) bool {
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

// isRemoteMountWindows 判定规则:
//  1. UNC 路径(\\server\share\... 或 //server/share/...)视为远程
//  2. 盘符形如 "Z:\..." → 用 'net use Z:' 判断是否映射网络驱动器
func isRemoteMountWindows(dirPath string) bool {
	if strings.HasPrefix(dirPath, `\\`) || strings.HasPrefix(dirPath, `//`) {
		return true
	}
	if len(dirPath) >= 2 && dirPath[1] == ':' {
		drive := strings.ToUpper(dirPath[:2])
		cmd := exec.Command("net", "use", drive)
		if err := cmd.Run(); err == nil {
			return true
		}
	}
	return false
}
