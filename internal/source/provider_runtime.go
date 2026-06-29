package source

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"fyms/internal/repository"
)

// 限流器按 providerID 全局共享。Handler/worker 每次请求都会 new 一个 ProviderRuntimeManager，
// 若限流器挂在实例上则每次重置、形同虚设；改为包级全局后所有实例对同一 provider 共享同一限流器，
// 真正约束对单个源站的并发/频率（尤其是后台 catalog_fetch 批量抓取）。
var (
	providerLimiterMu sync.Mutex
	providerLimiters  = map[int64]*rate.Limiter{}
)

type ProviderRuntimeManager struct {
	repo   *repository.SourceRepository
	client *http.Client
	js     *JSRuntimeManager
	csp    *CSPRuntimeManager
	logger *slog.Logger
}

func NewProviderRuntimeManager(repo *repository.SourceRepository, client *http.Client) *ProviderRuntimeManager {
	if client == nil {
		client = http.DefaultClient
	}
	return &ProviderRuntimeManager{
		repo:   repo,
		client: client,
		logger: SourceLogger("provider"),
	}
}

func (m *ProviderRuntimeManager) WithJSRuntime(runtime *JSRuntimeManager) *ProviderRuntimeManager {
	m.js = runtime
	return m
}

func (m *ProviderRuntimeManager) WithCSPRuntime(runtime *CSPRuntimeManager) *ProviderRuntimeManager {
	m.csp = runtime
	return m
}

func IsProviderDisabledError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "provider 已禁用") || strings.Contains(msg, "provider 所属配置未启用")
}

func (m *ProviderRuntimeManager) Categories(ctx context.Context, providerID int64) ([]ProviderCategory, error) {
	start := time.Now()
	provider, row, err := m.enabledProvider(ctx, providerID)
	if err != nil {
		LogProviderAction(m.logger, start, providerID, "categories", err)
		return nil, err
	}
	if err := m.wait(row.ID).Wait(ctx); err != nil {
		err = fmt.Errorf("provider 限流等待失败: %w", err)
		LogProviderAction(m.logger, start, row.ID, "categories", err)
		return nil, err
	}
	items, err := provider.Categories(ctx)
	LogProviderAction(m.logger, start, row.ID, "categories", err, "count", len(items))
	return items, err
}

func (m *ProviderRuntimeManager) HomeProfile(ctx context.Context, providerID int64) (*ProviderHomeProfile, error) {
	start := time.Now()
	provider, row, err := m.enabledProvider(ctx, providerID)
	if err != nil {
		LogProviderAction(m.logger, start, providerID, "home_profile", err)
		return nil, err
	}
	profiler, ok := provider.(HomeProfiler)
	if !ok {
		err := fmt.Errorf("provider 不支持 HomeProfile")
		LogProviderAction(m.logger, start, row.ID, "home_profile", err)
		return nil, err
	}
	if err := m.wait(row.ID).Wait(ctx); err != nil {
		err = fmt.Errorf("provider 限流等待失败: %w", err)
		LogProviderAction(m.logger, start, row.ID, "home_profile", err)
		return nil, err
	}
	profile, err := profiler.HomeProfile(ctx)
	if profile != nil {
		profile.ProviderID = row.ID
		profile.RuntimeKind = row.RuntimeKind
	}
	LogProviderAction(m.logger, start, row.ID, "home_profile", err,
		"categories", providerHomeProfileCategoryCount(profile),
		"items", providerHomeProfileItemCount(profile))
	return profile, err
}

