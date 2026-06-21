package library

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"fyms/internal/dto"
	embysupport "fyms/internal/handlers/mediasupport"
	"fyms/internal/repository"
	"fyms/internal/source"
)

func sourceViewDisplayName(view repository.SourceLibraryView) string {
	if view.DisplayName != nil && strings.TrimSpace(*view.DisplayName) != "" {
		return strings.TrimSpace(*view.DisplayName)
	}
	return view.Name
}

func sourceViewDTO(state *AppState, view repository.SourceLibraryView, count int64) gin.H {
	id := view.PublicUUID
	name := sourceViewDisplayName(view)
	collectionType := sourceViewCollectionType(view.CollectionType)
	entry := gin.H{
		"Name":               name,
		"ServerId":           state.Config.ServerID,
		"Id":                 id,
		"Etag":               id,
		"Type":               "CollectionFolder",
		"IsFolder":           true,
		"ChildCount":         count,
		"RecursiveItemCount": count,
		"SortName":           sourceSortName(view.SortOrder, name),
		"DateCreated":        embyTime(view.CreatedAt),
		"ImageTags":          gin.H{},
		"BackdropImageTags":  []string{},
		"SourceLibraryView":  true,
		"UserData": gin.H{
			"PlaybackPositionTicks": 0,
			"PlayCount":             0,
			"IsFavorite":            false,
			"Played":                false,
			"UnplayedItemCount":     count,
		},
	}
	if collectionType != "" {
		entry["CollectionType"] = collectionType
	}
	return embysupport.ApplyCollectionFolderDefaults(entry, collectionType, collectionType != "")
}

func sourceItemDTO(state *AppState, item repository.SourceItem, userData *repository.SourceUserItemData) dto.BaseItemDto {
	itemType := sourceItemType(item.ItemType)
	sortName := item.Title
	if item.SortTitle != nil && strings.TrimSpace(*item.SortTitle) != "" {
		sortName = strings.TrimSpace(*item.SortTitle)
	}
	isFolder := itemType == "Series" || itemType == "Folder"
	canDownload := !isFolder
	canDelete := false
	supportsSync := true
	lockData := false
	locationType := "Virtual"
	mediaType := "Video"
	playAccess := "Full"
	dateCreated := embyTime(item.CreatedAt)
	out := dto.BaseItemDto{
		ID:                  item.PublicUUID,
		Name:                item.Title,
		ServerID:            state.Config.ServerID,
		Type:                itemType,
		IsFolder:            &isFolder,
		CanDelete:           &canDelete,
		CanDownload:         &canDownload,
		SupportsSync:        &supportsSync,
		SortName:            &sortName,
		ForcedSortName:      &sortName,
		PresentationUniqueKey: &item.PublicUUID,
		DisplayPreferencesID:  &item.PublicUUID,
		Overview:              item.Summary,
		ProductionYear:        item.Year,
		IndexNumber:           item.EpisodeNumber,
		ParentIndexNumber:     item.SeasonNumber,
		ImageTags:             sourceImageTags(item),
		BackdropImageTags:     sourceBackdropTags(item),
		ProviderIDs:           sourceProviderIDs(item.ProviderIDs),
		ExternalURLs:          []dto.ExternalUrl{},
		RemoteTrailers:        []dto.MediaUrl{},
		LockedFields:          []string{},
		LockData:              &lockData,
		LocationType:          &locationType,
		DateCreated:           &dateCreated,
		DateModified:          &dateCreated,
		UserData:              sourceUserDataDTO(userData),
	}
	if !isFolder {
		out.MediaType = &mediaType
		out.PlayAccess = &playAccess
	}
	if item.OriginalTitle != nil && strings.TrimSpace(*item.OriginalTitle) != "" {
		out.OriginalTitle = item.OriginalTitle
	}
	if item.Region != nil && strings.TrimSpace(*item.Region) != "" {
		out.ProductionLocations = []string{*item.Region}
	}
	if item.CategoryName != nil && strings.TrimSpace(*item.CategoryName) != "" {
		out.Genres = []string{*item.CategoryName}
	}
	return out
}

func sourceViewCollectionType(collectionType string) string {
	collectionType = strings.TrimSpace(collectionType)
	if collectionType == "" || strings.EqualFold(collectionType, "mixed") {
		return ""
	}
	return collectionType
}

