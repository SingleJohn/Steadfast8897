package appstate

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/config"
	"fyms/internal/gateway"
	"fyms/internal/repository"
	"fyms/internal/services"
	"fyms/internal/services/imagecache"
	"fyms/internal/services/sysmetrics"
	"fyms/internal/services/taskcenter"
	"fyms/internal/services/taskcenter/adapters"
	sourcebridge "fyms/internal/source"
)

type AppState struct {
	DB             *pgxpool.Pool
	Repo           *repository.Repository
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
	RefreshQueue   *services.RefreshQueue
	RefreshWorker  *services.RefreshWorker
	LogBuffer      *services.LogBuffer
	HTTPClient     *http.Client
	Notifier       *services.NotifyDispatcher
	Updater        *services.Updater
	GapScanTask    *services.GapScanTask
	BackfillTask   *services.BackfillTask
	TaskCenter     *taskcenter.Registry
	TaskChain      *taskcenter.ChainEngine
	CleanupTask    *adapters.CleanupAdapter
	SysMetrics     *sysmetrics.Collector
	ImageCache     *imagecache.ImageCache
	GatewayRuntime *gateway.Runtime
	JSRuntime      *sourcebridge.JSRuntimeManager
}

func GetState(c *gin.Context) *AppState {
	v, _ := c.Get("state")
	return v.(*AppState)
}
