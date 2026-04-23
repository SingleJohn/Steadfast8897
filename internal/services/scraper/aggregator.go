package scraper

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Aggregator 管理已注册的 Provider 集合,负责识别(Identify)调度,
// 并承担 Fill 阶段的字段级合并。
//
// Identify 流水线:
//  1. 按 Priority 升序遍历所有支持该 MediaType 的 provider
//  2. 每个 provider 调 Matcher.Identify,其内部先尝试 TryIDDirect,
//     失败后走 Candidates + pickBest(阈值过滤)
//  3. 首个非 nil Identity 采纳;非 TMDB 源 winner 尝试映射回 tmdb_id
//     (通过 TMDB provider 的 FindByExternalID),失败时保留原 winner
//
// Candidates 仍保留多源并发,供未匹配面板人工确认使用。
type Aggregator struct {
	mu        sync.RWMutex
	providers []Provider
	cache     Cache
	threshold float64
	policy    FieldPolicy
}

func NewAggregator(cache Cache) *Aggregator {
	return &Aggregator{
		cache:     cache,
		threshold: DefaultThreshold,
		policy:    DefaultFieldPolicy(),
	}
}

// SetFieldPolicy 覆盖字段级合并策略;BuildScrapeAggregator 在注入 RuntimeConfig 时调用。
func (a *Aggregator) SetFieldPolicy(p FieldPolicy) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.policy = p
}

// FieldPolicy 返回当前 policy 的只读副本。Fill 内部使用。
func (a *Aggregator) FieldPolicy() FieldPolicy {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.policy
}

// Register 注册一个 Provider。名称重复时后者覆盖前者。
func (a *Aggregator) Register(p Provider) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, existing := range a.providers {
		if existing.Name() == p.Name() {
			a.providers[i] = p
			a.sortLocked()
			return
		}
	}
	a.providers = append(a.providers, p)
	a.sortLocked()
}

// Unregister 按名称移除 Provider。
func (a *Aggregator) Unregister(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.providers[:0]
	for _, p := range a.providers {
		if p.Name() != name {
			out = append(out, p)
		}
	}
	a.providers = out
}

// Providers 返回当前注册的 provider 名称(按优先级)。
func (a *Aggregator) Providers() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	names := make([]string, 0, len(a.providers))
	for _, p := range a.providers {
		names = append(names, p.Name())
	}
	return names
}

// SetThreshold 覆盖识别阶段的置信度阈值。
func (a *Aggregator) SetThreshold(t float64) {
	if t <= 0 || t > 1 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.threshold = t
}

// Identify 按 Priority 升序逐个 provider 尝试 Matcher.Identify,
// 首个返回非 nil Identity 即采纳;非 TMDB winner 尝试映射回 tmdb_id。
// providers 参数已由 snapshot 按 Priority 升序过滤返回。
func (a *Aggregator) Identify(ctx context.Context, parsed ParsedName, t MediaType) (*Identity, error) {
	providers, threshold, cache := a.snapshot(t)
	if len(providers) == 0 {
		return nil, fmt.Errorf("no provider supports media type %s", t)
	}
	for _, p := range providers {
		id, err := NewMatcher(p, cache).WithThreshold(threshold).Identify(ctx, parsed, t)
		if err != nil || id == nil {
			continue
		}
		if id.Provider != "tmdb" {
			if mapped := a.remapToTMDB(ctx, id); mapped != nil {
				mapped.Score = id.Score
				mapped.Source = id.Source + "+tmdb_map"
				return mapped, nil
			}
		}
		return id, nil
	}
	return nil, ErrNoMatch
}

// Candidates 返回聚合后的候选列表,按分数降序。用于 identify_candidates 人工确认。
// 保留多源并发:人工纠错时需要看到所有来源的候选,方便用户手动挑。
func (a *Aggregator) Candidates(ctx context.Context, parsed ParsedName, t MediaType) ([]ScoredCandidate, error) {
	providers, threshold, cache := a.snapshot(t)
	if len(providers) == 0 {
		return nil, fmt.Errorf("no provider supports media type %s", t)
	}
	all, _ := concurrentCandidates(ctx, providers, cache, parsed, t, threshold)
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].Score > all[j].Score
	})
	if len(all) > candidateTopN {
		all = all[:candidateTopN]
	}
	return all, nil
}

// ProviderByName 返回指定名称的 Provider。
func (a *Aggregator) ProviderByName(name string) Provider {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, p := range a.providers {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

// RegisteredProviders 返回所有已注册 provider(按 Priority 升序)。
func (a *Aggregator) RegisteredProviders() []Provider {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]Provider, len(a.providers))
	copy(out, a.providers)
	return out
}

func (a *Aggregator) snapshot(t MediaType) ([]Provider, float64, Cache) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	ps := make([]Provider, 0, len(a.providers))
	for _, p := range a.providers {
		if p.Supports(t) {
			ps = append(ps, p)
		}
	}
	return ps, a.threshold, a.cache
}

func (a *Aggregator) sortLocked() {
	sort.SliceStable(a.providers, func(i, j int) bool {
		return a.providers[i].Priority() < a.providers[j].Priority()
	})
}

// remapToTMDB 给定非 TMDB 源 winner,尝试走 TMDB provider 的 FindByExternalID 转换。
// winner.ExternalIDs 里若已有 tmdb/imdb,优先用。
func (a *Aggregator) remapToTMDB(ctx context.Context, id *Identity) *Identity {
	tmdb := a.ProviderByName("tmdb")
	if tmdb == nil || id == nil {
		return nil
	}
	// 已经有 tmdb external id 就直接用
	if v, ok := id.ExternalIDs["tmdb"]; ok && strings.TrimSpace(v) != "" && v != "0" {
		return &Identity{
			Provider:    "tmdb",
			ProviderID:  strings.TrimSpace(v),
			ExternalIDs: cloneIDs(id.ExternalIDs),
		}
	}
	for _, kind := range []string{"imdb", "tvdb"} {
		externalID := strings.TrimSpace(id.ExternalIDs[kind])
		if externalID == "" {
			continue
		}
		pid, err := tmdb.FindByExternalID(ctx, kind, externalID)
		if err == nil && strings.TrimSpace(pid) != "" && pid != "0" {
			ids := cloneIDs(id.ExternalIDs)
			if ids == nil {
				ids = map[string]string{}
			}
			ids["tmdb"] = pid
			return &Identity{
				Provider:    "tmdb",
				ProviderID:  pid,
				ExternalIDs: ids,
			}
		}
	}
	return nil
}

// ---------- 并发 Candidates ----------

func concurrentCandidates(ctx context.Context, providers []Provider, cache Cache, parsed ParsedName, t MediaType, threshold float64) ([]ScoredCandidate, error) {
	type result struct {
		items []ScoredCandidate
		err   error
	}
	ch := make(chan result, len(providers))
	var wg sync.WaitGroup
	for _, p := range providers {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, err := NewMatcher(p, cache).WithThreshold(threshold).Candidates(ctx, parsed, t)
			ch <- result{items: items, err: err}
		}()
	}
	go func() { wg.Wait(); close(ch) }()

	var out []ScoredCandidate
	var firstErr error
	for r := range ch {
		if r.err != nil {
			if firstErr == nil && !errors.Is(r.err, ErrBreakerOpen) && !errors.Is(r.err, context.Canceled) {
				firstErr = r.err
			}
			continue
		}
		out = append(out, r.items...)
	}
	return out, firstErr
}
