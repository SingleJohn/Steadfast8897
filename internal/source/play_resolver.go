package source

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"fyms/internal/repository"
)

type PlayResult struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func ResolvePlay(ctx context.Context, playSource repository.SourcePlaySource) (*PlayResult, error) {
	start := time.Now()
	logger := SourceLogger("resolver")
	switch strings.ToLower(strings.TrimSpace(playSource.ParseMode)) {
	case "", "unknown", "direct":
		if err := ValidateOutboundURL(ctx, playSource.RawURL); err != nil {
			logResolvePlay(logger, start, playSource, err)
			return nil, err
		}
		headers, err := decodeHeaderMap(playSource.Headers)
		if err != nil {
			logResolvePlay(logger, start, playSource, err)
			return nil, err
		}
		logResolvePlay(logger, start, playSource, nil)
		return &PlayResult{URL: playSource.RawURL, Headers: headers}, nil
	case "resolver", "magnet", "cloud_share", "live", "unsupported":
		err := fmt.Errorf("需 runtime，暂不支持: %s", playSource.ParseMode)
		logResolvePlay(logger, start, playSource, err)
		return nil, err
	default:
		err := fmt.Errorf("需 runtime，暂不支持: %s", playSource.ParseMode)
		logResolvePlay(logger, start, playSource, err)
		return nil, err
	}
}

func logResolvePlay(logger *slog.Logger, start time.Time, playSource repository.SourcePlaySource, err error) {
	status := "ok"
	level := slog.LevelInfo
	attrs := []any{
		"provider_id", playSource.ProviderID,
		"action", "resolve_play",
		"status", status,
		"play_source_id", playSource.ID,
		"parse_mode", playSource.ParseMode,
		"url_hash", URLHash(playSource.RawURL),
		"cache_hit", false,
	}
	if err != nil {
		status = "error"
		level = slog.LevelWarn
		attrs[5] = status
		attrs = append(attrs, "error_type", ErrorType(err), "error", err)
	}
	LogSourceAction(logger, start, level, "[Resolver] resolve_play", attrs...)
}

func decodeHeaderMap(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return map[string]string{}, nil
	}
	var anyMap map[string]any
	if err := json.Unmarshal(raw, &anyMap); err != nil {
		return nil, fmt.Errorf("解析播放 headers 失败: %w", err)
	}
	out := make(map[string]string, len(anyMap))
	for key, value := range anyMap {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				out[key] = v
			}
		}
	}
	return out, nil
}
