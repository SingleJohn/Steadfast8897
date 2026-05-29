package proxy

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes registers the proxy download routes on the given engine.
// Routes: GET/HEAD /{alias}/{path} — alias must match a proxy_accounts row.
// Uses NoRoute fallback to avoid conflicting with existing API routes.
func RegisterRoutes(r *gin.Engine, svc *Service) {
	// Register as NoRoute handler: if no other route matches,
	// try to treat the first path segment as a proxy alias.
	prev := r.NoRoute
	_ = prev
	r.NoRoute(func(c *gin.Context) {
		path := strings.TrimLeft(c.Request.URL.Path, "/")
		if path == "" {
			c.JSON(http.StatusNotFound, gin.H{"message": "Not Found"})
			return
		}
		slash := strings.IndexByte(path, '/')
		if slash <= 0 {
			c.JSON(http.StatusNotFound, gin.H{"message": "Not Found"})
			return
		}
		alias := path[:slash]
		relPath := path[slash+1:]
		if relPath == "" {
			c.JSON(http.StatusNotFound, gin.H{"message": "Not Found"})
			return
		}

		// Check if this alias exists in proxy_accounts
		exists := svc.AliasExists(c.Request.Context(), alias)
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"message": "Not Found"})
			return
		}

		userAgent := c.Request.UserAgent()
		wantJSON := c.Query("json") == "1" || strings.Contains(c.GetHeader("Accept"), "application/json")

		result, err := svc.ResolveLink(c.Request.Context(), alias, relPath, userAgent)
		if err != nil {
			status := http.StatusBadGateway
			msg := err.Error()
			if strings.Contains(msg, "不存在") || strings.Contains(msg, "not found") {
				status = http.StatusNotFound
			} else if strings.Contains(msg, "已禁用") {
				status = http.StatusForbidden
			}
			c.JSON(status, gin.H{"message": msg})
			return
		}

		if wantJSON {
			c.JSON(http.StatusOK, gin.H{
				"url":        result.URL,
				"alias":      result.Alias,
				"type":       result.Type,
				"expires_at": result.ExpiresAt,
			})
			return
		}

		c.Redirect(http.StatusFound, result.URL)
	})
}

