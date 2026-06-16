package handlers

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"fyms/internal/dto"
	"fyms/internal/models"
)

func getUserViews(c *gin.Context) {
	state := GetState(c)
	userID := resolveUserID(c)
	if !matchUserOrAdmin(c, userID) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}

	ctx := c.Request.Context()
	scope, err := loadUserLibraryScope(ctx, state, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	cacheKey := "views:" + userID
	var cached map[string]interface{}
	if state.Cache.GetJSON(ctx, cacheKey, &cached) {
		c.JSON(http.StatusOK, cached)
		return
	}

	libs, err := models.GetAllLibraries(ctx, state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	sid := state.Config.ServerID

	// Read display settings
	platformPosition := state.Repo.SystemConfig.GetStringOrDefault(ctx, "platform_libraries_position", "")
	showItemCountStr := state.Repo.SystemConfig.GetStringOrDefault(ctx, "library_show_item_count", "")
	platformBefore := platformPosition == "before"
	showItemCount := showItemCountStr != "false"

	// Platform virtual libraries
	var platformEntries []gin.H
	if scope.AllowAll && models.IsPlatformLibrariesEnabled(ctx, state.DB) {
		platforms, _ := models.GetEnabledPlatforms(ctx, state.DB)
		for _, p := range platforms {
			if p.ItemCount == 0 {
				continue
			}
			vid := models.PlatformVirtualID(p.Dimension, p.MatchValue)
			colType := models.PlatformCollectionType(ctx, state.DB, p.Dimension, p.Values())
			imgTags := gin.H{}
			// 有生成封面、或是已知平台(内置 logo)时才挂 Primary。
			// 生成封面用 CoverImageTag 作为 Primary tag——它每次换图都会刷新,
			// 客户端据此感知图变;若仍用恒定的 vid,换封面后 tag 不变,客户端读缓存不更新。
			// 内置 logo 无 tag,退回 vid 保持稳定。
			if p.CoverImagePath != nil && *p.CoverImagePath != "" {
				if p.CoverImageTag != nil && *p.CoverImageTag != "" {
					imgTags["Primary"] = *p.CoverImageTag
				} else {
					imgTags["Primary"] = vid
				}
			} else if models.HasPlatformLogo(p.PlatformName) {
				imgTags["Primary"] = vid
			}
			var unplayedCount interface{}
			if showItemCount {
				unplayedCount = p.ItemCount
			} else {
				unplayedCount = 0
			}
			entry := gin.H{
				"Name":               p.EffectiveDisplayName(),
				"ServerId":           sid,
				"Id":                 vid,
				"Etag":               vid,
				"Type":               "CollectionFolder",
				"IsFolder":           true,
				"ChildCount":         p.ItemCount,
				"RecursiveItemCount": p.ItemCount,
				"SortName":           fmt.Sprintf("%04d", p.SortOrder),
				"ImageTags":          imgTags,
				"BackdropImageTags":  []string{},
				"PlatformLibrary":    true,
				"UserData": gin.H{
					"PlaybackPositionTicks": 0,
					"PlayCount":             0,
					"IsFavorite":            false,
					"Played":                false,
					"UnplayedItemCount":     unplayedCount,
				},
			}
			// 混合库(colType 为空)省略 CollectionType, 客户端才会同时显示电影和剧集
			if colType != "" {
				entry["CollectionType"] = colType
			}
			platformEntries = append(platformEntries, entry)
		}
	}

	libEntries := make([]gin.H, 0, len(libs))
	for _, lib := range libs {
		idStr := lib.ID.String()
		if !scope.allowsLibrary(idStr) {
			continue
		}

		var childCount int64
		childCount, _ = models.GetLibraryDisplayItemCount(ctx, state.DB, idStr)
		var recursiveCount int64
		state.DB.QueryRow(ctx,
			"SELECT COUNT(*) FROM items WHERE library_id = $1", lib.ID).Scan(&recursiveCount)

		imageTags := gin.H{}
		if lib.PrimaryImageTag != nil {
			imageTags["Primary"] = *lib.PrimaryImageTag
		}

		var unplayedCount interface{}
		if showItemCount {
			unplayedCount = childCount
		} else {
			unplayedCount = 0
		}

		entry := gin.H{
			"Name":               lib.Name,
			"ServerId":           sid,
			"Id":                 idStr,
			"Etag":               idStr,
			"Type":               "CollectionFolder",
			"CollectionType":     lib.CollectionType,
			"IsFolder":           true,
			"ChildCount":         childCount,
			"RecursiveItemCount": recursiveCount,
			"SortName":           strings.ToLower(lib.Name),
			"DateCreated":        lib.CreatedAt.UTC().Format(time.RFC3339),
			"ImageTags":          imageTags,
			"BackdropImageTags":  []string{},
			"UserData": gin.H{
				"PlaybackPositionTicks": 0,
				"PlayCount":             0,
				"IsFavorite":            false,
				"Played":                false,
				"UnplayedItemCount":     unplayedCount,
			},
		}
		if lib.CollectionType == "mixed" {
			entry["CanDelete"] = false
			entry["CanDownload"] = false
			entry["PresentationUniqueKey"] = idStr
			entry["DisplayPreferencesId"] = idStr
			entry["ForcedSortName"] = lib.Name
			entry["ProviderIds"] = gin.H{}
			entry["ExternalUrls"] = []interface{}{}
			entry["Taglines"] = []interface{}{}
			entry["RemoteTrailers"] = []interface{}{}
			entry["LockedFields"] = []interface{}{}
			entry["LockData"] = false
			delete(entry, "CollectionType")
			delete(entry, "ChildCount")
			delete(entry, "RecursiveItemCount")
			if ud, ok := entry["UserData"].(gin.H); ok {
				delete(ud, "PlayCount")
				delete(ud, "UnplayedItemCount")
			}
		} else if len(lib.Paths) > 0 {
			entry["Path"] = lib.Paths[0]
		}
		libEntries = append(libEntries, entry)
	}

	// 统一展示顺序:有 library_display_order 记录时,实际库与虚拟库按其交错排序;
	// 否则回退 platform_libraries_position(before/after)。
	order, _ := models.GetDisplayOrder(ctx, state.DB)
	out := make([]gin.H, 0, len(libEntries)+len(platformEntries))
	if len(order) > 0 {
		out = append(out, platformEntries...)
		out = append(out, libEntries...)
		sort.SliceStable(out, func(i, j int) bool {
			oi, iok := order[fmt.Sprint(out[i]["Id"])]
			oj, jok := order[fmt.Sprint(out[j]["Id"])]
			if iok && jok {
				return oi < oj
			}
			// 已排序条目排在未排序(新加)条目前面;两者皆未排序时保持稳定原序。
			return iok && !jok
		})
	} else if platformBefore {
		out = append(out, platformEntries...)
		out = append(out, libEntries...)
	} else {
		out = append(out, libEntries...)
		out = append(out, platformEntries...)
	}

	resp := gin.H{
		"Items":            out,
		"TotalRecordCount": len(out),
	}
	state.Cache.SetJSON(ctx, cacheKey, resp, 60*time.Second)
	c.JSON(http.StatusOK, resp)
}

// queryAny returns the first non-empty query parameter value among the given keys.
func queryAny(c *gin.Context, keys ...string) string {
	for _, k := range keys {
		if v := c.Query(k); v != "" {
			return v
		}
	}
	return ""
}

func parseCompatBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "y":
		return true, nil
	case "false", "0", "no", "n":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value %q", value)
	}
}

