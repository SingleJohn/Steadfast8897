package services

import (
	"path/filepath"
	"sync"
	"time"
)

const renamePairWindow = 500 * time.Millisecond

type pendingDelete struct {
	path     string
	isDir    bool
	source   string
	detected time.Time
	timer    *time.Timer
}

// renameBuffer 在 500ms 窗口内把成对的 Delete+Create 合并为 Rename 事件,
// 主要应对跨目录 mv 导致 fsnotify 产生 Remove+Create 两条事件的场景(同目录 mv
// fsnotify 本身就产 Rename 事件,无需再配对)。
//
// 匹配策略:basename + isDir 相同。Windows 下 fsnotify 不提供 inode,
// 只能用 basename 近似;跨目录同名的 mv 是常见真实场景,误配率可接受。
type renameBuffer struct {
	mu      sync.Mutex
	pending map[string]*pendingDelete // key: basename
	emit    func(IngestEvent)
}

func newRenameBuffer(emit func(IngestEvent)) *renameBuffer {
	return &renameBuffer{
		pending: make(map[string]*pendingDelete),
		emit:    emit,
	}
}

// OnDelete 先缓冲 500ms;超时未匹配就 flush 为普通 Delete 事件。
func (rb *renameBuffer) OnDelete(e IngestEvent) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	key := filepath.Base(e.Path)
	if existing, ok := rb.pending[key]; ok {
		// 同 basename 已有 pending(罕见):先让旧的失效,保留新的
		existing.timer.Stop()
	}

	pd := &pendingDelete{
		path:     e.Path,
		isDir:    e.IsDir,
		source:   e.Source,
		detected: e.DetectedAt,
	}
	captured := e
	pd.timer = time.AfterFunc(renamePairWindow, func() {
		rb.flushIfStillPending(key, captured)
	})
	rb.pending[key] = pd
}

func (rb *renameBuffer) flushIfStillPending(key string, e IngestEvent) {
	rb.mu.Lock()
	pd, ok := rb.pending[key]
	if !ok || pd.path != e.Path {
		rb.mu.Unlock()
		return
	}
	delete(rb.pending, key)
	rb.mu.Unlock()
	rb.emit(e)
}

// OnCreate 查 pending Delete,命中就转 Rename 发出,返回 true 说明已作为 Rename 处理;
// 否则返回 false,由调用方走普通 Create 入队。
func (rb *renameBuffer) OnCreate(e IngestEvent) bool {
	rb.mu.Lock()
	key := filepath.Base(e.Path)
	pd, ok := rb.pending[key]
	if !ok || pd.isDir != e.IsDir || pd.path == e.Path {
		rb.mu.Unlock()
		return false
	}
	pd.timer.Stop()
	delete(rb.pending, key)
	rb.mu.Unlock()

	rb.emit(IngestEvent{
		Kind:       EventRename,
		OldPath:    pd.path,
		Path:       e.Path,
		IsDir:      e.IsDir,
		Source:     e.Source,
		DetectedAt: e.DetectedAt,
	})
	return true
}
