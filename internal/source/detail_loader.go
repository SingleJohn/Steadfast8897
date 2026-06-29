package source

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"fyms/internal/repository"
)

const ensureDetailTimeout = 12 * time.Second
const ensureFallbackDetailTimeout = 5 * time.Second

// sourceSeriesDetailTTL：连载剧 detail 追更 TTL。客户端打开剧集时若超过该时长，
// 自动重拉一次 detail 以补齐新增集数。电影类不受 TTL 影响（内容不会变）。
const sourceSeriesDetailTTL = 12 * time.Hour

var sourceDetailLoadGroup singleflight.Group

type EnsureDetailResult struct {
	Item        *repository.SourceItem
	PlaySources []repository.SourcePlaySource
	Loaded      bool
}

func EnsureItemDetailLoaded(ctx context.Context, repo *repository.SourceRepository, client *http.Client, jsRuntime *JSRuntimeManager, cspRuntime *CSPRuntimeManager, itemID int64) (*EnsureDetailResult, error) {
	return ensureItemDetailLoadedWithTimeout(ctx, repo, client, jsRuntime, cspRuntime, itemID, ensureDetailTimeout)
}

func EnsureItemDetailLoadedForFallback(ctx context.Context, repo *repository.SourceRepository, client *http.Client, jsRuntime *JSRuntimeManager, cspRuntime *CSPRuntimeManager, itemID int64) (*EnsureDetailResult, error) {
	return ensureItemDetailLoadedWithTimeout(ctx, repo, client, jsRuntime, cspRuntime, itemID, ensureFallbackDetailTimeout)
}

func ensureItemDetailLoadedWithTimeout(ctx context.Context, repo *repository.SourceRepository, client *http.Client, jsRuntime *JSRuntimeManager, cspRuntime *CSPRuntimeManager, itemID int64, timeout time.Duration) (*EnsureDetailResult, error) {
	start := time.Now()
	logger := SourceLogger("provider")
	result := &EnsureDetailResult{}
	if repo == nil {
		err := fmt.Errorf("source detail loader 缺少 repository")
		logEnsureDetail(logger, start, 0, itemID, false, err)
		return result, err
	}
	item, err := repo.GetSourceItemByID(ctx, itemID)
	if err != nil {
		logEnsureDetail(logger, start, 0, itemID, false, err)
		return result, err
	}
	if item == nil {
		err := fmt.Errorf("source item 不存在: %d", itemID)
		logEnsureDetail(logger, start, 0, itemID, false, err)
		return result, err
	}
	result.Item = item
	// 已加载且未过期(电影永不过期；连载剧看 TTL)→ 直接用缓存的 play sources。
	if item.DetailLoaded && !sourceItemDetailExpired(ctx, repo, item) {
		playSources, err := repo.ListPlaySourcesForItem(ctx, item.ID)
		result.PlaySources = playSources
		logEnsureDetail(logger, start, item.ProviderID, item.ID, false, err)
		return result, err
	}
	return loadAndIngestDetail(ctx, repo, client, jsRuntime, cspRuntime, item, start, logger, result, timeout)
}

// RefreshSourceItemDetail 强制重拉某条在线条目的 detail，无视 DetailLoaded/TTL。
// 供后台刷新队列与"手动刷新集数"按钮复用，成功后会更新 detail_refreshed_at。
func RefreshSourceItemDetail(ctx context.Context, repo *repository.SourceRepository, client *http.Client, jsRuntime *JSRuntimeManager, cspRuntime *CSPRuntimeManager, itemID int64) (*EnsureDetailResult, error) {
	start := time.Now()
	logger := SourceLogger("provider")
	result := &EnsureDetailResult{}
	if repo == nil {
		err := fmt.Errorf("source detail loader 缺少 repository")
		logEnsureDetail(logger, start, 0, itemID, false, err)
		return result, err
	}
	item, err := repo.GetSourceItemByID(ctx, itemID)
	if err != nil {
		logEnsureDetail(logger, start, 0, itemID, false, err)
		return result, err
	}
	if item == nil {
		err := fmt.Errorf("source item 不存在: %d", itemID)
		logEnsureDetail(logger, start, 0, itemID, false, err)
		return result, err
	}
	result.Item = item
	return loadAndIngestDetail(ctx, repo, client, jsRuntime, cspRuntime, item, start, logger, result, ensureDetailTimeout)
}

// sourceItemDetailExpired 判断连载剧 detail 是否过期需要重拉。非连载(电影)恒为 false。
func sourceItemDetailExpired(ctx context.Context, repo *repository.SourceRepository, item *repository.SourceItem) bool {
	if !strings.EqualFold(strings.TrimSpace(item.ItemType), "series") {
		return false
	}
	ts, err := repo.SourceItemDetailRefreshedAt(ctx, item.ID)
	if err != nil {
		return false
	}
	if ts == nil {
		return true
	}
	return time.Since(*ts) >= sourceSeriesDetailTTL
}

func loadAndIngestDetail(ctx context.Context, repo *repository.SourceRepository, client *http.Client, jsRuntime *JSRuntimeManager, cspRuntime *CSPRuntimeManager, item *repository.SourceItem, start time.Time, logger *slog.Logger, result *EnsureDetailResult, timeout time.Duration) (*EnsureDetailResult, error) {
	key := fmt.Sprintf("source-detail:%d", item.ID)
	value, err, _ := sourceDetailLoadGroup.Do(key, func() (any, error) {
		if timeout <= 0 {
			timeout = ensureDetailTimeout
		}
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()
		manager := NewProviderRuntimeManager(repo, client).WithJSRuntime(jsRuntime).WithCSPRuntime(cspRuntime)
		_, loadedItem, playSources, err := manager.Detail(loadCtx, item.ProviderID, item.SourceItemID)
		if err != nil {
			return &EnsureDetailResult{Item: item}, err
		}
		// 记录追更时间，TTL 判断据此计算(放 Do 内只在领头协程执行一次)。
		_ = repo.MarkSourceItemDetailRefreshed(loadCtx, item.ID)
		return &EnsureDetailResult{Item: loadedItem, PlaySources: playSources, Loaded: true}, nil
	})
	if err != nil {
		logEnsureDetail(logger, start, item.ProviderID, item.ID, false, err)
		return result, err
	}
	loaded, ok := value.(*EnsureDetailResult)
	if !ok || loaded == nil {
		err := fmt.Errorf("source detail loader 返回异常")
		logEnsureDetail(logger, start, item.ProviderID, item.ID, false, err)
		return result, err
	}
	if loaded.Item != nil {
		result.Item = loaded.Item
	}
	result.PlaySources = loaded.PlaySources
	result.Loaded = loaded.Loaded
	logEnsureDetail(logger, start, result.Item.ProviderID, result.Item.ID, result.Loaded, nil)
	return result, nil
}

func logEnsureDetail(logger *slog.Logger, start time.Time, providerID int64, itemID int64, loaded bool, err error) {
	status := "ok"
	level := slog.LevelInfo
	attrs := []any{
		"provider_id", providerID,
		"action", "ensure_detail",
		"status", status,
		"source_item_id", itemID,
		"loaded", loaded,
		"cache_hit", false,
	}
	if err != nil {
		status = "error"
		level = slog.LevelWarn
		attrs[5] = status
		attrs = append(attrs, "error_type", ErrorType(err), "error", sanitizeDetailError(err))
	}
	LogSourceAction(logger, start, level, "[Provider] ensure_detail", attrs...)
}

func sanitizeDetailError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "://") {
		return fmt.Errorf("%s", URLHash(msg))
	}
	return err
}
