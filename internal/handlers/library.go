package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
	"fyms/internal/middleware"
	"fyms/internal/models"
	"fyms/internal/services"
)

func RegisterLibraryRoutes(group *gin.RouterGroup, state *AppState, authMW, adminMW, optAuthMW gin.HandlerFunc) {
	u := group.Group("")
	u.GET("/Users/:userId/Views", authMW, getUserViews)
	u.GET("/Users/:userId/Items", authMW, getItems)
	u.GET("/Users/:userId/Items/Resume", authMW, getResumeItems)
	u.GET("/Users/:userId/Items/Latest", authMW, getLatestItems)
	u.GET("/Users/:userId/Items/:itemId", authMW, getItemDetail)

	u.GET("/Items/:itemId/Similar", optAuthMW, getSimilarItems)

	u.POST("/Library/VirtualFolders", adminMW, addLibrary)
	u.DELETE("/Library/VirtualFolders", adminMW, deleteLibrary)
	u.POST("/Library/VirtualFolders/Name", adminMW, renameLibrary)
	u.POST("/Library/VirtualFolders/Paths", adminMW, addLibraryPath)
	u.DELETE("/Library/VirtualFolders/Paths", adminMW, removeLibraryPath)

	u.POST("/Items/:itemId/Images/:imageType", adminMW, uploadImage)
	u.DELETE("/Items/:itemId/Images/:imageType", adminMW, deleteImage)

	u.POST("/Library/Refresh", adminMW, refreshAll)
	u.POST("/Items/:itemId/Refresh", adminMW, scrapeItem)

	u.GET("/Library/VirtualFolders", authMW, getVirtualFolders)
	u.GET("/Library/VirtualFolders/:id", authMW, getVirtualFolderDetail)
	u.POST("/Library/VirtualFolders/Add", adminMW, addLibrary)
	u.POST("/Library/VirtualFolders/Update", adminMW, updateLibraryInfo)
	u.POST("/Library/VirtualFolders/:id/Refresh", adminMW, refreshSingleLibrary)
	u.POST("/Library/VirtualFolders/:id/Image", adminMW, uploadLibraryImage)
	u.DELETE("/Library/VirtualFolders/:id/Image", adminMW, deleteLibraryImage)
	u.GET("/Library/Scan/Progress", getScanProgress)

	u.POST("/Library/Probe/Start", adminMW, startProbe)
	u.POST("/Library/Probe/Stop", adminMW, stopProbe)
	u.GET("/Library/Probe/Progress", getProbeProgress)

	u.POST("/Items/:itemId/Scrape", adminMW, scrapeItem)
	u.POST("/Library/Scrape/All", adminMW, scrapeAll)
	u.POST("/Library/Scrape/Stop", adminMW, stopScrape)
	u.GET("/Library/Scrape/Progress", getScrapeProgress)
	u.GET("/Library/Scrape/Missing", getMissingScrapeCount)

	u.POST("/Library/Browse", adminMW, browseDir)
	u.GET("/Library/BrowseDirectories", adminMW, browseDirGet)

	u.POST("/Library/Refresh/Metadata", adminMW, scrapeAll)

	u.GET("/Users/:userId/Items/LatestBatch", authMW, getLatestBatch)

	u.GET("/Genres", getGenres)
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

func getUserViews(c *gin.Context) {
	state := GetState(c)
	userID := c.Param("userId")
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

	out := make([]gin.H, 0, len(libs))
	sid := state.Config.ServerID
	for _, lib := range libs {
		idStr := lib.ID.String()

		var childCount int64
		state.DB.QueryRow(ctx,
			"SELECT COUNT(*) FROM items WHERE library_id = $1 AND type IN ('Movie','Series')", lib.ID).Scan(&childCount)
		var recursiveCount int64
		state.DB.QueryRow(ctx,
			"SELECT COUNT(*) FROM items WHERE library_id = $1", lib.ID).Scan(&recursiveCount)

		imageTags := gin.H{}
		if lib.PrimaryImageTag != nil {
			imageTags["Primary"] = *lib.PrimaryImageTag
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
				"PlayCount":            0,
				"IsFavorite":           false,
				"Played":               false,
				"UnplayedItemCount":    childCount,
			},
		}
		if len(lib.Paths) > 0 {
			entry["Path"] = lib.Paths[0]
		}
		out = append(out, entry)
	}

	resp := gin.H{
		"Items":            out,
		"TotalRecordCount": len(out),
	}
	state.Cache.SetJSON(ctx, cacheKey, resp, 24*time.Hour)
	c.JSON(http.StatusOK, resp)
}

