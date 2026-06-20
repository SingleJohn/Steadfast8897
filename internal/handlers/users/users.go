package users

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"fyms/internal/handlers/shared"
	"fyms/internal/middleware"
	"fyms/internal/models"
	"fyms/internal/repository"
)

var (
	authHeaderReUsers = regexp.MustCompile(`(?i)^(?:MediaBrowser|Emby)\s+(.+)$`)
	pairReUsers       = regexp.MustCompile(`(\w+)="([^"]*)"`)
)

type loginAttempt struct {
	count       int
	lastAttempt time.Time
}

var (
	loginFailures   = make(map[string]*loginAttempt)
	loginFailuresMu sync.Mutex
)

func checkLoginRate(ip string) (int, error) {
	loginFailuresMu.Lock()
	defer loginFailuresMu.Unlock()
	entry, ok := loginFailures[ip]
	if !ok {
		return 0, nil
	}
	elapsed := time.Since(entry.lastAttempt)
	if elapsed > 15*time.Minute {
		delete(loginFailures, ip)
		return 0, nil
	}
	if entry.count >= 10 {
		remaining := 15*time.Minute - elapsed
		return 0, fmt.Errorf("Too many login attempts. Try again in %d seconds.", int(remaining.Seconds()))
	}
	return entry.count, nil
}

func recordLoginFailure(ip string) {
	loginFailuresMu.Lock()
	defer loginFailuresMu.Unlock()
	entry, ok := loginFailures[ip]
	if !ok || time.Since(entry.lastAttempt) > 15*time.Minute {
		loginFailures[ip] = &loginAttempt{count: 1, lastAttempt: time.Now()}
		return
	}
	entry.count++
	entry.lastAttempt = time.Now()
}

func clearLoginFailure(ip string) {
	loginFailuresMu.Lock()
	defer loginFailuresMu.Unlock()
	delete(loginFailures, ip)
}

func parseAuthHeaderUsers(header string) middleware.AuthInfo {
	var info middleware.AuthInfo
	m := authHeaderReUsers.FindStringSubmatch(header)
	if m == nil {
		return info
	}
	pairs := pairReUsers.FindAllStringSubmatch(m[1], -1)
	for _, p := range pairs {
		key := strings.ToLower(p[1])
		value, _ := url.QueryUnescape(p[2])
		switch key {
		case "userid":
			info.UserID = &value
		case "client":
			info.Client = &value
		case "device":
			info.Device = &value
		case "deviceid":
			info.DeviceID = &value
		case "version":
			info.Version = &value
		case "token":
			info.Token = &value
		}
	}
	return info
}

func requestAccessToken(c *gin.Context) string {
	var token string
	header := c.GetHeader("Authorization")
	if header == "" {
		header = c.GetHeader("X-Emby-Authorization")
	}
	if header != "" {
		info := parseAuthHeaderUsers(header)
		if info.Token != nil {
			token = *info.Token
		}
	}
	if token == "" {
		if t := c.GetHeader("X-Emby-Token"); t != "" {
			token = t
		} else if t := c.GetHeader("X-MediaBrowser-Token"); t != "" {
			token = t
		}
	}
	if token == "" {
		if t := c.Query("api_key"); t != "" {
			token = t
		} else if t := c.Query("ApiKey"); t != "" {
			token = t
		}
	}
	return token
}

func strOrPtr(a *string, def string) string {
	if a != nil && *a != "" {
		return *a
	}
	return def
}