func sourceEpisodeDTO(state *AppState, ep repository.SourceEpisode, userData *repository.SourceUserItemData) dto.BaseItemDto {
	id := source.EpisodePublicUUID(ep.SourceItemUUID, ep.EpisodeKey)
	name := strings.TrimSpace(ep.EpisodeTitle)
	if name == "" {
		name = ep.EpisodeKey
	}
	sortName := name
	isFolder := false
	canDownload := true
	canDelete := false
	supportsSync := true
	lockData := false
	locationType := "Virtual"
	mediaType := "Video"
	playAccess := "Full"
	dateCreated := embyTime(ep.FirstSeenAt)
	return dto.BaseItemDto{
		ID:                    id,
		Name:                  name,
		ServerID:              state.Config.ServerID,
		Type:                  "Episode",
		MediaType:             &mediaType,
		IsFolder:              &isFolder,
		CanDelete:             &canDelete,
		CanDownload:           &canDownload,
		SupportsSync:          &supportsSync,
		SortName:              &sortName,
		ForcedSortName:        &sortName,
		PresentationUniqueKey: &id,
		DisplayPreferencesID:  &id,
		Overview:              ep.SeriesSummary,
		IndexNumber:           ep.EpisodeNumber,
		ParentID:              &ep.SourceItemUUID,
		SeriesID:              &ep.SourceItemUUID,
		SeriesName:            &ep.SeriesTitle,
		ImageTags:             sourceEpisodeImageTags(ep),
		BackdropImageTags:     sourceEpisodeBackdropTags(ep),
		ProviderIDs:           sourceProviderIDs(nil),
		ExternalURLs:          []dto.ExternalUrl{},
		RemoteTrailers:        []dto.MediaUrl{},
		LockedFields:          []string{},
		LockData:              &lockData,
		LocationType:          &locationType,
		PlayAccess:            &playAccess,
		DateCreated:           &dateCreated,
		DateModified:          &dateCreated,
		UserData:              sourceUserDataDTO(userData),
		ParentPrimaryImageItemID: &ep.SourceItemUUID,
		ParentThumbItemID:        &ep.SourceItemUUID,
	}
}

func sourceUserDataDTO(data *repository.SourceUserItemData) *dto.UserItemDataDto {
	if data == nil {
		return &dto.UserItemDataDto{PlaybackPositionTicks: 0, PlayCount: 0, IsFavorite: false, Played: false}
	}
	var lastPlayed *string
	if data.LastPlayedDate != nil {
		s := embyTime(*data.LastPlayedDate)
		lastPlayed = &s
	}
	return &dto.UserItemDataDto{
		PlaybackPositionTicks: data.PlaybackPositionTicks,
		PlayCount:             data.PlayCount,
		IsFavorite:            data.IsFavorite,
		Played:                data.Played,
		LastPlayedDate:        lastPlayed,
	}
}

func sourceItemType(itemType string) string {
	switch strings.ToLower(strings.TrimSpace(itemType)) {
	case "movie":
		return "Movie"
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

func sourceImageTags(item repository.SourceItem) map[string]string {
	if item.PosterURL == nil || strings.TrimSpace(*item.PosterURL) == "" {
		return map[string]string{}
	}
	return map[string]string{"Primary": sourceImageTag(item.ProviderID, item.PublicUUID, *item.PosterURL)}
}

func sourceBackdropTags(item repository.SourceItem) []string {
	if item.BackdropURL == nil || strings.TrimSpace(*item.BackdropURL) == "" {
		return []string{}
	}
	return []string{sourceImageTag(item.ProviderID, item.PublicUUID, *item.BackdropURL)}
}

func sourceEpisodeImageTags(ep repository.SourceEpisode) map[string]string {
	if ep.PosterURL == nil || strings.TrimSpace(*ep.PosterURL) == "" {
		return map[string]string{}
	}
	return map[string]string{"Primary": sourceImageTag(ep.ProviderID, ep.SourceItemUUID, *ep.PosterURL)}
}

func sourceEpisodeBackdropTags(ep repository.SourceEpisode) []string {
	if ep.BackdropURL == nil || strings.TrimSpace(*ep.BackdropURL) == "" {
		return []string{}
	}
	return []string{sourceImageTag(ep.ProviderID, ep.SourceItemUUID, *ep.BackdropURL)}
}

func sourceProviderIDs(raw []byte) *json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		msg := json.RawMessage(`{}`)
		return &msg
	}
	msg := json.RawMessage(append([]byte(nil), raw...))
	return &msg
}

func sourceSortName(order int32, name string) string {
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(time.Unix(int64(order), 0).UTC().Format("150405")+" "+strings.ToLower(name)), "0"))
}

func embyTime(t time.Time) string {
	if t.IsZero() {
		t = time.Unix(0, 0)
	}
	return t.UTC().Format("2006-01-02T15:04:05.0000000") + "Z"
}
