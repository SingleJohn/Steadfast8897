package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

func (s *Store) LoadConfig(ctx context.Context) (*GatewayConfig, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT value FROM gateway_config WHERE key = 'main'`).Scan(&raw)
	if err != nil {
		return DefaultGatewayConfig(), nil
	}
	cfg := DefaultGatewayConfig()
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("unmarshal gateway config: %w", err)
	}
	return cfg, nil
}

func (s *Store) SaveConfig(ctx context.Context, cfg *GatewayConfig) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal gateway config: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO gateway_config (key, value, updated_at) VALUES ('main', $1, NOW())
		ON CONFLICT (key) DO UPDATE SET value = $1, updated_at = NOW()
	`, raw)
	return err
}

// Request log operations

type RequestLog struct {
	ID               int64     `json:"id"`
	CreatedAt        time.Time `json:"created_at"`
	Tag              string    `json:"tag"`
	SourceID         string    `json:"source_id"`
	ClientIP         string    `json:"client_ip"`
	Method           string    `json:"method"`
	Path             string    `json:"path"`
	Query            string    `json:"query"`
	Status           int       `json:"status"`
	LatencyMs        int64     `json:"latency_ms"`
	BytesIn          int64     `json:"bytes_in"`
	BytesOut         int64     `json:"bytes_out"`
	EmbyUserID       string    `json:"emby_user_id"`
	EmbyUserName     string    `json:"emby_user_name"`
	RedirectBackend  string    `json:"redirect_backend"`
	RedirectSource   string    `json:"redirect_source"`
	RedirectLocation string    `json:"redirect_location"`
	RedirectTrace    string    `json:"redirect_trace"`
	ObjectKey        string    `json:"object_key"`
	RouteID          string    `json:"route_id"`
	PoolID           string    `json:"pool_id"`
	UserAgent        string    `json:"user_agent"`
	Referer          string    `json:"referer"`
	Headers          string    `json:"headers"`
}

func (s *Store) InsertRequestLogs(ctx context.Context, logs []RequestLog) error {
	if len(logs) == 0 {
		return nil
	}
	cols := []string{
		"tag", "source_id", "client_ip", "method", "path", "query",
		"status", "latency_ms", "bytes_in", "bytes_out",
		"emby_user_id", "emby_user_name", "redirect_backend", "redirect_source",
		"redirect_location", "redirect_trace", "object_key", "route_id", "pool_id",
		"user_agent", "referer", "headers",
	}
	_, err := s.pool.CopyFrom(ctx,
		pgx.Identifier{"gateway_request_logs"},
		cols,
		pgx.CopyFromSlice(len(logs), func(i int) ([]any, error) {
			l := logs[i]
			return []any{
				l.Tag, l.SourceID, l.ClientIP, l.Method, l.Path, l.Query,
				l.Status, l.LatencyMs, l.BytesIn, l.BytesOut,
				l.EmbyUserID, l.EmbyUserName, l.RedirectBackend, l.RedirectSource,
				l.RedirectLocation, l.RedirectTrace, l.ObjectKey, l.RouteID, l.PoolID,
				l.UserAgent, l.Referer, l.Headers,
			}, nil
		}),
	)
	return err
}

type LogQueryParams struct {
	Tag      string `json:"tag"`
	SourceID string `json:"source_id"`
	Status   int    `json:"status"`
	Limit    int    `json:"limit"`
	Offset   int    `json:"offset"`
}

