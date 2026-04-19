package scraper

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
)

// Fill 基于识别出的 Identity,并发拉取所有已注册 provider 的 Details,
// 按 Priority 升序做字段级合并,返回 MergedDetails。
//
// 主 provider(与 Identity 匹配)用 Identity.ProviderID 直拉;
// 其它 provider 优先用 ExternalIDs 里对应 kind 的 ID(FindByExternalID→GetByID),
// 无对应 ID 时用主 Details 的 title/year 做 Search→取 top1→GetByID。
func (a *Aggregator) Fill(ctx context.Context, match *Identity, parsed ParsedName, t MediaType) (*MergedDetails, error) {
	if match == nil || match.Provider == "" || match.ProviderID == "" {
		return nil, ErrNoMatch
	}
	a.mu.RLock()
	providers := make([]Provider, 0, len(a.providers))
	for _, p := range a.providers {
		if p.Supports(t) {
			providers = append(providers, p)
		}
	}
	a.mu.RUnlock()
	if len(providers) == 0 {
		return nil, ErrNoMatch
	}

	// 主 provider 先拉,拿到 title/year 作为其它 provider Search 的种子。
	var primary Provider
	for _, p := range providers {
		if p.Name() == match.Provider {
			primary = p
			break
		}
	}
	if primary == nil {
		return nil, ErrNoMatch
	}
	primaryDetails, err := primary.GetByID(ctx, t, match.ProviderID)
	if err != nil {
		return nil, err
	}
	if primaryDetails == nil {
		return nil, ErrNoMatch
	}

	// 构造辅源 Search 的种子(TMDB 一般返回中/英文标题)。
	seed := Query{
		Title:         firstNonEmpty(primaryDetails.Title, parsed.Title),
		OriginalTitle: firstNonEmpty(primaryDetails.OriginalTitle, parsed.OriginalTitle),
		Year:          firstNonNilYear(primaryDetails.Year, parsed.Year),
	}

	// 辅源并发拉取。
	type slot struct {
		provider Provider
		details  *Details
	}
	ch := make(chan slot, len(providers))
	var wg sync.WaitGroup
	for _, p := range providers {
		if p.Name() == match.Provider {
			continue
		}
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := fetchSecondary(ctx, p, t, match, seed)
			ch <- slot{provider: p, details: d}
		}()
	}
	go func() { wg.Wait(); close(ch) }()

	// 按 Priority 升序堆叠,首条非空优先。主 provider 永远先参与合并。
	ordered := []*Details{primaryDetails}
	secondary := make(map[string]*Details)
	for s := range ch {
		if s.details != nil {
			secondary[s.provider.Name()] = s.details
		}
	}
	// 按 priority 升序排辅源
	secProviders := make([]Provider, 0, len(secondary))
	for _, p := range providers {
		if p.Name() == match.Provider {
			continue
		}
		if _, ok := secondary[p.Name()]; ok {
			secProviders = append(secProviders, p)
		}
	}
	sort.SliceStable(secProviders, func(i, j int) bool {
		return secProviders[i].Priority() < secProviders[j].Priority()
	})
	for _, p := range secProviders {
		ordered = append(ordered, secondary[p.Name()])
	}

	merged := MergeDetailsWithPolicy(a.FieldPolicy(), match, ordered...)
	if merged != nil {
		slog.Debug("[Aggregator] fill",
			"primary", match.Provider,
			"provider_id", match.ProviderID,
			"secondary_count", len(secProviders))
	}
	return merged, nil
}

// fetchSecondary 辅源抓取:外部 ID 直达优先,失败则 Search 打分挑 top1。
func fetchSecondary(ctx context.Context, p Provider, t MediaType, match *Identity, seed Query) *Details {
	// 1. 对应 provider 的 ID(如 bangumi:xx)直接 GetByID
	if v, ok := match.ExternalIDs[p.Name()]; ok {
		if id := strings.TrimSpace(v); id != "" && id != "0" {
			if d, err := p.GetByID(ctx, t, id); err == nil && d != nil {
				return d
			}
		}
	}
	// 2. 用已知 ExternalIDs 通过 FindByExternalID 转 provider 内部 ID
	for _, kind := range []string{"tmdb", "imdb", "tvdb", "bangumi", "douban"} {
		if kind == p.Name() {
			continue
		}
		v := strings.TrimSpace(match.ExternalIDs[kind])
		if v == "" {
			continue
		}
		pid, err := p.FindByExternalID(ctx, kind, v)
		if err != nil || strings.TrimSpace(pid) == "" || pid == "0" {
			continue
		}
		if d, err := p.GetByID(ctx, t, pid); err == nil && d != nil {
			return d
		}
	}
	// 3. Search + 取 top1
	cands, err := p.Search(ctx, t, seed)
	if err != nil || len(cands) == 0 {
		return nil
	}
	top := cands[0]
	if strings.TrimSpace(top.ProviderID) == "" {
		return nil
	}
	d, err := p.GetByID(ctx, t, top.ProviderID)
	if err != nil {
		return nil
	}
	return d
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func firstNonNilYear(values ...*int32) *int32 {
	for _, v := range values {
		if v != nil && *v > 0 {
			return v
		}
	}
	return nil
}
