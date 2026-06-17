package services

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Target    string `json:"target"`
	Message   string `json:"message"`
}

type LogBuffer struct {
	mu      sync.Mutex
	entries []LogEntry
	maxSize int
	start   int
	count   int
}

func NewLogBuffer(maxSize int) *LogBuffer {
	return &LogBuffer{
		entries: make([]LogEntry, maxSize),
		maxSize: maxSize,
	}
}

func (lb *LogBuffer) Push(entry LogEntry) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	idx := (lb.start + lb.count) % lb.maxSize
	lb.entries[idx] = entry
	if lb.count < lb.maxSize {
		lb.count++
	} else {
		lb.start = (lb.start + 1) % lb.maxSize
	}
}

func (lb *LogBuffer) PushMsg(level, target, message string) {
	lb.Push(LogEntry{
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		Level:     level,
		Target:    target,
		Message:   message,
	})
}

func (lb *LogBuffer) Get(level string, limit int) []LogEntry {
	return lb.GetFiltered(level, "", limit)
}

func (lb *LogBuffer) GetFiltered(level, target string, limit int) []LogEntry {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	var filtered []LogEntry
	for i := lb.count - 1; i >= 0 && len(filtered) < limit; i-- {
		idx := (lb.start + i) % lb.maxSize
		e := lb.entries[idx]
		if matchesLogFilter(e, level, target) {
			filtered = append(filtered, e)
		}
	}

	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}
	return filtered
}

func matchesLogFilter(e LogEntry, level, target string) bool {
	levelOK := level == "" || level == "ALL" || e.Level == level
	targetOK := target == "" || target == "ALL" || e.Target == target
	return levelOK && targetOK
}

// BufferHandler is a slog.Handler that captures all log events into LogBuffer
// while delegating to an inner handler for actual output.
type BufferHandler struct {
	inner  slog.Handler
	buffer *LogBuffer
	group  string
	attrs  []slog.Attr
}

func NewBufferHandler(inner slog.Handler, buffer *LogBuffer) *BufferHandler {
	return &BufferHandler{inner: inner, buffer: buffer}
}

func (h *BufferHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *BufferHandler) Handle(ctx context.Context, r slog.Record) error {
	levelStr := "INFO"
	switch {
	case r.Level >= slog.LevelError:
		levelStr = "ERROR"
	case r.Level >= slog.LevelWarn:
		levelStr = "WARN"
	case r.Level >= slog.LevelInfo:
		levelStr = "INFO"
	default:
		levelStr = "DEBUG"
	}

	target := ResolveLogTarget(h.group, h.attrs, r)

	msg := r.Message
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "log_target" || a.Key == "module" || a.Key == "component" {
			return true
		}
		msg += fmt.Sprintf(" %s=%v", a.Key, a.Value)
		return true
	})
	for _, a := range h.attrs {
		if a.Key == "log_target" || a.Key == "module" || a.Key == "component" {
			continue
		}
		msg += fmt.Sprintf(" %s=%v", a.Key, a.Value)
	}

	h.buffer.Push(LogEntry{
		Timestamp: r.Time.UTC().Format("2006-01-02T15:04:05.000Z"),
		Level:     levelStr,
		Target:    target,
		Message:   msg,
	})

	return h.inner.Handle(ctx, r)
}

func (h *BufferHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &BufferHandler{
		inner:  h.inner.WithAttrs(attrs),
		buffer: h.buffer,
		group:  h.group,
		attrs:  append(h.attrs, attrs...),
	}
}

func (h *BufferHandler) WithGroup(name string) slog.Handler {
	g := name
	if h.group != "" {
		g = h.group + "." + name
	}
	return &BufferHandler{
		inner:  h.inner.WithGroup(name),
		buffer: h.buffer,
		group:  g,
		attrs:  h.attrs,
	}
}
