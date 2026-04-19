package scraper

import "context"

// Provider 是 scraper 对外部元数据源的统一契约。
// 实现者应同时具备识别（Search / FindByExternalID）与填充（GetByID）能力；
// Matcher 使用其识别子集，Aggregator.Fill（M4 实装）使用 GetByID。
type Provider interface {
	// Name 返回 provider 的唯一名称："tmdb" / "bangumi" / "douban" / "tvdb" / "fanart"。
	Name() string

	// Priority 越小越优先；用于冲突裁决和字段填充顺序。
	Priority() int

	// Supports 标识本 provider 是否处理该类型的媒体。
	Supports(MediaType) bool

	// Search 根据 Query 返回候选列表。未命中返回空列表而非 error。
	Search(ctx context.Context, t MediaType, q Query) ([]Candidate, error)

	// FindByExternalID 用外部 ID 换回本 provider 的内部 ID。
	// kind 例如 imdb/tmdb/tvdb/bangumi/douban。
	FindByExternalID(ctx context.Context, kind, id string) (string, error)

	// GetByID 返回完整 Details。M3 阶段只有 TMDB 实现；M4 开始多源聚合。
	GetByID(ctx context.Context, t MediaType, id string) (*Details, error)
}
