package compat

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	embysupport "fyms/internal/handlers/mediasupport"
	"fyms/internal/middleware"
	"fyms/internal/repository"
	"fyms/internal/source"
)

func handleSourceCompatItems(c *gin.Context, state *AppState, parentID string, recursive bool) bool {
	ctx := c.Request.Context()
	resolved, err := source.ResolveEntity(ctx, state.DB, parentID)
	if err != nil || resolved == nil {
		return false
	}
	auth := middleware.GetAuthUser(c)
	userID := ""
	if auth != nil && !strings.HasPrefix(auth.ID, "api-key-") {
		userID = auth.ID
	}
	switch resolved.Kind {
	case source.EntityKindSourceView:
		view, err := state.Repo.Source.GetLibraryViewByID(ctx, resolved.SourceViewID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return true
		}
		if view == nil || !view.Enabled || !view.ExposeToEmby {
			c.JSON(http.StatusOK, gin.H{"Items": []interface{}{}, "TotalRecordCount": 0})
			return true
		}
		limit, offset := compatSourcePaging(c)
		items, total, err := state.Repo.Source.ListItemsForLibraryView(ctx, *view, repository.SourceItemListOptions{
			Limit:        limit,
			Offset:       offset,
			SearchTerm:   strings.TrimSpace(compatQueryAny(c, "SearchTerm", "searchTerm", "searchterm")),
			IncludeTypes: compatSourceIncludeTypes(compatQueryAny(c, "IncludeItemTypes", "includeItemTypes", "includeitemtypes")),
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return true
		}
		out := make([]gin.H, 0, len(items))
		for i := range items {
			data, _ := state.Repo.Source.GetUserItemData(ctx, userID, items[i].ID)
			out = append(out, compatSourceItemDTO(state, items[i], data))
		}
		c.JSON(http.StatusOK, gin.H{"Items": out, "TotalRecordCount": total})
		return true
	case source.EntityKindSourceItem:
		if !recursive {
			return false
		}
		item, err := state.Repo.Source.GetSourceItemByID(ctx, resolved.SourceItemID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return true
		}
		if item == nil || compatSourceItemType(item.ItemType) != "Series" {
			c.JSON(http.StatusOK, gin.H{"Items": []interface{}{}, "TotalRecordCount": 0})
			return true
		}
		episodes, err := state.Repo.Source.ListEpisodesForSeries(ctx, item.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return true
		}
		out := make([]gin.H, 0, len(episodes))
		for i := range episodes {
			data, _ := state.Repo.Source.GetUserItemData(ctx, userID, episodes[i].SourceItemID)
			out = append(out, compatSourceEpisodeDTO(state, episodes[i], data))
		}
		c.JSON(http.StatusOK, gin.H{"Items": out, "TotalRecordCount": len(out)})
		return true
	case source.EntityKindSourceEpisode:
		c.JSON(http.StatusOK, gin.H{"Items": []interface{}{}, "TotalRecordCount": 0})
		return true
	default:
		return false
	}
}

func compatSourceItemDTO(state *AppState, item repository.SourceItem, data *repository.SourceUserItemData) gin.H {
	id := item.PublicUUID
	name := item.Title
	sortName := name
	if item.SortTitle != nil && strings.TrimSpace(*item.SortTitle) != "" {
		sortName = strings.TrimSpace(*item.SortTitle)
	}
	itemType := compatSourceItemType(item.ItemType)
	isPlayable := itemType == "Movie" || itemType == "Episode"
	out := gin.H{
		"Id":                    id,
		"Name":                  name,
		"ServerId":              state.Config.ServerID,
		"Etag":                  id,
		"Type":                  itemType,
		"IsFolder":              !isPlayable,
		"SortName":              sortName,
		"ForcedSortName":        sortName,
		"PresentationUniqueKey": id,
		"DisplayPreferencesId":  id,
		"LocationType":          "Virtual",
		"CanDelete":             false,
		"CanDownload":           isPlayable,
		"SupportsSync":          true,
		"ImageTags":             compatSourceImageTags(item.ProviderID, item.PublicUUID, item.PosterURL),
		"BackdropImageTags":     compatSourceBackdropTags(item.ProviderID, item.PublicUUID, item.BackdropURL),
		"ProviderIds":           compatSourceProviderIDs(item.ProviderIDs),
		"UserData":              compatSourceUserData(data),
		"DateCreated":           compatSourceTime(item.CreatedAt),
		"DateModified":          compatSourceTime(item.UpdatedAt),
	}
	if isPlayable {
		out["MediaType"] = "Video"
		out["PlayAccess"] = "Full"
	}
	if item.Summary != nil && strings.TrimSpace(*item.Summary) != "" {
		out["Overview"] = *item.Summary
	}
	if item.Year != nil {
		out["ProductionYear"] = *item.Year
	}
	if item.OriginalTitle != nil && strings.TrimSpace(*item.OriginalTitle) != "" {
		out["OriginalTitle"] = *item.OriginalTitle
	}
	embysupport.ApplyBaseItemEmbyDefaults(out)
	return out
}

func compatSourceEpisodeDTO(state *AppState, ep repository.SourceEpisode, data *repository.SourceUserItemData) gin.H {
	id := source.EpisodePublicUUID(ep.SourceItemUUID, ep.EpisodeKey)
	name := strings.TrimSpace(ep.EpisodeTitle)
	if name == "" {
		name = ep.EpisodeKey
	}
	out := gin.H{
		"Id":                    id,
		"Name":                  name,
		"ServerId":              state.Config.ServerID,
		"Etag":                  id,
		"Type":                  "Episode",
		"MediaType":             "Video",
		"IsFolder":              false,
		"SortName":              name,
		"ForcedSortName":        name,
		"PresentationUniqueKey": id,
		"DisplayPreferencesId":  id,
		"LocationType":          "Virtual",
		"CanDelete":             false,
		"CanDownload":           true,
		"SupportsSync":          true,
		"PlayAccess":            "Full",
		"ParentId":              ep.SourceItemUUID,
		"SeriesId":              ep.SourceItemUUID,
		"SeriesName":            ep.SeriesTitle,
		"IndexNumber":           ep.EpisodeNumber,
		"ImageTags":             compatSourceImageTags(ep.ProviderID, ep.SourceItemUUID, ep.PosterURL),
		"BackdropImageTags":     compatSourceBackdropTags(ep.ProviderID, ep.SourceItemUUID, ep.BackdropURL),
		"ProviderIds":           gin.H{},
		"UserData":              compatSourceUserData(data),
		"DateCreated":           compatSourceTime(ep.FirstSeenAt),
		"DateModified":          compatSourceTime(ep.FirstSeenAt),
	}
	if ep.SeriesSummary != nil && strings.TrimSpace(*ep.SeriesSummary) != "" {
		out["Overview"] = *ep.SeriesSummary
	}
	embysupport.ApplyBaseItemEmbyDefaults(out)
	return out
}

func compatSourcePaging(c *gin.Context) (int64, int64) {
	limit := int64(100)
	offset := int64(0)
	if s := strings.TrimSpace(compatQueryAny(c, "Limit", "limit")); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			limit = n
		}
	}
	if s := strings.TrimSpace(compatQueryAny(c, "StartIndex", "startIndex", "startindex")); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

