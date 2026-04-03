package handlers

import (
	"context"
	"encoding/json"
	"fmt"
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
	"fyms/internal/middleware"
	"fyms/internal/models"
)

const backupDir = "data/backups"

var backupCategories = []string{"settings", "users", "libraries", "media", "activity"}

func tablesForCategory(cat string) []string {
	switch cat {
	case "settings":
		return []string{"system_config"}
	case "users":
		return []string{"users", "user_policies", "api_keys", "access_tokens", "user_library_access"}
	case "libraries":
		return []string{"libraries"}
	case "media":
		return []string{"genres", "items", "item_genres", "cast_members", "media_versions", "media_streams", "user_item_data"}
	case "activity":
		return []string{"playback_activity"}
	default:
		return nil
	}
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

func systemInfo(state *AppState, public bool) gin.H {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	port := state.Config.Port
	info := gin.H{
		"ServerName":             state.Config.ServerName,
		"Version":                state.Config.Version,
		"Id":                     state.Config.ServerID,
		"OperatingSystem":        runtime.GOOS,
		"ProductName":            "FYMS",
		"StartupWizardCompleted": true,
		"LocalAddress":           fmt.Sprintf("http://%s:%d", getLocalIP(), port),
		"CanSelfRestart":         true,
	}

	if !public {
		info["OperatingSystemDisplayName"] = fmt.Sprintf("%s %s", runtime.GOOS, runtime.GOARCH)
		info["HasPendingRestart"] = false
		info["IsShuttingDown"] = false
		info["CanLaunchWebBrowser"] = false
		info["HasUpdateAvailable"] = false
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
	group.GET("/System/Backups/:filename", adminMW, downloadBackup)
	group.DELETE("/System/Backups/:filename", adminMW, deleteBackup)
	group.POST("/System/Restore", adminMW, restoreBackup)
	group.POST("/System/EmbyMigrate", adminMW, embyMigrate)
}

func getSystemInfo(c *gin.Context) {
	c.JSON(http.StatusOK, systemInfo(GetState(c), false))
}

func getSystemInfoPublic(c *gin.Context) {
	c.JSON(http.StatusOK, systemInfo(GetState(c), true))
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

	for key, raw := range updates {
		valStr := configValueString(raw)
		_, err := state.DB.Exec(ctx,
			`INSERT INTO system_config (key, value) VALUES ($1, $2)
			 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
			key, valStr)
		if err != nil {
			slog.Error("postConfiguration", "key", key, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
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
	c.JSON(http.StatusOK, gin.H{
		"LoginDisclaimer":     "",
		"CustomCss":           "",
		"SplashscreenEnabled": false,
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

func createBackup(c *gin.Context) {
	ctx := c.Request.Context()
	state := GetState(c)

	var body backupRequest
	_ = c.ShouldBindJSON(&body)

	categories := resolveCategories(body.Categories)
	if len(categories) == 0 {
		categories = backupCategories
	}

	data := make(map[string]json.RawMessage)
	for _, cat := range categories {
		for _, table := range tablesForCategory(cat) {
			rows, err := exportTable(ctx, state.DB, table)
			if err != nil {
				slog.Error("createBackup export", "table", table, "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
			}
			raw, err := json.Marshal(rows)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				return
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
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	_ = os.MkdirAll(backupDir, 0755)
	filename := fmt.Sprintf("backup_%s.json", time.Now().Format("20060102_150405"))
	filepath := filepath.Join(backupDir, filename)
	if err := os.WriteFile(filepath, content, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	slog.Info("[Backup] Created", "filename", filename, "size_kb", len(content)/1024, "categories", categories)
	c.JSON(http.StatusOK, gin.H{
		"filename":   filename,
		"size":       len(content),
		"categories": categories,
	})
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

	orderedCats := []string{"settings", "users", "libraries", "media", "activity"}
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

type embyMigrateRequest struct {
	Users  []struct {
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
