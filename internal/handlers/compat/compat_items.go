package compat

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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

func getPersons(c *gin.Context, state *AppState) {
	start := int64(0)
	// 对齐 Emby：未显式传 Limit 时返回全部 person，不做默认分页。
	// gfriends-inputer 等外部头像工具依赖一次性拿到全量演职人员来判断谁缺头像；
	// 旧实现默认 50 会让它们只看到前 50 个（按名排序），误判“没有需要下载的头像”。
	limit := int64(0)
	if v := c.Query("StartIndex"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			start = n
		}
	}
	if v := c.Query("Limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			limit = n
		}
	}

	search := strings.TrimSpace(c.Query("SearchTerm"))
	nameStartsWith := strings.TrimSpace(c.Query("NameStartsWith"))
	filters := parseCSVQuery(c.Query("Filters"))
	userID := shared.ResolveUserID(c)
	favoriteOnly := hasCSVValue(filters, "IsFavorite")
	if favoriteOnly {
		if _, err := uuid.Parse(strings.TrimSpace(userID)); err != nil {
			c.JSON(http.StatusOK, gin.H{"Items": []gin.H{}, "TotalRecordCount": 0})
			return
		}
	}
	persons, total, err := models.ListPersons(c.Request.Context(), state.DB, models.PersonListOptions{
		Search:         search,
		NameStartsWith: nameStartsWith,
		UserID:         userID,
		Filters:        filters,
		Limit:          limit,
		Offset:         start,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	items := make([]gin.H, 0, len(persons))
	personIDs := make([]string, 0, len(persons))
	for i := range persons {
		personIDs = append(personIDs, persons[i].ID)
	}
	favoriteMap := map[string]bool{}
	if userID != "" {
		if m, merr := models.GetUserPersonFavoriteMap(c.Request.Context(), state.DB, userID, personIDs); merr == nil {
			favoriteMap = m
		}
	}
	for i := range persons {
		var ud *dto.UserDataRow
		if favoriteOnly {
			ud = models.PersonUserDataRow(true)
		} else if fav, ok := favoriteMap[persons[i].ID]; ok {
			ud = models.PersonUserDataRow(fav)
		}
		items = append(items, personItemDTO(state, &persons[i], ud))
	}
	c.JSON(http.StatusOK, gin.H{"Items": items, "TotalRecordCount": total})
}

func parseCSVQuery(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func hasCSVValue(values []string, want string) bool {
	for _, v := range values {
		if strings.EqualFold(strings.TrimSpace(v), want) {
			return true
		}
	}
	return false
}

// getPersonByName 对齐 Emby `GET /Persons/{Name}`（Items-by-Name 单演员详情）。
// 第三方刮削工具（mdc-ng 等）先用它拿到演员详情/Id，再回传头像；缺这个路由会报“未找到详情页”。
func getPersonByName(c *gin.Context, state *AppState) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusNotFound, gin.H{"message": "Not found"})
		return
	}
	p, err := models.GetPersonByName(c.Request.Context(), state.DB, name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if p == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Not found"})
		return
	}
	userID := shared.ResolveUserID(c)
	var ud *dto.UserDataRow
	if userID != "" {
		if u, uerr := models.GetUserPersonData(c.Request.Context(), state.DB, userID, p.ID); uerr == nil {
			ud = u
		}
	}
	c.JSON(http.StatusOK, PersonDetailDTO(state, p, ud))
}

