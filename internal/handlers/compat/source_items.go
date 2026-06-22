package compat

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"fyms/internal/dto"
	embysupport "fyms/internal/handlers/mediasupport"
	"fyms/internal/handlers/shared"
	"fyms/internal/middleware"
	"fyms/internal/repository"
	"fyms/internal/source"
)

const sourceEmbySearchEnabledKey = "source_emby_search_enabled"

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
		ensureCompatSourceItemDetail(c, state, item.ID)
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

func ensureCompatSourceItemDetail(c *gin.Context, state *AppState, sourceItemID int64) *source.EnsureDetailResult {
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

func shouldAppendSourceSearchResults(c *gin.Context, state *AppState, searchTerm, parentID, ids string) bool {
	if strings.TrimSpace(searchTerm) == "" || strings.TrimSpace(parentID) != "" || strings.TrimSpace(ids) != "" {
		return false
	}
	if state == nil || state.Repo == nil || state.Repo.SystemConfig == nil {
		return false
	}
	return state.Repo.SystemConfig.GetBoolOrDefault(c.Request.Context(), sourceEmbySearchEnabledKey, true)
}

func sourceSearchAllowedForAuth(c *gin.Context, state *AppState) (bool, error) {
	auth := middleware.GetAuthUser(c)
	if auth == nil {
		return false, nil
	}
	if strings.HasPrefix(auth.ID, "api-key-") || auth.IsAdmin {
		return true, nil
	}
	scope, err := shared.LoadUserLibraryScope(c.Request.Context(), state, auth.ID)
	if err != nil {
		return false, err
	}
	return scope == nil || scope.AllowAll, nil
}

func appendCompatSourceSearchItems(c *gin.Context, state *AppState, items []gin.H, total int64, searchTerm, parentID, ids, includeTypes string, limit, offset int64) ([]gin.H, int64, error) {
	if !shouldAppendSourceSearchResults(c, state, searchTerm, parentID, ids) {
		return items, total, nil
	}
	if ok, err := sourceSearchAllowedForAuth(c, state); err != nil {
		return items, total, err
	} else if !ok {
		return items, total, nil
	}
	sourceLimit := limit
	if sourceLimit <= 0 {
		sourceLimit = 20
	}
	if sourceLimit > 50 {
		sourceLimit = 50
	}
	rows, sourceTotal, err := state.Repo.Source.SearchSourceItems(c.Request.Context(), repository.SourceItemSearchOptions{
		SearchTerm:   searchTerm,
		IncludeTypes: compatSourceIncludeTypes(includeTypes),
		Limit:        sourceLimit,
		Offset:       offset,
	})
	if err != nil {
		return items, total, err
	}
	for i := range rows {
		items = append(items, compatSourceItemDTO(state, rows[i], nil))
	}
	return items, total + sourceTotal, nil
}

func AppendSourceSearchDTOs(c *gin.Context, state *AppState, items []dto.BaseItemDto, total int64, searchTerm, parentID string, includeTypes []string, limit, offset int64) ([]dto.BaseItemDto, int64, error) {
	if !shouldAppendSourceSearchResults(c, state, searchTerm, parentID, "") {
		return items, total, nil
	}
	if ok, err := sourceSearchAllowedForAuth(c, state); err != nil {
		return items, total, err
	} else if !ok {
		return items, total, nil
	}
	sourceLimit := limit
	if sourceLimit <= 0 {
		sourceLimit = 20
	}
	if sourceLimit > 50 {
		sourceLimit = 50
	}
	rows, sourceTotal, err := state.Repo.Source.SearchSourceItems(c.Request.Context(), repository.SourceItemSearchOptions{
		SearchTerm:   searchTerm,
		IncludeTypes: includeTypes,
		Limit:        sourceLimit,
		Offset:       offset,
	})
	if err != nil {
		return items, total, err
	}
	for i := range rows {
		items = append(items, sourceItemDTOForCompatSearch(state, rows[i]))
	}
	return items, total + sourceTotal, nil
}

func AppendCompatSourceSearchItems(c *gin.Context, state *AppState, items []gin.H, total int64, searchTerm, parentID, ids, includeTypes string, limit, offset int64) ([]gin.H, int64, error) {
	return appendCompatSourceSearchItems(c, state, items, total, searchTerm, parentID, ids, includeTypes, limit, offset)
}

func sourceItemDTOForCompatSearch(state *AppState, item repository.SourceItem) dto.BaseItemDto {
	id := item.PublicUUID
	itemType := compatSourceItemType(item.ItemType)
	isFolder := itemType == "Series" || itemType == "Folder"
	canDownload := !isFolder
	canDelete := false
	supportsSync := true
	lockData := false
	locationType := "Virtual"
	mediaType := "Video"
	playAccess := "Full"
	sortName := item.Title
	if item.SortTitle != nil && strings.TrimSpace(*item.SortTitle) != "" {
		sortName = strings.TrimSpace(*item.SortTitle)
	}
	dateCreated := compatSourceTime(item.CreatedAt)
	out := dto.BaseItemDto{
		ID:                    id,
		Name:                  item.Title,
		ServerID:              state.Config.ServerID,
		Type:                  itemType,
		IsFolder:              &isFolder,
		CanDelete:             &canDelete,
		CanDownload:           &canDownload,
		SupportsSync:          &supportsSync,
		SortName:              &sortName,
		ForcedSortName:        &sortName,
		PresentationUniqueKey: &id,
		DisplayPreferencesID:  &id,
		Overview:              item.Summary,
		ProductionYear:        item.Year,
		ImageTags:             sourceSearchImageTags(item),
		BackdropImageTags:     sourceSearchBackdropTags(item),
		ProviderIDs:           sourceSearchProviderIDs(item.ProviderIDs),
		ExternalURLs:          []dto.ExternalUrl{},
		RemoteTrailers:        []dto.MediaUrl{},
		LockedFields:          []string{},
		LockData:              &lockData,
		LocationType:          &locationType,
		DateCreated:           &dateCreated,
		DateModified:          &dateCreated,
		UserData:              &dto.UserItemDataDto{PlaybackPositionTicks: 0, PlayCount: 0, IsFavorite: false, Played: false},
	}
	if !isFolder {
		out.MediaType = &mediaType
		out.PlayAccess = &playAccess
	}
	return out
}

func sourceSearchImageTags(item repository.SourceItem) map[string]string {
	if item.PosterURL == nil || strings.TrimSpace(*item.PosterURL) == "" {
		return map[string]string{}
	}
	return map[string]string{"Primary": compatSourceImageTag(item.ProviderID, item.PublicUUID, *item.PosterURL)}
}

func sourceSearchBackdropTags(item repository.SourceItem) []string {
	if item.BackdropURL == nil || strings.TrimSpace(*item.BackdropURL) == "" {
		return []string{}
	}
	return []string{compatSourceImageTag(item.ProviderID, item.PublicUUID, *item.BackdropURL)}
}

func sourceSearchProviderIDs(raw []byte) *json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		msg := json.RawMessage(`{}`)
		return &msg
	}
	msg := json.RawMessage(append([]byte(nil), raw...))
	return &msg
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