func formatTimeRFC3339(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func buildUserResponse(ctx context.Context, st *AppState, u *repository.User, includeConfig bool) (map[string]interface{}, error) {
	policy, err := st.Repo.Users.GetUserPolicy(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	policyResp := formatRepositoryPolicyResponse(policy, u.IsAdmin)
	policyResp["IsDisabled"] = u.IsDisabled
	policyResp["IsHidden"] = u.IsHidden

	resp := map[string]interface{}{
		"Name":                      u.Name,
		"ServerId":                  st.Config.ServerID,
		"Id":                        u.ID.String(),
		"HasPassword":               true,
		"HasConfiguredPassword":     true,
		"HasConfiguredEasyPassword": false,
		"Policy":                    policyResp,
	}
	// Match Rust: only include date fields when non-nil
	if u.LastLoginDate != nil {
		resp["LastLoginDate"] = u.LastLoginDate.UTC().Format("2006-01-02T15:04:05.0000000Z")
	}
	if u.LastActivityDate != nil {
		resp["LastActivityDate"] = u.LastActivityDate.UTC().Format("2006-01-02T15:04:05.0000000Z")
	}

	if includeConfig {
		resp["Configuration"] = map[string]interface{}{
			"PlayDefaultAudioTrack":      true,
			"SubtitleLanguagePreference": "",
			"DisplayMissingEpisodes":     false,
			"SubtitleMode":               "Default",
			"EnableLocalPassword":        false,
			"OrderedViews":               []interface{}{},
			"LatestItemsExcludes":        []interface{}{},
			"MyMediaExcludes":            []interface{}{},
			"HidePlayedInLatest":         true,
			"RememberAudioSelections":    true,
			"RememberSubtitleSelections": true,
			"EnableNextEpisodeAutoPlay":  true,
		}
	}

	return resp, nil
}

func formatRepositoryPolicyResponse(policy *repository.UserPolicy, isAdmin bool) map[string]interface{} {
	if policy != nil {
		blockedFolders := policy.BlockedMediaFolders
		if blockedFolders == nil {
			blockedFolders = []string{}
		}
		enabledFolders := policy.EnabledFolders
		if enabledFolders == nil {
			enabledFolders = []string{}
		}
		return map[string]interface{}{
			"IsAdministrator":                 policy.IsAdministrator,
			"IsDisabled":                      false,
			"IsHidden":                        false,
			"EnableAllFolders":                policy.EnableAllFolders,
			"BlockedMediaFolders":             blockedFolders,
			"EnabledFolders":                  enabledFolders,
			"EnableRemoteAccess":              policy.EnableRemoteAccess,
			"EnableMediaPlayback":             policy.EnableMediaPlayback,
			"EnableAudioPlaybackTranscoding":  policy.EnableAudioTranscoding,
			"EnableVideoPlaybackTranscoding":  policy.EnableVideoTranscoding,
			"EnablePlaybackRemuxing":          policy.EnablePlaybackRemuxing,
			"EnableContentDeletion":           policy.EnableContentDeletion,
			"EnableContentDownloading":        policy.EnableContentDownloading,
			"EnableSubtitleDownloading":       policy.EnableSubtitleManagement,
			"EnableSubtitleManagement":        policy.EnableSubtitleManagement,
			"EnableLiveTvAccess":              policy.EnableLiveTvAccess,
			"EnableLiveTvManagement":          policy.EnableLiveTvManagement,
			"EnableUserPreferenceAccess":      policy.EnableUserPreferenceAccess,
			"EnableRemoteControlOfOtherUsers": policy.EnableRemoteControl,
			"EnableSharedDeviceControl":       policy.EnableSharedDeviceControl,
			"MaxParentalRating":               policy.MaxParentalRating,
			"RemoteClientBitrateLimit":        policy.RemoteClientBitrateLimit,
			"SimultaneousStreamLimit":         policy.SimultaneousStreamLimit,
			"EnableSyncTranscoding":           true,
			"EnableMediaConversion":           true,
		}
	}

	return map[string]interface{}{
		"IsAdministrator":                 isAdmin,
		"IsDisabled":                      false,
		"IsHidden":                        false,
		"EnableAllFolders":                true,
		"EnableRemoteAccess":              true,
		"EnableMediaPlayback":             true,
		"EnableAudioPlaybackTranscoding":  true,
		"EnableVideoPlaybackTranscoding":  true,
		"EnablePlaybackRemuxing":          true,
		"EnableContentDeletion":           isAdmin,
		"EnableContentDownloading":        true,
		"EnableSubtitleDownloading":       true,
		"EnableSubtitleManagement":        true,
		"EnableLiveTvAccess":              true,
		"EnableLiveTvManagement":          false,
		"EnableUserPreferenceAccess":      true,
		"EnableRemoteControlOfOtherUsers": false,
		"EnableSharedDeviceControl":       false,
		"MaxParentalRating":               nil,
		"RemoteClientBitrateLimit":        0,
		"SimultaneousStreamLimit":         0,
		"EnableSyncTranscoding":           true,
		"EnableMediaConversion":           true,
	}
}

// RegisterUserRoutes registers Emby-compatible user and startup routes.
func RegisterUserRoutes(group *gin.RouterGroup, state *AppState, authMW, adminMW, optAuthMW gin.HandlerFunc) {
	_ = state
	group.GET("/Users/Public", GetPublicUsers)
	group.GET("/Users", authMW, GetAllUsers)
	group.GET("/Users/Query", authMW, QueryUsers)
	group.GET("/Users/Me", authMW, getMe)
	group.POST("/Users/New", adminMW, CreateUser)
	group.POST("/Users/AuthenticateByName", optAuthMW, AuthenticateByName)
	group.POST("/Users/authenticatebyname", optAuthMW, AuthenticateByName)

	group.GET("/Startup/User", StartupUser)
	group.POST("/Startup/User", createStartupUser)
	group.POST("/Startup/Complete", StartupComplete)
	group.GET("/Startup/Configuration", StartupConfiguration)

	group.POST("/Sessions/Logout", authMW, Logout)

	group.GET("/Users/:userId", authMW, GetUserByID)
	group.POST("/Users/:userId", authMW, UpdateUser)
	group.DELETE("/Users/:userId", adminMW, DeleteUser)
	group.POST("/Users/:userId/Password", authMW, ChangePassword)
	group.POST("/Users/:userId/Policy", adminMW, UpdatePolicy)
	group.POST("/Users/:userId/Authenticate", optAuthMW, AuthenticateByUserID)
	group.POST("/Users/:userId/Configuration", authMW, userConfiguration)
}

func GetPublicUsers(c *gin.Context) {
	c.JSON(http.StatusOK, []interface{}{})
}

func GetAllUsers(c *gin.Context) {
	st := GetState(c)
	ctx := c.Request.Context()
	users, err := st.Repo.Users.ListUsers(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	out := make([]map[string]interface{}, 0, len(users))
	for i := range users {
		m, err := buildUserResponse(ctx, st, &users[i], true)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		out = append(out, m)
	}
	c.JSON(http.StatusOK, out)
}

func QueryUsers(c *gin.Context) {
	st := GetState(c)
	ctx := c.Request.Context()

	nameFilter := c.Query("NameStartsWithOrGreater")
	if nameFilter == "" {
		nameFilter = c.Query("nameStartsWithOrGreater")
	}

	users, err := st.Repo.Users.ListUsers(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	out := make([]map[string]interface{}, 0)
	for i := range users {
		if nameFilter != "" && users[i].Name < nameFilter {
			continue
		}
		m, err := buildUserResponse(ctx, st, &users[i], true)
		if err != nil {
			continue
		}
		out = append(out, m)
	}
	c.JSON(http.StatusOK, gin.H{
		"Items":            out,
		"TotalRecordCount": len(out),
	})
}

type createUserBody struct {
	Name     string `json:"Name"`
	Password string `json:"Password"`
}

func CreateUser(c *gin.Context) {
	st := GetState(c)
	var body createUserBody
	if err := c.ShouldBindJSON(&body); err != nil || body.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request body"})
		return
	}
	password := body.Password
	if password == "" {
		password = uuid.New().String()[:16]
	}
	u, err := st.Repo.Users.CreateUser(c.Request.Context(), body.Name, password, false)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	m, err := buildUserResponse(c.Request.Context(), st, u, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, m)
}

func GetUserByID(c *gin.Context) {
	st := GetState(c)
	uid, err := shared.ParseUserIDParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid user id"})
		return
	}
	auth := middleware.GetAuthUser(c)
	if auth == nil || (!auth.IsAdmin && auth.ID != uid.String()) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}
	u, err := st.Repo.Users.GetUserByID(c.Request.Context(), uid)
	if err != nil || u == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "User not found"})
		return
	}
	m, err := buildUserResponse(c.Request.Context(), st, u, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, m)
}

