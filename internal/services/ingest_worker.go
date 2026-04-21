package services

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// 一个 IngestEvent 约 150 字节(两个路径字符串 + 几个枚举/时间戳),
	// 10 万条 ~15MB,对服务可忽略,但能把"大库首扫 + fsnotify 抖动"场景下的溢出压到 0。
	// buffer 起的是削峰作用 —— 真正瓶颈仍是 worker 吞吐,调大 worker 才是增加处理速度的办法。
	ingestChannelBuffer = 100000
	ingestWorkers       = 4

	// fsnotify 对下载中的视频文件会持续推 Write 事件,首次读到的 mediainfo/size 可能不完整;
	// processCreate 入口对视频文件做"写入稳定"判定 —— 两次 stat 间隔 fileStableInterval,
	// size+mtime 连续 fileStableChecks 次不变才算稳定;总 stat 次数上限 fileStableMaxAttempts,
	// 防下载中的文件把 worker 永久挂住。不稳定就丢弃事件,fsnotify 后续 Write(或下载完成的
	// close → IN_CLOSE_WRITE)会再次触发。
	fileStableInterval    = 2 * time.Second
	fileStableChecks      = 2
	fileStableMaxAttempts = 5
)

// IngestWorker 是扫描/监控/Webhook 统一的事件消费端。
// 3 个事件产源(FileWatcher/webhook/Phase 3 scan)都通过 Submit 入队,
// N 个 consume goroutine 并发处理,落库走现有的 scanOneMovie/scanOneShow
// 以保证与手动全扫行为完全一致。
type IngestWorker struct {
	ch       chan IngestEvent
	overflow atomic.Int64
	pool     *pgxpool.Pool
	cache    *CacheService
	libs     *libraryIndex
	renames  *renameBuffer

	// consumers:每个元素对应一个运行中的 consume goroutine 的 stop channel。
	// SetWorkerCount 用它实现动态增减(system_config.ingest_worker_count 可调,
	// Admin handler 改完立即生效;另有 60s 轮询兜底)。
	workerMu   sync.Mutex
	consumers  []chan struct{}
	workersCtx context.Context

	// inflight 记录 Tag 对应的在处理事件数,Barrier 轮询等待归零。
	// 只对设了 Tag 的 scan 事件计数;FileWatcher/Webhook 事件 Tag 为空,不进入计数。
	inflight sync.Map // key: Tag(string) → *atomic.Int64

	// stabilizing 正在做"文件写入稳定性检查"的路径集合。
	// fsnotify 对下载中视频文件高频推 Write,若每个事件都挂住一个 worker 做 2s 稳定性检查,
	// 4 个 worker 会被打满。这里做去重:同路径已在等就直接丢当前事件,首次等完后才释放。
	stabilizing sync.Map // key: path(string) → struct{}{}
}

func NewIngestWorker(pool *pgxpool.Pool, cache *CacheService) *IngestWorker {
	w := &IngestWorker{
		ch:    make(chan IngestEvent, ingestChannelBuffer),
		pool:  pool,
		cache: cache,
		libs:  newLibraryIndex(pool),
	}
	w.renames = newRenameBuffer(w.enqueue)
	return w
}

// Submit 事件入口。Delete 和 Create 会先走 renameBuffer 以尝试配对成 Rename。
// 非阻塞:channel 满则 overflow 计数 + warn 日志(每 100 条打一次,避免刷屏)。
func (w *IngestWorker) Submit(e IngestEvent) {
	switch e.Kind {
	case EventDelete:
		w.renames.OnDelete(e)
	case EventCreate:
		if !w.renames.OnCreate(e) {
			w.enqueue(e)
		}
	default:
		w.enqueue(e)
	}
}

func (w *IngestWorker) enqueue(e IngestEvent) {
	if e.Tag != "" {
		cnt, _ := w.inflight.LoadOrStore(e.Tag, new(atomic.Int64))
		cnt.(*atomic.Int64).Add(1)
	}
	select {
	case w.ch <- e:
	default:
		// channel 满,事件丢弃。若之前加了 inflight 计数要对冲,否则 Barrier 永不返回。
		if e.Tag != "" {
			if cnt, ok := w.inflight.Load(e.Tag); ok {
				cnt.(*atomic.Int64).Add(-1)
			}
		}
		prev := w.overflow.Add(1)
		if prev%100 == 1 {
			slog.Warn("[Ingest] channel overflow, event dropped",
				"total_dropped", prev, "kind", e.Kind.String(), "path", e.Path)
		}
	}
}

// OverflowCount 供观测/管理面板查询 channel 溢出总数。
func (w *IngestWorker) OverflowCount() int64 { return w.overflow.Load() }

