package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"

	sourcebridge "fyms/internal/source"
)

func testSourceRuntimeJS(c *gin.Context, state *AppState) {
	var req sourcebridge.JSRuntimeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if state == nil || state.JSRuntime == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "JS runtime 未初始化"})
		return
	}
	resp, err := state.JSRuntime.Run(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func listSourceRuntimeArtifacts(c *gin.Context, state *AppState) {
	if state == nil || state.Repo == nil || state.Repo.Source == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "source repository 未初始化"})
		return
	}
	items, err := state.Repo.Source.ListRuntimeArtifacts(c.Request.Context(), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