type updateUserBody struct {
	Name   string               `json:"Name"`
	Policy *updateUserPolicyVal `json:"Policy,omitempty"`
}

type updateUserPolicyVal struct {
	IsHidden                        *bool    `json:"IsHidden,omitempty"`
	IsDisabled                      *bool    `json:"IsDisabled,omitempty"`
	IsAdministrator                 *bool    `json:"IsAdministrator,omitempty"`
	EnableAllFolders                *bool    `json:"EnableAllFolders,omitempty"`
	EnableRemoteAccess              *bool    `json:"EnableRemoteAccess,omitempty"`
	EnableMediaPlayback             *bool    `json:"EnableMediaPlayback,omitempty"`
	EnableAudioPlaybackTranscoding  *bool    `json:"EnableAudioPlaybackTranscoding,omitempty"`
	EnableVideoPlaybackTranscoding  *bool    `json:"EnableVideoPlaybackTranscoding,omitempty"`
	EnablePlaybackRemuxing          *bool    `json:"EnablePlaybackRemuxing,omitempty"`
	EnableContentDeletion           *bool    `json:"EnableContentDeletion,omitempty"`
	EnableContentDownloading        *bool    `json:"EnableContentDownloading,omitempty"`
	EnableSubtitleManagement        *bool    `json:"EnableSubtitleManagement,omitempty"`
	EnableLiveTvAccess              *bool    `json:"EnableLiveTvAccess,omitempty"`
	EnableLiveTvManagement          *bool    `json:"EnableLiveTvManagement,omitempty"`
	EnableUserPreferenceAccess      *bool    `json:"EnableUserPreferenceAccess,omitempty"`
	EnableRemoteControlOfOtherUsers *bool    `json:"EnableRemoteControlOfOtherUsers,omitempty"`
	EnableSharedDeviceControl       *bool    `json:"EnableSharedDeviceControl,omitempty"`
	RemoteClientBitrateLimit        *int32   `json:"RemoteClientBitrateLimit,omitempty"`
	SimultaneousStreamLimit         *int32   `json:"SimultaneousStreamLimit,omitempty"`
	BlockedMediaFolders             []string `json:"BlockedMediaFolders,omitempty"`
	EnabledFolders                  []string `json:"EnabledFolders,omitempty"`
}

