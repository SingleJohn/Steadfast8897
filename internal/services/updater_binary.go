package services

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// startApplyBinary 裸机(Linux/macOS)二进制自更新:
// 下载 → sha256 校验 → 解压 → 备份旧二进制 → 替换 → syscall.Exec 自替换进程。
func (u *Updater) startApplyBinary(ctx context.Context) (UpdateStatus, error) {
	u.mu.Lock()
	u.reloadStateLocked()
	if isUpdateTaskActive(u.status.Status) {
		st := cloneUpdateStatus(u.status)
		u.mu.Unlock()
		return st, fmt.Errorf("update task already running")
	}
	u.mu.Unlock()

	target := BuildTargetName()
	if target == "" {
		return u.GetStatus(ctx), fmt.Errorf("unsupported platform: %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	checked, err := u.Check(ctx)
	if err != nil {
		return checked, err
	}
	if !checked.HasUpdate {
		return checked, fmt.Errorf("no update available")
	}

	// Check 只填了 UpdateStatus,需要重新 resolve 拿 assets。
	channel := normalizeUpdateChannel(checked.Channel)
	if channel == "" {
		channel = defaultUpdateChannel
	}
	release, err := u.resolveLatestRelease(ctx, channel)
	if err != nil {
		return checked, fmt.Errorf("resolve release: %w", err)
	}
	if len(release.Assets) == 0 {
		return checked, fmt.Errorf("github release has no assets,cannot auto-update")
	}

	ext := BuildArchiveExt()
	assetName := fmt.Sprintf("fyms_%s_%s.%s", release.Version, target, ext)
	var archiveAsset, checksumAsset *gitHubAsset
	for i := range release.Assets {
		a := &release.Assets[i]
		if a.Name == assetName {
			archiveAsset = a
		}
		if a.Name == "checksums.txt" {
			checksumAsset = a
		}
	}
	if archiveAsset == nil {
		return checked, fmt.Errorf("no release asset for target %s (%s)", target, assetName)
	}
	if checksumAsset == nil {
		return checked, fmt.Errorf("checksums.txt missing in release")
	}

	// 标记 pulling
	u.mu.Lock()
	now := time.Now().UTC().Format(time.RFC3339)
	u.status.Status = "pulling"
	u.status.Message = "正在下载新版本"
	u.status.StartedAt = &now
	u.status.CompletedAt = nil
	u.status.Error = nil
	u.status.TargetVersion = release.Version
	u.appendLogLocked(fmt.Sprintf("开始下载 %s", archiveAsset.Name))
	u.persistStateLocked()
	u.mu.Unlock()

	updateDir := filepath.Join(u.cfg.DataDir, "update")
	downloadDir := filepath.Join(updateDir, "download")
	stagingDir := filepath.Join(updateDir, "staging")
	backupDir := filepath.Join(updateDir, "backup")
	_ = os.MkdirAll(backupDir, 0755)
	_ = os.RemoveAll(stagingDir)
	_ = os.MkdirAll(stagingDir, 0755)

	archivePath := filepath.Join(downloadDir, archiveAsset.Name)
	checksumPath := filepath.Join(downloadDir, "checksums.txt")

	client := u.httpClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}

	if err := downloadFile(ctx, client, archiveAsset.BrowserDownloadURL, archivePath); err != nil {
		return u.markBinaryFailure(ctx, fmt.Errorf("download archive: %w", err))
	}
	if err := downloadFile(ctx, client, checksumAsset.BrowserDownloadURL, checksumPath); err != nil {
		return u.markBinaryFailure(ctx, fmt.Errorf("download checksums: %w", err))
	}
	sums, err := parseChecksumsFile(checksumPath)
	if err != nil {
		return u.markBinaryFailure(ctx, fmt.Errorf("parse checksums: %w", err))
	}
	wantSum, ok := sums[archiveAsset.Name]
	if !ok {
		return u.markBinaryFailure(ctx, fmt.Errorf("no checksum entry for %s", archiveAsset.Name))
	}
	gotSum, err := sha256File(archivePath)
	if err != nil {
		return u.markBinaryFailure(ctx, fmt.Errorf("compute sha256: %w", err))
	}
	if !strings.EqualFold(wantSum, gotSum) {
		return u.markBinaryFailure(ctx, fmt.Errorf("checksum mismatch: want %s got %s", wantSum, gotSum))
	}

	u.mu.Lock()
	u.status.Status = "recreating"
	u.status.Message = "正在解压并替换二进制"
	u.appendLogLocked("校验通过,开始解压")
	u.persistStateLocked()
	u.mu.Unlock()

	if ext == "zip" {
		if err := extractZip(archivePath, stagingDir); err != nil {
			return u.markBinaryFailure(ctx, fmt.Errorf("extract zip: %w", err))
		}
	} else {
		if err := extractTarGz(archivePath, stagingDir); err != nil {
			return u.markBinaryFailure(ctx, fmt.Errorf("extract tar.gz: %w", err))
		}
	}

	stagedBinary := filepath.Join(stagingDir, fmt.Sprintf("fyms_%s_%s", release.Version, target), BuildBinaryName())
	if _, err := os.Stat(stagedBinary); err != nil {
		return u.markBinaryFailure(ctx, fmt.Errorf("staged binary not found: %s", stagedBinary))
	}

	currentExe, err := os.Executable()
	if err != nil {
		return u.markBinaryFailure(ctx, fmt.Errorf("locate current executable: %w", err))
	}
	if resolved, rerr := filepath.EvalSymlinks(currentExe); rerr == nil {
		currentExe = resolved
	}

	// 备份当前版本(同版本覆盖,避免下载失败后重复占用磁盘)
	backupPath := filepath.Join(backupDir, fmt.Sprintf("fyms-%s.bak", u.cfg.Version))
	_ = os.Remove(backupPath)
	if err := copyFile(currentExe, backupPath, 0755); err != nil {
		return u.markBinaryFailure(ctx, fmt.Errorf("backup current binary: %w", err))
	}
	pruneBackups(backupDir, 2)

	// 替换:Unix 上 rename 正在执行的文件 OK,inode 不变直到进程退出。
	// 用 rename(atomic)优先,失败再尝试 copyFile(跨设备场景)。
	if err := os.Rename(stagedBinary, currentExe); err != nil {
		if copyErr := copyFile(stagedBinary, currentExe, 0755); copyErr != nil {
			return u.markBinaryFailure(ctx, fmt.Errorf("install new binary: %w (rename error: %v)", copyErr, err))
		}
	}

	u.mu.Lock()
	nowR := time.Now().UTC().Format(time.RFC3339)
	u.status.Status = "restarting"
	u.status.Message = "二进制替换成功,即将重启"
	u.status.StartedAt = &nowR
	u.appendLogLocked("二进制已替换,准备 exec 自替换")
	u.persistStateLocked()
	u.mu.Unlock()

	// fire-and-forget:300ms 后 exec,HTTP 响应先返回。
	go func() {
		time.Sleep(300 * time.Millisecond)
		argv := append([]string(nil), os.Args...)
		if err := execSelf(argv, os.Environ()); err != nil {
			slog.Error("exec self failed", "error", err)
			u.MarkFailure(fmt.Errorf("exec self: %w", err))
		}
	}()

	return u.GetStatus(ctx), nil
}

func (u *Updater) markBinaryFailure(ctx context.Context, err error) (UpdateStatus, error) {
	u.MarkFailure(err)
	return u.GetStatus(ctx), err
}

// copyFile 复制文件到 dst,覆盖已存在。父目录不存在时自动创建。
// 用 copy 而非 rename 是为了处理源和目标跨设备挂载的情况(/tmp 和 /usr/local/bin)。
func copyFile(src, dst string, mode os.FileMode) error {
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	df, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(df, sf); err != nil {
		df.Close()
		return err
	}
	return df.Close()
}

// pruneBackups 保留最新的 keep 个 .bak 文件,其余删掉。
func pruneBackups(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type entryInfo struct {
		path    string
		modTime time.Time
	}
	var bakFiles []entryInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".bak") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		bakFiles = append(bakFiles, entryInfo{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	if len(bakFiles) <= keep {
		return
	}
	sort.Slice(bakFiles, func(i, j int) bool {
		return bakFiles[i].modTime.After(bakFiles[j].modTime)
	})
	for _, old := range bakFiles[keep:] {
		_ = os.Remove(old.path)
	}
}
