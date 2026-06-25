package admin

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func getSourceConfig(c *gin.Context, state *AppState) {
	id, ok := pathInt64(c, "id")
	if !ok {
		return
	}
	item, err := state.Repo.Source.GetConfigDetail(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "source config not found"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func getSourceConfigImpact(c *gin.Context, state *AppState) {
	id, ok := pathInt64(c, "id")
	if !ok {
		return
	}
	impact, err := state.Repo.Source.GetConfigImpact(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if impact == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "source config not found"})
		return
	}
	c.JSON(http.StatusOK, impact)
}

func deleteSourceConfig(c *gin.Context, state *AppState) {
	id, ok := pathInt64(c, "id")
	if !ok {
		return
	}
	if !queryBool(c, "confirm", false) {
		impact, err := state.Repo.Source.GetConfigImpact(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		if impact == nil {
			c.JSON(http.StatusNotFound, gin.H{"message": "source config not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "confirm=true required",
			"impact":  impact,
		})
		return
	}
	result, err := state.Repo.Source.DeleteConfigCascade(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if result == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "source config not found"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func queryBool(c *gin.Context, name string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(c.Query(name)))
	if raw == "" {
		return fallback
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}