func UpdateUser(c *gin.Context) {
	st := GetState(c)
	ctx := c.Request.Context()
	uid, err := shared.ParseUserIDParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid user id"})
		return
	}
	auth := middleware.GetAuthUser(c)
	if auth == nil || (!auth.IsAdmin && auth.ID != uid.String()) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}
	var body updateUserBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request body"})
		return
	}

	var newName *string
	if body.Name != "" {
		newName = &body.Name
	}
	var newHidden *bool
	if body.Policy != nil {
		if body.Policy.IsHidden != nil {
			newHidden = body.Policy.IsHidden
		}
		if body.Policy.IsDisabled != nil {
			_ = st.Repo.Users.SetUserDisabled(ctx, uid, *body.Policy.IsDisabled)
		}
		pu := policyValToUpdate(body.Policy)
		if hasPolicyField(&pu) {
			if err := models.UpsertUserPolicy(ctx, st.DB, uid, &pu); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
			st.Cache.DelPattern(ctx, "views:*")
		}
	}

	if _, err := st.Repo.Users.UpdateUser(ctx, uid, newName, newHidden); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	updated, err := st.Repo.Users.GetUserByID(ctx, uid)
	if err != nil || updated == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "User not found"})
		return
	}
	m, err := buildUserResponse(ctx, st, updated, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, m)
}