func parseItemQueryOptions(c *gin.Context, userID string) (*models.ItemQueryOptions, error) {
	opts := &models.ItemQueryOptions{}

	if pid := strings.TrimSpace(c.Query("ParentId")); pid != "" {
		opts.ParentID = &pid
	}
	if s := strings.TrimSpace(c.Query("IncludeItemTypes")); s != "" {
		for _, t := range strings.Split(s, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				opts.IncludeItemTypes = append(opts.IncludeItemTypes, t)
			}
		}
	}
	if s := strings.TrimSpace(c.Query("SortBy")); s != "" {
		opts.SortBy = &s
	}
	if s := strings.TrimSpace(c.Query("SortOrder")); s != "" {
		opts.SortOrder = &s
	}
	if s := strings.TrimSpace(c.Query("Limit")); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, err
		}
		opts.Limit = &n
	}
	if s := strings.TrimSpace(c.Query("StartIndex")); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, err
		}
		opts.StartIndex = &n
	}
	recursive := strings.EqualFold(c.Query("Recursive"), "true") || c.Query("Recursive") == "1"
	opts.Recursive = recursive

	if s := strings.TrimSpace(c.Query("SearchTerm")); s != "" {
		opts.SearchTerm = &s
	}
	if s := strings.TrimSpace(c.Query("Filters")); s != "" {
		for _, f := range strings.Split(s, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				opts.Filters = append(opts.Filters, f)
			}
		}
	}
	if s := strings.TrimSpace(c.Query("GenreIds")); s != "" {
		for _, g := range strings.Split(s, ",") {
			g = strings.TrimSpace(g)
			if g != "" {
				opts.GenreIDs = append(opts.GenreIDs, g)
			}
		}
	}
	if s := strings.TrimSpace(c.Query("Years")); s != "" {
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

	opts.UserID = &userID
	return opts, nil
}

func getItems(c *gin.Context) {
	state := GetState(c)
	pathUser := c.Param("userId")
	if !matchUserOrAdmin(c, pathUser) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}

	opts, err := parseItemQueryOptions(c, pathUser)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
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
		"Items":             items,
		"TotalRecordCount": res.TotalCount,
	})
}

func getResumeItems(c *gin.Context) {
	state := GetState(c)
	pathUser := c.Param("userId")
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
		"Items":             items,
		"TotalRecordCount": res.TotalCount,
	})
}

