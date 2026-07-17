package media

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"fyms/internal/dto"
	embysupport "fyms/internal/handlers/mediasupport"
	"fyms/internal/handlers/shared"
	"fyms/internal/middleware"
	"fyms/internal/models"
	"fyms/internal/repository"
	"fyms/internal/services"
)

// RegisterVideoRoutes registers playback and streaming endpoints.
func RegisterVideoRoutes(group *gin.RouterGroup, state *AppState, authMW gin.HandlerFunc) {
	// PlaybackInfo requires authentication
	auth := group.Group("")
	auth.Use(authMW)
	auth.GET("/Items/:itemId/PlaybackInfo", func(c *gin.Context) { getPlaybackInfo(c, state) })
	auth.POST("/Items/:itemId/PlaybackInfo", func(c *gin.Context) { getPlaybackInfo(c, state) })

	// Stream endpoints: NO route-level auth (matches Rust behavior).
	// Auth is handled internally via api_key query param / X-Emby-Token header.
	// This allows 302-redirected clients to access streams without re-authenticating.
	group.GET("/Videos/:itemId/stream", func(c *gin.Context) { streamVideo(c, state) })
	group.GET("/Videos/:itemId/stream.:container", func(c *gin.Context) { streamVideo(c, state) })
	group.GET("/Videos/:itemId/trailer", func(c *gin.Context) { streamTrailer(c, state) })
	group.GET("/SourcePlay/:playSourceUUID/stream", func(c *gin.Context) { streamSourcePlay(c, state) })
	group.GET("/Videos/:itemId/:mediaSourceId/Subtitles/:index/Stream.:format", func(c *gin.Context) { streamSubtitle(c, state) })
	group.GET("/Videos/:itemId/:mediaSourceId/Subtitles/:index/:startPositionTicks/Stream.:format", func(c *gin.Context) { streamSubtitle(c, state) })
}

type mediaVersionRow struct {
	ID           uuid.UUID
	Name         string
	FilePath     string
	Container    *string
	IsPrimary    bool
	RuntimeTicks *int64
	Bitrate      *int64
	Size         *int64
	MediaInfo    []byte

	// M7.4 画质字段
	Resolution   *string
	HDRFormat    *string
	VideoCodec   *string
	AudioCodec   *string
	Source       *string
	QualityLabel *string

	ChaptersJSON []byte
}

func loadMediaVersions(ctx context.Context, state *AppState, itemID string) ([]mediaVersionRow, error) {
	versions, err := state.Repo.Playback.ListMediaVersionsForItem(ctx, itemID)
	if err != nil {
		return nil, err
	}

	out := make([]mediaVersionRow, 0, len(versions))
	for _, v := range versions {
		out = append(out, mediaVersionRow{
			ID:           v.UUID,
			Name:         v.Name,
			FilePath:     v.FilePath,
			Container:    v.Container,
			IsPrimary:    v.IsPrimary,
			RuntimeTicks: v.RuntimeTicks,
			Bitrate:      v.Bitrate,
			Size:         v.Size,
			MediaInfo:    v.MediaInfo,
			Resolution:   v.Resolution,
			HDRFormat:    v.HDRFormat,
			VideoCodec:   v.VideoCodec,
			AudioCodec:   v.AudioCodec,
			Source:       v.Source,
			QualityLabel: v.QualityLabel,
			ChaptersJSON: v.ChaptersJSON,
		})
	}
	return out, nil
}

func playbackJSONInt64(m map[string]interface{}, key string) *int64 {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch n := v.(type) {
	case float64:
		i := int64(n)
		return &i
	case int64:
		i := n
		return &i
	case int:
		i := int64(n)
		return &i
	}
	return nil
}

func nonEmptyStringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func mediaSourceIDFromQuery(c *gin.Context) string {
	query := c.Request.URL.Query()
	for _, key := range []string{"MediaSourceId", "mediaSourceId", "MediaSourceID", "mediasourceid"} {
		if values, ok := query[key]; ok && len(values) > 0 {
			if value := strings.TrimSpace(values[0]); value != "" {
				return value
			}
		}
	}
	for key, values := range query {
		if strings.EqualFold(key, "MediaSourceId") && len(values) > 0 {
			if value := strings.TrimSpace(values[0]); value != "" {
				return value
			}
		}
	}
	return ""
}

