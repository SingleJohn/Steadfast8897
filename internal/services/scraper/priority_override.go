package scraper

import "context"

// WithPriority 返回一个 Provider 包装,重写其 Priority() 而保持其它方法透传。
// 用于 BuildScrapeAggregator 按 system_config.scrape_provider_priority 覆盖 Provider
// 源码硬编码的 Priority。
func WithPriority(p Provider, priority int) Provider {
	if p == nil {
		return nil
	}
	return &priorityOverride{inner: p, priority: priority}
}

type priorityOverride struct {
	inner    Provider
	priority int
}

func (p *priorityOverride) Name() string                     { return p.inner.Name() }
func (p *priorityOverride) Priority() int                    { return p.priority }
func (p *priorityOverride) Supports(t MediaType) bool        { return p.inner.Supports(t) }
func (p *priorityOverride) Search(ctx context.Context, t MediaType, q Query) ([]Candidate, error) {
	return p.inner.Search(ctx, t, q)
}
func (p *priorityOverride) FindByExternalID(ctx context.Context, kind, id string) (string, error) {
	return p.inner.FindByExternalID(ctx, kind, id)
}
func (p *priorityOverride) GetByID(ctx context.Context, t MediaType, id string) (*Details, error) {
	return p.inner.GetByID(ctx, t, id)
}
