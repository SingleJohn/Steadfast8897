package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"fyms/internal/models"
	"fyms/internal/services"
)

func scrapeItem(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	ctx := c.Request.Context()
	_, err := services.ScrapeItem(ctx, state.DB, itemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func searchTmdbForItem(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	var body struct {
		Query  string `json:"query"`
		Year   *int32 `json:"year,omitempty"`
		TmdbID *int64 `json:"tmdbId,omitempty"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请求参数无效"})
		return
	}
	if body.TmdbID == nil && strings.TrimSpace(body.Query) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请提供搜索关键词或 TMDB ID"})
		return
	}
	if body.TmdbID != nil && *body.TmdbID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请提供有效的 TMDB ID"})
		return
	}
	results, err := services.SearchTMDBForItem(c.Request.Context(), state.DB, itemID, strings.TrimSpace(body.Query), body.Year, body.TmdbID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}

func scrapeItemByTmdbId(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	var body struct {
		TmdbId int64 `json:"tmdbId"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.TmdbId <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请提供有效的 TMDB ID"})
		return
	}
	_, err := services.ScrapeItemByTMDBID(c.Request.Context(), state.DB, itemID, body.TmdbId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func getIdentifyCandidates(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	items, err := services.ListIdentifyCandidates(c.Request.Context(), state.DB, itemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func applyIdentifyCandidate(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	candidateID := c.Param("candidateId")
	// 候选采纳可能走 provider.GetByID(豆瓣 HTML 解析)+ TMDB Fill,总时长给 30s 兜底
	// 避免 TMDB/豆瓣慢响应让 HTTP 连接 hang 到前端超时
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	provider, externalID, err := services.ResolveIdentifyCandidate(ctx, state.DB, itemID, candidateID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	result, err := services.ScrapeItemByProviderID(ctx, state.DB, itemID, provider, externalID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_, _ = state.DB.Exec(ctx, "DELETE FROM identify_candidates WHERE item_id = $1::uuid", itemID)
	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"provider": provider,
		"tmdb_id":  result["tmdb_id"],
	})
}

func listUnmatchedItems(c *gin.Context) {
	state := GetState(c)
	limit := 200
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	itemType := strings.TrimSpace(c.Query("type"))
	items, err := services.ListUnmatchedItems(c.Request.Context(), state.DB, itemType, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "count": len(items)})
}

func batchApplyIdentifyCandidates(c *gin.Context) {
	state := GetState(c)
	var body struct {
		Items []struct {
			ItemID      string `json:"item_id"`
			CandidateID string `json:"candidate_id"`
		} `json:"items"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	if len(body.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "items is required"})
		return
	}
	type applyResult struct {
		ItemID   string `json:"item_id"`
		OK       bool   `json:"ok"`
		Message  string `json:"message,omitempty"`
		Provider string `json:"provider,omitempty"`
		TmdbID   int64  `json:"tmdb_id,omitempty"`
	}
	results := make([]applyResult, 0, len(body.Items))
	// 批量采纳每条 15s 超时,避免单条拖慢整个批次;上游 body 解析走无超时 context
	for _, pair := range body.Items {
		res := applyResult{ItemID: pair.ItemID}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		provider, externalID, err := services.ResolveIdentifyCandidate(ctx, state.DB, pair.ItemID, pair.CandidateID)
		if err != nil {
			cancel()
			res.Message = err.Error()
			results = append(results, res)
			continue
		}
		out, err := services.ScrapeItemByProviderID(ctx, state.DB, pair.ItemID, provider, externalID)
		if err != nil {
			cancel()
			res.Message = err.Error()
			results = append(results, res)
			continue
		}
		_, _ = state.DB.Exec(ctx, "DELETE FROM identify_candidates WHERE item_id = $1::uuid", pair.ItemID)
		cancel()
		res.OK = true
		res.Provider = provider
		if v, ok := out["tmdb_id"].(int64); ok {
			res.TmdbID = v
		}
		results = append(results, res)
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}

// scrapeAll 是"刮削缺失元数据"的入口。
// 方案 C 后不再跑一个 legacy 批处理任务,改为把所有缺 overview 且未识别的
// Movie/Series 以 refresh 优先级入队 identify,由 ScrapeWorker 持续消费。
// 返回入队数量,前端 toast 提示后引导用户到"观测中心 > 后台任务"看进度。
func scrapeAll(c *gin.Context) {
	state := GetState(c)
	n, err := services.EnqueueMissingScrapeIdentify(c.Request.Context(), state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"enqueued": n})
}

func refreshLibraryMetadata(c *gin.Context) {
	state := GetState(c)
	if state.RefreshQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "refresh queue not ready"})
		return
	}

	req, hasBody, err := parseLibraryRefreshRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	scopes, err := resolveLibraryRefreshScopes(req, hasBody, []services.RefreshScope{services.RefreshScopeMetadata})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	opts := buildLibraryRefreshOptions(req)
	if opts.ValidateOnly && opts.AllowRemote {
		c.JSON(http.StatusBadRequest, gin.H{"message": "validate_only 不支持 allow_remote=true"})
		return
	}

	scopeItems, queuedTasks, err := enqueueLibraryRefreshScopes(c.Request.Context(), state, nil, scopes, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"status":        "accepted",
		"queued_tasks":  queuedTasks,
		"scope_items":   scopeItems,
		"allow_remote":  opts.AllowRemote,
		"validate_only": opts.ValidateOnly,
	})
}

// stopScrape 是旧 API 的兼容 no-op。刮削已由 scrape_queue 驱动,
// 单条失败任务请在后台任务面板重试/取消,整体"停止刮削"已无语义。
func stopScrape(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "刮削已由队列驱动,无需停止;请到后台任务面板重试/取消单条失败任务",
	})
}

func getScrapeProgress(c *gin.Context) {
	state := GetState(c)
	c.JSON(http.StatusOK, buildEffectiveScrapeProgress(c.Request.Context(), state))
}

func getMissingScrapeCount(c *gin.Context) {
	state := GetState(c)
	n, err := services.GetMissingScrapeCount(c.Request.Context(), state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"MissingCount": n})
}

func getTaskSummary(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	c.JSON(http.StatusOK, taskSummaryResponse{
		Scrape:   buildEffectiveScrapeProgress(ctx, state),
		Probe:    buildEffectiveProbeProgress(ctx, state),
		Platform: buildPlatformTaskSummary(ctx, state),
	})
}

var rescrapeProgress struct {
	mu         sync.Mutex
	Running    bool  `json:"running"`
	Total      int64 `json:"total"`
	Success    int64 `json:"success"`
	NotFound   int64 `json:"not_found"`   // TMDB search returned no results
	FetchError int64 `json:"fetch_error"` // API timeout/network error
	Processed  int64 `json:"processed"`
}

var platformScanState struct {
	mu      sync.Mutex
	running bool
}

func getRescrapeProgress(c *gin.Context, state *AppState) {
	c.JSON(http.StatusOK, buildRescrapeProgressResponse(c.Request.Context(), state))
}

// rescrapeMissingStudio reprocesses items still pending/error for platform identification.
func rescrapeMissingStudio(c *gin.Context, state *AppState) {
	rescrapeProgress.mu.Lock()
	if rescrapeProgress.Running {
		rescrapeProgress.mu.Unlock()
		c.JSON(http.StatusOK, gin.H{"message": "already running", "total": atomic.LoadInt64(&rescrapeProgress.Total)})
		return
	}
	rescrapeProgress.Running = true
	rescrapeProgress.mu.Unlock()

	ctx := c.Request.Context()

	items, err := models.GetItemsPendingPlatformScan(ctx, state.DB, 0, false, false)
	if err != nil {
		rescrapeProgress.mu.Lock()
		rescrapeProgress.Running = false
		rescrapeProgress.mu.Unlock()
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	totalCount := int64(len(items))

	if totalCount == 0 {
		rescrapeProgress.mu.Lock()
		rescrapeProgress.Running = false
		rescrapeProgress.mu.Unlock()
		c.JSON(http.StatusOK, gin.H{"message": "no items to rescrape", "total": 0})
		return
	}

	atomic.StoreInt64(&rescrapeProgress.Total, totalCount)
	atomic.StoreInt64(&rescrapeProgress.Success, 0)
	atomic.StoreInt64(&rescrapeProgress.NotFound, 0)
	atomic.StoreInt64(&rescrapeProgress.FetchError, 0)
	atomic.StoreInt64(&rescrapeProgress.Processed, 0)

	go func() {
		defer func() {
			rescrapeProgress.mu.Lock()
			rescrapeProgress.Running = false
			rescrapeProgress.mu.Unlock()
		}()
		bgCtx := context.Background()
		client := services.TmdbClientFromConfig(bgCtx, state.DB)
		if client == nil {
			slog.Error("[Rescrape] Failed to create TMDB client")
			return
		}

		batchSize := 500
		for start := 0; start < len(items); start += batchSize {
			end := start + batchSize
			if end > len(items) {
				end = len(items)
			}
			batch := items[start:end]
			sem := make(chan struct{}, 3)
			var wg sync.WaitGroup
			for _, item := range batch {
				sem <- struct{}{}
				wg.Add(1)
				go func(scanItem models.PlatformScanItem) {
					defer func() { <-sem; wg.Done() }()
					var err error
					if scanItem.TmdbID != nil && *scanItem.TmdbID != 0 {
						_, err = services.RefreshItemMetadataByTMDBID(bgCtx, state.DB, scanItem.ID, client)
					} else {
						_, err = services.ScrapeItemWithClient(bgCtx, state.DB, scanItem.ID, client)
					}
					if err != nil {
						errMsg := err.Error()
						if strings.Contains(errMsg, "no platform matched") {
							atomic.AddInt64(&rescrapeProgress.NotFound, 1)
							_ = models.MarkPlatformScanNoMatch(bgCtx, state.DB, scanItem.ID, models.PlatformScanSourceSearch, errMsg)
						} else if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "no TMDB ID") || strings.Contains(errMsg, "no results") || strings.Contains(errMsg, "no match") || strings.Contains(errMsg, "identify failed") {
							atomic.AddInt64(&rescrapeProgress.NotFound, 1)
							_ = models.MarkPlatformScanUnidentified(bgCtx, state.DB, scanItem.ID, models.PlatformScanSourceSearch, errMsg)
						} else {
							atomic.AddInt64(&rescrapeProgress.FetchError, 1)
							_ = models.MarkPlatformScanError(bgCtx, state.DB, scanItem.ID, models.PlatformScanSourceSearch, errMsg)
						}
					} else {
						atomic.AddInt64(&rescrapeProgress.Success, 1)
					}
					atomic.AddInt64(&rescrapeProgress.Processed, 1)
				}(item)
			}
			wg.Wait()
		}

		s := atomic.LoadInt64(&rescrapeProgress.Success)
		nf := atomic.LoadInt64(&rescrapeProgress.NotFound)
		fe := atomic.LoadInt64(&rescrapeProgress.FetchError)
		slog.Info("[Rescrape] Done", "total", totalCount, "success", s, "not_found", nf, "fetch_error", fe)
	}()

	c.JSON(http.StatusOK, gin.H{"message": "rescraping", "total": totalCount})
}
