package handlers

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
	"fyms/internal/middleware"
	"fyms/internal/models"
	"fyms/internal/services"
)

type activePlayback struct {
	itemID            string
	itemName          string
	itemType          string
	seriesName        string
	clientName        string
	deviceName        string
	deviceID          string
	appVersion        string
	clientIP          string
	userAgent         string
	playMethod        string
	playSessionID     string
	startTimeMs       int64
	lastProgressMs    int64
	lastPositionTicks int64
}

var (
	activePlaybacks   = make(map[string]*activePlayback)
	activePlaybacksMu sync.RWMutex
)

func playbackKey(userID, deviceID string) string {
	return userID + ":" + deviceID
}

// ActivePlaybackCount returns the number of active playback sessions for a user.
func ActivePlaybackCount(userID string) int {
	activePlaybacksMu.RLock()
	defer activePlaybacksMu.RUnlock()
	count := 0
	prefix := userID + ":"
	for k := range activePlaybacks {
		if strings.HasPrefix(k, prefix) {
			count++
		}
	}
	return count
}

// resolveClientName 决定展示用的客户端名,通用两层策略(不再维护硬编码客户端清单):
//  1. 优先用客户端鉴权头自报的 Client 字段(几乎所有 Emby 协议客户端都带,最权威);
//  2. 没有时从 User-Agent 兜底 —— 见 clientNameFromUserAgent。
//
// 新客户端无需改代码即可正确显示。
func resolveClientName(authClient string, userAgent string) string {
	if c := strings.TrimSpace(authClient); c != "" && !strings.EqualFold(c, "Unknown") && !strings.EqualFold(c, "Unknown Client") {
		return c
	}
	return clientNameFromUserAgent(userAgent)
}

// clientNameFromUserAgent 从 UA 兜底提取客户端名:
//   - 浏览器(UA 以 Mozilla/ 开头,含各类 WebView)归一为 Chrome/Edge/Firefox/Safari 等友好名,
//     不返回冗长且带版本号的 UA 原值,避免同一浏览器升级后碎成多条;
//   - 原生 App 的 UA 形如 "Yamby/2.0.3.4 (Android)" / "Infuse/8.0",取首个产品名 token
//     (到斜杠/空格/括号为止),通用提取,不写死任何 App 名。
func clientNameFromUserAgent(ua string) string {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return "Unknown"
	}
	if strings.HasPrefix(ua, "Mozilla/") {
		switch {
		case strings.Contains(ua, "Edg/"): // Chromium Edge 必须在 Chrome 之前判断
			return "Microsoft Edge"
		case strings.Contains(ua, "OPR/"), strings.Contains(ua, "Opera"):
			return "Opera"
		case strings.Contains(ua, "Firefox/"):
			return "Firefox"
		case strings.Contains(ua, "Chrome/"):
			return "Chrome"
		case strings.Contains(ua, "Safari/"):
			return "Safari"
		}
		return "Web Browser"
	}
	if i := strings.IndexAny(ua, "/ ("); i > 0 {
		return strings.TrimSpace(ua[:i])
	}
	return ua
}

func resolveDeviceName(authDevice string, userAgent string) string {
	if authDevice != "" && authDevice != "Unknown" && authDevice != "Unknown Device" {
		return authDevice
	}
	if strings.Contains(userAgent, "iPhone") {
		return "iPhone"
	}
	if strings.Contains(userAgent, "iPad") {
		return "iPad"
	}
	if strings.Contains(userAgent, "Android") {
		return "Android"
	}
	if strings.Contains(userAgent, "Mac") {
		return "Mac"
	}
	if strings.Contains(userAgent, "Windows") {
		return "Windows"
	}
	if strings.Contains(userAgent, "Apple TV") {
		return "Apple TV"
	}
	return "Unknown"
}

func getClientIP(c *gin.Context) string {
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		if parts := strings.SplitN(xff, ",", 2); len(parts) > 0 {
			if ip := strings.TrimSpace(parts[0]); ip != "" {
				return ip
			}
		}
	}
	if xri := c.GetHeader("X-Real-IP"); xri != "" {
		return xri
	}
	return c.ClientIP()
}

