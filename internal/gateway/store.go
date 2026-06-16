package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	dbgen "fyms/internal/db/gen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool    *pgxpool.Pool
	queries *dbgen.Queries
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, queries: dbgen.New(pool)}
}

func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

func (s *Store) LoadConfig(ctx context.Context) (*GatewayConfig, error) {
	raw, err := s.queries.GetGatewayConfig(ctx)
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
	return s.queries.UpsertGatewayConfig(ctx, raw)
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

	countParams := dbgen.CountGatewayRequestLogsParams{
		Column1:  p.Tag != "",
		Tag:      p.Tag,
		Column3:  p.SourceID != "",
		SourceID: p.SourceID,
		Column5:  p.Status > 0,
		Status:   int32(p.Status),
	}
	total, err := s.queries.CountGatewayRequestLogs(ctx, countParams)
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.queries.ListGatewayRequestLogs(ctx, dbgen.ListGatewayRequestLogsParams{
		Column1:  p.Tag != "",
		Tag:      p.Tag,
		Column3:  p.SourceID != "",
		SourceID: p.SourceID,
		Column5:  p.Status > 0,
		Status:   int32(p.Status),
		Limit:    int32(p.Limit),
		Offset:   int32(p.Offset),
	})
	if err != nil {
		return nil, 0, err
	}
	results := make([]RequestLog, 0, len(rows))
	for _, row := range rows {
		results = append(results, mapGatewayRequestLog(row))
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
	return s.queries.UpsertGatewayDailyStat(ctx, dbgen.UpsertGatewayDailyStatParams{
		Day:          pgtype.Date{Time: stat.Day, Valid: true},
		Tag:          stat.Tag,
		SourceID:     stat.SourceID,
		Requests:     stat.Requests,
		Redirects302: stat.Redirects302,
		Status4xx:    stat.Status4xx,
		Status5xx:    stat.Status5xx,
		BytesIn:      stat.BytesIn,
		BytesOut:     stat.BytesOut,
		LatencyMsSum: stat.LatencyMsSum,
		LatencyMsMax: stat.LatencyMsMax,
		LatencyMsMin: stat.LatencyMsMin,
	})
}

func (s *Store) QueryDailyStats(ctx context.Context, sourceID string, days int) ([]DailyStat, error) {
	if days <= 0 {
		days = 30
	}
	rows, err := s.queries.ListGatewayDailyStats(ctx, dbgen.ListGatewayDailyStatsParams{
		Column1:  int32(days),
		Column2:  sourceID != "",
		SourceID: sourceID,
	})
	if err != nil {
		return nil, err
	}
	results := make([]DailyStat, 0, len(rows))
	for _, row := range rows {
		results = append(results, mapGatewayDailyStat(row))
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
	summary := &RedirectSummary{ByBackend: map[string]int64{}}
	params := dbgen.CountGatewayRedirectsParams{
		Column1:  int32(hours),
		Column2:  sourceID != "",
		SourceID: sourceID,
	}

	var err error
	summary.Total, err = s.queries.CountGatewayRedirects(ctx, params)
	if err != nil {
		return nil, err
	}

	byBackend, err := s.queries.CountGatewayRedirectsByBackend(ctx, dbgen.CountGatewayRedirectsByBackendParams(params))
	if err != nil {
		return nil, err
	}
	for _, row := range byBackend {
		summary.ByBackend[row.RedirectBackend] = row.Count
	}

	topUsers, err := s.queries.ListGatewayRedirectTopUsers(ctx, dbgen.ListGatewayRedirectTopUsersParams(params))
	if err != nil {
		return nil, err
	}
	for _, row := range topUsers {
		summary.TopUsers = append(summary.TopUsers, TopEntry{Key: row.Key, Count: row.Count})
	}

	topIPs, err := s.queries.ListGatewayRedirectTopIPs(ctx, dbgen.ListGatewayRedirectTopIPsParams(params))
	if err != nil {
		return nil, err
	}
	for _, row := range topIPs {
		summary.TopIPs = append(summary.TopIPs, TopEntry{Key: row.Key, Count: row.Count})
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

	filter := newGatewaySQLFilter()
	filter.add("tag =", tag)
	if mode == "redirect302" {
		filter.add("status =", 302)
	}
	filter.addIfNotEmpty("source_id =", p.SourceID)
	filter.addIfTime("created_at >=", p.Since)
	filter.addIfTime("created_at <=", p.Until)
	where, args, argPos := filter.build()

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

func mapGatewayRequestLog(row dbgen.GatewayRequestLog) RequestLog {
	return RequestLog{
		ID:               row.ID,
		CreatedAt:        timeFromTimestamptz(row.CreatedAt),
		Tag:              row.Tag,
		SourceID:         row.SourceID,
		ClientIP:         row.ClientIp,
		Method:           row.Method,
		Path:             row.Path,
		Query:            row.Query,
		Status:           int(row.Status),
		LatencyMs:        row.LatencyMs,
		BytesIn:          row.BytesIn,
		BytesOut:         row.BytesOut,
		EmbyUserID:       row.EmbyUserID,
		EmbyUserName:     row.EmbyUserName,
		RedirectBackend:  row.RedirectBackend,
		RedirectSource:   row.RedirectSource,
		RedirectLocation: row.RedirectLocation,
		RedirectTrace:    row.RedirectTrace,
		ObjectKey:        row.ObjectKey,
		RouteID:          row.RouteID,
		PoolID:           row.PoolID,
		UserAgent:        row.UserAgent,
		Referer:          row.Referer,
		Headers:          row.Headers,
	}
}

func mapGatewayDailyStat(row dbgen.GatewayDailyStat) DailyStat {
	return DailyStat{
		ID:            row.ID,
		Day:           timeFromDate(row.Day),
		Tag:           row.Tag,
		SourceID:      row.SourceID,
		Requests:      row.Requests,
		Redirects302:  row.Redirects302,
		Status4xx:     row.Status4xx,
		Status5xx:     row.Status5xx,
		BytesIn:       row.BytesIn,
		BytesOut:      row.BytesOut,
		LatencyMsSum:  row.LatencyMsSum,
		LatencyMsMax:  row.LatencyMsMax,
		LatencyMsMin:  row.LatencyMsMin,
		LastUpdatedAt: timeFromTimestamptz(row.LastUpdatedAt),
	}
}

func timeFromDate(v pgtype.Date) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return v.Time
}

func timeFromTimestamptz(v pgtype.Timestamptz) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return v.Time
}

type gatewaySQLFilter struct {
	parts []string
	args  []any
}

func newGatewaySQLFilter() *gatewaySQLFilter {
	return &gatewaySQLFilter{parts: []string{"WHERE 1=1"}, args: make([]any, 0, 6)}
}

func (f *gatewaySQLFilter) add(cond string, value any) {
	f.args = append(f.args, value)
	f.parts = append(f.parts, fmt.Sprintf("AND %s $%d", cond, len(f.args)))
}

func (f *gatewaySQLFilter) addIfNotEmpty(cond, value string) {
	if value != "" {
		f.add(cond, value)
	}
}

func (f *gatewaySQLFilter) addIfTime(cond string, value *time.Time) {
	if value != nil {
		f.add(cond, *value)
	}
}

func (f *gatewaySQLFilter) build() (string, []any, int) {
	return strings.Join(f.parts, " "), f.args, len(f.args) + 1
}

// Cleanup old data

func (s *Store) CleanupOldLogs(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		retentionDays = 7
	}
	return s.queries.CleanupOldGatewayLogs(ctx, int32(retentionDays))
}

func (s *Store) CleanupOldStats(ctx context.Context, retentionDays int) error {
	if retentionDays <= 0 {
		retentionDays = 90
	}
	return s.queries.CleanupOldGatewayStats(ctx, int32(retentionDays))
}
