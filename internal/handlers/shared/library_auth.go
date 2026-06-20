package shared

import (
	"github.com/gin-gonic/gin"

	"fyms/internal/middleware"
)

func MatchUserOrAdmin(c *gin.Context, userID string) bool {
	u := middleware.GetAuthUser(c)
	if u == nil {
		return false
	}
	if u.IsAdmin {
		return true
	}
	return CanonicalUserIDString(u.ID) == CanonicalUserIDString(userID)
}

// resolveUserID 优先取 URL path 上的 :userId；为空时（如 DS One 这类客户端
// 仅依赖 token 反查）回退到当前已认证用户。
func ResolveUserID(c *gin.Context) string {
	if uid := c.Param("userId"); uid != "" {
		return CanonicalUserIDString(uid)
	}
	if u := middleware.GetAuthUser(c); u != nil {
		return CanonicalUserIDString(u.ID)
	}
	return ""
}