func insertPlaybackActivity(ctx context.Context, st *AppState, userID, itemID, itemType, itemName, clientName, deviceName, clientIP, userAgent, playMethod string, seriesName *string, durationSec int64) {
	if durationSec <= 5 {
		return
	}
	if playMethod == "" {
		playMethod = "DirectPlay"
	}
	_, err := st.DB.Exec(ctx,
		`INSERT INTO playback_activity (user_id, item_id, item_type, item_name, play_method, client_name, device_name, play_duration, client_ip, series_name, user_agent)
		 VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		userID, itemID, &itemType, &itemName, playMethod, clientName, deviceName, int(durationSec), clientIP, seriesName, userAgent,
	)
	if err != nil {
		log.Printf("[Play] Failed to insert playback activity: %v", err)
	}
}

// FlushStalePlaybacks removes stale playback sessions (no progress for >120s)
// and records their activity. Called periodically from main.
func FlushStalePlaybacks(pool *pgxpool.Pool, sm *services.SessionManager) {
	nowMs := time.Now().UnixMilli()
	activePlaybacksMu.Lock()
	defer activePlaybacksMu.Unlock()
	for key, pb := range activePlaybacks {
		if nowMs-pb.lastProgressMs > 120_000 {
			durationSec := (pb.lastProgressMs - pb.startTimeMs) / 1000
			if durationSec > 5 {
				parts := strings.SplitN(key, ":", 2)
				userID := parts[0]
				var sn *string
				if pb.seriesName != "" {
					sn = &pb.seriesName
				}
				pm := pb.playMethod
				if pm == "" {
					pm = "DirectPlay"
				}
				_, err := pool.Exec(context.Background(),
					`INSERT INTO playback_activity (user_id, item_id, item_type, item_name, play_method, client_name, device_name, play_duration, client_ip, series_name, user_agent)
					 VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
					userID, pb.itemID, &pb.itemType, &pb.itemName, pm, pb.clientName, pb.deviceName, int(durationSec), pb.clientIP, sn, pb.userAgent,
				)
				if err != nil {
					slog.Debug("stale playback flush insert failed", "error", err)
				}
				if len(parts) > 1 {
					sm.ClearNowPlaying(userID, parts[1])
				}
			}
			delete(activePlaybacks, key)
		}
	}
}

func RegisterPlaybackRoutes(group *gin.RouterGroup, state *AppState, authMW gin.HandlerFunc) {
	_ = state
	group.POST("/Sessions/Playing", authMW, OnPlaybackStart)
	group.POST("/Sessions/Playing/Progress", authMW, OnPlaybackProgress)
	group.POST("/Sessions/Playing/Stopped", authMW, OnPlaybackStopped)

	group.POST("/Users/:userId/PlayedItems/:itemId", authMW, MarkPlayed)
	group.DELETE("/Users/:userId/PlayedItems/:itemId", authMW, MarkUnplayed)
	group.POST("/Users/:userId/FavoriteItems/:itemId", authMW, MarkFavorite)
	group.DELETE("/Users/:userId/FavoriteItems/:itemId", authMW, UnmarkFavorite)
	group.POST("/Users/:userId/Items/:itemId/HideFromResume", authMW, HideFromResume)

	// 兼容省略 :userId 段的客户端(Forward 等),从 token 反查用户。
	group.POST("/Users/PlayedItems/:itemId", authMW, MarkPlayed)
	group.DELETE("/Users/PlayedItems/:itemId", authMW, MarkUnplayed)
	group.POST("/Users/FavoriteItems/:itemId", authMW, MarkFavorite)
	group.DELETE("/Users/FavoriteItems/:itemId", authMW, UnmarkFavorite)
	group.POST("/Users/Items/:itemId/HideFromResume", authMW, HideFromResume)
}

func deviceIDFromRequest(c *gin.Context) string {
	return strOrPtr(middleware.GetAuthInfo(c).DeviceID, "unknown")
}

func nowPlayingFromItem(item *services.NowPlaying) *services.NowPlaying {
	return item
}

// --- Request bodies ---

type playbackBody struct {
	ItemId        string `json:"ItemId"`
	PositionTicks int64  `json:"PositionTicks"`
	IsPaused      bool   `json:"IsPaused"`
	MediaSourceId string `json:"MediaSourceId"`
	PlaySessionId string `json:"PlaySessionId"`
}

