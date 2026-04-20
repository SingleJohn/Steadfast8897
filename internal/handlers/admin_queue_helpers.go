package handlers

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"fyms/internal/services"
)

// tmdbRequestCountSnapshot 用别名避免 handlers 直接引用 package-level symbol。
func tmdbRequestCountSnapshot() int64 {
	return services.TmdbRequestCount()
}

// persistIngestWorkerCount 把 ingest_worker_count 写入 system_config,
// 重启后 IngestWorker 启动会通过 loadDesiredCount 读到这个值。
func persistIngestWorkerCount(c *gin.Context, n int) error {
	state := GetState(c)
	return services.WriteSystemConfigValue(c.Request.Context(), state.DB, "ingest_worker_count", strconv.Itoa(n))
}
