package services

import (
	"context"
	"net/http"
	"sync"

	"fyms/internal/services/scraper"
	"fyms/internal/services/scraper/providers"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BuildScrapeAggregator 按运行时配置注册启用的 Provider。
// 放在 services 包是为了避免 scraper 包反向导入 providers 子包形成循环。
//
// 原先每次 ScrapeItemWithClient 都会走到这里重建 Aggregator(及内部的 HTTP client 连接池、
// Provider 初始化等)—— worker 并发高时是不小的开销。Phase 4 引入 GetScrapeAggregator 做
// key=*TmdbClient 的 atomic.Pointer 缓存,worker 用稳定 client 时可复用。
func BuildScrapeAggregator(cache scraper.Cache, cfg scraper.RuntimeConfig, tmdb scraper.Provider, httpClient *http.Client) *scraper.Aggregator {
	agg := scraper.NewAggregator(cache)
	if cfg.ConfidenceThreshold > 0 {
		agg.SetThreshold(cfg.ConfidenceThreshold)
	}
	if cfg.Strategy != "" {
		agg.SetStrategy(cfg.Strategy)
	}
	if len(cfg.FieldPriority) > 0 {
		agg.SetFieldPolicy(mergeFieldPolicy(scraper.DefaultFieldPolicy(), cfg.FieldPriority))
	}

	enabled := make(map[string]struct{}, len(cfg.ProvidersEnabled))
	for _, n := range cfg.ProvidersEnabled {
		enabled[n] = struct{}{}
	}
	tmdbOnly := len(enabled) == 0

	register := func(p scraper.Provider) {
		if p == nil {
			return
		}
		if cfg.ProviderPriority != nil {
			if v, ok := cfg.ProviderPriority[p.Name()]; ok {
				p = scraper.WithPriority(p, v)
			}
		}
		agg.Register(p)
	}

	if tmdb != nil && (tmdbOnly || hasProvider(enabled, "tmdb")) {
		register(tmdb)
	}
	if !tmdbOnly {
		if hasProvider(enabled, "bangumi") {
			register(providers.NewBangumiProvider(httpClient, cfg.BangumiUA))
		}
		if cfg.DoubanEnabled && hasProvider(enabled, "douban") {
			register(providers.NewDoubanProvider(httpClient, cfg.DoubanUA, cfg.DoubanCookie))
		}
		if cfg.TVDBAPIKey != "" && hasProvider(enabled, "tvdb") {
			register(providers.NewTVDBProvider(httpClient, cfg.TVDBAPIKey, cfg.TVDBPin))
		}
		if cfg.FanartAPIKey != "" && hasProvider(enabled, "fanart") {
			register(providers.NewFanartProvider(httpClient, cfg.FanartAPIKey))
		}
	}
	return agg
}

// ========== Aggregator 缓存 ==========
//
// 缓存 key 从 *TmdbClient 升级为 (client, configHash):
//   - 不同库的 effective config 可能不同 → 对应不同 aggregator 实例
//   - 同 effective config 的多个库 → 共享实例(hash 相同)
//   - TmdbClient 指针变化(凭据重置)→ 自动失效,不会串用旧 http.Transport
//
// 配置变更时调 InvalidateScrapeAggregator 清空整表:
//   - Admin 改 system_config.scrape_* / tmdb_*
//   - Admin 改某库 libraries.scrape_config
//   - TMDB key 轮换导致 TmdbClient 重建

type aggregatorCacheKey struct {
	client *TmdbClient
	hash   uint64
}

var aggregatorCache sync.Map // aggregatorCacheKey -> *scraper.Aggregator

// GetScrapeAggregator 按 global RuntimeConfig 构造/返回 aggregator。
// 用于无 libraryID 的调用点(手动 Identify、SearchTMDB 等)。
// 同 (client, configHash) 的请求命中缓存;不同 hash 独立构造。
func GetScrapeAggregator(cache scraper.Cache, cfg scraper.RuntimeConfig, tmdb *TmdbClient, httpClient *http.Client) *scraper.Aggregator {
	key := aggregatorCacheKey{client: tmdb, hash: hashRuntimeConfig(cfg)}
	if v, ok := aggregatorCache.Load(key); ok {
		return v.(*scraper.Aggregator)
	}
	agg := BuildScrapeAggregator(cache, cfg, tmdb, httpClient)
	actual, _ := aggregatorCache.LoadOrStore(key, agg)
	return actual.(*scraper.Aggregator)
}

// GetScrapeAggregatorForLibrary 按 libraryID 读 effective config 构造 aggregator。
// libraryID 为空时退化为 GetScrapeAggregator(global cfg),行为与旧代码一致。
func GetScrapeAggregatorForLibrary(
	ctx context.Context, pool *pgxpool.Pool,
	cache scraper.Cache, tmdb *TmdbClient, httpClient *http.Client,
	libraryID string,
) *scraper.Aggregator {
	cfg := LoadEffectiveScrapeConfig(ctx, pool, libraryID)
	return GetScrapeAggregator(cache, cfg, tmdb, httpClient)
}

// InvalidateScrapeAggregator 清空所有 (client, hash) 桶。
// 供 Admin 修改 scrape 相关配置后调用。
func InvalidateScrapeAggregator() {
	aggregatorCache.Range(func(k, _ any) bool {
		aggregatorCache.Delete(k)
		return true
	})
}

func hasProvider(set map[string]struct{}, name string) bool {
	_, ok := set[name]
	return ok
}

// mergeFieldPolicy 把 system_config 里的 FieldPriority 部分覆盖到默认 policy。
// 未在配置中出现的字段保留默认,避免少配一项就让某字段退化成"传入顺序"。
func mergeFieldPolicy(base scraper.FieldPolicy, override map[string][]string) scraper.FieldPolicy {
	pick := func(key string, fallback []string) []string {
		if v, ok := override[key]; ok && len(v) > 0 {
			return v
		}
		return fallback
	}
	return scraper.FieldPolicy{
		Overview:      pick("overview", base.Overview),
		Title:         pick("title", base.Title),
		OriginalTitle: pick("original_title", base.OriginalTitle),
		Tagline:       pick("tagline", base.Tagline),
		Premiered:     pick("premiered", base.Premiered),
		Year:          pick("year", base.Year),
		Rating:        pick("rating", base.Rating),
		Actors:        pick("actors", base.Actors),
		Poster:        pick("poster", base.Poster),
		Backdrop:      pick("backdrop", base.Backdrop),
		SeasonPoster:  pick("season_poster", base.SeasonPoster),
	}
}
