package repository

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type StatsRepository struct {
	pool *pgxpool.Pool
}

type PlaybackActivityCreate struct {
	UserID      string
	ItemID      string
	ItemType    string
	ItemName    string
	PlayMethod  string
	ClientName  string
	DeviceName  string
	DurationSec int64
	ClientIP    string
	SeriesName  *string
	UserAgent   string
}

type StatsUserActivityRow struct {
	UserID        string
	UserName      *string
	LastSeen      *time.Time
	ItemName      *string
	ClientName    *string
	TotalPlays    int64
	TotalDuration int64
}

type StatsDailyActivityRow struct {
	Day           time.Time
	Count         int64
	TotalDuration int64
}

type StatsHourlyRow struct {
	DayOfWeek int
	Hour      int
	Count     int64
}

type StatsBreakdownRow struct {
	Label         string
	Count         int64
	TotalDuration int64
}

type StatsRecentPlaybackRow struct {
	DateCreated  time.Time
	ItemName     *string
	ItemType     *string
	SeriesName   *string
	ClientName   *string
	DeviceName   *string
	ClientIP     *string
	PlayDuration *int32
	UserName     *string
}

type StatsUsageFilter struct {
	Days           int
	User           string
	ClientName     string
	DeviceName     string
	ClientIP       string
	MinClientCount int
	MinPlayerCount int
	MinIPCount     int
	SortBy         string
	SortOrder      string
	Page           int
	PageSize       int
}

type StatsUsageRow struct {
	UserID        string
	UserName      string
	LastSeen      *time.Time
	TotalPlays    int64
	TotalDuration int64
	ClientCount   int64
	PlayerCount   int64
	IPCount       int64
	LastItem      *string
	LastClient    *string
	LastDevice    *string
	LastClientIP  *string
	LastUserAgent *string
	TopClients    string
	TopPlayers    string
	TopIPs        string
	TopUserAgents string
	TotalRows     int64
}

type StatsUsageSummary struct {
	Users       int64
	Plays       int64
	Duration    int64
	ClientCount int64
	PlayerCount int64
	IPCount     int64
}

func NewStatsRepository(pool *pgxpool.Pool) *StatsRepository {
	return &StatsRepository{pool: pool}
}

func (r *StatsRepository) InsertPlaybackActivity(ctx context.Context, row PlaybackActivityCreate) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO playback_activity (user_id, item_id, item_type, item_name, play_method, client_name, device_name, play_duration, client_ip, series_name, user_agent)
		 VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		row.UserID, row.ItemID, &row.ItemType, &row.ItemName, row.PlayMethod, row.ClientName, row.DeviceName, int(row.DurationSec), row.ClientIP, row.SeriesName, row.UserAgent,
	)
	return err
}