func (m *ProviderRuntimeManager) Search(ctx context.Context, providerID int64, req SearchRequest) (*ProviderPage, []repository.SourceItem, error) {
	start := time.Now()
	provider, row, err := m.enabledProvider(ctx, providerID)
	if err != nil {
		LogProviderAction(m.logger, start, providerID, "search", err)
		return nil, nil, err
	}
	if err := m.wait(row.ID).Wait(ctx); err != nil {
		err = fmt.Errorf("provider 限流等待失败: %w", err)
		LogProviderAction(m.logger, start, row.ID, "search", err)
		return nil, nil, err
	}
	page, err := provider.Search(ctx, req)
	if err != nil {
		LogProviderAction(m.logger, start, row.ID, "search", err, "keyword_len", len(strings.TrimSpace(req.Keyword)))
		return nil, nil, err
	}
	ingestor, err := NewSourceIngestor(m.repo, row.SourceKey, row.ID)
	if err != nil {
		LogProviderAction(m.logger, start, row.ID, "search", err)
		return nil, nil, err
	}
	items, err := ingestor.IngestPage(ctx, page)
	if err != nil {
		LogProviderAction(m.logger, start, row.ID, "search", err)
		return nil, nil, err
	}
	LogProviderAction(m.logger, start, row.ID, "search", nil,
		"keyword_len", len(strings.TrimSpace(req.Keyword)),
		"page", req.Page,
		"count", len(items),
		"cache_hit", false)
	return page, items, nil
}

// SearchPreview 执行一次站点搜索但不写入 source_items，用于聚合搜索测试(dry-run)。
// 结果在内存中映射为 SourceItem，便于复用聚合分组/打分逻辑而不污染媒体库与缓存。
func (m *ProviderRuntimeManager) SearchPreview(ctx context.Context, providerID int64, req SearchRequest) (*ProviderPage, []repository.SourceItem, error) {
	start := time.Now()
	provider, row, err := m.enabledProvider(ctx, providerID)
	if err != nil {
		LogProviderAction(m.logger, start, providerID, "search_preview", err)
		return nil, nil, err
	}
	if err := m.wait(row.ID).Wait(ctx); err != nil {
		err = fmt.Errorf("provider 限流等待失败: %w", err)
		LogProviderAction(m.logger, start, row.ID, "search_preview", err)
		return nil, nil, err
	}
	page, err := provider.Search(ctx, req)
	if err != nil {
		LogProviderAction(m.logger, start, row.ID, "search_preview", err, "keyword_len", len(strings.TrimSpace(req.Keyword)))
		return nil, nil, err
	}
	items := SnapshotsToSourceItems(row.SourceKey, row.ID, page)
	LogProviderAction(m.logger, start, row.ID, "search_preview", nil,
		"keyword_len", len(strings.TrimSpace(req.Keyword)),
		"page", req.Page,
		"count", len(items))
	return page, items, nil
}

// FetchCategory 拉取某分类某页内容并写入 source_items，用于批量填充在线虚拟库。
func (m *ProviderRuntimeManager) FetchCategory(ctx context.Context, providerID int64, req CategoryRequest) (*ProviderPage, []repository.SourceItem, error) {
	start := time.Now()
	provider, row, err := m.enabledProvider(ctx, providerID)
	if err != nil {
		LogProviderAction(m.logger, start, providerID, "category_fetch", err)
		return nil, nil, err
	}
	if err := m.wait(row.ID).Wait(ctx); err != nil {
		err = fmt.Errorf("provider 限流等待失败: %w", err)
		LogProviderAction(m.logger, start, row.ID, "category_fetch", err)
		return nil, nil, err
	}
	page, err := provider.Category(ctx, req)
	if err != nil {
		LogProviderAction(m.logger, start, row.ID, "category_fetch", err, "category_id", req.CategoryID, "page", req.Page)
		return nil, nil, err
	}
	ingestor, err := NewSourceIngestor(m.repo, row.SourceKey, row.ID)
	if err != nil {
		LogProviderAction(m.logger, start, row.ID, "category_fetch", err)
		return nil, nil, err
	}
	items, err := ingestor.IngestPage(ctx, page)
	if err != nil {
		LogProviderAction(m.logger, start, row.ID, "category_fetch", err)
		return nil, nil, err
	}
	LogProviderAction(m.logger, start, row.ID, "category_fetch", nil,
		"category_id", req.CategoryID, "page", req.Page, "count", len(items))
	return page, items, nil
}