func policyValToUpdate(p *updateUserPolicyVal) models.PolicyUpdate {
	var pu models.PolicyUpdate
	pu.IsAdministrator = p.IsAdministrator
	pu.EnableAllFolders = p.EnableAllFolders
	pu.EnableRemoteAccess = p.EnableRemoteAccess
	pu.EnableMediaPlayback = p.EnableMediaPlayback
	pu.EnableAudioTranscoding = p.EnableAudioPlaybackTranscoding
	pu.EnableVideoTranscoding = p.EnableVideoPlaybackTranscoding
	pu.EnablePlaybackRemuxing = p.EnablePlaybackRemuxing
	pu.EnableContentDeletion = p.EnableContentDeletion
	pu.EnableContentDownloading = p.EnableContentDownloading
	pu.EnableSubtitleManagement = p.EnableSubtitleManagement
	pu.EnableLiveTvAccess = p.EnableLiveTvAccess
	pu.EnableLiveTvManagement = p.EnableLiveTvManagement
	pu.EnableUserPreferenceAccess = p.EnableUserPreferenceAccess
	pu.EnableRemoteControl = p.EnableRemoteControlOfOtherUsers
	pu.EnableSharedDeviceControl = p.EnableSharedDeviceControl
	pu.RemoteClientBitrateLimit = p.RemoteClientBitrateLimit
	pu.SimultaneousStreamLimit = p.SimultaneousStreamLimit
	pu.BlockedMediaFolders = p.BlockedMediaFolders
	pu.EnabledFolders = p.EnabledFolders
	return pu
}

func hasPolicyField(p *models.PolicyUpdate) bool {
	return p.IsAdministrator != nil || p.EnableAllFolders != nil || p.EnableRemoteAccess != nil ||
		p.EnableMediaPlayback != nil || p.EnableAudioTranscoding != nil || p.EnableVideoTranscoding != nil ||
		p.EnablePlaybackRemuxing != nil || p.EnableContentDeletion != nil || p.EnableContentDownloading != nil ||
		p.EnableSubtitleManagement != nil || p.EnableLiveTvAccess != nil || p.EnableLiveTvManagement != nil ||
		p.EnableUserPreferenceAccess != nil || p.EnableRemoteControl != nil || p.EnableSharedDeviceControl != nil ||
		p.RemoteClientBitrateLimit != nil || p.SimultaneousStreamLimit != nil ||
		p.BlockedMediaFolders != nil || p.EnabledFolders != nil
}

