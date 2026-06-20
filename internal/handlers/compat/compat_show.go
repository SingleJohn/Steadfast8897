package compat

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"fyms/internal/dto"
	mediahandlers "fyms/internal/handlers/media"
	embysupport "fyms/internal/handlers/mediasupport"
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

	seasonIDs, err := state.Repo.Playback.ListSeasonIDsForCompat(ctx, *suid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	var items []dto.BaseItemDto
	for _, id := range seasonIDs {
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

		if seriesImageTag != nil {
			d.SeriesPrimaryImageTag = seriesImageTag
			if d.SeriesPrimaryImageItemID == nil {
				d.SeriesPrimaryImageItemID = suid
			}
			if d.ParentPrimaryImageItemID == nil {
				d.ParentPrimaryImageItemID = suid
			}
			if d.ParentPrimaryImageTag == nil {
				d.ParentPrimaryImageTag = seriesImageTag
			}
			if d.ParentThumbItemID == nil {
				d.ParentThumbItemID = suid
			}
			if d.ParentThumbImageTag == nil {
				d.ParentThumbImageTag = seriesImageTag
			}
		}
		if len(d.BackdropImageTags) == 0 && seriesBackdropTag != nil {
			d.ParentBackdropItemID = suid
			d.ParentBackdropImageTags = []string{*seriesBackdropTag}
		}
		items = append(items, d)
	}
	ApplyUnplayedItemCounts(ctx, state.DB, userID, items)
	c.JSON(http.StatusOK, gin.H{"Items": embysupport.BaseItemsToEmbyMaps(items), "TotalRecordCount": EmbyTotalRecordCount(c, int64(len(items)))})
}

func getEpisodes(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	seriesIDParam := c.Param("seriesId")
	seasonID := c.Query("SeasonId")
	if seasonID == "" {
		seasonID = c.Query("seasonId")
	}
	seasonNumQuery := c.Query("Season")
	if seasonNumQuery == "" {
		seasonNumQuery = c.Query("season")
	}

	var suid *string
	var err error
	var resolvedSeasonID string
	if seasonID != "" {
		sid, rerr := models.ResolveToUUID(ctx, state.DB, seasonID)
		if rerr != nil || sid == nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid SeasonId"})
			return
		}
		var parentSeriesID string
		if parent, err := state.Repo.Playback.GetSeasonParentSeriesID(ctx, *sid); err == nil && parent != nil && *parent != "" {
			resolvedSeasonID = *sid
			parentSeriesID = *parent
			suid = &parentSeriesID
		}
	}
	if suid == nil {
		suid, err = models.ResolveToUUID(ctx, state.DB, seriesIDParam)
		if err != nil || suid == nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid series id"})
			return
		}
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

	var bindID string
	var bySeason bool
	if resolvedSeasonID != "" {
		bindID = resolvedSeasonID
		bySeason = true
	} else if seasonNumQuery != "" {
		seasonNum, perr := strconv.ParseInt(seasonNumQuery, 10, 32)
		if perr != nil || seasonNum < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid Season"})
			return
		}
		seasonIDByNumber, err := state.Repo.Playback.FindSeasonIDByNumber(ctx, *suid, int32(seasonNum))
		if err != nil || seasonIDByNumber == nil {
			c.JSON(http.StatusOK, gin.H{"Items": []dto.BaseItemDto{}, "TotalRecordCount": 0})
			return
		}
		bindID = *seasonIDByNumber
		bySeason = true
	} else {
		bindID = *suid
	}

	var totalCount int64
	var episodeIDs []string
	if bySeason {
		totalCount, _ = state.Repo.Playback.CountEpisodesBySeason(ctx, bindID)
		episodeIDs, err = state.Repo.Playback.ListEpisodeIDsBySeason(ctx, bindID, limit, startIndex)
	} else {
		totalCount, _ = state.Repo.Playback.CountEpisodesBySeries(ctx, bindID)
		episodeIDs, err = state.Repo.Playback.ListEpisodeIDsBySeries(ctx, bindID, limit, startIndex)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	seriesRow, _ := models.GetItemByID(ctx, state.DB, *suid)
	var seriesImageTag, seriesBackdropTag, seriesNameVal *string
	if seriesRow != nil {
		seriesImageTag = seriesRow.PrimaryImageTag
		seriesBackdropTag = seriesRow.BackdropImageTag
		seriesNameVal = &seriesRow.Name
	}

	var items []dto.BaseItemDto
	for _, id := range episodeIDs {
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

		if seriesImageTag != nil {
			d.SeriesPrimaryImageTag = seriesImageTag
			if d.SeriesPrimaryImageItemID == nil {
				d.SeriesPrimaryImageItemID = suid
			}
			if d.ParentPrimaryImageItemID == nil {
				d.ParentPrimaryImageItemID = suid
			}
			if d.ParentPrimaryImageTag == nil {
				d.ParentPrimaryImageTag = seriesImageTag
			}
			if d.ParentThumbItemID == nil {
				d.ParentThumbItemID = suid
			}
			if d.ParentThumbImageTag == nil {
				d.ParentThumbImageTag = seriesImageTag
			}
		}
		if len(d.BackdropImageTags) == 0 && seriesBackdropTag != nil {
			d.ParentBackdropItemID = suid
			d.ParentBackdropImageTags = []string{*seriesBackdropTag}
		}

		if row.FilePath != nil && *row.FilePath != "" {
			sources := mediahandlers.BuildItemMediaSources(ctx, state, id, row)
			if len(sources) > 0 {
				mediahandlers.HideMediaSourceSizeForInfuse(c, sources)
				d.MediaSources = sources
				d.MediaStreams = sources[0].MediaStreams
			}
		}

		items = append(items, d)
	}
	ApplySeasonNames(ctx, state.DB, items)
	c.JSON(http.StatusOK, gin.H{"Items": embysupport.BaseItemsToEmbyMaps(items), "TotalRecordCount": EmbyTotalRecordCount(c, totalCount)})
}
