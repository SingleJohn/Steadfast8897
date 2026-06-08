package services

import "time"

// EventKind 是 ingest 事件流中统一的事件类型。
// Phase 1 的三大 producer(fsnotify FileWatcher、webhook HandleFileChangeEvents、
// Phase 3 的手动扫描)都产出这几种事件;Worker 是唯一的消费端。
type EventKind int

const (
	EventCreate EventKind = iota
	EventModify
	EventDelete
	EventRename
)

func (k EventKind) String() string {
	switch k {
	case EventCreate:
		return "create"
	case EventModify:
		return "modify"
	case EventDelete:
		return "delete"
	case EventRename:
		return "rename"
	}
	return "unknown"
}

// IngestEvent 是 ingest Worker 的唯一事件数据结构。
// OldPath 仅在 Rename 时有意义;其他场景下为空。
//
// Tag 用于 Barrier 等待:scan(Phase 3)每事件设 Tag=libraryID,
// 处理完成后计数归零 Barrier 可返回。FileWatcher / Webhook 不设 Tag(不参与计数)。
type IngestEvent struct {
	Kind           EventKind
	Path           string
	OldPath        string
	IsDir          bool
	Source         string // "fsnotify" / "webhook" / "scan"
	Tag            string
	LibraryID      string
	CollectionType string
	DetectedAt     time.Time
}
