package handlers

import (
	"github.com/gin-gonic/gin"

	"fyms/internal/middleware"
)

func matchUserOrAdmin(c *gin.Context, userID string) bool {
	u := middleware.GetAuthUser(c)
	if u == nil {
		return false
	}
	if u.IsAdmin {
		return true
	}
	return canonicalUserIDString(u.ID) == canonicalUserIDString(userID)
}

// resolveUserID 优先取 URL path 上的 :userId；为空时（如 DS One 这类客户端
// 仅依赖 token 反查）回退到当前已认证用户。
func resolveUserID(c *gin.Context) string {
	if uid := c.Param("userId"); uid != "" {
		return canonicalUserIDString(uid)
	}
	if u := middleware.GetAuthUser(c); u != nil {
		return canonicalUserIDString(u.ID)
	}
	return ""
}
