package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/config"
	"fyms/internal/services"
)

type AppState struct {
	DB             *pgxpool.Pool
	Cache          *services.CacheService
	Config         *config.AppConfig
	SessionManager *services.SessionManager
	ProgressBuffer *services.ProgressBuffer
	ScanProgress   *services.ScanProgressTracker
	ProbeTask      *services.ProbeTask
	FileWatcher    *services.FileWatcher
	LogBuffer      *services.LogBuffer
	ScrapeTask     *services.ScrapeTask
	HTTPClient     *http.Client
}

func GetState(c *gin.Context) *AppState {
	v, _ := c.Get("state")
	return v.(*AppState)
}