func mediaSourceIDFromPlaybackRequest(c *gin.Context) string {
	if msid := mediaSourceIDFromQuery(c); msid != "" {
		return msid
	}
	if c.Request.Body == nil {
		return ""
	}

	var body map[string]interface{}
	if err := json.NewDecoder(io.LimitReader(c.Request.Body, 1<<20)).Decode(&body); err != nil {
		return ""
	}
	for key, value := range body {
		if !strings.EqualFold(key, "MediaSourceId") {
			continue
		}
		if s, ok := value.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func preferMediaSource(sources []dto.MediaSourceInfo, mediaSourceID string) []dto.MediaSourceInfo {
	mediaSourceID = strings.TrimSpace(mediaSourceID)
	if mediaSourceID == "" || len(sources) < 2 {
		return sources
	}
	for i := range sources {
		if sources[i].ID != mediaSourceID {
			continue
		}
		if i == 0 {
			return sources
		}
		selected := sources[i]
		copy(sources[1:i+1], sources[0:i])
		sources[0] = selected
		return sources
	}
	return sources
}

func defaultStreamIndexes(streams []dto.MediaStreamInfo) (*int32, *int32) {
	var defaultAudioIndex *int32
	var defaultSubtitleIndex *int32
	for i := range streams {
		stream := streams[i]
		switch stream.Type {
		case "Audio":
			if defaultAudioIndex == nil {
				index := stream.Index
				defaultAudioIndex = &index
			}
			if stream.IsDefault {
				index := stream.Index
				defaultAudioIndex = &index
			}
		case "Subtitle":
			if defaultSubtitleIndex == nil {
				index := stream.Index
				defaultSubtitleIndex = &index
			}
			if stream.IsDefault {
				index := stream.Index
				defaultSubtitleIndex = &index
			}
		}
	}
	return defaultAudioIndex, defaultSubtitleIndex
}

func backfillMovieDirectoryMediaVersions(ctx context.Context, state *AppState, itemID string, item *dto.ItemRow) ([]mediaVersionRow, error) {
	if item == nil || item.ItemType != "Movie" || item.FilePath == nil || *item.FilePath == "" {
		hasFilePath := false
		if item != nil {
			hasFilePath = item.FilePath != nil && *item.FilePath != ""
		}
		slog.Info("playback backfill skipped", "itemId", itemID, "hasItem", item != nil, "itemType", func() string {
			if item == nil {
				return ""
			}
			return item.ItemType
		}(), "hasFilePath", hasFilePath)
		return nil, nil
	}

	dir := filepath.Dir(*item.FilePath)
	dirCache := services.CacheDir(dir)
	if len(dirCache) == 0 {
		slog.Warn("playback backfill empty dir cache", "itemId", itemID, "dir", dir)
		return nil, nil
	}

	var videoFiles [][2]string
	for _, entry := range dirCache {
		if services.IsVideoExt(filepath.Ext(entry[0])) {
			videoFiles = append(videoFiles, entry)
		}
	}
	slog.Info("playback backfill discovered files", "itemId", itemID, "dir", dir, "videoFileCount", len(videoFiles), "itemFilePath", *item.FilePath)
	if len(videoFiles) == 0 {
		return nil, nil
	}

	primaryIndex := 0
	for i, entry := range videoFiles {
		if entry[1] == *item.FilePath {
			primaryIndex = i
			break
		}
	}
	if primaryIndex != 0 {
		videoFiles[0], videoFiles[primaryIndex] = videoFiles[primaryIndex], videoFiles[0]
	}

	for i, entry := range videoFiles {
		filePath := entry[1]
		versionName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
		if versionName == "" {
			versionName = "Unknown"
		}

		mi := services.ReadMediainfoJSONCached(filePath, dirCache)
		container := strings.TrimPrefix(strings.ToLower(filepath.Ext(filePath)), ".")
		if container == "strm" {
			if rp := services.ResolveStrmPath(filePath); rp != nil {
				resolved := strings.TrimPrefix(filepath.Ext(*rp), ".")
				if resolved != "" {
					container = resolved
				}
			}
		}
		if container == "" && item.Container != nil {
			container = *item.Container
		}
		if container == "" {
			container = "mkv"
		}

		var runtimeTicks, bitrate, size *int64
		var mediaInfoValue interface{}
		if mi != nil {
			runtimeTicks = playbackJSONInt64(mi, "RunTimeTicks")
			bitrate = playbackJSONInt64(mi, "Bitrate")
			size = playbackJSONInt64(mi, "Size")
			if raw, err := json.Marshal(mi); err == nil {
				mediaInfoValue = string(raw)
			}
		}

		q, qLabel := services.ComputeMediaVersionQuality(filepath.Base(filePath), mi)

		mvID, err := state.Repo.Playback.UpsertMediaVersion(ctx, repository.MediaVersionUpsert{
			ItemID:       itemID,
			Name:         versionName,
			FilePath:     filePath,
			Container:    container,
			IsPrimary:    i == 0,
			MediaInfo:    mediaInfoValue,
			RuntimeTicks: runtimeTicks,
			Bitrate:      bitrate,
			Size:         size,
			Resolution:   nonEmptyStringPtr(q.Resolution),
			HDRFormat:    nonEmptyStringPtr(q.HDRFormat),
			VideoCodec:   nonEmptyStringPtr(q.VideoCodec),
			AudioCodec:   nonEmptyStringPtr(q.AudioCodec),
			Source:       nonEmptyStringPtr(q.Source),
			QualityLabel: nonEmptyStringPtr(qLabel),
		})
		if err != nil {
			slog.Warn("playback backfill insert failed", "itemId", itemID, "filePath", filePath, "error", err)
			return nil, err
		}
		if parsedItemID, perr := uuid.Parse(itemID); perr == nil {
			services.SyncExternalSubtitles(ctx, state.DB, parsedItemID, mvID, filePath, dirCache)
		}
	}
	slog.Info("playback backfill inserted versions", "itemId", itemID, "count", len(videoFiles))

	return loadMediaVersions(ctx, state, itemID)
}

func getPlaybackInfo(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	itemID := c.Param("itemId")
	selectedMediaSourceID := mediaSourceIDFromPlaybackRequest(c)
	if handleSourcePlaybackInfo(c, state, itemID, selectedMediaSourceID) {
		return
	}
	uid, err := models.ResolveToUUID(ctx, state.DB, itemID)
	if err != nil || uid == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid item id"})
		return
	}

	item, err := models.GetItemByID(ctx, state.DB, *uid)
	if err != nil || item == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Item not found"})
		return
	}

	// If this is a merged secondary, redirect to the primary item
	mergedToID, _ := state.Repo.Playback.GetMergedPrimaryID(ctx, *uid)
	if mergedToID != nil && *mergedToID != "" {
		primary, perr := models.GetItemByID(ctx, state.DB, *mergedToID)
		if perr == nil && primary != nil {
			item = primary
			uid = &primary.ID
		}
	}

	authUser := middleware.GetAuthUser(c)
	if authUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
		return
	}
	if ok, err := shared.UserCanAccessItem(ctx, state, authUser.ID, *uid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	} else if !ok {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}

	var policy *repository.UserPolicy
	if !strings.HasPrefix(authUser.ID, "api-key-") {
		userUUID, err := uuid.Parse(authUser.ID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
			return
		}
		policy, err = state.Repo.Users.GetUserPolicy(ctx, userUUID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Policy error"})
			return
		}
	}
	if policy != nil && !policy.EnableMediaPlayback {
		c.JSON(http.StatusForbidden, gin.H{"message": "Playback disabled"})
		return
	}
	if policy != nil && policy.SimultaneousStreamLimit > 0 {
		if n := state.SessionManager.CountActiveStreams(authUser.ID); int32(n) >= policy.SimultaneousStreamLimit {
			c.JSON(http.StatusTooManyRequests, gin.H{"message": "Too many simultaneous streams"})
			return
		}
	}

	versions, err := loadMediaVersions(ctx, state, *uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	if len(versions) <= 1 {
		if refreshed, err := backfillMovieDirectoryMediaVersions(ctx, state, *uid, item); err == nil && len(refreshed) > len(versions) {
			versions = refreshed
		} else if err != nil {
			slog.Warn("backfill movie media_versions failed", "itemId", *uid, "error", err)
		}
	}

	if len(versions) == 0 && item.FilePath != nil && *item.FilePath != "" {
		cn := item.Container
		versions = append(versions, mediaVersionRow{
			ID:           uuid.Nil,
			Name:         "Default",
			FilePath:     *item.FilePath,
			Container:    cn,
			IsPrimary:    true,
			RuntimeTicks: item.RuntimeTicks,
		})
	}

	streamRows, err := models.GetMediaStreams(ctx, state.DB, *uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	mediaStreams := make([]dto.MediaStreamInfo, 0, len(streamRows))
	for i := range streamRows {
		mediaStreams = append(mediaStreams, dto.FormatMediaStreamDto(&streamRows[i]))
	}

	var sources []dto.MediaSourceInfo
	versionUserData := map[string]repository.MediaVersionUserData{}
	if authUser != nil {
		if rows, err := state.Repo.MediaVersionUserData.ListForItem(ctx, authUser.ID, *uid); err == nil {
			versionUserData = rows
		}
	}
	for idx, mv := range versions {
		msid := mv.ID.String()
		if mv.ID == uuid.Nil {
			msid = *uid
		}

		actualPath := mv.FilePath
		actualContainer := ""
		if mv.Container != nil {
			actualContainer = *mv.Container
		}
		protocol := "File"
		isRemote := false

		if strings.HasSuffix(strings.ToLower(mv.FilePath), ".strm") {
			if rp := resolveStrmPath(mv.FilePath); rp != nil {
				actualPath = rp.filePath
				actualContainer = rp.container
				isRemote = rp.isRemote
				if isRemote {
					protocol = "Http"
				}
			}
		} else if strings.HasPrefix(strings.ToLower(actualPath), "http://") || strings.HasPrefix(strings.ToLower(actualPath), "https://") {
			protocol = "Http"
			isRemote = true
		}

		if actualContainer == "" && item.Container != nil {
			actualContainer = *item.Container
		}

		versionStreams := mediaStreams
		if len(mv.MediaInfo) > 0 {
			var mi map[string]json.RawMessage
			if json.Unmarshal(mv.MediaInfo, &mi) == nil {
				if msRaw, ok := mi["MediaStreams"]; ok {
					var miStreams []dto.MediaStreamInfo
					if json.Unmarshal(msRaw, &miStreams) == nil && len(miStreams) > 0 {
						versionStreams = miStreams
					}
				}
			}
		}
		if len(versionStreams) == 0 && idx == 0 {
			versionStreams = mediaStreams
		}
		versionStreams = AppendExternalSubtitleStreams(ctx, state.DB, *uid, msid, versionStreams)
		defaultAudioIndex, defaultSubtitleIndex := defaultStreamIndexes(versionStreams)

		src := dto.MediaSourceInfo{
			ID:                         msid,
			Path:                       actualPath,
			Protocol:                   protocol,
			Type:                       "Default",
			Container:                  actualContainer,
			Name:                       mv.Name,
			IsRemote:                   isRemote,
			RunTimeTicks:               mv.RuntimeTicks,
			SupportsDirectPlay:         true,
			SupportsDirectStream:       true,
			SupportsTranscoding:        false,
			MediaStreams:               versionStreams,
			ReadAtNativeFramerate:      false,
			Size:                       mv.Size,
			DirectStreamURL:            fmt.Sprintf("/Videos/%s/stream.%s?MediaSourceId=%s&Static=true", *uid, actualContainer, msid),
			ETag:                       msid,
			Formats:                    []string{},
			DefaultAudioStreamIndex:    defaultAudioIndex,
			DefaultSubtitleStreamIndex: defaultSubtitleIndex,
			FymsResolution:             mv.Resolution,
			FymsHdrFormat:              mv.HDRFormat,
			FymsVideoCodec:             mv.VideoCodec,
			FymsAudioCodec:             mv.AudioCodec,
			FymsSource:                 mv.Source,
			FymsQualityLabel:           mv.QualityLabel,
		}
		if mv.Bitrate != nil {
			b := int64(*mv.Bitrate)
			src.Bitrate = &b
		}
		if data, ok := versionUserData[msid]; ok {
			ApplyMediaSourceUserData(&src, &data)
		}
		ApplyMediaSourceCompatDefaults(&src, *uid)
		sources = append(sources, src)
	}

	// Append MediaSources from merged secondary items
	mergedSources := collectMergedPlaybackSources(ctx, state, *uid, authUser.ID, mediaStreams)
	if len(mergedSources) > 0 {
		sources = append(sources, mergedSources...)
	}

	sources = preferMediaSource(sources, selectedMediaSourceID)

	playSessionID := strings.ReplaceAll(uuid.New().String(), "-", "")
	if sources == nil {
		sources = []dto.MediaSourceInfo{}
	}

	HideMediaSourceSizeForInfuse(c, sources)

	// 播放必经接口:异步探测当前 media_version 并回填 MediaStreams(strm/远程媒体
	// 入库未探测时详情为空)。比依赖客户端上报 Sessions/Playing 更可靠。
	// fire-and-forget,内部自带去重与 mediainfo 已有判断,失败不影响播放。
	go services.ProbeOnPlay(state.DB, *uid, selectedMediaSourceID)

	c.JSON(http.StatusOK, gin.H{
		"MediaSources":  embysupport.MediaSourcesToEmbyMaps(sources),
		"PlaySessionId": playSessionID,
	})
}

// resolveStreamUser resolves the user ID from api_key query param or X-Emby-Token header.
// Returns empty string if no valid token is found (stream still proceeds, matching Rust).
func resolveStreamUser(ctx context.Context, state *AppState, c *gin.Context) string {
	token := c.Query("api_key")
	if token == "" {
		token = c.GetHeader("X-Emby-Token")
	}
	if token == "" {
		// Also try the Authorization header used by middleware
		if authUser := middleware.GetAuthUser(c); authUser != nil {
			return authUser.ID
		}
		return ""
	}
	tok, err := state.Repo.Sessions.GetAccessToken(ctx, token)
	if err != nil || tok == nil {
		return ""
	}
	return tok.UserID.String()
}

type resolvedPath struct {
	filePath  string
	container string
	isRemote  bool
}

func (p *resolvedPath) FilePath() string {
	return p.filePath
}

func (p *resolvedPath) Container() string {
	return p.container
}

func (p *resolvedPath) IsRemote() bool {
	return p.isRemote
}

func resolveStrmPath(strmPath string) *resolvedPath {
	if !strings.HasSuffix(strings.ToLower(strmPath), ".strm") {
		return nil
	}
	data, err := os.ReadFile(strmPath)
	if err != nil {
		return nil
	}
	lines := strings.SplitN(string(data), "\n", 2)
	if len(lines) == 0 {
		return nil
	}
	line := strings.TrimSpace(lines[0])
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}

	resolved := line
	isRemote := false

	if strings.HasPrefix(resolved, "http://") || strings.HasPrefix(resolved, "https://") {
		isRemote = true
	} else if strings.HasPrefix(resolved, "/") {
		if _, err := os.Stat(resolved); err != nil {
			mntPath := "/mnt" + resolved
			if _, err := os.Stat(mntPath); err == nil {
				resolved = mntPath
			} else {
				fixed := strings.Replace(resolved, "/CloudNAS", "/mnt/CloudNAS", 1)
				if fixed != resolved {
					if _, err := os.Stat(fixed); err == nil {
						resolved = fixed
					}
				}
			}
		}
	} else {
		return nil
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(resolved), "."))
	if ext == "" {
		ext = "mkv"
	}
	return &resolvedPath{filePath: resolved, container: ext, isRemote: isRemote}
}

