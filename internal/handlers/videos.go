package handlers

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
	"github.com/jackc/pgx/v5"

	"fyms/internal/dto"
	"fyms/internal/middleware"
	"fyms/internal/models"
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
}

func loadMediaVersions(ctx context.Context, state *AppState, itemID string) ([]mediaVersionRow, error) {
	rows, err := state.DB.Query(ctx,
		`SELECT id, name, file_path, container, is_primary, runtime_ticks, bitrate, size, mediainfo,
		        resolution, hdr_format, video_codec, audio_codec, source, quality_label
		 FROM media_versions WHERE item_id = $1::uuid
		 ORDER BY is_primary DESC, created_at ASC`,
		itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []mediaVersionRow
	for rows.Next() {
		var v mediaVersionRow
		if err := rows.Scan(&v.ID, &v.Name, &v.FilePath, &v.Container, &v.IsPrimary, &v.RuntimeTicks, &v.Bitrate, &v.Size, &v.MediaInfo,
			&v.Resolution, &v.HDRFormat, &v.VideoCodec, &v.AudioCodec, &v.Source, &v.QualityLabel); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return versions, nil
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

		if _, err := state.DB.Exec(ctx,
			`INSERT INTO media_versions (item_id, name, file_path, container, is_primary, mediainfo, runtime_ticks, bitrate, size, resolution, hdr_format, video_codec, audio_codec, source, quality_label)
			 VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			 ON CONFLICT (item_id, file_path) DO UPDATE SET
			 	name = EXCLUDED.name,
			 	container = EXCLUDED.container,
			 	is_primary = EXCLUDED.is_primary,
			 	mediainfo = COALESCE(EXCLUDED.mediainfo, media_versions.mediainfo),
			 	runtime_ticks = COALESCE(EXCLUDED.runtime_ticks, media_versions.runtime_ticks),
			 	bitrate = COALESCE(EXCLUDED.bitrate, media_versions.bitrate),
			 	size = COALESCE(EXCLUDED.size, media_versions.size),
			 	resolution = COALESCE(EXCLUDED.resolution, media_versions.resolution),
			 	hdr_format = COALESCE(EXCLUDED.hdr_format, media_versions.hdr_format),
			 	video_codec = COALESCE(EXCLUDED.video_codec, media_versions.video_codec),
			 	audio_codec = COALESCE(EXCLUDED.audio_codec, media_versions.audio_codec),
			 	source = COALESCE(EXCLUDED.source, media_versions.source),
			 	quality_label = COALESCE(EXCLUDED.quality_label, media_versions.quality_label)`,
			itemID, versionName, filePath, container, i == 0, mediaInfoValue, runtimeTicks, bitrate, size,
			services.NullableStr(q.Resolution), services.NullableStr(q.HDRFormat), services.NullableStr(q.VideoCodec),
			services.NullableStr(q.AudioCodec), services.NullableStr(q.Source), services.NullableStr(qLabel),
		); err != nil {
			slog.Warn("playback backfill insert failed", "itemId", itemID, "filePath", filePath, "error", err)
			return nil, err
		}
	}
	slog.Info("playback backfill inserted versions", "itemId", itemID, "count", len(videoFiles))

	return loadMediaVersions(ctx, state, itemID)
}

func getPlaybackInfo(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	itemID := c.Param("itemId")
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
	var mergedToID *string
	state.DB.QueryRow(ctx, "SELECT merged_to_id::text FROM items WHERE id = $1::uuid", *uid).Scan(&mergedToID)
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

	var policy *models.UserPolicy
	if !strings.HasPrefix(authUser.ID, "api-key-") {
		userUUID, err := uuid.Parse(authUser.ID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
			return
		}
		policy, err = models.GetUserPolicy(ctx, state.DB, userUUID)
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
		if n := countUserPlayingStreams(state.SessionManager, authUser.ID); int32(n) >= policy.SimultaneousStreamLimit {
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

		src := dto.MediaSourceInfo{
			ID:                    msid,
			Path:                  actualPath,
			Protocol:              protocol,
			Type:                  "Default",
			Container:             actualContainer,
			Name:                  mv.Name,
			IsRemote:              isRemote,
			RunTimeTicks:          mv.RuntimeTicks,
			SupportsDirectPlay:    true,
			SupportsDirectStream:  true,
			SupportsTranscoding:   false,
			MediaStreams:          versionStreams,
			ReadAtNativeFramerate: false,
			Size:                  mv.Size,
			DirectStreamURL:       fmt.Sprintf("/Videos/%s/stream.%s?MediaSourceId=%s&Static=true", *uid, actualContainer, msid),
			ETag:                  msid,
			Formats:               []string{},
		}
		if mv.Bitrate != nil {
			b := int64(*mv.Bitrate)
			src.Bitrate = &b
		}
		sources = append(sources, src)
	}

	// Append MediaSources from merged secondary items
	mergedSources := collectMergedPlaybackSources(ctx, state, *uid, mediaStreams)
	if len(mergedSources) > 0 {
		sources = append(sources, mergedSources...)
	}

	playSessionID := strings.ReplaceAll(uuid.New().String(), "-", "")
	if sources == nil {
		sources = []dto.MediaSourceInfo{}
	}

	hideMediaSourceSizeForInfuse(c, sources)

	c.JSON(http.StatusOK, gin.H{
		"MediaSources":  sources,
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
	var userID string
	err := state.DB.QueryRow(ctx,
		"SELECT user_id::text FROM access_tokens WHERE token = $1", token).Scan(&userID)
	if err != nil {
		return ""
	}
	return userID
}

func countUserPlayingStreams(sm *services.SessionManager, userID string) int {
	n := 0
	for _, s := range sm.GetActiveSessions() {
		if s.UserID == userID && s.NowPlaying != nil {
			n++
		}
	}
	return n
}

type resolvedPath struct {
	filePath  string
	container string
	isRemote  bool
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
		if userUUID, err := uuid.Parse(userID); err == nil {
			policy, _ := models.GetUserPolicy(ctx, state.DB, userUUID)
			if policy != nil && !policy.EnableMediaPlayback {
				c.JSON(http.StatusForbidden, gin.H{"message": "Playback disabled"})
				return
			}
			if policy != nil && policy.SimultaneousStreamLimit > 0 {
				if n := countUserPlayingStreams(state.SessionManager, userID); int32(n) > policy.SimultaneousStreamLimit {
					c.JSON(http.StatusForbidden, gin.H{"message": "Stream limit reached"})
					return
				}
			}
		}
	}

	msid := c.Query("MediaSourceId")
	var filePath string
	if msid != "" {
		// Match Rust: query by id only, without item_id constraint
		var fp string
		err := state.DB.QueryRow(ctx,
			`SELECT file_path FROM media_versions WHERE id = $1::uuid`,
			msid).Scan(&fp)
		if err == pgx.ErrNoRows {
			slog.Warn("[Stream] media_versions not found", "msid", msid, "itemId", itemID)
			c.JSON(http.StatusNotFound, gin.H{"message": "Media source not found"})
			return
		}
		if err != nil {
			slog.Error("[Stream] DB error", "msid", msid, "err", err)
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		filePath = fp
		slog.Info("[Stream] resolved media_version", "msid", msid, "path", fp)
	} else {
		err := state.DB.QueryRow(ctx,
			`SELECT file_path FROM media_versions WHERE item_id = $1::uuid ORDER BY is_primary DESC, created_at ASC LIMIT 1`,
			*uid).Scan(&filePath)
		if err == pgx.ErrNoRows {
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
	var trailerPath *string
	if err := state.DB.QueryRow(ctx,
		"SELECT local_trailer_path FROM items WHERE id = $1::uuid", *uid).Scan(&trailerPath); err != nil ||
		trailerPath == nil || *trailerPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"message": "No local trailer"})
		return
	}
	serveMediaFile(c, state, itemID, *trailerPath)
}

// collectMergedPlaybackSources finds media_versions from items that have been
// merged into the given primary item and returns them as additional MediaSources.
func collectMergedPlaybackSources(ctx context.Context, state *AppState, primaryID string, fallbackStreams []dto.MediaStreamInfo) []dto.MediaSourceInfo {
	sibRows, err := state.DB.Query(ctx,
		`SELECT s.id::text, l.name AS lib_name
		 FROM items s JOIN libraries l ON s.library_id = l.id
		 WHERE s.merged_to_id = $1::uuid AND l.deleted_at IS NULL`, primaryID)
	if err != nil {
		return nil
	}
	defer sibRows.Close()

	type sibInfo struct{ ID, LibName string }
	var siblings []sibInfo
	for sibRows.Next() {
		var si sibInfo
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
		mvRows, err := state.DB.Query(ctx,
			`SELECT id, name, file_path, container, is_primary, runtime_ticks, bitrate, size, mediainfo
			 FROM media_versions WHERE item_id = $1::uuid ORDER BY is_primary DESC, created_at ASC`, sib.ID)
		if err != nil {
			continue
		}
		for mvRows.Next() {
			var mv mediaVersionRow
			if err := mvRows.Scan(&mv.ID, &mv.Name, &mv.FilePath, &mv.Container, &mv.IsPrimary, &mv.RuntimeTicks, &mv.Bitrate, &mv.Size, &mv.MediaInfo); err != nil {
				continue
			}
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

			srcName := sib.LibName + " - " + mv.Name
			src := dto.MediaSourceInfo{
				ID:                    msid,
				Path:                  actualPath,
				Protocol:              protocol,
				Type:                  "Default",
				Container:             actualContainer,
				Name:                  srcName,
				IsRemote:              isRemote,
				RunTimeTicks:          mv.RuntimeTicks,
				SupportsDirectPlay:    true,
				SupportsDirectStream:  true,
				SupportsTranscoding:   false,
				MediaStreams:          versionStreams,
				ReadAtNativeFramerate: false,
				Size:                  mv.Size,
				DirectStreamURL:       fmt.Sprintf("/Videos/%s/stream.%s?MediaSourceId=%s&Static=true", primaryID, actualContainer, msid),
				ETag:                  msid,
				Formats:               []string{},
			}
			if mv.Bitrate != nil {
				b := int64(*mv.Bitrate)
				src.Bitrate = &b
			}
			merged = append(merged, src)
		}
		mvRows.Close()
	}
	return merged
}

func mimeForPath(p string) string {
	ext := strings.ToLower(filepath.Ext(p))
	switch ext {
	case ".mp4":
		return "video/mp4"
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
