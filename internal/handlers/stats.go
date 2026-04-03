package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/middleware"
)

func RegisterStatsRoutes(group *gin.RouterGroup, state *AppState, authMW, adminMW gin.HandlerFunc) {
	_ = authMW
	var _ *pgxpool.Pool = state.DB
	_ = (*middleware.AuthUser)(nil)
	s := group.Group("")
	s.GET("/Stats/UserActivity", adminMW, getUserActivity)
	s.GET("/Stats/DailyActivity", adminMW, getDailyActivity)
	s.GET("/Stats/HourlyReport", adminMW, getHourlyReport)
	s.GET("/Stats/BreakdownReport", adminMW, getBreakdownReport)
	s.GET("/Stats/RecentPlayback", adminMW, getRecentPlayback)

	s.GET("/user_usage_stats/user_activity", adminMW, getUserActivity)
	s.GET("/user_usage_stats/PlayActivity", adminMW, getDailyActivity)
	s.GET("/user_usage_stats/HourlyReport", adminMW, getHourlyReport)
	s.GET("/user_usage_stats/:type/BreakdownReport", adminMW, getBreakdownReportLegacy)
	s.GET("/user_usage_stats/RecentPlayback", adminMW, getRecentPlayback)
}

func getUserActivity(c *gin.Context) {
	state := GetState(c)
	days := 30
	if s := strings.TrimSpace(c.Query("days")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			days = n
		}
	}

	rows, err := state.DB.Query(c.Request.Context(),
		`SELECT pa.user_id::text, u.name as user_name,
			MAX(pa.date_created) as last_seen,
			MAX(pa.item_name) as last_item_name,
			MAX(pa.client_name) as last_client_name,
			COUNT(*) as total_plays,
			COALESCE(SUM(pa.play_duration), 0)::bigint as total_duration
		 FROM playback_activity pa
		 LEFT JOIN users u ON pa.user_id = u.id
		 WHERE pa.date_created >= NOW() - INTERVAL '1 day' * $1
		 GROUP BY pa.user_id, u.name
		 ORDER BY last_seen DESC`, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	var out []gin.H
	for rows.Next() {
		var id string
		var name *string
		var lastSeen *time.Time
		var itemName, clientName *string
		var totalPlays, totalDuration int64
		if err := rows.Scan(&id, &name, &lastSeen, &itemName, &clientName, &totalPlays, &totalDuration); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		entry := gin.H{
			"user_id":         id,
			"user_name":       ptrStrOr(name, "Unknown"),
			"has_image":       false,
			"total_plays":     totalPlays,
			"total_play_time": totalDuration,
		}
		if lastSeen != nil {
			entry["last_seen"] = lastSeen.UTC().Format(time.RFC3339)
		}
		if itemName != nil {
			entry["item_name"] = *itemName
		}
		if clientName != nil {
			entry["client_name"] = *clientName
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
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

	since := time.Now().UTC().AddDate(0, 0, -days)

	rows, err := state.DB.Query(c.Request.Context(),
		`SELECT date_created::date AS day, COUNT(*)::bigint,
			COALESCE(SUM(play_duration), 0)::bigint AS total_duration
		 FROM playback_activity
		 WHERE date_created >= $1
		 GROUP BY 1
		 ORDER BY 1`,
		since)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	var out []gin.H
	for rows.Next() {
		var day time.Time
		var cnt, dur int64
		if err := rows.Scan(&day, &cnt, &dur); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		out = append(out, gin.H{
			"date":           day.Format("2006-01-02"),
			"count":          cnt,
			"total_duration": dur,
		})
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
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

	rows, err := state.DB.Query(c.Request.Context(),
		`SELECT EXTRACT(DOW FROM date_created)::int AS day_of_week,
			EXTRACT(HOUR FROM date_created)::int AS hour,
			COUNT(*)::bigint
		 FROM playback_activity
		 WHERE date_created >= NOW() - INTERVAL '1 day' * $1
		 GROUP BY day_of_week, hour
		 ORDER BY day_of_week, hour`, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	var out []gin.H
	for rows.Next() {
		var dow, hour int
		var cnt int64
		if err := rows.Scan(&dow, &hour, &cnt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		out = append(out, gin.H{"DayOfWeek": dow, "Hour": hour, "Count": cnt})
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
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

	var groupCol, labelCol string
	needJoin := false
	switch reportType {
	case "UserId":
		groupCol = "pa.user_id"
		labelCol = "u.name"
		needJoin = true
	case "ItemType":
		groupCol = "pa.item_type"
		labelCol = "pa.item_type"
	case "ClientName":
		groupCol = "pa.client_name"
		labelCol = "pa.client_name"
	case "DeviceName":
		groupCol = "pa.device_name"
		labelCol = "pa.device_name"
	case "PlaybackMethod":
		groupCol = "pa.play_method"
		labelCol = "pa.play_method"
	default:
		groupCol = "pa.item_type"
		labelCol = "pa.item_type"
	}

	join := ""
	if needJoin {
		join = "LEFT JOIN users u ON pa.user_id = u.id"
	}
	sql := "SELECT COALESCE(" + labelCol + "::text, 'Unknown') as label, COUNT(*)::bigint as count," +
		" COALESCE(SUM(pa.play_duration), 0)::bigint as total_duration" +
		" FROM playback_activity pa " + join +
		" WHERE pa.date_created >= NOW() - INTERVAL '1 day' * $1" +
		" GROUP BY " + groupCol + ", " + labelCol +
		" ORDER BY count DESC"

	rows, err := state.DB.Query(c.Request.Context(), sql, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	var out []gin.H
	for rows.Next() {
		var label string
		var cnt, dur int64
		if err := rows.Scan(&label, &cnt, &dur); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		out = append(out, gin.H{"label": label, "count": cnt, "total_duration": dur})
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
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

	rows, err := state.DB.Query(c.Request.Context(),
		`SELECT pa.date_created, pa.item_name, pa.item_type, pa.series_name,
			pa.client_name, pa.device_name, pa.client_ip, pa.play_duration,
			u.name AS user_name
		 FROM playback_activity pa
		 LEFT JOIN users u ON pa.user_id = u.id
		 ORDER BY pa.date_created DESC
		 LIMIT $1`,
		limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	var out []gin.H
	for rows.Next() {
		var (
			dateCreated  time.Time
			itemName     *string
			itemType     *string
			seriesName   *string
			clientName   *string
			deviceName   *string
			clientIP     *string
			playDuration *int32
			userName     *string
		)
		if err := rows.Scan(
			&dateCreated, &itemName, &itemType, &seriesName,
			&clientName, &deviceName, &clientIP, &playDuration,
			&userName,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		out = append(out, gin.H{
			"date":          dateCreated.UTC().Format(time.RFC3339),
			"user_name":     userName,
			"item_name":     itemName,
			"item_type":     itemType,
			"series_name":   seriesName,
			"client_name":   clientName,
			"device_name":   deviceName,
			"client_ip":     clientIP,
			"play_duration": playDuration,
		})
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if out == nil {
		out = []gin.H{}
	}
	c.JSON(http.StatusOK, out)
}
