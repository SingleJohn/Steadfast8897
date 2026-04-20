package services

import (
	"net/http"
	"sync/atomic"

	"fyms/internal/services/scraper"
	"fyms/internal/services/scraper/providers"
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
			register(providers.NewDoubanProvider(httpClient))
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

// ========== Aggregator 缓存(Phase 4 优化) ==========

type cachedAggregatorEntry struct {
	client *TmdbClient
	agg    *scraper.Aggregator
}

var cachedAggregator atomic.Pointer[cachedAggregatorEntry]

// GetScrapeAggregator 返回和 client 指针匹配的缓存 aggregator;未命中时重建并写缓存。
// 缓存 key 用 *TmdbClient 的指针身份 —— worker 持有 stable client 时 O(1) 复用;
// 其他调用点(用户手动 Identify / scrapeEpisodeMetadata)各自 new client 时
// 每次都是 cache miss → 回退到 BuildScrapeAggregator,行为不变。
//
// 配置变更时需调 InvalidateScrapeAggregator(Admin 改 tmdb_api_key / providers 配置后)。
func GetScrapeAggregator(cache scraper.Cache, cfg scraper.RuntimeConfig, tmdb *TmdbClient, httpClient *http.Client) *scraper.Aggregator {
	if e := cachedAggregator.Load(); e != nil && e.client == tmdb {
		return e.agg
	}
	agg := BuildScrapeAggregator(cache, cfg, tmdb, httpClient)
	cachedAggregator.Store(&cachedAggregatorEntry{client: tmdb, agg: agg})
	return agg
}

// InvalidateScrapeAggregator 让下一次 GetScrapeAggregator 重建。
// 供 Admin 修改 scrape 相关 config 后调用。
func InvalidateScrapeAggregator() {
	cachedAggregator.Store(nil)
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
