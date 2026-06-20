package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"fyms/internal/dto"
	"fyms/internal/middleware"
	"fyms/internal/models"
)

func getNextUpItems(c *gin.Context, state *AppState) {
	auth := middleware.GetAuthUser(c)
	if auth == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
		return
	}
	userID := strings.TrimSpace(c.Query("UserId"))
	if userID == "" {
		userID = auth.ID
	}
	if userID != auth.ID && !auth.IsAdmin {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}

	ctx := c.Request.Context()
	var seriesID *string
	if sid := strings.TrimSpace(c.Query("SeriesId")); sid != "" {
		if resolved, err := models.ResolveToUUID(ctx, state.DB, sid); err == nil && resolved != nil {
			seriesID = resolved
		} else {
			seriesID = &sid
		}
	}

	limit := int64(20)
	if s := strings.TrimSpace(c.Query("Limit")); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			limit = n
		}
	}

	res, err := models.QueryNextUp(ctx, state.DB, userID, seriesID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	serverID := state.Config.ServerID
	items := make([]dto.BaseItemDto, 0, len(res.Items))
	for i := range res.Items {
		var ud *dto.UserDataRow
		if i < len(res.UserData) {
			ud = &res.UserData[i]
		}
		items = append(items, dto.FormatItemDto(&res.Items[i], serverID, ud))
	}
	applySeasonNames(ctx, state.DB, items)

	c.JSON(http.StatusOK, gin.H{
		"Items":            baseItemsToEmbyMaps(items),
		"TotalRecordCount": embyTotalRecordCount(c, res.TotalCount),
	})
}
