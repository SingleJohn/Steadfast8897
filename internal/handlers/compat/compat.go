package compat

import (
	"github.com/gin-gonic/gin"
)

// RegisterCompatRoutes registers Emby-compatible endpoints used by third-party clients and plugins.
func RegisterCompatRoutes(group *gin.RouterGroup, state *AppState, authMW, adminMW, optAuthMW gin.HandlerFunc) {
	_ = optAuthMW

	group.GET("/Sessions", adminMW, func(c *gin.Context) { getSessions(c, state) })
	group.GET("/DisplayPreferences/usersettings", getDisplayPrefs)
	group.POST("/DisplayPreferences/usersettings", postDisplayPrefs)
	group.GET("/DisplayPreferences/:id", getDisplayPrefs)
	group.POST("/DisplayPreferences/:id", postDisplayPrefs)

	group.GET("/Plugins", stubPlugins)
	group.GET("/Channels", stubItemsEmpty)
	group.GET("/Shows/NextUp", authMW, func(c *gin.Context) { getNextUpItems(c, state) })
	group.GET("/Studios", authMW, stubItemsEmpty)
	group.GET("/Artists", authMW, stubItemsEmpty)

	group.GET("/LiveTv/Info", stubLiveTv)
	group.GET("/LiveTv/Channels", stubItemsEmpty)
	group.GET("/LiveTv/Programs", stubItemsEmpty)

	group.GET("/Notifications", emptyJSONArray)
	group.GET("/Notifications/Types", emptyJSONArray)
	group.GET("/Notifications/:userId/Summary", stubNotifications)

	group.GET("/Shows/:seriesId/Seasons", authMW, func(c *gin.Context) { getSeasons(c, state) })
	group.GET("/Shows/:seriesId/Episodes", authMW, func(c *gin.Context) { getEpisodes(c, state) })

	group.POST("/Auth/Keys", adminMW, func(c *gin.Context) { createApiKey(c, state) })
	group.GET("/Auth/Keys", adminMW, func(c *gin.Context) { listApiKeys(c, state) })
	group.DELETE("/Auth/Keys/:keyId", adminMW, func(c *gin.Context) { deleteApiKey(c, state) })
	group.POST("/ApiKeys", adminMW, func(c *gin.Context) { createApiKey(c, state) })
	group.GET("/ApiKeys", adminMW, func(c *gin.Context) { listApiKeys(c, state) })
	group.DELETE("/ApiKeys/:keyId", adminMW, func(c *gin.Context) { deleteApiKey(c, state) })

	group.GET("/Items/Counts", authMW, func(c *gin.Context) { getItemCounts(c, state) })
	group.GET("/Items", authMW, func(c *gin.Context) { itemsSearch(c, state) })
	group.GET("/Devices", emptyJSONArray)
	group.GET("/Devices/Info", authMW, func(c *gin.Context) { deviceInfo(c, state) })
	group.GET("/Search/Hints", authMW, func(c *gin.Context) { searchHints(c, state) })

	group.POST("/Sessions/:sessionId/Playing/Stop", adminMW, func(c *gin.Context) { sessionStop(c, state) })
	group.POST("/Sessions/:sessionId/Message", authMW, func(c *gin.Context) { sessionMessage(c, state) })
	group.POST("/Sessions/Capabilities/Full", emptyOK)

	group.POST("/user_usage_stats/submit_custom_query", adminMW, func(c *gin.Context) { submitCustomQuery(c, state) })
	group.GET("/Persons", authMW, func(c *gin.Context) { getPersons(c, state) })
	// Emby Items-by-Name 单演员详情。第三方刮削器（mdc-ng 等）按名取详情，缺此路由会“未找到详情页”。
	group.GET("/Persons/:name", authMW, func(c *gin.Context) { getPersonByName(c, state) })
}
