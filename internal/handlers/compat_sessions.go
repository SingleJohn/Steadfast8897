package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"fyms/internal/models"
	"fyms/internal/services"
)

func getSessions(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	sessions := state.SessionManager.GetActiveSessions()
	out := make([]gin.H, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, formatEmbySessionInfo(ctx, s, state))
	}
	c.JSON(http.StatusOK, out)
}

func formatEmbySessionInfo(ctx context.Context, s services.ActiveSession, state *AppState) gin.H {
	h := gin.H{
		"Id":                 s.UserID + "_" + s.DeviceID,
		"UserId":             s.UserID,
		"UserName":           s.UserName,
		"Client":             s.AppName,
		"DeviceId":           s.DeviceID,
		"DeviceName":         s.DeviceName,
		"ApplicationVersion": s.AppVersion,
		"ServerId":           state.Config.ServerID,
		"RemoteEndPoint":     s.ClientIP,
		"LastActivityDate":   s.LastActivity.UTC().Format("2006-01-02T15:04:05.0000000Z"),
	}
	if s.NowPlaying != nil {
		np := s.NowPlaying
		item := gin.H{
			"Id":           np.ItemID,
			"Name":         np.ItemName,
			"Type":         np.ItemType,
			"ServerId":     state.Config.ServerID,
			"RunTimeTicks": np.RuntimeTicks,
		}
		if np.SeriesName != nil {
			item["SeriesName"] = *np.SeriesName
		}
		if np.SeasonIndex != nil {
			item["ParentIndexNumber"] = *np.SeasonIndex
		}
		if np.EpisodeIndex != nil {
			item["IndexNumber"] = *np.EpisodeIndex
		}
		if np.PrimaryImageItemID != nil {
			item["PrimaryImageItemId"] = *np.PrimaryImageItemID
		}

		streams, err := models.GetMediaStreams(ctx, state.DB, np.ItemID)
		if err == nil && len(streams) > 0 {
			ms := make([]gin.H, 0, len(streams))
			for i := range streams {
				s := &streams[i]
				entry := gin.H{
					"Type":         s.StreamType,
					"Codec":        ptrOrEmpty(s.Codec),
					"DisplayTitle": ptrOrEmpty(s.DisplayTitle),
					"IsDefault":    s.IsDefault != nil && *s.IsDefault,
				}
				if s.Width != nil {
					entry["Width"] = *s.Width
				}
				if s.Height != nil {
					entry["Height"] = *s.Height
				}
				if s.BitRate != nil {
					entry["BitRate"] = *s.BitRate
				}
				if s.Channels != nil {
					entry["Channels"] = *s.Channels
				}
				ms = append(ms, entry)
			}
			item["MediaStreams"] = ms
		}

		var container string
		var bitrate *int32
		err = state.DB.QueryRow(ctx,
			"SELECT container, bitrate FROM media_versions WHERE item_id = $1::uuid AND is_primary = true LIMIT 1",
			np.ItemID).Scan(&container, &bitrate)
		if err == nil {
			item["Container"] = container
			item["Bitrate"] = bitrate
		}

		h["NowPlayingItem"] = item
		h["PlayState"] = gin.H{
			"IsPaused":      np.IsPaused,
			"PositionTicks": np.PositionTicks,
			"CanSeek":       true,
			"PlayMethod":    "DirectPlay",
		}
	} else {
		h["PlayState"] = gin.H{
			"IsPaused":      false,
			"PositionTicks": int64(0),
			"CanSeek":       true,
		}
	}
	return h
}

func sessionStop(c *gin.Context, state *AppState) {
	sessionID := c.Param("sessionId")
	if idx := strings.Index(sessionID, "_"); idx > 0 {
		userID := sessionID[:idx]
		deviceID := sessionID[idx+1:]
		state.SessionManager.ClearNowPlaying(userID, deviceID)
	}
	c.Status(http.StatusNoContent)
}

func sessionMessage(c *gin.Context) {
	c.Status(http.StatusNoContent)
}
