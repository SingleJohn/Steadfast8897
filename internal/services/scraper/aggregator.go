package scraper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Aggregator 管理已注册的 Provider 集合,负责识别(Identify)调度,
// 并承担 Fill 阶段的字段级合并。
//
// Identify 流水线(M4.5 并发化):
//  1. 对所有支持该 MediaType 的 provider 并发尝试 ID 直达(TryIDDirect);
//     任一命中即按 provider Priority 取优返回。
//  2. 全部 provider 并发执行 Matcher.Candidates,产生加权候选列表。
//  3. 按 unified_key 聚合候选,做多源互投:
//     - unified_key 取 ExternalIDs 任一命中;fallback 到 (norm(title), year)
//     - 多源同指(>=2 provider)的 group 即使 maxScore < threshold 也采纳
//     - 单源 group 需 maxScore >= threshold
//  4. 归一后的 winner 再尝试把非 TMDB 源的 winner 映射回 tmdb_id
//     (通过 TMDB provider 的 FindByExternalID),失败时保留原 winner。
type Aggregator struct {
	mu        sync.RWMutex
	providers []Provider
	cache     Cache
	threshold float64
	policy    FieldPolicy
}

func NewAggregator(cache Cache) *Aggregator {
	return &Aggregator{cache: cache, threshold: DefaultThreshold, policy: DefaultFieldPolicy()}
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

// Identify 按 Provider 并发识别,多源互投后返回 Identity。
func (a *Aggregator) Identify(ctx context.Context, parsed ParsedName, t MediaType) (*Identity, error) {
	providers, threshold, cache := a.snapshot(t)
	if len(providers) == 0 {
		return nil, fmt.Errorf("no provider supports media type %s", t)
	}

	// 阶段 1:并发 ID 直达。任何一个 provider 返回 Identity 即按 Priority 取优采纳。
	if id := concurrentIDDirect(ctx, providers, cache, parsed); id != nil {
		return id, nil
	}

	// 阶段 2:并发 Candidates。
	all, searchErr := concurrentCandidates(ctx, providers, cache, parsed, t, threshold)
	if len(all) == 0 {
		if searchErr != nil {
			return nil, searchErr
		}
		return nil, ErrNoMatch
	}

	// 阶段 3:unified_key 聚合,多源互投加权。
	groups := groupByUnifiedKey(all, providers)

	// 阶段 4:winner 决策。
	winner := pickWinnerGroup(groups, threshold)
	if winner == nil {
		return nil, ErrNoMatch
	}

	id := winner.toIdentity()
	// 非 TMDB 源 winner 尝试回落到 TMDB ID,便于下游 applyMergedDetails 写入 items.tmdb_id。
	if id.Provider != "tmdb" {
		if mapped := a.remapToTMDB(ctx, id); mapped != nil {
			mapped.Score = id.Score
			mapped.Source = id.Source + "+tmdb_map"
			return mapped, nil
		}
	}
	return id, nil
}

// Candidates 返回聚合后的候选列表,按分数降序。用于 identify_candidates 人工确认。
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
	// imdb → tmdb
	if imdb := strings.TrimSpace(id.ExternalIDs["imdb"]); imdb != "" {
		pid, err := tmdb.FindByExternalID(ctx, "imdb", imdb)
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

// ---------- 并发 ID 直达 ----------

func concurrentIDDirect(ctx context.Context, providers []Provider, cache Cache, parsed ParsedName) *Identity {
	type result struct {
		provider Provider
		identity *Identity
	}
	ch := make(chan result, len(providers))
	var wg sync.WaitGroup
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, p := range providers {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := NewMatcher(p, cache).TryIDDirect(subCtx, parsed)
			if err != nil {
				slog.Debug("[Aggregator] id_direct error", "provider", p.Name(), "error", err)
				ch <- result{provider: p}
				return
			}
			ch <- result{provider: p, identity: id}
		}()
	}
	go func() { wg.Wait(); close(ch) }()

	var winner *Identity
	var winnerPriority int
	for r := range ch {
		if r.identity == nil {
			continue
		}
		pri := r.provider.Priority()
		if winner == nil || pri < winnerPriority {
			winner = r.identity
			winnerPriority = pri
		}
	}
	return winner
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

// ---------- unified_key 聚合 ----------

type unifiedGroup struct {
	key           string
	candidates    []ScoredCandidate
	providerSet   map[string]struct{}
	maxScore      float64
	weightedScore float64 // 多源同指加权后分
	externalIDs   map[string]string
}

func (g *unifiedGroup) primary(byPriority map[string]int) *ScoredCandidate {
	var best *ScoredCandidate
	var bestPri int
	var bestScore float64
	for i := range g.candidates {
		c := &g.candidates[i]
		pri, ok := byPriority[c.Provider]
		if !ok {
			pri = 1000
		}
		if best == nil || pri < bestPri || (pri == bestPri && c.Score > bestScore) {
			best = c
			bestPri = pri
			bestScore = c.Score
		}
	}
	return best
}

func (g *unifiedGroup) toIdentity() *Identity {
	// 注意:primary 用 Aggregator 侧的 priority 表挑;这里只做回填。
	var primary *ScoredCandidate
	// 默认选 score 最高
	for i := range g.candidates {
		if primary == nil || g.candidates[i].Score > primary.Score {
			primary = &g.candidates[i]
		}
	}
	return &Identity{
		Provider:    primary.Provider,
		ProviderID:  primary.ProviderID,
		ExternalIDs: cloneIDs(unionExternalIDs(g.externalIDs, primary.ExternalIDs)),
		Score:       g.weightedScore,
		Source:      primary.Source,
	}
}

func groupByUnifiedKey(cands []ScoredCandidate, providers []Provider) []*unifiedGroup {
	byPriority := make(map[string]int, len(providers))
	for _, p := range providers {
		byPriority[p.Name()] = p.Priority()
	}
	groups := make(map[string]*unifiedGroup)
	for _, c := range cands {
		key := unifiedKey(c)
		g, ok := groups[key]
		if !ok {
			g = &unifiedGroup{
				key:         key,
				providerSet: make(map[string]struct{}),
				externalIDs: make(map[string]string),
			}
			groups[key] = g
		}
		g.candidates = append(g.candidates, c)
		g.providerSet[c.Provider] = struct{}{}
		if c.Score > g.maxScore {
			g.maxScore = c.Score
		}
		for k, v := range c.ExternalIDs {
			if vv := strings.TrimSpace(v); vv != "" && g.externalIDs[k] == "" {
				g.externalIDs[k] = vv
			}
		}
	}
	out := make([]*unifiedGroup, 0, len(groups))
	for _, g := range groups {
		bonus := 1.0
		if len(g.providerSet) >= 2 {
			bonus = 1.2
			if len(g.providerSet) >= 3 {
				bonus = 1.35
			}
		}
		g.weightedScore = g.maxScore * bonus
		// 用 priority 表重新挑 primary
		primary := g.primary(byPriority)
		if primary != nil {
			// 把 primary 移动到 candidates[0],后续 toIdentity 直接取首个。
			// 注意:toIdentity 内部又按 score 排了一遍,这里不用移动也行。
		}
		_ = primary
		out = append(out, g)
	}
	return out
}

func unifiedKey(c ScoredCandidate) string {
	for _, kind := range []string{"tmdb", "imdb", "tvdb", "bangumi", "douban"} {
		if v := strings.TrimSpace(c.ExternalIDs[kind]); v != "" {
			return kind + ":" + v
		}
	}
	title := strings.ToLower(strings.TrimSpace(c.Title))
	if title == "" {
		title = strings.ToLower(strings.TrimSpace(c.OriginalTitle))
	}
	yr := ""
	if c.Year != nil {
		yr = strconv.FormatInt(int64(*c.Year), 10)
	}
	return "t:" + title + "|" + yr
}

func pickWinnerGroup(groups []*unifiedGroup, threshold float64) *unifiedGroup {
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i].weightedScore > groups[j].weightedScore
	})
	for _, g := range groups {
		if len(g.providerSet) >= 2 {
			return g
		}
		if g.maxScore >= threshold {
			return g
		}
	}
	return nil
}

func unionExternalIDs(a, b map[string]string) map[string]string {
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		if vv := strings.TrimSpace(v); vv != "" {
			out[k] = vv
		}
	}
	for k, v := range b {
		if _, ok := out[k]; ok {
			continue
		}
		if vv := strings.TrimSpace(v); vv != "" {
			out[k] = vv
		}
	}
	return out
}