// ChannelDepth 返回当前 channel 待处理事件数(供 metrics 打点)。
func (w *IngestWorker) ChannelDepth() int { return len(w.ch) }

// Run 启动 Worker:加载 library 映射 + 启动定时刷新 + 按 system_config 启动 N 个消费 goroutine。
// 传入的 ctx 结束后所有 goroutine 停止。
func (w *IngestWorker) Run(ctx context.Context) {
	if err := w.libs.Refresh(ctx); err != nil {
		slog.Error("[Ingest] Library index refresh failed", "error", err)
	}
	w.libs.StartAutoRefresh(ctx, 60*time.Second)

	w.workersCtx = ctx
	n := w.loadDesiredCount(ctx)
	w.SetWorkerCount(n)
	slog.Info("[Ingest] Worker started", "workers", n, "buffer", ingestChannelBuffer)

	go w.watchConfig(ctx, 60*time.Second)

	<-ctx.Done()
	slog.Info("[Ingest] Worker stopping")
	w.workerMu.Lock()
	for _, ch := range w.consumers {
		close(ch)
	}
	w.consumers = nil
	w.workerMu.Unlock()
}

// WorkerCount 返回当前运行中的 consume goroutine 数。
func (w *IngestWorker) WorkerCount() int {
	w.workerMu.Lock()
	defer w.workerMu.Unlock()
	return len(w.consumers)
}

// SetWorkerCount 动态调整 consume goroutine 数量。n<cur 时关闭多余的 stopCh;
// n>cur 时 spawn 新 goroutine。取值范围 [1, 64],超出自动 clamp。
// 幂等:n==cur 时直接返回。
func (w *IngestWorker) SetWorkerCount(n int) {
	if n < 1 {
		n = 1
	}
	if n > 64 {
		n = 64
	}
	w.workerMu.Lock()
	defer w.workerMu.Unlock()

	cur := len(w.consumers)
	if n == cur {
		return
	}
	if n > cur {
		for i := cur; i < n; i++ {
			stopCh := make(chan struct{})
			w.consumers = append(w.consumers, stopCh)
			go w.consume(w.workersCtx, i, stopCh)
		}
		slog.Info("[Ingest] Resized workers up", "from", cur, "to", n)
		return
	}
	for i := n; i < cur; i++ {
		close(w.consumers[i])
	}
	w.consumers = w.consumers[:n]
	slog.Info("[Ingest] Resized workers down", "from", cur, "to", n)
}

func (w *IngestWorker) consume(ctx context.Context, id int, stopCh chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		case e := <-w.ch:
			start := time.Now()
			if err := w.processEvent(ctx, e); err != nil {
				slog.Warn("[Ingest] processEvent failed",
					"worker", id, "kind", e.Kind.String(), "path", e.Path, "error", err)
			}
			w.cache.Del(ctx, "views:all")
			w.cache.DelPattern(ctx, "latest:*")
			if e.Tag != "" {
				if cnt, ok := w.inflight.Load(e.Tag); ok {
					cnt.(*atomic.Int64).Add(-1)
				}
			}
			if dur := time.Since(start); dur > 2*time.Second {
				slog.Info("[Ingest] Slow event",
					"worker", id, "kind", e.Kind.String(), "path", e.Path, "duration", dur)
			}
		}
	}
}

// loadDesiredCount 从 system_config.ingest_worker_count 读目标数量;默认 4。
func (w *IngestWorker) loadDesiredCount(ctx context.Context) int {
	raw := readSystemConfigValue(ctx, w.pool, "ingest_worker_count")
	if raw == "" {
		return ingestWorkers
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return ingestWorkers
	}
	return n
}

// watchConfig 周期性对齐 system_config.ingest_worker_count(兜底:Admin handler 也会直接调 SetWorkerCount)。
func (w *IngestWorker) watchConfig(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n := w.loadDesiredCount(ctx); n != w.WorkerCount() {
				w.SetWorkerCount(n)
			}
		}
	}
}

