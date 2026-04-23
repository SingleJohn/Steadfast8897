package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// RegisterAdminQueueRoutes 注册刮削队列/metrics/worker 管理 endpoint(Phase 5 队列面板用)。
//
//	GET  /Admin/ScrapeQueue/Stats             → pending/running/done/failed 计数
//	GET  /Admin/ScrapeQueue/Recent?limit=20   → 最近 failed+running 任务(含 item_name/error)
//	GET  /Admin/ScrapeQueue/Task/:id          → 单条任务详情(含 request_url / response_status / response_sample)
//	POST /Admin/ScrapeQueue/Retry/:id         → 重置单个 failed 任务为 pending
//	POST /Admin/ScrapeQueue/RetryAllFailed    → 重置所有 failed 任务
//	GET  /Admin/RefreshQueue/Stats            → refresh_queue pending/running/done/failed 计数
//	GET  /Admin/RefreshQueue/Recent?limit=20  → 最近 failed+running refresh 任务
//	GET  /Admin/RefreshQueue/Task/:id         → 单条 refresh 任务详情(含 options/source/scope)
//	POST /Admin/RefreshQueue/Retry/:id        → 重置单个 failed refresh 任务
//	POST /Admin/RefreshQueue/RetryAllFailed   → 重置所有 failed refresh 任务
//	GET  /Admin/Metrics/Snapshot              → ingest/scrape/tmdb 的当前快照
//	POST /Admin/Scrape/Cache/Invalidate       → 让 Aggregator/TmdbClient 缓存失效(改 tmdb 配置免重启)
//	POST /Admin/Ingest/Workers {count: N}     → 动态调整 ingest worker 数量(同步写 system_config)
//	POST /Admin/Scrape/Workers {count: N}     → 动态调整 scrape worker 数量(同步写 system_config)
//	POST /Admin/Refresh/Workers {count: N}    → 动态调整 refresh worker 数量(同步写 system_config)
func RegisterAdminQueueRoutes(group *gin.RouterGroup, state *AppState, adminMW gin.HandlerFunc) {
	_ = state
	group.GET("/Admin/ScrapeQueue/Stats", adminMW, getScrapeQueueStats)
	group.GET("/Admin/ScrapeQueue/Recent", adminMW, getScrapeQueueRecent)
	group.GET("/Admin/ScrapeQueue/Task/:id", adminMW, getScrapeQueueTaskDetail)
	group.POST("/Admin/ScrapeQueue/Retry/:id", adminMW, retryScrapeQueueTask)
	group.POST("/Admin/ScrapeQueue/RetryAllFailed", adminMW, retryAllFailedTasks)
	group.GET("/Admin/RefreshQueue/Stats", adminMW, getRefreshQueueStats)
	group.GET("/Admin/RefreshQueue/Recent", adminMW, getRefreshQueueRecent)
	group.GET("/Admin/RefreshQueue/Task/:id", adminMW, getRefreshQueueTaskDetail)
	group.POST("/Admin/RefreshQueue/Retry/:id", adminMW, retryRefreshQueueTask)
	group.POST("/Admin/RefreshQueue/RetryAllFailed", adminMW, retryAllFailedRefreshTasks)
	group.GET("/Admin/Metrics/Snapshot", adminMW, getMetricsSnapshot)
	group.POST("/Admin/Scrape/Cache/Invalidate", adminMW, invalidateScrapeCache)
	group.POST("/Admin/Ingest/Workers", adminMW, setIngestWorkerCount)
	group.POST("/Admin/Scrape/Workers", adminMW, setScrapeWorkerCount)
	group.POST("/Admin/Refresh/Workers", adminMW, setRefreshWorkerCount)
}

func getScrapeQueueStats(c *gin.Context) {
	state := GetState(c)
	if state.ScrapeQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "scrape queue not initialized"})
		return
	}
	stats, err := state.ScrapeQueue.Stats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"pending": stats.Pending,
		"running": stats.Running,
		"done":    stats.Done,
		"failed":  stats.Failed,
	})
}

func getScrapeQueueRecent(c *gin.Context) {
	state := GetState(c)
	if state.ScrapeQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "scrape queue not initialized"})
		return
	}
	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	offset := 0
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	status := c.Query("status") // "failed" | "running" | "pending" | ""(failed+running)

	ctx := c.Request.Context()
	tasks, err := state.ScrapeQueue.Recent(ctx, status, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	total, _ := state.ScrapeQueue.RecentCount(ctx, status)
	c.JSON(http.StatusOK, gin.H{"tasks": tasks, "total": total})
}

func getScrapeQueueTaskDetail(c *gin.Context) {
	state := GetState(c)
	if state.ScrapeQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "scrape queue not initialized"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return
	}
	t, err := state.ScrapeQueue.GetTaskDetail(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

func retryScrapeQueueTask(c *gin.Context) {
	state := GetState(c)
	if state.ScrapeQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "scrape queue not initialized"})
		return
	}
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return
	}
	if err := state.ScrapeQueue.RetryTask(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func retryAllFailedTasks(c *gin.Context) {
	state := GetState(c)
	if state.ScrapeQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "scrape queue not initialized"})
		return
	}
	n, err := state.ScrapeQueue.RetryAllFailed(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"reset": n})
}

