package users

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"fyms/internal/middleware"
	"fyms/internal/repository"
	"fyms/internal/services"
)

func requestClientIP(c *gin.Context) string {
	ip := c.GetHeader("X-Forwarded-For")
	if ip != "" {
		ip = strings.TrimSpace(strings.SplitN(ip, ",", 2)[0])
	}
	if ip == "" {
		ip = strings.TrimSpace(c.GetHeader("X-Real-IP"))
	}
	if ip == "" {
		ip = c.ClientIP()
	}
	return ip
}

func authenticateResponse(c *gin.Context, st *AppState, u *repository.User, token string) {
	user, err := buildUserResponse(c.Request.Context(), st, u, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	info := middleware.GetAuthInfo(c)
	userID := u.ID.String()
	deviceID := strOrPtr(info.DeviceID, "unknown")
	deviceName := strOrPtr(info.Device, "")
	client := strOrPtr(info.Client, "FYMS")
	appVersion := strOrPtr(info.Version, st.Config.Version)
	clientIP := requestClientIP(c)
	now := time.Now().UTC().Format("2006-01-02T15:04:05.0000000Z")

	st.SessionManager.UpdateSession(userID, u.Name, deviceID, deviceName, client, appVersion, clientIP)
	c.JSON(http.StatusOK, gin.H{
		"User": user,
		"SessionInfo": gin.H{
			"PlayState": gin.H{
				"CanSeek":        false,
				"IsPaused":       false,
				"IsMuted":        false,
				"RepeatMode":     "RepeatNone",
				"SleepTimerMode": "None",
				"SubtitleOffset": 0,
				"Shuffle":        false,
				"PlaybackRate":   1,
			},
			"AdditionalUsers":       []interface{}{},
			"RemoteEndPoint":        clientIP,
			"Protocol":              c.Request.Proto,
			"PlayableMediaTypes":    []interface{}{},
			"PlaylistIndex":         0,
			"PlaylistLength":        0,
			"Id":                    services.EmbySessionID(userID, deviceID),
			"ServerId":              st.Config.ServerID,
			"UserId":                userID,
			"UserName":              u.Name,
			"Client":                client,
			"LastActivityDate":      now,
			"DeviceName":            deviceName,
			"DeviceId":              deviceID,
			"ApplicationVersion":    appVersion,
			"SupportedCommands":     []interface{}{},
			"SupportsRemoteControl": false,
		},
		"AccessToken": token,
		"ServerId":    st.Config.ServerID,
	})
}