func ResolveStrmPath(strmPath string) *resolvedPath {
	return resolveStrmPath(strmPath)
}

func streamVideo(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	itemID := c.Param("itemId")
	uid, err := models.ResolveToUUID(ctx, state.DB, itemID)
	if err != nil || uid == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid item id"})
		return
	}

	// Resolve user from token (optional, matching Rust: no auth required for stream).
	// Check api_key query param, then X-Emby-Token header.
	userID := resolveStreamUser(ctx, state, c)
	if userID != "" {
		if ok, err := shared.UserCanAccessItem(ctx, state, userID, *uid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		} else if !ok {
			c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
			return
		}
		if userUUID, err := uuid.Parse(userID); err == nil {
			policy, _ := state.Repo.Users.GetUserPolicy(ctx, userUUID)
			if policy != nil && !policy.EnableMediaPlayback {
				c.JSON(http.StatusForbidden, gin.H{"message": "Playback disabled"})
				return
			}
			if policy != nil && policy.SimultaneousStreamLimit > 0 {
				if n := state.SessionManager.CountActiveStreams(userID); int32(n) > policy.SimultaneousStreamLimit {
					c.JSON(http.StatusForbidden, gin.H{"message": "Stream limit reached"})
					return
				}
			}
		}
	}

	msid := mediaSourceIDFromQuery(c)
	var filePath string
	if msid != "" {
		// Match Rust: query by id only, without item_id constraint
		fp, err := state.Repo.Playback.GetMediaVersionFilePath(ctx, msid)
		if err == nil && fp == nil {
			slog.Warn("[Stream] media_versions not found", "msid", msid, "itemId", itemID)
			c.JSON(http.StatusNotFound, gin.H{"message": "Media source not found"})
			return
		}
		if err != nil {
			slog.Error("[Stream] DB error", "msid", msid, "err", err)
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		filePath = *fp
		slog.Info("[Stream] resolved media_version", "msid", msid, "path", filePath)
	} else {
		fp, err := state.Repo.Playback.GetPrimaryMediaVersionFilePath(ctx, *uid)
		if err == nil && fp == nil {
			var row *dto.ItemRow
			row, err = models.GetItemByID(ctx, state.DB, *uid)
			if err != nil || row == nil || row.FilePath == nil || *row.FilePath == "" {
				slog.Warn("[Stream] no media file", "itemId", itemID)
				c.JSON(http.StatusNotFound, gin.H{"message": "No media file"})
				return
			}
			filePath = *row.FilePath
		} else if err != nil {
			slog.Error("[Stream] DB error", "itemId", itemID, "err", err)
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		} else {
			filePath = *fp
		}
	}

	serveMediaFile(c, state, itemID, filePath)
}

