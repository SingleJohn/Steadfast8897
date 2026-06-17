package services

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	"fyms/internal/repository"
	"fyms/internal/services/scraper"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LoadLibraryScrapeOverride 读取 libraries.scrape_config(JSONB)并反序列化成 ConfigOverride。
// libraryID 为空 / 行不存在 / scrape_config IS NULL / 内容反序列化后全空 都返回 (nil, nil)。
// JSON 解析失败返回 error,上层应降级到纯全局配置。
func LoadLibraryScrapeOverride(ctx context.Context, pool *pgxpool.Pool, libraryID string) (*scraper.ConfigOverride, error) {
	if strings.TrimSpace(libraryID) == "" {
		return nil, nil
	}
	libID, err := uuid.Parse(libraryID)
	if err != nil {
		return nil, nil
	}
	lib, err := repository.NewLibraryRepository(pool).GetLibraryByID(ctx, libID)
	if err != nil {
		return nil, fmt.Errorf("load library scrape_config: %w", err)
	}
	if lib == nil || lib.ScrapeConfig == nil || strings.TrimSpace(*lib.ScrapeConfig) == "" {
		return nil, nil
	}
	var ov scraper.ConfigOverride
	if err := json.Unmarshal([]byte(*lib.ScrapeConfig), &ov); err != nil {
		return nil, fmt.Errorf("parse scrape_config JSON: %w", err)
	}
	if ov.IsEmpty() {
		return nil, nil
	}
	return &ov, nil
}

// LoadEffectiveScrapeConfig 返回全局 + 库级 override 合并后的最终配置。
// 当 libraryID 为空 / 库不存在 / override 为空 / 解析失败时降级到纯全局配置。
// 选择"读错降级"而不是返回 error,避免单个库 scrape_config 损坏波及整个 worker。
func LoadEffectiveScrapeConfig(ctx context.Context, pool *pgxpool.Pool, libraryID string) scraper.RuntimeConfig {
	global := scraper.LoadRuntimeConfig(ctx, pool)
	if strings.TrimSpace(libraryID) == "" {
		return global
	}
	override, err := LoadLibraryScrapeOverride(ctx, pool, libraryID)
	if err != nil || override == nil {
		return global
	}
	return scraper.MergeOverride(global, override)
}

// hashRuntimeConfig 给 aggregator 缓存算稳定 key。
// 覆盖所有影响 BuildScrapeAggregator 行为的字段:
//   - ProvidersEnabled(order-sensitive)
//   - ProviderPriority / FieldPriority(key 先排序保证稳定)
//   - ConfidenceThreshold / AutoApply
//   - 凭据的"是否非空"(影响 provider 是否注册)
//
// 不含凭据明文,避免把 api key 塞进日志/metrics。
func hashRuntimeConfig(cfg scraper.RuntimeConfig) uint64 {
	h := fnv.New64a()

	for _, p := range cfg.ProvidersEnabled {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	h.Write([]byte{1})

	priKeys := make([]string, 0, len(cfg.ProviderPriority))
	for k := range cfg.ProviderPriority {
		priKeys = append(priKeys, k)
	}
	sort.Strings(priKeys)
	for _, k := range priKeys {
		fmt.Fprintf(h, "%s=%d;", k, cfg.ProviderPriority[k])
	}
	h.Write([]byte{2})

	fieldKeys := make([]string, 0, len(cfg.FieldPriority))
	for k := range cfg.FieldPriority {
		fieldKeys = append(fieldKeys, k)
	}
	sort.Strings(fieldKeys)
	for _, k := range fieldKeys {
		h.Write([]byte(k))
		h.Write([]byte{':'})
		for _, v := range cfg.FieldPriority[k] {
			h.Write([]byte(v))
			h.Write([]byte{','})
		}
		h.Write([]byte{';'})
	}
	h.Write([]byte{3})

	fmt.Fprintf(h, "t=%.4f;a=%t;adult_filter=%t",
		cfg.ConfidenceThreshold, cfg.AutoApply, cfg.AdultContentFilterEnabled)
	h.Write([]byte{4})

	credTag := func(s string) byte {
		if strings.TrimSpace(s) == "" {
			return '0'
		}
		return '1'
	}
	h.Write([]byte{
		credTag(cfg.Credentials.TVDBAPIKey),
		credTag(cfg.Credentials.TVDBPin),
		credTag(cfg.Credentials.FanartAPIKey),
		credTag(cfg.Credentials.DoubanCookie),
	})
	return h.Sum64()
}