// personDetailDTO 严格镜像真实 Emby `GET /Persons/{Name}` 的返回字段集（依据官方
// Emby 服务器实测样本对齐，不多不少）。第三方 Rust 客户端（mdc-ng）的演员详情结构体
// 按真实 Emby 建模并 deny_unknown_fields——多出 Emby 不返回的字段（IsFolder /
// LocationType / Overview / PrimaryImageTag 等）会触发 "error decoding response body"，
// 故此处刻意不复用更宽松的 personItemDTO，也不附加任何 Emby 不返回的字段。
func PersonDetailDTO(state *AppState, p *models.Person, userData *dto.UserDataRow) gin.H {
	ts := embyTimestampFromEpoch(p.ImageTag)
	etag := p.ImageTag
	if etag == "" {
		etag = p.ID
	}
	providerIDs := personProviderIDMap(p)
	item := gin.H{
		"Name":                  p.Name,
		"ServerId":              state.Config.ServerID,
		"Id":                    p.ID,
		"Etag":                  etag,
		"DateCreated":           ts,
		"DateModified":          ts,
		"CanDelete":             false,
		"CanDownload":           false,
		"PresentationUniqueKey": p.ID,
		"SortName":              p.Name,
		"ForcedSortName":        p.Name,
		"ExternalUrls":          personExternalUrls(providerIDs),
		"ProductionLocations":   strOrEmpty(p.ProductionLocations),
		"Taglines":              strOrEmpty(p.Taglines),
		"RemoteTrailers":        []gin.H{},
		"ProviderIds":           providerIDs,
		"Type":                  "Person",
		"DisplayPreferencesId":  p.ID,
		"ImageTags":             gin.H{},
		"BackdropImageTags":     []string{},
		"LockedFields":          []string{},
		"LockData":              false,
		"UserData":              personUserDataDTO(userData),
	}
	if p.Overview != nil && *p.Overview != "" {
		item["Overview"] = *p.Overview
	}
	if pd := personPremiereDate(p); pd != "" {
		item["PremiereDate"] = pd
	}
	if p.ProductionYear != nil {
		item["ProductionYear"] = *p.ProductionYear
	}
	// Genres/Tags 仅在有值时输出：空时与真实 Emby 详情一致(不返回该键，规避客户端
	// deny_unknown 风险)；有值时如实暴露给其它客户端(三围/身高/罩杯等存在 Tags 里)。
	if len(p.Genres) > 0 {
		item["Genres"] = p.Genres
	}
	if len(p.Tags) > 0 {
		item["Tags"] = p.Tags
	}
	if p.ImagePath != nil && *p.ImagePath != "" {
		tag := imageTagOr(p, p.ImageTag)
		item["ImageTags"] = gin.H{"Primary": tag}
		item["PrimaryImageAspectRatio"] = 0.6666666666666666
	}
	if p.BackdropPath != nil && *p.BackdropPath != "" {
		item["BackdropImageTags"] = []string{imageTagOr(p, p.ImageTag)}
	}
	return item
}

// personProviderIDMap 合并完整外部 id 映射 + Tmdb 兜底(键 "Tmdb",Emby 习惯)。
func personProviderIDMap(p *models.Person) map[string]string {
	out := map[string]string{}
	for k, v := range p.ProviderIDs {
		out[k] = v
	}
	if p.TmdbPersonID != nil {
		if _, ok := out["Tmdb"]; !ok {
			out["Tmdb"] = strconv.FormatInt(int64(*p.TmdbPersonID), 10)
		}
	}
	return out
}

// personExternalUrls 依据外部 id 生成 Emby 风格 ExternalUrls(IMDb / TheMovieDb)。
func personExternalUrls(ids map[string]string) []gin.H {
	out := []gin.H{}
	get := func(want string) string {
		for k, v := range ids {
			if strings.EqualFold(k, want) && strings.TrimSpace(v) != "" {
				return v
			}
		}
		return ""
	}
	if v := get("Imdb"); v != "" {
		out = append(out, gin.H{"Name": "IMDb", "Url": "https://www.imdb.com/name/" + v})
	}
	if v := get("Tmdb"); v != "" {
		out = append(out, gin.H{"Name": "TheMovieDb", "Url": "https://www.themoviedb.org/person/" + v})
	}
	return out
}

// personPremiereDate 把存储的 "YYYY-MM-DD"(或已含 T 的串)转 Emby 时间串;空则 ""。
func personPremiereDate(p *models.Person) string {
	if p.PremiereDate == nil {
		return ""
	}
	s := strings.TrimSpace(*p.PremiereDate)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "T") {
		return s
	}
	return s + "T00:00:00.0000000Z"
}

