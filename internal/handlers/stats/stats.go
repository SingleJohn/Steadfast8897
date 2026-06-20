package stats

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"fyms/internal/repository"
)

func RegisterStatsRoutes(group *gin.RouterGroup, state *AppState, authMW, adminMW gin.HandlerFunc) {
	_ = authMW
	s := group.Group("")
	s.GET("/Stats/UserActivity", adminMW, getUserActivity)
	s.GET("/Stats/DailyActivity", adminMW, getDailyActivity)
	s.GET("/Stats/HourlyReport", adminMW, getHourlyReport)
	s.GET("/Stats/BreakdownReport", adminMW, getBreakdownReport)
	s.GET("/Stats/RecentPlayback", adminMW, getRecentPlayback)
	s.GET("/Stats/UserUsageRanking", adminMW, getUserUsageRanking)

	s.GET("/user_usage_stats/user_activity", adminMW, getUserActivity)
	s.GET("/user_usage_stats/PlayActivity", adminMW, getDailyActivity)
	s.GET("/user_usage_stats/HourlyReport", adminMW, getHourlyReport)
	s.GET("/user_usage_stats/:type/BreakdownReport", adminMW, getBreakdownReportLegacy)
	s.GET("/user_usage_stats/RecentPlayback", adminMW, getRecentPlayback)
	s.GET("/user_usage_stats/UserUsageRanking", adminMW, getUserUsageRanking)
}

func getUserActivity(c *gin.Context) {
	state := GetState(c)
	days := 30
	if s := strings.TrimSpace(c.Query("days")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			days = n
		}
	}

	rows, err := repository.NewStatsRepository(state.DB).UserActivity(c.Request.Context(), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	var out []gin.H
	for _, row := range rows {
		entry := gin.H{
			"user_id":         row.UserID,
			"user_name":       ptrStrOr(row.UserName, "Unknown"),
			"has_image":       false,
			"total_plays":     row.TotalPlays,
			"total_play_time": row.TotalDuration,
		}
		if row.LastSeen != nil {
			entry["last_seen"] = row.LastSeen.UTC().Format(time.RFC3339)
		}
		if row.ItemName != nil {
			entry["item_name"] = *row.ItemName
		}
		if row.ClientName != nil {
			entry["client_name"] = *row.ClientName
		}
		out = append(out, entry)
	}
	if out == nil {
		out = []gin.H{}
	}
	c.JSON(http.StatusOK, out)
}

func ptrStrOr(p *string, def string) string {
	if p != nil {
		return *p
	}
	return def
}

type statsUsageBucket struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

func statsUsageBuckets(raw string) []statsUsageBucket {
	var out []statsUsageBucket
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []statsUsageBucket{}
	}
	return out
}

func parseStatsIntQuery(c *gin.Context, key string, def, min, max int) (int, bool) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || (max > 0 && n > max) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid " + key})
		return 0, false
	}
	return n, true
}

func statsUsageFilter(c *gin.Context) (repository.StatsUsageFilter, bool) {
	days, ok := parseStatsIntQuery(c, "days", 30, 0, 3650)
	if !ok {
		return repository.StatsUsageFilter{}, false
	}
	return repository.StatsUsageFilter{
		Days:       days,
		User:       strings.TrimSpace(c.Query("user")),
		ClientName: strings.TrimSpace(c.Query("client_name")),
		DeviceName: strings.TrimSpace(c.Query("device_name")),
		ClientIP:   strings.TrimSpace(c.Query("client_ip")),
	}, true
}

