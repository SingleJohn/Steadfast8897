package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/config"
	"fyms/internal/services"
	"fyms/internal/services/sysmetrics"
	"fyms/internal/services/taskcenter"
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
	Ingest         *services.IngestWorker
	ScrapeQueue    *services.ScrapeQueue
	ScrapeWorker   *services.ScrapeWorker
	LogBuffer      *services.LogBuffer
	ScrapeTask     *services.ScrapeTask
	HTTPClient     *http.Client
	Updater        *services.Updater
	GapScanTask    *services.GapScanTask
	BackfillTask   *services.BackfillTask
	TaskCenter     *taskcenter.Registry
	TaskChain      *taskcenter.ChainEngine
	SysMetrics     *sysmetrics.Collector
}

func GetState(c *gin.Context) *AppState {
	v, _ := c.Get("state")
	return v.(*AppState)
}