func (s *Store) QueryRequestLogs(ctx context.Context, p LogQueryParams) ([]RequestLog, int64, error) {
	if p.Limit <= 0 {
		p.Limit = 50
	}
	where := "WHERE 1=1"
	args := []any{}
	n := 1
	if p.Tag != "" {
		where += fmt.Sprintf(" AND tag = $%d", n)
		args = append(args, p.Tag)
		n++
	}
	if p.SourceID != "" {
		where += fmt.Sprintf(" AND source_id = $%d", n)
		args = append(args, p.SourceID)
		n++
	}
	if p.Status > 0 {
		where += fmt.Sprintf(" AND status = $%d", n)
		args = append(args, p.Status)
		n++
	}

	var total int64
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM gateway_request_logs "+where, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	query := fmt.Sprintf("SELECT id,created_at,tag,source_id,client_ip,method,path,query,status,latency_ms,bytes_in,bytes_out,emby_user_id,emby_user_name,redirect_backend,redirect_source,redirect_location,redirect_trace,object_key,route_id,pool_id,user_agent,referer,headers FROM gateway_request_logs %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d", where, n, n+1)
	args = append(args, p.Limit, p.Offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var results []RequestLog
	for rows.Next() {
		var l RequestLog
		err := rows.Scan(&l.ID, &l.CreatedAt, &l.Tag, &l.SourceID, &l.ClientIP, &l.Method, &l.Path, &l.Query,
			&l.Status, &l.LatencyMs, &l.BytesIn, &l.BytesOut, &l.EmbyUserID, &l.EmbyUserName,
			&l.RedirectBackend, &l.RedirectSource, &l.RedirectLocation, &l.RedirectTrace,
			&l.ObjectKey, &l.RouteID, &l.PoolID, &l.UserAgent, &l.Referer, &l.Headers)
		if err != nil {
			return nil, 0, err
		}
		results = append(results, l)
	}
	return results, total, nil
}

// Daily stats operations

type DailyStat struct {
	ID            int64     `json:"id"`
	Day           time.Time `json:"day"`
	Tag           string    `json:"tag"`
	SourceID      string    `json:"source_id"`
	Requests      int64     `json:"requests"`
	Redirects302  int64     `json:"redirects302"`
	Status4xx     int64     `json:"status4xx"`
	Status5xx     int64     `json:"status5xx"`
	BytesIn       int64     `json:"bytes_in"`
	BytesOut      int64     `json:"bytes_out"`
	LatencyMsSum  int64     `json:"latency_ms_sum"`
	LatencyMsMax  int64     `json:"latency_ms_max"`
	LatencyMsMin  int64     `json:"latency_ms_min"`
	LastUpdatedAt time.Time `json:"last_updated_at"`
}

func (s *Store) UpsertDailyStat(ctx context.Context, stat DailyStat) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO gateway_daily_stats (day, tag, source_id, requests, redirects302, status4xx, status5xx, bytes_in, bytes_out, latency_ms_sum, latency_ms_max, latency_ms_min, last_updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NOW())
		ON CONFLICT (day, tag, source_id) DO UPDATE SET
			requests = gateway_daily_stats.requests + EXCLUDED.requests,
			redirects302 = gateway_daily_stats.redirects302 + EXCLUDED.redirects302,
			status4xx = gateway_daily_stats.status4xx + EXCLUDED.status4xx,
			status5xx = gateway_daily_stats.status5xx + EXCLUDED.status5xx,
			bytes_in = gateway_daily_stats.bytes_in + EXCLUDED.bytes_in,
			bytes_out = gateway_daily_stats.bytes_out + EXCLUDED.bytes_out,
			latency_ms_sum = gateway_daily_stats.latency_ms_sum + EXCLUDED.latency_ms_sum,
			latency_ms_max = GREATEST(gateway_daily_stats.latency_ms_max, EXCLUDED.latency_ms_max),
			latency_ms_min = LEAST(gateway_daily_stats.latency_ms_min, EXCLUDED.latency_ms_min),
			last_updated_at = NOW()
	`, stat.Day, stat.Tag, stat.SourceID, stat.Requests, stat.Redirects302, stat.Status4xx, stat.Status5xx, stat.BytesIn, stat.BytesOut, stat.LatencyMsSum, stat.LatencyMsMax, stat.LatencyMsMin)
	return err
}

func (s *Store) QueryDailyStats(ctx context.Context, sourceID string, days int) ([]DailyStat, error) {
	if days <= 0 {
		days = 30
	}
	query := `SELECT id,day,tag,source_id,requests,redirects302,status4xx,status5xx,bytes_in,bytes_out,latency_ms_sum,latency_ms_max,latency_ms_min,last_updated_at
		FROM gateway_daily_stats WHERE day >= NOW() - INTERVAL '1 day' * $1`
	args := []any{days}
	if sourceID != "" {
		query += " AND source_id = $2"
		args = append(args, sourceID)
	}
	query += " ORDER BY day ASC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []DailyStat
	for rows.Next() {
		var d DailyStat
		err := rows.Scan(&d.ID, &d.Day, &d.Tag, &d.SourceID, &d.Requests, &d.Redirects302,
			&d.Status4xx, &d.Status5xx, &d.BytesIn, &d.BytesOut, &d.LatencyMsSum,
			&d.LatencyMsMax, &d.LatencyMsMin, &d.LastUpdatedAt)
		if err != nil {
			return nil, err
		}
		results = append(results, d)
	}
	return results, nil
}

// Redirect-specific queries

type RedirectSummary struct {
	Total     int64            `json:"total"`
	ByBackend map[string]int64 `json:"by_backend"`
	TopUsers  []TopEntry       `json:"top_users"`
	TopIPs    []TopEntry       `json:"top_ips"`
}

type TopEntry struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

func (s *Store) GetRedirectSummary(ctx context.Context, sourceID string, hours int) (*RedirectSummary, error) {
	if hours <= 0 {
		hours = 24
	}
	interval := fmt.Sprintf("%d hours", hours)

	summary := &RedirectSummary{ByBackend: map[string]int64{}}

	// Total
	baseWhere := fmt.Sprintf("WHERE tag = 'proxy' AND status = 302 AND redirect_backend <> '' AND created_at >= NOW() - INTERVAL '%s'", interval)
	if sourceID != "" {
		baseWhere += fmt.Sprintf(" AND source_id = '%s'", sourceID)
	}

	err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM gateway_request_logs "+baseWhere).Scan(&summary.Total)
	if err != nil {
		return nil, err
	}

	// By backend
	rows, err := s.pool.Query(ctx, fmt.Sprintf("SELECT redirect_backend, COUNT(*) FROM gateway_request_logs %s GROUP BY redirect_backend ORDER BY COUNT(*) DESC", baseWhere))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var c int64
		if err := rows.Scan(&k, &c); err != nil {
			return nil, err
		}
		summary.ByBackend[k] = c
	}

	// Top users
	rows2, err := s.pool.Query(ctx, fmt.Sprintf("SELECT COALESCE(NULLIF(emby_user_name,''), emby_user_id), COUNT(*) FROM gateway_request_logs %s AND emby_user_id <> '' GROUP BY 1 ORDER BY 2 DESC LIMIT 10", baseWhere))
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var e TopEntry
		if err := rows2.Scan(&e.Key, &e.Count); err != nil {
			return nil, err
		}
		summary.TopUsers = append(summary.TopUsers, e)
	}

	// Top IPs
	rows3, err := s.pool.Query(ctx, fmt.Sprintf("SELECT client_ip, COUNT(*) FROM gateway_request_logs %s GROUP BY client_ip ORDER BY 2 DESC LIMIT 10", baseWhere))
	if err != nil {
		return nil, err
	}
	defer rows3.Close()
	for rows3.Next() {
		var e TopEntry
		if err := rows3.Scan(&e.Key, &e.Count); err != nil {
			return nil, err
		}
		summary.TopIPs = append(summary.TopIPs, e)
	}

	return summary, nil
}

type IPStatsSummaryParams struct {
	Tag      string
	Mode     string
	SourceID string
	Since    *time.Time
	Until    *time.Time
	Limit    int
	Scope    string
}

type IPStatsTopIP struct {
	ClientIP string `json:"client_ip"`
	Count    int64  `json:"count"`
	Country  string `json:"country"`
	Prov     string `json:"prov"`
	City     string `json:"city"`
	Area     string `json:"area"`
	BigArea  string `json:"big_area"`
	ISP      string `json:"isp"`
	IPType   string `json:"ip_type"`
}

type IPStatsCountryBucket struct {
	Country string  `json:"country"`
	Count   int64   `json:"count"`
	Percent float64 `json:"percent"`
}

type IPStatsProvBucket struct {
	Country string  `json:"country"`
	Prov    string  `json:"prov"`
	Count   int64   `json:"count"`
	Percent float64 `json:"percent"`
}

type IPStatsCityBucket struct {
	Country string  `json:"country"`
	Prov    string  `json:"prov"`
	City    string  `json:"city"`
	Count   int64   `json:"count"`
	Percent float64 `json:"percent"`
}

type IPStatsAreaBucket struct {
	Country string  `json:"country"`
	Prov    string  `json:"prov"`
	City    string  `json:"city"`
	Area    string  `json:"area"`
	Count   int64   `json:"count"`
	Percent float64 `json:"percent"`
}

type IPStatsBigAreaBucket struct {
	BigArea string  `json:"big_area"`
	Count   int64   `json:"count"`
	Percent float64 `json:"percent"`
}

type IPStatsISPBucket struct {
	ISP     string  `json:"isp"`
	Count   int64   `json:"count"`
	Percent float64 `json:"percent"`
}

type IPStatsIPTypeBucket struct {
	IPType  string  `json:"ip_type"`
	Count   int64   `json:"count"`
	Percent float64 `json:"percent"`
}

type IPStatsSummary struct {
	Scope         string                 `json:"scope,omitempty"`
	Source        string                 `json:"source,omitempty"`
	Tag           string                 `json:"tag"`
	Mode          string                 `json:"mode"`
	SourceID      string                 `json:"source_id"`
	Since         string                 `json:"since"`
	Until         string                 `json:"until"`
	Total         int64                  `json:"total"`
	PendingEnrich int64                  `json:"pending_enrich"`
	TopIPs        []IPStatsTopIP         `json:"top_ips"`
	ByCountry     []IPStatsCountryBucket `json:"by_country"`
	ByProv        []IPStatsProvBucket    `json:"by_prov"`
	ByCity        []IPStatsCityBucket    `json:"by_city"`
	ByArea        []IPStatsAreaBucket    `json:"by_area"`
	ByBigArea     []IPStatsBigAreaBucket `json:"by_big_area"`
	ByISP         []IPStatsISPBucket     `json:"by_isp"`
	ByIPType      []IPStatsIPTypeBucket  `json:"by_ip_type"`
}

func (s *Store) GetIPStatsSummary(ctx context.Context, p IPStatsSummaryParams) (*IPStatsSummary, error) {
	tag := strings.TrimSpace(p.Tag)
	if tag == "" {
		tag = "proxy"
	}
	mode := strings.TrimSpace(p.Mode)
	if mode == "" {
		mode = "all"
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}

	whereParts := []string{"WHERE 1=1"}
	args := make([]any, 0, 6)
	argPos := 1
	addCond := func(cond string, value any) {
		whereParts = append(whereParts, fmt.Sprintf("AND %s $%d", cond, argPos))
		args = append(args, value)
		argPos++
	}

	if tag != "" {
		addCond("tag =", tag)
	}
	if mode == "redirect302" {
		addCond("status =", 302)
	}
	if p.SourceID != "" {
		addCond("source_id =", p.SourceID)
	}
	if p.Since != nil {
		addCond("created_at >=", *p.Since)
	}
	if p.Until != nil {
		addCond("created_at <=", *p.Until)
	}

	where := strings.Join(whereParts, " ")

	summary := &IPStatsSummary{
		Scope:         p.Scope,
		Tag:           tag,
		Mode:          mode,
		SourceID:      p.SourceID,
		Since:         formatOptionalTime(p.Since),
		Until:         formatOptionalTime(p.Until),
		PendingEnrich: 0,
		TopIPs:        []IPStatsTopIP{},
		ByCountry:     []IPStatsCountryBucket{},
		ByProv:        []IPStatsProvBucket{},
		ByCity:        []IPStatsCityBucket{},
		ByArea:        []IPStatsAreaBucket{},
		ByBigArea:     []IPStatsBigAreaBucket{},
		ByISP:         []IPStatsISPBucket{},
		ByIPType:      []IPStatsIPTypeBucket{},
	}

	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM gateway_request_logs "+where, args...).Scan(&summary.Total); err != nil {
		return nil, err
	}

	topQuery := fmt.Sprintf(`
		SELECT client_ip, COUNT(*)
		FROM gateway_request_logs
		%s AND client_ip <> ''
		GROUP BY client_ip
		ORDER BY COUNT(*) DESC, client_ip ASC
		LIMIT $%d
	`, where, argPos)
	topArgs := append(append([]any{}, args...), limit)
	rows, err := s.pool.Query(ctx, topQuery, topArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var ip string
		var count int64
		if err := rows.Scan(&ip, &count); err != nil {
			return nil, err
		}
		summary.TopIPs = append(summary.TopIPs, IPStatsTopIP{
			ClientIP: ip,
			Count:    count,
			Country:  "未知",
			Prov:     "未知",
			City:     "未知",
			Area:     "未知",
			BigArea:  "未知",
			ISP:      "未知",
			IPType:   "未知",
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if summary.Total > 0 {
		percent := 100.0
		total := summary.Total
		summary.ByCountry = append(summary.ByCountry, IPStatsCountryBucket{Country: "未知", Count: total, Percent: percent})
		summary.ByProv = append(summary.ByProv, IPStatsProvBucket{Country: "未知", Prov: "未知", Count: total, Percent: percent})
		summary.ByCity = append(summary.ByCity, IPStatsCityBucket{Country: "未知", Prov: "未知", City: "未知", Count: total, Percent: percent})
		summary.ByArea = append(summary.ByArea, IPStatsAreaBucket{Country: "未知", Prov: "未知", City: "未知", Area: "未知", Count: total, Percent: percent})
		summary.ByBigArea = append(summary.ByBigArea, IPStatsBigAreaBucket{BigArea: "未知", Count: total, Percent: percent})
		summary.ByISP = append(summary.ByISP, IPStatsISPBucket{ISP: "未知", Count: total, Percent: percent})
		summary.ByIPType = append(summary.ByIPType, IPStatsIPTypeBucket{IPType: "未知", Count: total, Percent: percent})
	}

	return summary, nil
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// Cleanup old data

func (s *Store) CleanupOldLogs(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		retentionDays = 7
	}
	_, err := s.pool.Exec(ctx, fmt.Sprintf("DELETE FROM gateway_request_logs WHERE created_at < NOW() - INTERVAL '%d days'", retentionDays))
	return err
}

func (s *Store) CleanupOldStats(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		retentionDays = 90
	}
	_, err := s.pool.Exec(ctx, fmt.Sprintf("DELETE FROM gateway_daily_stats WHERE day < NOW() - INTERVAL '%d days'", retentionDays))
	return err
}
