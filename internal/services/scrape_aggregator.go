package services

import (
	"net/http"

	"fyms/internal/services/scraper"
	"fyms/internal/services/scraper/providers"
)

// BuildScrapeAggregator 按运行时配置注册启用的 Provider。
// 放在 services 包是为了避免 scraper 包反向导入 providers 子包形成循环。
// 命中场景:ScrapeItemWithClient 每次进来时重建一个 Aggregator(未单例化)。
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
