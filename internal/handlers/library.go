package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
	"fyms/internal/middleware"
	"fyms/internal/models"
	"fyms/internal/services"
	"fyms/internal/services/coverart"
	"fyms/internal/services/taskcenter"

	"github.com/disintegration/imaging"
)

func RegisterLibraryRoutes(group *gin.RouterGroup, state *AppState, authMW, adminMW, optAuthMW gin.HandlerFunc) {
	u := group.Group("")
	u.GET("/Users/:userId/Views", authMW, getUserViews)
	u.GET("/Users/:userId/Items", authMW, getItems)
	u.GET("/Users/:userId/Items/Resume", authMW, getResumeItems)
	u.GET("/Users/:userId/Items/Latest", authMW, getLatestItems)
	u.GET("/Users/:userId/Items/:itemId", authMW, getItemDetail)

	// Forward 等客户端会省略 :userId 段直接请求 /Users/Views、/Users/Items 等;
	// 这里挂上无 userId 段的兼容路由,handler 通过 resolveUserID 从 token 取 userId。
	u.GET("/Users/Views", authMW, getUserViews)
	u.GET("/Users/Items", authMW, getItems)
	u.GET("/Users/Items/Resume", authMW, getResumeItems)
	u.GET("/Users/Items/Latest", authMW, getLatestItems)

	u.GET("/Items/:itemId/Similar", optAuthMW, getSimilarItems)

	u.POST("/Library/VirtualFolders", adminMW, addLibrary)
	u.DELETE("/Library/VirtualFolders", adminMW, deleteLibrary)
	u.POST("/Library/VirtualFolders/Name", adminMW, renameLibrary)
	u.POST("/Library/VirtualFolders/Paths", adminMW, addLibraryPath)
	u.DELETE("/Library/VirtualFolders/Paths", adminMW, removeLibraryPath)

	u.POST("/Items/:itemId/Images/:imageType", adminMW, uploadImage)
	u.DELETE("/Items/:itemId/Images/:imageType", adminMW, deleteImage)

	u.POST("/Library/Refresh", adminMW, refreshAll)
	u.POST("/Items/:itemId/Refresh", adminMW, refreshItem)

	u.GET("/Library/VirtualFolders", authMW, getVirtualFolders)
	u.GET("/Library/VirtualFolders/Query", authMW, getVirtualFolders)
	u.GET("/Library/VirtualFolders/:id", authMW, getVirtualFolderDetail)
	u.POST("/Library/VirtualFolders/Add", adminMW, addLibrary)
	u.POST("/Library/VirtualFolders/Update", adminMW, updateLibraryInfo)
	u.POST("/Library/VirtualFolders/:id/Refresh", adminMW, refreshSingleLibrary)
	u.POST("/Library/VirtualFolders/:id/Image", adminMW, uploadLibraryImage)
	u.POST("/Library/VirtualFolders/:id/ImageUrl", adminMW, setLibraryImageFromURL)
	u.POST("/Library/VirtualFolders/:id/Image/Generate", adminMW, generateLibraryCover)
	u.DELETE("/Library/VirtualFolders/:id/Image", adminMW, deleteLibraryImage)
	u.GET("/Library/CoverArt/Styles", authMW, listCoverArtStyles)
	u.POST("/Library/CoverArt/GenerateAll", adminMW, generateAllLibraryCovers)
	u.GET("/Library/Scan/Progress", getScanProgress)

	u.POST("/Library/Probe/Start", adminMW, startProbe)
	u.POST("/Library/Probe/Stop", adminMW, stopProbe)
	u.GET("/Library/Probe/Progress", getProbeProgress)

	u.POST("/Items/:itemId/Scrape", adminMW, scrapeItem)
	u.POST("/Items/:itemId/SearchTmdb", adminMW, searchTmdbForItem)
	u.POST("/Items/:itemId/ScrapeByTmdbId", adminMW, scrapeItemByTmdbId)
	u.GET("/Items/:itemId/IdentifyCandidates", adminMW, getIdentifyCandidates)
	u.POST("/Items/:itemId/IdentifyCandidates/:candidateId/Apply", adminMW, applyIdentifyCandidate)
	u.GET("/Library/Scrape/Unmatched", adminMW, listUnmatchedItems)
	u.POST("/Library/Scrape/Unmatched/Apply", adminMW, batchApplyIdentifyCandidates)
	u.POST("/Library/Scrape/All", adminMW, scrapeAll)
	u.POST("/Library/Scrape/Stop", adminMW, stopScrape)
	u.GET("/Library/Scrape/Progress", getScrapeProgress)
	u.GET("/Library/Scrape/Missing", getMissingScrapeCount)
	u.GET("/Library/Tasks/Summary", func(c *gin.Context) { getTaskSummary(c, state) })

	u.POST("/Library/MergeVersions", adminMW, func(c *gin.Context) { mergeVersions(c, state) })

	u.POST("/Library/Browse", adminMW, browseDir)
	u.GET("/Library/BrowseDirectories", adminMW, browseDirGet)

	u.POST("/Library/Refresh/Metadata", adminMW, refreshLibraryMetadata)

	// M7.Backfill: 存量数据回填(画质标签 / Episode 标题 / Episode 缩略图)
	u.POST("/Library/Backfill/Start", adminMW, func(c *gin.Context) { startBackfill(c, state) })
	u.POST("/Library/Backfill/Stop", adminMW, func(c *gin.Context) { stopBackfill(c, state) })
	u.GET("/Library/Backfill/Progress", adminMW, func(c *gin.Context) { getBackfillProgress(c, state) })
	u.GET("/Library/Backfill/Config", adminMW, func(c *gin.Context) { getBackfillConfig(c, state) })
	u.POST("/Library/Backfill/Config", adminMW, func(c *gin.Context) { updateBackfillConfig(c, state) })
	u.POST("/Library/Backfill/Reset/Quality", adminMW, func(c *gin.Context) { resetBackfillQuality(c, state) })
	u.POST("/Library/Backfill/Reset/EpisodeImage", adminMW, func(c *gin.Context) { resetBackfillEpisodeImage(c, state) })

	u.GET("/Users/:userId/Items/LatestBatch", authMW, getLatestBatch)
	u.GET("/Users/Items/LatestBatch", authMW, getLatestBatch)

	u.GET("/Genres", getGenres)

	// Library sort order
	u.POST("/Library/VirtualFolders/SortOrder", adminMW, func(c *gin.Context) { updateLibrarySortOrder(c, state) })
	// 统一展示顺序(实际库 + 虚拟库交错)
	u.POST("/Library/DisplayOrder", adminMW, func(c *gin.Context) { updateDisplayOrder(c, state) })

	// Platform libraries
	u.GET("/Library/Platforms", adminMW, func(c *gin.Context) { getPlatforms(c, state) })
	u.POST("/Library/Platforms", adminMW, func(c *gin.Context) { addPlatform(c, state) })
	// 多维度虚拟库:发现 distinct 值 / 批量添加 / 封面生成
	u.GET("/Library/Platforms/Discover", adminMW, func(c *gin.Context) { discoverPlatformDimension(c, state) })
	u.POST("/Library/Platforms/Batch", adminMW, func(c *gin.Context) { addPlatformsBatch(c, state) })
	u.POST("/Library/Platforms/CoverArt/GenerateAll", adminMW, func(c *gin.Context) { generateAllPlatformCovers(c, state) })
	u.POST("/Library/Platforms/:id/Enable", adminMW, func(c *gin.Context) { setPlatformEnabled(c, state, true) })
	u.POST("/Library/Platforms/:id/Disable", adminMW, func(c *gin.Context) { setPlatformEnabled(c, state, false) })
	u.POST("/Library/Platforms/:id/Image/Generate", adminMW, func(c *gin.Context) { generatePlatformCover(c, state) })
	u.POST("/Library/Platforms/:id/Rename", adminMW, func(c *gin.Context) { renamePlatform(c, state) })
	// 多值聚合:把若干匹配值合并进/移出某虚拟库
	u.POST("/Library/Platforms/:id/Values", adminMW, func(c *gin.Context) { addPlatformValues(c, state) })
	u.DELETE("/Library/Platforms/:id/Values", adminMW, func(c *gin.Context) { removePlatformValue(c, state) })
	u.DELETE("/Library/Platforms/:id", adminMW, func(c *gin.Context) { deletePlatform(c, state) })
	u.POST("/Library/Platforms/Scan", adminMW, func(c *gin.Context) { scanPlatformStudios(c, state) })
	u.POST("/Library/Platforms/ScanFilename", adminMW, func(c *gin.Context) { scanPlatformByFilename(c, state) })
	u.POST("/Library/Platforms/Rescrape", adminMW, func(c *gin.Context) { rescrapeMissingStudio(c, state) })
	u.GET("/Library/Platforms/Rescrape/Progress", adminMW, func(c *gin.Context) { getRescrapeProgress(c, state) })
	u.POST("/Library/Platforms/SortOrder", adminMW, func(c *gin.Context) { updatePlatformSortOrder(c, state) })
}

func matchUserOrAdmin(c *gin.Context, userID string) bool {
	u := middleware.GetAuthUser(c)
	if u == nil {
		return false
	}
	if u.IsAdmin {
		return true
	}
	return u.ID == userID
}

// resolveUserID 优先取 URL path 上的 :userId；为空时（如 DS One 这类客户端
// 仅依赖 token 反查）回退到当前已认证用户。
func resolveUserID(c *gin.Context) string {
	if uid := c.Param("userId"); uid != "" {
		return uid
	}
	if u := middleware.GetAuthUser(c); u != nil {
		return u.ID
	}
	return ""
}

