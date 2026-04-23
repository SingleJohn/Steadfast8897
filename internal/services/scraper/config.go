package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultBangumiUA = "fyms/1.0 (github.com/ffoocn/fyms)"
	defaultDoubanUA  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// ProviderCredentials 聚合所有"访问外部 provider 所需的凭据/UA"。
// 与 RuntimeConfig 的行为字段分开:
//   - 行为字段(ProvidersEnabled / FieldPriority / Threshold 等)可被库级 override 覆盖
//   - 凭据永远全局,不暴露给前端 effective 响应
//
// 是否启用豆瓣源由 ProvidersEnabled 列表决定,不在此结构里单独开关。
type ProviderCredentials struct {
	TVDBAPIKey   string
	TVDBPin      string
	FanartAPIKey string
	BangumiUA    string
	DoubanUA     string
	DoubanCookie string
}

type RuntimeConfig struct {
	ProvidersEnabled          []string
	ProviderPriority          map[string]int
	FieldPriority             map[string][]string
	ConfidenceThreshold       float64
	AutoApply                 bool
	AdultContentFilterEnabled bool
	Credentials               ProviderCredentials
}

func LoadRuntimeConfig(ctx context.Context, pool *pgxpool.Pool) RuntimeConfig {
	cfg := RuntimeConfig{
		ProvidersEnabled:          []string{"tmdb", "bangumi", "douban", "tvdb", "fanart"},
		ConfidenceThreshold:       DefaultThreshold,
		AutoApply:                 true,
		AdultContentFilterEnabled: true,
		Credentials: ProviderCredentials{
			DoubanUA:  defaultDoubanUA,
			BangumiUA: defaultBangumiUA,
		},
	}

	if pool == nil {
		return cfg
	}

	if v := loadConfigValue(ctx, pool, "scrape_providers_enabled"); v != "" {
		var names []string
		if json.Unmarshal([]byte(v), &names) == nil {
			cfg.ProvidersEnabled = normalizeProviderNames(names)
		}
	}
	if v := loadConfigValue(ctx, pool, "scrape_provider_priority"); v != "" {
		var raw map[string]int
		if json.Unmarshal([]byte(v), &raw) == nil {
			cfg.ProviderPriority = normalizeProviderPriority(raw)
		}
	}
	if v := loadConfigValue(ctx, pool, "scrape_field_priority"); v != "" {
		var raw map[string][]string
		if json.Unmarshal([]byte(v), &raw) == nil {
			cfg.FieldPriority = normalizeFieldPriority(raw)
		}
	}
	if v := loadConfigValue(ctx, pool, "scrape_confidence_threshold"); v != "" {
		if parsed := parseThreshold(v); parsed > 0 {
			cfg.ConfidenceThreshold = parsed
		}
	}
	if v := loadConfigValue(ctx, pool, "scrape_auto_apply"); v != "" {
		cfg.AutoApply = parseBool(v, true)
	}
	if v := loadConfigValue(ctx, pool, "scrape_adult_content_filter_enabled"); v != "" {
		cfg.AdultContentFilterEnabled = parseBool(v, true)
	}
	if v := loadConfigValue(ctx, pool, "douban_ua"); v != "" {
		cfg.Credentials.DoubanUA = strings.TrimSpace(v)
	}
	if cfg.Credentials.DoubanUA == "" {
		cfg.Credentials.DoubanUA = defaultDoubanUA
	}
	cfg.Credentials.DoubanCookie = strings.TrimSpace(loadConfigValue(ctx, pool, "douban_cookie"))
	if v := loadConfigValue(ctx, pool, "bangumi_ua"); v != "" {
		cfg.Credentials.BangumiUA = strings.TrimSpace(v)
	}
	if cfg.Credentials.BangumiUA == "" {
		cfg.Credentials.BangumiUA = defaultBangumiUA
	}
	cfg.Credentials.TVDBAPIKey = strings.TrimSpace(loadConfigValue(ctx, pool, "tvdb_api_key"))
	cfg.Credentials.TVDBPin = strings.TrimSpace(loadConfigValue(ctx, pool, "tvdb_pin"))
	cfg.Credentials.FanartAPIKey = strings.TrimSpace(loadConfigValue(ctx, pool, "fanart_api_key"))
	return cfg
}

func loadConfigValue(ctx context.Context, pool *pgxpool.Pool, key string) string {
	var val *string
	if err := pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = $1", key).Scan(&val); err != nil || val == nil {
		return ""
	}
	return strings.TrimSpace(*val)
}

func normalizeFieldPriority(raw map[string][]string) map[string][]string {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string][]string, len(raw))
	for k, list := range raw {
		field := strings.ToLower(strings.TrimSpace(k))
		if field == "" || len(list) == 0 {
			continue
		}
		names := make([]string, 0, len(list))
		seen := make(map[string]struct{}, len(list))
		for _, n := range list {
			name := strings.ToLower(strings.TrimSpace(n))
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
		if len(names) > 0 {
			out[field] = names
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeProviderPriority(raw map[string]int) map[string]int {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]int, len(raw))
	for k, v := range raw {
		name := strings.ToLower(strings.TrimSpace(k))
		if name == "" {
			continue
		}
		out[name] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeProviderNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func parseBool(raw string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return fallback
	}
}

func parseThreshold(raw string) float64 {
	var v float64
	if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%f", &v); err != nil {
		return 0
	}
	if v <= 0 || v > 1 {
		return 0
	}
	return v
}