func DeleteUser(c *gin.Context) {
	st := GetState(c)
	uid, err := shared.ParseUserIDParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid user id"})
		return
	}
	if err := st.Repo.Users.DeleteUser(c.Request.Context(), uid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

type authenticateByNameBody struct {
	Username string `json:"Username"`
	Pw       string `json:"Pw"`
	Password string `json:"Password"`
}

func authenticateResponse(c *gin.Context, st *AppState, u *repository.User, token string) {
	m, err := buildUserResponse(c.Request.Context(), st, u, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	// Match Rust: only User + AccessToken + ServerId (no SessionInfo)
	c.JSON(http.StatusOK, gin.H{
		"User":        m,
		"AccessToken": token,
		"ServerId":    st.Config.ServerID,
	})
}

func AuthenticateByName(c *gin.Context) {
	st := GetState(c)

	ip := c.GetHeader("X-Forwarded-For")
	if ip != "" {
		ip = strings.SplitN(ip, ",", 2)[0]
		ip = strings.TrimSpace(ip)
	}
	if ip == "" {
		ip = c.GetHeader("X-Real-IP")
	}
	if ip == "" {
		ip = c.ClientIP()
	}
	if _, err := checkLoginRate(ip); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"message": err.Error()})
		return
	}

	var body authenticateByNameBody
	contentType := c.GetHeader("Content-Type")

	// Read raw body for flexible parsing
	rawBody, _ := io.ReadAll(c.Request.Body)
	c.Request.Body = io.NopCloser(strings.NewReader(string(rawBody)))

	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		body.Username = c.PostForm("Username")
		if body.Username == "" {
			body.Username = c.PostForm("username")
		}
		body.Pw = c.PostForm("Pw")
		if body.Pw == "" {
			body.Pw = c.PostForm("pw")
		}
		if body.Pw == "" {
			body.Pw = c.PostForm("Password")
		}
	} else {
		// Try standard JSON binding first
		_ = c.ShouldBindJSON(&body)
		// If Username is empty, try case-insensitive parsing
		if body.Username == "" {
			var raw map[string]interface{}
			if json.Unmarshal(rawBody, &raw) == nil {
				for k, v := range raw {
					s, _ := v.(string)
					switch strings.ToLower(k) {
					case "username", "name":
						if body.Username == "" {
							body.Username = s
						}
					case "pw", "password":
						if body.Pw == "" {
							body.Pw = s
						}
					}
				}
			}
		}
	}
	if body.Username == "" {
		slog.Warn("login: empty username", "content_type", contentType, "body", string(rawBody))
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request body"})
		return
	}
	password := body.Pw
	if password == "" {
		password = body.Password
	}

	info := middleware.GetAuthInfo(c)
	if qs := c.Request.URL.RawQuery; qs != "" {
		if info.DeviceID == nil || *info.DeviceID == "" {
			if v := c.Query("X-Emby-Device-Id"); v != "" {
				info.DeviceID = &v
			}
		}
		if info.Device == nil || *info.Device == "" {
			if v := c.Query("X-Emby-Device-Name"); v != "" {
				decoded, _ := url.QueryUnescape(v)
				info.Device = &decoded
			}
		}
		if info.Client == nil || *info.Client == "" {
			if v := c.Query("X-Emby-Client"); v != "" {
				info.Client = &v
			}
		}
		if info.Version == nil || *info.Version == "" {
			if v := c.Query("X-Emby-Client-Version"); v != "" {
				info.Version = &v
			}
		}
	}

	u, err := st.Repo.Users.GetUserByName(c.Request.Context(), body.Username)
	if err != nil || u == nil {
		recordLoginFailure(ip)
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Invalid username or password"})
		return
	}
	if u.IsDisabled {
		recordLoginFailure(ip)
		c.JSON(http.StatusForbidden, gin.H{"message": "User is disabled"})
		return
	}
	if !st.Repo.Users.VerifyPassword(c.Request.Context(), u, password) {
		recordLoginFailure(ip)
		slog.Warn("login: password mismatch", "username", body.Username, "pw_len", len(password), "pw_raw", password, "body_pw", body.Pw, "body_password", body.Password, "content_type", contentType, "raw_body", string(rawBody))
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Invalid username or password"})
		return
	}
	clearLoginFailure(ip)
	middleware.SetAuthInfo(c, info)
	token, err := st.Repo.Sessions.CreateAccessToken(c.Request.Context(), u.ID,
		strOrPtr(info.DeviceID, "unknown"),
		strOrPtr(info.Device, ""),
		strOrPtr(info.Client, "FYMS"),
		strOrPtr(info.Version, st.Config.Version),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_ = st.Repo.Users.UpdateLastLogin(c.Request.Context(), u.ID)
	authenticateResponse(c, st, u, token)
}

type changePasswordBody struct {
	CurrentPw string `json:"CurrentPw"`
	NewPw     string `json:"NewPw"`
}

func ChangePassword(c *gin.Context) {
	st := GetState(c)
	uid, err := shared.ParseUserIDParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid user id"})
		return
	}
	auth := middleware.GetAuthUser(c)
	if auth == nil || (!auth.IsAdmin && auth.ID != uid.String()) {
		c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden"})
		return
	}
	var body changePasswordBody
	contentType := c.GetHeader("Content-Type")
	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		body.CurrentPw = c.PostForm("CurrentPw")
		body.NewPw = c.PostForm("NewPw")
	} else {
		_ = c.ShouldBindJSON(&body)
	}
	u, err := st.Repo.Users.GetUserByID(c.Request.Context(), uid)
	if err != nil || u == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "User not found"})
		return
	}
	if !auth.IsAdmin {
		if !st.Repo.Users.VerifyPassword(c.Request.Context(), u, body.CurrentPw) {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "Invalid current password"})
			return
		}
	}
	if err := st.Repo.Users.UpdatePassword(c.Request.Context(), uid, body.NewPw); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func UpdatePolicy(c *gin.Context) {
	st := GetState(c)
	uid, err := shared.ParseUserIDParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid user id"})
		return
	}
	var body models.PolicyUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request body"})
		return
	}
	if err := models.UpsertUserPolicy(c.Request.Context(), st.DB, uid, &body); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	st.Cache.DelPattern(c.Request.Context(), "views:*")
	u, err := st.Repo.Users.GetUserByID(c.Request.Context(), uid)
	if err != nil || u == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "User not found"})
		return
	}
	m, err := buildUserResponse(c.Request.Context(), st, u, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, m)
}