func getUserViews(c *gin.Context) {
	state := GetState(c)
	userID := resolveUserID(c)
	if !matchUserOrAdmin(c, userID) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}

	ctx := c.Request.Context()
	cacheKey := "views:all"
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
	var platformPosition, showItemCountStr *string
	_ = state.DB.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'platform_libraries_position'").Scan(&platformPosition)
	_ = state.DB.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'library_show_item_count'").Scan(&showItemCountStr)
	platformBefore := platformPosition != nil && *platformPosition == "before"
	showItemCount := showItemCountStr == nil || *showItemCountStr != "false"

	// Platform virtual libraries
	var platformEntries []gin.H
	if models.IsPlatformLibrariesEnabled(ctx, state.DB) {
		platforms, _ := models.GetEnabledPlatforms(ctx, state.DB)
		for _, p := range platforms {
			if p.ItemCount == 0 {
				continue
			}
			vid := models.PlatformVirtualID(p.Dimension, p.MatchValue)
			colType := models.PlatformCollectionType(ctx, state.DB, p.Dimension, p.Values())
			imgTags := gin.H{}
			// 有生成封面、或是已知平台(内置 logo)时才挂 Primary
			if (p.CoverImagePath != nil && *p.CoverImagePath != "") || models.HasPlatformLogo(p.PlatformName) {
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
		if len(lib.Paths) > 0 {
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

	// 大批量分页列表：跳过 series_fallback JOIN 提升性能
	if opts.Recursive && opts.ParentID == nil && opts.UserID == nil && len(opts.GenreIDs) == 0 {
		opts.LightMode = true
	}

	// Handle platform virtual library (UUID-based lookup)
	if opts.ParentID != nil {
		if p, ok := models.ResolvePlatformVirtualID(ctx, state.DB, *opts.ParentID); ok {
			opts.ParentID = nil
			applyVirtualDimension(opts, p)
			if len(opts.IncludeItemTypes) == 0 {
				opts.IncludeItemTypes = []string{"Movie", "Series"}
			}
			opts.Recursive = true
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

	c.JSON(http.StatusOK, gin.H{
		"Items":            items,
		"TotalRecordCount": res.TotalCount,
	})
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
		"TotalRecordCount": res.TotalCount,
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

	// Handle platform virtual library
	if p, ok := models.ResolvePlatformVirtualID(ctx, state.DB, parentID); ok {
		studioOpt := &models.ItemQueryOptions{
			IncludeItemTypes: []string{"Movie", "Series"},
			Limit:            &limit,
			Recursive:        true,
		}
		applyVirtualDimension(studioOpt, p)
		sb := "DateCreated"
		so := "Descending"
		studioOpt.SortBy = &sb
		studioOpt.SortOrder = &so
		res, err := models.QueryItems(ctx, state.DB, studioOpt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		sid := state.Config.ServerID
		items := make([]dto.BaseItemDto, 0, len(res.Items))
		for i := range res.Items {
			items = append(items, dto.FormatItemDto(&res.Items[i], sid, nil))
		}
		c.JSON(http.StatusOK, items)
		return
	}

	rows, err := models.GetLatestItems(ctx, state.DB, parentID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	sid := state.Config.ServerID
	needMediaSources := strings.Contains(c.Query("Fields"), "MediaSources") || strings.Contains(c.Query("Fields"), "Path")
	items := make([]dto.BaseItemDto, 0, len(rows))
	for i := range rows {
		item := dto.FormatItemDto(&rows[i], sid, nil)
		if needMediaSources {
			applyListMediaSourceDisplay(c, ctx, state, &rows[i], &item)
		}
		items = append(items, item)
	}

	c.JSON(http.StatusOK, items)
}

func applyListMediaSourceDisplay(c *gin.Context, ctx context.Context, state *AppState, row *dto.ItemRow, item *dto.BaseItemDto) {
	if row == nil || item == nil || (row.ItemType != "Movie" && row.ItemType != "Episode") {
		return
	}
	sources := buildItemMediaSources(ctx, state, row.ID, row)
	if len(sources) == 0 {
		return
	}
	hideMediaSourceSizeForInfuse(c, sources)
	item.MediaSources = sources
	item.MediaStreams = sources[0].MediaStreams
	if strings.TrimSpace(sources[0].Path) != "" {
		path := sources[0].Path
		item.Path = &path
	}
	if strings.TrimSpace(sources[0].Container) != "" {
		container := sources[0].Container
		item.Container = &container
	}
}

func enrichItemDetail(ctx context.Context, pool *pgxpool.Pool, item *dto.ItemRow, userID string, serverID string) (dto.BaseItemDto, error) {
	var ud *dto.UserDataRow
	if u, err := models.GetUserItemData(ctx, pool, userID, item.ID); err == nil {
		ud = u
	}

	base := dto.FormatItemDto(item, serverID, ud)

	var seriesItem *dto.ItemRow
	if item.ItemType == "Episode" || item.ItemType == "Season" {
		sid := item.SeriesID
		if sid == nil {
			sid = item.ParentID
		}
		if sid != nil {
			if s, err := models.GetItemByID(ctx, pool, *sid); err == nil && s != nil {
				seriesItem = s
			}
		}
	}

	if seriesItem != nil {
		if len(base.ImageTags) == 0 && seriesItem.PrimaryImageTag != nil {
			base.SeriesPrimaryImageTag = seriesItem.PrimaryImageTag
			base.SeriesPrimaryImageItemID = &seriesItem.ID
			base.ParentPrimaryImageItemID = &seriesItem.ID
			base.ParentPrimaryImageTag = seriesItem.PrimaryImageTag
			base.ParentThumbItemID = &seriesItem.ID
			base.ParentThumbImageTag = seriesItem.PrimaryImageTag
		}
		if len(base.BackdropImageTags) == 0 && seriesItem.BackdropImageTag != nil {
			base.ParentBackdropItemID = &seriesItem.ID
			base.ParentBackdropImageTags = []string{*seriesItem.BackdropImageTag}
		}
		if base.Overview == nil {
			base.Overview = seriesItem.Overview
		}
	}

	genrePairs, err := models.GetItemGenres(ctx, pool, item.ID)
	if err != nil {
		return base, err
	}
	if len(genrePairs) == 0 && seriesItem != nil {
		genrePairs, _ = models.GetItemGenres(ctx, pool, seriesItem.ID)
	}
	if len(genrePairs) > 0 {
		base.GenreItems = make([]dto.GenreItem, 0, len(genrePairs))
		base.Genres = make([]string, 0, len(genrePairs))
		for _, p := range genrePairs {
			base.GenreItems = append(base.GenreItems, dto.GenreItem{Name: p[1], ID: p[0]})
			base.Genres = append(base.Genres, p[1])
		}
	}

	// Tags(与 Genres 分离)
	if tags, terr := models.GetItemTags(ctx, pool, item.ID); terr == nil && len(tags) > 0 {
		base.Tags = tags
	}

	// 额外 Backdrop(extrafanart)→ 追加到 BackdropImageTags(主图为 Backdrop/0,extra 为 1..N)。
	// 仅当已有主 backdrop(占据数组 0 位)时追加,保证 tag 下标与 /Images/Backdrop/{index} 对齐。
	if len(base.BackdropImageTags) > 0 {
		if extra, eerr := models.GetItemExtraBackdrops(ctx, pool, item.ID); eerr == nil && len(extra) > 0 {
			base.BackdropImageTags = append(base.BackdropImageTags, extra...)
		}
	}

	// 详情侧补 original_title / 预告片(RemoteTrailers),列表场景不带以减负。
	var origTitle, trailerURL *string
	if err := pool.QueryRow(ctx,
		"SELECT original_title, trailer_url FROM items WHERE id = $1::uuid", item.ID,
	).Scan(&origTitle, &trailerURL); err == nil {
		if origTitle != nil && *origTitle != "" {
			base.OriginalTitle = origTitle
		}
		if trailerURL != nil && *trailerURL != "" {
			base.RemoteTrailers = []dto.MediaUrl{{Url: *trailerURL, Name: item.Name}}
		}
	}

	cast, err := models.GetItemCast(ctx, pool, item.ID)
	if err != nil {
		return base, err
	}
	if len(cast) == 0 && seriesItem != nil {
		cast, _ = models.GetItemCast(ctx, pool, seriesItem.ID)
	}
	if len(cast) > 0 {
		base.People = cast
	}

	streams, err := models.GetMediaStreams(ctx, pool, item.ID)
	if err != nil {
		return base, err
	}
	streamDtos := make([]dto.MediaStreamInfo, 0, len(streams))
	for i := range streams {
		streamDtos = append(streamDtos, dto.FormatMediaStreamDto(&streams[i]))
	}
	base.MediaStreams = streamDtos

	mvRows, err := pool.Query(ctx,
		`SELECT id::text, name, file_path, COALESCE(container, ''), is_primary, runtime_ticks, bitrate, size, mediainfo,
		        resolution, hdr_format, video_codec, audio_codec, source, quality_label
		 FROM media_versions WHERE item_id = $1::uuid ORDER BY is_primary DESC, created_at`,
		item.ID)
	if err != nil {
		return base, err
	}
	defer mvRows.Close()

	sources := make([]dto.MediaSourceInfo, 0)
	mvIdx := 0
	for mvRows.Next() {
		var idStr, name, fpath, container string
		var isPrimary bool
		var rt *int64
		var br *int32
		var sz *int64
		var mediaInfoJSON []byte
		var resolution, hdrFormat, videoCodec, audioCodec, source, qualityLabel *string
		if err := mvRows.Scan(&idStr, &name, &fpath, &container, &isPrimary, &rt, &br, &sz, &mediaInfoJSON,
			&resolution, &hdrFormat, &videoCodec, &audioCodec, &source, &qualityLabel); err != nil {
			return base, err
		}
		bitrate := (*int64)(nil)
		if br != nil {
			v := int64(*br)
			bitrate = &v
		}
		versionStreams := streamDtos
		if len(mediaInfoJSON) > 0 {
			var mi map[string]json.RawMessage
			if json.Unmarshal(mediaInfoJSON, &mi) == nil {
				if msRaw, ok := mi["MediaStreams"]; ok {
					var miStreams []dto.MediaStreamInfo
					if json.Unmarshal(msRaw, &miStreams) == nil && len(miStreams) > 0 {
						versionStreams = miStreams
					}
				}
			}
		}
		if len(versionStreams) == 0 && mvIdx == 0 {
			versionStreams = streamDtos
		}
		displayPath, displayContainer, displayProtocol, displayRemote := mediaSourceDisplayInfo(fpath, container)
		ms := dto.MediaSourceInfo{
			ID:                    idStr,
			Path:                  displayPath,
			Protocol:              displayProtocol,
			Type:                  "Default",
			Container:             displayContainer,
			Name:                  name,
			IsRemote:              displayRemote,
			RunTimeTicks:          rt,
			SupportsDirectPlay:    true,
			SupportsDirectStream:  true,
			SupportsTranscoding:   false,
			MediaStreams:          versionStreams,
			Bitrate:               bitrate,
			Size:                  sz,
			ReadAtNativeFramerate: false,
			DirectStreamURL:       fmt.Sprintf("/Videos/%s/stream.%s?MediaSourceId=%s&Static=true", item.ID, displayContainer, idStr),
			ETag:                  idStr,
			Formats:               []string{},
			FymsResolution:        resolution,
			FymsHdrFormat:         hdrFormat,
			FymsVideoCodec:        videoCodec,
			FymsAudioCodec:        audioCodec,
			FymsSource:            source,
			FymsQualityLabel:      qualityLabel,
		}
		sources = append(sources, ms)
		mvIdx++
	}
	if err := mvRows.Err(); err != nil {
		return base, err
	}
	if len(sources) == 0 && (len(streamDtos) > 0 || strOrPath(item) != "") {
		ms := dto.MediaSourceInfo{
			ID:                    item.ID,
			Path:                  strOrPath(item),
			Protocol:              "File",
			Type:                  "Default",
			Container:             strVal(item.Container),
			Name:                  item.Name,
			IsRemote:              false,
			RunTimeTicks:          item.RuntimeTicks,
			SupportsDirectPlay:    true,
			SupportsDirectStream:  true,
			SupportsTranscoding:   false,
			MediaStreams:          streamDtos,
			ReadAtNativeFramerate: false,
			Formats:               []string{},
		}
		sources = []dto.MediaSourceInfo{ms}
	}
	base.MediaSources = sources

	if item.ItemType == "Movie" || item.ItemType == "Episode" {
		mergedSources := collectMergedMediaSources(ctx, pool, item.ID, streamDtos)
		if len(mergedSources) > 0 {
			base.MediaSources = append(base.MediaSources, mergedSources...)
		}
		// Emby standard: set MediaSourceCount so clients know multiple versions exist
		msc := int32(len(base.MediaSources))
		if msc > 1 {
			base.MediaSourceCount = &msc
		}
	}

	return base, nil
}

// collectMergedMediaSources finds items merged into itemID (via merged_to_id)
// and returns their media_versions as additional MediaSourceInfo entries.
func collectMergedMediaSources(ctx context.Context, pool *pgxpool.Pool, itemID string, fallbackStreams []dto.MediaStreamInfo) []dto.MediaSourceInfo {
	sibRows, err := pool.Query(ctx,
		`SELECT s.id::text, l.name AS lib_name
		 FROM items s JOIN libraries l ON s.library_id = l.id
		 WHERE s.merged_to_id = $1::uuid AND l.deleted_at IS NULL`, itemID)
	if err != nil {
		return nil
	}
	defer sibRows.Close()

	type siblingInfo struct{ ID, LibName string }
	var siblings []siblingInfo
	for sibRows.Next() {
		var si siblingInfo
		if err := sibRows.Scan(&si.ID, &si.LibName); err != nil {
			continue
		}
		siblings = append(siblings, si)
	}
	if len(siblings) == 0 {
		return nil
	}

	var merged []dto.MediaSourceInfo
	for _, sib := range siblings {
		mvRows, err := pool.Query(ctx,
			`SELECT id::text, name, file_path, COALESCE(container,''), is_primary, runtime_ticks, bitrate, size, mediainfo,
			        resolution, hdr_format, video_codec, audio_codec, source, quality_label
			 FROM media_versions WHERE item_id = $1::uuid ORDER BY is_primary DESC, created_at`,
			sib.ID)
		if err != nil {
			continue
		}
		for mvRows.Next() {
			var idStr, name, fpath, container string
			var isPrimary bool
			var rt *int64
			var br *int32
			var sz *int64
			var mediaInfoJSON []byte
			var resolution, hdrFormat, videoCodec, audioCodec, source, qualityLabel *string
			if err := mvRows.Scan(&idStr, &name, &fpath, &container, &isPrimary, &rt, &br, &sz, &mediaInfoJSON,
				&resolution, &hdrFormat, &videoCodec, &audioCodec, &source, &qualityLabel); err != nil {
				continue
			}
			bitrate := (*int64)(nil)
			if br != nil {
				v := int64(*br)
				bitrate = &v
			}
			versionStreams := fallbackStreams
			if len(mediaInfoJSON) > 0 {
				var mi map[string]json.RawMessage
				if json.Unmarshal(mediaInfoJSON, &mi) == nil {
					if msRaw, ok := mi["MediaStreams"]; ok {
						var miStreams []dto.MediaStreamInfo
						if json.Unmarshal(msRaw, &miStreams) == nil && len(miStreams) > 0 {
							versionStreams = miStreams
						}
					}
				}
			}
			srcName := sib.LibName + " - " + name
			displayPath, displayContainer, displayProtocol, displayRemote := mediaSourceDisplayInfo(fpath, container)
			ms := dto.MediaSourceInfo{
				ID:                    idStr,
				Path:                  displayPath,
				Protocol:              displayProtocol,
				Type:                  "Default",
				Container:             displayContainer,
				Name:                  srcName,
				IsRemote:              displayRemote,
				RunTimeTicks:          rt,
				SupportsDirectPlay:    true,
				SupportsDirectStream:  true,
				SupportsTranscoding:   false,
				MediaStreams:          versionStreams,
				Bitrate:               bitrate,
				Size:                  sz,
				ReadAtNativeFramerate: false,
				DirectStreamURL:       fmt.Sprintf("/Videos/%s/stream.%s?MediaSourceId=%s&Static=true", itemID, displayContainer, idStr),
				ETag:                  idStr,
				Formats:               []string{},
				FymsResolution:        resolution,
				FymsHdrFormat:         hdrFormat,
				FymsVideoCodec:        videoCodec,
				FymsAudioCodec:        audioCodec,
				FymsSource:            source,
				FymsQualityLabel:      qualityLabel,
			}
			merged = append(merged, ms)
		}
		mvRows.Close()
	}
	return merged
}

func mediaSourceDisplayInfo(filePath, container string) (string, string, string, bool) {
	displayPath := filePath
	displayContainer := container
	displayProtocol := "File"
	displayRemote := false
	if strings.HasSuffix(strings.ToLower(filePath), ".strm") {
		if rp := resolveStrmPath(filePath); rp != nil {
			displayPath = rp.filePath
			if rp.container != "" {
				displayContainer = rp.container
			}
			displayRemote = rp.isRemote
			if displayRemote {
				displayProtocol = "Http"
			}
		}
	}
	if displayContainer == "" {
		displayContainer = strings.TrimPrefix(strings.ToLower(filepath.Ext(displayPath)), ".")
	}
	if displayContainer == "" {
		displayContainer = "mkv"
	}
	return displayPath, displayContainer, displayProtocol, displayRemote
}

func strOrPath(item *dto.ItemRow) string {
	if item.ResolvedPath != nil && *item.ResolvedPath != "" {
		return *item.ResolvedPath
	}
	if item.FilePath != nil {
		return *item.FilePath
	}
	return ""
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func getItemDetail(c *gin.Context) {
	state := GetState(c)
	pathUser := resolveUserID(c)
	if !matchUserOrAdmin(c, pathUser) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}

	itemID := c.Param("itemId")
	ctx := c.Request.Context()

	// Check if this is a platform virtual library
	if p, ok := models.ResolvePlatformVirtualID(ctx, state.DB, itemID); ok {
		count, _ := models.CountItemsForVirtual(ctx, state.DB, p.Dimension, p.Values())
		colType := models.PlatformCollectionType(ctx, state.DB, p.Dimension, p.Values())
		imgTags := gin.H{}
		if (p.CoverImagePath != nil && *p.CoverImagePath != "") || models.HasPlatformLogo(p.PlatformName) {
			imgTags["Primary"] = itemID
		}
		resp := gin.H{
			"Name":               p.EffectiveDisplayName(),
			"ServerId":           state.Config.ServerID,
			"Id":                 itemID,
			"Etag":               itemID,
			"Type":               "CollectionFolder",
			"IsFolder":           true,
			"ChildCount":         count,
			"RecursiveItemCount": count,
			"SortName":           strings.ToLower(p.PlatformName),
			"ImageTags":          imgTags,
			"BackdropImageTags":  []string{},
			"PlatformLibrary":    true,
			"UserData": gin.H{
				"PlaybackPositionTicks": 0,
				"PlayCount":             0,
				"IsFavorite":            false,
				"Played":                false,
				"UnplayedItemCount":     count,
			},
		}
		// 混合库(colType 为空)省略 CollectionType, 客户端才会同时显示电影和剧集
		if colType != "" {
			resp["CollectionType"] = colType
		}
		c.JSON(http.StatusOK, resp)
		return
	}

	if uid, err := uuid.Parse(itemID); err == nil {
		lib, lerr := models.GetLibraryByID(ctx, state.DB, uid)
		if lerr == nil && lib != nil {
			var childCount int64
			childCount, _ = models.GetLibraryDisplayItemCount(ctx, state.DB, uid.String())
			var recursiveCount int64
			state.DB.QueryRow(ctx,
				"SELECT COUNT(*) FROM items WHERE library_id = $1", uid).Scan(&recursiveCount)

			imageTags := gin.H{}
			if lib.PrimaryImageTag != nil {
				imageTags["Primary"] = *lib.PrimaryImageTag
			}

			resp := gin.H{
				"Name":               lib.Name,
				"ServerId":           state.Config.ServerID,
				"Id":                 lib.ID.String(),
				"Etag":               lib.ID.String(),
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
					"UnplayedItemCount":     childCount,
				},
			}
			if len(lib.Paths) > 0 {
				resp["Path"] = lib.Paths[0]
			}
			c.JSON(http.StatusOK, resp)
			return
		}
	}

	item, err := models.GetItemByAnyID(ctx, state.DB, itemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
		return
	}

	// If this is a merged secondary, transparently serve the primary's data
	// so the client gets the primary's metadata + aggregated MediaSources.
	var mergedToID *string
	state.DB.QueryRow(ctx, "SELECT merged_to_id::text FROM items WHERE id = $1::uuid", item.ID).Scan(&mergedToID)
	if mergedToID != nil && *mergedToID != "" {
		primary, perr := models.GetItemByAnyID(ctx, state.DB, *mergedToID)
		if perr == nil && primary != nil {
			item = primary
		}
	}

	base, err := enrichItemDetail(ctx, state.DB, item, pathUser, state.Config.ServerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	// 本地预告片(trailers/ 目录)→ 追加一条绝对地址 RemoteTrailers,Infuse 可"播放预告片"。
	var localTrailer *string
	state.DB.QueryRow(ctx, "SELECT local_trailer_path FROM items WHERE id = $1::uuid", item.ID).Scan(&localTrailer)
	if localTrailer != nil && *localTrailer != "" {
		scheme := "http"
		if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		trailerURL := scheme + "://" + c.Request.Host + "/Videos/" + item.ID + "/trailer"
		base.RemoteTrailers = append(base.RemoteTrailers, dto.MediaUrl{Url: trailerURL, Name: "预告片"})
	}

	hideMediaSourceSizeForInfuse(c, base.MediaSources)
	if len(base.MediaSources) > 0 {
		if strings.TrimSpace(base.MediaSources[0].Path) != "" {
			path := base.MediaSources[0].Path
			base.Path = &path
		}
		if strings.TrimSpace(base.MediaSources[0].Container) != "" {
			container := base.MediaSources[0].Container
			base.Container = &container
		}
	}

	// Rust converts DTO to JSON value then explicitly adds MediaSources/MediaStreams
	// for Movie/Episode. We replicate that: marshal→map→inject fields.
	rawJSON, _ := json.Marshal(base)
	var result gin.H
	if err := json.Unmarshal(rawJSON, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	if item.ItemType == "Movie" || item.ItemType == "Episode" {
		// Ensure MediaSources is always present (even as []) for playable items
		if _, ok := result["MediaSources"]; !ok {
			result["MediaSources"] = []dto.MediaSourceInfo{}
		}

		// Top-level MediaStreams: if DB had no streams, try mediainfo fallback (matching Rust)
		if base.MediaStreams == nil || len(base.MediaStreams) == 0 {
			var miRaw []byte
			err := state.DB.QueryRow(ctx,
				`SELECT mediainfo->'MediaStreams' FROM media_versions
				 WHERE item_id = $1::uuid AND mediainfo IS NOT NULL
				 ORDER BY is_primary DESC LIMIT 1`, item.ID).Scan(&miRaw)
			if err == nil && len(miRaw) > 0 {
				var miStreams []dto.MediaStreamInfo
				if json.Unmarshal(miRaw, &miStreams) == nil && len(miStreams) > 0 {
					result["MediaStreams"] = miStreams
				}
			}
		}
	}

	c.JSON(http.StatusOK, result)
}

func getSimilarItems(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	ctx := c.Request.Context()

	resolved, err := models.ResolveToUUID(ctx, state.DB, itemID)
	if err != nil || resolved == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
		return
	}

	var libID string
	err = state.DB.QueryRow(ctx, "SELECT library_id::text FROM items WHERE id = $1::uuid", *resolved).Scan(&libID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	idRows, err := state.DB.Query(ctx,
		`SELECT id::text FROM items WHERE library_id = $1::uuid AND id <> $2::uuid
		 AND type IN ('Movie', 'Series', 'Episode', 'Video')
		 ORDER BY RANDOM() LIMIT 12`,
		libID, *resolved)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	var ids []string
	for idRows.Next() {
		var id string
		if err := idRows.Scan(&id); err != nil {
			idRows.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		ids = append(ids, id)
	}
	idRows.Close()

	var items []dto.ItemRow
	for _, id := range ids {
		row, err := models.GetItemByID(ctx, state.DB, id)
		if err != nil || row == nil {
			continue
		}
		items = append(items, *row)
	}

	sid := state.Config.ServerID
	out := make([]dto.BaseItemDto, 0, len(items))
	for i := range items {
		out = append(out, dto.FormatItemDto(&items[i], sid, nil))
	}
	c.JSON(http.StatusOK, gin.H{"Items": out, "TotalRecordCount": len(out)})
}

type virtualFolderBody struct {
	Name           string   `json:"Name"`
	CollectionType string   `json:"CollectionType"`
	Paths          []string `json:"Paths"`
}

func addLibrary(c *gin.Context) {
	state := GetState(c)
	var body virtualFolderBody
	_ = c.ShouldBindJSON(&body)

	if qn := c.Query("name"); qn != "" {
		body.Name = qn
	}
	if qct := c.Query("collectionType"); qct != "" {
		body.CollectionType = qct
	}
	if body.Name == "" || body.CollectionType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Name and CollectionType required"})
		return
	}
	lib, err := models.CreateLibrary(c.Request.Context(), state.DB, body.Name, body.CollectionType, body.Paths)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
	_ = lib
}

// deleteLibrary 把库标记为 deleted_at = NOW() 后立即返回 204,真正的 items
// 物理删除由 CleanupAdapter 后台分批跑(避免大库 DELETE 阻塞请求几分钟)。
// 完成后通过 task center SSE 推 succeeded snapshot,前端可据此 toast 通知。
func deleteLibrary(c *gin.Context) {
	state := GetState(c)
	idStr := strings.TrimSpace(c.Query("id"))
	if idStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "id required"})
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return
	}

	// 取库名给 cleanup snapshot 展示用;库不存在则当幂等成功处理。
	lib, err := models.GetLibraryByID(c.Request.Context(), state.DB, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if lib == nil {
		c.Status(http.StatusNoContent)
		return
	}

	marked, err := models.MarkLibraryDeleted(c.Request.Context(), state.DB, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)

	if marked && state.CleanupTask != nil {
		state.CleanupTask.Enqueue(id, lib.Name)
	}
	c.Status(http.StatusNoContent)
}

type renameLibraryBody struct {
	ID   string `json:"Id"`
	Name string `json:"Name"`
}

func renameLibrary(c *gin.Context) {
	state := GetState(c)
	var body renameLibraryBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	id, err := uuid.Parse(body.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid Id"})
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Name required"})
		return
	}
	name := strings.TrimSpace(body.Name)
	lib, err := models.UpdateLibrary(c.Request.Context(), state.DB, id, &name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.JSON(http.StatusOK, lib)
}

type libraryPathBody struct {
	ID   string `json:"Id"`
	Path string `json:"Path"`
}

func addLibraryPath(c *gin.Context) {
	state := GetState(c)
	var body libraryPathBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	id, err := uuid.Parse(body.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid Id"})
		return
	}
	if strings.TrimSpace(body.Path) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Path required"})
		return
	}
	if err := models.AddLibraryPath(c.Request.Context(), state.DB, id, body.Path); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

func removeLibraryPath(c *gin.Context) {
	state := GetState(c)
	idStr := strings.TrimSpace(c.Query("id"))
	path := strings.TrimSpace(c.Query("path"))
	if idStr == "" || path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "id and path required"})
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return
	}
	if err := models.RemoveLibraryPath(c.Request.Context(), state.DB, id, path); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

func uploadImage(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	imageType := strings.TrimSpace(c.Param("imageType"))
	ctx := c.Request.Context()

	resolved, err := models.ResolveToUUID(ctx, state.DB, itemID)
	if err != nil || resolved == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		data, rerr := io.ReadAll(c.Request.Body)
		if rerr != nil || len(data) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"message": "file required (multipart field 'file')"})
			return
		}
		ext := ".bin"
		switch imageType {
		case "Primary", "Thumb":
			ext = ".jpg"
		}
		if err := saveItemImage(ctx, state, *resolved, imageType, ext, data); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	defer src.Close()
	data, err := io.ReadAll(src)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	ext := filepath.Ext(file.Filename)
	if ext == "" {
		ext = ".jpg"
	}
	if err := saveItemImage(ctx, state, *resolved, imageType, ext, data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func saveItemImage(ctx context.Context, state *AppState, itemUUID, imageType, ext string, data []byte) error {
	tag := uuid.New().String()
	dir := filepath.Join(state.Config.DataDir, "images", itemUUID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	safeType := strings.ReplaceAll(strings.ToLower(imageType), "/", "_")
	fpath := filepath.Join(dir, safeType+ext)
	if err := os.WriteFile(fpath, data, 0644); err != nil {
		return err
	}

	switch strings.ToLower(imageType) {
	case "primary", "thumb":
		_, err := state.DB.Exec(ctx,
			"UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid",
			fpath, tag, itemUUID)
		return err
	case "backdrop", "backdrops":
		_, err := state.DB.Exec(ctx,
			"UPDATE items SET backdrop_image_path = $1, backdrop_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid",
			fpath, tag, itemUUID)
		return err
	default:
		_, err := state.DB.Exec(ctx,
			"UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid",
			fpath, tag, itemUUID)
		return err
	}
}

func deleteImage(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	imageType := strings.TrimSpace(c.Param("imageType"))
	ctx := c.Request.Context()

	resolved, err := models.ResolveToUUID(ctx, state.DB, itemID)
	if err != nil || resolved == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
		return
	}

	switch strings.ToLower(imageType) {
	case "primary", "thumb":
		_, err = state.DB.Exec(ctx,
			"UPDATE items SET primary_image_path = NULL, primary_image_tag = NULL, updated_at = NOW() WHERE id = $1::uuid",
			*resolved)
	case "backdrop", "backdrops":
		_, err = state.DB.Exec(ctx,
			"UPDATE items SET backdrop_image_path = NULL, backdrop_image_tag = NULL, updated_at = NOW() WHERE id = $1::uuid",
			*resolved)
	default:
		_, err = state.DB.Exec(ctx,
			"UPDATE items SET primary_image_path = NULL, primary_image_tag = NULL, updated_at = NOW() WHERE id = $1::uuid",
			*resolved)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func refreshAll(c *gin.Context) {
	state := GetState(c)
	req, hasBody, err := parseLibraryRefreshRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	scopes, err := resolveLibraryRefreshScopes(req, hasBody, nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	scanStarted := shouldRunLibraryScan(req, hasBody, true)
	opts := buildLibraryRefreshOptions(req)
	if opts.ValidateOnly && opts.AllowRemote {
		c.JSON(http.StatusBadRequest, gin.H{"message": "validate_only 不支持 allow_remote=true"})
		return
	}
	if len(scopes) > 0 && state.RefreshQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "refresh queue not ready"})
		return
	}

	resp := gin.H{"status": "accepted", "scan_started": scanStarted}
	queueRefreshAfterScan := scanStarted && len(scopes) > 0
	if queueRefreshAfterScan {
		resp["refresh_queued_after_scan"] = true
		resp["refresh_scopes"] = refreshScopeNames(scopes)
	}
	if len(scopes) > 0 && !queueRefreshAfterScan {
		scopeItems, queuedTasks, err := enqueueLibraryRefreshScopes(c.Request.Context(), state, nil, scopes, opts)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		resp["queued_tasks"] = queuedTasks
		resp["scope_items"] = scopeItems
		resp["allow_remote"] = opts.AllowRemote
		resp["validate_only"] = opts.ValidateOnly
	}

	if scanStarted {
		go func() {
			ctx := context.Background()
			if queueRefreshAfterScan {
				services.ScanAllLibrariesWithOptions(ctx, state.DB, state.Cache, state.ScanProgress, state.Ingest, func(lib models.Library) services.ScanLibraryOptions {
					libID := lib.ID
					return services.ScanLibraryOptions{
						AfterComplete: enqueueLibraryRefreshScopesAfterScan(state, &libID, scopes, opts),
					}
				})
				return
			}
			services.ScanAllLibraries(ctx, state.DB, state.Cache, state.ScanProgress, state.Ingest)
		}()
	}
	c.JSON(http.StatusAccepted, resp)
}

func refreshSingle(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	ctx := c.Request.Context()

	resolved, err := models.ResolveToUUID(ctx, state.DB, itemID)
	if err != nil || resolved == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
		return
	}

	var lib models.Library
	err = state.DB.QueryRow(ctx,
		`SELECT l.id, l.name, l.collection_type, l.paths, l.created_at, l.primary_image_path, l.primary_image_tag
		 FROM libraries l JOIN items i ON i.library_id = l.id WHERE i.id = $1::uuid AND l.deleted_at IS NULL`,
		*resolved).Scan(&lib.ID, &lib.Name, &lib.CollectionType, &lib.Paths, &lib.CreatedAt, &lib.PrimaryImagePath, &lib.PrimaryImageTag)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	go func() {
		bg := context.Background()
		services.ScanLibrary(bg, state.DB, state.Cache, state.ScanProgress, state.Ingest, lib.ID.String(), lib.CollectionType, lib.Paths, lib.Name)
	}()
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

func getVirtualFolders(c *gin.Context) {
	state := GetState(c)
	ctx := c.Request.Context()
	libs, err := models.GetAllLibraries(ctx, state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(libs))
	for _, lib := range libs {
		idStr := lib.ID.String()
		locations := lib.Paths
		if locations == nil {
			locations = []string{}
		}

		var itemCount int64
		_ = state.DB.QueryRow(ctx,
			"SELECT COUNT(*) FROM items WHERE library_id = $1::uuid AND type IN ('Movie','Series','Episode')",
			idStr).Scan(&itemCount)

		entry := gin.H{
			"Name":               lib.Name,
			"Locations":          locations,
			"CollectionType":     lib.CollectionType,
			"ItemId":             idStr,
			"Guid":               idStr,
			"RecursiveItemCount": itemCount,
		}
		if lib.PrimaryImageTag != nil {
			entry["ImageTag"] = *lib.PrimaryImageTag
		}
		out = append(out, entry)
	}
	c.JSON(http.StatusOK, out)
}

func getScanProgress(c *gin.Context) {
	state := GetState(c)
	all := state.ScanProgress.GetAll()
	items := make([]gin.H, 0, len(all))
	for _, p := range all {
		entry := gin.H{
			"LibraryId":      p.LibraryID,
			"LibraryName":    p.LibraryName,
			"Status":         p.Status,
			"TotalItems":     p.TotalItems,
			"ProcessedItems": p.ProcessedItems,
			"Percentage":     p.Percentage,
			"StartedAt":      time.UnixMilli(p.StartedAt).UTC().Format(time.RFC3339),
		}
		if p.CurrentItem != nil {
			entry["CurrentItem"] = *p.CurrentItem
		}
		if p.CompletedAt != nil {
			entry["CompletedAt"] = time.UnixMilli(*p.CompletedAt).UTC().Format(time.RFC3339)
		}
		if p.Error != nil {
			entry["Error"] = *p.Error
		}
		items = append(items, entry)
	}
	c.JSON(http.StatusOK, gin.H{"Items": items})
}

func startProbe(c *gin.Context) {
	state := GetState(c)
	threads := 5
	if s := strings.TrimSpace(c.Query("threads")); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		threads = n
	}

	if t := state.TaskCenter.Get(taskcenter.KindProbe); t != nil {
		_, err := t.Start(c.Request.Context(), taskcenter.StartParams{"threads": threads}, taskcenter.TriggerManual)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, state.ProbeTask.GetProgress())
		return
	}

	if err := state.ProbeTask.Start(state.DB, threads); err != nil {
		c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, state.ProbeTask.GetProgress())
}

func stopProbe(c *gin.Context) {
	state := GetState(c)
	if t := state.TaskCenter.Get(taskcenter.KindProbe); t != nil {
		_ = t.Stop()
	} else {
		state.ProbeTask.Stop()
	}
	c.JSON(http.StatusOK, state.ProbeTask.GetProgress())
}

type rescrapeProgressResponse struct {
	Running      bool  `json:"running"`
	Total        int64 `json:"total"`
	Success      int64 `json:"success"`
	NotFound     int64 `json:"not_found"`
	FetchError   int64 `json:"fetch_error"`
	Processed    int64 `json:"processed"`
	PendingTotal int64 `json:"pending_total"`
	Percentage   int   `json:"percentage"`
}

type platformTaskSummary struct {
	ScanRunning      bool                     `json:"scan_running"`
	PendingTotal     int64                    `json:"pending_total"`
	PendingTMDBReady int64                    `json:"pending_tmdb_ready_total"`
	PendingMetadata  int64                    `json:"pending_metadata_total"`
	ItemsTotal       int64                    `json:"items_total"`
	Rescrape         rescrapeProgressResponse `json:"rescrape"`
}

// scrapeProgressResponse 替代已删除的 services.ScrapeProgress。
// 方案 C 后刮削由 scrape_queue + ScrapeWorker 持续消费驱动,
// 这里的字段是从 scrape_queue.Stats 派生出的兼容 shape,
// 供 /Library/Scrape/Progress 和 /Library/Tasks/Summary 保持旧契约。
type scrapeProgressResponse struct {
	Status         string `json:"status"`
	TotalItems     int64  `json:"total_items"`
	ProcessedItems int64  `json:"processed_items"`
	SuccessItems   int64  `json:"success_items"`
	FailedItems    int64  `json:"failed_items"`
	CurrentItem    string `json:"current_item,omitempty"`
	LastError      string `json:"last_error,omitempty"`
	Percentage     int    `json:"percentage"`
	MissingCount   int64  `json:"missing_count"`
	ItemsTotal     int64  `json:"items_total"`
}

type taskSummaryResponse struct {
	Scrape   scrapeProgressResponse `json:"scrape"`
	Probe    services.ProbeProgress `json:"probe"`
	Platform platformTaskSummary    `json:"platform"`
}

func buildEffectiveProbeProgress(ctx context.Context, state *AppState) services.ProbeProgress {
	prog := state.ProbeTask.GetProgress()
	if prog.Status != "running" && prog.Status != "stopping" {
		if cnt, err := services.GetMissingMediainfoCount(ctx, state.DB); err == nil {
			prog.MissingCount = cnt
		}
		if prog.MissingCount > 0 {
			prog.Status = "idle"
		}
	}
	if total, err := services.GetTotalMediaVersionsCount(ctx, state.DB); err == nil {
		prog.VersionsTotal = total
	}
	return prog
}

// buildEffectiveScrapeProgress 从 scrape_queue 派生刮削整体进度。
// 方案 C 后不再有 legacy ScrapeTask 的单一运行态:
//   - status: pending+running > 0 → running;否则 idle
//   - processed = done;total = done + pending + running + failed
//   - success/failed 分别映射到 done/failed
//   - missing_count 仍从 items 表(缺 overview 的 Movie/Series)查,
//     和 scrape_queue 各自独立,用于 UI 提示"还有多少待刮"
func buildEffectiveScrapeProgress(ctx context.Context, state *AppState) scrapeProgressResponse {
	resp := scrapeProgressResponse{Status: "idle"}
	if state.ScrapeQueue != nil {
		if stats, err := state.ScrapeQueue.Stats(ctx); err == nil {
			resp.SuccessItems = stats.Done
			resp.FailedItems = stats.Failed
			resp.ProcessedItems = stats.Done
			resp.TotalItems = stats.Done + stats.Pending + stats.Running + stats.Failed
			if stats.Pending+stats.Running > 0 {
				resp.Status = "running"
			}
			if resp.TotalItems > 0 {
				resp.Percentage = int(resp.ProcessedItems * 100 / resp.TotalItems)
			}
		}
	}
	if cnt, err := services.GetMissingScrapeCount(ctx, state.DB); err == nil {
		resp.MissingCount = cnt
	}
	if total, err := services.GetTopLevelItemCount(ctx, state.DB); err == nil {
		resp.ItemsTotal = total
	}
	return resp
}

func buildRescrapeProgressResponse(ctx context.Context, state *AppState) rescrapeProgressResponse {
	rescrapeProgress.mu.Lock()
	running := rescrapeProgress.Running
	rescrapeProgress.mu.Unlock()

	processed := atomic.LoadInt64(&rescrapeProgress.Processed)
	success := atomic.LoadInt64(&rescrapeProgress.Success)
	notFound := atomic.LoadInt64(&rescrapeProgress.NotFound)
	fetchError := atomic.LoadInt64(&rescrapeProgress.FetchError)
	total := atomic.LoadInt64(&rescrapeProgress.Total)
	pendingTotal := int64(0)
	if !running {
		if cnt, err := models.CountItemsPendingPlatformScan(ctx, state.DB, false, false); err == nil {
			pendingTotal = cnt
		}
	}
	pct := 0
	if total > 0 {
		pct = int(processed * 100 / total)
	}

	return rescrapeProgressResponse{
		Running:      running,
		Total:        total,
		Success:      success,
		NotFound:     notFound,
		FetchError:   fetchError,
		Processed:    processed,
		PendingTotal: pendingTotal,
		Percentage:   pct,
	}
}

func buildPlatformTaskSummary(ctx context.Context, state *AppState) platformTaskSummary {
	pendingTotal, _ := models.CountItemsPendingPlatformScan(ctx, state.DB, false, false)
	pendingTMDBReady, _ := models.CountItemsPendingPlatformScan(ctx, state.DB, true, false)
	pendingMetadata, _ := models.CountItemsPendingPlatformMetadataScrape(ctx, state.DB)
	itemsTotal, _ := services.GetTopLevelItemCount(ctx, state.DB)

	platformScanState.mu.Lock()
	scanRunning := platformScanState.running
	platformScanState.mu.Unlock()

	return platformTaskSummary{
		ScanRunning:      scanRunning,
		PendingTotal:     pendingTotal,
		PendingTMDBReady: pendingTMDBReady,
		PendingMetadata:  pendingMetadata,
		ItemsTotal:       itemsTotal,
		Rescrape:         buildRescrapeProgressResponse(ctx, state),
	}
}

func getProbeProgress(c *gin.Context) {
	state := GetState(c)
	c.JSON(http.StatusOK, buildEffectiveProbeProgress(c.Request.Context(), state))
}

type itemRefreshRequest struct {
	Scope              string `json:"scope"`
	Metadata           *bool  `json:"metadata"`
	Images             *bool  `json:"images"`
	ReplaceAllMetadata bool   `json:"replace_all_metadata"`
	ReplaceAllImages   bool   `json:"replace_all_images"`
	ValidateOnly       bool   `json:"validate_only"`
	AllowRemote        *bool  `json:"allow_remote"`
	RefreshSubtree     bool   `json:"refresh_subtree"`
}

type libraryRefreshRequest struct {
	Scan               *bool  `json:"scan"`
	Scope              string `json:"scope"`
	Metadata           *bool  `json:"metadata"`
	Images             *bool  `json:"images"`
	ReplaceAllMetadata bool   `json:"replace_all_metadata"`
	ReplaceAllImages   bool   `json:"replace_all_images"`
	ValidateOnly       bool   `json:"validate_only"`
	AllowRemote        *bool  `json:"allow_remote"`
	RefreshSubtree     bool   `json:"refresh_subtree"`
}

func parseItemRefreshRequest(c *gin.Context) (itemRefreshRequest, error) {
	var req itemRefreshRequest
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return req, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return req, nil
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return req, err
	}
	return req, nil
}

func parseLibraryRefreshRequest(c *gin.Context) (libraryRefreshRequest, bool, error) {
	var req libraryRefreshRequest
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return req, false, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return req, false, nil
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return req, true, err
	}
	return req, true, nil
}

