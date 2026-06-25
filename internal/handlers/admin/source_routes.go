package admin

import "github.com/gin-gonic/gin"

func RegisterSourceRoutes(group *gin.RouterGroup, state *AppState, adminMW gin.HandlerFunc) {
	group.POST("/SourceSearch", adminMW, func(c *gin.Context) { federatedSourceSearch(c, state) })
	group.POST("/SourceRuntime/TestJS", adminMW, func(c *gin.Context) { testSourceRuntimeJS(c, state) })
	group.POST("/SourceRuntime/TestCSP", adminMW, func(c *gin.Context) { testSourceRuntimeCSP(c, state) })
	group.GET("/SourceRuntime/Artifacts", adminMW, func(c *gin.Context) { listSourceRuntimeArtifacts(c, state) })
	group.POST("/SourceRuntime/Artifacts/:id/Trust", adminMW, func(c *gin.Context) { trustSourceRuntimeArtifact(c, state) })
	group.GET("/SourceRuntime/Invocations", adminMW, func(c *gin.Context) { listSourceRuntimeInvocations(c, state) })
	group.GET("/SourceRuntime/Invocations/:id", adminMW, func(c *gin.Context) { getSourceRuntimeInvocation(c, state) })
	group.GET("/SourceParsers", adminMW, func(c *gin.Context) { listSourceParsers(c, state) })
	group.POST("/SourceParsers/:id/Enable", adminMW, func(c *gin.Context) { setSourceParserEnabled(c, state, true) })
	group.POST("/SourceParsers/:id/Disable", adminMW, func(c *gin.Context) { setSourceParserEnabled(c, state, false) })

	group.POST("/SourceConfigs/ImportTVBox", adminMW, func(c *gin.Context) { importTVBoxConfig(c, state) })
	group.POST("/SourceConfigs/ImportCMSList", adminMW, func(c *gin.Context) { importCMSListConfig(c, state) })
	group.GET("/SourceConfigs", adminMW, func(c *gin.Context) { listSourceConfigs(c, state) })
	group.GET("/SourceConfigs/:id", adminMW, func(c *gin.Context) { getSourceConfig(c, state) })
	group.GET("/SourceConfigs/:id/Impact", adminMW, func(c *gin.Context) { getSourceConfigImpact(c, state) })
	group.POST("/SourceConfigs/:id/Enable", adminMW, func(c *gin.Context) { setSourceConfigEnabled(c, state, true) })
	group.POST("/SourceConfigs/:id/Disable", adminMW, func(c *gin.Context) { setSourceConfigEnabled(c, state, false) })
	group.DELETE("/SourceConfigs/:id", adminMW, func(c *gin.Context) { deleteSourceConfig(c, state) })

	group.GET("/SourceProviders", adminMW, func(c *gin.Context) { listSourceProviders(c, state) })
	group.POST("/SourceProviders/BatchEnable", adminMW, func(c *gin.Context) { batchEnableSourceProviders(c, state) })
	group.POST("/SourceProviders/BatchDisable", adminMW, func(c *gin.Context) { batchDisableSourceProviders(c, state) })
	group.POST("/SourceProviders/BatchDelete", adminMW, func(c *gin.Context) { batchDeleteSourceProviders(c, state) })
	group.POST("/SourceProviders/BatchHealthCheck", adminMW, func(c *gin.Context) { batchHealthCheckSourceProviders(c, state) })
	group.POST("/SourceProviders/:id/Enable", adminMW, func(c *gin.Context) { setSourceProviderEnabled(c, state, true) })
	group.POST("/SourceProviders/:id/Disable", adminMW, func(c *gin.Context) { setSourceProviderEnabled(c, state, false) })
	group.POST("/SourceProviders/:id/HealthCheck", adminMW, func(c *gin.Context) { healthCheckSourceProvider(c, state) })
	group.POST("/SourceProviders/:id/Diagnose", adminMW, func(c *gin.Context) { diagnoseSourceProvider(c, state) })
	group.GET("/SourceProviders/:id/HomeProfile", adminMW, func(c *gin.Context) { getSourceProviderHomeProfile(c, state) })
	group.POST("/SourceProviders/:id/Search", adminMW, func(c *gin.Context) { searchSourceProvider(c, state) })
	group.POST("/SourceProviders/:id/Detail", adminMW, func(c *gin.Context) { detailSourceProvider(c, state) })
	group.GET("/SourceProviders/:id/Categories", adminMW, func(c *gin.Context) { listSourceProviderCategories(c, state) })
}
