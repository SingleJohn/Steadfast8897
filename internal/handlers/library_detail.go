package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
	"fyms/internal/models"
)

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
	// 仅 resolved 模式才用 MediaSource 的解析路径覆盖 item.Path;
	// strm 模式(默认)保留 FormatItemDto 产出的 .strm 路径,对齐 Emby(item.Path=.strm)。
	if dto.StrmItemPathResolved() && strings.TrimSpace(sources[0].Path) != "" {
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
		        resolution, hdr_format, video_codec, audio_codec, source, quality_label, chapters
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
		var mediaInfoJSON, chaptersJSON []byte
		var resolution, hdrFormat, videoCodec, audioCodec, source, qualityLabel *string
		if err := mvRows.Scan(&idStr, &name, &fpath, &container, &isPrimary, &rt, &br, &sz, &mediaInfoJSON,
			&resolution, &hdrFormat, &videoCodec, &audioCodec, &source, &qualityLabel, &chaptersJSON); err != nil {
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
		versionStreams = appendExternalSubtitleStreams(ctx, pool, item.ID, idStr, versionStreams)
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
			Chapters:              parseChaptersJSON(chaptersJSON),
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
	if len(base.MediaSources) > 0 {
		base.MediaStreams = base.MediaSources[0].MediaStreams
	}

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
			        resolution, hdr_format, video_codec, audio_codec, source, quality_label, chapters
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
			var mediaInfoJSON, chaptersJSON []byte
			var resolution, hdrFormat, videoCodec, audioCodec, source, qualityLabel *string
			if err := mvRows.Scan(&idStr, &name, &fpath, &container, &isPrimary, &rt, &br, &sz, &mediaInfoJSON,
				&resolution, &hdrFormat, &videoCodec, &audioCodec, &source, &qualityLabel, &chaptersJSON); err != nil {
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
			versionStreams = appendExternalSubtitleStreams(ctx, pool, itemID, idStr, versionStreams)
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
				Chapters:              parseChaptersJSON(chaptersJSON),
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
	itemID := c.Param("itemId")
	ctx := c.Request.Context()

	// Emby 里 person 也是 item：/Items/{personId} 返回演员详情（与 /Persons/{Name} 同构）。
	// 必须置于用户/库权限逻辑之前：person 是全局实体不做库级管控（与 /Persons 端点一致），
	// 且用 API Key 鉴权（无具体用户）时下方 loadUserLibraryScope 会因空 userID 报错 500。
	// 第三方刮削器（mdc-ng）取详情/回填前会拉这个 URL。
	if _, perr := uuid.Parse(itemID); perr == nil {
		if person, perr2 := models.GetPersonByID(ctx, state.DB, itemID); perr2 == nil && person != nil {
			userID := resolveUserID(c)
			var ud *dto.UserDataRow
			if userID != "" {
				if u, uerr := models.GetUserPersonData(ctx, state.DB, userID, itemID); uerr == nil {
					ud = u
				}
			}
			c.JSON(http.StatusOK, personDetailDTO(state, person, ud))
			return
		}
	}

	pathUser := resolveUserID(c)
	if !matchUserOrAdmin(c, pathUser) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}

	scope, err := loadUserLibraryScope(ctx, state, pathUser)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	// Check if this is a platform virtual library
	if p, ok := models.ResolvePlatformVirtualID(ctx, state.DB, itemID); ok {
		if !scope.AllowAll {
			c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
			return
		}
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
			if !scope.allowsLibrary(uid.String()) {
				c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
				return
			}
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
	if _, err := uuid.Parse(item.ID); err == nil {
		if ok, err := userCanAccessItem(ctx, state, pathUser, item.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		} else if !ok {
			c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
			return
		}
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
	if _, err := uuid.Parse(item.ID); err == nil {
		if ok, err := userCanAccessItem(ctx, state, pathUser, item.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		} else if !ok {
			c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
			return
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
		// 仅 resolved 模式才用 MediaSource 的解析路径覆盖顶层 Path;
		// strm 模式(默认)保留 FormatItemDto 产出的 .strm 路径,对齐 Emby:
		// item 级 Path 为 .strm,解析后真实路径只出现在 MediaSources[].Path。
		if dto.StrmItemPathResolved() && strings.TrimSpace(base.MediaSources[0].Path) != "" {
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
	limit := int64(12)
	if s := strings.TrimSpace(queryAny(c, "Limit", "limit")); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			limit = n
		}
	}

	resolved, err := models.ResolveToUUID(ctx, state.DB, itemID)
	if err != nil || resolved == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
		return
	}

	var libID string
	err = state.DB.QueryRow(ctx, "SELECT library_id::text FROM items WHERE id = $1::uuid", *resolved).Scan(&libID)
	if err != nil {
		if models.PersonExists(ctx, state.DB, *resolved) {
			c.JSON(http.StatusOK, gin.H{"Items": []dto.BaseItemDto{}, "TotalRecordCount": 0})
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	idRows, err := state.DB.Query(ctx,
		`SELECT id::text FROM items WHERE library_id = $1::uuid AND id <> $2::uuid
		 AND type IN ('Movie', 'Series', 'Episode', 'Video')
		 ORDER BY RANDOM() LIMIT $3::bigint`,
		libID, *resolved, limit)
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