func (m *ProviderRuntimeManager) Detail(ctx context.Context, providerID int64, sourceItemID string) (*ProviderDetail, *repository.SourceItem, []repository.SourcePlaySource, error) {
	start := time.Now()
	provider, row, err := m.enabledProvider(ctx, providerID)
	if err != nil {
		LogProviderAction(m.logger, start, providerID, "detail", err)
		return nil, nil, nil, err
	}
	if err := m.wait(row.ID).Wait(ctx); err != nil {
		err = fmt.Errorf("provider 限流等待失败: %w", err)
		LogProviderAction(m.logger, start, row.ID, "detail", err)
		return nil, nil, nil, err
	}
	detail, err := provider.Detail(ctx, sourceItemID)
	if err != nil {
		LogProviderAction(m.logger, start, row.ID, "detail", err, "source_item_hash", URLHash(sourceItemID))
		return nil, nil, nil, err
	}
	ingestor, err := NewSourceIngestor(m.repo, row.SourceKey, row.ID)
	if err != nil {
		LogProviderAction(m.logger, start, row.ID, "detail", err)
		return nil, nil, nil, err
	}
	item, playSources, err := ingestor.IngestDetail(ctx, detail)
	if err != nil {
		LogProviderAction(m.logger, start, row.ID, "detail", err)
		return nil, nil, nil, err
	}
	LogProviderAction(m.logger, start, row.ID, "detail", nil, "play_source_count", len(playSources), "cache_hit", false)
	return detail, item, playSources, nil
}

func (m *ProviderRuntimeManager) ResolvePlay(ctx context.Context, playSource repository.SourcePlaySource) (*PlayResult, error) {
	start := time.Now()
	provider, row, err := m.enabledProvider(ctx, playSource.ProviderID)
	if err != nil {
		LogProviderAction(m.logger, start, playSource.ProviderID, "resolve_play", err)
		return nil, err
	}
	if err := m.wait(row.ID).Wait(ctx); err != nil {
		err = fmt.Errorf("provider 限流等待失败: %w", err)
		LogProviderAction(m.logger, start, row.ID, "resolve_play", err)
		return nil, err
	}
	snapshot, err := playSourceSnapshotFromRepository(playSource)
	if err != nil {
		LogProviderAction(m.logger, start, row.ID, "resolve_play", err)
		return nil, err
	}
	result, err := provider.ResolvePlay(ctx, snapshot)
	LogProviderAction(m.logger, start, row.ID, "resolve_play", err, "parse_mode", playSource.ParseMode, "url_hash", URLHash(playSource.RawURL))
	return result, err
}

func providerHomeProfileCategoryCount(profile *ProviderHomeProfile) int {
	if profile == nil {
		return 0
	}
	return len(profile.Categories)
}

func providerHomeProfileItemCount(profile *ProviderHomeProfile) int {
	if profile == nil {
		return 0
	}
	return len(profile.HomeItems)
}

func (m *ProviderRuntimeManager) HealthCheck(ctx context.Context, providerID int64) (*repository.SourceProvider, error) {
	start := time.Now()
	provider, row, err := m.provider(ctx, providerID)
	if err != nil {
		LogProviderAction(m.logger, start, providerID, "health", err)
		return nil, err
	}
	if err := m.wait(row.ID).Wait(ctx); err != nil {
		err = fmt.Errorf("provider 限流等待失败: %w", err)
		LogProviderAction(m.logger, start, row.ID, "health", err)
		return nil, err
	}
	summary, categories := m.providerHealthSummary(ctx, provider, row)
	categoryRaw := jsonBytes(categories, "[]")
	summaryRaw := jsonBytes(summary, "{}")
	var lastError *string
	if summary.Message != "" {
		lastError = &summary.Message
	}
	updated, err := m.repo.UpdateProviderHealthSummary(ctx, row.ID, summary.OverallStatus, lastError, categoryRaw, summaryRaw)
	if err != nil {
		LogProviderAction(m.logger, start, row.ID, "health", err)
		return updated, err
	}
	LogProviderAction(m.logger, start, row.ID, "health", nil,
		"status", summary.OverallStatus,
		"runtime_status", summary.RuntimeStatus,
		"home_status", summary.HomeStatus,
		"category_status", summary.CategoryStatus,
		"search_status", summary.SearchStatus,
		"categories", len(categories))
	return updated, nil
}

