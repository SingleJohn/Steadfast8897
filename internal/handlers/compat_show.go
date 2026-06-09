package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"fyms/internal/dto"
	"fyms/internal/middleware"
	"fyms/internal/models"
)

func getSeasons(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	seriesID := c.Param("seriesId")
	suid, err := models.ResolveToUUID(ctx, state.DB, seriesID)
	if err != nil || suid == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid series id"})
		return
	}

	seriesRow, _ := models.GetItemByID(ctx, state.DB, *suid)
	var seriesImageTag, seriesBackdropTag, seriesNameVal *string
	if seriesRow != nil {
		seriesImageTag = seriesRow.PrimaryImageTag
		seriesBackdropTag = seriesRow.BackdropImageTag
		seriesNameVal = &seriesRow.Name
	}

	rows, err := state.DB.Query(ctx,
		`SELECT id FROM items WHERE parent_id = $1::uuid AND type = 'Season' ORDER BY index_number NULLS LAST, sort_name ASC`,
		*suid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	var items []dto.BaseItemDto
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		row, err := models.GetItemByID(ctx, state.DB, id)
		if err != nil || row == nil {
			continue
		}
		d := dto.FormatItemDto(row, state.Config.ServerID, nil)
		d.SeriesID = suid
		if d.SeriesName == nil {
			d.SeriesName = seriesNameVal
		}
		childCount, _ := models.GetChildCount(ctx, state.DB, id)
		d.ChildCount = &childCount

		if len(d.ImageTags) == 0 && seriesImageTag != nil {
			d.SeriesPrimaryImageTag = seriesImageTag
			d.SeriesPrimaryImageItemID = suid
			d.ParentPrimaryImageItemID = suid
			d.ParentPrimaryImageTag = seriesImageTag
			d.ParentThumbItemID = suid
			d.ParentThumbImageTag = seriesImageTag
		}
		if len(d.BackdropImageTags) == 0 && seriesBackdropTag != nil {
			d.ParentBackdropItemID = suid
			d.ParentBackdropImageTags = []string{*seriesBackdropTag}
		}
		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Items": items, "TotalRecordCount": len(items)})
}

func getEpisodes(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	seriesID := c.Param("seriesId")
	seasonID := c.Query("SeasonId")
	if seasonID == "" {
		seasonID = c.Query("seasonId")
	}

	suid, err := models.ResolveToUUID(ctx, state.DB, seriesID)
	if err != nil || suid == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid series id"})
		return
	}

	auth := middleware.GetAuthUser(c)
	userID := ""
	if quid := c.Query("UserId"); quid != "" {
		userID = quid
	} else if quid := c.Query("userId"); quid != "" {
		userID = quid
	} else if auth != nil && !strings.HasPrefix(auth.ID, "api-key-") {
		userID = auth.ID
	}
	if userID != "" {
		if _, err := uuid.Parse(userID); err != nil {
			userID = ""
		}
	}

	limit := int64(0)
	if v := c.Query("Limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			limit = n
		}
	}
	startIndex := int64(0)
	if v := c.Query("StartIndex"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			startIndex = n
		}
	}

	var countSQL, itemSQL string
	var bindID string
	if seasonID != "" {
		sid, rerr := models.ResolveToUUID(ctx, state.DB, seasonID)
		if rerr != nil || sid == nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid SeasonId"})
			return
		}
		bindID = *sid
		countSQL = "SELECT COUNT(*) FROM items WHERE parent_id = $1::uuid AND type = 'Episode'"
		itemSQL = `SELECT i.id FROM items i WHERE i.parent_id = $1::uuid AND i.type = 'Episode' ORDER BY i.index_number NULLS LAST, i.sort_name ASC, i.id ASC`
	} else {
		bindID = *suid
		countSQL = "SELECT COUNT(*) FROM items WHERE series_id = $1::uuid AND type = 'Episode'"
		itemSQL = `SELECT i.id FROM items i WHERE i.series_id = $1::uuid AND i.type = 'Episode' ORDER BY i.parent_index_number NULLS LAST, i.index_number NULLS LAST, i.id ASC`
	}

	var totalCount int64
	_ = state.DB.QueryRow(ctx, countSQL, bindID).Scan(&totalCount)

	if limit > 0 {
		itemSQL += " LIMIT " + strconv.FormatInt(limit, 10)
	}
	if startIndex > 0 {
		itemSQL += " OFFSET " + strconv.FormatInt(startIndex, 10)
	}

	rows, qerr := state.DB.Query(ctx, itemSQL, bindID)
	if qerr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": qerr.Error()})
		return
	}
	defer rows.Close()

	seriesRow, _ := models.GetItemByID(ctx, state.DB, *suid)
	var seriesImageTag, seriesBackdropTag, seriesNameVal *string
	if seriesRow != nil {
		seriesImageTag = seriesRow.PrimaryImageTag
		seriesBackdropTag = seriesRow.BackdropImageTag
		seriesNameVal = &seriesRow.Name
	}

	var items []dto.BaseItemDto
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		row, err := models.GetItemByID(ctx, state.DB, id)
		if err != nil || row == nil {
			continue
		}

		var ud *dto.UserDataRow
		if userID != "" {
			u, err := models.GetUserItemData(ctx, state.DB, userID, id)
			if err == nil && u != nil {
				ud = u
			}
		}
		d := dto.FormatItemDto(row, state.Config.ServerID, ud)
		d.SeriesID = suid
		if d.SeriesName == nil {
			d.SeriesName = seriesNameVal
		}

		if len(d.ImageTags) == 0 && seriesImageTag != nil {
			d.SeriesPrimaryImageTag = seriesImageTag
			d.SeriesPrimaryImageItemID = suid
			d.ParentPrimaryImageItemID = suid
			d.ParentPrimaryImageTag = seriesImageTag
			d.ParentThumbItemID = suid
			d.ParentThumbImageTag = seriesImageTag
		}
		if len(d.BackdropImageTags) == 0 && seriesBackdropTag != nil {
			d.ParentBackdropItemID = suid
			d.ParentBackdropImageTags = []string{*seriesBackdropTag}
		}

		if row.FilePath != nil && *row.FilePath != "" {
			sources := buildItemMediaSources(ctx, state, id, row)
			if len(sources) > 0 {
				hideMediaSourceSizeForInfuse(c, sources)
				d.MediaSources = sources
				d.MediaStreams = sources[0].MediaStreams
			}
		}

		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Items": items, "TotalRecordCount": totalCount})
}
