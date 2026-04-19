package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io"
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
	"fyms/internal/gateway"
	"fyms/internal/handlers"
	"fyms/internal/middleware"
	"fyms/internal/services"
	"fyms/internal/services/sysmetrics"
	"fyms/internal/services/taskcenter"
	"fyms/internal/services/taskcenter/adapters"
)


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

	os.MkdirAll("data/logs", 0755)

	// 日志同时写到 stdout 和 data/logs/fyms-YYYY-MM-DD.log
	logFileName := fmt.Sprintf("data/logs/fyms-%s.log", time.Now().Format("2006-01-02"))
	logFile, logFileErr := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	var logWriter io.Writer = os.Stdout
	if logFileErr == nil {
		logWriter = io.MultiWriter(os.Stdout, logFile)
	}
	textHandler := slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: slog.LevelInfo})
	bufHandler := services.NewBufferHandler(textHandler, logBuffer)
	slog.SetDefault(slog.New(bufHandler))

	go cleanupOldLogs("data/logs", 7)

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
	services.SetScrapeCache(cache)
	sessionManager := services.NewSessionManager()
	progressBuffer := services.NewProgressBuffer(pool)
	scanProgress := services.NewScanProgressTracker(pool)
	probeTask := services.NewProbeTask()
	fileWatcher := services.NewFileWatcher()
	scrapeTask := services.NewScrapeTask()

	var proxyURL *string
	pool.QueryRow(context.Background(), "SELECT value FROM system_config WHERE key = 'tmdb_proxy'").Scan(&proxyURL)

	var httpClient *http.Client
	if proxyURL != nil && *proxyURL != "" {
		proxyURLParsed, err := url.Parse(*proxyURL)
		if err == nil {
			httpClient = &http.Client{
				Timeout: 15 * time.Second,
				Transport: &http.Transport{
					Proxy: http.ProxyURL(proxyURLParsed),
				},
			}
			slog.Info("HTTP client configured with proxy", "proxy", *proxyURL)
		} else {
			httpClient = &http.Client{Timeout: 15 * time.Second}
		}
	} else {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	// Updater needs a direct (non-proxied) client to reach Docker Hub and GitHub.
	updateHTTPClient := &http.Client{Timeout: 30 * time.Second}
	updater := services.NewUpdater(cfg, pool, updateHTTPClient, logBuffer)

	gapScanTask := services.NewGapScanTask()
	backfillTask := services.NewBackfillTask()

	// 系统资源采集：CPU/RAM 走 gopsutil，网络出口由中间件累加 + 会话码率估算。
	bitrateEstimator := services.NewRedirectBitrateEstimator(pool, sessionManager)
	sysCollector := sysmetrics.NewCollector(2*time.Second, bitrateEstimator.Estimate)

	state := &handlers.AppState{
		DB:             pool,
		Cache:          cache,
		Config:         cfg,
		SessionManager: sessionManager,
		ProgressBuffer: progressBuffer,
		ScanProgress:   scanProgress,
		ProbeTask:      probeTask,
		FileWatcher:    fileWatcher,
		LogBuffer:      logBuffer,
		ScrapeTask:     scrapeTask,
		HTTPClient:     httpClient,
		Updater:        updater,
		GapScanTask:    gapScanTask,
		BackfillTask:   backfillTask,
		SysMetrics:     sysCollector,
	}
	sysCollector.Start(context.Background())

	// 任务中心：注册 5 个适配器。M1 只读聚合，M2 才会写 task_runs。
	taskRegistry := taskcenter.NewRegistry()
	taskRegistry.Register(adapters.NewScanAdapter(scanProgress))
	taskRegistry.Register(adapters.NewScrapeAdapter(scrapeTask, pool))
	taskRegistry.Register(adapters.NewProbeAdapter(probeTask, pool))
	taskRegistry.Register(adapters.NewBackfillAdapter(backfillTask, pool))
	taskRegistry.Register(adapters.NewUpdateAdapter(updater, pool))
	state.TaskCenter = taskRegistry
	// SSE 广播：每秒扫描快照，仅在关键字段变化时推送。
	taskRegistry.StartBroadcaster(context.Background(), time.Second)

	// 任务链：scan → probe → backfill(image)。默认关闭，DB 里可切换。
	chainEngine := taskcenter.NewChainEngine(taskRegistry, nil)
	chainCtx := context.Background()
	if enabled := services.ReadBoolSystemConfig(chainCtx, pool, "task_chain_enabled", false); enabled {
		chainEngine.SetEnabled(true)
	}
	if raw := services.ReadSystemConfigValue(chainCtx, pool, "task_chain_rules"); raw != "" {
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
	gwRuntime := gateway.NewRuntime(gwStore, slog.Default(), cfg.Port)

	gwCfg, err := gwStore.LoadConfig(ctx)
	if err != nil {
		slog.Warn("Failed to load gateway config, using defaults", "error", err)
		gwCfg = gateway.DefaultGatewayConfig()
	}
	if err := gwRuntime.Rebuild(ctx, gwCfg); err != nil {
		slog.Error("Failed to start gateway runtime", "error", err)
	}

	authMW := middleware.RequireAuth(pool, cache, sessionManager)
	adminMW := middleware.RequireAdmin(pool, cache, sessionManager)
	optAuthMW := middleware.OptionalAuth(pool, cache, sessionManager)

	registerRoutes := func(group *gin.RouterGroup) {
		handlers.RegisterSystemRoutes(group, state, adminMW)
		handlers.RegisterUserRoutes(group, state, authMW, adminMW, optAuthMW)
		handlers.RegisterLibraryRoutes(group, state, authMW, adminMW, optAuthMW)
		handlers.RegisterPlaybackRoutes(group, state, authMW)
		handlers.RegisterVideoRoutes(group, state, authMW)
		handlers.RegisterImageRoutes(group, state)
		handlers.RegisterCompatRoutes(group, state, authMW, adminMW, optAuthMW)
		handlers.RegisterEmbyCompatRoutes(group, state, adminMW)
		handlers.RegisterStatsRoutes(group, state, authMW, adminMW)
		handlers.RegisterWebhookRoutes(group, state)
		handlers.RegisterTaskCenterRoutes(group, state, adminMW)
		handlers.RegisterSystemMetricsRoutes(group, state, adminMW)
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
				strings.HasPrefix(p, "/Stats") ||
				strings.HasPrefix(p, "/Plugins") ||
				strings.HasPrefix(p, "/Shows") ||
				strings.HasPrefix(p, "/Search") ||
				strings.HasPrefix(p, "/Tasks")
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
		c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
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
			if status >= 500 {
				slog.Error(msg)
			} else if status >= 400 {
				slog.Warn(msg)
			} else {
				slog.Info(msg)
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