func (r libraryRefreshRequest) toItemRefreshRequest() itemRefreshRequest {
	return itemRefreshRequest{
		Scope:              r.Scope,
		Metadata:           r.Metadata,
		Images:             r.Images,
		ReplaceAllMetadata: r.ReplaceAllMetadata,
		ReplaceAllImages:   r.ReplaceAllImages,
		ValidateOnly:       r.ValidateOnly,
		AllowRemote:        r.AllowRemote,
		RefreshSubtree:     r.RefreshSubtree,
	}
}

func hasExplicitLibraryRefresh(req libraryRefreshRequest) bool {
	return strings.TrimSpace(req.Scope) != "" ||
		req.Metadata != nil ||
		req.Images != nil ||
		req.ReplaceAllMetadata ||
		req.ReplaceAllImages ||
		req.ValidateOnly ||
		req.AllowRemote != nil ||
		req.RefreshSubtree
}

func isSubtreeRefreshScope(scope string) bool {
	return strings.EqualFold(strings.TrimSpace(scope), string(services.RefreshScopeSubtree))
}

func shouldRunLibraryScan(req libraryRefreshRequest, hasBody bool, defaultScan bool) bool {
	if req.Scan != nil {
		return *req.Scan
	}
	if hasBody && hasExplicitLibraryRefresh(req) {
		return false
	}
	return defaultScan
}