// itemTypeCanonical 把客户端传入的各种大小写形式映射到 FYMS 数据库 items.type
// 的标准值。Lenna 等客户端会传 "movie" 全小写,SQL 精确匹配 i.type='Movie'
// 时查不到任何记录导致媒体库为空。
var itemTypeCanonical = map[string]string{
	"movie":            "Movie",
	"series":           "Series",
	"episode":          "Episode",
	"season":           "Season",
	"boxset":           "BoxSet",
	"playlist":         "Playlist",
	"musicvideo":       "MusicVideo",
	"video":            "Video",
	"audio":            "Audio",
	"folder":           "Folder",
	"collectionfolder": "CollectionFolder",
	"userview":         "UserView",
	"musicalbum":       "MusicAlbum",
	"musicartist":      "MusicArtist",
}

func normalizeItemType(s string) string {
	if v, ok := itemTypeCanonical[strings.ToLower(strings.TrimSpace(s))]; ok {
		return v
	}
	return s
}

func parseItemQueryOptions(c *gin.Context, userID string) (*models.ItemQueryOptions, error) {
	opts := &models.ItemQueryOptions{}

	if pid := strings.TrimSpace(queryAny(c, "ParentId", "parentId", "parentid")); pid != "" {
		opts.ParentID = &pid
	}
	if s := strings.TrimSpace(queryAny(c, "ParentIds", "parentIds", "parentids")); s != "" {
		for _, id := range strings.Split(s, ",") {
			if id = strings.TrimSpace(id); id != "" {
				opts.ParentIDs = append(opts.ParentIDs, id)
			}
		}
	}
	if s := strings.TrimSpace(queryAny(c, "IncludeItemTypes", "includeItemTypes", "includeitemtypes")); s != "" {
		for _, t := range strings.Split(s, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				opts.IncludeItemTypes = append(opts.IncludeItemTypes, normalizeItemType(t))
			}
		}
	}
	if s := strings.TrimSpace(queryAny(c, "SortBy", "sortBy", "sortby")); s != "" {
		opts.SortBy = &s
	}
	if s := strings.TrimSpace(queryAny(c, "SortOrder", "sortOrder", "sortorder")); s != "" {
		opts.SortOrder = &s
	}
	if s := strings.TrimSpace(queryAny(c, "Limit", "limit")); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, err
		}
		opts.Limit = &n
	}
	if s := strings.TrimSpace(queryAny(c, "StartIndex", "startIndex", "startindex")); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, err
		}
		opts.StartIndex = &n
	}
	recStr := queryAny(c, "Recursive", "recursive")
	recursive := strings.EqualFold(recStr, "true") || recStr == "1"
	opts.Recursive = recursive

	if s := strings.TrimSpace(queryAny(c, "HasSubtitles", "hasSubtitles", "hassubtitles")); s != "" {
		v, err := parseCompatBool(s)
		if err != nil {
			return nil, err
		}
		opts.HasSubtitles = &v
	}

	if s := strings.TrimSpace(queryAny(c, "SearchTerm", "searchTerm", "searchterm")); s != "" {
		opts.SearchTerm = &s
	}
	if s := strings.TrimSpace(queryAny(c, "NameStartsWith", "nameStartsWith", "namestartswith")); s != "" {
		opts.NameStartsWith = &s
	}
	if s := strings.TrimSpace(queryAny(c, "Filters", "filters")); s != "" {
		for _, f := range strings.Split(s, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				opts.Filters = append(opts.Filters, f)
			}
		}
	}
	if s := strings.TrimSpace(queryAny(c, "GenreIds", "genreIds", "genreids")); s != "" {
		for _, g := range strings.Split(s, ",") {
			g = strings.TrimSpace(g)
			if g != "" {
				opts.GenreIDs = append(opts.GenreIDs, g)
			}
		}
	}
	if s := strings.TrimSpace(queryAny(c, "Genres", "genres")); s != "" {
		for _, g := range strings.Split(s, ",") {
			g = strings.TrimSpace(g)
			if g != "" {
				opts.GenreNames = append(opts.GenreNames, g)
			}
		}
	}
	if s := strings.TrimSpace(queryAny(c, "TagIds", "tagIds", "tagids")); s != "" {
		for _, raw := range strings.Split(s, ",") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			n, err := strconv.Atoi(raw)
			if err != nil {
				return nil, err
			}
			opts.TagIDs = append(opts.TagIDs, n)
		}
	}
	if s := strings.TrimSpace(queryAny(c, "Tags", "tags")); s != "" {
		for _, tag := range strings.Split(s, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				opts.TagNames = append(opts.TagNames, tag)
			}
		}
	}
	if s := strings.TrimSpace(queryAny(c, "PersonIds", "personIds", "personids")); s != "" {
		for _, id := range strings.Split(s, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				opts.PersonIDs = append(opts.PersonIDs, id)
			}
		}
	}
	if s := strings.TrimSpace(queryAny(c, "Person", "Persons", "person", "persons")); s != "" {
		for _, name := range strings.Split(s, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				opts.PersonNames = append(opts.PersonNames, name)
			}
		}
	}
	if s := strings.TrimSpace(queryAny(c, "PersonTypes", "personTypes", "persontypes")); s != "" {
		for _, typ := range strings.Split(s, ",") {
			typ = strings.TrimSpace(typ)
			if typ != "" {
				opts.PersonTypes = append(opts.PersonTypes, typ)
			}
		}
	}
	if s := strings.TrimSpace(queryAny(c, "Years", "years")); s != "" {
		for _, y := range strings.Split(s, ",") {
			y = strings.TrimSpace(y)
			if y == "" {
				continue
			}
			n, err := strconv.Atoi(y)
			if err != nil {
				return nil, err
			}
			opts.Years = append(opts.Years, n)
		}
	}

	// AnyProviderIdEquals=tmdb.755898 —— 聚合类客户端用外部站点 ID 跨源匹配同一影片。
	// 支持 ; 或 , 分隔多个,每个按第一个 "." 拆成 provider 与 id;provider 名小写化。
	if s := strings.TrimSpace(queryAny(c, "AnyProviderIdEquals", "anyProviderIdEquals", "anyprovideridequals")); s != "" {
		for _, raw := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ',' }) {
			raw = strings.TrimSpace(raw)
			dot := strings.Index(raw, ".")
			if dot <= 0 || dot >= len(raw)-1 {
				continue // 缺少 provider 或 id,跳过
			}
			provider := strings.ToLower(strings.TrimSpace(raw[:dot]))
			id := strings.TrimSpace(raw[dot+1:])
			if provider != "" && id != "" {
				opts.AnyProviderID = append(opts.AnyProviderID, models.ProviderIDMatch{Provider: provider, ID: id})
			}
		}
	}

	opts.UserID = &userID
	return opts, nil
}