// Barrier 阻塞直到指定 Tag 的所有 inflight 事件都处理完成。
// 用于 scan 场景:遍历产完事件后等 worker drain,再做差集 Delete。
// 采用 100ms 轮询,简单可靠;Barrier 通常一次 scan 只调 1~2 次。
func (w *IngestWorker) Barrier(ctx context.Context, tag string) {
	if tag == "" {
		return
	}
	for {
		if w.InflightCount(tag) <= 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// InflightCount 返回指定 Tag 当前在处理的事件数,供 scan 进度轮询。
func (w *IngestWorker) InflightCount(tag string) int64 {
	cnt, ok := w.inflight.Load(tag)
	if !ok {
		return 0
	}
	return cnt.(*atomic.Int64).Load()
}

func (w *IngestWorker) processEvent(ctx context.Context, e IngestEvent) error {
	switch e.Kind {
	case EventCreate, EventModify:
		// Modify 事件只对视频文件有意义(mediainfo 可能变了)。
		// nfo/jpg/mediainfo.json 等 sidecar 的 modify 不改变 item 树,
		// 但会触发一次完整 scanOneShow(2~7s)+ autoScrapeNewItems,
		// 第三方刮削器反复改 season.nfo 时会把日志和 DB 打爆。
		if e.Kind == EventModify && !e.IsDir && !IsVideoExt(strings.ToLower(filepath.Ext(e.Path))) {
			return nil
		}
		return w.processCreate(ctx, e)
	case EventDelete:
		return w.processDelete(ctx, e)
	case EventRename:
		return w.processRename(ctx, e)
	}
	return nil
}

func (w *IngestWorker) processCreate(ctx context.Context, e IngestEvent) error {
	// 只对 fsnotify 来源的视频文件做稳定性检查:
	//   - scan 源产的事件文件已在磁盘静止
	//   - webhook 源通常是下载完成通知
	//   - 目录 Create 不涉及文件内容,无需等
	//   - 非视频文件(nfo/jpg/mediainfo.json)体积小,写入瞬时,无害
	if e.Source == "fsnotify" && !e.IsDir && IsVideoExt(strings.ToLower(filepath.Ext(e.Path))) {
		if _, loaded := w.stabilizing.LoadOrStore(e.Path, struct{}{}); loaded {
			slog.Debug("[Ingest] Skip event: path already stabilizing", "path", e.Path)
			return nil
		}
		defer w.stabilizing.Delete(e.Path)
		if !waitFileStable(ctx, e.Path) {
			slog.Debug("[Ingest] Skip event: file still being written", "path", e.Path)
			return nil
		}
	}

	libID, colType, ok := w.libs.Match(e.Path)
	if !ok {
		slog.Debug("[Ingest] Create skipped: path outside any library", "path", e.Path)
		return nil
	}
	switch colType {
	case "movies":
		return w.processMovieCreate(ctx, libID, e)
	case "tvshows":
		return w.processTvCreate(ctx, libID, e)
	}
	return nil
}

// waitFileStable 两次 stat 间隔 fileStableInterval,size+mtime 连续 fileStableChecks 次
// 不变才算稳定。总 stat 次数上限 fileStableMaxAttempts,防下载中的大文件永久挂住 worker。
// 文件不存在、期间 stat 失败、ctx 取消、超过尝试次数 → 返回 false(丢弃事件)。
func waitFileStable(ctx context.Context, path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	lastSize := info.Size()
	lastMtime := info.ModTime()

	stable := 0
	for attempts := 0; attempts < fileStableMaxAttempts; attempts++ {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(fileStableInterval):
		}
		info, err = os.Stat(path)
		if err != nil {
			return false
		}
		if info.Size() == lastSize && info.ModTime().Equal(lastMtime) {
			stable++
			if stable >= fileStableChecks {
				return true
			}
			continue
		}
		lastSize = info.Size()
		lastMtime = info.ModTime()
		stable = 0
	}
	return false
}

func (w *IngestWorker) processMovieCreate(ctx context.Context, libID string, e IngestEvent) error {
	path := e.Path
	isDir := e.IsDir
	// BDMV 布局:/<movie>/BDMV/STREAM/*.strm|m2ts 事件,改用电影根目录整目录入库,
	// 否则 name 会被解析成 "STREAM"/"00000" 之类的无意义名字。
	if !isDir {
		if root := findBdmvMovieRoot(path); root != "" {
			path = root
			isDir = true
		}
	}
	name := filepath.Base(path)
	existing := map[string]bool{}
	scanOneMovie(ctx, w.pool, libID, name, path, isDir, existing)
	go autoScrapeNewItems(ctx, w.pool, libID)
	return nil
}

func (w *IngestWorker) processTvCreate(ctx context.Context, libID string, e IngestEvent) error {
	showPath := w.findShowRoot(libID, e.Path)
	if showPath == "" {
		slog.Debug("[Ingest] Tv create skipped: no show dir found", "path", e.Path)
		return nil
	}
	scanOneShow(ctx, w.pool, libID, filepath.Base(showPath), showPath)
	go autoScrapeNewItems(ctx, w.pool, libID)
	return nil
}

// findShowRoot 从文件路径向上找第一个 isShowDir()==true 的目录;
// 若 filePath 本身就是目录(IsDir=true)且是 show 根,也返回它。
func (w *IngestWorker) findShowRoot(libID, filePath string) string {
	roots := w.libs.libraryRoots(libID)
	cleaned := filepath.Clean(filePath)

	for _, root := range roots {
		rel, err := filepath.Rel(root, cleaned)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		cur := cleaned
		for cur != root && cur != filepath.Dir(cur) {
			if isShowDir(cur) {
				return cur
			}
			cur = filepath.Dir(cur)
		}
	}
	return ""
}

// processDelete 统一删除逻辑。
//
// 两种语义分开处理,避免嵌套 item 互相误杀:
//
//  1. Source="scan"(pruneMissingPaths 产的差集 Delete):
//     是"item 层面"的删除 —— 某条 DB 记录不该存在了。只按 file_path 精确匹配删,
//     子项靠 items.parent_id ON DELETE CASCADE 级联。
//     反例:错误 Series Y(file_path=/show/Season 1) 与正确 Series Z(file_path=/show)
//     下的 Episode 物理路径相同(都指向同一文件),LIKE '/show/Season 1/%' 会把
//     Z 下的合法 Episode 一并删掉。
//
//  2. Source="fsnotify"/"webhook"(物理文件/目录消失):
//     是"路径层面"的删除 —— 这个路径及其下所有文件消失了。Season.file_path 通常为 NULL
//     不能靠 CASCADE 兜底,必须 LIKE '<path>/%' 匹配该目录下所有 Episode。
func (w *IngestWorker) processDelete(ctx context.Context, e IngestEvent) error {
	norm := filepath.Clean(e.Path)

	var (
		tag pgconn.CommandTag
		err error
	)
	if e.Source == "scan" {
		tag, err = w.pool.Exec(ctx,
			"DELETE FROM items WHERE file_path = $1", norm)
	} else {
		prefix := norm + string(filepath.Separator) + "%"
		tag, err = w.pool.Exec(ctx,
			"DELETE FROM items WHERE file_path = $1 OR file_path LIKE $2",
			norm, prefix)
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		slog.Info("[Ingest] Delete removed items",
			"count", tag.RowsAffected(), "path", norm, "source", e.Source)
		_ = cleanupEmptyParents(ctx, w.pool)
	}
	return nil
}

// processRename 保留 item 身份(播放进度/merge 关系/media_versions),只改 file_path。
// 若旧路径在库里找不到(ingest 漏了之前的 Create),fallback 按 Create 处理新路径。
func (w *IngestWorker) processRename(ctx context.Context, e IngestEvent) error {
	oldPath := filepath.Clean(e.OldPath)
	newPath := filepath.Clean(e.Path)

	if e.IsDir {
		rows, err := w.pool.Query(ctx,
			"SELECT id, file_path FROM items WHERE file_path = $1 OR file_path LIKE $2",
			oldPath, oldPath+string(filepath.Separator)+"%")
		if err != nil {
			return err
		}
		type upd struct {
			id uuid.UUID
			fp string
		}
		var updates []upd
		for rows.Next() {
			var u upd
			if rows.Scan(&u.id, &u.fp) == nil {
				updates = append(updates, u)
			}
		}
		rows.Close()

		for _, u := range updates {
			var nfp string
			if u.fp == oldPath {
				nfp = newPath
			} else {
				nfp = newPath + u.fp[len(oldPath):]
			}
			w.pool.Exec(ctx, "UPDATE items SET file_path = $1, updated_at = NOW() WHERE id = $2", nfp, u.id)
			w.pool.Exec(ctx, "UPDATE media_versions SET file_path = $1 WHERE file_path = $2", nfp, u.fp)
		}
		if len(updates) > 0 {
			slog.Info("[Ingest] Rename updated items", "count", len(updates), "from", oldPath, "to", newPath)
		}
		return nil
	}

	tag, err := w.pool.Exec(ctx,
		"UPDATE items SET file_path = $1, updated_at = NOW() WHERE file_path = $2",
		newPath, oldPath)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		w.pool.Exec(ctx, "UPDATE media_versions SET file_path = $1 WHERE file_path = $2", newPath, oldPath)
		slog.Info("[Ingest] Rename", "from", oldPath, "to", newPath)
		return nil
	}
	// 老路径不在库里:当作新建处理
	return w.processCreate(ctx, IngestEvent{
		Kind:       EventCreate,
		Path:       newPath,
		IsDir:      false,
		Source:     e.Source,
		DetectedAt: time.Now(),
	})
}