// serveMediaFile 解析并输出一个媒体文件路径:strm/http → 302;本地文件 → 直出(支持 Range);
// 本地不存在 → 交给 gateway 自播放路由兜底。streamVideo 与 streamTrailer 共用。
func serveMediaFile(c *gin.Context, state *AppState, itemID, filePath string) {
	if strings.HasSuffix(strings.ToLower(filePath), ".strm") {
		if rp := resolveStrmPath(filePath); rp != nil {
			if rp.isRemote {
				slog.Info("[Stream] 302 redirect (strm remote)", "url", rp.filePath)
				c.Redirect(http.StatusFound, rp.filePath)
				return
			}
			filePath = rp.filePath
		} else {
			slog.Warn("[Stream] strm resolve failed", "strmPath", filePath)
		}
	}
	if strings.HasPrefix(strings.ToLower(filePath), "http://") || strings.HasPrefix(strings.ToLower(filePath), "https://") {
		slog.Info("[Stream] 302 redirect (http)", "url", filePath)
		c.Redirect(http.StatusFound, filePath)
		return
	}

	fi, err := os.Stat(filePath)
	if err != nil {
		slog.Warn("[Stream] file not found on disk", "path", filePath, "err", err)
		if state.GatewayRuntime != nil && state.GatewayRuntime.TryResolveSelfPlaybackRoute(c.Writer, c.Request, itemID) {
			return
		}
		c.JSON(http.StatusNotFound, gin.H{"message": "File not found"})
		return
	}
	size := fi.Size()
	contentType := mimeForPath(filePath)

	rangeHdr := c.GetHeader("Range")
	if rangeHdr == "" {
		c.Header("Accept-Ranges", "bytes")
		c.Header("Content-Type", contentType)
		c.Header("Content-Length", strconv.FormatInt(size, 10))
		c.File(filePath)
		return
	}

	// Range request
	const prefix = "bytes="
	if !strings.HasPrefix(rangeHdr, prefix) {
		c.Header("Accept-Ranges", "bytes")
		c.Header("Content-Type", contentType)
		c.Header("Content-Length", strconv.FormatInt(size, 10))
		c.File(filePath)
		return
	}
	rng := strings.TrimPrefix(rangeHdr, prefix)
	parts := strings.SplitN(rng, "-", 2)
	if len(parts) != 2 {
		c.Status(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	start, err1 := strconv.ParseInt(parts[0], 10, 64)
	var end int64
	if parts[1] == "" {
		end = size - 1
	} else {
		end, err = strconv.ParseInt(parts[1], 10, 64)
	}
	if err != nil || err1 != nil || start < 0 || end >= size || start > end {
		c.Status(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	chunkLen := end - start + 1
	f, err := os.Open(filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer f.Close()

	if _, err := f.Seek(start, io.SeekStart); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	c.Header("Content-Type", contentType)
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Length", strconv.FormatInt(chunkLen, 10))
	c.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	c.Status(http.StatusPartialContent)
	io.CopyN(c.Writer, f, chunkLen)
}

// streamTrailer 输出电影的本地预告片(items.local_trailer_path),复用 serveMediaFile。
func streamTrailer(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	itemID := c.Param("itemId")
	uid, err := models.ResolveToUUID(ctx, state.DB, itemID)
	if err != nil || uid == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid item id"})
		return
	}
	trailerPath, err := state.Repo.Playback.GetLocalTrailerPath(ctx, *uid)
	if err != nil || trailerPath == nil || *trailerPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"message": "No local trailer"})
		return
	}
	serveMediaFile(c, state, itemID, *trailerPath)
}

func streamSubtitle(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	itemID := c.Param("itemId")
	uid, err := models.ResolveToUUID(ctx, state.DB, itemID)
	if err != nil || uid == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid item id"})
		return
	}

	userID := resolveStreamUser(ctx, state, c)
	if userID != "" {
		if ok, err := shared.UserCanAccessItem(ctx, state, userID, *uid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		} else if !ok {
			c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
			return
		}
	}

	mediaSourceID := strings.TrimSpace(c.Param("mediaSourceId"))
	streamIndex, err := strconv.ParseInt(c.Param("index"), 10, 32)
	if err != nil || mediaSourceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid subtitle request"})
		return
	}

	embeddedStreams, err := loadEmbeddedStreamsForMediaVersion(ctx, state, mediaSourceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	sub, err := ExternalSubtitleByIndex(ctx, state.DB, mediaSourceID, int32(streamIndex), embeddedStreams)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if sub == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Subtitle not found"})
		return
	}
	serveSubtitleFile(c, sub.FilePath)
}