func getItems(c *gin.Context) {
	state := GetState(c)
	pathUser := resolveUserID(c)
	if !matchUserOrAdmin(c, pathUser) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}

	ctx := c.Request.Context()

	opts, err := parseItemQueryOptions(c, pathUser)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	scope, err := loadUserLibraryScope(ctx, state, pathUser)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	applyLibraryScope(opts, scope)

	// 大批量分页列表：跳过 series_fallback JOIN 提升性能
	if opts.Recursive && opts.ParentID == nil && opts.UserID == nil && len(opts.GenreIDs) == 0 {
		opts.LightMode = true
	}

	// Handle platform virtual library (UUID-based lookup)
	if opts.ParentID != nil {
		if p, ok := models.ResolvePlatformVirtualID(ctx, state.DB, *opts.ParentID); ok {
			if !scope.AllowAll {
				c.JSON(http.StatusOK, gin.H{"Items": []interface{}{}, "TotalRecordCount": 0})
				return
			}
			opts.ParentID = nil
			applyVirtualDimension(opts, p)
			if len(opts.IncludeItemTypes) == 0 {
				opts.IncludeItemTypes = []string{"Movie", "Series"}
			}
			opts.Recursive = true
		} else {
			if empty, err := resolvePhysicalParentForItems(ctx, state, scope, opts); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			} else if empty {
				c.JSON(http.StatusOK, gin.H{"Items": []interface{}{}, "TotalRecordCount": 0})
				return
			}
		}
	}
	res, err := models.QueryItems(ctx, state.DB, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	sid := state.Config.ServerID
	needMediaSources := strings.Contains(c.Query("Fields"), "MediaSources") || strings.Contains(c.Query("Fields"), "Path")
	items := make([]dto.BaseItemDto, 0, len(res.Items))
	for i := range res.Items {
		var ud *dto.UserDataRow
		if i < len(res.UserData) {
			ud = &res.UserData[i]
		}
		item := dto.FormatItemDtoList(&res.Items[i], sid, ud)
		if needMediaSources {
			applyListMediaSourceDisplay(c, ctx, state, &res.Items[i], &item)
		}
		items = append(items, item)
	}
	applyUnplayedItemCounts(ctx, state.DB, pathUser, items)
	applySeasonNames(ctx, state.DB, items)

	c.JSON(http.StatusOK, gin.H{
		"Items":            items,
		"TotalRecordCount": embyTotalRecordCount(c, res.TotalCount),
	})
}

