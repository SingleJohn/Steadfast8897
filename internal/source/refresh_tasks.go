package source

import (
	"context"
	"net/http"

	"fyms/internal/repository"
)

// sourceRefreshExecutor 执行单条刷新任务，按需构建 ProviderRuntimeManager。
type sourceRefreshExecutor struct {
	repo   *repository.SourceRepository
	client *http.Client
	js     *JSRuntimeManager
	csp    *CSPRuntimeManager
}

func (e *sourceRefreshExecutor) manager() *ProviderRuntimeManager {
	return NewProviderRuntimeManager(e.repo, e.client).WithJSRuntime(e.js).WithCSPRuntime(e.csp)
}

// runCatalogFetch 遍历某 provider 的分类，把前若干页内容批量入库以填充虚拟库。
// 单分类/单页失败不中断整批；只要抓到过内容就算成功，全程失败才返回错误触发重试。
func (e *sourceRefreshExecutor) runCatalogFetch(ctx context.Context, providerID int64) error {
	manager := e.manager()
	categories, err := manager.Categories(ctx, providerID)
	if err != nil {
		if IsProviderDisabledError(err) {
			return nil // provider 已禁用/配置停用 —— 无内容可抓，视为完成
		}
		return err
	}
	if len(categories) == 0 {
		return nil
	}
	if len(categories) > sourceCatalogFetchMaxCategories {
		categories = categories[:sourceCatalogFetchMaxCategories]
	}
	ingested := 0
	var firstErr error
	for _, cat := range categories {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		for page := 1; page <= sourceCatalogFetchPagesPerCategory; page++ {
			pageData, items, err := manager.FetchCategory(ctx, providerID, CategoryRequest{CategoryID: cat.ID, Page: page})
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				break // 该分类抓取失败，换下一个分类
			}
			ingested += len(items)
			// 已到末页 / 本页无内容 → 该分类抓取结束
			if pageData == nil || len(items) == 0 || (pageData.PageCount > 0 && page >= pageData.PageCount) {
				break
			}
		}
	}
	if ingested == 0 && firstErr != nil {
		return firstErr
	}
	return nil
}

// runDetailRefresh 强制重拉某条在线剧的 detail，追更集数。
func (e *sourceRefreshExecutor) runDetailRefresh(ctx context.Context, itemID int64) error {
	_, err := RefreshSourceItemDetail(ctx, e.repo, e.client, e.js, e.csp, itemID)
	if err != nil && IsProviderDisabledError(err) {
		return nil
	}
	return err
}
