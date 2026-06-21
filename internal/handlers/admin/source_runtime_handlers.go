package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"

	sourcebridge "fyms/internal/source"
)

func testSourceRuntimeJS(c *gin.Context, state *AppState) {
	var req sourcebridge.DRPYPoCRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	dataDir := "data"
	if state != nil && state.Config != nil && state.Config.DataDir != "" {
		dataDir = state.Config.DataDir
	}
	client := http.DefaultClient
	if state != nil && state.HTTPClient != nil {
		client = state.HTTPClient
	}
	runner := sourcebridge.NewDRPYPoCRunner(client, dataDir)
	resp, err := runner.Run(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}