func (m *ProviderRuntimeManager) providerHealthSummary(ctx context.Context, provider Provider, row *repository.SourceProvider) (ProviderHealthSummary, []ProviderCategory) {
	summary := ProviderHealthSummary{
		RuntimeStatus:   ProviderHealthStatusOK,
		HomeStatus:      ProviderHealthStatusUnknown,
		CategoryStatus:  ProviderHealthStatusUnknown,
		SearchStatus:    ProviderHealthStatusSkipped,
		PlayReadyStatus: ProviderHealthStatusSkipped,
		CheckedAt:       time.Now(),
	}
	categories := []ProviderCategory{}
	homeStart := time.Now()
	if profiler, ok := provider.(HomeProfiler); ok {
		profile, err := profiler.HomeProfile(ctx)
		summary.Home.LatencyMS = time.Since(homeStart).Milliseconds()
		if err != nil && profile == nil {
			if providerHealthRuntimeFailure(err) {
				summary.RuntimeStatus = ProviderHealthStatusError
			}
			summary.HomeStatus = ProviderHealthStatusError
			summary.CategoryStatus = ProviderHealthStatusError
			summary.Home = providerHealthMethodError(err, summary.Home.LatencyMS)
		} else {
			if profile != nil {
				categories = profile.Categories
				summary.Home.CategoriesCount = len(profile.Categories)
				summary.Home.FiltersCount = profile.FiltersCount
				summary.Home.ItemsCount = len(profile.HomeItems)
				summary.Home.Status = providerHealthHomeStatus(profile)
				summary.HomeStatus = summary.Home.Status
				summary.Category.CategoriesCount = len(profile.Categories)
				summary.Category.Status = providerHealthCategoryStatus(profile)
				summary.CategoryStatus = summary.Category.Status
			}
			if err != nil {
				if providerHealthRuntimeFailure(err) {
					summary.RuntimeStatus = ProviderHealthStatusPartial
				}
				summary.Home.Message = err.Error()
				summary.Home.ErrorType = ErrorType(err)
			}
		}
	} else {
		categoriesStart := time.Now()
		nextCategories, err := provider.Categories(ctx)
		summary.Home.LatencyMS = time.Since(categoriesStart).Milliseconds()
		if err != nil {
			if providerHealthRuntimeFailure(err) {
				summary.RuntimeStatus = ProviderHealthStatusError
			}
			summary.HomeStatus = ProviderHealthStatusError
			summary.CategoryStatus = ProviderHealthStatusError
			summary.Home = providerHealthMethodError(err, summary.Home.LatencyMS)
		} else {
			categories = nextCategories
			summary.Home.CategoriesCount = len(categories)
			summary.Home.Status = providerHealthCountStatus(len(categories))
			summary.HomeStatus = summary.Home.Status
			summary.Category.CategoriesCount = len(categories)
			summary.Category.Status = providerHealthCountStatus(len(categories))
			summary.CategoryStatus = summary.Category.Status
		}
	}
	if row != nil && row.Searchable && summary.RuntimeStatus != ProviderHealthStatusError {
		summary.Search = providerHealthSearch(ctx, provider)
		summary.SearchStatus = summary.Search.Status
	}
	summary.OverallStatus = providerHealthOverall(summary)
	summary.Message = providerHealthMessage(summary)
	return summary, categories
}