func getRefreshQueueStats(c *gin.Context) {
	state := GetState(c)
	if state.RefreshQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "refresh queue not initialized"})
		return
	}
	stats, err := state.RefreshQueue.Stats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"pending": stats.Pending,
		"running": stats.Running,
		"done":    stats.Done,
		"failed":  stats.Failed,
	})
}

func getRefreshQueueRecent(c *gin.Context) {
	state := GetState(c)
	if state.RefreshQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "refresh queue not initialized"})
		return
	}
	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	offset := 0
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	status := c.Query("status")

	ctx := c.Request.Context()
	tasks, err := state.RefreshQueue.Recent(ctx, status, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	total, _ := state.RefreshQueue.RecentCount(ctx, status)
	c.JSON(http.StatusOK, gin.H{"tasks": tasks, "total": total})
}

func getRefreshQueueTaskDetail(c *gin.Context) {
	state := GetState(c)
	if state.RefreshQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "refresh queue not initialized"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return
	}
	t, err := state.RefreshQueue.GetTaskDetail(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

func retryRefreshQueueTask(c *gin.Context) {
	state := GetState(c)
	if state.RefreshQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "refresh queue not initialized"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return
	}
	if err := state.RefreshQueue.RetryTask(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func retryAllFailedRefreshTasks(c *gin.Context) {
	state := GetState(c)
	if state.RefreshQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "refresh queue not initialized"})
		return
	}
	n, err := state.RefreshQueue.RetryAllFailed(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"reset": n})
}

// getMetricsSnapshot 返回当前时刻的 ingest/scrape/tmdb 指标快照。
// 前端队列面板用它做 KPI 卡展示。
func getMetricsSnapshot(c *gin.Context) {
	state := GetState(c)
	resp := gin.H{}

	if state.Ingest != nil {
		resp["ingest_channel_depth"] = state.Ingest.ChannelDepth()
		resp["ingest_overflow_total"] = state.Ingest.OverflowCount()
		resp["ingest_worker_count"] = state.Ingest.WorkerCount()
	}
	if state.ScrapeQueue != nil {
		if stats, err := state.ScrapeQueue.Stats(c.Request.Context()); err == nil {
			resp["scrape_pending"] = stats.Pending
			resp["scrape_running"] = stats.Running
			resp["scrape_failed"] = stats.Failed
			resp["scrape_done"] = stats.Done
		}
	}
	if state.ScrapeWorker != nil {
		resp["scrape_worker_count"] = state.ScrapeWorker.WorkerCount()
	}
	if state.RefreshQueue != nil {
		if stats, err := state.RefreshQueue.Stats(c.Request.Context()); err == nil {
			resp["refresh_pending"] = stats.Pending
			resp["refresh_running"] = stats.Running
			resp["refresh_failed"] = stats.Failed
			resp["refresh_done"] = stats.Done
		}
	}
	if state.RefreshWorker != nil {
		resp["refresh_worker_count"] = state.RefreshWorker.WorkerCount()
	}
	// TmdbRequestCount 是 package 级导出函数
	resp["tmdb_requests_total"] = tmdbRequestCountSnapshot()

	c.JSON(http.StatusOK, resp)
}

func invalidateScrapeCache(c *gin.Context) {
	state := GetState(c)
	if state.ScrapeWorker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "scrape worker not initialized"})
		return
	}
	state.ScrapeWorker.InvalidateCachedClient()
	c.JSON(http.StatusOK, gin.H{"status": "invalidated"})
}

func setIngestWorkerCount(c *gin.Context) {
	state := GetState(c)
	if state.Ingest == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "ingest worker not initialized"})
		return
	}
	var body struct {
		Count int `json:"count"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	if body.Count < 1 || body.Count > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "count must be in [1, 64]"})
		return
	}
	// 1) 先写 system_config(持久化,重启后生效)
	if err := persistIngestWorkerCount(c, body.Count); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	// 2) 立即生效
	state.Ingest.SetWorkerCount(body.Count)
	c.JSON(http.StatusOK, gin.H{"count": state.Ingest.WorkerCount()})
}

// setScrapeWorkerCount 跟 setIngestWorkerCount 对称,区别:
//   - 上限 16(TMDB 共享限流 3rps,再多只是在 limiter 上排队)
//   - system_config key = scrape_worker_count
func setScrapeWorkerCount(c *gin.Context) {
	state := GetState(c)
	if state.ScrapeWorker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "scrape worker not initialized"})
		return
	}
	var body struct {
		Count int `json:"count"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	if body.Count < 1 || body.Count > 16 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "count must be in [1, 16]"})
		return
	}
	if err := persistScrapeWorkerCount(c, body.Count); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	state.ScrapeWorker.SetWorkerCount(body.Count)
	c.JSON(http.StatusOK, gin.H{"count": state.ScrapeWorker.WorkerCount()})
}

func setRefreshWorkerCount(c *gin.Context) {
	state := GetState(c)
	if state.RefreshWorker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "refresh worker not initialized"})
		return
	}
	var body struct {
		Count int `json:"count"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	if body.Count < 1 || body.Count > 8 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "count must be in [1, 8]"})
		return
	}
	if err := persistRefreshWorkerCount(c, body.Count); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	state.RefreshWorker.SetWorkerCount(body.Count)
	c.JSON(http.StatusOK, gin.H{"count": state.RefreshWorker.WorkerCount()})
}