// --- Handlers ---

func OnPlaybackStart(c *gin.Context) {
	st := GetState(c)
	auth := middleware.GetAuthUser(c)
	if auth == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
		return
	}

	if !strings.HasPrefix(auth.ID, "api-key-") {
		if userUUID, err := uuid.Parse(auth.ID); err == nil {
			if policy, err := models.GetUserPolicy(c.Request.Context(), st.DB, userUUID); err == nil && policy != nil {
				if policy.SimultaneousStreamLimit > 0 {
					count := ActivePlaybackCount(auth.ID)
					if int32(count) >= policy.SimultaneousStreamLimit {
						slog.Warn("[Play] User exceeded stream limit", "user", auth.Name, "active", count, "limit", policy.SimultaneousStreamLimit)
						c.JSON(http.StatusForbidden, gin.H{"message": fmt.Sprintf("Stream limit reached (%d/%d)", count, policy.SimultaneousStreamLimit)})
						return
					}
				}
			}
		}
	}

	var body playbackBody
	if err := c.ShouldBindJSON(&body); err != nil || body.ItemId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request body"})
		return
	}

	info := middleware.GetAuthInfo(c)
	userAgent := c.GetHeader("User-Agent")
	clientIP := getClientIP(c)
	deviceID := deviceIDFromRequest(c)

	clientName := resolveClientName(strOrPtr(info.Client, ""), userAgent)
	deviceName := resolveDeviceName(strOrPtr(info.Device, ""), userAgent)

	item, err := loadItemForPlayback(c.Request.Context(), st, body.ItemId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	resolvedItemID := body.ItemId
	if item != nil {
		resolvedItemID = item.ID
	}

	st.ProgressBuffer.BufferProgress(&services.ProgressEntry{
		UserID:        auth.ID,
		ItemID:        resolvedItemID,
		PositionTicks: body.PositionTicks,
	})

	st.SessionManager.UpdateSession(
		auth.ID, auth.Name, deviceID,
		deviceName,
		clientName,
		strOrPtr(info.Version, ""),
		clientIP,
	)

	np := buildNowPlaying(item, resolvedItemID, body.PositionTicks, body.IsPaused, body.PlaySessionId)
	st.SessionManager.SetNowPlaying(auth.ID, deviceID, np)

	// 首次播放异步回填 MediaStreams(strm 远程媒体入库时未探测,详情为空)。
	// fire-and-forget:内部自带独立 context 与去重,失败不影响播放。对齐 Emby
	// 「播放一次后详情就有音视频轨道信息」的行为。
	go services.ProbeOnPlay(st.DB, resolvedItemID, body.MediaSourceId)

	itemName := "Unknown"
	itemType := "Unknown"
	seriesName := ""
	if item != nil {
		itemName = item.Name
		itemType = item.ItemType
		if item.SeriesName != nil {
			seriesName = *item.SeriesName
		}
	}

	log.Printf("[Play] User '%s' started playing '%s' (%s)", auth.Name, itemName, clientName)

	nowMs := time.Now().UnixMilli()
	activePlaybacksMu.Lock()
	playMethod := c.GetHeader("X-Play-Method")
	if playMethod == "" {
		playMethod = "DirectPlay"
	}

	activePlaybacks[playbackKey(auth.ID, deviceID)] = &activePlayback{
		itemID:            resolvedItemID,
		itemName:          itemName,
		itemType:          itemType,
		seriesName:        seriesName,
		clientName:        clientName,
		deviceName:        deviceName,
		deviceID:          deviceID,
		appVersion:        strOrPtr(info.Version, ""),
		clientIP:          clientIP,
		userAgent:         userAgent,
		playMethod:        playMethod,
		playSessionID:     body.PlaySessionId,
		startTimeMs:       nowMs,
		lastProgressMs:    nowMs,
		lastPositionTicks: body.PositionTicks,
	}
	activePlaybacksMu.Unlock()

	services.EmitPlaybackNotify(
		services.NotifyEventPlaybackStart,
		resolvedItemID,
		auth.ID,
		auth.Name,
		buildNotifySession(clientIP, clientName, deviceName, deviceID, strOrPtr(info.Version, ""), body.PlaySessionId),
		&services.NotifyPlaybackInfo{PositionTicks: body.PositionTicks, PlaylistIndex: 0, PlaylistLength: 1},
		nil,
	)

	c.Status(http.StatusNoContent)
}