func getLatestItems(c *gin.Context) {
	state := GetState(c)
	pathUser := c.Param("userId")
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
	rows, err := models.GetLatestItems(ctx, state.DB, parentID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	sid := state.Config.ServerID
	items := make([]dto.BaseItemDto, 0, len(rows))
	for i := range rows {
		items = append(items, dto.FormatItemDto(&rows[i], sid, nil))
	}

	c.JSON(http.StatusOK, items)
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

	cast, err := models.GetItemCast(ctx, pool, item.ID)
	if err != nil {
		return base, err
	}
	if len(cast) == 0 && seriesItem != nil {
		cast, _ = models.GetItemCast(ctx, pool, seriesItem.ID)
	}
	base.People = cast

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
		`SELECT id::text, name, file_path, COALESCE(container, ''), is_primary, runtime_ticks, bitrate, size, mediainfo
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
		if err := mvRows.Scan(&idStr, &name, &fpath, &container, &isPrimary, &rt, &br, &sz, &mediaInfoJSON); err != nil {
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
		ms := dto.MediaSourceInfo{
			ID:                    idStr,
			Path:                  fpath,
			Protocol:              "File",
			Type:                  "Default",
			Container:             container,
			Name:                  name,
			IsRemote:              false,
			RunTimeTicks:          rt,
			SupportsDirectPlay:    true,
			SupportsDirectStream:  true,
			SupportsTranscoding:   true,
			MediaStreams:           versionStreams,
			Bitrate:               bitrate,
			Size:                  sz,
			ReadAtNativeFramerate: false,
			DirectStreamURL:       fmt.Sprintf("/Videos/%s/stream.%s?MediaSourceId=%s&Static=true", item.ID, container, idStr),
			ETag:                  idStr,
		}
		sources = append(sources, ms)
		mvIdx++
	}
	if err := mvRows.Err(); err != nil {
		return base, err
	}
	if len(sources) == 0 && len(streamDtos) > 0 {
		ms := dto.MediaSourceInfo{
			ID:                   item.ID,
			Path:                 strOrPath(item),
			Protocol:             "File",
			Type:                 "Default",
			Container:            strVal(item.Container),
			Name:                 item.Name,
			IsRemote:             false,
			RunTimeTicks:         item.RuntimeTicks,
			SupportsDirectPlay:   true,
			SupportsDirectStream: true,
			SupportsTranscoding:  true,
			MediaStreams:         streamDtos,
			ReadAtNativeFramerate: false,
		}
		sources = []dto.MediaSourceInfo{ms}
	}
	base.MediaSources = sources

	return base, nil
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
	pathUser := c.Param("userId")
	if !matchUserOrAdmin(c, pathUser) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}

	itemID := c.Param("itemId")
	ctx := c.Request.Context()

	if uid, err := uuid.Parse(itemID); err == nil {
		lib, lerr := models.GetLibraryByID(ctx, state.DB, uid)
		if lerr == nil && lib != nil {
			var childCount int64
			state.DB.QueryRow(ctx,
				"SELECT COUNT(*) FROM items WHERE library_id = $1 AND type IN ('Movie','Series')", uid).Scan(&childCount)
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
					"PlayCount":            0,
					"IsFavorite":           false,
					"Played":               false,
					"UnplayedItemCount":    childCount,
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

	base, err := enrichItemDetail(ctx, state.DB, item, pathUser, state.Config.ServerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, base)
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
	if err := models.DeleteLibrary(c.Request.Context(), state.DB, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
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
	go func() {
		ctx := context.Background()
		services.ScanAllLibraries(ctx, state.DB, state.Cache, state.ScanProgress)
	}()
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
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
		 FROM libraries l JOIN items i ON i.library_id = l.id WHERE i.id = $1::uuid`,
		*resolved).Scan(&lib.ID, &lib.Name, &lib.CollectionType, &lib.Paths, &lib.CreatedAt, &lib.PrimaryImagePath, &lib.PrimaryImageTag)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	go func() {
		bg := context.Background()
		services.ScanLibrary(bg, state.DB, state.Cache, state.ScanProgress, lib.ID.String(), lib.CollectionType, lib.Paths, lib.Name)
	}()
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

func getVirtualFolders(c *gin.Context) {
	state := GetState(c)
	libs, err := models.GetAllLibraries(c.Request.Context(), state.DB)
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
		entry := gin.H{
			"Name":           lib.Name,
			"Locations":      locations,
			"CollectionType": lib.CollectionType,
			"ItemId":         idStr,
			"Guid":           idStr,
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
	if err := state.ProbeTask.Start(state.DB, threads); err != nil {
		c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, state.ProbeTask.GetProgress())
}

func stopProbe(c *gin.Context) {
	state := GetState(c)
	state.ProbeTask.Stop()
	c.JSON(http.StatusOK, state.ProbeTask.GetProgress())
}

func getProbeProgress(c *gin.Context) {
	state := GetState(c)
	prog := state.ProbeTask.GetProgress()
	if prog.Status == "idle" {
		if cnt, err := services.GetMissingMediainfoCount(c.Request.Context(), state.DB); err == nil {
			prog.MissingCount = cnt
		}
	}
	c.JSON(http.StatusOK, prog)
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

func scrapeAll(c *gin.Context) {
	state := GetState(c)
	if err := state.ScrapeTask.Start(c.Request.Context(), state.DB); err != nil {
		c.JSON(http.StatusConflict, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, state.ScrapeTask.GetProgress())
}

func stopScrape(c *gin.Context) {
	state := GetState(c)
	state.ScrapeTask.Stop()
	c.JSON(http.StatusOK, state.ScrapeTask.GetProgress())
}

func getScrapeProgress(c *gin.Context) {
	state := GetState(c)
	c.JSON(http.StatusOK, state.ScrapeTask.GetProgress())
}

func getMissingScrapeCount(c *gin.Context) {
	state := GetState(c)
	var n int64
	err := state.DB.QueryRow(c.Request.Context(),
		`SELECT COUNT(*) FROM items WHERE (overview IS NULL OR overview = '') AND type IN ('Movie', 'Series')`).Scan(&n)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"MissingCount": n})
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
	_ = state.DB.QueryRow(ctx,
		"SELECT COUNT(*) FROM items WHERE library_id = $1 AND type IN ('Movie','Series')", id).Scan(&itemCount)

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
	go func() {
		bg := context.Background()
		services.ScanLibrary(bg, state.DB, state.Cache, state.ScanProgress, lib.ID.String(), lib.CollectionType, lib.Paths, lib.Name)
	}()
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
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
	pathUser := c.Param("userId")
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
