package gateway

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// RegisterAPIRoutes registers the gateway management API endpoints.
func RegisterAPIRoutes(r *gin.RouterGroup, store *Store, runtime *Runtime, adminMW gin.HandlerFunc) {
	r.GET("/Gateway/Config", adminMW, handleGetConfig(store))
	r.POST("/Gateway/Config", adminMW, handleSaveConfig(store, runtime))
	r.GET("/Gateway/Logs", adminMW, handleListLogs(store))
	r.GET("/Gateway/Stats/Daily", adminMW, handleDailyStats(store))
	r.GET("/Gateway/IPStats/Summary", adminMW, handleIPStatsSummary(store))
	r.GET("/Gateway/Redirects/Summary", adminMW, handleRedirectSummary(store))
	r.GET("/Gateway/Redirects/Logs", adminMW, handleRedirectLogs(store))
	r.GET("/Gateway/Backends", adminMW, handleListBackends(store))
	r.GET("/Gateway/Health/Emby", adminMW, handleEmbyHealth(store))
	r.POST("/Gateway/Emby/Check", adminMW, handleEmbyCheck())
}

func handleGetConfig(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg, err := store.LoadConfig(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, cfg)
	}
}

func handleSaveConfig(store *Store, runtime *Runtime) gin.HandlerFunc {
	return func(c *gin.Context) {
		var cfg GatewayConfig
		if err := c.ShouldBindJSON(&cfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := store.SaveConfig(c.Request.Context(), &cfg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// Hot reload
		if err := runtime.Rebuild(c.Request.Context(), &cfg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "config saved but rebuild failed: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

func handleListLogs(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		params := LogQueryParams{
			Tag:      c.Query("tag"),
			SourceID: c.Query("source_id"),
			Limit:    intQuery(c, "limit", 50),
			Offset:   intQuery(c, "offset", 0),
		}
		if s := c.Query("status"); s != "" {
			params.Status, _ = strconv.Atoi(s)
		}
		logs, total, err := store.QueryRequestLogs(c.Request.Context(), params)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": logs, "total": total})
	}
}

func handleDailyStats(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		sourceID := c.Query("source_id")
		days := intQuery(c, "days", 30)
		stats, err := store.QueryDailyStats(c.Request.Context(), sourceID, days)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, stats)
	}
}

func handleIPStatsSummary(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		params := IPStatsSummaryParams{
			Tag:      c.DefaultQuery("tag", "proxy"),
			Mode:     c.DefaultQuery("mode", "all"),
			SourceID: c.Query("source_id"),
			Limit:    intQuery(c, "limit", 20),
			Scope:    c.Query("scope"),
			Since:    unixTimeQuery(c, "since"),
			Until:    unixTimeQuery(c, "until"),
		}
		summary, err := store.GetIPStatsSummary(c.Request.Context(), params)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, summary)
	}
}

func handleRedirectSummary(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		sourceID := c.Query("source_id")
		hours := intQuery(c, "hours", 24)
		summary, err := store.GetRedirectSummary(c.Request.Context(), sourceID, hours)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, summary)
	}
}

func handleRedirectLogs(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		params := LogQueryParams{
			Tag:      "proxy",
			Status:   302,
			SourceID: c.Query("source_id"),
			Limit:    intQuery(c, "limit", 50),
			Offset:   intQuery(c, "offset", 0),
		}
		logs, total, err := store.QueryRequestLogs(c.Request.Context(), params)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": logs, "total": total})
	}
}

func handleListBackends(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg, err := store.LoadConfig(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		type backendInfo struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Type string `json:"type"`
		}
		var result []backendInfo
		for _, b := range cfg.Backends {
			if b.Enabled {
				result = append(result, backendInfo{ID: b.ID, Name: b.Name, Type: b.Type})
			}
		}
		c.JSON(http.StatusOK, result)
	}
}

func handleEmbyHealth(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg, err := store.LoadConfig(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		type sourceHealth struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		}
		var result []sourceHealth
		for _, src := range cfg.Sources {
			status := "unknown"
			if src.Enabled {
				status = "enabled"
			} else {
				status = "disabled"
			}
			result = append(result, sourceHealth{ID: src.ID, Name: src.Name, Status: status})
		}
		c.JSON(http.StatusOK, result)
	}
}

func handleEmbyCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Host   string `json:"host"`
			ApiKey string `json:"api_key"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get(req.Host + "/System/Info/Public")
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": err.Error()})
			return
		}
		defer resp.Body.Close()
		c.JSON(http.StatusOK, gin.H{"success": resp.StatusCode == 200, "status_code": resp.StatusCode})
	}
}

func intQuery(c *gin.Context, key string, defaultVal int) int {
	s := c.Query(key)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}

func unixTimeQuery(c *gin.Context, key string) *time.Time {
	raw := c.Query(key)
	if raw == "" {
		return nil
	}
	sec, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	t := time.Unix(sec, 0).UTC()
	return &t
}
