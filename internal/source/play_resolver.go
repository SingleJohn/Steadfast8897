package source

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"fyms/internal/repository"
)

type PlayResult struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func ResolvePlay(ctx context.Context, playSource repository.SourcePlaySource) (*PlayResult, error) {
	switch strings.ToLower(strings.TrimSpace(playSource.ParseMode)) {
	case "", "unknown", "direct":
		if err := ValidateOutboundURL(ctx, playSource.RawURL); err != nil {
			return nil, err
		}
		headers, err := decodeHeaderMap(playSource.Headers)
		if err != nil {
			return nil, err
		}
		return &PlayResult{URL: playSource.RawURL, Headers: headers}, nil
	case "resolver", "magnet", "cloud_share", "live", "unsupported":
		return nil, fmt.Errorf("需 runtime，暂不支持: %s", playSource.ParseMode)
	default:
		return nil, fmt.Errorf("需 runtime，暂不支持: %s", playSource.ParseMode)
	}
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