func providerHealthHomeStatus(profile *ProviderHomeProfile) string {
	if profile == nil {
		return ProviderHealthStatusUnknown
	}
	homeOK := profile.Sources.HomeContent.OK
	videoOK := profile.Sources.HomeVideoContent.OK
	if homeOK || videoOK {
		if homeOK && videoOK {
			return ProviderHealthStatusOK
		}
		return ProviderHealthStatusPartial
	}
	if len(profile.Categories) > 0 || profile.FiltersCount > 0 {
		return ProviderHealthStatusPartial
	}
	if profile.Sources.HomeContent.Status == ProviderHomeSourceStatusError && profile.Sources.HomeVideoContent.Status == ProviderHomeSourceStatusError {
		return ProviderHealthStatusError
	}
	return ProviderHealthStatusUnknown
}

func providerHealthCategoryStatus(profile *ProviderHomeProfile) string {
	if profile == nil {
		return ProviderHealthStatusUnknown
	}
	if len(profile.Categories) > 0 {
		return ProviderHealthStatusOK
	}
	if profile.Sources.HomeContent.Status == ProviderHomeSourceStatusError {
		return ProviderHealthStatusError
	}
	return ProviderHealthStatusUnknown
}

func providerHealthCountStatus(count int) string {
	if count > 0 {
		return ProviderHealthStatusOK
	}
	return ProviderHealthStatusUnknown
}

func providerHealthRuntimeFailure(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "runtime 未初始化") ||
		strings.Contains(message, "runtime manager 未初始化") ||
		strings.Contains(message, "sidecar") ||
		strings.Contains(message, "worker") ||
		strings.Contains(message, "需 runtime") ||
		strings.Contains(message, "需要后续 runtime")
}

func providerHealthSearch(ctx context.Context, provider Provider) ProviderHealthMethodSummary {
	start := time.Now()
	page, err := provider.Search(ctx, SearchRequest{Keyword: "test", Page: 1})
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return providerHealthMethodError(err, latency)
	}
	itemsCount := 0
	if page != nil {
		itemsCount = len(page.Items)
	}
	status := ProviderHealthStatusOK
	if itemsCount == 0 {
		status = ProviderHealthStatusUnknown
	}
	return ProviderHealthMethodSummary{Status: status, ItemsCount: itemsCount, LatencyMS: latency}
}

func providerHealthMethodError(err error, latencyMS int64) ProviderHealthMethodSummary {
	status := ProviderHealthStatusError
	if ErrorType(err) == "site_unavailable" {
		status = ProviderHealthStatusUnusable
	}
	return ProviderHealthMethodSummary{
		Status:    status,
		ErrorType: ErrorType(err),
		Message:   err.Error(),
		LatencyMS: latencyMS,
	}
}

func playSourceSnapshotFromRepository(playSource repository.SourcePlaySource) (PlaySourceSnapshot, error) {
	headers := map[string]any{}
	if len(playSource.Headers) > 0 {
		if err := json.Unmarshal(playSource.Headers, &headers); err != nil {
			return PlaySourceSnapshot{}, fmt.Errorf("解析播放 headers 失败: %w", err)
		}
	}
	resolverPayload := map[string]any{}
	if len(playSource.ResolverPayload) > 0 {
		_ = json.Unmarshal(playSource.ResolverPayload, &resolverPayload)
	}
	return PlaySourceSnapshot{
		LineName:        playSource.LineName,
		EpisodeTitle:    playSource.EpisodeTitle,
		EpisodeKey:      playSource.EpisodeKey,
		EpisodeNumber:   playSource.EpisodeNumber,
		RawURL:          playSource.RawURL,
		ParseMode:       playSource.ParseMode,
		Flag:            playSource.Flag,
		Headers:         headers,
		ResolverPayload: resolverPayload,
		SortOrder:       playSource.SortOrder,
	}, nil
}

func (m *ProviderRuntimeManager) enabledProvider(ctx context.Context, providerID int64) (Provider, *repository.SourceProvider, error) {
	provider, row, err := m.provider(ctx, providerID)
	if err != nil {
		return nil, nil, err
	}
	if !row.Enabled {
		return nil, nil, fmt.Errorf("provider 已禁用")
	}
	if row.ConfigID != nil {
		config, err := m.repo.GetConfigImportByID(ctx, *row.ConfigID)
		if err != nil {
			return nil, nil, err
		}
		if config != nil && (!config.Enabled || config.ImportStatus != "active") {
			return nil, nil, fmt.Errorf("provider 所属配置未启用")
		}
	}
	return provider, row, nil
}

