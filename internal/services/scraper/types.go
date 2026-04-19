package scraper

import (
	"context"
	"time"
)

type MediaType string

const (
	MediaMovie  MediaType = "Movie"
	MediaSeries MediaType = "Series"
)

// Query 打包一次搜索调用需要的上下文；便于后续给 Provider 传额外 hint。
type Query struct {
	Title         string
	OriginalTitle string
	Year          *int32
	Language      string
	Hint          string
}

// Candidate 是从 Provider 搜索返回的一条候选结果。
// ProviderID 是 provider 内部主键；跨源归一统一依赖 ExternalIDs。
type Candidate struct {
	ProviderID    string
	Title         string
	OriginalTitle string
	Year          *int32
	Popularity    float64
	PosterURL     string
	ExternalIDs   map[string]string
}

// Identity 是识别阶段的产物，不含字段级详情；交给后续 Fill 阶段去拉取。
type Identity struct {
	Provider    string
	ProviderID  string
	ExternalIDs map[string]string
	Score       float64
	Source      string
}

type ScoredCandidate struct {
	Provider    string
	ProviderID  string
	ExternalIDs map[string]string
	Title       string
	OriginalTitle string
	Year        *int32
	Score       float64
	Popularity  float64
	PosterURL   string
	Source      string
}

// Actor 是 Provider 统一返回的演职员记录。与 services.NfoActor 对齐。
type Actor struct {
	Name     string
	Role     string
	Order    int
	TmdbID   *int32
	ImageURL *string
}

// Details 是 Provider.GetByID 返回的完整元数据，用于 M4 Aggregator.Fill
// 做字段级合并。M3 阶段由 TmdbClient 实现，但暂不替代 applyTMDBDetails 的
// raw-map 流程；字段持续稳定后再切换。
type Details struct {
	Provider      string
	ProviderID    string
	ExternalIDs   map[string]string
	Platforms     []string
	Title         string
	OriginalTitle string
	Overview      string
	Tagline       string
	Year          *int32
	Premiered     string
	Rating        *float64
	Genres        []string
	Studios       []string
	Actors        []Actor
	Directors     []string
	PosterURLs    []string
	BackdropURLs  []string
	SeasonPosters map[int32]string
}

// Cache 是 scraper 包对外声明的缓存接口，避免反向依赖 services。
// services.CacheService 通过 adapter 实现它。
type Cache interface {
	GetJSON(ctx context.Context, key string, dest any) bool
	SetJSON(ctx context.Context, key string, value any, ttl time.Duration)
}