func (r *StatsRepository) UserActivity(ctx context.Context, days int) ([]StatsUserActivityRow, error) {
	rows, err := r.pool.Query(ctx,
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
		return nil, err
	}
	defer rows.Close()
	var out []StatsUserActivityRow
	for rows.Next() {
		var row StatsUserActivityRow
		if err := rows.Scan(&row.UserID, &row.UserName, &row.LastSeen, &row.ItemName, &row.ClientName, &row.TotalPlays, &row.TotalDuration); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *StatsRepository) DailyActivity(ctx context.Context, days int) ([]StatsDailyActivityRow, error) {
	since := time.Now().UTC().AddDate(0, 0, -days)
	rows, err := r.pool.Query(ctx,
		`SELECT date_created::date AS day, COUNT(*)::bigint,
			COALESCE(SUM(play_duration), 0)::bigint AS total_duration
		 FROM playback_activity
		 WHERE date_created >= $1
		 GROUP BY 1
		 ORDER BY 1`,
		since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatsDailyActivityRow
	for rows.Next() {
		var row StatsDailyActivityRow
		if err := rows.Scan(&row.Day, &row.Count, &row.TotalDuration); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *StatsRepository) HourlyReport(ctx context.Context, days int) ([]StatsHourlyRow, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT EXTRACT(DOW FROM date_created)::int AS day_of_week,
			EXTRACT(HOUR FROM date_created)::int AS hour,
			COUNT(*)::bigint
		 FROM playback_activity
		 WHERE date_created >= NOW() - INTERVAL '1 day' * $1
		 GROUP BY day_of_week, hour
		 ORDER BY day_of_week, hour`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatsHourlyRow
	for rows.Next() {
		var row StatsHourlyRow
		if err := rows.Scan(&row.DayOfWeek, &row.Hour, &row.Count); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *StatsRepository) BreakdownReport(ctx context.Context, days int, reportType string) ([]StatsBreakdownRow, error) {
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
	rows, err := r.pool.Query(ctx, sql, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatsBreakdownRow
	for rows.Next() {
		var row StatsBreakdownRow
		if err := rows.Scan(&row.Label, &row.Count, &row.TotalDuration); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *StatsRepository) RecentPlayback(ctx context.Context, limit int32) ([]StatsRecentPlaybackRow, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT pa.date_created, pa.item_name, pa.item_type, pa.series_name,
			pa.client_name, pa.device_name, pa.client_ip, pa.play_duration,
			u.name AS user_name
		 FROM playback_activity pa
		 LEFT JOIN users u ON pa.user_id = u.id
		 ORDER BY pa.date_created DESC
		 LIMIT $1`,
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatsRecentPlaybackRow
	for rows.Next() {
		var row StatsRecentPlaybackRow
		if err := rows.Scan(&row.DateCreated, &row.ItemName, &row.ItemType, &row.SeriesName, &row.ClientName, &row.DeviceName, &row.ClientIP, &row.PlayDuration, &row.UserName); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *StatsRepository) UserUsageRanking(ctx context.Context, filter StatsUsageFilter) ([]StatsUsageRow, StatsUsageSummary, error) {
	where, args := statsUsageWhere(filter, 1)
	sortCol, ok := statsUsageSortColumn(filter.SortBy)
	if !ok {
		return nil, StatsUsageSummary{}, fmt.Errorf("invalid sort_by")
	}
	sortOrder := strings.ToUpper(strings.TrimSpace(filter.SortOrder))
	if sortOrder != "ASC" {
		sortOrder = "DESC"
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	havingParts := []string{
		"client_count >= " + strconv.Itoa(filter.MinClientCount),
		"player_count >= " + strconv.Itoa(filter.MinPlayerCount),
		"ip_count >= " + strconv.Itoa(filter.MinIPCount),
	}
	having := "WHERE " + strings.Join(havingParts, " AND ")
	limitArg := "$" + strconv.Itoa(len(args)+1)
	offsetArg := "$" + strconv.Itoa(len(args)+2)
	queryArgs := append(append([]any{}, args...), filter.PageSize, (filter.Page-1)*filter.PageSize)
	orderBy := sortCol + " " + sortOrder + " NULLS LAST, user_name ASC, user_id ASC"
	outerOrderBy := "paged." + sortCol + " " + sortOrder + " NULLS LAST, paged.user_name ASC, paged.user_id ASC"
	if sortCol == "user_name" {
		orderBy = sortCol + " " + sortOrder + " NULLS LAST, user_id ASC"
		outerOrderBy = "paged." + sortCol + " " + sortOrder + " NULLS LAST, paged.user_id ASC"
	}
	query := statsUsageRankingSQL(where, having, orderBy, outerOrderBy, limitArg, offsetArg)
	rows, err := r.pool.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, StatsUsageSummary{}, err
	}
	defer rows.Close()
	var items []StatsUsageRow
	for rows.Next() {
		var row StatsUsageRow
		if err := rows.Scan(
			&row.UserID, &row.UserName, &row.LastSeen, &row.TotalPlays, &row.TotalDuration,
			&row.ClientCount, &row.PlayerCount, &row.IPCount, &row.LastItem, &row.LastClient,
			&row.LastDevice, &row.LastClientIP, &row.LastUserAgent, &row.TopClients, &row.TopPlayers, &row.TopIPs,
			&row.TopUserAgents, &row.TotalRows,
		); err != nil {
			return nil, StatsUsageSummary{}, err
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, StatsUsageSummary{}, err
	}
	summaryQuery := statsUsageSummarySQL(where, having)
	var summary StatsUsageSummary
	if err := r.pool.QueryRow(ctx, summaryQuery, args...).Scan(
		&summary.Users,
		&summary.Plays,
		&summary.Duration,
		&summary.ClientCount,
		&summary.PlayerCount,
		&summary.IPCount,
	); err != nil {
		return nil, StatsUsageSummary{}, err
	}
	return items, summary, nil
}

func statsUsageWhere(filter StatsUsageFilter, startArg int) (string, []any) {
	var args []any
	var filters []string
	addArg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(startArg+len(args)-1)
	}
	if filter.Days > 0 {
		filters = append(filters, "pa.date_created >= NOW() - INTERVAL '1 day' * "+addArg(filter.Days))
	}
	if strings.TrimSpace(filter.User) != "" {
		filters = append(filters, "COALESCE(u.name, '') ILIKE "+addArg("%"+strings.TrimSpace(filter.User)+"%"))
	}
	if strings.TrimSpace(filter.ClientName) != "" {
		filters = append(filters, "COALESCE(NULLIF(BTRIM(pa.client_name), ''), 'Unknown') = "+addArg(strings.TrimSpace(filter.ClientName)))
	}
	if strings.TrimSpace(filter.DeviceName) != "" {
		filters = append(filters, "COALESCE(NULLIF(BTRIM(pa.device_name), ''), 'Unknown') = "+addArg(strings.TrimSpace(filter.DeviceName)))
	}
	if strings.TrimSpace(filter.ClientIP) != "" {
		filters = append(filters, "pa.client_ip = "+addArg(strings.TrimSpace(filter.ClientIP)))
	}
	if len(filters) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(filters, " AND "), args
}

func statsUsageSortColumn(sortBy string) (string, bool) {
	switch sortBy {
	case "last_seen", "total_plays", "total_duration", "client_count", "player_count", "ip_count", "user_name":
		return sortBy, true
	default:
		return "", false
	}
}

func statsUsageRankingSQL(where, having, orderBy, outerOrderBy, limitArg, offsetArg string) string {
	return `WITH filtered AS (
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
			(ARRAY_AGG(item_name ORDER BY date_created DESC) FILTER (WHERE item_name IS NOT NULL AND item_name <> ''))[1] AS last_item_name,
			(ARRAY_AGG(client_name ORDER BY date_created DESC) FILTER (WHERE client_name IS NOT NULL AND client_name <> ''))[1] AS last_client_name,
			(ARRAY_AGG(device_name ORDER BY date_created DESC) FILTER (WHERE device_name IS NOT NULL AND device_name <> ''))[1] AS last_device_name,
			(ARRAY_AGG(client_ip ORDER BY date_created DESC) FILTER (WHERE client_ip IS NOT NULL AND client_ip <> ''))[1] AS last_client_ip,
			(ARRAY_AGG(user_agent ORDER BY date_created DESC) FILTER (WHERE user_agent IS NOT NULL AND BTRIM(user_agent) <> ''))[1] AS last_user_agent
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
		) x WHERE rn <= 5 GROUP BY user_id
	),
	top_players AS (
		SELECT user_id::text, jsonb_agg(jsonb_build_object('label', label, 'count', count) ORDER BY count DESC, label ASC) AS items
		FROM (
			SELECT user_id, COALESCE(NULLIF(BTRIM(client_name), ''), 'Unknown') AS label, COUNT(*)::bigint AS count,
				ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY COUNT(*) DESC, COALESCE(NULLIF(BTRIM(client_name), ''), 'Unknown') ASC) AS rn
			FROM filtered
			GROUP BY user_id, COALESCE(NULLIF(BTRIM(client_name), ''), 'Unknown')
		) x WHERE rn <= 5 GROUP BY user_id
	),
	top_ips AS (
		SELECT user_id::text, jsonb_agg(jsonb_build_object('label', label, 'count', count) ORDER BY count DESC, label ASC) AS items
		FROM (
			SELECT user_id, client_ip AS label, COUNT(*)::bigint AS count,
				ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY COUNT(*) DESC, client_ip ASC) AS rn
			FROM filtered
			WHERE client_ip IS NOT NULL AND client_ip <> ''
			GROUP BY user_id, client_ip
		) x WHERE rn <= 5 GROUP BY user_id
	),
	top_user_agents AS (
		SELECT user_id::text, jsonb_agg(jsonb_build_object('label', label, 'count', count) ORDER BY count DESC, label ASC) AS items
		FROM (
			SELECT user_id, user_agent AS label, COUNT(*)::bigint AS count,
				ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY COUNT(*) DESC, user_agent ASC) AS rn
			FROM filtered
			WHERE user_agent IS NOT NULL AND BTRIM(user_agent) <> ''
			GROUP BY user_id, user_agent
		) x WHERE rn <= 5 GROUP BY user_id
	),
	paged AS (
		SELECT * FROM ranked
		ORDER BY ` + orderBy + `
		LIMIT ` + limitArg + ` OFFSET ` + offsetArg + `
	)
	SELECT paged.user_id, paged.user_name, paged.last_seen, paged.total_plays, paged.total_duration,
		paged.client_count, paged.player_count, paged.ip_count, paged.last_item_name, paged.last_client_name,
		paged.last_device_name, paged.last_client_ip, paged.last_user_agent,
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
}

func statsUsageSummarySQL(where, having string) string {
	return `WITH filtered AS (
			SELECT pa.*, u.name AS user_name
			FROM playback_activity pa
			LEFT JOIN users u ON pa.user_id = u.id
			` + where + `
		),
		agg AS (
			SELECT user_id, COUNT(*)::bigint AS total_plays,
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
		SELECT COUNT(*)::bigint, COALESCE(SUM(total_plays), 0)::bigint,
			COALESCE(SUM(total_duration), 0)::bigint,
			COALESCE(SUM(client_count), 0)::bigint,
			COALESCE(SUM(player_count), 0)::bigint,
			COALESCE(SUM(ip_count), 0)::bigint
		FROM ranked`
}
