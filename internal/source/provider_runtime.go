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

func (m *ProviderRuntimeManager) Categories(ctx context.Context, providerID int64) ([]ProviderCategory, error) {
	start := time.Now()
	provider, row, err := m.enabledNativeCMSProvider(ctx, providerID)
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
	provider, row, err := m.enabledNativeCMSProvider(ctx, providerID)
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

func (m *ProviderRuntimeManager) HealthCheck(ctx context.Context, providerID int64) (*repository.SourceProvider, error) {
	start := time.Now()
	provider, row, err := m.nativeCMSProvider(ctx, providerID)
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

func (m *ProviderRuntimeManager) enabledNativeCMSProvider(ctx context.Context, providerID int64) (*CMSProvider, *repository.SourceProvider, error) {
	provider, row, err := m.nativeCMSProvider(ctx, providerID)
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

func (m *ProviderRuntimeManager) nativeCMSProvider(ctx context.Context, providerID int64) (*CMSProvider, *repository.SourceProvider, error) {
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
	if row.ProviderKind != "cms_vod" || row.RuntimeKind != "native_cms" {
		return nil, nil, fmt.Errorf("provider 需要后续 runtime: %s/%s", row.ProviderKind, row.RuntimeKind)
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
		return nil, nil, err
	}
	return provider, row, nil
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
