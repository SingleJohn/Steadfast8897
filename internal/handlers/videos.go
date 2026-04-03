package handlers

import (
	"encoding/json"
	"fmt"
	"io"
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
	g := group.Group("")
	g.Use(authMW)
	g.GET("/Items/:itemId/PlaybackInfo", func(c *gin.Context) { getPlaybackInfo(c, state) })
	g.POST("/Items/:itemId/PlaybackInfo", func(c *gin.Context) { getPlaybackInfo(c, state) })
	g.GET("/Videos/:itemId/stream", func(c *gin.Context) { streamVideo(c, state) })
	g.GET("/Videos/:itemId/stream.:container", func(c *gin.Context) { streamVideo(c, state) })
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

	rows, err := state.DB.Query(ctx,
		`SELECT id, name, file_path, container, is_primary, runtime_ticks, bitrate, size, mediainfo
		 FROM media_versions WHERE item_id = $1::uuid
		 ORDER BY is_primary DESC, created_at ASC`,
		*uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	var versions []mediaVersionRow
	for rows.Next() {
		var v mediaVersionRow
		if err := rows.Scan(&v.ID, &v.Name, &v.FilePath, &v.Container, &v.IsPrimary, &v.RuntimeTicks, &v.Bitrate, &v.Size, &v.MediaInfo); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
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

	var mediaStreams []dto.MediaStreamInfo
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
			SupportsTranscoding:   policy == nil || policy.EnableVideoTranscoding,
			MediaStreams:           versionStreams,
			ReadAtNativeFramerate: false,
			Size:                  mv.Size,
			DirectStreamURL:       fmt.Sprintf("/Videos/%s/stream.%s?MediaSourceId=%s&Static=true", *uid, actualContainer, msid),
			ETag:                  msid,
		}
		if mv.Bitrate != nil {
			b := int64(*mv.Bitrate)
			src.Bitrate = &b
		}
		sources = append(sources, src)
	}

	playSessionID := uuid.New().String()
	c.JSON(http.StatusOK, gin.H{
		"MediaSources":  sources,
		"PlaySessionId": playSessionID,
	})
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

	authUser := middleware.GetAuthUser(c)
	if authUser == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
		return
	}
	if !strings.HasPrefix(authUser.ID, "api-key-") {
		userUUID, perr := uuid.Parse(authUser.ID)
		if perr != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
			return
		}
		policy, err := models.GetUserPolicy(ctx, state.DB, userUUID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Policy error"})
			return
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
	}

	msid := c.Query("MediaSourceId")
	var filePath string
	if msid != "" {
		var fp string
		err := state.DB.QueryRow(ctx,
			`SELECT file_path FROM media_versions WHERE id = $1::uuid AND item_id = $2::uuid`,
			msid, *uid).Scan(&fp)
		if err == pgx.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"message": "Media source not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		filePath = fp
	} else {
		err := state.DB.QueryRow(ctx,
			`SELECT file_path FROM media_versions WHERE item_id = $1::uuid ORDER BY is_primary DESC, created_at ASC LIMIT 1`,
			*uid).Scan(&filePath)
		if err == pgx.ErrNoRows {
			var row *dto.ItemRow
			row, err = models.GetItemByID(ctx, state.DB, *uid)
			if err != nil || row == nil || row.FilePath == nil || *row.FilePath == "" {
				c.JSON(http.StatusNotFound, gin.H{"message": "No media file"})
				return
			}
			filePath = *row.FilePath
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}

	if strings.HasSuffix(strings.ToLower(filePath), ".strm") {
		if rp := resolveStrmPath(filePath); rp != nil {
			if rp.isRemote {
				c.Redirect(http.StatusFound, rp.filePath)
				return
			}
			filePath = rp.filePath
		}
	}
	if strings.HasPrefix(strings.ToLower(filePath), "http://") || strings.HasPrefix(strings.ToLower(filePath), "https://") {
		c.Redirect(http.StatusFound, filePath)
		return
	}

	fi, err := os.Stat(filePath)
	if err != nil {
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