func resolveLibraryRefreshScopes(req libraryRefreshRequest, hasBody bool, defaultScopes []services.RefreshScope) ([]services.RefreshScope, error) {
	if !hasBody || !hasExplicitLibraryRefresh(req) {
		return defaultScopes, nil
	}
	return resolveItemRefreshScopes(req.toItemRefreshRequest())
}

func buildLibraryRefreshOptions(req libraryRefreshRequest) services.RefreshOptions {
	opts := services.DefaultRefreshOptionsForSource(services.RefreshSourceManual)
	opts.AllowRemote = false
	opts.ReplaceAllMetadata = req.ReplaceAllMetadata
	opts.ReplaceAllImages = req.ReplaceAllImages
	opts.ValidateOnly = req.ValidateOnly
	opts.RefreshSubtree = req.RefreshSubtree || isSubtreeRefreshScope(req.Scope)
	if req.AllowRemote != nil {
		opts.AllowRemote = *req.AllowRemote
	}
	return opts
}

func refreshItemTypesForScope(scope services.RefreshScope) []string {
	switch scope {
	case services.RefreshScopeMetadata:
		return []string{"Movie", "Series", "Episode"}
	case services.RefreshScopeImages:
		return []string{"Movie", "Series", "Season", "Episode"}
	case services.RefreshScopeSubtree:
		return []string{"Series"}
	default:
		return nil
	}
}

