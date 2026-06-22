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

type ProviderRuntimeManager struct {
	repo     *repository.SourceRepository
	client   *http.Client
	js       *JSRuntimeManager
	mu       sync.Mutex
	limiters map[int64]*rate.Limiter
	logger   *slog.Logger
}

func NewProviderRuntimeManager(repo *repository.SourceRepository, client *http.Client) *ProviderRuntimeManager {
	if client == nil {
		client = http.DefaultClient
	}
	return &ProviderRuntimeManager{
		repo:     repo,
		client:   client,
		limiters: map[int64]*rate.Limiter{},
		logger:   SourceLogger("provider"),
	}
}

func (m *ProviderRuntimeManager) WithJSRuntime(runtime *JSRuntimeManager) *ProviderRuntimeManager {
	m.js = runtime
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
	categories, err := provider.Categories(ctx)
	if err != nil {
		msg := err.Error()
		status := "error"
		if ErrorType(err) == "site_unavailable" {
			status = "unhealthy"
		}
		updated, updateErr := m.repo.UpdateProviderHealth(ctx, row.ID, status, &msg, nil)
		if updateErr != nil {
			LogProviderAction(m.logger, start, row.ID, "health", updateErr)
			return updated, updateErr
		}
		LogProviderAction(m.logger, start, row.ID, "health", err)
		return updated, nil
	}
	raw := jsonBytes(categories, "[]")
	updated, err := m.repo.UpdateProviderHealth(ctx, row.ID, "ok", nil, raw)
	LogProviderAction(m.logger, start, row.ID, "health", err, "count", len(categories))
	return updated, err
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
	if len(row.Headers) > 0 {
		var raw map[string]any
		if err := json.Unmarshal(row.Headers, &raw); err == nil {
			for key, value := range raw {
				if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
					headers[key] = s
				}
			}
		}
	}
	provider, err := NewCMSProvider(
		row.SourceKey,
		row.API,
		WithCMSHTTPClient(m.client),
		WithCMSTimeout(time.Duration(row.TimeoutMS)*time.Millisecond),
		WithCMSHeaders(headers),
	)
	if err != nil {
		return nil, err
	}
	return provider, nil
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

func (m *ProviderRuntimeManager) wait(providerID int64) *rate.Limiter {
	m.mu.Lock()
	defer m.mu.Unlock()
	limiter := m.limiters[providerID]
	if limiter == nil {
		limiter = rate.NewLimiter(rate.Every(500*time.Millisecond), 2)
		m.limiters[providerID] = limiter
	}
	return limiter
}
