package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/config"
	"fyms/internal/dto"
	"fyms/internal/middleware"
	"fyms/internal/models"
	"fyms/internal/services"
	"fyms/internal/services/taskcenter"
)

const backupDir = "data/backups"

// backupCategories 同时是恢复时的表执行顺序(靠前先恢复)。新增类别须放在依赖它的类别之前。
var backupCategories = []string{"settings", "users", "libraries", "platforms", "gateway", "media", "activity"}

func tablesForCategory(cat string) []string {
	switch cat {
	case "settings":
		return []string{"system_config"}
	case "users":
		return []string{"users", "user_policies", "api_keys", "access_tokens", "user_library_access"}
	case "libraries":
		return []string{"libraries"}
	case "platforms":
		// 平台/虚拟库定义 + 实际库与虚拟库的混排顺序(library_display_order 按值引用库 id,无外键)
		return []string{"platform_libraries", "library_display_order"}
	case "gateway":
		// 仅 302 回源配置(key='main'),不含请求日志/统计这些运行期数据
		return []string{"gateway_config"}
	case "media":
		return []string{"genres", "items", "item_genres", "cast_members", "media_versions", "media_streams", "user_item_data"}
	case "activity":
		return []string{"playback_activity"}
	default:
		return nil
	}
}

// categoriesForTables 由 data 里出现的表反推类别(用于上传文件缺 categories 字段时兜底)。
func categoriesForTables(data map[string]json.RawMessage) []string {
	var cats []string
	for _, cat := range backupCategories {
		for _, t := range tablesForCategory(cat) {
			if _, ok := data[t]; ok {
				cats = append(cats, cat)
				break
			}
		}
	}
	return cats
}

func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	if udp, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return udp.IP.String()
	}
	return "127.0.0.1"
}

func systemInfo(ctx context.Context, state *AppState, public bool) gin.H {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	updateStatus := state.Updater.GetStatus(context.Background())
	branding := services.LoadBrandingConfig(ctx, state.DB, state.Config)

	port := state.Config.Port
	info := gin.H{
		"ServerName":             branding.ServerName,
		"Version":                state.Config.Version,
		"Id":                     state.Config.ServerID,
		"OperatingSystem":        runtime.GOOS,
		"ProductName":            "FYMS",
		"StartupWizardCompleted": true,
		"LocalAddress":           fmt.Sprintf("http://%s:%d", getLocalIP(), port),
		"CanSelfRestart":         true,
	}
	if branding.IconURL != "" {
		info["BrandIconUrl"] = branding.IconURL
	}

	if !public {
		info["OperatingSystemDisplayName"] = fmt.Sprintf("%s %s", runtime.GOOS, runtime.GOARCH)
		info["HasPendingRestart"] = updateStatus.Status == "pulling" || updateStatus.Status == "recreating" || updateStatus.Status == "restarting"
		info["IsShuttingDown"] = false
		info["CanLaunchWebBrowser"] = false
		info["HasUpdateAvailable"] = updateStatus.HasUpdate
		info["UpdateStatus"] = updateStatus
		info["TranscodingTempPath"] = ""
		info["LogPath"] = ""
		info["InternalMetadataPath"] = ""
		info["CachePath"] = state.Config.CacheDir
		info["ProcessId"] = os.Getpid()
		info["HeapAllocatedBytes"] = m.Alloc
		info["SystemTotalBytes"] = m.Sys
		info["ServerDateTime"] = time.Now().UTC().Format(time.RFC3339)
		if config.BuildCommit != "" {
			info["BuildCommit"] = config.BuildCommit
		}
		if config.BuildTime != "" {
			info["BuildTime"] = config.BuildTime
		}
		if config.BuildRepo != "" {
			info["BuildRepo"] = config.BuildRepo
		}
	}

	return info
}