func compatSourceIncludeTypes(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "movie":
			out = append(out, "Movie")
		case "series":
			out = append(out, "Series")
		}
	}
	return out
}

func compatSourceItemType(itemType string) string {
	switch strings.ToLower(strings.TrimSpace(itemType)) {
	case "series":
		return "Series"
	case "episode":
		return "Episode"
	case "folder":
		return "Folder"
	default:
		return "Movie"
	}
}

func compatSourceImageTags(providerID int64, publicUUID string, url *string) gin.H {
	if url == nil || strings.TrimSpace(*url) == "" {
		return gin.H{}
	}
	return gin.H{"Primary": compatSourceImageTag(providerID, publicUUID, *url)}
}

func compatSourceBackdropTags(providerID int64, publicUUID string, url *string) []string {
	if url == nil || strings.TrimSpace(*url) == "" {
		return []string{}
	}
	return []string{compatSourceImageTag(providerID, publicUUID, *url)}
}

func compatSourceImageTag(providerID int64, publicUUID, imageURL string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(imageURL)))
	return fmt.Sprintf("source-%d-%s-%x", providerID, publicUUID, sum[:8])
}

func compatSourceProviderIDs(raw []byte) gin.H {
	if len(raw) == 0 || !json.Valid(raw) {
		return gin.H{}
	}
	var out gin.H
	if err := json.Unmarshal(raw, &out); err != nil {
		return gin.H{}
	}
	return out
}

func compatSourceUserData(data *repository.SourceUserItemData) gin.H {
	if data == nil {
		return gin.H{"PlaybackPositionTicks": 0, "PlayCount": 0, "IsFavorite": false, "Played": false}
	}
	out := gin.H{
		"PlaybackPositionTicks": data.PlaybackPositionTicks,
		"PlayCount":             data.PlayCount,
		"IsFavorite":            data.IsFavorite,
		"Played":                data.Played,
	}
	if data.LastPlayedDate != nil {
		out["LastPlayedDate"] = compatSourceTime(*data.LastPlayedDate)
	}
	return out
}

func compatSourceTime(t time.Time) string {
	if t.IsZero() {
		t = time.Unix(0, 0)
	}
	return t.UTC().Format("2006-01-02T15:04:05.0000000") + "Z"
}
