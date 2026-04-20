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
//	GET  /Admin/Metrics/Snapshot              → ingest/scrape/tmdb 的当前快照
//	POST /Admin/Scrape/Cache/Invalidate       → 让 Aggregator/TmdbClient 缓存失效(改 tmdb 配置免重启)
//	POST /Admin/Ingest/Workers {count: N}     → 动态调整 ingest worker 数量(同步写 system_config)
func RegisterAdminQueueRoutes(group *gin.RouterGroup, state *AppState, adminMW gin.HandlerFunc) {
	_ = state
	group.GET("/Admin/ScrapeQueue/Stats", adminMW, getScrapeQueueStats)
	group.GET("/Admin/ScrapeQueue/Recent", adminMW, getScrapeQueueRecent)
	group.GET("/Admin/ScrapeQueue/Task/:id", adminMW, getScrapeQueueTaskDetail)
	group.POST("/Admin/ScrapeQueue/Retry/:id", adminMW, retryScrapeQueueTask)
	group.POST("/Admin/ScrapeQueue/RetryAllFailed", adminMW, retryAllFailedTasks)
	group.GET("/Admin/Metrics/Snapshot", adminMW, getMetricsSnapshot)
	group.POST("/Admin/Scrape/Cache/Invalidate", adminMW, invalidateScrapeCache)
	group.POST("/Admin/Ingest/Workers", adminMW, setIngestWorkerCount)
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
	limit := 20
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	tasks, err := state.ScrapeQueue.Recent(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
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
