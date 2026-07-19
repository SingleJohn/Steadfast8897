package compat

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"fyms/internal/dto"
	mediahandlers "fyms/internal/handlers/media"
	embysupport "fyms/internal/handlers/mediasupport"
	"fyms/internal/handlers/shared"
	"fyms/internal/middleware"
	"fyms/internal/models"
	"fyms/internal/repository"
	"fyms/internal/services"
)

func getItemCounts(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	opts := repository.CompatItemCountOptions{}
	if auth := middleware.GetAuthUser(c); auth != nil && !auth.IsAdmin && !strings.HasPrefix(auth.ID, "api-key-") {
		scope, err := shared.LoadUserLibraryScope(ctx, state, auth.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		if !scope.AllowAll {
			opts.RestrictLibraries = true
			opts.AllowedLibraryIDs = scope.IDs
		}
	}
	counts, err := repository.NewCompatItemsRepository(state.DB).CountItemTypes(ctx, []string{"Movie", "Series", "Episode"}, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	movie := counts["Movie"]
	series := counts["Series"]
	episodes := counts["Episode"]
	c.JSON(http.StatusOK, gin.H{
		"MovieCount":      movie,
		"SeriesCount":     series,
		"EpisodeCount":    episodes,
		"ArtistCount":     0,
		"ProgramCount":    0,
		"TrailerCount":    0,
		"SongCount":       0,
		"AlbumCount":      0,
		"MusicVideoCount": 0,
		"BoxSetCount":     0,
		"BookCount":       0,
		"ItemCount":       movie + series + episodes,
	})
}

func deviceInfo(c *gin.Context, state *AppState) {
	deviceID := c.Query("Id")
	sessions := state.SessionManager.GetActiveSessions()
	var found *services.ActiveSession
	for i := range sessions {
		if sessions[i].DeviceID == deviceID {
			found = &sessions[i]
			break
		}
	}
	name := "Unknown"
	appName := "Unknown"
	userName := ""
	userID := ""
	lastActivity := ""
	if found != nil {
		name = found.DeviceName
		appName = found.AppName
		userName = found.UserName
		userID = found.UserID
		lastActivity = found.LastActivity.UTC().Format("2006-01-02T15:04:05.0000000Z")
	}
	c.JSON(http.StatusOK, gin.H{
		"Id":               deviceID,
		"Name":             name,
		"AppName":          appName,
		"LastUserName":     userName,
		"LastUserId":       userID,
		"DateLastActivity": lastActivity,
	})
}

func compatQueryAny(c *gin.Context, keys ...string) string {
	for _, k := range keys {
		if v := c.Query(k); v != "" {
			return v
		}
	}
	return ""
}

func compatItemsIncludeTypes(includeTypes string) []string {
	if strings.TrimSpace(includeTypes) == "" {
		return nil
	}
	validTypes := map[string]bool{"Movie": true, "Series": true, "Episode": true, "Season": true, "Folder": true}
	typeMap := map[string]string{"Video": "Movie"}
	seen := map[string]bool{}
	var out []string
	for _, t := range strings.Split(includeTypes, ",") {
		// 先按 itemTypeCanonical 规范化大小写,Lenna 等客户端会传 "movie" 小写,
		// 直接精确匹配 i.type='movie' 会查不到记录。
		t = shared.NormalizeItemType(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if mapped, ok := typeMap[t]; ok {
			t = mapped
		}
		if t == "Person" || t == "CollectionFolder" {
			continue
		}
		if !validTypes[t] || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

func compatSearchHintTypes(includeTypes string) []string {
	if strings.TrimSpace(includeTypes) == "" {
		return nil
	}
	var out []string
	for _, t := range strings.Split(includeTypes, ",") {
		// 规范化大小写, 与 parseItemQueryOptions 行为一致, Lenna 等客户端
		// 传 "movie" 小写时仍能命中 SQL 精确匹配 i.type='Movie'.
		t = shared.NormalizeItemType(strings.TrimSpace(t))
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func itemsSearch(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()

	ids := compatQueryAny(c, "Ids", "ids")
	searchTerm := compatQueryAny(c, "SearchTerm", "searchTerm", "searchterm")
	includeTypes := compatQueryAny(c, "IncludeItemTypes", "includeItemTypes", "includeitemtypes")
	fields := compatQueryAny(c, "Fields", "fields")
	parentID := compatQueryAny(c, "ParentId", "parentId", "parentid")
	recStr := compatQueryAny(c, "Recursive", "recursive")
	recursive := strings.EqualFold(recStr, "true") || recStr == "1"
	var hasSubtitles *bool
	if s := strings.TrimSpace(compatQueryAny(c, "HasSubtitles", "hasSubtitles", "hassubtitles")); s != "" {
		v, err := shared.ParseCompatBool(s)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		hasSubtitles = &v
	}
	limitStr := compatQueryAny(c, "Limit", "limit")
	limitVal := int64(50)
	if limitStr != "" {
		if n, err := strconv.ParseInt(limitStr, 10, 64); err == nil && n > 0 {
			limitVal = n
		}
	}
	startIndex := int64(0)
	if v := compatQueryAny(c, "StartIndex", "startIndex", "startindex"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			startIndex = n
		}
	}

	useEmbyID := false
	if ids != "" {
		parts := strings.Split(ids, ",")
		allInt := true
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, err := strconv.ParseInt(p, 10, 64); err != nil {
				allInt = false
				break
			}
		}
		useEmbyID = allInt
	}

	auth := middleware.GetAuthUser(c)
	var authUserID string
	if auth != nil && !strings.HasPrefix(auth.ID, "api-key-") {
		authUserID = auth.ID
	}
	var scope *shared.UserLibraryScope
	if authUserID != "" {
		var err error
		scope, err = shared.LoadUserLibraryScope(ctx, state, authUserID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}

	searchOpts := repository.CompatItemsSearchOptions{
		AuthUserID: authUserID,
		IDs:        ids,
		UseEmbyID:  useEmbyID,
		SearchTerm: searchTerm,
		Limit:      limitVal,
		Offset:     startIndex,
	}
	if parentID != "" {
		if handleSourceCompatItems(c, state, parentID, recursive) {
			return
		}
		if p, ok := models.ResolvePlatformVirtualID(ctx, state.DB, parentID); ok {
			if scope != nil && !scope.AllowAll && !p.IsLatest() {
				c.JSON(http.StatusOK, gin.H{"Items": []interface{}{}, "TotalRecordCount": 0})
				return
			}
			parentMode := repository.CompatItemsParentPlatformStudio
			switch p.Dimension {
			case models.PlatformDimLatest:
				parentMode = repository.CompatItemsParentPlatformLatest
			case models.PlatformDimActor:
				parentMode = repository.CompatItemsParentPlatformActor
			case models.PlatformDimNumPrefix:
				parentMode = repository.CompatItemsParentPlatformNumPrefix
			}
			searchOpts.Parent = &repository.CompatItemsParentFilter{
				Mode:      parentMode,
				Value:     p.MatchValue,
				Values:    p.Values(),
				ItemLimit: p.LatestLimit(),
			}
		} else {
			pid, _ := models.ResolveToUUID(ctx, state.DB, parentID)
			if pid != nil {
				mode := repository.CompatItemsParentLibrary
				if recursive {
					mode = repository.CompatItemsParentLibraryRecursive
				}
				searchOpts.Parent = &repository.CompatItemsParentFilter{Mode: mode, Value: *pid}
			}
		}
	}
	if scope != nil && !scope.AllowAll {
		searchOpts.RestrictLibraries = true
		searchOpts.AllowedLibraryIDs = scope.IDs
	}
	searchOpts.IncludeTypes = compatItemsIncludeTypes(includeTypes)
	searchOpts.HasSubtitles = hasSubtitles
	searchOpts.AnyProviderIDEquals = strings.TrimSpace(compatQueryAny(c, "AnyProviderIdEquals", "anyProviderIdEquals", "anyprovideridequals"))
	result, err := repository.NewCompatItemsRepository(state.DB).SearchItems(ctx, searchOpts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	needMediaSources := strings.Contains(fields, "MediaSources") || strings.Contains(fields, "Path")
	needPrimaryImageAspectRatio := strings.Contains(fields, "PrimaryImageAspectRatio")
	needGenres := strings.Contains(fields, "Genres")
	needPeople := strings.Contains(fields, "People")

	var items []gin.H
	for _, m := range result.Rows {
		row := models.MapColsToItemRow(m)
		itemID := row.ID
		if itemID == "" {
			continue
		}

		ud := models.MapColsToUserDataRow(m)
		var udPtr *dto.UserDataRow
		if ud.PlaybackPositionTicks != nil || ud.IsFavorite != nil {
			udPtr = &ud
		}

		d := dto.FormatItemDtoList(&row, state.Config.ServerID, udPtr)
		if needPrimaryImageAspectRatio {
			d = dto.FormatItemDtoListWithPrimaryImageAspectRatio(&row, state.Config.ServerID, udPtr)
		}
		result := dtoToMap(d)

		if embyID, ok := m["emby_id"]; ok && embyID != nil {
			result["EmbyId"] = embyID
			if useEmbyID {
				result["Id"] = fmt.Sprintf("%v", embyID)
			}
		}

		if row.ItemType == "Movie" || row.ItemType == "Episode" {
			if needMediaSources {
				sources := mediahandlers.BuildItemMediaSources(ctx, state, itemID, &row, authUserID)
				if len(sources) > 0 {
					mediahandlers.HideMediaSourceSizeForInfuse(c, sources)
					result["MediaSources"] = embysupport.MediaSourcesToEmbyMaps(sources)
					result["MediaStreams"] = sources[0].MediaStreams
					if strings.TrimSpace(sources[0].Path) != "" {
						result["Path"] = sources[0].Path
					}
					if strings.TrimSpace(sources[0].Container) != "" {
						result["Container"] = sources[0].Container
					}
				}
			}
			// Emby standard: MediaSourceCount tells clients how many versions exist.
			// Only set when > 1 (matches Jellyfin DtoService behavior).
			msc := models.GetMediaSourceCount(ctx, state.DB, itemID)
			if msc > 1 {
				result["MediaSourceCount"] = msc
			}
		}

		if needGenres {
			genres, _ := models.GetItemGenres(ctx, state.DB, itemID)
			genreNames := make([]string, 0, len(genres))
			for _, g := range genres {
				genreNames = append(genreNames, g[1])
			}
			result["Genres"] = genreNames
		}
		if needPeople {
			cast, _ := models.GetItemCast(ctx, state.DB, itemID)
			if cast != nil {
				result["People"] = cast
			} else {
				result["People"] = []interface{}{}
			}
		}

		items = append(items, result)
	}
	if items == nil {
		items = []gin.H{}
	}
	total := result.Total
	if nextItems, nextTotal, err := AppendCompatSourceSearchItems(c, state, items, total, searchTerm, parentID, ids, includeTypes, limitVal, startIndex); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	} else {
		items = nextItems
		total = nextTotal
	}
	c.JSON(http.StatusOK, gin.H{"Items": items, "TotalRecordCount": total})
}

func uuidToString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case [16]byte:
		u, err := uuid.FromBytes(t[:])
		if err != nil {
			return ""
		}
		return u.String()
	default:
		if s, ok := v.(interface{ String() string }); ok {
			return s.String()
		}
		return ""
	}
}

func dtoToMap(d dto.BaseItemDto) gin.H {
	return embysupport.BaseItemToEmbyMap(d)
}

func searchHints(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()

	searchTerm := compatQueryAny(c, "SearchTerm", "searchTerm", "searchterm")
	if searchTerm == "" {
		c.JSON(http.StatusOK, gin.H{"SearchHints": []interface{}{}, "TotalRecordCount": 0})
		return
	}

	limitVal := int64(20)
	if v := compatQueryAny(c, "Limit", "limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			limitVal = n
		}
	}
	startIndex := int64(0)
	if v := compatQueryAny(c, "StartIndex", "startIndex", "startindex"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			startIndex = n
		}
	}

	includeTypes := compatQueryAny(c, "IncludeItemTypes", "includeItemTypes", "includeitemtypes")

	result, err := repository.NewCompatItemsRepository(state.DB).SearchHints(ctx, repository.CompatSearchHintsOptions{
		SearchTerm:   searchTerm,
		IncludeTypes: compatSearchHintTypes(includeTypes),
		Limit:        limitVal,
		Offset:       startIndex,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	var hints []gin.H
	for _, row := range result.Rows {
		mediaType := "Video"
		hint := gin.H{
			"Id":        row.ID,
			"ItemId":    row.ID,
			"Name":      row.Name,
			"Type":      row.ItemType,
			"MediaType": mediaType,
			"ServerId":  state.Config.ServerID,
		}
		if row.ProductionYear != nil {
			hint["ProductionYear"] = *row.ProductionYear
		}
		if row.RuntimeTicks != nil {
			hint["RunTimeTicks"] = *row.RuntimeTicks
		}
		if row.PrimaryImageTag != nil {
			hint["PrimaryImageTag"] = *row.PrimaryImageTag
			hint["ThumbImageTag"] = *row.PrimaryImageTag
		} else if (row.ItemType == "Episode" || row.ItemType == "Season") && row.SeriesPrimaryImageTag != nil {
			hint["PrimaryImageTag"] = *row.SeriesPrimaryImageTag
			hint["ThumbImageTag"] = *row.SeriesPrimaryImageTag
			if row.SeriesFallbackID != nil {
				hint["PrimaryImageItemId"] = *row.SeriesFallbackID
				hint["ThumbImageItemId"] = *row.SeriesFallbackID
			}
		}
		if row.BackdropImageTag != nil {
			hint["BackdropImageTag"] = *row.BackdropImageTag
		} else if (row.ItemType == "Episode" || row.ItemType == "Season") && row.SeriesBackdropImageTag != nil {
			hint["BackdropImageTag"] = *row.SeriesBackdropImageTag
			if row.SeriesFallbackID != nil {
				hint["BackdropImageItemId"] = *row.SeriesFallbackID
			}
		}
		if row.SeriesName != nil {
			hint["Series"] = *row.SeriesName
		}
		if row.IndexNumber != nil {
			hint["IndexNumber"] = *row.IndexNumber
		}
		if row.ParentIndexNumber != nil {
			hint["ParentIndexNumber"] = *row.ParentIndexNumber
		}
		if row.CommunityRating != nil {
			hint["CommunityRating"] = *row.CommunityRating
		}

		isFolder := row.ItemType == "Series" || row.ItemType == "Season" || row.ItemType == "CollectionFolder" || row.ItemType == "Folder"
		hint["IsFolder"] = isFolder

		hints = append(hints, hint)
	}
	if hints == nil {
		hints = []gin.H{}
	}
	total := result.Total
	if shouldAppendSourceSearchResults(c, state, searchTerm, "", "") {
		// 与 /Items 搜索一致:读缓存前在预算内触发一次实时聚合,慢站后台回填(B1)。
		warmSourceSearchCache(c, state, searchTerm, limitVal)
		rows, _, err := state.Repo.Source.SearchSourceItems(ctx, repository.SourceItemSearchOptions{
			SearchTerm:   searchTerm,
			IncludeTypes: compatSourceIncludeTypes(includeTypes),
			Limit:        limitVal,
			Offset:       startIndex,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		rows, err = dedupeCompatSourceSearchHintRows(ctx, state, rows)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		for i := range rows {
			hints = append(hints, compatSourceSearchHint(state, rows[i]))
		}
		total += int64(len(rows))
	}
	c.JSON(http.StatusOK, gin.H{"SearchHints": hints, "TotalRecordCount": total})
}

// hideMediaSourceSizeForInfuse 暂不隐藏 MediaSource.Size,用于验证 Infuse 在
