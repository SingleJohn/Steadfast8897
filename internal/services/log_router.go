package services

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var routedLogTargets = []string{
	"http",
	"scan",
	"ingest",
	"scrape",
	"tmdb",
	"gateway",
	"source",
	"provider",
	"resolver",
	"database",
	"playback",
	"tasks",
	"metrics",
	"system",
}

type RoutedLogHandler struct {
	mu       *sync.Mutex
	all      slog.Handler
	console  slog.Handler
	errors   slog.Handler
	targets  map[string]slog.Handler
	group    string
	attrs    []slog.Attr
	closers  []io.Closer
	fallback slog.Handler
}

func NewRoutedLogHandler(logDir string, consoleLevel slog.Leveler, fileLevel slog.Leveler) (*RoutedLogHandler, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}

	closers := make([]io.Closer, 0, len(routedLogTargets)+2)
	today := time.Now().Format("2006-01-02")
	openLog := func(name string) (io.Writer, error) {
		f, err := os.OpenFile(filepath.Join(logDir, fmt.Sprintf("%s-%s.log", name, today)), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, err
		}
		closers = append(closers, f)
		return f, nil
	}

	allWriter, err := openLog("fyms")
	if err != nil {
		return nil, err
	}
	errorWriter, err := openLog("errors")
	if err != nil {
		closeLogFiles(closers)
		return nil, err
	}

	targetHandlers := make(map[string]slog.Handler, len(routedLogTargets))
	for _, target := range routedLogTargets {
		w, err := openLog(target)
		if err != nil {
			closeLogFiles(closers)
			return nil, err
		}
		targetHandlers[target] = slog.NewTextHandler(w, &slog.HandlerOptions{Level: fileLevel})
	}

	return &RoutedLogHandler{
		mu:      &sync.Mutex{},
		all:     slog.NewTextHandler(allWriter, &slog.HandlerOptions{Level: fileLevel}),
		console: slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: consoleLevel}),
		errors:  slog.NewTextHandler(errorWriter, &slog.HandlerOptions{Level: slog.LevelWarn}),
		targets: targetHandlers,
		closers: closers,
	}, nil
}

func NewFallbackLogHandler(consoleLevel slog.Leveler) *RoutedLogHandler {
	return &RoutedLogHandler{
		mu:       &sync.Mutex{},
		fallback: slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: consoleLevel}),
		targets:  map[string]slog.Handler{},
	}
}

func LogLevelFromEnv(name string, fallback slog.Level) slog.Level {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return fallback
	}
}

func (h *RoutedLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if h.fallback != nil {
		return h.fallback.Enabled(ctx, level)
	}
	return h.console.Enabled(ctx, level) || h.all.Enabled(ctx, level) || h.errors.Enabled(ctx, level)
}

func (h *RoutedLogHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.fallback != nil {
		return h.fallback.Handle(ctx, r)
	}

	var firstErr error
	handle := func(handler slog.Handler) {
		if handler == nil || !handler.Enabled(ctx, r.Level) {
			return
		}
		if err := handler.Handle(ctx, r); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	handle(h.console)
	handle(h.all)
	if r.Level >= slog.LevelWarn {
		handle(h.errors)
	}
	target := ResolveLogTarget(h.group, h.attrs, r)
	handle(h.targets[target])
	if target == "" || h.targets[target] == nil {
		handle(h.targets[defaultLogTarget])
	}
	return firstErr
}

func (h *RoutedLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := h.clone()
	next.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	if h.fallback != nil {
		next.fallback = h.fallback.WithAttrs(attrs)
		return next
	}
	next.all = h.all.WithAttrs(attrs)
	next.console = h.console.WithAttrs(attrs)
	next.errors = h.errors.WithAttrs(attrs)
	next.targets = withAttrsForTargets(h.targets, attrs)
	return next
}

func (h *RoutedLogHandler) WithGroup(name string) slog.Handler {
	next := h.clone()
	if next.group != "" {
		next.group += "." + name
	} else {
		next.group = name
	}
	if h.fallback != nil {
		next.fallback = h.fallback.WithGroup(name)
		return next
	}
	next.all = h.all.WithGroup(name)
	next.console = h.console.WithGroup(name)
	next.errors = h.errors.WithGroup(name)
	next.targets = withGroupForTargets(h.targets, name)
	return next
}

func (h *RoutedLogHandler) Close() error {
	return closeLogFiles(h.closers)
}

func (h *RoutedLogHandler) clone() *RoutedLogHandler {
	return &RoutedLogHandler{
		mu:       h.mu,
		all:      h.all,
		console:  h.console,
		errors:   h.errors,
		targets:  h.targets,
		group:    h.group,
		attrs:    append([]slog.Attr(nil), h.attrs...),
		closers:  h.closers,
		fallback: h.fallback,
	}
}

func withAttrsForTargets(targets map[string]slog.Handler, attrs []slog.Attr) map[string]slog.Handler {
	next := make(map[string]slog.Handler, len(targets))
	for target, handler := range targets {
		next[target] = handler.WithAttrs(attrs)
	}
	return next
}

func withGroupForTargets(targets map[string]slog.Handler, name string) map[string]slog.Handler {
	next := make(map[string]slog.Handler, len(targets))
	for target, handler := range targets {
		next[target] = handler.WithGroup(name)
	}
	return next
}

func closeLogFiles(closers []io.Closer) error {
	var firstErr error
	for _, closer := range closers {
		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
