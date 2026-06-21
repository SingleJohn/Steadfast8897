package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/appstate"
	adminhandlers "fyms/internal/handlers/admin"
	compathandlers "fyms/internal/handlers/compat"
	libraryhandlers "fyms/internal/handlers/library"
	mediahandlers "fyms/internal/handlers/media"
	playbackhandlers "fyms/internal/handlers/playback"
	statshandlers "fyms/internal/handlers/stats"
	systemhandlers "fyms/internal/handlers/system"
	userhandlers "fyms/internal/handlers/users"
	webhookhandlers "fyms/internal/handlers/webhook"
	"fyms/internal/services"
)

type AppState = appstate.AppState

func GetState(c *gin.Context) *AppState {
	return appstate.GetState(c)
}

func RegisterSystemRoutes(group *gin.RouterGroup, state *AppState, adminMW gin.HandlerFunc) {
	systemhandlers.RegisterSystemRoutes(group, state, adminMW)
}

func RegisterUserRoutes(group *gin.RouterGroup, state *AppState, authMW, adminMW, optAuthMW gin.HandlerFunc) {
	userhandlers.RegisterUserRoutes(group, state, authMW, adminMW, optAuthMW)
}

func RegisterLibraryRoutes(group *gin.RouterGroup, state *AppState, authMW, adminMW, optAuthMW gin.HandlerFunc) {
	libraryhandlers.RegisterLibraryRoutes(group, state, authMW, adminMW, optAuthMW)
}

func RegisterPlaybackRoutes(group *gin.RouterGroup, state *AppState, authMW gin.HandlerFunc) {
	playbackhandlers.RegisterPlaybackRoutes(group, state, authMW)
}

func RegisterVideoRoutes(group *gin.RouterGroup, state *AppState, authMW gin.HandlerFunc) {
	mediahandlers.RegisterVideoRoutes(group, state, authMW)
}

func RegisterImageRoutes(group *gin.RouterGroup, state *AppState) {
	mediahandlers.RegisterImageRoutes(group, state)
}

func RegisterCompatRoutes(group *gin.RouterGroup, state *AppState, authMW, adminMW, optAuthMW gin.HandlerFunc) {
	compathandlers.RegisterCompatRoutes(group, state, authMW, adminMW, optAuthMW)
}

func RegisterEmbyCompatRoutes(group *gin.RouterGroup, state *AppState, adminMW gin.HandlerFunc) {
	compathandlers.RegisterEmbyCompatRoutes(group, state, adminMW)
}

func RegisterStatsRoutes(group *gin.RouterGroup, state *AppState, authMW, adminMW gin.HandlerFunc) {
	statshandlers.RegisterStatsRoutes(group, state, authMW, adminMW)
}

func RegisterWebhookRoutes(group *gin.RouterGroup, state *AppState) {
	webhookhandlers.RegisterWebhookRoutes(group, state)
}

func RegisterNotifyAdminRoutes(group *gin.RouterGroup, state *AppState, adminMW gin.HandlerFunc) {
	adminhandlers.RegisterNotifyAdminRoutes(group, state, adminMW)
}

func RegisterTaskCenterRoutes(group *gin.RouterGroup, state *AppState, adminMW gin.HandlerFunc) {
	adminhandlers.RegisterTaskCenterRoutes(group, state, adminMW)
}

func RegisterSystemMetricsRoutes(group *gin.RouterGroup, state *AppState, adminMW gin.HandlerFunc) {
	adminhandlers.RegisterSystemMetricsRoutes(group, state, adminMW)
}

func RegisterAdminQueueRoutes(group *gin.RouterGroup, state *AppState, adminMW gin.HandlerFunc) {
	adminhandlers.RegisterAdminQueueRoutes(group, state, adminMW)
}

func RegisterScrapeConfigRoutes(group *gin.RouterGroup, adminMW gin.HandlerFunc) {
	adminhandlers.RegisterScrapeConfigRoutes(group, adminMW)
}

func RegisterSourceRoutes(group *gin.RouterGroup, state *AppState, adminMW gin.HandlerFunc) {
	adminhandlers.RegisterSourceRoutes(group, state, adminMW)
}

func FlushStalePlaybacks(pool *pgxpool.Pool, sm *services.SessionManager) {
	playbackhandlers.FlushStalePlaybacks(pool, sm)
}