func (m *ProviderRuntimeManager) provider(ctx context.Context, providerID int64) (Provider, *repository.SourceProvider, error) {
	if m == nil || m.repo == nil {
		return nil, nil, fmt.Errorf("provider runtime 缺少 repository")
	}
	row, err := m.repo.GetProviderByID(ctx, providerID)
	if err != nil {
		return nil, nil, err
	}
	if row == nil {
		return nil, nil, fmt.Errorf("provider 不存在: %d", providerID)
	}
	if row.ProviderKind == "cms_vod" && row.RuntimeKind == "native_cms" {
		provider, err := m.nativeCMSProvider(row)
		return provider, row, err
	}
	if row.ProviderKind == "drpy_js" && row.RuntimeKind == JSRuntimeKindNodeDRPY {
		provider, err := m.jsProvider(ctx, row)
		return provider, row, err
	}
	if row.ProviderKind == "tvbox_site" && row.RuntimeKind == JSRuntimeKindNodeDRPY {
		provider, err := m.jsProvider(ctx, row)
		return provider, row, err
	}
	if row.ProviderKind == "tvbox_site" && row.RuntimeKind == CSPRuntimeKindJVM {
		provider, err := m.cspProvider(ctx, row)
		return provider, row, err
	}
	return nil, nil, fmt.Errorf("provider 需要后续 runtime: %s/%s", row.ProviderKind, row.RuntimeKind)
}

func (m *ProviderRuntimeManager) nativeCMSProvider(row *repository.SourceProvider) (*CMSProvider, error) {
	if row == nil {
		return nil, fmt.Errorf("provider 不存在")
	}
	if row.ProviderKind != "cms_vod" || row.RuntimeKind != "native_cms" {
		return nil, fmt.Errorf("provider 需要后续 runtime: %s/%s", row.ProviderKind, row.RuntimeKind)
	}
	headers := map[string]string{}
	searchAC := ""
	detailAC := ""
	categoryAC := ""
	headers = headerMapFromJSON(row.Headers)
	if len(row.Ext) > 0 {
		var raw map[string]any
		if err := json.Unmarshal(row.Ext, &raw); err == nil {
			searchAC = stringExt(raw, "search_ac")
			detailAC = stringExt(raw, "detail_ac")
			categoryAC = stringExt(raw, "category_ac")
		}
	}
	provider, err := NewCMSProvider(
		row.SourceKey,
		row.API,
		WithCMSHTTPClient(m.client),
		WithCMSTimeout(time.Duration(row.TimeoutMS)*time.Millisecond),
		WithCMSHeaders(headers),
		WithCMSActions(searchAC, detailAC, categoryAC),
		WithCMSCategoryWhitelist(cmsCategoryWhitelistFromProvider(row)),
	)
	if err != nil {
		return nil, err
	}
	return provider, nil
}

func cmsCategoryWhitelistFromProvider(row *repository.SourceProvider) []string {
	if row == nil {
		return nil
	}
	categories := stringListFromJSON(row.Categories)
	if len(categories) > 0 {
		return categories
	}
	if len(row.Capabilities) == 0 || string(row.Capabilities) == "null" {
		return nil
	}
	var capabilities map[string]any
	if err := json.Unmarshal(row.Capabilities, &capabilities); err != nil {
		return nil
	}
	source, _ := capabilities["category_source"].(string)
	if !strings.EqualFold(strings.TrimSpace(source), "tvbox_site_whitelist") {
		return nil
	}
	if values, ok := capabilities["categories"].([]any); ok {
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	}
	return nil
}