func loadLibraryRefreshTargetIDs(ctx context.Context, pool *pgxpool.Pool, libraryID *uuid.UUID, scope services.RefreshScope) ([]string, error) {
	types := refreshItemTypesForScope(scope)
	if len(types) == 0 {
		return nil, fmt.Errorf("unsupported batch refresh scope: %s", scope)
	}

	var (
		rows pgx.Rows
		err  error
	)
	if libraryID != nil {
		rows, err = pool.Query(ctx,
			`SELECT id::text
			   FROM items i
			   JOIN libraries l ON l.id = i.library_id
			  WHERE i.library_id = $1::uuid
			    AND l.deleted_at IS NULL
			    AND i.merged_to_id IS NULL
			    AND i.type = ANY($2::text[])
			  ORDER BY i.created_at ASC`,
			*libraryID, types,
		)
	} else {
		rows, err = pool.Query(ctx,
			`SELECT id::text
			   FROM items i
			   JOIN libraries l ON l.id = i.library_id
			  WHERE l.deleted_at IS NULL
			    AND i.merged_to_id IS NULL
			    AND i.type = ANY($1::text[])
			  ORDER BY i.created_at ASC`,
			types,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func enqueueLibraryRefreshScopes(ctx context.Context, state *AppState, libraryID *uuid.UUID, scopes []services.RefreshScope, opts services.RefreshOptions) (map[string]int, int64, error) {
	scopeItems := make(map[string]int, len(scopes))
	var queuedTasks int64
	for _, scope := range scopes {
		itemIDs, err := loadLibraryRefreshTargetIDs(ctx, state.DB, libraryID, scope)
		if err != nil {
			return nil, 0, err
		}
		scopeItems[string(scope)] = len(itemIDs)
		n, err := state.RefreshQueue.EnqueueBatch(ctx, itemIDs, scope, services.RefreshSourceManual, services.RefreshPriorityManual, opts)
		if err != nil {
			return nil, 0, err
		}
		queuedTasks += n
	}
	return scopeItems, queuedTasks, nil
}

func enqueueLibraryRefreshScopesAfterScan(state *AppState, libraryID *uuid.UUID, scopes []services.RefreshScope, opts services.RefreshOptions) func(context.Context) {
	return func(ctx context.Context) {
		libraryLabel := "all"
		if libraryID != nil {
			libraryLabel = libraryID.String()
		}
		scopeItems, queuedTasks, err := enqueueLibraryRefreshScopes(ctx, state, libraryID, scopes, opts)
		if err != nil {
			slog.Warn("[LibraryRefresh] enqueue after scan failed", "library", libraryLabel, "error", err)
			return
		}
		slog.Info("[LibraryRefresh] queued after scan", "library", libraryLabel, "scopes", refreshScopeNames(scopes), "items", scopeItems, "tasks", queuedTasks)
	}
}

func refreshScopeNames(scopes []services.RefreshScope) []string {
	names := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		names = append(names, string(scope))
	}
	return names
}

func resolveItemRefreshScopes(req itemRefreshRequest) ([]services.RefreshScope, error) {
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	switch scope {
	case "", string(services.RefreshScopeMetadata), string(services.RefreshScopeImages):
	case string(services.RefreshScopeSubtree):
		req.RefreshSubtree = true
	default:
		return nil, fmt.Errorf("unsupported refresh scope: %s", req.Scope)
	}

	if req.RefreshSubtree {
		return []services.RefreshScope{services.RefreshScopeSubtree}, nil
	}

	wantMetadata := scope == string(services.RefreshScopeMetadata)
	wantImages := scope == string(services.RefreshScopeImages)
	explicitSelection := scope != ""
	if req.Metadata != nil {
		wantMetadata = *req.Metadata
		explicitSelection = true
	}
	if req.Images != nil {
		wantImages = *req.Images
		explicitSelection = true
	}
	if !wantMetadata && !wantImages {
		if explicitSelection {
			return nil, fmt.Errorf("no refresh scope selected")
		}
		wantMetadata = true
	}

	scopes := make([]services.RefreshScope, 0, 2)
	if wantMetadata {
		scopes = append(scopes, services.RefreshScopeMetadata)
	}
	if wantImages {
		scopes = append(scopes, services.RefreshScopeImages)
	}
	return scopes, nil
}

func refreshItem(c *gin.Context) {
	state := GetState(c)
	if state.RefreshQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "refresh queue not ready"})
		return
	}

	itemID := c.Param("itemId")
	ctx := c.Request.Context()
	item, err := models.GetItemByID(ctx, state.DB, itemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
		return
	}

	req, err := parseItemRefreshRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}

	scopes, err := resolveItemRefreshScopes(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	for _, scope := range scopes {
		if scope == services.RefreshScopeSubtree && item.ItemType != "Series" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "subtree refresh 仅支持 Series"})
			return
		}
	}

	opts := services.DefaultRefreshOptionsForSource(services.RefreshSourceManual)
	opts.ReplaceAllMetadata = req.ReplaceAllMetadata
	opts.ReplaceAllImages = req.ReplaceAllImages
	opts.ValidateOnly = req.ValidateOnly
	opts.RefreshSubtree = req.RefreshSubtree || isSubtreeRefreshScope(req.Scope)
	if req.AllowRemote != nil {
		opts.AllowRemote = *req.AllowRemote
	}
	if opts.ValidateOnly && opts.AllowRemote {
		c.JSON(http.StatusBadRequest, gin.H{"message": "validate_only 不支持 allow_remote=true"})
		return
	}

	for _, scope := range scopes {
		if err := state.RefreshQueue.Enqueue(ctx, itemID, scope, services.RefreshSourceManual, services.RefreshPriorityManual, opts); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}

	scopeNames := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scopeNames = append(scopeNames, string(scope))
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"queued":        true,
		"item_id":       itemID,
		"item_type":     item.ItemType,
		"scopes":        scopeNames,
		"allow_remote":  opts.AllowRemote,
		"validate_only": opts.ValidateOnly,
		"message":       "已加入刷新队列，稍后自动生效",
	})
}

