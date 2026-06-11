package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/services"
)

type AuthUser struct {
	ID      string
	Name    string
	IsAdmin bool
}

type AuthInfo struct {
	UserID   *string
	Client   *string
	Device   *string
	DeviceID *string
	Version  *string
	Token    *string
}

type cachedAuth struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	IsAdmin    bool    `json:"is_admin"`
	DeviceID   *string `json:"device_id,omitempty"`
	DeviceName *string `json:"device_name,omitempty"`
	AppName    *string `json:"app_name,omitempty"`
}

const (
	authUserKey = "auth_user"
	authInfoKey = "auth_info"
)

var (
	authHeaderRe = regexp.MustCompile(`(?i)^(?:MediaBrowser|Emby)\s+(.+)$`)
	// 同时兼容带引号 key="value" 与不带引号 key=value(逗号分隔)两种格式:
	// 标准 Emby/Web/Infuse 用前者,Yamby 等部分客户端用后者(不加引号),
	// 旧的仅匹配引号的正则会让这些客户端的 Client/Device 全部解析失败 → 显示 Unknown。
	pairRe = regexp.MustCompile(`(\w+)=(?:"([^"]*)"|([^,]*))`)
)

func parseAuthHeader(header string) AuthInfo {
	var info AuthInfo
	m := authHeaderRe.FindStringSubmatch(header)
	if m == nil {
		return info
	}
	pairs := pairRe.FindAllStringSubmatch(m[1], -1)
	for _, p := range pairs {
		key := strings.ToLower(p[1])
		raw := p[2]
		if raw == "" {
			raw = strings.TrimSpace(p[3])
		}
		value, _ := url.QueryUnescape(raw)
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

func extractToken(c *gin.Context) (string, AuthInfo) {
	var authInfo AuthInfo
	var token string

	header := c.GetHeader("Authorization")
	if header == "" {
		header = c.GetHeader("X-Emby-Authorization")
	}
	if header != "" {
		authInfo = parseAuthHeader(header)
		if authInfo.Token != nil {
			token = *authInfo.Token
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

	return token, authInfo
}

func validateToken(ctx context.Context, pool *pgxpool.Pool, cache *services.CacheService, sm *services.SessionManager, token string, authInfo *AuthInfo) *AuthUser {
	cacheKey := fmt.Sprintf("auth:%s", token)

	var cached cachedAuth
	if cache.GetJSON(ctx, cacheKey, &cached) {
		deviceID := strOr(authInfo.DeviceID, cached.DeviceID, "unknown")
		deviceName := strOr(authInfo.Device, cached.DeviceName, "")
		appName := strOr(authInfo.Client, cached.AppName, "")
		version := ""
		if authInfo.Version != nil {
			version = *authInfo.Version
		}
		sm.UpdateSession(cached.ID, cached.Name, deviceID, deviceName, appName, version, "")
		return &AuthUser{ID: cached.ID, Name: cached.Name, IsAdmin: cached.IsAdmin}
	}

	var tokenRow struct {
		UserID     uuid.UUID
		DeviceID   string
		DeviceName string
		AppName    string
		AppVersion string
	}
	err := pool.QueryRow(ctx,
		"SELECT user_id, device_id, device_name, app_name, app_version FROM access_tokens WHERE token = $1",
		token).Scan(&tokenRow.UserID, &tokenRow.DeviceID, &tokenRow.DeviceName, &tokenRow.AppName, &tokenRow.AppVersion)

	if err == nil {
		var userName string
		var isAdmin, isDisabled bool
		err := pool.QueryRow(ctx,
			"SELECT name, is_admin, is_disabled FROM users WHERE id = $1",
			tokenRow.UserID).Scan(&userName, &isAdmin, &isDisabled)
		if err != nil || isDisabled {
			return nil
		}

		userID := tokenRow.UserID.String()
		ca := cachedAuth{
			ID: userID, Name: userName, IsAdmin: isAdmin,
			DeviceID: &tokenRow.DeviceID, DeviceName: &tokenRow.DeviceName, AppName: &tokenRow.AppName,
		}
		cache.SetJSON(ctx, cacheKey, ca, 5*time.Minute)

		deviceID := strOr(authInfo.DeviceID, &tokenRow.DeviceID, tokenRow.DeviceID)
		deviceName := strOr(authInfo.Device, &tokenRow.DeviceName, tokenRow.DeviceName)
		appName := strOr(authInfo.Client, &tokenRow.AppName, tokenRow.AppName)
		version := strOr(authInfo.Version, &tokenRow.AppVersion, tokenRow.AppVersion)
		sm.UpdateSession(userID, userName, deviceID, deviceName, appName, version, "")

		return &AuthUser{ID: userID, Name: userName, IsAdmin: isAdmin}
	}

	if err != pgx.ErrNoRows {
		slog.Error("Error checking access token", "error", err)
	}

	apiCacheKey := fmt.Sprintf("apikey:%s", token)
	var apiKeyID string
	if v, ok := cache.Get(ctx, apiCacheKey); ok {
		apiKeyID = v
	} else {
		var kid uuid.UUID
		err := pool.QueryRow(ctx, "SELECT id FROM api_keys WHERE key = $1", token).Scan(&kid)
		if err == nil {
			apiKeyID = kid.String()
			cache.Set(ctx, apiCacheKey, apiKeyID, 10*time.Minute)
		}
	}

	if apiKeyID != "" {
		go func() {
			pool.Exec(context.Background(),
				"UPDATE api_keys SET last_used_at = NOW() WHERE id = $1::uuid", apiKeyID)
		}()

		apiUser := AuthUser{
			ID:      fmt.Sprintf("api-key-%s", apiKeyID),
			Name:    "API",
			IsAdmin: true,
		}
		ca := cachedAuth{ID: apiUser.ID, Name: apiUser.Name, IsAdmin: true}
		cache.SetJSON(ctx, cacheKey, ca, 10*time.Minute)
		return &apiUser
	}

	return nil
}

func strOr(a *string, b *string, def string) string {
	if a != nil && *a != "" {
		return *a
	}
	if b != nil && *b != "" {
		return *b
	}
	return def
}

func RequireAuth(pool *pgxpool.Pool, cache *services.CacheService, sm *services.SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, authInfo := extractToken(c)
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
			c.Abort()
			return
		}

		user := validateToken(c.Request.Context(), pool, cache, sm, token, &authInfo)
		if user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "Invalid token"})
			c.Abort()
			return
		}

		c.Set(authUserKey, user)
		c.Set(authInfoKey, &authInfo)
		c.Next()
	}
}

