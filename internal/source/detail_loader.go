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

var sourceDetailLoadGroup singleflight.Group

type EnsureDetailResult struct {
	Item        *repository.SourceItem
	PlaySources []repository.SourcePlaySource
	Loaded      bool
}

func EnsureItemDetailLoaded(ctx context.Context, repo *repository.SourceRepository, client *http.Client, jsRuntime *JSRuntimeManager, itemID int64) (*EnsureDetailResult, error) {
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
	if item.DetailLoaded {
		playSources, err := repo.ListPlaySourcesForItem(ctx, item.ID)
		result.PlaySources = playSources
		logEnsureDetail(logger, start, item.ProviderID, item.ID, false, err)
		return result, err
	}
	key := fmt.Sprintf("source-detail:%d", item.ID)
	value, err, _ := sourceDetailLoadGroup.Do(key, func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ensureDetailTimeout)
		defer cancel()
		manager := NewProviderRuntimeManager(repo, client).WithJSRuntime(jsRuntime)
		_, loadedItem, playSources, err := manager.Detail(loadCtx, item.ProviderID, item.SourceItemID)
		if err != nil {
			return &EnsureDetailResult{Item: item}, err
		}
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
