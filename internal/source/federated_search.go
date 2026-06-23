package source

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"fyms/internal/repository"
)

const (
	defaultFederatedSearchLimit        = 50
	maxFederatedSearchLimit            = 100
	defaultFederatedSearchGrace        = 3 * time.Second
	maxFederatedProviderDefaultTimeout = 15 * time.Second
)

type FederatedSearchRequest struct {
	Keyword string
	Limit   int
}

type FederatedSearchResponse struct {
	Keyword    string                  `json:"keyword"`
	Total      int                     `json:"total"`
	Items      []FederatedSearchItem   `json:"items"`
	Errors     []FederatedSearchError  `json:"errors,omitempty"`
	Provider   FederatedSearchProvider `json:"provider"`
	LatencyMS  int64                   `json:"latency_ms"`
	Truncated  bool                    `json:"truncated"`
	CacheWrite bool                    `json:"cache_write"`
}

type FederatedSearchProvider struct {
	Total   int `json:"total"`
	Success int `json:"success"`
	Failed  int `json:"failed"`
}

type FederatedSearchItem struct {
	PublicUUID     string                          `json:"public_uuid"`
	Title          string                          `json:"title"`
	Year           *int32                          `json:"year,omitempty"`
	ItemType       string                          `json:"item_type"`
	NormalizedKind string                          `json:"normalized_kind"`
	Region         *string                         `json:"region,omitempty"`
	PosterURL      *string                         `json:"poster_url,omitempty"`
	Remarks        *string                         `json:"remarks,omitempty"`
	ProviderCount  int                             `json:"provider_count"`
	Providers      []FederatedSearchItemProvider   `json:"providers"`
	Score          int                             `json:"score"`
	SourceItems    []FederatedSearchItemSourceItem `json:"source_items"`
}

type FederatedSearchItemProvider struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	SourceKey    string  `json:"source_key"`
	HealthStatus string  `json:"health_status"`
	ItemUUID     string  `json:"item_uuid"`
	SourceItemID string  `json:"source_item_id"`
	Remarks      *string `json:"remarks,omitempty"`
}

type FederatedSearchItemSourceItem struct {
	PublicUUID   string `json:"public_uuid"`
	ProviderID   int64  `json:"provider_id"`
	SourceItemID string `json:"source_item_id"`
}