func RequireAdmin(pool *pgxpool.Pool, cache *services.CacheService, sm *services.SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, authInfo := extractToken(c)
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
			c.Abort()
			return
		}

		user := validateToken(c.Request.Context(), pool, cache, sm, token, &authInfo)
		if user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "Invalid token"})
			c.Abort()
			return
		}

		if !user.IsAdmin {
			c.JSON(http.StatusForbidden, gin.H{"message": "Admin only"})
			c.Abort()
			return
		}

		c.Set(authUserKey, user)
		c.Set(authInfoKey, &authInfo)
		c.Next()
	}
}

func OptionalAuth(pool *pgxpool.Pool, cache *services.CacheService, sm *services.SessionManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, authInfo := extractToken(c)
		if token != "" {
			user := validateToken(c.Request.Context(), pool, cache, sm, token, &authInfo)
			if user != nil {
				c.Set(authUserKey, user)
			}
		}
		c.Set(authInfoKey, &authInfo)
		c.Next()
	}
}

func GetAuthUser(c *gin.Context) *AuthUser {
	if v, exists := c.Get(authUserKey); exists {
		if u, ok := v.(*AuthUser); ok {
			return u
		}
	}
	return nil
}

func GetAuthInfo(c *gin.Context) *AuthInfo {
	if v, exists := c.Get(authInfoKey); exists {
		if i, ok := v.(*AuthInfo); ok {
			return i
		}
	}
	return &AuthInfo{}
}

func SetAuthInfo(c *gin.Context, info *AuthInfo) {
	c.Set(authInfoKey, info)
}