func OnPlaybackProgress(c *gin.Context) {
	st := GetState(c)
	auth := middleware.GetAuthUser(c)
	if auth == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
		return
	}
	var body playbackBody
	if err := c.ShouldBindJSON(&body); err != nil || body.ItemId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request body"})
		return
	}

	info := middleware.GetAuthInfo(c)
	userAgent := c.GetHeader("User-Agent")
	clientIP := getClientIP(c)
	deviceID := deviceIDFromRequest(c)

	item, err := loadItemForPlayback(c.Request.Context(), st, body.ItemId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	resolvedItemID := body.ItemId
	if item != nil {
		resolvedItemID = item.ID
	}

	st.ProgressBuffer.BufferProgress(&services.ProgressEntry{
		UserID:        auth.ID,
		ItemID:        resolvedItemID,
		PositionTicks: body.PositionTicks,
	})

	clientName := resolveClientName(strOrPtr(info.Client, ""), userAgent)
	deviceName := resolveDeviceName(strOrPtr(info.Device, ""), userAgent)

	st.SessionManager.UpdateSession(
		auth.ID, auth.Name, deviceID,
		deviceName,
		clientName,
		strOrPtr(info.Version, ""),
		clientIP,
	)

	key := playbackKey(auth.ID, deviceID)
	nowMs := time.Now().UnixMilli()

	needNew := false
	activePlaybacksMu.Lock()
	existing, ok := activePlaybacks[key]
	if ok && existing.itemID == resolvedItemID {
		existing.lastProgressMs = nowMs
		existing.lastPositionTicks = body.PositionTicks
	} else if ok {
		durationSec := (nowMs - existing.startTimeMs) / 1000
		if durationSec > 5 {
			var sn *string
			if existing.seriesName != "" {
				sn = &existing.seriesName
			}
			insertPlaybackActivity(c.Request.Context(), st, auth.ID, existing.itemID, existing.itemType, existing.itemName, existing.clientName, existing.deviceName, existing.clientIP, existing.userAgent, existing.playMethod, sn, durationSec)
		}
		needNew = true
	} else {
		needNew = true
	}
	activePlaybacksMu.Unlock()

	np := buildNowPlaying(item, resolvedItemID, body.PositionTicks, body.IsPaused, body.PlaySessionId)
	progressPM := c.GetHeader("X-Play-Method")
	if progressPM == "" {
		// Inherit from existing activePlayback if available
		activePlaybacksMu.RLock()
		if pb, ok := activePlaybacks[key]; ok {
			progressPM = pb.playMethod
		}
		activePlaybacksMu.RUnlock()
	}
	np.PlayMethod = progressPM
	st.SessionManager.SetNowPlaying(auth.ID, deviceID, np)

	if needNew {
		itemName := "Unknown"
		itemType := "Unknown"
		seriesName := ""
		if item != nil {
			itemName = item.Name
			itemType = item.ItemType
			if item.SeriesName != nil {
				seriesName = *item.SeriesName
			}
		}
		activePlaybacksMu.Lock()
		pm := c.GetHeader("X-Play-Method")
		if pm == "" {
			pm = "DirectPlay"
		}
		activePlaybacks[key] = &activePlayback{
			itemID:            resolvedItemID,
			itemName:          itemName,
			itemType:          itemType,
			seriesName:        seriesName,
			clientName:        clientName,
			deviceName:        deviceName,
			deviceID:          deviceID,
			appVersion:        strOrPtr(info.Version, ""),
			clientIP:          clientIP,
			userAgent:         userAgent,
			playMethod:        pm,
			playSessionID:     body.PlaySessionId,
			startTimeMs:       nowMs,
			lastProgressMs:    nowMs,
			lastPositionTicks: body.PositionTicks,
		}
		activePlaybacksMu.Unlock()
	}

	c.Status(http.StatusNoContent)
}