func RegisterSystemRoutes(group *gin.RouterGroup, state *AppState, adminMW gin.HandlerFunc) {
	group.GET("/System/Info", getSystemInfo)
	group.GET("/System/Info/Public", getSystemInfoPublic)
	// Mac/部分 Emby 官方客户端发全小写路径，Gin 路由大小写敏感会 404，需显式别名
	group.GET("/system/info", getSystemInfo)
	group.GET("/system/info/public", getSystemInfoPublic)
	group.GET("/System/Ping", ping)
	group.POST("/System/Ping", ping)
	group.POST("/System/Restart", adminMW, restart)
	group.POST("/System/Shutdown", adminMW, shutdown)
	group.GET("/System/Configuration", adminMW, getConfiguration)
	group.POST("/System/Configuration", adminMW, postConfiguration)
	group.GET("/web/ConfigurationPage", configPage)
	group.GET("/Branding/Configuration", branding)
	group.GET("/System/Logs", adminMW, getLogs)
	group.POST("/System/Backup", adminMW, createBackup)
	group.GET("/System/Backups", adminMW, listBackups)
	group.POST("/System/Backups/Upload", adminMW, uploadBackup)
	group.GET("/System/Backups/:filename", adminMW, downloadBackup)
	group.GET("/System/Backups/:filename/Summary", adminMW, backupSummaryHandler)
	group.DELETE("/System/Backups/:filename", adminMW, deleteBackup)
	group.POST("/System/Restore", adminMW, restoreBackup)
	group.POST("/System/EmbyMigrate", adminMW, embyMigrate)
	group.GET("/System/Update/Status", adminMW, getUpdateStatus)
	group.GET("/System/Update/Progress", adminMW, getUpdateStatus)
	group.POST("/System/Update/Check", adminMW, checkForUpdate)
	group.POST("/System/Update/Apply", adminMW, applyUpdate)
	group.POST("/System/Update/Rollback", adminMW, rollbackUpdate)
	group.POST("/System/Update/Channel", adminMW, setUpdateChannel)
}

func getSystemInfo(c *gin.Context) {
	info := systemInfo(c.Request.Context(), GetState(c), false)
	applyEmbyOfficialOverrides(c, info)
	c.JSON(http.StatusOK, info)
}

func getSystemInfoPublic(c *gin.Context) {
	info := systemInfo(c.Request.Context(), GetState(c), true)
	applyEmbyOfficialOverrides(c, info)
	c.JSON(http.StatusOK, info)
}

// isEmbyOfficialClient 识别 Emby 官方客户端，用于伪装 Version/ProductName 通过其严格校验。
// 命中条件：UA 含 Emby/、Emby Theater、Emby for、EmbyAndroid；
// 或 Authorization 头里 Client 是 Emby Theater / Emby for ... / Emby Web / Emby Mobile。
// 前端用 Client="Media Server Web"，不会命中。
func isEmbyOfficialClient(c *gin.Context) bool {
	// Emby JS SDK 通用行为：所有 Emby 官方客户端 (Mac/iOS/Android/Web) 都会
	// 设 X-Emby-Client 头。FYMS 前端不设此头，第三方客户端 (Infuse/Yamby
	// /Hills 等) 也不用 Emby JS SDK，所以不会有这头。最可靠的命中条件。
	if c.GetHeader("X-Emby-Client") != "" {
		return true
	}
	ua := c.GetHeader("User-Agent")
	if strings.Contains(ua, "Emby/") ||
		strings.Contains(ua, "Emby Theater") ||
		strings.Contains(ua, "Emby for ") ||
		strings.Contains(ua, "EmbyAndroid") {
		return true
	}
	auth := c.GetHeader("X-Emby-Authorization")
	if auth == "" {
		auth = c.GetHeader("Authorization")
	}
	return strings.Contains(auth, `Client="Emby Theater"`) ||
		strings.Contains(auth, `Client="Emby for `) ||
		strings.Contains(auth, `Client="Emby Web"`) ||
		strings.Contains(auth, `Client="Emby Mobile"`)
}

func applyEmbyOfficialOverrides(c *gin.Context, info gin.H) {
	if !isEmbyOfficialClient(c) {
		return
	}
	// 必须严格等于 4.7.14：Emby Mobile (com.emby.mobile) connectionmanager.js 里
	// compareVersions 把返回值当 boolean 用，-1/1 都是 truthy → 任何 !== "4.7.14"
	// 都会被判定为"需要更新"。这是该客户端的 bug，只能精确匹配 minServerVersion。
	info["Version"] = "4.7.14"
	info["ProductName"] = "Emby Server"
}