func resolvePhysicalParentForItems(ctx context.Context, state *AppState, scope *userLibraryScope, opts *models.ItemQueryOptions) (bool, error) {
	if opts == nil || opts.ParentID == nil {
		return false, nil
	}
	parentID := strings.TrimSpace(*opts.ParentID)
	if parentID == "" {
		return false, nil
	}

	if uid, err := uuid.Parse(parentID); err == nil {
		if lib, lerr := models.GetLibraryByID(ctx, state.DB, uid); lerr != nil {
			return false, lerr
		} else if lib != nil {
			if scope != nil && !scope.allowsLibrary(parentID) {
				return true, nil
			}
			opts.ParentLibraryID = &parentID
			opts.ParentID = nil
			return false, nil
		}
	}

	resolved, err := models.ResolveToUUID(ctx, state.DB, parentID)
	if err != nil {
		return false, err
	}
	if resolved == nil {
		return true, nil
	}
	item, err := models.GetItemByID(ctx, state.DB, *resolved)
	if err != nil {
		return false, err
	}
	if item == nil {
		return true, nil
	}
	if opts.Recursive {
		opts.RecursiveParentID = &item.ID
		opts.ParentID = nil
	}
	return false, nil
}

func getResumeItems(c *gin.Context) {
	state := GetState(c)
	pathUser := resolveUserID(c)
	if !matchUserOrAdmin(c, pathUser) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}

	opts, err := parseItemQueryOptions(c, pathUser)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	opts.Filters = append([]string{"IsResumable"}, opts.Filters...)
	if len(opts.IncludeItemTypes) == 0 {
		opts.IncludeItemTypes = []string{"Movie", "Episode"}
	}
	if opts.SortBy == nil {
		sb := "DatePlayed"
		opts.SortBy = &sb
	}
	if opts.SortOrder == nil {
		so := "Descending"
		opts.SortOrder = &so
	}
	if opts.Limit == nil {
		lim := int64(12)
		opts.Limit = &lim
	}

	ctx := c.Request.Context()
	scope, err := loadUserLibraryScope(ctx, state, pathUser)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	applyLibraryScope(opts, scope)

	res, err := models.QueryItems(ctx, state.DB, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	sid := state.Config.ServerID
	items := make([]dto.BaseItemDto, 0, len(res.Items))
	for i := range res.Items {
		var ud *dto.UserDataRow
		if i < len(res.UserData) {
			ud = &res.UserData[i]
		}
		items = append(items, dto.FormatItemDto(&res.Items[i], sid, ud))
	}

	c.JSON(http.StatusOK, gin.H{
		"Items":            items,
		"TotalRecordCount": embyTotalRecordCount(c, res.TotalCount),
	})
}