func OnPlaybackStopped(c *gin.Context) {
	st := GetState(c)
	auth := middleware.GetAuthUser(c)
	if auth == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
		return
	}
	var body playbackBody
	if err := c.ShouldBindJSON(&body); err != nil || body.ItemId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request body"})
		return
	}

	deviceID := deviceIDFromRequest(c)
	key := playbackKey(auth.ID, deviceID)

	activePlaybacksMu.Lock()
	session, existed := activePlaybacks[key]
	if existed {
		delete(activePlaybacks, key)
	}
	activePlaybacksMu.Unlock()

	if existed {
		nowMs := time.Now().UnixMilli()
		durationSec := (nowMs - session.startTimeMs) / 1000
		if durationSec > 5 {
			log.Printf("[Play] User '%s' stopped '%s' after %ds", auth.Name, session.itemName, durationSec)
			var sn *string
			if session.seriesName != "" {
				sn = &session.seriesName
			}
			insertPlaybackActivity(c.Request.Context(), st, auth.ID, session.itemID, session.itemType, session.itemName, session.clientName, session.deviceName, session.clientIP, session.userAgent, session.playMethod, sn, durationSec)
		}
	}

	st.SessionManager.ClearNowPlaying(auth.ID, deviceID)

	resolved, err := models.ResolveToUUID(c.Request.Context(), st.DB, body.ItemId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	itemUUID := body.ItemId
	if resolved != nil {
		itemUUID = *resolved
	}

	item, _ := loadItemForPlayback(c.Request.Context(), st, body.ItemId)
	var played *bool
	pos := body.PositionTicks
	if pos <= 0 && existed && session.lastPositionTicks > 0 {
		pos = session.lastPositionTicks
	}
	if item != nil && item.RuntimeTicks != nil && *item.RuntimeTicks > 0 {
		// 看完判定阈值可在系统设置里配置(playback_played_threshold,默认 90%)。
		th := st.Repo.SystemConfig.GetIntOrDefault(c.Request.Context(), "playback_played_threshold", 90)
		if th < 1 {
			th = 1
		}
		if th > 100 {
			th = 100
		}
		pct := pos * 100 / *item.RuntimeTicks
		if pct >= int64(th) {
			t := true
			played = &t
			// 看完后清零续播位置,让该集干净离开"继续观看",由 NextUp 推下一集。
			pos = 0
		}
	}
	var position *int64
	if pos > 0 || (played != nil && *played) {
		position = &pos
	}

	ud, _ := models.GetUserItemData(c.Request.Context(), st.DB, auth.ID, itemUUID)
	var playCount *int32
	if ud != nil && ud.PlayCount != nil {
		v := *ud.PlayCount + 1
		playCount = &v
	} else {
		v := int32(1)
		playCount = &v
	}

	if err := models.UpsertUserItemData(c.Request.Context(), st.DB, auth.ID, itemUUID, position, playCount, nil, played); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	updatedUD, _ := models.GetUserItemData(c.Request.Context(), st.DB, auth.ID, itemUUID)
	notifySession := buildNotifySessionFromPlayback(session, deviceID, body.PlaySessionId)
	services.EmitPlaybackNotify(
		services.NotifyEventPlaybackStop,
		itemUUID,
		auth.ID,
		auth.Name,
		notifySession,
		&services.NotifyPlaybackInfo{
			PlayedToCompletion: played != nil && *played,
			PositionTicks:      pos,
			PlaylistIndex:      0,
			PlaylistLength:     1,
		},
		updatedUD,
	)
	c.Status(http.StatusNoContent)
}

// --- Mark Played / Unplayed / Favorite ---

