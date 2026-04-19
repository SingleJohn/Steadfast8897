package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"fyms/internal/services"
	"fyms/internal/services/taskcenter"
)

// RegisterTaskCenterRoutes 挂载任务中心 HTTP 接口。
//
// M1 只开放只读聚合：
//   - GET /Tasks              所有任务当前 Snapshot
//   - GET /Tasks/:kind        指定任务 Snapshot
//   - GET /Tasks/history      运行历史（按 kind / parent_id / limit 过滤）
//
// Start/Stop/Stream 将在 M2/M3 补齐。
// 路由注册到 root 和 /emby 两组（与其他 Register*Routes 保持一致）。
func RegisterTaskCenterRoutes(group *gin.RouterGroup, state *AppState, adminMW gin.HandlerFunc) {
	g := group.Group("")
	g.GET("/Tasks", adminMW, func(c *gin.Context) { listTasks(c, state) })
	g.GET("/Tasks/stream", adminMW, func(c *gin.Context) { streamTasks(c, state) })
	g.GET("/Tasks/history", adminMW, func(c *gin.Context) { listTaskHistory(c, state) })
	g.GET("/Tasks/:kind", adminMW, func(c *gin.Context) { getTask(c, state) })
	g.POST("/Tasks/:kind/start", adminMW, func(c *gin.Context) { startTask(c, state) })
	g.POST("/Tasks/:kind/stop", adminMW, func(c *gin.Context) { stopTask(c, state) })

	g.GET("/Tasks/chain", adminMW, func(c *gin.Context) { getTaskChain(c, state) })
	g.POST("/Tasks/chain", adminMW, func(c *gin.Context) { updateTaskChain(c, state) })
}

// getTaskChain 返回当前任务链配置（开关 + 规则）。
func getTaskChain(c *gin.Context, state *AppState) {
	if state.TaskChain == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "task chain not initialized"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled": state.TaskChain.IsEnabled(),
		"rules":   state.TaskChain.Rules(),
	})
}

// updateTaskChainRequest 是 POST /Tasks/chain 的请求体；字段可选，未提供时不改动。
type updateTaskChainRequest struct {
	Enabled *bool                  `json:"enabled"`
	Rules   *[]taskcenter.ChainRule `json:"rules"`
}

func updateTaskChain(c *gin.Context, state *AppState) {
	if state.TaskChain == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "task chain not initialized"})
		return
	}
	var req updateTaskChainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	ctx := c.Request.Context()
	if req.Rules != nil {
		state.TaskChain.SetRules(*req.Rules)
		if js, err := state.TaskChain.RulesJSON(); err == nil {
			_ = services.WriteSystemConfigValue(ctx, state.DB, "task_chain_rules", js)
		}
	}
	if req.Enabled != nil {
		state.TaskChain.SetEnabled(*req.Enabled)
		_ = services.WriteBoolSystemConfig(ctx, state.DB, "task_chain_enabled", *req.Enabled)
	}

	c.JSON(http.StatusOK, gin.H{
		"enabled": state.TaskChain.IsEnabled(),
		"rules":   state.TaskChain.Rules(),
	})
}

func listTasks(c *gin.Context, state *AppState) {
	if state.TaskCenter == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "task center not initialized"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": state.TaskCenter.SnapshotAll()})
}

func getTask(c *gin.Context, state *AppState) {
	if state.TaskCenter == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "task center not initialized"})
		return
	}
	kind := taskcenter.Kind(c.Param("kind"))
	t := state.TaskCenter.Get(kind)
	if t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "unknown task kind: " + string(kind)})
		return
	}
	c.JSON(http.StatusOK, t.Snapshot())
}

func listTaskHistory(c *gin.Context, state *AppState) {
	filter := taskcenter.HistoryFilter{
		Kind:  taskcenter.Kind(c.Query("kind")),
		Limit: parseIntDefault(c.Query("limit"), 100),
	}
	if raw := c.Query("parent_id"); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			filter.ParentID = &v
		}
	}
	rows, err := taskcenter.History(c.Request.Context(), state.DB, filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": rows})
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

// startTask 启动一个任务。body 是自由 JSON，由适配器解析
// （如 probe 的 threads、backfill 的 stages、update 的 action）。
// 幂等：已在跑则返回当前 runID 不报错。
func startTask(c *gin.Context, state *AppState) {
	if state.TaskCenter == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "task center not initialized"})
		return
	}
	kind := taskcenter.Kind(c.Param("kind"))
	t := state.TaskCenter.Get(kind)
	if t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "unknown task kind: " + string(kind)})
		return
	}
	var params taskcenter.StartParams
	_ = c.ShouldBindJSON(&params) // 空 body 合法，适配器自己兜默认值

	runID, err := t.Start(c.Request.Context(), params, taskcenter.TriggerManual)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"message": err.Error(), "snapshot": t.Snapshot()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"runId": runID, "snapshot": t.Snapshot()})
}

// stopTask 请求停止任务；若任务不可取消或已终止，返回当前 snapshot 即视为成功。
func stopTask(c *gin.Context, state *AppState) {
	if state.TaskCenter == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "task center not initialized"})
		return
	}
	kind := taskcenter.Kind(c.Param("kind"))
	t := state.TaskCenter.Get(kind)
	if t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "unknown task kind: " + string(kind)})
		return
	}
	if err := t.Stop(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"snapshot": t.Snapshot()})
}

// streamTasks 以 SSE 形式推送任务状态变化。
//
// 事件语义：
//   - 事件类型统一为 "snapshot"，payload 是一个 Snapshot 对象。
//   - 连接建立后先推一遍 SnapshotAll 作为初始值，客户端收到 5 条 snapshot 后即可完整渲染。
//   - Registry 的 broadcaster 只在关键字段变化时调用 Publish，心跳 15s 一次防代理断开。
func streamTasks(c *gin.Context, state *AppState) {
	if state.TaskCenter == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "task center not initialized"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // 让 nginx 不缓冲

	ch, cancel := state.TaskCenter.Subscribe()
	defer cancel()

	// 先推初始全量，避免前端白屏等下一次变化。
	for _, s := range state.TaskCenter.SnapshotAll() {
		if !writeSSE(c, "snapshot", s) {
			return
		}
	}
	c.Writer.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	done := c.Request.Context().Done()
	for {
		select {
		case s, ok := <-ch:
			if !ok {
				return
			}
			if !writeSSE(c, "snapshot", s) {
				return
			}
			c.Writer.Flush()
		case <-heartbeat.C:
			if _, err := c.Writer.Write([]byte(": keepalive\n\n")); err != nil {
				return
			}
			c.Writer.Flush()
		case <-done:
			return
		}
	}
}

// writeSSE 写一个 SSE 事件。返回 false 表示连接已断开，调用方应退出循环。
func writeSSE(c *gin.Context, event string, payload any) bool {
	b, err := json.Marshal(payload)
	if err != nil {
		return true // 序列化失败不视为连接断开
	}
	if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, b); err != nil {
		return false
	}
	return true
}