func ping(c *gin.Context) {
	c.String(http.StatusOK, "FYMS")
}

func restart(c *gin.Context) {
	if u := middleware.GetAuthUser(c); u != nil {
		slog.Info("Server restart requested", "by", u.Name)
	} else {
		slog.Info("Server restart requested")
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
	c.Status(http.StatusNoContent)
}

func shutdown(c *gin.Context) {
	if u := middleware.GetAuthUser(c); u != nil {
		slog.Info("Server shutdown requested", "by", u.Name)
	} else {
		slog.Info("Server shutdown requested")
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
	c.Status(http.StatusNoContent)
}

func getConfiguration(c *gin.Context) {
	ctx := c.Request.Context()
	state := GetState(c)
	rows, err := state.DB.Query(ctx, "SELECT key, value FROM system_config")
	if err != nil {
		slog.Error("getConfiguration", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	cfg := gin.H{}
	for rows.Next() {
		var k string
		var v *string
		if err := rows.Scan(&k, &v); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		if v != nil {
			cfg[k] = *v
		} else {
			cfg[k] = nil
		}
	}
	c.JSON(http.StatusOK, cfg)
}

func postConfiguration(c *gin.Context) {
	ctx := c.Request.Context()
	state := GetState(c)

	var updates map[string]json.RawMessage
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	needViewsInvalidate := false
	needScrapeInvalidate := false
	needLimiterApply := false
	needActorImgInvalidate := false
	for key, raw := range updates {
		valStr := configValueString(raw)
		switch key {
		case services.BrandServerNameKey:
			valStr = strings.TrimSpace(valStr)
			if valStr == "" {
				valStr = state.Config.ServerName
			}
		case services.BrandIconSVGKey:
			valStr = strings.TrimSpace(valStr)
			if valStr != "" && !services.IsSVGDocument(valStr) {
				c.JSON(http.StatusBadRequest, gin.H{"message": "brand_icon_svg must be a valid svg document"})
				return
			}
		}
		_, err := state.DB.Exec(ctx,
			`INSERT INTO system_config (key, value) VALUES ($1, $2)
			 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
			key, valStr)
		if err != nil {
			slog.Error("postConfiguration", "key", key, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		switch key {
		case "platform_libraries_enabled", "platform_libraries_position", "library_show_item_count":
			needViewsInvalidate = true
		case "tmdb_rate_per_sec", "tmdb_rate_burst":
			needLimiterApply = true
		case "image_cache_copy_local":
			// 本地原图是否复制到 cache/sources(false=直读)。实时生效,无需重启。
			if state.ImageCache != nil {
				state.ImageCache.SetCopyLocal(valStr == "true")
			}
		case "strm_item_path_mode":
			// strm 条目 item.Path 返回 .strm 文件路径('strm')还是解析后真实路径('resolved')。
			// 实时生效,影响详情/列表/通知;MediaSources(播放)不受影响。
			dto.SetStrmItemPathMode(valStr)
		}
		if strings.HasPrefix(key, "scrape_") ||
			strings.HasPrefix(key, "tmdb_") ||
			strings.HasPrefix(key, "tvdb_") ||
			strings.HasPrefix(key, "fanart_") ||
			strings.HasPrefix(key, "douban_") ||
			strings.HasPrefix(key, "bangumi_") {
			needScrapeInvalidate = true
		}
		if strings.HasPrefix(key, "actor_img_") {
			needActorImgInvalidate = true
		}
	}
	if needViewsInvalidate {
		state.Cache.DelPattern(ctx, "views:*")
	}
	if needScrapeInvalidate {
		services.InvalidateScrapeAggregator()
	}
	if needLimiterApply {
		services.ApplyTmdbLimiterConfig(ctx, state.DB)
	}
	if needActorImgInvalidate {
		services.InvalidateActorImageConfig()
	}
	c.Status(http.StatusNoContent)
}

func configValueString(raw json.RawMessage) string {
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func configPage(c *gin.Context) {
	c.JSON(http.StatusOK, []any{})
}

func branding(c *gin.Context) {
	state := GetState(c)
	brandingCfg := services.LoadBrandingConfig(c.Request.Context(), state.DB, state.Config)
	c.JSON(http.StatusOK, gin.H{
		"LoginDisclaimer":     "",
		"CustomCss":           "",
		"SplashscreenEnabled": false,
		"ServerName":          brandingCfg.ServerName,
		"IconUrl":             brandingCfg.IconURL,
		"HasIcon":             brandingCfg.HasIcon,
	})
}

func getLogs(c *gin.Context) {
	state := GetState(c)
	level := c.Query("level")
	limit := 500
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	entries := state.LogBuffer.Get(level, limit)
	c.JSON(http.StatusOK, gin.H{
		"entries": entries,
		"total":   len(entries),
	})
}

type backupRequest struct {
	Categories []string `json:"categories"`
}

type updateApplyRequest struct {
	Categories []string `json:"categories"`
}

type updateChannelRequest struct {
	Channel string `json:"channel"`
}

func exportTable(ctx context.Context, pool *pgxpool.Pool, table string) ([]json.RawMessage, error) {
	sql := fmt.Sprintf("SELECT row_to_json(t) FROM %s t", table)
	rows, err := pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []json.RawMessage
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return out, rows.Err()
}

func createBackupSnapshot(ctx context.Context, state *AppState, categories []string) (gin.H, error) {
	categories = resolveCategories(categories)
	if len(categories) == 0 {
		categories = backupCategories
	}

	data := make(map[string]json.RawMessage)
	for _, cat := range categories {
		for _, table := range tablesForCategory(cat) {
			rows, err := exportTable(ctx, state.DB, table)
			if err != nil {
				return nil, err
			}
			raw, err := json.Marshal(rows)
			if err != nil {
				return nil, err
			}
			data[table] = raw
		}
	}

	payload := gin.H{
		"version":    "1.0",
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"categories": categories,
		"data":       data,
	}
	content, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}

	_ = os.MkdirAll(backupDir, 0755)
	filename := fmt.Sprintf("backup_%s.json", time.Now().Format("20060102_150405"))
	path := filepath.Join(backupDir, filename)
	if err := os.WriteFile(path, content, 0644); err != nil {
		return nil, err
	}

	slog.Info("[Backup] Created", "filename", filename, "size_kb", len(content)/1024, "categories", categories)
	return gin.H{
		"filename":   filename,
		"size":       len(content),
		"categories": categories,
	}, nil
}

func createBackup(c *gin.Context) {
	ctx := c.Request.Context()
	state := GetState(c)

	var body backupRequest
	_ = c.ShouldBindJSON(&body)
	result, err := createBackupSnapshot(ctx, state, body.Categories)
	if err != nil {
		slog.Error("createBackup failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func resolveCategories(in []string) []string {
	for _, s := range in {
		if s == "all" {
			return append([]string(nil), backupCategories...)
		}
	}
	return in
}

type backupListEntry struct {
	Filename   string   `json:"filename"`
	Size       int64    `json:"size"`
	Categories []string `json:"categories"`
	CreatedAt  *string  `json:"created_at,omitempty"`
}

func listBackups(c *gin.Context) {
	_ = os.MkdirAll(backupDir, 0755)
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		c.JSON(http.StatusOK, []backupListEntry{})
		return
	}

	list := make([]backupListEntry, 0)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := e.Info()
		size := int64(0)
		var created *string
		if err == nil {
			size = info.Size()
			if t := info.ModTime(); !t.IsZero() {
				s := t.UTC().Format(time.RFC3339)
				created = &s
			}
		}
		categories := parseBackupCategoriesHeader(name)
		list = append(list, backupListEntry{
			Filename:   name,
			Size:       size,
			Categories: categories,
			CreatedAt:  created,
		})
	}

	sort.Slice(list, func(i, j int) bool {
		ci, cj := list[i].CreatedAt, list[j].CreatedAt
		if ci == nil || cj == nil {
			return false
		}
		return *ci > *cj
	})

	c.JSON(http.StatusOK, list)
}

func parseBackupCategoriesHeader(filename string) []string {
	path := filepath.Join(backupDir, filename)
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, 1024)
	n, _ := f.Read(buf)
	header := string(buf[:n])
	if start := strings.Index(header, `"categories"`); start >= 0 {
		if arrStart := strings.Index(header[start:], "["); arrStart >= 0 {
			arrStart += start
			if arrEnd := strings.Index(header[arrStart:], "]"); arrEnd >= 0 {
				arrStr := header[arrStart : arrStart+arrEnd+1]
				var cats []string
				if json.Unmarshal([]byte(arrStr), &cats) == nil {
					return cats
				}
			}
		}
	}
	return nil
}

// backupSummary 描述一个备份/导出文件里包含哪些类别、每张表多少行,
// 供前端在恢复前"先解析展示有哪些东西"。
type backupSummary struct {
	Filename   string         `json:"filename"`
	Version    string         `json:"version"`
	CreatedAt  string         `json:"created_at"`
	Categories []string       `json:"categories"`
	Counts     map[string]int `json:"counts"`
}

// summarizeBackup 解析备份 JSON,返回摘要与原始 data(供导入时按规范格式重新落盘)。
// 只数组长度,不深度反序列化每行,避免大媒体备份解析过重。
func summarizeBackup(raw []byte) (*backupSummary, map[string]json.RawMessage, error) {
	var parsed struct {
		Version    string                     `json:"version"`
		CreatedAt  string                     `json:"created_at"`
		Categories []string                   `json:"categories"`
		Data       map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, nil, fmt.Errorf("无法解析备份文件: %w", err)
	}
	if parsed.Data == nil {
		return nil, nil, fmt.Errorf("备份文件中没有 data 数据")
	}
	counts := make(map[string]int)
	for table, rawRows := range parsed.Data {
		var rows []json.RawMessage
		if json.Unmarshal(rawRows, &rows) == nil {
			counts[table] = len(rows)
		}
	}
	cats := parsed.Categories
	if len(cats) == 0 {
		cats = categoriesForTables(parsed.Data)
	}
	return &backupSummary{
		Version:    parsed.Version,
		CreatedAt:  parsed.CreatedAt,
		Categories: cats,
		Counts:     counts,
	}, parsed.Data, nil
}

// uploadBackup 接收上传的导出 JSON,解析校验后以规范格式落盘到 data/backups,
// 返回摘要供前端展示。落盘后即出现在备份历史,复用现有按类别恢复流程。
func uploadBackup(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "缺少上传文件"})
		return
	}
	f, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	summary, data, err := summarizeBackup(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	// 以规范格式重新落盘:保证 listBackups 头部 categories 解析与后续恢复一致。
	payload := gin.H{
		"version":    orDefault(summary.Version, "1.0"),
		"created_at": orDefault(summary.CreatedAt, time.Now().UTC().Format(time.RFC3339)),
		"categories": summary.Categories,
		"data":       data,
	}
	content, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_ = os.MkdirAll(backupDir, 0755)
	filename := fmt.Sprintf("import_%s.json", time.Now().Format("20060102_150405"))
	if err := os.WriteFile(filepath.Join(backupDir, filename), content, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	summary.Filename = filename
	slog.Info("[Backup] Imported", "filename", filename, "categories", summary.Categories)
	c.JSON(http.StatusOK, summary)
}

// backupSummaryHandler 解析已存在的备份文件并返回摘要(供历史备份恢复前预览行数)。
func backupSummaryHandler(c *gin.Context) {
	filename := c.Param("filename")
	if !safeBackupName(filename) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid filename"})
		return
	}
	raw, err := os.ReadFile(filepath.Join(backupDir, filename))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Not found"})
		return
	}
	summary, _, err := summarizeBackup(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	summary.Filename = filename
	c.JSON(http.StatusOK, summary)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func safeBackupName(filename string) bool {
	return filename != "" && !strings.Contains(filename, "..") && !strings.ContainsAny(filename, `/\`)
}

func downloadBackup(c *gin.Context) {
	filename := c.Param("filename")
	if !safeBackupName(filename) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid filename"})
		return
	}
	path := filepath.Join(backupDir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Not found"})
		return
	}
	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/json", data)
}

func deleteBackup(c *gin.Context) {
	filename := c.Param("filename")
	if !safeBackupName(filename) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid filename"})
		return
	}
	path := filepath.Join(backupDir, filename)
	if err := os.Remove(path); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Not found"})
		return
	}
	slog.Info("[Backup] Deleted", "filename", filename)
	c.Status(http.StatusNoContent)
}

type restoreRequest struct {
	Filename   string   `json:"filename"`
	Categories []string `json:"categories"`
}

func restoreBackup(c *gin.Context) {
	ctx := c.Request.Context()
	state := GetState(c)

	var body restoreRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if !safeBackupName(body.Filename) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid filename"})
		return
	}

	path := filepath.Join(backupDir, body.Filename)
	raw, err := os.ReadFile(path)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Not found"})
		return
	}

	var backup struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &backup); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Invalid backup file: %v", err)})
		return
	}
	if backup.Data == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "No data in backup"})
		return
	}

	categories := resolveCategories(body.Categories)
	if len(categories) == 0 {
		categories = backupCategories
	}

	orderedCats := backupCategories
	tx, err := state.DB.Begin(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer tx.Rollback(ctx)

	var restoredTables []string
	for _, cat := range orderedCats {
		if !containsStr(categories, cat) {
			continue
		}
		tables := tablesForCategory(cat)
		reverse := append([]string(nil), tables...)
		for i, j := 0, len(reverse)-1; i < j; i, j = i+1, j-1 {
			reverse[i], reverse[j] = reverse[j], reverse[i]
		}
		for _, table := range reverse {
			if _, err := tx.Exec(ctx, fmt.Sprintf("TRUNCATE %s CASCADE", table)); err != nil {
				slog.Error("restore truncate", "table", table, "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
		}
		for _, table := range tables {
			rawRows, ok := backup.Data[table]
			if !ok {
				continue
			}
			var rows []json.RawMessage
			if err := json.Unmarshal(rawRows, &rows); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("Invalid table data %s: %v", table, err)})
				return
			}
			for _, row := range rows {
				rowJSON := string(row)
				sql := fmt.Sprintf(
					`INSERT INTO %s SELECT * FROM json_populate_record(NULL::%s, $1::json) ON CONFLICT DO NOTHING`,
					table, table)
				if _, err := tx.Exec(ctx, sql, rowJSON); err != nil {
					slog.Error("restore insert", "table", table, "error", err)
					c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
					return
				}
			}
			restoredTables = append(restoredTables, table)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	state.Cache.DelPattern(ctx, "*")

	slog.Info("[Restore] Restored", "filename", body.Filename, "tables", restoredTables)
	c.JSON(http.StatusOK, gin.H{
		"success":         true,
		"restored_tables": restoredTables,
	})
}

func containsStr(slice []string, s string) bool {
	for _, x := range slice {
		if x == s {
			return true
		}
	}
	return false
}

func getUpdateStatus(c *gin.Context) {
	state := GetState(c)
	c.JSON(http.StatusOK, state.Updater.GetStatus(c.Request.Context()))
}

func checkForUpdate(c *gin.Context) {
	state := GetState(c)
	ctx := c.Request.Context()
	if t := state.TaskCenter.Get(taskcenter.KindUpdate); t != nil {
		if _, err := t.Start(ctx, taskcenter.StartParams{"action": "check"}, taskcenter.TriggerManual); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"message": err.Error(), "status": state.Updater.GetStatus(ctx)})
			return
		}
		c.JSON(http.StatusOK, state.Updater.GetStatus(ctx))
		return
	}
	status, err := state.Updater.Check(ctx)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error(), "status": status})
		return
	}
	c.JSON(http.StatusOK, status)
}

func setUpdateChannel(c *gin.Context) {
	state := GetState(c)
	var body updateChannelRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	status, err := state.Updater.SetChannel(c.Request.Context(), body.Channel)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, status)
}

func applyUpdate(c *gin.Context) {
	state := GetState(c)
	var body updateApplyRequest
	_ = c.ShouldBindJSON(&body)

	state.Updater.MarkBackingUp()
	if _, err := createBackupSnapshot(c.Request.Context(), state, body.Categories); err != nil {
		state.Updater.MarkFailure(fmt.Errorf("create backup before update: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	ctx := c.Request.Context()
	if t := state.TaskCenter.Get(taskcenter.KindUpdate); t != nil {
		if _, err := t.Start(ctx, taskcenter.StartParams{"action": "apply"}, taskcenter.TriggerManual); err != nil {
			state.Updater.MarkFailure(err)
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "status": state.Updater.GetStatus(ctx)})
			return
		}
		c.JSON(http.StatusOK, state.Updater.GetStatus(ctx))
		return
	}

	status, err := state.Updater.StartApply(ctx)
	if err != nil {
		state.Updater.MarkFailure(err)
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "status": status})
		return
	}
	c.JSON(http.StatusOK, status)
}

func rollbackUpdate(c *gin.Context) {
	state := GetState(c)
	ctx := c.Request.Context()
	if t := state.TaskCenter.Get(taskcenter.KindUpdate); t != nil {
		if _, err := t.Start(ctx, taskcenter.StartParams{"action": "rollback"}, taskcenter.TriggerManual); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "status": state.Updater.GetStatus(ctx)})
			return
		}
		c.JSON(http.StatusOK, state.Updater.GetStatus(ctx))
		return
	}

	status, err := state.Updater.StartRollback(ctx)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "status": status})
		return
	}
	c.JSON(http.StatusOK, status)
}

type embyMigrateRequest struct {
	Users []struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	} `json:"users"`
	Policy *models.PolicyUpdate `json:"policy"`
}

func embyMigrate(c *gin.Context) {
	ctx := c.Request.Context()
	state := GetState(c)

	var body embyMigrateRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	const placeholderHash = "$2b$10$placeholder.not.a.valid.bcrypt.hash.000000000000000000000"
	total := len(body.Users)
	var imported, skipped int64
	var errs []string

	for _, eu := range body.Users {
		if strings.TrimSpace(eu.Name) == "" {
			skipped++
			continue
		}

		var existingID uuid.UUID
		err := state.DB.QueryRow(ctx, `SELECT id FROM users WHERE name = $1`, eu.Name).Scan(&existingID)
		if err == nil {
			skipped++
			continue
		}
		if err != pgx.ErrNoRows {
			errs = append(errs, fmt.Sprintf("%s: %v", eu.Name, err))
			continue
		}

		var embyHash *string
		if eu.Password != "" {
			embyHash = &eu.Password
		}

		var userID uuid.UUID
		err = state.DB.QueryRow(ctx,
			`INSERT INTO users (name, password_hash, emby_password_hash, is_admin)
			 VALUES ($1, $2, $3, FALSE) ON CONFLICT (name) DO NOTHING RETURNING id`,
			eu.Name, placeholderHash, embyHash,
		).Scan(&userID)
		if err == pgx.ErrNoRows {
			skipped++
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", eu.Name, err))
			continue
		}

		if body.Policy != nil {
			if err := models.UpsertUserPolicy(ctx, state.DB, userID, body.Policy); err != nil {
				errs = append(errs, fmt.Sprintf("%s: policy error: %v", eu.Name, err))
			}
		} else {
			_, _ = state.DB.Exec(ctx,
				`INSERT INTO user_policies (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, userID)
		}
		imported++
	}

	slog.Info("[EmbyMigrate]", "total", total, "imported", imported, "skipped", skipped, "errors", len(errs))
	c.JSON(http.StatusOK, gin.H{
		"total":    total,
		"imported": imported,
		"skipped":  skipped,
		"errors":   errs,
	})
}
