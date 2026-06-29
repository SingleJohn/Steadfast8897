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

// source_emby_live_search_enabled 控制 Emby 客户端搜索时是否同步触发一次跨源聚合搜索，
// 把命中实时写入 source_items 后再返回。默认关闭；开启后会增加客户端搜索延迟和外站请求量。
const sourceEmbyLiveSearchEnabledKey = "source_emby_live_search_enabled"

const (
	// 同步直搜的整体超时上限：Emby 搜索请求不应被慢站长时间阻塞。
	sourceLiveSearchTimeout = 10 * time.Second
	// 同一关键词在该窗口内只触发一次 live 搜索，避免输入联想逐字符打爆所有源。
	sourceLiveSearchTTL = 45 * time.Second
)

var (
	sourceLiveSearchMu   sync.Mutex
	sourceLiveSearchSeen = map[string]time.Time{}
)

// warmSourceSearchCache 在 Emby 搜索读取缓存前，同步跑一次跨源聚合搜索把命中写入 source_items。
// 失败/超时只记录日志并降级为读现有缓存，不影响主搜索流程。调用方需已通过开关与权限校验。
func warmSourceSearchCache(c *gin.Context, state *AppState, searchTerm string, limit int64) {
	term := strings.TrimSpace(searchTerm)
	if term == "" || state == nil || state.Repo == nil {
		return
	}
	if state.Repo.SystemConfig == nil ||
		!state.Repo.SystemConfig.GetBoolOrDefault(c.Request.Context(), sourceEmbyLiveSearchEnabledKey, false) {
		return
	}
	if !claimLiveSearch(term) {
		return
	}

	lim := int(limit)
	if lim <= 0 || lim > 50 {
		lim = 50
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), sourceLiveSearchTimeout)
	defer cancel()
	manager := source.NewProviderRuntimeManager(state.Repo.Source, state.HTTPClient).
		WithJSRuntime(state.JSRuntime).
		WithCSPRuntime(state.CSPRuntime)
	if _, err := manager.FederatedSearch(ctx, source.FederatedSearchRequest{Keyword: term, Limit: lim}); err != nil {
		slog.Debug("[Source] emby live search warm failed",
			"log_target", "source",
			"action", "emby_live_search",
			"keyword_len", len(term),
			"error_type", source.ErrorType(err),
			"error", err)
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
