package compat

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func emptyJSONArray(c *gin.Context) {
	c.JSON(http.StatusOK, []interface{}{})
}

func emptyOK(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

func stubPlugins(c *gin.Context) {
	c.JSON(http.StatusOK, []interface{}{})
}

func stubItemsEmpty(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"Items": []interface{}{}, "TotalRecordCount": 0})
}

// getNextUpItems 实现 Emby 的 /Shows/NextUp:首页"接下来观看"那一行的数据源。

func stubLiveTv(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"Services": []interface{}{}, "IsEnabled": false, "EnabledUsers": []interface{}{}})
}

func stubNotifications(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"UnreadCount": 0, "MaxUnreadCount": 0})
}

func ptrOrEmpty(p *string) string {
	if p != nil {
		return *p
	}
	return ""
}

func getDisplayPrefs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"Id":               "usersettings",
		"SortBy":           "SortName",
		"SortOrder":        "Ascending",
		"RememberIndexing": false,
		"RememberSorting":  false,
		"CustomPrefs":      gin.H{},
	})
}

func postDisplayPrefs(c *gin.Context) {
	c.Status(http.StatusNoContent)
}
