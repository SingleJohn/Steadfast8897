package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"fyms/internal/services"
	"fyms/internal/services/taskcenter"
)

// startBackfillRequest 允许 body 指定要跑的 stages,缺省跑 DefaultBackfillStages(quality → name → image)。
type startBackfillRequest struct {
	Stages []string `json:"stages"`
}

// startBackfill 统一走任务中心：确保 task_runs 完整记录父/子 run。
// Registry 未初始化时降级直接调 BackfillTask（保留启动过程中的兼容路径）。
func startBackfill(c *gin.Context, state *AppState) {
	var req startBackfillRequest
	_ = c.ShouldBindJSON(&req)

	if t := state.TaskCenter.Get(taskcenter.KindBackfill); t != nil {
		params := taskcenter.StartParams{}
		if len(req.Stages) > 0 {
			raw := make([]any, len(req.Stages))
			for i, s := range req.Stages {
				raw[i] = s
			}
			params["stages"] = raw
		}
		if _, err := t.Start(c.Request.Context(), params, taskcenter.TriggerManual); err != nil {
			c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, state.BackfillTask.GetProgress())
		return
	}

	// Registry 缺失时的降级路径（实践中不应发生）。
	var stages []services.BackfillStage
	for _, s := range req.Stages {
		stages = append(stages, services.BackfillStage(s))
	}
	if err := state.BackfillTask.Start(c.Request.Context(), state.DB, stages); err != nil {
		c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, state.BackfillTask.GetProgress())
}

func stopBackfill(c *gin.Context, state *AppState) {
	if t := state.TaskCenter.Get(taskcenter.KindBackfill); t != nil {
		_ = t.Stop()
	} else {
		state.BackfillTask.Stop()
	}
	c.JSON(http.StatusOK, state.BackfillTask.GetProgress())
}

func getBackfillProgress(c *gin.Context, state *AppState) {
	c.JSON(http.StatusOK, state.BackfillTask.GetProgress())
}

type backfillConfigResponse struct {
	EnabledOnStartup  bool `json:"enabled_on_startup"`
	EpisodeStillFetch bool `json:"episode_still_fetch"`
}

func getBackfillConfig(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	c.JSON(http.StatusOK, backfillConfigResponse{
		EnabledOnStartup:  services.ReadBackfillEnabledOnStartup(ctx, state.DB),
		EpisodeStillFetch: services.ReadEpisodeStillFetch(ctx, state.DB),
	})
}

type updateBackfillConfigRequest struct {
	EnabledOnStartup  *bool `json:"enabled_on_startup"`
	EpisodeStillFetch *bool `json:"episode_still_fetch"`
}

func updateBackfillConfig(c *gin.Context, state *AppState) {
	var req updateBackfillConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ctx := c.Request.Context()
	if req.EnabledOnStartup != nil {
		_ = state.Repo.SystemConfig.SetBool(ctx, "backfill_enabled_on_startup", *req.EnabledOnStartup)
	}
	if req.EpisodeStillFetch != nil {
		_ = state.Repo.SystemConfig.SetBool(ctx, "episode_still_fetch", *req.EpisodeStillFetch)
	}
	getBackfillConfig(c, state)
}

// resetBackfillQuality 清空 media_versions 的画质列,便于全量重算(幂等判定条件会再次命中)。
func resetBackfillQuality(c *gin.Context, state *AppState) {
	if err := state.Repo.Background.ResetMediaVersionQuality(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "quality fields reset"})
}

// resetBackfillEpisodeImage 只清理由 TMDB still 下载写入的 Episode 封面,不碰本地兜底命中的路径。
func resetBackfillEpisodeImage(c *gin.Context, state *AppState) {
	rows, err := state.Repo.Background.ResetEpisodeStillImages(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "episode still fields reset", "rows": rows})
}
