package services

import (
	"net/http"
	"sync/atomic"

	"golang.org/x/time/rate"

	"fyms/internal/services/scraper"
)

// sharedScrapeCache 在 main.go 启动时通过 SetScrapeCache 注入；
// 用于 Matcher 的搜索结果缓存。未注入时 Matcher 自动退化为无缓存。
var sharedScrapeCache scraper.Cache

func SetScrapeCache(c scraper.Cache) {
	sharedScrapeCache = c
}

// sharedTmdbLimiter 是所有 TMDB 调用路径共享的 rate.Limiter,
// main.go 启动时通过 SetTmdbLimiter 注入。未注入时 tmdbGet 不限流
// (降级兼容,但生产应始终注入)。
var sharedTmdbLimiter *rate.Limiter

func SetTmdbLimiter(l *rate.Limiter) {
	sharedTmdbLimiter = l
}

const (
	TMDB_BASE       = "https://api.themoviedb.org/3"
	TMDB_IMAGE_BASE = "https://image.tmdb.org/t/p"
)

type TmdbClient struct {
	httpClient *http.Client
	apiKeys    []string
	keyIndex   atomic.Uint64
	language   string
}