func getUserUsageRanking(c *gin.Context) {
	state := GetState(c)

	page, ok := parseStatsIntQuery(c, "page", 1, 1, 0)
	if !ok {
		return
	}
	pageSize, ok := parseStatsIntQuery(c, "page_size", 20, 1, 100)
	if !ok {
		return
	}

	sortBy := strings.TrimSpace(c.Query("sort_by"))
	if sortBy == "" {
		sortBy = "total_plays"
	}
	if _, ok := statsUsageSortColumn(sortBy); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid sort_by"})
		return
	}

	sortOrder := strings.ToUpper(strings.TrimSpace(c.Query("sort_order")))
	if sortOrder == "" {
		sortOrder = "DESC"
	}
	if sortOrder != "ASC" && sortOrder != "DESC" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid sort_order"})
		return
	}

	filter, ok := statsUsageFilter(c)
	if !ok {
		return
	}
	minClientCount, ok := parseStatsIntQuery(c, "min_client_count", 0, 0, 0)
	if !ok {
		return
	}
	minPlayerCount, ok := parseStatsIntQuery(c, "min_player_count", 0, 0, 0)
	if !ok {
		return
	}
	minIPCount, ok := parseStatsIntQuery(c, "min_ip_count", 0, 0, 0)
	if !ok {
		return
	}

	filter.Page = page
	filter.PageSize = pageSize
	filter.SortBy = sortBy
	filter.SortOrder = sortOrder
	filter.MinClientCount = minClientCount
	filter.MinPlayerCount = minPlayerCount
	filter.MinIPCount = minIPCount
	rows, summary, err := repository.NewStatsRepository(state.DB).UserUsageRanking(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	var total int64
	var items []gin.H
	for _, row := range rows {
		total = row.TotalRows
		entry := gin.H{
			"user_id":          row.UserID,
			"user_name":        row.UserName,
			"total_plays":      row.TotalPlays,
			"total_duration":   row.TotalDuration,
			"client_count":     row.ClientCount,
			"player_count":     row.PlayerCount,
			"ip_count":         row.IPCount,
			"last_item_name":   ptrStrOr(row.LastItem, ""),
			"last_client_name": ptrStrOr(row.LastClient, ""),
			"last_device_name": ptrStrOr(row.LastDevice, ""),
			"last_client_ip":   ptrStrOr(row.LastClientIP, ""),
			"last_user_agent":  ptrStrOr(row.LastUserAgent, ""),
			"top_clients":      statsUsageBuckets(row.TopClients),
			"top_players":      statsUsageBuckets(row.TopPlayers),
			"top_ips":          statsUsageBuckets(row.TopIPs),
			"top_user_agents":  statsUsageBuckets(row.TopUserAgents),
		}
		if row.LastSeen != nil {
			entry["last_seen"] = row.LastSeen.UTC().Format(time.RFC3339)
		}
		items = append(items, entry)
	}
	if items == nil {
		items = []gin.H{}
	}
	if total == 0 {
		total = summary.Users
	}
	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"summary": gin.H{
			"active_users":   summary.Users,
			"total_plays":    summary.Plays,
			"total_duration": summary.Duration,
			"client_count":   summary.ClientCount,
			"player_count":   summary.PlayerCount,
			"ip_count":       summary.IPCount,
		},
	})
}

func statsUsageSortColumn(sortBy string) (string, bool) {
	switch sortBy {
	case "last_seen", "total_plays", "total_duration", "client_count", "player_count", "ip_count", "user_name":
		return sortBy, true
	default:
		return "", false
	}
}

func getDailyActivity(c *gin.Context) {
	state := GetState(c)
	days := 30
	if s := strings.TrimSpace(c.Query("days")); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"message": "invalid days"})
			return
		}
		days = n
	}

	rows, err := repository.NewStatsRepository(state.DB).DailyActivity(c.Request.Context(), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	var out []gin.H
	for _, row := range rows {
		out = append(out, gin.H{
			"date":           row.Day.Format("2006-01-02"),
			"count":          row.Count,
			"total_duration": row.TotalDuration,
		})
	}
	if out == nil {
		out = []gin.H{}
	}
	c.JSON(http.StatusOK, out)
}

func getHourlyReport(c *gin.Context) {
	state := GetState(c)
	days := 30
	if s := strings.TrimSpace(c.Query("days")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			days = n
		}
	}

	rows, err := repository.NewStatsRepository(state.DB).HourlyReport(c.Request.Context(), days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	var out []gin.H
	for _, row := range rows {
		out = append(out, gin.H{"DayOfWeek": row.DayOfWeek, "Hour": row.Hour, "Count": row.Count})
	}
	if out == nil {
		out = []gin.H{}
	}
	c.JSON(http.StatusOK, out)
}

func breakdownReportQuery(c *gin.Context, reportType string) {
	state := GetState(c)
	days := 30
	if s := strings.TrimSpace(c.Query("days")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			days = n
		}
	}

	rows, err := repository.NewStatsRepository(state.DB).BreakdownReport(c.Request.Context(), days, reportType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	var out []gin.H
	for _, row := range rows {
		out = append(out, gin.H{"label": row.Label, "count": row.Count, "total_duration": row.TotalDuration})
	}
	if out == nil {
		out = []gin.H{}
	}
	c.JSON(http.StatusOK, out)
}

func getBreakdownReport(c *gin.Context) {
	t := strings.TrimSpace(c.Query("type"))
	if t == "" {
		t = "ItemType"
	}
	breakdownReportQuery(c, t)
}

func getBreakdownReportLegacy(c *gin.Context) {
	t := strings.TrimSpace(c.Param("type"))
	if t == "" {
		t = "ItemType"
	}
	breakdownReportQuery(c, t)
}

func getRecentPlayback(c *gin.Context) {
	state := GetState(c)
	limit := int32(50)
	if s := strings.TrimSpace(c.Query("limit")); s != "" {
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil || n < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"message": "invalid limit"})
			return
		}
		limit = int32(n)
	}

	rows, err := repository.NewStatsRepository(state.DB).RecentPlayback(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	var out []gin.H
	for _, row := range rows {
		out = append(out, gin.H{
			"date":          row.DateCreated.UTC().Format(time.RFC3339),
			"user_name":     row.UserName,
			"item_name":     row.ItemName,
			"item_type":     row.ItemType,
			"series_name":   row.SeriesName,
			"client_name":   row.ClientName,
			"device_name":   row.DeviceName,
			"client_ip":     row.ClientIP,
			"play_duration": row.PlayDuration,
		})
	}
	if out == nil {
		out = []gin.H{}
	}
	c.JSON(http.StatusOK, out)
}
