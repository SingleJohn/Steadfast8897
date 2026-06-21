package library

import (
	"github.com/gin-gonic/gin"
)

func RegisterLibraryRoutes(group *gin.RouterGroup, state *AppState, authMW, adminMW, optAuthMW gin.HandlerFunc) {
	u := group.Group("")
	u.GET("/Users/:userId/Views", authMW, getUserViews)
	u.GET("/Users/:userId/Items", authMW, getItems)
	u.GET("/Users/:userId/Items/Resume", authMW, getResumeItems)
	u.GET("/Users/:userId/Items/Latest", authMW, getLatestItems)
	u.GET("/Users/:userId/Items/:itemId", authMW, getItemDetail)

	// Forward 等客户端会省略 :userId 段直接请求 /Users/Views、/Users/Items 等;
	// 这里挂上无 userId 段的兼容路由,handler 通过 resolveUserID 从 token 取 userId。
	u.GET("/Users/Views", authMW, getUserViews)
	u.GET("/Users/Items", authMW, getItems)
	u.GET("/Users/Items/Resume", authMW, getResumeItems)
	u.GET("/Users/Items/Latest", authMW, getLatestItems)

	u.GET("/Items/:itemId/Similar", optAuthMW, getSimilarItems)

	// 标准 Emby 单条目(无 user 维度):EM 等管理端依赖。
	// getItemDetail 内部 resolveUserID 在缺 :userId 时回退到当前 token 用户。
	u.GET("/Items/:itemId", authMW, getItemDetail)
	// Emby 条目元数据更新(POST /Items/{Id})。第三方刮削器(mdc-ng)回写演员
	// Overview/ProviderIds 等;真实 Emby 返回 204。当前仅持久化 person 可存字段。
	u.POST("/Items/:itemId", adminMW, updateItemMetadata)

	u.POST("/Library/VirtualFolders", adminMW, addLibrary)
	u.DELETE("/Library/VirtualFolders", adminMW, deleteLibrary)
	u.POST("/Library/VirtualFolders/Name", adminMW, renameLibrary)
	u.POST("/Library/VirtualFolders/Paths", adminMW, addLibraryPath)
	u.DELETE("/Library/VirtualFolders/Paths", adminMW, removeLibraryPath)

	u.POST("/Items/:itemId/Images/:imageType", adminMW, uploadImage)
	u.DELETE("/Items/:itemId/Images/:imageType", adminMW, deleteImage)
	u.GET("/Items/:itemId/DeleteInfo", adminMW, getItemDeleteInfo)
	u.POST("/Items/:itemId/Delete", adminMW, deleteItemCompat)
	u.DELETE("/Items/:itemId", adminMW, deleteItemCompat)

	// 演员头像:批量按名/TMDB 补全 + 覆盖统计。
	u.POST("/Library/ActorImages/BackfillAll", adminMW, backfillAllActorImages)
	u.GET("/Library/ActorImages/Summary", adminMW, actorImageSummary)

	// 演员管理(后台人工核对/编辑/清理)。头像上传/删除复用 /Items/{id}/Images/...。
	u.GET("/Library/Actors", adminMW, listActorsAdmin)
	u.POST("/Library/Actors/BulkDelete", adminMW, bulkDeleteActors)
	u.GET("/Library/Actors/:id", adminMW, getActorAdmin)
	u.PATCH("/Library/Actors/:id", adminMW, updateActorAdmin)
	u.DELETE("/Library/Actors/:id", adminMW, deleteActor)

	u.POST("/Library/Refresh", adminMW, refreshAll)
	u.POST("/Items/:itemId/Refresh", adminMW, refreshItem)

	u.GET("/Library/VirtualFolders", authMW, getVirtualFolders)
	u.GET("/Library/VirtualFolders/Query", authMW, getVirtualFolders)
	u.GET("/Library/VirtualFolders/:id", authMW, getVirtualFolderDetail)
	u.POST("/Library/VirtualFolders/Add", adminMW, addLibrary)
	u.POST("/Library/VirtualFolders/Update", adminMW, updateLibraryInfo)
	u.POST("/Library/VirtualFolders/:id/Refresh", adminMW, refreshSingleLibrary)
	u.POST("/Library/VirtualFolders/:id/Image", adminMW, uploadLibraryImage)
	u.POST("/Library/VirtualFolders/:id/ImageUrl", adminMW, setLibraryImageFromURL)
	u.POST("/Library/VirtualFolders/:id/Image/Generate", adminMW, generateLibraryCover)
	u.DELETE("/Library/VirtualFolders/:id/Image", adminMW, deleteLibraryImage)
	u.GET("/Library/CoverArt/Styles", authMW, listCoverArtStyles)
	u.POST("/Library/CoverArt/GenerateAll", adminMW, generateAllLibraryCovers)
	u.GET("/Library/Scan/Progress", getScanProgress)

	u.POST("/Library/Probe/Start", adminMW, startProbe)
	u.POST("/Library/Probe/Stop", adminMW, stopProbe)
	u.GET("/Library/Probe/Progress", getProbeProgress)

	u.POST("/Items/:itemId/Scrape", adminMW, scrapeItem)
	u.POST("/Items/:itemId/SearchTmdb", adminMW, searchTmdbForItem)
	u.POST("/Items/:itemId/ScrapeByTmdbId", adminMW, scrapeItemByTmdbId)
	u.GET("/Items/:itemId/IdentifyCandidates", adminMW, getIdentifyCandidates)
	u.POST("/Items/:itemId/IdentifyCandidates/:candidateId/Apply", adminMW, applyIdentifyCandidate)
	u.GET("/Library/Scrape/Unmatched", adminMW, listUnmatchedItems)
	u.POST("/Library/Scrape/Unmatched/Apply", adminMW, batchApplyIdentifyCandidates)
	u.POST("/Library/Scrape/All", adminMW, scrapeAll)
	u.POST("/Library/Scrape/Stop", adminMW, stopScrape)
	u.GET("/Library/Scrape/Progress", getScrapeProgress)
	u.GET("/Library/Scrape/Missing", getMissingScrapeCount)
	u.GET("/Library/Tasks/Summary", func(c *gin.Context) { getTaskSummary(c, state) })

	u.POST("/Library/MergeVersions", adminMW, func(c *gin.Context) { mergeVersions(c, state) })

	u.POST("/Library/Browse", adminMW, browseDir)
	u.GET("/Library/BrowseDirectories", adminMW, browseDirGet)

	u.POST("/Library/Refresh/Metadata", adminMW, refreshLibraryMetadata)

	// M7.Backfill: 存量数据回填(画质标签 / Episode 标题 / Episode 缩略图)
	u.POST("/Library/Backfill/Start", adminMW, func(c *gin.Context) { startBackfill(c, state) })
	u.POST("/Library/Backfill/Stop", adminMW, func(c *gin.Context) { stopBackfill(c, state) })
	u.GET("/Library/Backfill/Progress", adminMW, func(c *gin.Context) { getBackfillProgress(c, state) })
	u.GET("/Library/Backfill/Config", adminMW, func(c *gin.Context) { getBackfillConfig(c, state) })
	u.POST("/Library/Backfill/Config", adminMW, func(c *gin.Context) { updateBackfillConfig(c, state) })
	u.POST("/Library/Backfill/Reset/Quality", adminMW, func(c *gin.Context) { resetBackfillQuality(c, state) })
	u.POST("/Library/Backfill/Reset/EpisodeImage", adminMW, func(c *gin.Context) { resetBackfillEpisodeImage(c, state) })

	u.GET("/Users/:userId/Items/LatestBatch", authMW, getLatestBatch)
	u.GET("/Users/Items/LatestBatch", authMW, getLatestBatch)

	u.GET("/Genres", getGenres)
	u.GET("/Tags", authMW, getTags)

	// Library sort order
	u.POST("/Library/VirtualFolders/SortOrder", adminMW, func(c *gin.Context) { updateLibrarySortOrder(c, state) })
	// 统一展示顺序(实际库 + 虚拟库交错)
	u.POST("/Library/DisplayOrder", adminMW, func(c *gin.Context) { updateDisplayOrder(c, state) })

	// Platform libraries
	u.GET("/Library/Platforms", adminMW, func(c *gin.Context) { getPlatforms(c, state) })
	u.POST("/Library/Platforms", adminMW, func(c *gin.Context) { addPlatform(c, state) })
	// 多维度虚拟库:发现 distinct 值 / 批量添加 / 封面生成
	u.GET("/Library/Platforms/Discover", adminMW, func(c *gin.Context) { discoverPlatformDimension(c, state) })
	u.POST("/Library/Platforms/Batch", adminMW, func(c *gin.Context) { addPlatformsBatch(c, state) })
	u.POST("/Library/Platforms/CoverArt/GenerateAll", adminMW, func(c *gin.Context) { generateAllPlatformCovers(c, state) })
	u.POST("/Library/Platforms/:id/Enable", adminMW, func(c *gin.Context) { setPlatformEnabled(c, state, true) })
	u.POST("/Library/Platforms/:id/Disable", adminMW, func(c *gin.Context) { setPlatformEnabled(c, state, false) })
	u.POST("/Library/Platforms/:id/Image/Generate", adminMW, func(c *gin.Context) { generatePlatformCover(c, state) })
	u.DELETE("/Library/Platforms/:id/Image", adminMW, func(c *gin.Context) { deletePlatformCover(c, state) })
	u.POST("/Library/Platforms/:id/Rename", adminMW, func(c *gin.Context) { renamePlatform(c, state) })
	// 多值聚合:把若干匹配值合并进/移出某虚拟库
	u.POST("/Library/Platforms/:id/Values", adminMW, func(c *gin.Context) { addPlatformValues(c, state) })
	u.DELETE("/Library/Platforms/:id/Values", adminMW, func(c *gin.Context) { removePlatformValue(c, state) })
	u.DELETE("/Library/Platforms/:id", adminMW, func(c *gin.Context) { deletePlatform(c, state) })
	u.POST("/Library/Platforms/Scan", adminMW, func(c *gin.Context) { scanPlatformStudios(c, state) })
	u.POST("/Library/Platforms/ScanFilename", adminMW, func(c *gin.Context) { scanPlatformByFilename(c, state) })
	u.POST("/Library/Platforms/Rescrape", adminMW, func(c *gin.Context) { rescrapeMissingStudio(c, state) })
	u.GET("/Library/Platforms/Rescrape/Progress", adminMW, func(c *gin.Context) { getRescrapeProgress(c, state) })
	u.POST("/Library/Platforms/SortOrder", adminMW, func(c *gin.Context) { updatePlatformSortOrder(c, state) })

	// Source library views
	u.GET("/Library/SourceViews", adminMW, func(c *gin.Context) { listSourceViews(c, state) })
	u.POST("/Library/SourceViews", adminMW, func(c *gin.Context) { createSourceView(c, state) })
	u.GET("/Library/SourceViews/Discover", adminMW, func(c *gin.Context) { discoverSourceViewValues(c, state) })
	u.PUT("/Library/SourceViews/:id", adminMW, func(c *gin.Context) { updateSourceView(c, state) })
	u.DELETE("/Library/SourceViews/:id", adminMW, func(c *gin.Context) { deleteSourceView(c, state) })
	u.POST("/Library/SourceViews/:id/Rename", adminMW, func(c *gin.Context) { renameSourceView(c, state) })
	u.POST("/Library/SourceViews/:id/Image/Generate", adminMW, func(c *gin.Context) { generateSourceViewCover(c, state) })
	u.DELETE("/Library/SourceViews/:id/Image", adminMW, func(c *gin.Context) { deleteSourceViewCover(c, state) })
	u.POST("/Library/SourceViews/DisplayOrder", adminMW, func(c *gin.Context) { updateSourceViewDisplayOrder(c, state) })
}
