package compat

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"fyms/internal/source"
)

// source_emby_live_search_enabled 控制 Emby 客户端搜索时是否触发跨源聚合搜索，
// 把命中实时写入 source_items 后再返回。默认开启；关闭后客户端只能搜到已缓存的在线条目。
const sourceEmbyLiveSearchEnabledKey = "source_emby_live_search_enabled"

const (
	// 同步预算：客户端搜索最多阻塞这么久，预算内已写入缓存的命中随本次返回。
	sourceLiveSearchSyncBudget = 6 * time.Second
	// 后台预算：超出同步预算后，慢站仍在后台继续聚合并回填缓存，供下次搜索秒回。
	sourceLiveSearchBackgroundTimeout = 30 * time.Second
	// 同一关键词在该窗口内只触发一次 live 搜索，避免输入联想逐字符打爆所有源。
	sourceLiveSearchTTL = 45 * time.Second
)

var (
	sourceLiveSearchMu   sync.Mutex
	sourceLiveSearchSeen = map[string]time.Time{}
)

// warmSourceSearchCache 在 Emby 搜索读取缓存前触发跨源聚合搜索把命中写入 source_items。
// 采用「同步预算 + 后台回填」：在 sourceLiveSearchSyncBudget 内返回(已写入缓存的命中随本次返回)，
// 慢站则脱离请求上下文在后台继续聚合并回填缓存，供下次搜索秒回。失败只记录日志，不影响主搜索流程。
// 调用方需已通过开关与权限校验。
func warmSourceSearchCache(c *gin.Context, state *AppState, searchTerm string, limit int64) {
	term := strings.TrimSpace(searchTerm)
	if term == "" || state == nil || state.Repo == nil {
		return
	}
	if state.Repo.SystemConfig == nil ||
		!state.Repo.SystemConfig.GetBoolOrDefault(c.Request.Context(), sourceEmbyLiveSearchEnabledKey, true) {
		return
	}
	if !claimLiveSearch(term) {
		return
	}

	lim := int(limit)
	if lim <= 0 || lim > 50 {
		lim = 50
	}
	manager := source.NewProviderRuntimeManager(state.Repo.Source, state.HTTPClient).
		WithJSRuntime(state.JSRuntime).
		WithCSPRuntime(state.CSPRuntime)

	// 后台上下文脱离请求生命周期，响应返回后聚合仍可继续把命中回填进缓存。
	bgCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), sourceLiveSearchBackgroundTimeout)
	done := make(chan struct{})
	go func() {
		defer cancel()
		defer close(done)
		if _, err := manager.FederatedSearch(bgCtx, source.FederatedSearchRequest{Keyword: term, Limit: lim}); err != nil {
			slog.Debug("[Source] emby live search warm failed",
				"log_target", "source",
				"action", "emby_live_search",
				"keyword_len", len(term),
				"error_type", source.ErrorType(err),
				"error", err)
		}
	}()

	// 在同步预算内等待:预算到点即返回(已写入缓存的命中随本次读取返回),剩余由后台继续回填。
	select {
	case <-done:
	case <-time.After(sourceLiveSearchSyncBudget):
	case <-c.Request.Context().Done():
	}
}

// claimLiveSearch 在 TTL 窗口内对同一关键词去重，返回 true 表示本次应执行 live 搜索。
func claimLiveSearch(term string) bool {
	key := strings.ToLower(term)
	now := time.Now()
	sourceLiveSearchMu.Lock()
	defer sourceLiveSearchMu.Unlock()
	if last, ok := sourceLiveSearchSeen[key]; ok && now.Sub(last) < sourceLiveSearchTTL {
		return false
	}
	sourceLiveSearchSeen[key] = now
	if len(sourceLiveSearchSeen) > 512 {
		for k, t := range sourceLiveSearchSeen {
			if now.Sub(t) > sourceLiveSearchTTL {
				delete(sourceLiveSearchSeen, k)
			}
		}
	}
	return true
}