type authenticateByIDBody struct {
	Pw string `json:"Pw"`
}

func AuthenticateByUserID(c *gin.Context) {
	st := GetState(c)
	uid, err := shared.ParseUserIDParam(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid user id"})
		return
	}
	var body authenticateByIDBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid request body"})
		return
	}
	u, err := st.Repo.Users.GetUserByID(c.Request.Context(), uid)
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Invalid user or password"})
		return
	}
	if u.IsDisabled {
		c.JSON(http.StatusForbidden, gin.H{"message": "User is disabled"})
		return
	}
	if !st.Repo.Users.VerifyPassword(c.Request.Context(), u, body.Pw) {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Invalid user or password"})
		return
	}
	info := middleware.GetAuthInfo(c)
	token, err := st.Repo.Sessions.CreateAccessToken(c.Request.Context(), u.ID,
		strOrPtr(info.DeviceID, "unknown"),
		strOrPtr(info.Device, ""),
		strOrPtr(info.Client, "FYMS"),
		strOrPtr(info.Version, st.Config.Version),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_ = st.Repo.Users.UpdateLastLogin(c.Request.Context(), u.ID)
	authenticateResponse(c, st, u, token)
}

func Logout(c *gin.Context) {
	st := GetState(c)
	token := requestAccessToken(c)
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "No token"})
		return
	}
	ctx := c.Request.Context()
	if err := st.Repo.Sessions.DeleteAccessToken(ctx, token); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	st.Cache.Del(ctx, fmt.Sprintf("auth:%s", token))
	c.Status(http.StatusNoContent)
}

func StartupUser(c *gin.Context) {
	st := GetState(c)
	ctx := c.Request.Context()
	count, _ := st.Repo.Users.CountUsers(ctx)
	if count > 0 {
		c.JSON(http.StatusForbidden, gin.H{"message": "Setup already complete"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"Name":     "",
		"ServerId": st.Config.ServerID,
	})
}

func StartupComplete(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

func StartupConfiguration(c *gin.Context) {
	st := GetState(c)
	ctx := c.Request.Context()
	count, _ := st.Repo.Users.CountUsers(ctx)
	c.JSON(http.StatusOK, gin.H{
		"IsComplete":                count > 0,
		"UICulture":                 "zh-CN",
		"MetadataCountryCode":       "CN",
		"PreferredMetadataLanguage": "zh",
	})
}

func getMe(c *gin.Context) {
	st := GetState(c)
	auth := middleware.GetAuthUser(c)
	if auth == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
		return
	}
	uid, err := uuid.Parse(auth.ID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
		return
	}
	u, err := st.Repo.Users.GetUserByID(c.Request.Context(), uid)
	if err != nil || u == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "User not found"})
		return
	}
	m, err := buildUserResponse(c.Request.Context(), st, u, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, m)
}

type startupUserBody struct {
	Name     string `json:"Name"`
	Password string `json:"Password"`
}

func createStartupUser(c *gin.Context) {
	st := GetState(c)
	ctx := c.Request.Context()

	var count int64
	count, _ = st.Repo.Users.CountUsers(ctx)
	if count > 0 {
		c.JSON(http.StatusForbidden, gin.H{"message": "Setup already completed"})
		return
	}

	var body startupUserBody
	if err := c.ShouldBindJSON(&body); err != nil || body.Name == "" || body.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Name and Password required"})
		return
	}

	u, err := st.Repo.Users.CreateUser(ctx, body.Name, body.Password, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	m, err := buildUserResponse(ctx, st, u, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, m)
}

func userConfiguration(c *gin.Context) {
	c.Status(http.StatusNoContent)
}
