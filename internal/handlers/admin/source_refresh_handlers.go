package admin

import (
	sourcebridge "fyms/internal/source"
	"net/http"

	"github.com/gin-gonic/gin"
)

// fetchProviderCatalog 把单个 provider 的分类抓取入队(后台批量入库以填充虚拟库)。
func fetchProviderCatalog(c *gin.Context, state *AppState) {
	id, ok := pathInt64(c, "id")
	if !ok {
		return
	}
	queue := sourcebridge.NewSourceRefreshQueue(state.Repo.Source)
	if err := queue.EnqueueCatalogFetch(c.Request.Context(), id, sourcebridge.RefreshPriorityManual); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"enqueued": 1, "count": 1})
}

// batchFetchProviderCatalog 批量入队抓取(配合跨页全选)。
func batchFetchProviderCatalog(c *gin.Context, state *AppState) {
	var req sourceProviderBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ids := compactRequestInt64s(req.ProviderIDs)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "provider_ids required"})
		return
	}
	queue := sourcebridge.NewSourceRefreshQueue(state.Repo.Source)
	enqueued := 0
	for _, id := range ids {
		if err := queue.EnqueueCatalogFetch(c.Request.Context(), id, sourcebridge.RefreshPriorityManual); err == nil {
			enqueued++
		}
	}
	c.JSON(http.StatusOK, gin.H{"enqueued": enqueued, "count": enqueued})
}

// refreshSourceItemDetail 同步重拉某条在线条目的 detail(追更集数),立等结果。
func refreshSourceItemDetail(c *gin.Context, state *AppState) {
	id, ok := pathInt64(c, "id")
	if !ok {
		return
	}
	result, err := sourcebridge.RefreshSourceItemDetail(c.Request.Context(), state.Repo.Source, state.HTTPClient, state.JSRuntime, state.CSPRuntime, id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"loaded": result.Loaded, "play_source_count": len(result.PlaySources)})
}

// sourceRefreshQueueStats 返回在线源刷新队列各状态计数,供前端面板展示进度。
func sourceRefreshQueueStats(c *gin.Context, state *AppState) {
	stats, err := state.Repo.Source.SourceRefreshQueueStats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}