func scrapeItem(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	ctx := c.Request.Context()
	_, err := services.ScrapeItem(ctx, state.DB, itemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func searchTmdbForItem(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	var body struct {
		Query  string `json:"query"`
		Year   *int32 `json:"year,omitempty"`
		TmdbID *int64 `json:"tmdbId,omitempty"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请求参数无效"})
		return
	}
	if body.TmdbID == nil && strings.TrimSpace(body.Query) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请提供搜索关键词或 TMDB ID"})
		return
	}
	if body.TmdbID != nil && *body.TmdbID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请提供有效的 TMDB ID"})
		return
	}
	results, err := services.SearchTMDBForItem(c.Request.Context(), state.DB, itemID, strings.TrimSpace(body.Query), body.Year, body.TmdbID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}

func scrapeItemByTmdbId(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	var body struct {
		TmdbId int64 `json:"tmdbId"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.TmdbId <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请提供有效的 TMDB ID"})
		return
	}
	_, err := services.ScrapeItemByTMDBID(c.Request.Context(), state.DB, itemID, body.TmdbId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func getIdentifyCandidates(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	items, err := services.ListIdentifyCandidates(c.Request.Context(), state.DB, itemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func applyIdentifyCandidate(c *gin.Context) {
	state := GetState(c)
	itemID := c.Param("itemId")
	candidateID := c.Param("candidateId")
	// 候选采纳可能走 provider.GetByID(豆瓣 HTML 解析)+ TMDB Fill,总时长给 30s 兜底
	// 避免 TMDB/豆瓣慢响应让 HTTP 连接 hang 到前端超时
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	provider, externalID, err := services.ResolveIdentifyCandidate(ctx, state.DB, itemID, candidateID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	result, err := services.ScrapeItemByProviderID(ctx, state.DB, itemID, provider, externalID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_, _ = state.DB.Exec(ctx, "DELETE FROM identify_candidates WHERE item_id = $1::uuid", itemID)
	c.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"provider": provider,
		"tmdb_id":  result["tmdb_id"],
	})
}

func listUnmatchedItems(c *gin.Context) {
	state := GetState(c)
	limit := 200
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	itemType := strings.TrimSpace(c.Query("type"))
	items, err := services.ListUnmatchedItems(c.Request.Context(), state.DB, itemType, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "count": len(items)})
}

func batchApplyIdentifyCandidates(c *gin.Context) {
	state := GetState(c)
	var body struct {
		Items []struct {
			ItemID      string `json:"item_id"`
			CandidateID string `json:"candidate_id"`
		} `json:"items"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	if len(body.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "items is required"})
		return
	}
	type applyResult struct {
		ItemID   string `json:"item_id"`
		OK       bool   `json:"ok"`
		Message  string `json:"message,omitempty"`
		Provider string `json:"provider,omitempty"`
		TmdbID   int64  `json:"tmdb_id,omitempty"`
	}
	results := make([]applyResult, 0, len(body.Items))
	// 批量采纳每条 15s 超时,避免单条拖慢整个批次;上游 body 解析走无超时 context
	for _, pair := range body.Items {
		res := applyResult{ItemID: pair.ItemID}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		provider, externalID, err := services.ResolveIdentifyCandidate(ctx, state.DB, pair.ItemID, pair.CandidateID)
		if err != nil {
			cancel()
			res.Message = err.Error()
			results = append(results, res)
			continue
		}
		out, err := services.ScrapeItemByProviderID(ctx, state.DB, pair.ItemID, provider, externalID)
		if err != nil {
			cancel()
			res.Message = err.Error()
			results = append(results, res)
			continue
		}
		_, _ = state.DB.Exec(ctx, "DELETE FROM identify_candidates WHERE item_id = $1::uuid", pair.ItemID)
		cancel()
		res.OK = true
		res.Provider = provider
		if v, ok := out["tmdb_id"].(int64); ok {
			res.TmdbID = v
		}
		results = append(results, res)
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}

// scrapeAll 是"刮削缺失元数据"的入口。
// 方案 C 后不再跑一个 legacy 批处理任务,改为把所有缺 overview 且未识别的
// Movie/Series 以 refresh 优先级入队 identify,由 ScrapeWorker 持续消费。
// 返回入队数量,前端 toast 提示后引导用户到"观测中心 > 后台任务"看进度。
func scrapeAll(c *gin.Context) {
	state := GetState(c)
	n, err := services.EnqueueMissingScrapeIdentify(c.Request.Context(), state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"enqueued": n})
}

func refreshLibraryMetadata(c *gin.Context) {
	state := GetState(c)
	if state.RefreshQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "refresh queue not ready"})
		return
	}

	req, hasBody, err := parseLibraryRefreshRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	scopes, err := resolveLibraryRefreshScopes(req, hasBody, []services.RefreshScope{services.RefreshScopeMetadata})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	opts := buildLibraryRefreshOptions(req)
	if opts.ValidateOnly && opts.AllowRemote {
		c.JSON(http.StatusBadRequest, gin.H{"message": "validate_only 不支持 allow_remote=true"})
		return
	}

	scopeItems, queuedTasks, err := enqueueLibraryRefreshScopes(c.Request.Context(), state, nil, scopes, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"status":        "accepted",
		"queued_tasks":  queuedTasks,
		"scope_items":   scopeItems,
		"allow_remote":  opts.AllowRemote,
		"validate_only": opts.ValidateOnly,
	})
}

// stopScrape 是旧 API 的兼容 no-op。刮削已由 scrape_queue 驱动,
// 单条失败任务请在后台任务面板重试/取消,整体"停止刮削"已无语义。
func stopScrape(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "刮削已由队列驱动,无需停止;请到后台任务面板重试/取消单条失败任务",
	})
}

func getScrapeProgress(c *gin.Context) {
	state := GetState(c)
	c.JSON(http.StatusOK, buildEffectiveScrapeProgress(c.Request.Context(), state))
}

func getMissingScrapeCount(c *gin.Context) {
	state := GetState(c)
	n, err := services.GetMissingScrapeCount(c.Request.Context(), state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"MissingCount": n})
}

func getTaskSummary(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	c.JSON(http.StatusOK, taskSummaryResponse{
		Scrape:   buildEffectiveScrapeProgress(ctx, state),
		Probe:    buildEffectiveProbeProgress(ctx, state),
		Platform: buildPlatformTaskSummary(ctx, state),
	})
}

func mergeVersions(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	merged, err := models.MergeMultiVersionItems(ctx, state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	slog.Info("MergeVersions completed", "merged", merged)

	// Gather diagnostic counts. primary 不再要求 tmdb_id IS NOT NULL ——
	// 多源合并后 primary 可能仅有 douban/bangumi 等非 TMDB 外部 ID。
	var totalPrimaries, totalSecondaries int64
	state.DB.QueryRow(ctx,
		`SELECT COUNT(*) FROM items WHERE merged_to_id IS NULL
		   AND EXISTS (SELECT 1 FROM items s WHERE s.merged_to_id = items.id)`).Scan(&totalPrimaries)
	state.DB.QueryRow(ctx,
		"SELECT COUNT(*) FROM items WHERE merged_to_id IS NOT NULL").Scan(&totalSecondaries)

	c.JSON(http.StatusOK, gin.H{
		"merged":            merged,
		"total_primaries":   totalPrimaries,
		"total_secondaries": totalSecondaries,
	})
}

type browseBody struct {
	Path string `json:"path"`
}

func browseDir(c *gin.Context) {
	_ = GetState(c)
	var body browseBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	p := filepath.Clean(body.Path)
	if p == "" || p == "." {
		p = "/"
	}

	entries, err := os.ReadDir(p)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	type entry struct {
		Name        string `json:"Name"`
		IsDirectory bool   `json:"IsDirectory"`
		Path        string `json:"Path"`
	}
	out := make([]entry, 0, len(entries))
	for _, e := range entries {
		full := filepath.Join(p, e.Name())
		out = append(out, entry{
			Name:        e.Name(),
			IsDirectory: e.IsDir(),
			Path:        full,
		})
	}
	c.JSON(http.StatusOK, gin.H{"Path": p, "Entries": out})
}

func getGenres(c *gin.Context) {
	state := GetState(c)
	rows, err := models.GetAllGenresWithCounts(c.Request.Context(), state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"Items":            rows,
		"TotalRecordCount": len(rows),
	})
}

func getVirtualFolderDetail(c *gin.Context) {
	state := GetState(c)
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return
	}
	ctx := c.Request.Context()
	lib, err := models.GetLibraryByID(ctx, state.DB, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if lib == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Library not found"})
		return
	}

	var itemCount int64
	itemCount, _ = models.GetLibraryDisplayItemCount(ctx, state.DB, id.String())

	locations := make([]string, 0)
	if lib.Paths != nil {
		locations = lib.Paths
	}

	imageTag := ""
	if lib.PrimaryImageTag != nil {
		imageTag = *lib.PrimaryImageTag
	}

	dateCreated := lib.CreatedAt.UTC().Format(time.RFC3339)

	c.JSON(http.StatusOK, gin.H{
		"Id":             lib.ID.String(),
		"Name":           lib.Name,
		"CollectionType": lib.CollectionType,
		"Locations":      locations,
		"ItemId":         lib.ID.String(),
		"ItemCount":      itemCount,
		"DateCreated":    dateCreated,
		"ImageTag":       imageTag,
	})
}

func refreshSingleLibrary(c *gin.Context) {
	state := GetState(c)
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return
	}
	ctx := c.Request.Context()
	lib, err := models.GetLibraryByID(ctx, state.DB, id)
	if err != nil || lib == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Library not found"})
		return
	}

	req, hasBody, err := parseLibraryRefreshRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	scopes, err := resolveLibraryRefreshScopes(req, hasBody, nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	scanStarted := shouldRunLibraryScan(req, hasBody, true)
	opts := buildLibraryRefreshOptions(req)
	if opts.ValidateOnly && opts.AllowRemote {
		c.JSON(http.StatusBadRequest, gin.H{"message": "validate_only 不支持 allow_remote=true"})
		return
	}
	if len(scopes) > 0 && state.RefreshQueue == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "refresh queue not ready"})
		return
	}

	resp := gin.H{"status": "accepted", "scan_started": scanStarted, "library_id": lib.ID.String()}
	queueRefreshAfterScan := scanStarted && len(scopes) > 0
	if queueRefreshAfterScan {
		resp["refresh_queued_after_scan"] = true
		resp["refresh_scopes"] = refreshScopeNames(scopes)
	}
	if len(scopes) > 0 && !queueRefreshAfterScan {
		scopeItems, queuedTasks, err := enqueueLibraryRefreshScopes(ctx, state, &lib.ID, scopes, opts)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		resp["queued_tasks"] = queuedTasks
		resp["scope_items"] = scopeItems
		resp["allow_remote"] = opts.AllowRemote
		resp["validate_only"] = opts.ValidateOnly
	}

	if scanStarted {
		go func() {
			bg := context.Background()
			if queueRefreshAfterScan {
				services.ScanLibraryWithOptions(bg, state.DB, state.Cache, state.ScanProgress, state.Ingest, lib.ID.String(), lib.CollectionType, lib.Paths, lib.Name, services.ScanLibraryOptions{
					AfterComplete: enqueueLibraryRefreshScopesAfterScan(state, &lib.ID, scopes, opts),
				})
				return
			}
			services.ScanLibrary(bg, state.DB, state.Cache, state.ScanProgress, state.Ingest, lib.ID.String(), lib.CollectionType, lib.Paths, lib.Name)
		}()
	}
	c.JSON(http.StatusAccepted, resp)
}

func uploadLibraryImage(c *gin.Context) {
	state := GetState(c)
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return
	}
	ctx := c.Request.Context()
	lib, err := models.GetLibraryByID(ctx, state.DB, id)
	if err != nil || lib == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Library not found"})
		return
	}

	var data []byte
	file, ferr := c.FormFile("file")
	if ferr != nil {
		raw, rerr := io.ReadAll(c.Request.Body)
		if rerr != nil || len(raw) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"message": "file required"})
			return
		}
		data = raw
	} else {
		src, err := file.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		defer src.Close()
		data, err = io.ReadAll(src)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}

	if len(data) > 20*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "File too large (max 20MB)"})
		return
	}

	imgDir := filepath.Join("data", "library-images", idStr)
	if err := os.MkdirAll(imgDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	fpath := filepath.Join(imgDir, "primary.jpg")
	if err := os.WriteFile(fpath, data, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	tag := uuid.New().String()
	if err := models.UpdateLibraryImage(ctx, state.DB, id, fpath, tag); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.JSON(http.StatusOK, gin.H{"ImageTag": tag})
}

func setLibraryImageFromURL(c *gin.Context) {
	state := GetState(c)
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return
	}
	ctx := c.Request.Context()
	lib, err := models.GetLibraryByID(ctx, state.DB, id)
	if err != nil || lib == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Library not found"})
		return
	}

	var body struct {
		Url string `json:"Url"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Url == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Url is required"})
		return
	}

	if !strings.HasPrefix(body.Url, "http://") && !strings.HasPrefix(body.Url, "https://") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Url must start with http:// or https://"})
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(body.Url)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Failed to fetch image: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Remote server returned %d", resp.StatusCode)})
		return
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "image/") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "URL does not point to an image (Content-Type: " + ct + ")"})
		return
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 20*1024*1024+1))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to read image data"})
		return
	}
	if len(data) > 20*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Image too large (max 20MB)"})
		return
	}

	imgDir := filepath.Join("data", "library-images", idStr)
	if err := os.MkdirAll(imgDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	fpath := filepath.Join(imgDir, "primary.jpg")
	if err := os.WriteFile(fpath, data, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	tag := uuid.New().String()
	if err := models.UpdateLibraryImage(ctx, state.DB, id, fpath, tag); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.JSON(http.StatusOK, gin.H{"ImageTag": tag})
}

func deleteLibraryImage(c *gin.Context) {
	state := GetState(c)
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return
	}
	ctx := c.Request.Context()
	if err := models.DeleteLibraryImage(ctx, state.DB, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	imgPath := filepath.Join(state.Config.CacheDir, "images", "lib_"+idStr+".jpg")
	_ = os.Remove(imgPath)
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

// listCoverArtStyles 返回所有注册的封面生成风格,供前端下拉展示。
func listCoverArtStyles(c *gin.Context) {
	items := coverart.List()
	out := make([]map[string]any, 0, len(items))
	for _, g := range items {
		w, h := g.AspectRatio()
		out = append(out, map[string]any{
			"name":        g.Name(),
			"label":       g.Label(),
			"aspectRatio": fmt.Sprintf("%d:%d", w, h),
		})
	}
	c.JSON(http.StatusOK, out)
}

type generateLibraryCoverBody struct {
	Style   string         `json:"Style"`
	Options map[string]any `json:"Options"`
}

// generateLibraryCover 调用 coverart 风格生成封面,写入磁盘并更新 DB。
// POST /Library/VirtualFolders/:id/Image/Generate
// body: { "Style": string, "Options"?: map[string]any }
func generateLibraryCover(c *gin.Context) {
	state := GetState(c)
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid id"})
		return
	}
	ctx := c.Request.Context()
	lib, err := models.GetLibraryByID(ctx, state.DB, id)
	if err != nil || lib == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Library not found"})
		return
	}

	var body generateLibraryCoverBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	gen, style, ok := resolveCoverGenerator(body.Style)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "unknown cover style: " + strings.TrimSpace(body.Style)})
		return
	}

	tag, err := renderAndSaveLibraryCover(ctx, state, lib, gen, body.Options)
	if err != nil {
		switch {
		case err == coverart.ErrBusy:
			c.JSON(http.StatusConflict, gin.H{"message": "generation already in progress"})
			return
		case err == coverart.ErrNoPosters:
			c.JSON(http.StatusUnprocessableEntity, gin.H{"message": "媒体库暂无可用海报素材,请先扫描入库"})
			return
		case err == coverart.ErrFontMissing:
			c.JSON(http.StatusFailedDependency, gin.H{"message": err.Error()})
			return
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"message": "render failed: " + err.Error()})
			return
		}
	}
	invalidateViewsCache(c, state)
	c.JSON(http.StatusOK, gin.H{"ImageTag": tag, "Style": style})
}