func MarkPlayed(c *gin.Context) {
	st := GetState(c)
	userID := resolveUserID(c)
	if !matchUserOrAdmin(c, userID) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}
	itemID := c.Param("itemId")
	resolved, err := models.ResolveToUUID(c.Request.Context(), st.DB, itemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	iid := itemID
	if resolved != nil {
		iid = *resolved
	}
	pos := int64(0)
	t := true
	if err := models.UpsertUserItemData(c.Request.Context(), st.DB, userID, iid, &pos, nil, nil, &t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	ud, _ := models.GetUserItemData(c.Request.Context(), st.DB, userID, iid)
	services.EmitUserDataNotify(services.NotifyEventItemMarkPlayed, iid, userID, notifyUserName(c.Request.Context(), st, userID), ud)
	c.JSON(http.StatusOK, gin.H{"Played": true})
}

func MarkUnplayed(c *gin.Context) {
	st := GetState(c)
	userID := resolveUserID(c)
	if !matchUserOrAdmin(c, userID) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}
	itemID := c.Param("itemId")
	resolved, err := models.ResolveToUUID(c.Request.Context(), st.DB, itemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	iid := itemID
	if resolved != nil {
		iid = *resolved
	}
	pos := int64(0)
	f := false
	if err := models.UpsertUserItemData(c.Request.Context(), st.DB, userID, iid, &pos, nil, nil, &f); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	ud, _ := models.GetUserItemData(c.Request.Context(), st.DB, userID, iid)
	services.EmitUserDataNotify(services.NotifyEventItemMarkUnplayed, iid, userID, notifyUserName(c.Request.Context(), st, userID), ud)
	c.JSON(http.StatusOK, gin.H{"Played": false})
}

func MarkFavorite(c *gin.Context) {
	st := GetState(c)
	userID := resolveUserID(c)
	if !matchUserOrAdmin(c, userID) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}
	itemID := c.Param("itemId")
	if _, err := uuid.Parse(itemID); err == nil && models.PersonExists(c.Request.Context(), st.DB, itemID) {
		if err := models.UpsertUserPersonFavorite(c.Request.Context(), st.DB, userID, itemID, true); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		ud := models.PersonUserDataRow(true)
		services.EmitUserDataNotify(services.NotifyEventItemRate, itemID, userID, notifyUserName(c.Request.Context(), st, userID), ud)
		c.JSON(http.StatusOK, userDataResponse(ud))
		return
	}
	resolved, err := models.ResolveToUUID(c.Request.Context(), st.DB, itemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	iid := itemID
	if resolved != nil {
		iid = *resolved
	}
	t := true
	if err := models.UpsertUserItemData(c.Request.Context(), st.DB, userID, iid, nil, nil, &t, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	ud, _ := models.GetUserItemData(c.Request.Context(), st.DB, userID, iid)
	services.EmitUserDataNotify(services.NotifyEventItemRate, iid, userID, notifyUserName(c.Request.Context(), st, userID), ud)
	c.JSON(http.StatusOK, userDataResponse(ud))
}

func UnmarkFavorite(c *gin.Context) {
	st := GetState(c)
	userID := resolveUserID(c)
	if !matchUserOrAdmin(c, userID) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}
	itemID := c.Param("itemId")
	if _, err := uuid.Parse(itemID); err == nil && models.PersonExists(c.Request.Context(), st.DB, itemID) {
		if err := models.UpsertUserPersonFavorite(c.Request.Context(), st.DB, userID, itemID, false); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		ud := models.PersonUserDataRow(false)
		services.EmitUserDataNotify(services.NotifyEventItemRate, itemID, userID, notifyUserName(c.Request.Context(), st, userID), ud)
		c.JSON(http.StatusOK, userDataResponse(ud))
		return
	}
	resolved, err := models.ResolveToUUID(c.Request.Context(), st.DB, itemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	iid := itemID
	if resolved != nil {
		iid = *resolved
	}
	f := false
	if err := models.UpsertUserItemData(c.Request.Context(), st.DB, userID, iid, nil, nil, &f, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	ud, _ := models.GetUserItemData(c.Request.Context(), st.DB, userID, iid)
	services.EmitUserDataNotify(services.NotifyEventItemRate, iid, userID, notifyUserName(c.Request.Context(), st, userID), ud)
	c.JSON(http.StatusOK, userDataResponse(ud))
}

func userDataResponse(ud *dto.UserDataRow) gin.H {
	resp := gin.H{
		"PlaybackPositionTicks": int64(0),
		"PlayCount":             int32(0),
		"IsFavorite":            false,
		"Played":                false,
	}
	if ud == nil {
		return resp
	}
	if ud.PlaybackPositionTicks != nil {
		resp["PlaybackPositionTicks"] = *ud.PlaybackPositionTicks
	}
	if ud.PlayCount != nil {
		resp["PlayCount"] = *ud.PlayCount
	}
	if ud.IsFavorite != nil {
		resp["IsFavorite"] = *ud.IsFavorite
	}
	if ud.Played != nil {
		resp["Played"] = *ud.Played
	}
	return resp
}

// HideFromResume 处理 POST /Users/:userId/Items/:itemId/HideFromResume?Hide=true|false
// Emby 客户端用于把某条目从"继续观看"列表中隐藏(或恢复显示),不丢失播放位置。
// Hide 参数缺省时默认 true,符合 Emby 行为。
func HideFromResume(c *gin.Context) {
	st := GetState(c)
	userID := resolveUserID(c)
	if !matchUserOrAdmin(c, userID) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}
	itemID := c.Param("itemId")
	resolved, err := models.ResolveToUUID(c.Request.Context(), st.DB, itemID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	iid := itemID
	if resolved != nil {
		iid = *resolved
	}
	hide := true
	if v := strings.TrimSpace(c.Query("Hide")); v != "" {
		hide = strings.EqualFold(v, "true") || v == "1"
	}
	if err := models.SetHiddenFromResume(c.Request.Context(), st.DB, userID, iid, hide); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"HiddenFromResume": hide})
}

// --- Helpers ---

func loadItemForPlayback(ctx context.Context, st *AppState, rawItemID string) (*dto.ItemRow, error) {
	resolved, err := models.ResolveToUUID(ctx, st.DB, rawItemID)
	if err != nil {
		return nil, err
	}
	id := rawItemID
	if resolved != nil {
		id = *resolved
	}
	return models.GetItemByAnyID(ctx, st.DB, id)
}

func notifyUserName(ctx context.Context, st *AppState, userID string) string {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return userID
	}
	u, err := models.FindUserByID(ctx, st.DB, uid)
	if err != nil || u == nil || u.Name == "" {
		return userID
	}
	return u.Name
}

