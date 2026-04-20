package handlers

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"fyms/internal/config"
)

// ImageCache 管理 data/cache/sources/ 与 data/cache/resized/ 两个目录。
// sources/ 存放从远程挂载盘或 URL materialize 来的原图，
// resized/ 存放按尺寸/质量/格式生成的缩略图。
// 两个目录共享同一个 LRU 清理任务,按 mtime 升序删除到 MaxGB 以内。
type ImageCache struct {
	sourceDir  string
	resizedDir string
	maxBytes   int64
	httpClient *http.Client
	sf         singleflight.Group
	dataDir    string

	touchMu sync.Mutex
	touched map[string]time.Time
}

func NewImageCache(cfg *config.AppConfig, httpClient *http.Client) *ImageCache {
	src := cfg.ImageCacheSourceDir
	if src == "" {
		src = filepath.Join(cfg.CacheDir, "sources")
	}
	rsz := cfg.ImageCacheResizedDir
	if rsz == "" {
		rsz = filepath.Join(cfg.CacheDir, "resized")
	}
	os.MkdirAll(src, 0755)
	os.MkdirAll(rsz, 0755)

	max := cfg.ImageCacheMaxGB
	if max <= 0 {
		max = 5
	}

	return &ImageCache{
		sourceDir:  src,
		resizedDir: rsz,
		maxBytes:   int64(max) * 1024 * 1024 * 1024,
		httpClient: httpClient,
		dataDir:    absOrSelf(cfg.DataDir),
		touched:    make(map[string]time.Time),
	}
}

// Materialize 返回可被本地快速读取的路径 + 源指纹。
// 本地路径在 dataDir 下(例如 data/metadata TMDB 下载的图)直接返回,不做复制;
// 否则复制到 sources/ 缓存。URL 源统一下载到 sources/。
// 源指纹包含 mtime+size(本地)或 URL(远程),可嵌入 resize cache key,源变化后自动失效。
func (c *ImageCache) Materialize(source string, isURL bool) (localPath, srcHash string, err error) {
	if isURL {
		return c.materializeURL(source)
	}
	return c.materializeLocal(source)
}

func (c *ImageCache) materializeLocal(source string) (string, string, error) {
	st, err := os.Stat(source)
	if err != nil {
		return "", "", err
	}
	h := fingerprintLocal(source, st.Size(), st.ModTime())
	if c.isUnderDataDir(source) {
		return source, h, nil
	}
	ext := strings.ToLower(filepath.Ext(source))
	if ext == "" || len(ext) > 6 {
		ext = ".img"
	}
	dst := filepath.Join(c.sourceDir, h+ext)
	if dstSt, err := os.Stat(dst); err == nil && dstSt.Size() == st.Size() {
		c.touch(dst)
		return dst, h, nil
	}
	_, err, _ = c.sf.Do(dst, func() (any, error) {
		if dstSt, err := os.Stat(dst); err == nil && dstSt.Size() == st.Size() {
			return nil, nil
		}
		return nil, copyFileAtomic(source, dst)
	})
	if err != nil {
		return "", "", err
	}
	c.touch(dst)
	return dst, h, nil
}

func (c *ImageCache) materializeURL(source string) (string, string, error) {
	h := fingerprintURL(source)
	dst := filepath.Join(c.sourceDir, "url_"+h+".img")
	if st, err := os.Stat(dst); err == nil && st.Size() > 0 {
		c.touch(dst)
		return dst, h, nil
	}
	_, err, _ := c.sf.Do(dst, func() (any, error) {
		if st, err := os.Stat(dst); err == nil && st.Size() > 0 {
			return nil, nil
		}
		client := c.httpClient
		if client == nil {
			client = http.DefaultClient
		}
		resp, err := client.Get(source)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("download failed: %s", resp.Status)
		}
		tmp := dst + ".part"
		f, err := os.Create(tmp)
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(f, resp.Body); err != nil {
			f.Close()
			os.Remove(tmp)
			return nil, err
		}
		if err := f.Close(); err != nil {
			os.Remove(tmp)
			return nil, err
		}
		return nil, os.Rename(tmp, dst)
	})
	if err != nil {
		return "", "", err
	}
	c.touch(dst)
	return dst, h, nil
}

// ResizedPath 返回指定文件名在 resized/ 下的绝对路径。
func (c *ImageCache) ResizedPath(name string) string {
	return filepath.Join(c.resizedDir, name)
}

// Touch 刷新一个缓存文件的 access 时间,用于 LRU。
// 批量节流:同一路径 5 分钟内只真正 Chtimes 一次,减少网络盘/SSD 写入。
func (c *ImageCache) Touch(path string) { c.touch(path) }

func (c *ImageCache) touch(path string) {
	now := time.Now()
	c.touchMu.Lock()
	last, ok := c.touched[path]
	if ok && now.Sub(last) < 5*time.Minute {
		c.touchMu.Unlock()
		return
	}
	c.touched[path] = now
	c.touchMu.Unlock()
	_ = os.Chtimes(path, now, now)
}

func (c *ImageCache) isUnderDataDir(p string) bool {
	if c.dataDir == "" {
		return false
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(c.dataDir, abs)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

// StartJanitor 启动后台清理:立即跑一次,之后每小时按 mtime 升序删除到 maxBytes 以内。
func (c *ImageCache) StartJanitor(ctx context.Context) {
	go func() {
		c.sweep()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.sweep()
			}
		}
	}()
}

type cacheEntry struct {
	path string
	size int64
	mod  time.Time
}

func (c *ImageCache) sweep() {
	if c.maxBytes <= 0 {
		return
	}
	var entries []cacheEntry
	var total int64
	for _, dir := range []string{c.sourceDir, c.resizedDir} {
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, ".part") {
				return nil
			}
			entries = append(entries, cacheEntry{path, info.Size(), info.ModTime()})
			total += info.Size()
			return nil
		})
	}
	if total <= c.maxBytes {
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].mod.Before(entries[j].mod) })
	var removed int
	var freed int64
	for _, e := range entries {
		if total <= c.maxBytes {
			break
		}
		if err := os.Remove(e.path); err == nil {
			total -= e.size
			freed += e.size
			removed++
		}
	}
	if removed > 0 {
		slog.Info("[ImageCache] sweep",
			"removed", removed,
			"freed_mb", freed/1024/1024,
			"remaining_mb", total/1024/1024,
			"max_gb", c.maxBytes/1024/1024/1024)
	}
}

func absOrSelf(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func fingerprintLocal(path string, size int64, mod time.Time) string {
	h := sha1.New()
	io.WriteString(h, path)
	fmt.Fprintf(h, "|%d|%d", size, mod.UnixNano())
	return hex.EncodeToString(h.Sum(nil))[:20]
}

func fingerprintURL(url string) string {
	h := sha1.New()
	io.WriteString(h, url)
	return hex.EncodeToString(h.Sum(nil))[:20]
}

func copyFileAtomic(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