func renderAndSaveLibraryCover(ctx context.Context, state *AppState, lib *models.Library, gen coverart.Generator, options map[string]any) (string, error) {
	release, err := coverart.AcquireBusy(lib.ID)
	if err != nil {
		return "", err
	}
	defer release()

	idStr := lib.ID.String()
	materials, err := coverart.PickMaterials(ctx, state.DB, lib.ID)
	if err != nil {
		return "", err
	}

	itemCount, _ := models.GetLibraryDisplayItemCount(ctx, state.DB, idStr)
	out, err := gen.Render(ctx, coverart.Input{
		LibraryID:      lib.ID,
		LibraryName:    lib.Name,
		CollectionType: lib.CollectionType,
		ItemCount:      int(itemCount),
		PosterPaths:    coverart.PosterPathsFromMaterials(materials),
		Materials:      materials,
		Options:        options,
		OutputWidth:    1920,
		OutputHeight:   1080,
	})
	if err != nil {
		return "", err
	}

	imgDir := filepath.Join("data", "library-images", idStr)
	if err := os.MkdirAll(imgDir, 0755); err != nil {
		return "", err
	}
	fpath := filepath.Join(imgDir, "primary.jpg")
	f, err := os.Create(fpath)
	if err != nil {
		return "", err
	}
	if err := imaging.Encode(f, out.Image, imaging.JPEG, imaging.JPEGQuality(out.Quality)); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("encode failed: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	tag := uuid.New().String()
	if err := models.UpdateLibraryImage(ctx, state.DB, lib.ID, fpath, tag); err != nil {
		return "", err
	}
	return tag, nil
}

// renderAndSaveVirtualCover 为多维度虚拟库生成并保存封面,复用 coverart 生成器。
func renderAndSaveVirtualCover(ctx context.Context, state *AppState, p *models.PlatformLibrary, gen coverart.Generator, options map[string]any) (string, error) {
	pid, perr := uuid.Parse(p.ID)
	if perr != nil {
		return "", perr
	}
	release, err := coverart.AcquireBusy(pid)
	if err != nil {
		return "", err
	}
	defer release()

	cond, ok := models.VirtualDimensionCondition(p.Dimension)
	if !ok {
		return "", fmt.Errorf("unknown dimension: %s", p.Dimension)
	}
	materials, err := coverart.PickMaterialsForVirtual(ctx, state.DB, cond, p.Values())
	if err != nil {
		return "", err
	}

	itemCount, _ := models.CountItemsForVirtual(ctx, state.DB, p.Dimension, p.Values())
	out, err := gen.Render(ctx, coverart.Input{
		LibraryID:      pid,
		LibraryName:    p.EffectiveDisplayName(),
		CollectionType: models.PlatformCollectionType(ctx, state.DB, p.Dimension, p.Values()),
		ItemCount:      int(itemCount),
		PosterPaths:    coverart.PosterPathsFromMaterials(materials),
		Materials:      materials,
		Options:        options,
		OutputWidth:    1920,
		OutputHeight:   1080,
	})
	if err != nil {
		return "", err
	}

	vid := models.PlatformVirtualID(p.Dimension, p.MatchValue)
	imgDir := filepath.Join("data", "library-images", "virtual")
	if err := os.MkdirAll(imgDir, 0755); err != nil {
		return "", err
	}
	fpath := filepath.Join(imgDir, vid+".jpg")
	f, err := os.Create(fpath)
	if err != nil {
		return "", err
	}
	if err := imaging.Encode(f, out.Image, imaging.JPEG, imaging.JPEGQuality(out.Quality)); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("encode failed: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	tag := uuid.New().String()
	if err := models.SetPlatformCover(ctx, state.DB, p.ID, fpath, tag); err != nil {
		return "", err
	}
	return tag, nil
}

// generatePlatformCover POST /Library/Platforms/:id/Image/Generate  body: {Style?, Options?}
func generatePlatformCover(c *gin.Context, state *AppState) {
	id := c.Param("id")
	p, err := models.GetPlatformByID(c.Request.Context(), state.DB, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "platform not found"})
		return
	}
	var body struct {
		Style   string         `json:"Style"`
		Options map[string]any `json:"Options"`
	}
	_ = c.ShouldBindJSON(&body)
	gen, style, ok := resolveCoverGenerator(body.Style)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "unknown cover style"})
		return
	}
	tag, err := renderAndSaveVirtualCover(c.Request.Context(), state, p, gen, body.Options)
	if err != nil {
		if errors.Is(err, coverart.ErrNoPosters) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"message": "该虚拟库下没有可用海报素材,无法生成封面"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.JSON(http.StatusOK, gin.H{"ImageTag": tag, "Style": style})
}

// generateAllPlatformCovers POST /Library/Platforms/CoverArt/GenerateAll  body: {Style?, Options?}
// 给所有已启用虚拟库批量生成封面(无素材的跳过)。
func generateAllPlatformCovers(c *gin.Context, state *AppState) {
	var body struct {
		Style   string         `json:"Style"`
		Options map[string]any `json:"Options"`
	}
	_ = c.ShouldBindJSON(&body)
	gen, style, ok := resolveCoverGenerator(body.Style)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "unknown cover style"})
		return
	}
	ctx := c.Request.Context()
	platforms, err := models.GetEnabledPlatforms(ctx, state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	generated, skipped := 0, 0
	for i := range platforms {
		if _, err := renderAndSaveVirtualCover(ctx, state, &platforms[i], gen, body.Options); err != nil {
			skipped++
			if !errors.Is(err, coverart.ErrNoPosters) {
				slog.Warn("[Platform] cover gen failed", "platform", platforms[i].PlatformName, "error", err)
			}
			continue
		}
		generated++
	}
	invalidateViewsCache(c, state)
	c.JSON(http.StatusOK, gin.H{"generated": generated, "skipped": skipped, "style": style})
}

func resolveCoverGenerator(style string) (coverart.Generator, string, bool) {
	style = strings.TrimSpace(style)
	if style == "" {
		if list := coverart.List(); len(list) > 0 {
			style = list[0].Name()
		}
	}
	gen, ok := coverart.Get(style)
	return gen, style, ok
}

// generateAllLibraryCovers 统一生成所有普通媒体库封面。
// POST /Library/CoverArt/GenerateAll
// body: { "Style": string, "Options"?: map[string]any }
func generateAllLibraryCovers(c *gin.Context) {
	state := GetState(c)
	ctx := c.Request.Context()

	var body generateLibraryCoverBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	gen, style, ok := resolveCoverGenerator(body.Style)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "unknown cover style: " + strings.TrimSpace(body.Style)})
		return
	}

	libs, err := models.GetAllLibraries(ctx, state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	type item struct {
		ID      string `json:"Id"`
		Name    string `json:"Name"`
		Status  string `json:"Status"`
		Message string `json:"Message,omitempty"`
	}
	items := make([]item, 0, len(libs))
	success, skipped, failed := 0, 0, 0
	for i := range libs {
		lib := &libs[i]
		tag, err := renderAndSaveLibraryCover(ctx, state, lib, gen, body.Options)
		if err == nil {
			success++
			items = append(items, item{ID: lib.ID.String(), Name: lib.Name, Status: "success", Message: tag})
			continue
		}
		if err == coverart.ErrFontMissing {
			c.JSON(http.StatusFailedDependency, gin.H{"message": err.Error()})
			return
		}
		if err == coverart.ErrNoPosters || err == coverart.ErrBusy {
			skipped++
			items = append(items, item{ID: lib.ID.String(), Name: lib.Name, Status: "skipped", Message: coverBatchMessage(err)})
			continue
		}
		failed++
		items = append(items, item{ID: lib.ID.String(), Name: lib.Name, Status: "failed", Message: err.Error()})
	}

	if success > 0 {
		invalidateViewsCache(c, state)
	}
	c.JSON(http.StatusOK, gin.H{
		"Style":   style,
		"Total":   len(libs),
		"Success": success,
		"Skipped": skipped,
		"Failed":  failed,
		"Items":   items,
	})
}

func coverBatchMessage(err error) string {
	switch err {
	case coverart.ErrNoPosters:
		return "媒体库暂无可用海报素材"
	case coverart.ErrBusy:
		return "已有生成任务进行中"
	default:
		return err.Error()
	}
}

type updateLibraryInfoBody struct {
	ID             string `json:"Id"`
	Name           string `json:"Name"`
	CollectionType string `json:"CollectionType"`
}

func updateLibraryInfo(c *gin.Context) {
	state := GetState(c)
	var body updateLibraryInfoBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	id, err := uuid.Parse(body.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid Id"})
		return
	}
	ctx := c.Request.Context()
	if body.Name != "" {
		name := strings.TrimSpace(body.Name)
		if _, err := models.UpdateLibrary(ctx, state.DB, id, &name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	if body.CollectionType != "" {
		_, err := state.DB.Exec(ctx, "UPDATE libraries SET collection_type = $1 WHERE id = $2", body.CollectionType, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	invalidateViewsCache(c, state)
	lib, err := models.GetLibraryByID(ctx, state.DB, id)
	if err != nil || lib == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Library not found"})
		return
	}
	c.JSON(http.StatusOK, lib)
}

func browseDirGet(c *gin.Context) {
	p := strings.TrimSpace(c.Query("path"))
	if p == "" {
		p = strings.TrimSpace(c.Query("Path"))
	}
	if p == "" {
		p = "/mnt"
	}
	p = filepath.Clean(p)

	entries, err := os.ReadDir(p)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	type dirEntry struct {
		Name string `json:"Name"`
		Path string `json:"Path"`
	}
	dirs := make([]dirEntry, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		full := filepath.Join(p, e.Name())
		dirs = append(dirs, dirEntry{
			Name: e.Name(),
			Path: full,
		})
	}
	c.JSON(http.StatusOK, gin.H{"Path": p, "Directories": dirs})
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
	sid := state.Config.ServerID
	result := make(map[string][]dto.BaseItemDto)

	for _, rawID := range strings.Split(libIDsRaw, ",") {
		libID := strings.TrimSpace(rawID)
		if libID == "" {
			continue
		}
		rows, err := models.GetLatestItems(ctx, state.DB, libID, limit)
		if err != nil {
			continue
		}
		items := make([]dto.BaseItemDto, 0, len(rows))
		for i := range rows {
			items = append(items, dto.FormatItemDto(&rows[i], sid, nil))
		}
		result[libID] = items
	}

	c.JSON(http.StatusOK, result)
}

func invalidateViewsCache(c *gin.Context, state *AppState) {
	state.Cache.Del(c.Request.Context(), "views:all")
}

// ============ Library Sort Order ============

func updateLibrarySortOrder(c *gin.Context, state *AppState) {
	var body []models.LibrarySortItem
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request body"})
		return
	}
	if err := models.BatchUpdateLibrarySortOrder(c.Request.Context(), state.DB, body); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

// updateDisplayOrder 整体重写实际库 + 虚拟库的统一展示顺序。
// POST /Library/DisplayOrder  body: [{Kind:"library"|"platform", Id}]
func updateDisplayOrder(c *gin.Context, state *AppState) {
	var body []models.DisplayOrderEntry
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request body"})
		return
	}
	if err := models.SetDisplayOrder(c.Request.Context(), state.DB, body); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

// ============ Platform Libraries ============

func getPlatforms(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	platforms, err := models.GetPlatformLibraries(ctx, state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	var globalEnabled *string
	_ = state.DB.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'platform_libraries_enabled'").Scan(&globalEnabled)

	items := make([]gin.H, 0, len(platforms))
	for _, p := range platforms {
		entry := gin.H{
			"Id":             p.ID,
			"PlatformName":   p.PlatformName,
			"DisplayName":    p.EffectiveDisplayName(),
			"CustomName":     p.DisplayName,
			"Enabled":        p.Enabled,
			"CollectionType": p.CollectionType,
			"ItemCount":      p.ItemCount,
			"SortOrder":      p.SortOrder,
			"Dimension":      p.Dimension,
			"MatchValue":     p.MatchValue,
			"MatchValues":    p.Values(),
			"HasCover":       p.CoverImagePath != nil && *p.CoverImagePath != "",
		}
		// 封面优先用生成封面(虚拟 ID 出图),否则已知平台用内置 logo
		if p.CoverImagePath != nil && *p.CoverImagePath != "" {
			coverTag := ""
			if p.CoverImageTag != nil {
				coverTag = *p.CoverImageTag
			}
			entry["CoverUrl"] = "/Items/" + models.PlatformVirtualID(p.Dimension, p.MatchValue) + "/Images/Primary?tag=" + url.QueryEscape(coverTag)
		} else if models.HasPlatformLogo(p.PlatformName) {
			entry["LogoUrl"] = "/Library/Platforms/Logo?name=" + url.QueryEscape(p.PlatformName)
		}
		items = append(items, entry)
	}
	c.JSON(http.StatusOK, gin.H{
		"GlobalEnabled": globalEnabled != nil && *globalEnabled == "true",
		"Platforms":     items,
	})
}

// applyVirtualDimension 把虚拟库的维度+匹配值翻译成 QueryItems 的对应过滤项。
func applyVirtualDimension(opts *models.ItemQueryOptions, p *models.PlatformLibrary) {
	vals := p.Values()
	switch p.Dimension {
	case models.PlatformDimActor:
		opts.ActorName = vals
	case models.PlatformDimNumPrefix:
		opts.CatalogPrefix = vals
	default:
		opts.Studio = vals
	}
}

func addPlatform(c *gin.Context, state *AppState) {
	var body struct {
		PlatformName string `json:"PlatformName"`
		Dimension    string `json:"Dimension"`  // 可选,默认 studio
		MatchValue   string `json:"MatchValue"` // 可选,默认 = PlatformName
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.PlatformName) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "PlatformName required"})
		return
	}
	matchValue := strings.TrimSpace(body.MatchValue)
	if matchValue == "" {
		matchValue = strings.TrimSpace(body.PlatformName)
	}
	if err := models.AddPlatformLibrary(c.Request.Context(), state.DB,
		body.Dimension, matchValue, strings.TrimSpace(body.PlatformName), false); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

// discoverPlatformDimension GET /Library/Platforms/Discover?dimension=&search=&minCount=
// 扫描本地数据列出某维度的 distinct 值 + 计数,供前端勾选添加。
func discoverPlatformDimension(c *gin.Context, state *AppState) {
	dimension := strings.TrimSpace(c.Query("dimension"))
	if dimension == "" {
		dimension = models.PlatformDimStudio
	}
	search := c.Query("search")
	var minCount int64 = 1
	if v := strings.TrimSpace(c.Query("minCount")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			minCount = n
		}
	}
	values, err := models.DiscoverDimensionValues(c.Request.Context(), state.DB, dimension, search, minCount)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"dimension": dimension, "values": values})
}

