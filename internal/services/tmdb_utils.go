package services

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
	"fyms/internal/services/scraper"
)

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		vv := strings.TrimSpace(v)
		if vv == "" {
			continue
		}
		dst[k] = vv
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}

func formatYear(y *int32) string {
	if y == nil {
		return ""
	}
	return strconv.FormatInt(int64(*y), 10)
}

func roundFloat(v float64, digits int) float64 {
	p := 1.0
	for i := 0; i < digits; i++ {
		p *= 10
	}
	return float64(int64(v*p+0.5)) / p
}

func dedupeNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// resolveTMDBIDFromIdentity 从 Identity 提取 tmdb_id;兼容 Provider=tmdb 的直达
// 与辅源 winner(ExternalIDs["tmdb"]) 两种情况。返回 0 表示无法获取。
func resolveTMDBIDFromIdentity(ident *scraper.Identity) int64 {
	if ident == nil {
		return 0
	}
	if ident.Provider == "tmdb" {
		if id, err := strconv.ParseInt(strings.TrimSpace(ident.ProviderID), 10, 64); err == nil && id > 0 {
			return id
		}
	}
	if v := strings.TrimSpace(ident.ExternalIDs["tmdb"]); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			return id
		}
	}
	return 0
}

func mediaTypeFor(itemType string) (scraper.MediaType, bool) {
	switch itemType {
	case "Movie":
		return scraper.MediaMovie, true
	case "Series":
		return scraper.MediaSeries, true
	default:
		return "", false
	}
}

const missingMetadataScrapeWhere = `(overview IS NULL OR overview = '')
    AND type IN ('Movie', 'Series')`

func GetMissingScrapeCount(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	return repository.NewBackgroundTaskRepository(pool).GetMissingScrapeCount(ctx)
}

func GetTopLevelItemCount(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	return repository.NewBackgroundTaskRepository(pool).GetTopLevelItemCount(ctx)
}

// ========== JSON helpers ==========

func jsonInt64(m map[string]interface{}, key string) (int64, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	case int64:
		return n, true
	case int:
		return int64(n), true
	}
	return 0, false
}

func jsonFloat64(m map[string]interface{}, key string) *float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch n := v.(type) {
	case float64:
		if n == 0 {
			return nil
		}
		return &n
	case json.Number:
		f, err := n.Float64()
		if err != nil || f == 0 {
			return nil
		}
		return &f
	}
	return nil
}

func jsonStringNonEmpty(m map[string]interface{}, key string) *string {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	return &s
}

func jsonStringPtr(m map[string]interface{}, key string) *string {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return nil
	}
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

func parseYearPrefix(dateStr string) int {
	if len(dateStr) < 4 {
		return 0
	}
	y := 0
	for _, c := range dateStr[:4] {
		if c < '0' || c > '9' {
			return 0
		}
		y = y*10 + int(c-'0')
	}
	return y
}
