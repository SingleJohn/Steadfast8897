package library

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"fyms/internal/models"
	"fyms/internal/services"
)

// backfillAllActorImages 全库批量补演员头像(按名源 + TMDB 入队)。
func backfillAllActorImages(c *gin.Context) {
	state := GetState(c)
	ctx := c.Request.Context()
	res, err := services.BackfillAllActorImages(ctx, state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

// actorImageSummary 返回演员头像覆盖统计,供前端展示。
func actorImageSummary(c *gin.Context) {
	state := GetState(c)
	ctx := c.Request.Context()
	stats, err := models.GetActorImageStats(ctx, state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}
