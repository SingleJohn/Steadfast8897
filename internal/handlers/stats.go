package handlers

import (
	"encoding/json"
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

func statsUsageFilter(c *gin.Context, startArg int) (string, []any, bool) {
	days, ok := parseStatsIntQuery(c, "days", 30, 0, 3650)
	if !ok {
		return "", nil, false
	}

	var args []any
	var filters []string
	addArg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(startArg+len(args)-1)
	}

	if days > 0 {
		filters = append(filters, "pa.date_created >= NOW() - INTERVAL '1 day' * "+addArg(days))
	}
	if v := strings.TrimSpace(c.Query("user")); v != "" {
		filters = append(filters, "COALESCE(u.name, '') ILIKE "+addArg("%"+v+"%"))
	}
	if v := strings.TrimSpace(c.Query("client_name")); v != "" {
		filters = append(filters, "COALESCE(NULLIF(BTRIM(pa.client_name), ''), 'Unknown') = "+addArg(v))
	}
	if v := strings.TrimSpace(c.Query("device_name")); v != "" {
		filters = append(filters, "COALESCE(NULLIF(BTRIM(pa.device_name), ''), 'Unknown') = "+addArg(v))
	}
	if v := strings.TrimSpace(c.Query("client_ip")); v != "" {
		filters = append(filters, "pa.client_ip = "+addArg(v))
	}
	if len(filters) == 0 {
		return "", args, true
	}
	return "WHERE " + strings.Join(filters, " AND "), args, true
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
	sortCols := map[string]string{
		"last_seen":      "last_seen",
		"total_plays":    "total_plays",
		"total_duration": "total_duration",
		"client_count":   "client_count",
		"player_count":   "player_count",
		"ip_count":       "ip_count",
		"user_name":      "user_name",
	}
	sortCol, ok := sortCols[sortBy]
	if !ok {
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

	where, args, ok := statsUsageFilter(c, 1)
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

	havingParts := []string{"client_count >= " + strconv.Itoa(minClientCount), "player_count >= " + strconv.Itoa(minPlayerCount), "ip_count >= " + strconv.Itoa(minIPCount)}
	having := "WHERE " + strings.Join(havingParts, " AND ")

	limitArg := "$" + strconv.Itoa(len(args)+1)
	offsetArg := "$" + strconv.Itoa(len(args)+2)
	queryArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	orderBy := sortCol + " " + sortOrder + " NULLS LAST, user_name ASC, user_id ASC"
	outerOrderBy := "paged." + sortCol + " " + sortOrder + " NULLS LAST, paged.user_name ASC, paged.user_id ASC"
	if sortCol == "user_name" {
		orderBy = sortCol + " " + sortOrder + " NULLS LAST, user_id ASC"
		outerOrderBy = "paged." + sortCol + " " + sortOrder + " NULLS LAST, paged.user_id ASC"
	}

	query := `WITH filtered AS (
		SELECT pa.*, u.name AS user_name
		FROM playback_activity pa
		LEFT JOIN users u ON pa.user_id = u.id
		` + where + `
	),
	agg AS (
		SELECT
			user_id::text,
			COALESCE(user_name, 'Unknown') AS user_name,
			MAX(date_created) AS last_seen,
			COUNT(*)::bigint AS total_plays,
			COALESCE(SUM(play_duration), 0)::bigint AS total_duration,
			COUNT(DISTINCT COALESCE(NULLIF(BTRIM(device_name), ''), 'Unknown'))::bigint AS client_count,
			COUNT(DISTINCT COALESCE(NULLIF(BTRIM(client_name), ''), 'Unknown'))::bigint AS player_count,
			COUNT(DISTINCT NULLIF(client_ip, ''))::bigint AS ip_count,
			(
				ARRAY_AGG(item_name ORDER BY date_created DESC)
				FILTER (WHERE item_name IS NOT NULL AND item_name <> '')
			)[1] AS last_item_name,
			(
				ARRAY_AGG(client_name ORDER BY date_created DESC)
				FILTER (WHERE client_name IS NOT NULL AND client_name <> '')
			)[1] AS last_client_name,
			(
				ARRAY_AGG(device_name ORDER BY date_created DESC)
				FILTER (WHERE device_name IS NOT NULL AND device_name <> '')
			)[1] AS last_device_name,
			(
				ARRAY_AGG(client_ip ORDER BY date_created DESC)
				FILTER (WHERE client_ip IS NOT NULL AND client_ip <> '')
			)[1] AS last_client_ip,
			(
				ARRAY_AGG(user_agent ORDER BY date_created DESC)
				FILTER (WHERE user_agent IS NOT NULL AND BTRIM(user_agent) <> '')
			)[1] AS last_user_agent
		FROM filtered
		GROUP BY user_id, user_name
	),
	ranked AS (
		SELECT * FROM agg ` + having + `
	),
	top_clients AS (
		SELECT user_id::text, jsonb_agg(jsonb_build_object('label', label, 'count', count) ORDER BY count DESC, label ASC) AS items
		FROM (
			SELECT user_id, COALESCE(NULLIF(BTRIM(device_name), ''), 'Unknown') AS label, COUNT(*)::bigint AS count,
				ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY COUNT(*) DESC, COALESCE(NULLIF(BTRIM(device_name), ''), 'Unknown') ASC) AS rn
			FROM filtered
			GROUP BY user_id, COALESCE(NULLIF(BTRIM(device_name), ''), 'Unknown')
		) x
		WHERE rn <= 5
		GROUP BY user_id
	),
	top_players AS (
		SELECT user_id::text, jsonb_agg(jsonb_build_object('label', label, 'count', count) ORDER BY count DESC, label ASC) AS items
		FROM (
			SELECT user_id, COALESCE(NULLIF(BTRIM(client_name), ''), 'Unknown') AS label, COUNT(*)::bigint AS count,
				ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY COUNT(*) DESC, COALESCE(NULLIF(BTRIM(client_name), ''), 'Unknown') ASC) AS rn
			FROM filtered
			GROUP BY user_id, COALESCE(NULLIF(BTRIM(client_name), ''), 'Unknown')
		) x
		WHERE rn <= 5
		GROUP BY user_id
	),
	top_ips AS (
		SELECT user_id::text, jsonb_agg(jsonb_build_object('label', label, 'count', count) ORDER BY count DESC, label ASC) AS items
		FROM (
			SELECT user_id, client_ip AS label, COUNT(*)::bigint AS count,
				ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY COUNT(*) DESC, client_ip ASC) AS rn
			FROM filtered
			WHERE client_ip IS NOT NULL AND client_ip <> ''
			GROUP BY user_id, client_ip
		) x
		WHERE rn <= 5
		GROUP BY user_id
	),
	top_user_agents AS (
		SELECT user_id::text, jsonb_agg(jsonb_build_object('label', label, 'count', count) ORDER BY count DESC, label ASC) AS items
		FROM (
			SELECT user_id, user_agent AS label, COUNT(*)::bigint AS count,
				ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY COUNT(*) DESC, user_agent ASC) AS rn
			FROM filtered
			WHERE user_agent IS NOT NULL AND BTRIM(user_agent) <> ''
			GROUP BY user_id, user_agent
		) x
		WHERE rn <= 5
		GROUP BY user_id
	),
	paged AS (
		SELECT * FROM ranked
		ORDER BY ` + orderBy + `
		LIMIT ` + limitArg + ` OFFSET ` + offsetArg + `
	)
	SELECT
		paged.user_id,
		paged.user_name,
		paged.last_seen,
		paged.total_plays,
		paged.total_duration,
		paged.client_count,
		paged.player_count,
		paged.ip_count,
		paged.last_item_name,
		paged.last_client_name,
		paged.last_device_name,
		paged.last_client_ip,
		paged.last_user_agent,
		COALESCE(top_clients.items, '[]'::jsonb)::text,
		COALESCE(top_players.items, '[]'::jsonb)::text,
		COALESCE(top_ips.items, '[]'::jsonb)::text,
		COALESCE(top_user_agents.items, '[]'::jsonb)::text,
		(SELECT COUNT(*)::bigint FROM ranked) AS total
	FROM paged
	LEFT JOIN top_clients ON top_clients.user_id = paged.user_id
	LEFT JOIN top_players ON top_players.user_id = paged.user_id
	LEFT JOIN top_ips ON top_ips.user_id = paged.user_id
	LEFT JOIN top_user_agents ON top_user_agents.user_id = paged.user_id
	ORDER BY ` + outerOrderBy

	rows, err := state.DB.Query(c.Request.Context(), query, queryArgs...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	var total int64
	var items []gin.H
	for rows.Next() {
		var (
			userID, userName                     string
			lastSeen                             *time.Time
			totalPlays, totalDuration            int64
			clientCount, playerCount, ipCount    int64
			lastItem, lastClient, lastDevice, ip *string
			lastUserAgent                        *string
			topClients, topPlayers, topIPs       string
			topUserAgents                        string
			rowTotal                             int64
		)
		if err := rows.Scan(
			&userID, &userName, &lastSeen, &totalPlays, &totalDuration,
			&clientCount, &playerCount, &ipCount, &lastItem, &lastClient,
			&lastDevice, &ip, &lastUserAgent, &topClients, &topPlayers, &topIPs,
			&topUserAgents, &rowTotal,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		total = rowTotal
		entry := gin.H{
			"user_id":          userID,
			"user_name":        userName,
			"total_plays":      totalPlays,
			"total_duration":   totalDuration,
			"client_count":     clientCount,
			"player_count":     playerCount,
			"ip_count":         ipCount,
			"last_item_name":   ptrStrOr(lastItem, ""),
			"last_client_name": ptrStrOr(lastClient, ""),
			"last_device_name": ptrStrOr(lastDevice, ""),
			"last_client_ip":   ptrStrOr(ip, ""),
			"last_user_agent":  ptrStrOr(lastUserAgent, ""),
			"top_clients":      statsUsageBuckets(topClients),
			"top_players":      statsUsageBuckets(topPlayers),
			"top_ips":          statsUsageBuckets(topIPs),
			"top_user_agents":  statsUsageBuckets(topUserAgents),
		}
		if lastSeen != nil {
			entry["last_seen"] = lastSeen.UTC().Format(time.RFC3339)
		}
		items = append(items, entry)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if items == nil {
		items = []gin.H{}
	}
	summaryQuery := `WITH filtered AS (
			SELECT pa.*, u.name AS user_name
			FROM playback_activity pa
			LEFT JOIN users u ON pa.user_id = u.id
			` + where + `
		),
		agg AS (
			SELECT
				user_id,
				COUNT(*)::bigint AS total_plays,
				COALESCE(SUM(play_duration), 0)::bigint AS total_duration,
				COUNT(DISTINCT COALESCE(NULLIF(BTRIM(device_name), ''), 'Unknown'))::bigint AS client_count,
				COUNT(DISTINCT COALESCE(NULLIF(BTRIM(client_name), ''), 'Unknown'))::bigint AS player_count,
				COUNT(DISTINCT NULLIF(client_ip, ''))::bigint AS ip_count
			FROM filtered
			GROUP BY user_id, user_name
		),
		ranked AS (
			SELECT * FROM agg ` + having + `
		)
		SELECT
			COUNT(*)::bigint,
			COALESCE(SUM(total_plays), 0)::bigint,
			COALESCE(SUM(total_duration), 0)::bigint,
			COALESCE(SUM(client_count), 0)::bigint,
			COALESCE(SUM(player_count), 0)::bigint,
			COALESCE(SUM(ip_count), 0)::bigint
		FROM ranked`
	var summaryUsers, summaryPlays, summaryDuration, summaryClients, summaryPlayers, summaryIPs int64
	if err := state.DB.QueryRow(c.Request.Context(), summaryQuery, args...).Scan(
		&summaryUsers,
		&summaryPlays,
		&summaryDuration,
		&summaryClients,
		&summaryPlayers,
		&summaryIPs,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if total == 0 {
		total = summaryUsers
	}
	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"summary": gin.H{
			"active_users":   summaryUsers,
			"total_plays":    summaryPlays,
			"total_duration": summaryDuration,
			"client_count":   summaryClients,
			"player_count":   summaryPlayers,
			"ip_count":       summaryIPs,
		},
	})
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
		groupCol = "COALESCE(NULLIF(BTRIM(pa.client_name), ''), 'Unknown')"
		labelCol = groupCol
	case "DeviceName":
		groupCol = "COALESCE(NULLIF(BTRIM(pa.device_name), ''), 'Unknown')"
		labelCol = groupCol
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