func getLatestItems(c *gin.Context) {
	state := GetState(c)
	pathUser := resolveUserID(c)
	if !matchUserOrAdmin(c, pathUser) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}

	parentID := strings.TrimSpace(c.Query("ParentId"))
	if parentID == "" {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}
	limit := int64(20)
	if s := strings.TrimSpace(c.Query("Limit")); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		limit = n
	}

	ctx := c.Request.Context()
	scope, err := loadUserLibraryScope(ctx, state, pathUser)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if _, ok := models.ResolvePlatformVirtualID(ctx, state.DB, parentID); ok {
		if !scope.AllowAll {
			c.JSON(http.StatusOK, []interface{}{})
			return
		}
	} else if !scope.allowsLibrary(parentID) {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}

	needMediaSources := strings.Contains(c.Query("Fields"), "MediaSources") || strings.Contains(c.Query("Fields"), "Path")
	if needMediaSources {
		if _, ok := models.ResolvePlatformVirtualID(ctx, state.DB, parentID); !ok {
			rows, err := models.GetLatestItems(ctx, state.DB, parentID, limit)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
			sid := state.Config.ServerID
			items := make([]dto.BaseItemDto, 0, len(rows))
			for i := range rows {
				item := dto.FormatItemDtoList(&rows[i], sid, nil)
				applyListMediaSourceDisplay(c, ctx, state, &rows[i], &item)
				items = append(items, item)
			}
			applyUnplayedItemCounts(ctx, state.DB, pathUser, items)
			c.JSON(http.StatusOK, items)
			return
		}
	}

	items, err := queryLatestItemsForParent(ctx, state, parentID, limit, scope, pathUser, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, items)
}

