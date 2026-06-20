package admin

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// RegisterSystemMetricsRoutes 注册 CPU/RAM/Net 实时观测接口。
//
//	GET /System/Metrics        → {current, history[60]} JSON
//	GET /System/Metrics/stream → SSE，event: "metric"，每次采样推送一条
//
// EventSource 不支持自定义 header，token 通过 ?api_key= 传递
// （middleware.RequireAdmin 已兼容该路径，同 /Tasks/stream）。
func RegisterSystemMetricsRoutes(group *gin.RouterGroup, state *AppState, adminMW gin.HandlerFunc) {
	_ = state
	group.GET("/System/Metrics", adminMW, getSystemMetrics)
	group.GET("/System/Metrics/stream", adminMW, streamSystemMetrics)
}

func getSystemMetrics(c *gin.Context) {
	state := GetState(c)
	if state.SysMetrics == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "sysmetrics not initialized"})
		return
	}
	b, err := state.SysMetrics.MarshalHistory()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", b)
}

func streamSystemMetrics(c *gin.Context) {
	state := GetState(c)
	if state.SysMetrics == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "sysmetrics not initialized"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	ch, cancel := state.SysMetrics.Subscribe()
	defer cancel()

	// 先推一条当前值避免白屏
	if !writeSSE(c, "metric", state.SysMetrics.Snapshot()) {
		return
	}
	c.Writer.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	done := c.Request.Context().Done()
	for {
		select {
		case s := <-ch:
			if !writeSSE(c, "metric", s) {
				return
			}
			c.Writer.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(c.Writer, ": keepalive\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		case <-done:
			return
		}
	}
}
