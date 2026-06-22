package library

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	embysupport "fyms/internal/handlers/mediasupport"
	"fyms/internal/handlers/shared"
	"fyms/internal/repository"
	"fyms/internal/source"
)

func sourceViews(c *gin.Context, state *AppState) ([]gin.H, error) {
	views, err := state.Repo.Source.ListExposedLibraryViews(c.Request.Context())
	if err != nil {
		return nil, err
	}
	out := make([]gin.H, 0, len(views))
	for i := range views {
		count, _ := state.Repo.Source.CountItemsForLibraryView(c.Request.Context(), views[i])
		out = append(out, sourceViewDTO(state, views[i], count))
	}
	return out, nil
}

func handleSourceItems(c *gin.Context, state *AppState, parentID string, userID string, scope *shared.UserLibraryScope) bool {
	ctx := c.Request.Context()
	resolved, err := source.ResolveEntity(ctx, state.DB, parentID)
	if err != nil || resolved == nil {
		return false
	}
	if scope != nil && !scope.AllowAll && (resolved.Kind == source.EntityKindSourceView || resolved.Kind == source.EntityKindSourceItem) {
		c.JSON(http.StatusOK, gin.H{"Items": []interface{}{}, "TotalRecordCount": 0})
		return true
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
		limit, offset := sourcePaging(c)
		items, total, err := state.Repo.Source.ListItemsForLibraryView(ctx, *view, repository.SourceItemListOptions{
			Limit:        limit,
			Offset:       offset,
			SearchTerm:   strings.TrimSpace(queryAny(c, "SearchTerm", "searchTerm", "searchterm")),
			IncludeTypes: sourceIncludeTypes(queryAny(c, "IncludeItemTypes", "includeItemTypes", "includeitemtypes")),
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return true
		}
		out := make([]gin.H, 0, len(items))
		for i := range items {
			ud, _ := state.Repo.Source.GetUserItemData(ctx, userID, items[i].ID)
			out = append(out, embysupport.BaseItemToEmbyMap(sourceItemDTO(state, items[i], ud)))
		}
		c.JSON(http.StatusOK, gin.H{"Items": out, "TotalRecordCount": shared.EmbyTotalRecordCount(c, total)})
		return true
	case source.EntityKindSourceItem:
		item, err := state.Repo.Source.GetSourceItemByID(ctx, resolved.SourceItemID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return true
		}
		if item == nil || sourceItemType(item.ItemType) != "Series" {
			c.JSON(http.StatusOK, gin.H{"Items": []interface{}{}, "TotalRecordCount": 0})
			return true
		}
		ensureSourceItemDetail(c, state, item.ID)
		episodes, err := state.Repo.Source.ListEpisodesForSeries(ctx, item.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return true
		}
		out := make([]gin.H, 0, len(episodes))
		for i := range episodes {
			ud, _ := state.Repo.Source.GetUserItemData(ctx, userID, episodes[i].SourceItemID)
			out = append(out, embysupport.BaseItemToEmbyMap(sourceEpisodeDTO(state, episodes[i], ud)))
		}
		c.JSON(http.StatusOK, gin.H{"Items": out, "TotalRecordCount": shared.EmbyTotalRecordCount(c, int64(len(out)))})
		return true
	case source.EntityKindSourceEpisode:
		c.JSON(http.StatusOK, gin.H{"Items": []interface{}{}, "TotalRecordCount": 0})
		return true
	default:
		return false
	}
}

func handleSourceItemDetail(c *gin.Context, state *AppState, itemID string, userID string, scope *shared.UserLibraryScope) bool {
	ctx := c.Request.Context()
	resolved, err := source.ResolveEntity(ctx, state.DB, itemID)
	if err != nil || resolved == nil {
		return false
	}
	if scope != nil && !scope.AllowAll && (resolved.Kind == source.EntityKindSourceView || resolved.Kind == source.EntityKindSourceItem || resolved.Kind == source.EntityKindSourceEpisode) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return true
	}
	switch resolved.Kind {
	case source.EntityKindSourceView:
		view, err := state.Repo.Source.GetLibraryViewByID(ctx, resolved.SourceViewID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return true
		}
		if view == nil || !view.Enabled || !view.ExposeToEmby {
			c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
			return true
		}
		count, _ := state.Repo.Source.CountItemsForLibraryView(ctx, *view)
		c.JSON(http.StatusOK, sourceViewDTO(state, *view, count))
		return true
	case source.EntityKindSourceItem:
		item, err := state.Repo.Source.GetSourceItemByID(ctx, resolved.SourceItemID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return true
		}
		if item == nil {
			c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
			return true
		}
		if loaded := ensureSourceItemDetail(c, state, item.ID); loaded != nil && loaded.Item != nil {
			item = loaded.Item
		}
		ud, _ := state.Repo.Source.GetUserItemData(ctx, userID, item.ID)
		c.JSON(http.StatusOK, embysupport.BaseItemToEmbyMap(sourceItemDTO(state, *item, ud)))
		return true
	case source.EntityKindSourceEpisode:
		episode, err := state.Repo.Source.GetEpisodeForSeries(ctx, resolved.SourceItemID, resolved.EpisodeKey)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return true
		}
		if episode == nil {
			c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
			return true
		}
		ud, _ := state.Repo.Source.GetUserItemData(ctx, userID, episode.SourceItemID)
		c.JSON(http.StatusOK, embysupport.BaseItemToEmbyMap(sourceEpisodeDTO(state, *episode, ud)))
		return true
	default:
		return false
	}
}

func ensureSourceItemDetail(c *gin.Context, state *AppState, sourceItemID int64) *source.EnsureDetailResult {
	result, err := source.EnsureItemDetailLoaded(c.Request.Context(), state.Repo.Source, state.HTTPClient, state.JSRuntime, state.CSPRuntime, sourceItemID)
	if err != nil {
		slog.Warn("[Source] ensure detail failed",
			"log_target", "source",
			"action", "ensure_detail",
			"source_item_id", sourceItemID,
			"error_type", source.ErrorType(err),
			"error", err)
		return result
	}
	return result
}

func sourcePaging(c *gin.Context) (int64, int64) {
	limit := int64(100)
	offset := int64(0)
	if s := strings.TrimSpace(queryAny(c, "Limit", "limit")); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			limit = n
		}
	}
	if s := strings.TrimSpace(queryAny(c, "StartIndex", "startIndex", "startindex")); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

func sourceIncludeTypes(raw string) []string {
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
