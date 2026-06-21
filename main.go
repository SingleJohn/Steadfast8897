package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"fyms/internal/config"
	"fyms/internal/database"
	"fyms/internal/dto"
	"fyms/internal/gateway"
	"fyms/internal/handlers"
	"fyms/internal/middleware"
	"fyms/internal/repository"
	"fyms/internal/services"
	"fyms/internal/services/imagecache"
	"fyms/internal/services/sysmetrics"
	"fyms/internal/services/taskcenter"
	"fyms/internal/services/taskcenter/adapters"
)

//go:embed all:web/dist
var embeddedWeb embed.FS

func init() {
	mime.AddExtensionType(".wasm", "application/wasm")
}

func main() {
	// 解析命令行参数
	databaseURL := flag.String("database-url", "", "PostgreSQL connection string (e.g., postgres://user:pass@host:5432/dbname)")
	flag.Parse()

	if len(os.Args) > 1 && os.Args[1] == services.UpdateRunnerCommandArg() {
		if err := services.RunUpdaterRunnerFromEnv(); err != nil {
			slog.Error("Updater runner failed", "error", err)
			os.Exit(1)
		}
		return
	}

	cfg := config.NewAppConfigWithArgs(databaseURL)

	logBuffer := services.NewLogBuffer(2000)

	consoleLevel := services.LogLevelFromEnv("FYMS_CONSOLE_LOG_LEVEL", slog.LevelInfo)
	fileLevel := services.LogLevelFromEnv("FYMS_FILE_LOG_LEVEL", slog.LevelInfo)
	logHandler, logErr := services.NewRoutedLogHandler("data/logs", consoleLevel, fileLevel)
	if logErr != nil {
		logHandler = services.NewFallbackLogHandler(consoleLevel)
	}
	defer logHandler.Close()
	bufHandler := services.NewBufferHandler(logHandler, logBuffer)
	slog.SetDefault(slog.New(bufHandler))

	go cleanupOldLogs("data/logs", 7)
	if logErr != nil {
		slog.Warn("file logging disabled, using stdout only", "log_target", "system", "error", logErr)
	}

	slog.Info("FYMS starting", "port", cfg.Port)

	pool, err := database.CreatePool(cfg)
	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if _, err := os.Stat("migrations"); err == nil {
		if err := database.RunMigrations(pool, "migrations"); err != nil {
			slog.Error("Failed to run migrations", "error", err)
			os.Exit(1)
		}
	}

	// 禁用 PG 并行查询，避免 parallel worker 打满 CPU
	for _, stmt := range []string{
		"ALTER SYSTEM SET max_parallel_workers_per_gather = 0",
		"ALTER SYSTEM SET max_parallel_workers = 0",
		"ALTER SYSTEM SET max_parallel_maintenance_workers = 0",
		"SELECT pg_reload_conf()",
	} {
		pool.Exec(context.Background(), stmt)
	}

	cache := services.NewCacheService(cfg.RedisHost, cfg.RedisPort, cfg.RedisPassword)
	repo := repository.New(pool)
	services.SetScrapeCache(cache)
	// Phase 2: 所有 TMDB 调用点共享同一个 rate.Limiter(默认 3 rps, burst 5),
	// 通过 TmdbClient.tmdbGet 自动 Wait,防止 worker/autoscrape/backfill/手动 Identify 叠加超频。
	// 启动后立刻根据 system_config.tmdb_rate_per_sec / tmdb_rate_burst 覆盖默认值;
	// postConfiguration handler 保存配置后也会调 ApplyTmdbLimiterConfig 实时生效。
	tmdbLimiter := services.NewTmdbLimiter()
	services.SetTmdbLimiter(tmdbLimiter)
	services.ApplyTmdbLimiterConfig(context.Background(), pool)
	sessionManager := services.NewSessionManager()
	progressBuffer := services.NewProgressBuffer(pool)
	scanProgress := services.NewScanProgressTracker(pool)
	probeTask := services.NewProbeTask()
	services.RegisterAutoProbeTask(probeTask)
	ingestWorker := services.NewIngestWorker(pool, cache)
	scrapeQueue := services.NewScrapeQueue(pool)
	scrapeWorker := services.NewScrapeWorker(pool, scrapeQueue, tmdbLimiter)
	refreshQueue := services.NewRefreshQueue(pool)
	refreshWorker := services.NewRefreshWorker(pool, refreshQueue, scrapeQueue)
	refreshScheduler := services.NewRefreshScheduler(pool, refreshQueue)
	fileWatcher := services.NewFileWatcher(ingestWorker, refreshScheduler)

	proxyURL, hasProxyURL, _ := repo.SystemConfig.GetString(context.Background(), "tmdb_proxy")

	var httpClient *http.Client
	if hasProxyURL && proxyURL != "" {
		proxyURLParsed, err := url.Parse(proxyURL)
		if err == nil {
			httpClient = &http.Client{
				Timeout: 15 * time.Second,
				Transport: &http.Transport{
					Proxy: http.ProxyURL(proxyURLParsed),
				},
			}
			slog.Info("HTTP client configured with proxy", "proxy", proxyURL)
		} else {
			httpClient = &http.Client{Timeout: 15 * time.Second}
		}
	} else {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	// Updater needs a direct (non-proxied) client to reach Docker Hub and GitHub.
	updateHTTPClient := &http.Client{Timeout: 30 * time.Second}
	updater := services.NewUpdater(cfg, pool, updateHTTPClient, logBuffer)
	notifyHTTPClient := &http.Client{Timeout: 10 * time.Second}
	notifier := services.NewNotifyDispatcher(pool, cfg, notifyHTTPClient)
	services.SetNotifier(notifier)

	gapScanTask := services.NewGapScanTask()
	backfillTask := services.NewBackfillTask()

	// 系统资源采集：CPU/RAM 走 gopsutil，网络出口由中间件累加 + 会话码率估算。
	bitrateEstimator := services.NewRedirectBitrateEstimator(pool, sessionManager)
	sysCollector := sysmetrics.NewCollector(2*time.Second, bitrateEstimator.Estimate)

	imageCache := imagecache.NewImageCache(cfg, httpClient)
	// 启动时从 system_config 加载"本地原图直读"开关(默认 false=直读)。
	// postConfiguration 保存后会调 imageCache.SetCopyLocal 实时生效。
	{
		copyLocal := repo.SystemConfig.GetBoolOrDefault(context.Background(), "image_cache_copy_local", false)
		imageCache.SetCopyLocal(copyLocal)
		dto.SetLocalImageDirectRead(!copyLocal)
	}

	// 启动时加载 strm item.Path 模式(默认 'strm'=返回 .strm 文件路径,对齐 Emby)。
	// postConfiguration 保存后会调 dto.SetStrmItemPathMode 实时生效。
	{
		strmPathMode, ok, _ := repo.SystemConfig.GetString(context.Background(), "strm_item_path_mode")
		if ok {
			dto.SetStrmItemPathMode(strmPathMode)
		}
	}

	state := &handlers.AppState{
		DB:             pool,
		Repo:           repo,
		Cache:          cache,
		Config:         cfg,
		SessionManager: sessionManager,
		ProgressBuffer: progressBuffer,
		ScanProgress:   scanProgress,
		ProbeTask:      probeTask,
		FileWatcher:    fileWatcher,
		Ingest:         ingestWorker,
		ScrapeQueue:    scrapeQueue,
		ScrapeWorker:   scrapeWorker,
		RefreshQueue:   refreshQueue,
		RefreshWorker:  refreshWorker,
		LogBuffer:      logBuffer,
		HTTPClient:     httpClient,
		Notifier:       notifier,
		Updater:        updater,
		GapScanTask:    gapScanTask,
		BackfillTask:   backfillTask,
		SysMetrics:     sysCollector,
		ImageCache:     imageCache,
	}
	sysCollector.Start(context.Background())
	imageCache.StartJanitor(context.Background())

	// 任务中心：注册 4 个适配器。刮削由 scrape_queue + ScrapeWorker 持续驱动,
	// 不再作为 task_center 的一等公民(方案 C)——全库刮削在 /Library/Scrape/All
	// 里退化为一次瞬时入队动作。
	taskRegistry := taskcenter.NewRegistry()
	taskRegistry.Register(adapters.NewScanAdapter(scanProgress))
	taskRegistry.Register(adapters.NewProbeAdapter(probeTask, pool))
	taskRegistry.Register(adapters.NewBackfillAdapter(backfillTask, pool))
	taskRegistry.Register(adapters.NewUpdateAdapter(updater, pool))
	cleanupTask := adapters.NewCleanupAdapter(pool)
	taskRegistry.Register(cleanupTask)
	state.TaskCenter = taskRegistry
	state.CleanupTask = cleanupTask
	// SSE 广播：每秒扫描快照，仅在关键字段变化时推送。
	taskRegistry.StartBroadcaster(context.Background(), time.Second)

	// 任务链：scan → probe → backfill(image)。默认关闭，DB 里可切换。
	chainEngine := taskcenter.NewChainEngine(taskRegistry, nil)
	chainCtx := context.Background()
	if enabled := repo.SystemConfig.GetBoolOrDefault(chainCtx, "task_chain_enabled", false); enabled {
		chainEngine.SetEnabled(true)
	}
	if raw := repo.SystemConfig.GetStringOrDefault(chainCtx, "task_chain_rules", ""); raw != "" {
		if err := chainEngine.LoadRulesJSON(raw); err != nil {
			slog.Warn("task chain: failed to load persisted rules, using defaults", "error", err)
		}
	}
	chainEngine.Start(chainCtx)
	state.TaskChain = chainEngine

	ctx := context.Background()
	// 启动时收尾上次崩溃/重启前遗留的 running/queued/stopping 行。
	if err := taskcenter.ReconcileOnStartup(ctx, pool); err != nil {
		slog.Warn("task_runs reconcile on startup failed", "error", err)
	}
	if err := adapters.ReconcileUpdateRunsOnStartup(ctx, pool, updater); err != nil {
		slog.Warn("update task_runs reconcile on startup failed", "error", err)
	}
	// 接管上次进程中途退出时未完成的库清理:有 deleted_at 标记但 items 未清空的库。
	go cleanupTask.ResumeAfterRestart(context.Background())
	go ingestWorker.Run(ctx)
	go scrapeWorker.Run(ctx)
	go refreshWorker.Run(ctx)
	go notifier.Run(ctx)
	go notifier.RunLibraryNewSweeper(ctx)
	services.StartMetricsLogger(ctx, ingestWorker, scrapeQueue)
	fileWatcher.Start(ctx, pool, cache)

	// M7.Backfill: 启动开关 + 24h 防重。保持异步,不阻塞启动。
	// 走任务中心以便 task_runs 记录 trigger=startup。
	go func() {
		if !services.ShouldAutoRunOnStartup(ctx, pool) {
			return
		}
		if t := taskRegistry.Get(taskcenter.KindBackfill); t != nil {
			if _, err := t.Start(ctx, nil, taskcenter.TriggerStartup); err != nil {
				slog.Info("[Backfill] auto start skipped", "reason", err)
				return
			}
			slog.Info("[Backfill] auto start triggered on startup")
		}
	}()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			handlers.FlushStalePlaybacks(pool, sessionManager)
		}
	}()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.RemoveExtraSlash = true
	r.Use(gin.Recovery())
	r.Use(corsMiddleware())
	r.Use(requestLogger())
	// 网络出口字节计数：必须在业务 handler 前挂载，c.Next() 后读取 Writer.Size()。
	r.Use(sysCollector.ByteCountMiddleware())

	r.Use(func(c *gin.Context) {
		c.Set("state", state)
		c.Next()
	})

	// Gateway (302 redirect engine)
	gwStore := gateway.NewStore(pool)
	gwRuntime := gateway.NewRuntime(gwStore, slog.With("log_target", "gateway"), cfg.Port)

	gwCfg, err := gwStore.LoadConfig(ctx)
	if err != nil {
		slog.Warn("Failed to load gateway config, using defaults", "error", err)
		gwCfg = gateway.DefaultGatewayConfig()
	}
	if err := gwRuntime.Rebuild(ctx, gwCfg); err != nil {
		slog.Error("Failed to start gateway runtime", "error", err)
	}
	state.GatewayRuntime = gwRuntime

	authMW := middleware.RequireAuth(pool, cache, sessionManager)
	adminMW := middleware.RequireAdmin(pool, cache, sessionManager)
	optAuthMW := middleware.OptionalAuth(pool, cache, sessionManager)

	registerRoutes := func(group *gin.RouterGroup) {
		handlers.RegisterSystemRoutes(group, state, adminMW)
		handlers.RegisterUserRoutes(group, state, authMW, adminMW, optAuthMW)
		// 浏览类元数据路由(媒体详情 / 列表 / 剧集 / 搜索等):非管理员隐藏物理存储路径。
		// 播放 / 视频 / 图片路由不挂此中间件,保留 PlaybackInfo 的 MediaSource.Path 直链能力。
		browse := group.Group("", middleware.HideMediaPaths())
		handlers.RegisterLibraryRoutes(browse, state, authMW, adminMW, optAuthMW)
		handlers.RegisterPlaybackRoutes(group, state, authMW)
		handlers.RegisterVideoRoutes(group, state, authMW)
		handlers.RegisterImageRoutes(group, state)
		handlers.RegisterCompatRoutes(browse, state, authMW, adminMW, optAuthMW)
		handlers.RegisterEmbyCompatRoutes(group, state, adminMW)
		handlers.RegisterStatsRoutes(group, state, authMW, adminMW)
		handlers.RegisterWebhookRoutes(group, state)
		handlers.RegisterNotifyAdminRoutes(group, state, adminMW)
		handlers.RegisterTaskCenterRoutes(group, state, adminMW)
		handlers.RegisterSystemMetricsRoutes(group, state, adminMW)
		handlers.RegisterAdminQueueRoutes(group, state, adminMW)
		handlers.RegisterScrapeConfigRoutes(group, adminMW)
		handlers.RegisterSourceRoutes(group, state, adminMW)
		gateway.RegisterAPIRoutes(group, gwStore, gwRuntime, adminMW)
	}

	root := r.Group("")
	registerRoutes(root)

	emby := r.Group("/emby")
	registerRoutes(emby)

	useEmbedded := false
	var webFS http.FileSystem
	if _, err := fs.Stat(embeddedWeb, "web/dist/index.html"); err == nil {
		subFS, _ := fs.Sub(embeddedWeb, "web/dist")
		webFS = http.FS(subFS)
		useEmbedded = true
		slog.Info("Serving frontend from embedded assets")
	} else if _, err := os.Stat("web/dist/index.html"); err == nil {
		webFS = http.Dir("web/dist")
		slog.Info("Serving frontend from filesystem (web/dist)")
	}

	if webFS != nil {
		r.StaticFS("/web/dist", webFS)
		r.NoRoute(func(c *gin.Context) {
			p := c.Request.URL.Path
			isAPI := strings.HasPrefix(p, "/api") ||
				strings.HasPrefix(p, "/emby") ||
				strings.HasPrefix(p, "/Gateway") ||
				strings.HasPrefix(p, "/Users") ||
				strings.HasPrefix(p, "/System") ||
				strings.HasPrefix(p, "/Items") ||
				strings.HasPrefix(p, "/Videos") ||
				strings.HasPrefix(p, "/Sessions") ||
				strings.HasPrefix(p, "/Library") ||
				strings.HasPrefix(p, "/Auth") ||
				strings.HasPrefix(p, "/Admin") ||
				strings.HasPrefix(p, "/Stats") ||
				strings.HasPrefix(p, "/Plugins") ||
				strings.HasPrefix(p, "/Shows") ||
				strings.HasPrefix(p, "/Search") ||
				strings.HasPrefix(p, "/Tasks") ||
				strings.HasPrefix(p, "/Notifications")
			if isAPI {
				c.JSON(404, gin.H{"message": "Not found"})
				return
			}
			if useEmbedded {
				if f, err := webFS.Open(p); err == nil {
					f.Close()
					c.FileFromFS(p, webFS)
					return
				}
				c.FileFromFS("index.html", webFS)
			} else {
				localPath := filepath.Join("web/dist", p)
				if info, err := os.Stat(localPath); err == nil && !info.IsDir() {
					c.File(localPath)
					return
				}
				c.File(filepath.Join("web/dist", "index.html"))
			}
		})
	}

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.Port)
	slog.Info("FYMS started", "addr", "http://"+addr, "serverID", cfg.ServerID)

	if err := r.Run(addr); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Emby-Token, X-Emby-Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery
		method := c.Request.Method

		ip := c.GetHeader("X-Forwarded-For")
		if ip != "" {
			ip = strings.SplitN(ip, ",", 2)[0]
			ip = strings.TrimSpace(ip)
		}
		if ip == "" {
			ip = c.GetHeader("X-Real-IP")
		}
		if ip == "" {
			ip = c.ClientIP()
		}

		isPolling := strings.HasSuffix(path, "/Scan/Progress") ||
			strings.HasSuffix(path, "/Probe/Progress") ||
			strings.HasSuffix(path, "/Sessions") ||
			strings.HasSuffix(path, "/Ping")

		start := time.Now()
		c.Next()
		elapsed := time.Since(start).Milliseconds()
		status := c.Writer.Status()

		if !isPolling {
			q := ""
			if query != "" {
				q = "?" + query
			}
			msg := fmt.Sprintf("%s %s%s → %d (%dms) ip=%s", method, path, q, status, elapsed, ip)
			logger := slog.With("log_target", "http")
			if status >= 500 {
				logger.Error(msg)
			} else if status >= 400 {
				logger.Warn(msg)
			} else {
				logger.Info(msg)
			}
		}
	}
}

func cleanupOldLogs(dir string, retentionDays int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(dir, entry.Name()))
			slog.Info("Cleaned up old log file", "file", entry.Name())
		}
	}
}
