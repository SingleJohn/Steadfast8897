package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"fyms/internal/repository"
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

func testSourceRuntimeCSP(c *gin.Context, state *AppState) {
	var req sourcebridge.CSPRuntimeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if state == nil || state.CSPRuntime == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "CSP runtime 未初始化"})
		return
	}
	resp, err := state.CSPRuntime.Run(c.Request.Context(), req)
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

func listSourceRuntimeInvocations(c *gin.Context, state *AppState) {
	if state == nil || state.Repo == nil || state.Repo.Source == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "source repository 未初始化"})
		return
	}
	var providerID *int64
	if raw := strings.TrimSpace(c.Query("provider_id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"message": "invalid provider_id"})
			return
		}
		providerID = &id
	}
	items, err := state.Repo.Source.ListRuntimeInvocations(c.Request.Context(), repository.SourceRuntimeInvocationListOptions{
		Limit:      int64(queryInt(c, "limit", 100)),
		Offset:     int64(queryInt(c, "offset", 0)),
		ProviderID: providerID,
		Method:     strings.TrimSpace(c.Query("method")),
		Status:     strings.TrimSpace(c.Query("status")),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
