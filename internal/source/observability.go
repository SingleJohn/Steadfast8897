package source

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"time"
)

func SourceLogger(target string) *slog.Logger {
	target = strings.TrimSpace(target)
	if target == "" {
		target = "source"
	}
	return slog.With("log_target", target)
}

func ErrorType(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "context_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "timeout"
		}
		return "network"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "rate") || strings.Contains(msg, "限流"):
		return "rate_limited"
	case strings.Contains(msg, "unsupported") || strings.Contains(msg, "暂不支持") || strings.Contains(msg, "runtime"):
		return "unsupported"
	case strings.Contains(msg, "ssrf") || strings.Contains(msg, "private") || strings.Contains(msg, "loopback") || strings.Contains(msg, "内网") || strings.Contains(msg, "链路本地"):
		return "blocked_url"
	case strings.Contains(msg, "status"):
		return "http_status"
	case strings.Contains(msg, "json"):
		return "parse"
	default:
		return "error"
	}
}

func URLHash(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil {
		u.User = nil
		u.RawQuery = ""
		u.Fragment = ""
		raw = u.String()
	}
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])[:12]
}

func LogSourceAction(logger *slog.Logger, start time.Time, level slog.Level, message string, attrs ...any) {
	if logger == nil {
		logger = SourceLogger("source")
	}
	fields := append(attrs, "latency_ms", time.Since(start).Milliseconds())
	logger.Log(context.Background(), level, message, fields...)
}

func LogProviderAction(logger *slog.Logger, start time.Time, providerID int64, action string, err error, attrs ...any) {
	status := "ok"
	level := slog.LevelInfo
	if err != nil {
		status = "error"
		level = slog.LevelWarn
		attrs = append(attrs, "error_type", ErrorType(err), "error", err)
	}
	attrs = append([]any{
		"provider_id", providerID,
		"action", action,
		"status", status,
	}, attrs...)
	LogSourceAction(logger, start, level, fmt.Sprintf("[Provider] %s", action), attrs...)
}