func buildNotifySession(remoteEndPoint, clientName, deviceName, deviceID, appVersion, sessionID string) *services.NotifySession {
	if sessionID == "" {
		sessionID = deviceID
	}
	return &services.NotifySession{
		RemoteEndPoint:     remoteEndPoint,
		Client:             clientName,
		DeviceName:         deviceName,
		DeviceID:           deviceID,
		ApplicationVersion: appVersion,
		ID:                 sessionID,
	}
}

func buildNotifySessionFromPlayback(pb *activePlayback, deviceID, sessionID string) *services.NotifySession {
	if pb == nil {
		return buildNotifySession("", "", "", deviceID, "", sessionID)
	}
	if sessionID == "" {
		sessionID = pb.playSessionID
	}
	did := pb.deviceID
	if did == "" {
		did = deviceID
	}
	return buildNotifySession(pb.clientIP, pb.clientName, pb.deviceName, did, pb.appVersion, sessionID)
}

func buildNowPlaying(item *dto.ItemRow, itemID string, positionTicks int64, isPaused bool, playSessionID string) *services.NowPlaying {
	if item == nil {
		return &services.NowPlaying{
			ItemID:        itemID,
			PositionTicks: positionTicks,
			IsPaused:      isPaused,
			PlaySessionID: playSessionID,
		}
	}
	np := &services.NowPlaying{
		ItemID:        item.ID,
		ItemName:      item.Name,
		ItemType:      item.ItemType,
		PositionTicks: positionTicks,
		IsPaused:      isPaused,
		RuntimeTicks:  item.RuntimeTicks,
		PlaySessionID: playSessionID,
	}
	if item.SeriesName != nil {
		np.SeriesName = item.SeriesName
	}
	if item.IndexNumber != nil {
		np.EpisodeIndex = item.IndexNumber
	}
	if item.ParentIndexNumber != nil {
		np.SeasonIndex = item.ParentIndexNumber
	}
	imgID := item.ID
	if (item.ItemType == "Episode" || item.ItemType == "Season") && item.SeriesID != nil {
		imgID = *item.SeriesID
	}
	np.PrimaryImageItemID = &imgID
	return np
}