func queryLatestItemsForParent(ctx context.Context, state *AppState, parentID string, limit int64, scope *userLibraryScope, userID string, platformByID map[string]models.PlatformLibrary) ([]dto.BaseItemDto, error) {
	var platform *models.PlatformLibrary
	if p, ok := platformByID[parentID]; ok {
		platform = &p
	} else if platformByID == nil {
		if p, ok := models.ResolvePlatformVirtualID(ctx, state.DB, parentID); ok {
			platform = p
		}
	}
	if platform != nil {
		if scope != nil && !scope.AllowAll {
			return []dto.BaseItemDto{}, nil
		}
		opts := &models.ItemQueryOptions{
			IncludeItemTypes: []string{"Movie", "Series"},
			Limit:            &limit,
			Recursive:        true,
		}
		applyVirtualDimension(opts, platform)
		applyLibraryScope(opts, scope)
		sb := "DateCreated"
		so := "Descending"
		opts.SortBy = &sb
		opts.SortOrder = &so
		res, err := models.QueryItems(ctx, state.DB, opts)
		if err != nil {
			return nil, err
		}
		sid := state.Config.ServerID
		items := make([]dto.BaseItemDto, 0, len(res.Items))
		for i := range res.Items {
			items = append(items, dto.FormatItemDtoList(&res.Items[i], sid, nil))
		}
		applyUnplayedItemCounts(ctx, state.DB, userID, items)
		return items, nil
	}

	if scope != nil && !scope.allowsLibrary(parentID) {
		return []dto.BaseItemDto{}, nil
	}
	rows, err := models.GetLatestItems(ctx, state.DB, parentID, limit)
	if err != nil {
		return nil, err
	}
	sid := state.Config.ServerID
	items := make([]dto.BaseItemDto, 0, len(rows))
	for i := range rows {
		items = append(items, dto.FormatItemDtoList(&rows[i], sid, nil))
	}
	applyUnplayedItemCounts(ctx, state.DB, userID, items)
	return items, nil
}

func loadPlatformVirtualIDMap(ctx context.Context, state *AppState) map[string]models.PlatformLibrary {
	platformByID := make(map[string]models.PlatformLibrary)
	if !models.IsPlatformLibrariesEnabled(ctx, state.DB) {
		return platformByID
	}
	platforms, err := models.GetEnabledPlatformsLite(ctx, state.DB)
	if err != nil || len(platforms) == 0 {
		return platformByID
	}
	for i := range platforms {
		platformByID[models.PlatformVirtualID(platforms[i].Dimension, platforms[i].MatchValue)] = platforms[i]
	}
	return platformByID
}

func getLatestBatch(c *gin.Context) {
	state := GetState(c)
	pathUser := resolveUserID(c)
	if !matchUserOrAdmin(c, pathUser) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}

	libIDsRaw := strings.TrimSpace(c.Query("LibraryIds"))
	if libIDsRaw == "" {
		libIDsRaw = strings.TrimSpace(c.Query("libraryIds"))
	}
	if libIDsRaw == "" {
		c.JSON(http.StatusOK, gin.H{})
		return
	}

	limit := int64(16)
	if s := strings.TrimSpace(c.Query("Limit")); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err == nil && n > 0 {
			limit = n
		}
	}

	ctx := c.Request.Context()
	scope, err := loadUserLibraryScope(ctx, state, pathUser)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	result := make(map[string][]dto.BaseItemDto)
	platformByID := loadPlatformVirtualIDMap(ctx, state)

	for _, rawID := range strings.Split(libIDsRaw, ",") {
		libID := strings.TrimSpace(rawID)
		if libID == "" {
			continue
		}
		items, err := queryLatestItemsForParent(ctx, state, libID, limit, scope, pathUser, platformByID)
		if err != nil {
			continue
		}
		result[libID] = items
	}

	c.JSON(http.StatusOK, result)
}