// addPlatformsBatch POST /Library/Platforms/Batch  body: {Dimension, Values:[...]}
// 批量把选中的维度值添加为虚拟库(默认 enabled=false)。
func addPlatformsBatch(c *gin.Context, state *AppState) {
	var body struct {
		Dimension string   `json:"Dimension"`
		Values    []string `json:"Values"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.Values) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Dimension and Values required"})
		return
	}
	ctx := c.Request.Context()
	added := 0
	for _, v := range body.Values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if err := models.AddPlatformLibrary(ctx, state.DB, body.Dimension, v, v, false); err != nil {
			slog.Warn("[Platform] batch add failed", "dimension", body.Dimension, "value", v, "error", err)
			continue
		}
		added++
	}
	invalidateViewsCache(c, state)
	c.JSON(http.StatusOK, gin.H{"added": added})
}

func setPlatformEnabled(c *gin.Context, state *AppState, enabled bool) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "id required"})
		return
	}
	if err := models.SetPlatformEnabledByID(c.Request.Context(), state.DB, id, enabled); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

// renamePlatform 设置虚拟库自定义显示名。POST /Library/Platforms/:id/Rename  body: {Name}
// Name 为空串则清除自定义名,回退默认本地化名。
func renamePlatform(c *gin.Context, state *AppState) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "id required"})
		return
	}
	var body struct {
		Name string `json:"Name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	if err := models.RenamePlatform(c.Request.Context(), state.DB, id, body.Name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

// addPlatformValues 把若干匹配值合并进某虚拟库(多维聚合,解决簡繁/译名拆库)。
// POST /Library/Platforms/:id/Values  body: {Values:[...]}
func addPlatformValues(c *gin.Context, state *AppState) {
	id := c.Param("id")
	var body struct {
		Values []string `json:"Values"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.Values) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Values required"})
		return
	}
	if err := models.AddPlatformValues(c.Request.Context(), state.DB, id, body.Values); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

// removePlatformValue 从某虚拟库移除一个匹配值(主匹配值不可移除)。
// DELETE /Library/Platforms/:id/Values?value=
func removePlatformValue(c *gin.Context, state *AppState) {
	id := c.Param("id")
	value := strings.TrimSpace(c.Query("value"))
	if value == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "value required"})
		return
	}
	if err := models.RemovePlatformValue(c.Request.Context(), state.DB, id, value); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

func deletePlatform(c *gin.Context, state *AppState) {
	id := c.Param("id")
	if err := models.DeletePlatformLibrary(c.Request.Context(), state.DB, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

func updatePlatformSortOrder(c *gin.Context, state *AppState) {
	var body struct {
		OrderedIds []string `json:"OrderedIds"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.OrderedIds) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "OrderedIds required"})
		return
	}
	if err := models.UpdatePlatformSortOrder(c.Request.Context(), state.DB, body.OrderedIds); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

func scanPlatformStudios(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()

	platformScanState.mu.Lock()
	if platformScanState.running {
		platformScanState.mu.Unlock()
		c.JSON(http.StatusOK, gin.H{"message": "already running"})
		return
	}
	platformScanState.running = true
	platformScanState.mu.Unlock()
	defer func() {
		if c.Writer.Written() && c.Writer.Status() >= 400 {
			platformScanState.mu.Lock()
			platformScanState.running = false
			platformScanState.mu.Unlock()
		}
	}()

	rescan := strings.EqualFold(c.Query("rescan"), "true")
	items, err := models.GetItemsPendingPlatformScan(ctx, state.DB, 50000, true, rescan)
	if err != nil {
		platformScanState.mu.Lock()
		platformScanState.running = false
		platformScanState.mu.Unlock()
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	if len(items) == 0 {
		platformScanState.mu.Lock()
		platformScanState.running = false
		platformScanState.mu.Unlock()
		noTmdbCount, _ := models.CountItemsPendingPlatformMetadataScrape(ctx, state.DB)
		c.JSON(http.StatusOK, gin.H{
			"message":          "no_items",
			"total":            0,
			"needs_scrape":     noTmdbCount,
			"needs_scrape_msg": fmt.Sprintf("有 %d 个项目尚未刮削 TMDB，需先执行全量刮削才能获取平台信息", noTmdbCount),
		})
		return
	}

	go func() {
		defer func() {
			platformScanState.mu.Lock()
			platformScanState.running = false
			platformScanState.mu.Unlock()
		}()
		bgCtx := context.Background()
		client := services.TmdbClientFromConfig(bgCtx, state.DB)
		if client == nil {
			slog.Error("[PlatformScan] Failed to create TMDB client, check API key config")
			return
		}

		type result struct {
			id       string
			itemType string
			studio   *string
			failed   bool
			errMsg   string
		}

		sem := make(chan struct{}, 5)
		results := make(chan result, len(items))

		for _, item := range items {
			sem <- struct{}{}
			go func(it models.PlatformScanItem) {
				defer func() { <-sem }()
				studio, fetchErr := services.RefreshPlatformOnlyByTMDBID(bgCtx, state.DB, it.ID, client)
				if fetchErr != nil {
					_ = models.MarkPlatformScanError(bgCtx, state.DB, it.ID, models.PlatformScanSourceTMDB, fetchErr.Error())
					results <- result{id: it.ID, itemType: it.ItemType, studio: nil, failed: true, errMsg: fetchErr.Error()}
					return
				}
				results <- result{id: it.ID, itemType: it.ItemType, studio: studio}
			}(item)
		}

		matched, noMatch, fetchErrors := 0, 0, 0
		for i := 0; i < len(items); i++ {
			r := <-results
			if r.failed {
				fetchErrors++
				continue
			}
			if r.studio == nil {
				noMatch++
			} else {
				matched++
			}
		}

		slog.Info("[PlatformScan] Done", "total", len(items), "matched", matched, "no_platform", noMatch, "fetch_errors", fetchErrors)
	}()

	c.JSON(http.StatusOK, gin.H{"message": "scanning", "total": len(items)})
}

// scanPlatformByFilename fills studio from filename patterns for items still missing studio.
func scanPlatformByFilename(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()

	patterns := []struct {
		platform string
		sql      string
	}{
		{"Netflix", "file_path ILIKE '%%.NF.%%' OR file_path ILIKE '%%Netflix%%'"},
		{"Disney+", "file_path ILIKE '%%.DSNP.%%' OR file_path ILIKE '%%Disney+%%'"},
		{"Apple TV+", "file_path ILIKE '%%.ATVP.%%' OR file_path ILIKE '%%Apple TV%%'"},
		{"Amazon", "file_path ILIKE '%%.AMZN.%%' OR file_path ILIKE '%%Amazon%%'"},
		{"HBO", "file_path ILIKE '%%.HMAX.%%' OR file_path ILIKE '%%.HBO.%%'"},
		{"Hulu", "file_path ILIKE '%%.HULU.%%'"},
		{"Paramount+", "file_path ILIKE '%%.PMTP.%%' OR file_path ILIKE '%%Paramount+%%'"},
		{"Peacock", "file_path ILIKE '%%.PCOK.%%'"},
		{"Crunchyroll", "file_path ILIKE '%%.CR.%%' OR file_path ILIKE '%%Crunchyroll%%'"},
	}

	total := 0
	for _, p := range patterns {
		tag, err := state.DB.Exec(ctx, fmt.Sprintf(
			`UPDATE items
			    SET studio = $1,
			        platform_scan_status = 'matched',
			        platform_scan_source = 'filename',
			        platform_scan_error = NULL,
			        platform_scanned_at = NOW()
			  WHERE type IN ('Movie', 'Series', 'Season', 'Episode')
			    AND platform_scan_status IN ('pending', 'unidentified', 'error', 'no_match')
			    AND (%s)`,
			p.sql), models.CanonicalPlatformName(p.platform))
		if err != nil {
			slog.Warn("[PlatformFilename] update failed", "platform", p.platform, "error", err)
			continue
		}
		total += int(tag.RowsAffected())

		_, err = state.DB.Exec(ctx, `UPDATE items
		    SET studio = $1,
		        platform_scan_status = 'matched',
		        platform_scan_source = 'filename',
		        platform_scan_error = NULL,
		        platform_scanned_at = NOW()
		  WHERE type = 'Series' AND id IN (
			SELECT DISTINCT series_id FROM items WHERE studio = $1 AND series_id IS NOT NULL
		  )`, models.CanonicalPlatformName(p.platform))
		if err != nil {
			slog.Warn("[PlatformFilename] propagate series failed", "platform", p.platform, "error", err)
		}
		_, err = state.DB.Exec(ctx, `UPDATE items
		    SET studio = $1,
		        platform_scan_status = 'matched',
		        platform_scan_source = 'filename',
		        platform_scan_error = NULL,
		        platform_scanned_at = NOW()
		  WHERE type = 'Season' AND parent_id IN (
			SELECT id FROM items WHERE studio = $1 AND type = 'Series'
		  )`, models.CanonicalPlatformName(p.platform))
		if err != nil {
			slog.Warn("[PlatformFilename] propagate season failed", "platform", p.platform, "error", err)
		}
	}

	invalidateViewsCache(c, state)
	slog.Info("[PlatformFilename] Done", "updated", total)
	c.JSON(http.StatusOK, gin.H{"message": "done", "updated": total})
}

// Rescrape progress tracking
var rescrapeProgress struct {
	mu         sync.Mutex
	Running    bool  `json:"running"`
	Total      int64 `json:"total"`
	Success    int64 `json:"success"`
	NotFound   int64 `json:"not_found"`   // TMDB search returned no results
	FetchError int64 `json:"fetch_error"` // API timeout/network error
	Processed  int64 `json:"processed"`
}

var platformScanState struct {
	mu      sync.Mutex
	running bool
}

func getRescrapeProgress(c *gin.Context, state *AppState) {
	c.JSON(http.StatusOK, buildRescrapeProgressResponse(c.Request.Context(), state))
}

// rescrapeMissingStudio reprocesses items still pending/error for platform identification.
func rescrapeMissingStudio(c *gin.Context, state *AppState) {
	rescrapeProgress.mu.Lock()
	if rescrapeProgress.Running {
		rescrapeProgress.mu.Unlock()
		c.JSON(http.StatusOK, gin.H{"message": "already running", "total": atomic.LoadInt64(&rescrapeProgress.Total)})
		return
	}
	rescrapeProgress.Running = true
	rescrapeProgress.mu.Unlock()

	ctx := c.Request.Context()

	items, err := models.GetItemsPendingPlatformScan(ctx, state.DB, 0, false, false)
	if err != nil {
		rescrapeProgress.mu.Lock()
		rescrapeProgress.Running = false
		rescrapeProgress.mu.Unlock()
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	totalCount := int64(len(items))

	if totalCount == 0 {
		rescrapeProgress.mu.Lock()
		rescrapeProgress.Running = false
		rescrapeProgress.mu.Unlock()
		c.JSON(http.StatusOK, gin.H{"message": "no items to rescrape", "total": 0})
		return
	}

	atomic.StoreInt64(&rescrapeProgress.Total, totalCount)
	atomic.StoreInt64(&rescrapeProgress.Success, 0)
	atomic.StoreInt64(&rescrapeProgress.NotFound, 0)
	atomic.StoreInt64(&rescrapeProgress.FetchError, 0)
	atomic.StoreInt64(&rescrapeProgress.Processed, 0)

	go func() {
		defer func() {
			rescrapeProgress.mu.Lock()
			rescrapeProgress.Running = false
			rescrapeProgress.mu.Unlock()
		}()
		bgCtx := context.Background()
		client := services.TmdbClientFromConfig(bgCtx, state.DB)
		if client == nil {
			slog.Error("[Rescrape] Failed to create TMDB client")
			return
		}

		batchSize := 500
		for start := 0; start < len(items); start += batchSize {
			end := start + batchSize
			if end > len(items) {
				end = len(items)
			}
			batch := items[start:end]
			sem := make(chan struct{}, 3)
			var wg sync.WaitGroup
			for _, item := range batch {
				sem <- struct{}{}
				wg.Add(1)
				go func(scanItem models.PlatformScanItem) {
					defer func() { <-sem; wg.Done() }()
					var err error
					if scanItem.TmdbID != nil && *scanItem.TmdbID != 0 {
						_, err = services.RefreshItemMetadataByTMDBID(bgCtx, state.DB, scanItem.ID, client)
					} else {
						_, err = services.ScrapeItemWithClient(bgCtx, state.DB, scanItem.ID, client)
					}
					if err != nil {
						errMsg := err.Error()
						if strings.Contains(errMsg, "no platform matched") {
							atomic.AddInt64(&rescrapeProgress.NotFound, 1)
							_ = models.MarkPlatformScanNoMatch(bgCtx, state.DB, scanItem.ID, models.PlatformScanSourceSearch, errMsg)
						} else if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "no TMDB ID") || strings.Contains(errMsg, "no results") || strings.Contains(errMsg, "no match") || strings.Contains(errMsg, "identify failed") {
							atomic.AddInt64(&rescrapeProgress.NotFound, 1)
							_ = models.MarkPlatformScanUnidentified(bgCtx, state.DB, scanItem.ID, models.PlatformScanSourceSearch, errMsg)
						} else {
							atomic.AddInt64(&rescrapeProgress.FetchError, 1)
							_ = models.MarkPlatformScanError(bgCtx, state.DB, scanItem.ID, models.PlatformScanSourceSearch, errMsg)
						}
					} else {
						atomic.AddInt64(&rescrapeProgress.Success, 1)
					}
					atomic.AddInt64(&rescrapeProgress.Processed, 1)
				}(item)
			}
			wg.Wait()
		}

		s := atomic.LoadInt64(&rescrapeProgress.Success)
		nf := atomic.LoadInt64(&rescrapeProgress.NotFound)
		fe := atomic.LoadInt64(&rescrapeProgress.FetchError)
		slog.Info("[Rescrape] Done", "total", totalCount, "success", s, "not_found", nf, "fetch_error", fe)
	}()

	c.JSON(http.StatusOK, gin.H{"message": "rescraping", "total": totalCount})
}