func loadEmbeddedStreamsForMediaVersion(ctx context.Context, state *AppState, mediaSourceID string) ([]dto.MediaStreamInfo, error) {
	info, err := state.Repo.Playback.GetMediaVersionItemAndInfo(ctx, mediaSourceID)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return []dto.MediaStreamInfo{}, nil
	}

	streamRows, err := state.Repo.Playback.ListMediaStreamsForItem(ctx, info.ItemID)
	if err != nil {
		return nil, err
	}
	streams := make([]dto.MediaStreamInfo, 0, len(streamRows))
	for i := range streamRows {
		streams = append(streams, dto.FormatMediaStreamDto(&streamRows[i]))
	}
	if len(info.MediaInfo) > 0 {
		var mi map[string]json.RawMessage
		if json.Unmarshal(info.MediaInfo, &mi) == nil {
			if msRaw, ok := mi["MediaStreams"]; ok {
				var miStreams []dto.MediaStreamInfo
				if json.Unmarshal(msRaw, &miStreams) == nil && len(miStreams) > 0 {
					streams = miStreams
				}
			}
		}
	}
	return streams, nil
}

func serveSubtitleFile(c *gin.Context, filePath string) {
	if _, err := os.Stat(filePath); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Subtitle file not found"})
		return
	}
	c.Header("Content-Type", subtitleMimeForPath(filePath))
	c.File(filePath)
}