// strOrEmpty 把可能为 nil 的切片渲染成 JSON 数组([] 而非 null)。
func strOrEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// imageTagFallback 取图片 tag(updated_at epoch),为空时退回 person id。
func imageTagOr(p *models.Person, tag string) string {
	if tag != "" {
		return tag
	}
	return p.ID
}

func personUserDataDTO(userData *dto.UserDataRow) gin.H {
	isFavorite := false
	if userData != nil && userData.IsFavorite != nil {
		isFavorite = *userData.IsFavorite
	}
	return gin.H{
		"PlaybackPositionTicks": 0,
		"PlayCount":             0,
		"IsFavorite":            isFavorite,
		"Played":                false,
	}
}

// embyTimestampFromEpoch 把 Unix 秒 epoch 字符串格式化成 Emby 的时间串（7 位小数 + Z）。
// 用于 DateCreated / DateModified —— mdc-ng 会按 DateTime 解析，必须是合法格式。
func embyTimestampFromEpoch(epoch string) string {
	n, err := strconv.ParseInt(strings.TrimSpace(epoch), 10, 64)
	if err != nil || n <= 0 {
		return "2020-01-01T00:00:00.0000000Z"
	}
	return time.Unix(n, 0).UTC().Format("2006-01-02T15:04:05.0000000") + "Z"
}

// personItemDTO 把全局 person 渲染成 Emby `/Persons` 列表项(对齐真实 Emby:Name/
// ServerId/Id/DateCreated/Type/UserData/ImageTags/BackdropImageTags,Overview 在有值时附带)。
// 仅当 person 实际有头像时才带 ImageTags.Primary —— 客户端据此判断谁缺头像。
func personItemDTO(state *AppState, p *models.Person, userData *dto.UserDataRow) gin.H {
	item := gin.H{
		"Name":              p.Name,
		"ServerId":          state.Config.ServerID,
		"Id":                p.ID,
		"DateCreated":       embyTimestampFromEpoch(p.ImageTag),
		"Type":              "Person",
		"ImageTags":         gin.H{},
		"BackdropImageTags": []string{},
		"ProviderIds":       personProviderIDMap(p),
		"UserData":          personUserDataDTO(userData),
	}
	if p.ImagePath != nil && *p.ImagePath != "" {
		item["ImageTags"] = gin.H{"Primary": imageTagOr(p, p.ImageTag)}
		item["PrimaryImageAspectRatio"] = 0.6666666666666666
	}
	if p.BackdropPath != nil && *p.BackdropPath != "" {
		item["BackdropImageTags"] = []string{imageTagOr(p, p.ImageTag)}
	}
	if p.Overview != nil && *p.Overview != "" {
		item["Overview"] = *p.Overview
	}
	return item
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
			if scope != nil && !scope.AllowAll {
				c.JSON(http.StatusOK, gin.H{"Items": []interface{}{}, "TotalRecordCount": 0})
				return
			}
			parentMode := repository.CompatItemsParentPlatformStudio
			switch p.Dimension {
			case models.PlatformDimActor:
				parentMode = repository.CompatItemsParentPlatformActor
			case models.PlatformDimNumPrefix:
				parentMode = repository.CompatItemsParentPlatformNumPrefix
			}
			searchOpts.Parent = &repository.CompatItemsParentFilter{
				Mode:  parentMode,
				Value: p.MatchValue,
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
				sources := mediahandlers.BuildItemMediaSources(ctx, state, itemID, &row)
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
	c.JSON(http.StatusOK, gin.H{"Items": items, "TotalRecordCount": result.Total})
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
	c.JSON(http.StatusOK, gin.H{"SearchHints": hints, "TotalRecordCount": result.Total})
}

// hideMediaSourceSizeForInfuse 暂不隐藏 MediaSource.Size,用于验证 Infuse 在