type FederatedSearchError struct {
	ProviderID   int64  `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	SourceKey    string `json:"source_key"`
	ErrorType    string `json:"error_type"`
	Message      string `json:"message"`
	LatencyMS    int64  `json:"latency_ms"`
}

type federatedProviderResult struct {
	provider repository.SourceProvider
	items    []repository.SourceItem
	err      error
	latency  int64
}

func (m *ProviderRuntimeManager) FederatedSearch(ctx context.Context, req FederatedSearchRequest) (*FederatedSearchResponse, error) {
	if m == nil || m.repo == nil {
		return nil, fmt.Errorf("provider runtime 缺少 repository")
	}
	keyword := strings.TrimSpace(req.Keyword)
	if keyword == "" {
		return nil, fmt.Errorf("搜索关键词不能为空")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultFederatedSearchLimit
	}
	if limit > maxFederatedSearchLimit {
		limit = maxFederatedSearchLimit
	}
	start := time.Now()

	providers, err := m.repo.ListProviders(ctx, repository.SourceProviderListOptions{
		Limit:      1000,
		OnlyUsable: true,
	})
	if err != nil {
		return nil, err
	}
	searchable := make([]repository.SourceProvider, 0, len(providers))
	for _, provider := range providers {
		if !isSearchableRuntimeProvider(provider) {
			continue
		}
		if !provider.Enabled || !provider.Searchable {
			continue
		}
		searchable = append(searchable, provider)
	}

	response := &FederatedSearchResponse{
		Keyword:    keyword,
		Provider:   FederatedSearchProvider{Total: len(searchable)},
		CacheWrite: true,
	}
	groups := map[string]*FederatedSearchItem{}
	results := make(chan federatedProviderResult, len(searchable))
	providerRoot := context.WithoutCancel(ctx)
	var wg sync.WaitGroup
	for _, provider := range searchable {
		provider := provider
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- m.searchProviderSafely(providerRoot, provider, keyword)
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	pending := make(map[int64]repository.SourceProvider, len(searchable))
	for _, provider := range searchable {
		pending[provider.ID] = provider
	}
	timer := time.NewTimer(federatedSearchBudget(searchable))
	defer timer.Stop()
	for len(pending) > 0 {
		select {
		case result, ok := <-results:
			if !ok {
				pending = map[int64]repository.SourceProvider{}
				break
			}
			delete(pending, result.provider.ID)
			applyFederatedProviderResult(response, groups, keyword, result)
		case <-timer.C:
			for {
				select {
				case result, ok := <-results:
					if !ok {
						pending = map[int64]repository.SourceProvider{}
						goto collected
					}
					delete(pending, result.provider.ID)
					applyFederatedProviderResult(response, groups, keyword, result)
				default:
					goto timedOut
				}
			}
		timedOut:
			for _, provider := range pending {
				applyFederatedProviderResult(response, groups, keyword, federatedProviderResult{
					provider: provider,
					err:      context.DeadlineExceeded,
					latency:  time.Since(start).Milliseconds(),
				})
			}
			pending = map[int64]repository.SourceProvider{}
		}
	}
collected:
	response.Items = flattenFederatedItems(groups)
	sort.SliceStable(response.Items, func(i, j int) bool {
		if response.Items[i].Score != response.Items[j].Score {
			return response.Items[i].Score > response.Items[j].Score
		}
		if response.Items[i].ProviderCount != response.Items[j].ProviderCount {
			return response.Items[i].ProviderCount > response.Items[j].ProviderCount
		}
		return strings.Compare(response.Items[i].Title, response.Items[j].Title) < 0
	})
	response.Total = len(response.Items)
	if len(response.Items) > limit {
		response.Items = response.Items[:limit]
		response.Truncated = true
	}
	response.LatencyMS = time.Since(start).Milliseconds()
	LogSourceAction(m.logger, start, slog.LevelInfo, "[Provider] federated_search",
		"action", "federated_search",
		"status", "ok",
		"keyword_len", len(keyword),
		"provider_total", response.Provider.Total,
		"provider_success", response.Provider.Success,
		"provider_failed", response.Provider.Failed,
		"hit_count", response.Total)
	return response, nil
}

func isSearchableRuntimeProvider(provider repository.SourceProvider) bool {
	switch {
	case provider.ProviderKind == "cms_vod" && provider.RuntimeKind == "native_cms":
		return true
	case provider.ProviderKind == "drpy_js" && provider.RuntimeKind == JSRuntimeKindNodeDRPY:
		return true
	case provider.ProviderKind == "tvbox_site" && provider.RuntimeKind == JSRuntimeKindNodeDRPY:
		return true
	case provider.ProviderKind == "tvbox_site" && provider.RuntimeKind == CSPRuntimeKindJVM:
		return true
	default:
		return false
	}
}

func (m *ProviderRuntimeManager) searchProviderSafely(ctx context.Context, provider repository.SourceProvider, keyword string) (result federatedProviderResult) {
	start := time.Now()
	result.provider = provider
	defer func() {
		result.latency = time.Since(start).Milliseconds()
		if recovered := recover(); recovered != nil {
			result.err = fmt.Errorf("provider panic: %v", recovered)
		}
	}()
	providerCtx, cancel := context.WithTimeout(ctx, federatedProviderTimeout(provider))
	defer cancel()
	_, items, err := m.Search(providerCtx, provider.ID, SearchRequest{Keyword: keyword, Page: 1})
	result.items = items
	result.err = err
	return result
}

func applyFederatedProviderResult(response *FederatedSearchResponse, groups map[string]*FederatedSearchItem, keyword string, result federatedProviderResult) {
	if result.err != nil {
		response.Provider.Failed++
		response.Errors = append(response.Errors, FederatedSearchError{
			ProviderID:   result.provider.ID,
			ProviderName: result.provider.Name,
			SourceKey:    result.provider.SourceKey,
			ErrorType:    ErrorType(result.err),
			Message:      result.err.Error(),
			LatencyMS:    result.latency,
		})
		return
	}
	response.Provider.Success++
	for _, item := range result.items {
		addFederatedItem(groups, keyword, result.provider, item)
	}
}

func federatedSearchBudget(providers []repository.SourceProvider) time.Duration {
	maxTimeout := defaultCMSTimeout
	for _, provider := range providers {
		if timeout := federatedProviderTimeout(provider); timeout > maxTimeout {
			maxTimeout = timeout
		}
	}
	return maxTimeout + defaultFederatedSearchGrace
}

func federatedProviderTimeout(provider repository.SourceProvider) time.Duration {
	if provider.TimeoutMS > 0 {
		return time.Duration(provider.TimeoutMS) * time.Millisecond
	}

	timeout := federatedProviderRuntimeDefaultTimeout(provider)
	// 聚合搜索需要与单站运行时默认超时保持同一语义，但不能让单个 25~30s 慢站拖住整次聚合。
	// 15s 足够覆盖“慢但活着”的 JS/CSP 站点，同时整体 budget 仍由 max provider timeout + grace 有界控制。
	if timeout > maxFederatedProviderDefaultTimeout {
		return maxFederatedProviderDefaultTimeout
	}
	return timeout
}

func federatedProviderRuntimeDefaultTimeout(provider repository.SourceProvider) time.Duration {
	switch {
	case provider.ProviderKind == "cms_vod" && provider.RuntimeKind == "native_cms":
		return defaultCMSTimeout
	case provider.RuntimeKind == JSRuntimeKindNodeDRPY:
		return jsRuntimeDefaultTimeout
	case provider.RuntimeKind == CSPRuntimeKindJVM:
		return cspRuntimeDefaultTimeout
	default:
		return defaultCMSTimeout
	}
}

func addFederatedItem(groups map[string]*FederatedSearchItem, keyword string, provider repository.SourceProvider, item repository.SourceItem) {
	key := federatedItemKey(item)
	group := groups[key]
	if group == nil {
		group = &FederatedSearchItem{
			PublicUUID:     item.PublicUUID,
			Title:          item.Title,
			Year:           item.Year,
			ItemType:       item.ItemType,
			NormalizedKind: item.NormalizedKind,
			Region:         item.Region,
			PosterURL:      item.PosterURL,
			Remarks:        item.Remarks,
			Score:          federatedItemScore(keyword, provider, item),
		}
		groups[key] = group
	}
	group.ProviderCount++
	group.Providers = append(group.Providers, FederatedSearchItemProvider{
		ID:           provider.ID,
		Name:         provider.Name,
		SourceKey:    provider.SourceKey,
		HealthStatus: provider.HealthStatus,
		ItemUUID:     item.PublicUUID,
		SourceItemID: item.SourceItemID,
		Remarks:      item.Remarks,
	})
	group.SourceItems = append(group.SourceItems, FederatedSearchItemSourceItem{
		PublicUUID:   item.PublicUUID,
		ProviderID:   provider.ID,
		SourceItemID: item.SourceItemID,
	})
	if score := federatedItemScore(keyword, provider, item); score > group.Score {
		group.Score = score
		group.PublicUUID = item.PublicUUID
		if group.PosterURL == nil {
			group.PosterURL = item.PosterURL
		}
	}
}

func flattenFederatedItems(groups map[string]*FederatedSearchItem) []FederatedSearchItem {
	out := make([]FederatedSearchItem, 0, len(groups))
	for _, item := range groups {
		out = append(out, *item)
	}
	return out
}

func federatedItemKey(item repository.SourceItem) string {
	return SourceItemSearchKey(item)
}

func federatedItemScore(keyword string, provider repository.SourceProvider, item repository.SourceItem) int {
	keyword = NormalizeSourceSearchTitle(keyword)
	title := NormalizeSourceSearchTitle(item.Title)
	score := 0
	switch {
	case keyword != "" && title == keyword:
		score += 100
	case keyword != "" && strings.HasPrefix(title, keyword):
		score += 80
	case keyword != "" && strings.Contains(title, keyword):
		score += 60
	default:
		score += 20
	}
	if provider.HealthStatus == "ok" {
		score += 10
	}
	if item.Year != nil && *item.Year > 0 {
		score += 3
	}
	if item.PosterURL != nil && strings.TrimSpace(*item.PosterURL) != "" {
		score += 2
	}
	return score
}