// collectMergedPlaybackSources finds media_versions from items that have been
// merged into the given primary item and returns them as additional MediaSources.
func collectMergedPlaybackSources(ctx context.Context, state *AppState, primaryID, userID string, fallbackStreams []dto.MediaStreamInfo) []dto.MediaSourceInfo {
	siblings, err := state.Repo.Playback.ListMergedSiblingItems(ctx, primaryID)
	if err != nil {
		return nil
	}
	if len(siblings) == 0 {
		return nil
	}

	var merged []dto.MediaSourceInfo
	for _, sib := range siblings {
		versionUserData := map[string]repository.MediaVersionUserData{}
		if userID != "" {
			if rows, err := state.Repo.MediaVersionUserData.ListForItem(ctx, userID, sib.ID); err == nil {
				versionUserData = rows
			}
		}
		versions, err := loadMediaVersions(ctx, state, sib.ID)
		if err != nil {
			continue
		}
		for _, mv := range versions {
			msid := mv.ID.String()
			actualPath := mv.FilePath
			actualContainer := ""
			if mv.Container != nil {
				actualContainer = *mv.Container
			}
			protocol := "File"
			isRemote := false
			if strings.HasSuffix(strings.ToLower(mv.FilePath), ".strm") {
				if rp := resolveStrmPath(mv.FilePath); rp != nil {
					actualPath = rp.filePath
					actualContainer = rp.container
					isRemote = rp.isRemote
					if isRemote {
						protocol = "Http"
					}
				}
			} else if strings.HasPrefix(strings.ToLower(actualPath), "http://") || strings.HasPrefix(strings.ToLower(actualPath), "https://") {
				protocol = "Http"
				isRemote = true
			}

			versionStreams := fallbackStreams
			if len(mv.MediaInfo) > 0 {
				var mi map[string]json.RawMessage
				if json.Unmarshal(mv.MediaInfo, &mi) == nil {
					if msRaw, ok := mi["MediaStreams"]; ok {
						var miStreams []dto.MediaStreamInfo
						if json.Unmarshal(msRaw, &miStreams) == nil && len(miStreams) > 0 {
							versionStreams = miStreams
						}
					}
				}
			}
			versionStreams = AppendExternalSubtitleStreams(ctx, state.DB, primaryID, msid, versionStreams)
			defaultAudioIndex, defaultSubtitleIndex := defaultStreamIndexes(versionStreams)

			srcName := sib.LibName + " - " + mv.Name
			src := dto.MediaSourceInfo{
				ID:                         msid,
				Path:                       actualPath,
				Protocol:                   protocol,
				Type:                       "Default",
				Container:                  actualContainer,
				Name:                       srcName,
				IsRemote:                   isRemote,
				RunTimeTicks:               mv.RuntimeTicks,
				SupportsDirectPlay:         true,
				SupportsDirectStream:       true,
				SupportsTranscoding:        false,
				MediaStreams:               versionStreams,
				ReadAtNativeFramerate:      false,
				Size:                       mv.Size,
				DirectStreamURL:            fmt.Sprintf("/Videos/%s/stream.%s?MediaSourceId=%s&Static=true", primaryID, actualContainer, msid),
				ETag:                       msid,
				Formats:                    []string{},
				DefaultAudioStreamIndex:    defaultAudioIndex,
				DefaultSubtitleStreamIndex: defaultSubtitleIndex,
				FymsResolution:             mv.Resolution,
				FymsHdrFormat:              mv.HDRFormat,
				FymsVideoCodec:             mv.VideoCodec,
				FymsAudioCodec:             mv.AudioCodec,
				FymsSource:                 mv.Source,
				FymsQualityLabel:           mv.QualityLabel,
			}
			if mv.Bitrate != nil {
				b := int64(*mv.Bitrate)
				src.Bitrate = &b
			}
			if data, ok := versionUserData[msid]; ok {
				ApplyMediaSourceUserData(&src, &data)
			}
			ApplyMediaSourceCompatDefaults(&src, primaryID)
			merged = append(merged, src)
		}
	}
	return merged
}

func mimeForPath(p string) string {
	ext := strings.ToLower(filepath.Ext(p))
	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".m3u8", ".m3u":
		return "application/vnd.apple.mpegurl"
	case ".mkv", ".mka", ".mks":
		return "video/x-matroska"
	case ".webm":
		return "video/webm"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".m4v":
		return "video/x-m4v"
	case ".ts", ".m2ts":
		return "video/mp2t"
	case ".strm":
		return "application/octet-stream"
	default:
		return "application/octet-stream"
	}
}

func subtitleMimeForPath(p string) string {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".srt":
		return "application/x-subrip; charset=utf-8"
	case ".vtt":
		return "text/vtt; charset=utf-8"
	case ".ass", ".ssa":
		return "text/plain; charset=utf-8"
	case ".smi", ".sami":
		return "application/x-sami; charset=utf-8"
	case ".ttml", ".dfxp":
		return "application/ttml+xml; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}