func stringListFromJSON(raw []byte) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func stringExt(raw map[string]any, key string) string {
	if value, ok := raw[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func (m *ProviderRuntimeManager) jsProvider(ctx context.Context, row *repository.SourceProvider) (*JSProvider, error) {
	if row == nil {
		return nil, fmt.Errorf("provider 不存在")
	}
	if m.js == nil {
		return nil, fmt.Errorf("JS runtime 未初始化")
	}
	engine := strings.TrimSpace(row.API)
	rule, baseURL := m.jsRuleAndBaseURL(ctx, row)
	return NewJSProvider(
		row.ID,
		row.SourceKey,
		row.Name,
		engine,
		rule,
		baseURL,
		headerMapFromJSON(row.Headers),
		m.js,
		time.Duration(row.TimeoutMS)*time.Millisecond,
	)
}

func (m *ProviderRuntimeManager) jsRuleAndBaseURL(ctx context.Context, row *repository.SourceProvider) (string, string) {
	baseURL := ""
	if row == nil {
		return "", baseURL
	}
	if row.ConfigID != nil {
		if config, err := m.repo.GetConfigImportByID(ctx, *row.ConfigID); err == nil && config != nil && config.BaseURL != nil {
			baseURL = strings.TrimSpace(*config.BaseURL)
		}
	}
	rule := ""
	if len(row.Ext) > 0 {
		var ext map[string]any
		if json.Unmarshal(row.Ext, &ext) == nil {
			if value, ok := ext["_path"].(string); ok {
				rule = strings.TrimSpace(value)
			} else if value, ok := ext["_raw"].(string); ok {
				rule = strings.TrimSpace(value)
			}
		}
	}
	if rule == "" && strings.HasSuffix(strings.ToLower(strings.TrimSpace(row.API)), ".js") {
		rule = strings.TrimSpace(row.API)
	}
	return rule, baseURL
}

func (m *ProviderRuntimeManager) cspProvider(ctx context.Context, row *repository.SourceProvider) (*CSPProvider, error) {
	if row == nil {
		return nil, fmt.Errorf("provider 不存在")
	}
	if m.csp == nil {
		return nil, fmt.Errorf("CSP runtime 未初始化")
	}
	spider, baseURL := m.cspSpiderAndBaseURL(ctx, row)
	return NewCSPProvider(
		row.ID,
		row.SourceKey,
		row.Name,
		row.API,
		spider,
		baseURL,
		row.Ext,
		headerMapFromJSON(row.Headers),
		m.csp,
		time.Duration(row.TimeoutMS)*time.Millisecond,
	)
}

func headerMapFromJSON(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return map[string]string{}
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return map[string]string{}
	}
	headers := map[string]string{}
	for key, value := range values {
		switch v := value.(type) {
		case string:
			headers[key] = v
		case float64, bool, json.Number:
			headers[key] = fmt.Sprint(v)
		}
	}
	return compactHeaderMap(headers)
}

func compactHeaderMap(headers map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range headers {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	return out
}

func (m *ProviderRuntimeManager) cspSpiderAndBaseURL(ctx context.Context, row *repository.SourceProvider) (string, string) {
	baseURL := ""
	spider := ""
	if row == nil {
		return spider, baseURL
	}
	if row.ConfigID != nil {
		if config, err := m.repo.GetConfigImportByID(ctx, *row.ConfigID); err == nil && config != nil {
			if config.BaseURL != nil {
				baseURL = strings.TrimSpace(*config.BaseURL)
			}
			if config.SpiderRef != nil {
				spider = strings.TrimSpace(*config.SpiderRef)
			}
		}
	}
	if spider == "" && len(row.RawSite) > 0 {
		var raw map[string]any
		if json.Unmarshal(row.RawSite, &raw) == nil {
			if value, ok := raw["spider"].(string); ok {
				spider = strings.TrimSpace(value)
			}
		}
	}
	return spider, baseURL
}

func (m *ProviderRuntimeManager) wait(providerID int64) *rate.Limiter {
	providerLimiterMu.Lock()
	defer providerLimiterMu.Unlock()
	limiter := providerLimiters[providerID]
	if limiter == nil {
		limiter = rate.NewLimiter(rate.Every(500*time.Millisecond), 2)
		providerLimiters[providerID] = limiter
	}
	return limiter
}
