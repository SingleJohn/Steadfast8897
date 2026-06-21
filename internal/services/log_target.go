package services

import (
	"log/slog"
	"strings"
)

const defaultLogTarget = "system"

func ResolveLogTarget(group string, attrs []slog.Attr, r slog.Record) string {
	if target := targetFromAttrs(attrs); target != "" {
		return target
	}
	var target string
	r.Attrs(func(a slog.Attr) bool {
		if v := targetFromAttr(a); v != "" {
			target = v
			return false
		}
		return true
	})
	if target != "" {
		return target
	}
	if group != "" {
		return normalizeLogTarget(group)
	}
	return inferLogTargetFromMessage(r.Message)
}

func targetFromAttrs(attrs []slog.Attr) string {
	for _, a := range attrs {
		if target := targetFromAttr(a); target != "" {
			return target
		}
	}
	return ""
}

func targetFromAttr(a slog.Attr) string {
	switch a.Key {
	case "log_target", "module", "component":
		return normalizeLogTarget(a.Value.String())
	default:
		return ""
	}
}

func normalizeLogTarget(target string) string {
	target = strings.TrimSpace(strings.ToLower(target))
	if target == "" {
		return ""
	}
	target = strings.TrimPrefix(target, "[")
	target = strings.TrimSuffix(target, "]")
	target = strings.ReplaceAll(target, ".", "-")
	target = strings.ReplaceAll(target, "_", "-")
	target = strings.ReplaceAll(target, " ", "-")
	switch target {
	case "sql", "db", "postgres", "postgresql":
		return "database"
	case "play", "stream":
		return "playback"
	case "watcher", "filewatcher", "file-watcher":
		return "ingest"
	case "refresh", "backfill", "probe", "cleanup", "update":
		return "tasks"
	}
	return target
}

func inferLogTargetFromMessage(message string) string {
	switch {
	case strings.HasPrefix(message, "[Scan]") || strings.HasPrefix(message, "[Prune]") || strings.HasPrefix(message, "scan:"):
		return "scan"
	case strings.HasPrefix(message, "[Ingest]") || strings.HasPrefix(message, "[FileWatcher]"):
		return "ingest"
	case strings.HasPrefix(message, "[Scrape") || strings.HasPrefix(message, "[Identify]") || strings.HasPrefix(message, "[Rescrape]"):
		return "scrape"
	case strings.HasPrefix(message, "[TMDB]") || strings.HasPrefix(message, "[Douban]") || strings.HasPrefix(message, "[Aggregator]") || strings.HasPrefix(message, "[Matcher]"):
		return "tmdb"
	case strings.HasPrefix(message, "[Source]") || strings.HasPrefix(message, "[SourceGC]"):
		return "source"
	case strings.HasPrefix(message, "[Provider]"):
		return "provider"
	case strings.HasPrefix(message, "[Resolver]"):
		return "resolver"
	case strings.HasPrefix(message, "[Stream]") || strings.HasPrefix(message, "[Play]") || strings.HasPrefix(message, "playback "):
		return "playback"
	case strings.HasPrefix(message, "[Metrics]"):
		return "metrics"
	case strings.HasPrefix(message, "[Backup]") || strings.HasPrefix(message, "[Restore]") || strings.HasPrefix(message, "Server "):
		return "system"
	case strings.HasPrefix(message, "SQL ") || strings.Contains(message, " SQL "):
		return "database"
	case strings.HasPrefix(message, "chain:") || strings.HasPrefix(message, "cleanup:") || strings.HasPrefix(message, "update ") || strings.Contains(message, " run "):
		return "tasks"
	default:
		return defaultLogTarget
	}
}
