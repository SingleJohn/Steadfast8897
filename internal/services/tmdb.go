package services

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"golang.org/x/time/rate"

	"fyms/internal/models"
	"fyms/internal/services/scraper"
)

// sharedScrapeCache 在 main.go 启动时通过 SetScrapeCache 注入；
// 用于 Matcher 的搜索结果缓存。未注入时 Matcher 自动退化为无缓存。
var sharedScrapeCache scraper.Cache

func SetScrapeCache(c scraper.Cache) {
	sharedScrapeCache = c
}

// sharedTmdbLimiter 是所有 TMDB 调用路径共享的 rate.Limiter,
// main.go 启动时通过 SetTmdbLimiter 注入。未注入时 tmdbGet 不限流
// (降级兼容,但生产应始终注入)。
var sharedTmdbLimiter *rate.Limiter

func SetTmdbLimiter(l *rate.Limiter) {
	sharedTmdbLimiter = l
}

const (
	TMDB_BASE       = "https://api.themoviedb.org/3"
	TMDB_IMAGE_BASE = "https://image.tmdb.org/t/p"
)

type TmdbClient struct {
	httpClient *http.Client
	apiKeys    []string
	keyIndex   atomic.Uint64
	language   string
}

type scrapeSaveTargets struct {
	PosterPath   string
	BackdropPath string
	NfoPath      string
}

type externalIDRecord struct {
	Provider string
	Value    string
}

type identifyCandidateRecord struct {
	ID         string                 `json:"id"`
	ItemID     string                 `json:"item_id"`
	Provider   string                 `json:"provider"`
	ExternalID string                 `json:"external_id"`
	Title      string                 `json:"title"`
	Year       *int32                 `json:"year,omitempty"`
	PosterURL  string                 `json:"poster_url"`
	Score      float64                `json:"score"`
	Payload    map[string]interface{} `json:"payload,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
}

type identifyFailureDetail struct {
	Stage                  string                           `json:"stage"`
	Reason                 string                           `json:"reason"`
	Threshold              float64                          `json:"threshold"`
	AutoApply              bool                             `json:"auto_apply"`
	AdultFilterEnabled     bool                             `json:"adult_filter_enabled"`
	Providers              []string                         `json:"providers,omitempty"`
	Parsed                 identifyFailureParsed            `json:"parsed"`
	Matched                *identifyFailureMatched          `json:"matched,omitempty"`
	SearchAttempts         []identifyFailureSearchAttempt   `json:"search_attempts,omitempty"`
	CandidatesTotal        int                              `json:"candidates_total"`
	BlockedCandidatesTotal int                              `json:"blocked_candidates_total,omitempty"`
	BestScore              *float64                         `json:"best_score,omitempty"`
	Candidates             []identifyFailureCandidateRecord `json:"candidates,omitempty"`
	BlockedCandidates      []identifyFailureCandidateRecord `json:"blocked_candidates,omitempty"`
}

type identifyFailureParsed struct {
	Title         string            `json:"title,omitempty"`
	OriginalTitle string            `json:"original_title,omitempty"`
	Year          *int32            `json:"year,omitempty"`
	IDs           map[string]string `json:"ids,omitempty"`
	MediaHint     string            `json:"media_hint,omitempty"`
	Junk          []string          `json:"junk,omitempty"`
}

type identifyFailureSearchAttempt struct {
	Source string `json:"source"`
	Query  string `json:"query"`
	Year   *int32 `json:"year,omitempty"`
}

type identifyFailureMatched struct {
	Provider    string            `json:"provider"`
	ProviderID  string            `json:"provider_id"`
	Source      string            `json:"source,omitempty"`
	Score       float64           `json:"score,omitempty"`
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
}

type identifyFailureCandidateRecord struct {
	Provider       string            `json:"provider"`
	ProviderID     string            `json:"provider_id"`
	Title          string            `json:"title"`
	OriginalTitle  string            `json:"original_title,omitempty"`
	Year           *int32            `json:"year,omitempty"`
	Score          float64           `json:"score"`
	Popularity     float64           `json:"popularity,omitempty"`
	Source         string            `json:"source,omitempty"`
	ExternalIDs    map[string]string `json:"external_ids,omitempty"`
	PosterURL      string            `json:"poster_url,omitempty"`
	Blocked        bool              `json:"blocked,omitempty"`
	AdultReasons   []string          `json:"adult_reasons,omitempty"`
	Certifications []string          `json:"certifications,omitempty"`
}

func TmdbClientFromConfig(ctx context.Context, pool *pgxpool.Pool) *TmdbClient {
	var rawKey *string
	err := pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'tmdb_api_key'").Scan(&rawKey)
	if err != nil || rawKey == nil || *rawKey == "" {
		return nil
	}

	var apiKeys []string
	for _, k := range strings.Split(*rawKey, ",") {
		k = strings.TrimSpace(k)
		if k != "" {
			apiKeys = append(apiKeys, k)
		}
	}
	if len(apiKeys) == 0 {
		return nil
	}

	slog.Info("[TMDB] Loaded API key(s)", "count", len(apiKeys))

	language := "zh-CN"
	var langVal *string
	if err := pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'tmdb_language'").Scan(&langVal); err == nil && langVal != nil && *langVal != "" {
		language = *langVal
	}

	var proxyURL *string
	_ = pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'tmdb_proxy'").Scan(&proxyURL)

	transport := &http.Transport{
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	if proxyURL != nil {
		rawProxy := strings.TrimSpace(*proxyURL)
		if rawProxy != "" {
			if u, err := url.Parse(rawProxy); err == nil && u.Scheme != "" && u.Host != "" {
				slog.Info("[TMDB] Using proxy", "proxy", redactProxyURL(u))
				transport.Proxy = http.ProxyURL(u)
			} else {
				slog.Warn("[TMDB] Invalid proxy URL, ignoring", "proxy", rawProxy, "error", err)
			}
		} else {
			slog.Info("[TMDB] Proxy not configured")
		}
	} else {
		slog.Info("[TMDB] Proxy not configured")
	}

	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
	}

	return &TmdbClient{
		httpClient: client,
		apiKeys:    apiKeys,
		language:   language,
	}
}

// sanitizeTmdbURL 把 api_key=XXX 替换成 api_key=***,避免日志泄漏。
func sanitizeTmdbURL(u string) string {
	const key = "api_key="
	idx := strings.Index(u, key)
	if idx < 0 {
		return u
	}
	tail := u[idx+len(key):]
	end := strings.IndexAny(tail, "&")
	if end < 0 {
		return u[:idx] + key + "***"
	}
	return u[:idx] + key + "***" + tail[end:]
}

func redactProxyURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	clean := *u
	if clean.User != nil {
		username := clean.User.Username()
		if username != "" {
			clean.User = url.UserPassword(username, "******")
		} else {
			clean.User = url.User("******")
		}
	}
	return clean.String()
}

func getScrapeSaveMode(ctx context.Context, pool *pgxpool.Pool) string {
	mode := "database"
	var val *string
	if err := pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'scrape_save_mode'").Scan(&val); err == nil && val != nil {
		switch strings.TrimSpace(*val) {
		case "database", "media_dir", "both":
			mode = strings.TrimSpace(*val)
		}
	}
	return mode
}

func resolveScrapeSaveTargets(ctx context.Context, pool *pgxpool.Pool, itemID, itemType string) scrapeSaveTargets {
	targets := scrapeSaveTargets{}

	switch itemType {
	case "Movie":
		var filePath *string
		if err := pool.QueryRow(ctx, "SELECT file_path FROM items WHERE id = $1::uuid", itemID).Scan(&filePath); err == nil && filePath != nil && *filePath != "" {
			if !strings.HasPrefix(strings.ToLower(*filePath), "http") {
				dir := filepath.Dir(*filePath)
				targets.PosterPath = filepath.Join(dir, "poster.jpg")
				targets.BackdropPath = filepath.Join(dir, "fanart.jpg")
				targets.NfoPath = filepath.Join(dir, "movie.nfo")
			}
		}
	case "Series":
		var episodePath *string
		if err := pool.QueryRow(ctx,
			"SELECT file_path FROM items WHERE series_id = $1::uuid AND type = 'Episode' AND file_path IS NOT NULL AND file_path NOT LIKE 'http%' ORDER BY created_at ASC LIMIT 1",
			itemID,
		).Scan(&episodePath); err == nil && episodePath != nil && *episodePath != "" {
			showDir := filepath.Dir(filepath.Dir(*episodePath))
			targets.PosterPath = filepath.Join(showDir, "poster.jpg")
			targets.BackdropPath = filepath.Join(showDir, "fanart.jpg")
			targets.NfoPath = filepath.Join(showDir, "tvshow.nfo")
		}
	}

	if targets.PosterPath != "" {
		slog.Debug("[TMDB] Resolved media save targets", "item_id", itemID, "poster", targets.PosterPath, "backdrop", targets.BackdropPath, "nfo", targets.NfoPath)
	} else {
		slog.Debug("[TMDB] No media directory target resolved, will use data/metadata/", "item_id", itemID, "type", itemType)
	}

	return targets
}

func resolveSeasonPosterMediaPath(ctx context.Context, pool *pgxpool.Pool, seasonID string) string {
	var episodePath *string
	if err := pool.QueryRow(ctx,
		"SELECT file_path FROM items WHERE parent_id = $1::uuid AND type = 'Episode' AND file_path IS NOT NULL ORDER BY created_at ASC LIMIT 1",
		seasonID,
	).Scan(&episodePath); err != nil || episodePath == nil || *episodePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(*episodePath), "poster.jpg")
}

// resolveEpisodeThumbMediaPath 返回 Episode 对应媒体目录内的 thumb 路径,形如
// `<视频同目录>/<视频 basename>-thumb.jpg`。这是 Emby/Jellyfin 的 thumb 命名约定,
// 也是 scanner 端 FindEpisodeThumbCached 首要识别的 pattern。
// file_path 为 http URL 或空时返回空串,调用方回退到 data/metadata。
func resolveEpisodeThumbMediaPath(ctx context.Context, pool *pgxpool.Pool, episodeID string) string {
	var filePath *string
	if err := pool.QueryRow(ctx,
		"SELECT file_path FROM items WHERE id = $1::uuid AND type = 'Episode'",
		episodeID,
	).Scan(&filePath); err != nil || filePath == nil || *filePath == "" {
		return ""
	}
	p := *filePath
	if strings.HasPrefix(strings.ToLower(p), "http") {
		return ""
	}
	stem := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
	if stem == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(p), stem+"-thumb.jpg")
}

func writeNfoFile(path string, itemType string, nfo *NfoData) bool {
	if path == "" || nfo == nil {
		return false
	}
	root := "movie"
	if itemType != "Movie" {
		root = "tvshow"
	}

	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString("<" + root + ">\n")
	writeNfoTag := func(name string, value *string) {
		if value == nil || *value == "" {
			return
		}
		b.WriteString("  <" + name + ">")
		xml.EscapeText(&b, []byte(*value))
		b.WriteString("</" + name + ">\n")
	}
	writeNfoTag("title", nfo.Title)
	writeNfoTag("originaltitle", nfo.OriginalTitle)
	writeNfoTag("plot", nfo.Plot)
	writeNfoTag("premiered", nfo.Premiered)
	writeNfoTag("imdbid", nfo.ImdbID)
	writeNfoTag("tagline", nfo.Tagline)

	if nfo.Year != nil {
		fmt.Fprintf(&b, "  <year>%d</year>\n", *nfo.Year)
	}
	if nfo.Rating != nil {
		fmt.Fprintf(&b, "  <rating>%.1f</rating>\n", *nfo.Rating)
	}
	if nfo.TmdbID != nil {
		fmt.Fprintf(&b, "  <tmdbid>%d</tmdbid>\n", *nfo.TmdbID)
	}
	if nfo.TvdbID != nil {
		fmt.Fprintf(&b, "  <tvdbid>%d</tvdbid>\n", *nfo.TvdbID)
	}
	for _, genre := range nfo.Genres {
		g := strings.TrimSpace(genre)
		if g == "" {
			continue
		}
		b.WriteString("  <genre>")
		xml.EscapeText(&b, []byte(g))
		b.WriteString("</genre>\n")
	}
	for _, director := range nfo.Directors {
		d := strings.TrimSpace(director)
		if d == "" {
			continue
		}
		b.WriteString("  <director>")
		xml.EscapeText(&b, []byte(d))
		b.WriteString("</director>\n")
	}
	for _, actor := range nfo.Actors {
		if strings.TrimSpace(actor.Name) == "" {
			continue
		}
		b.WriteString("  <actor>\n")
		b.WriteString("    <name>")
		xml.EscapeText(&b, []byte(actor.Name))
		b.WriteString("</name>\n")
		if strings.TrimSpace(actor.Role) != "" {
			b.WriteString("    <role>")
			xml.EscapeText(&b, []byte(actor.Role))
			b.WriteString("</role>\n")
		}
		if actor.TmdbID != nil {
			fmt.Fprintf(&b, "    <tmdbid>%d</tmdbid>\n", *actor.TmdbID)
		}
		b.WriteString("    <type>Actor</type>\n")
		b.WriteString("  </actor>\n")
	}
	b.WriteString("</" + root + ">\n")

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false
	}
	return os.WriteFile(path, []byte(b.String()), 0644) == nil
}

func (c *TmdbClient) cloneWithLanguage(lang string) *TmdbClient {
	return &TmdbClient{
		httpClient: c.httpClient,
		apiKeys:    c.apiKeys,
		language:   lang,
	}
}

func (c *TmdbClient) nextKey() string {
	idx := c.keyIndex.Add(1) - 1
	return c.apiKeys[idx%uint64(len(c.apiKeys))]
}

// tmdbRequestCount 统计 tmdbGet 的总调用数(Phase 4 metrics 观测用)。
var tmdbRequestCount atomic.Int64

// TmdbRequestCount 返回 tmdbGet 的累计调用次数。
func TmdbRequestCount() int64 { return tmdbRequestCount.Load() }

func (c *TmdbClient) tmdbGet(ctx context.Context, urlTemplate string) (map[string]interface{}, error) {
	tmdbRequestCount.Add(1)
	if sharedTmdbLimiter != nil {
		if err := sharedTmdbLimiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter wait: %w", err)
		}
	}
	maxRetries := len(c.apiKeys)
	for attempt := 0; attempt <= maxRetries; attempt++ {
		key := c.nextKey()
		reqURL := strings.ReplaceAll(urlTemplate, "{API_KEY}", key)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		// 部分代理/CDN WAF 会黑名单默认的 Go-http-client UA,显式带上浏览器 UA 避过
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; fyms/1.0; +https://github.com/ffoocn/fyms)")
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			DiagFrom(ctx).Record(reqURL, 0, nil, false)
			// 完整错误类型 + 字符串,诊断 "Access denied" / proxy / DNS 等异常必备
			slog.Warn("[TMDB] Request error", "error", err, "error_type", fmt.Sprintf("%T", err), "url", sanitizeTmdbURL(reqURL))
			return nil, err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			DiagFrom(ctx).Record(reqURL, resp.StatusCode, nil, false)
			return nil, fmt.Errorf("read body: %w", err)
		}

		diag := DiagFrom(ctx)

		if resp.StatusCode == http.StatusTooManyRequests {
			diag.Record(reqURL, resp.StatusCode, body, false)
			if attempt < maxRetries {
				suffix := key
				if len(suffix) > 6 {
					suffix = suffix[len(suffix)-6:]
				}
				slog.Debug("[TMDB] 429 rate limited, rotating to next key", "key_suffix", suffix)
				time.Sleep(500 * time.Millisecond)
				continue
			}
			slog.Warn("[TMDB] All keys rate limited", "count", len(c.apiKeys))
			return nil, fmt.Errorf("all %d API keys rate limited", len(c.apiKeys))
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			diag.Record(reqURL, resp.StatusCode, body, false)
			slog.Debug("[TMDB] HTTP error", "status", resp.StatusCode)
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err != nil {
			diag.Record(reqURL, resp.StatusCode, body, false)
			return nil, fmt.Errorf("json decode: %w", err)
		}
		diag.Record(reqURL, resp.StatusCode, body, true)
		return result, nil
	}
	return nil, fmt.Errorf("exhausted retries")
}

func (c *TmdbClient) SearchMovie(ctx context.Context, name string, year *int32) (map[string]interface{}, error) {
	u := fmt.Sprintf("%s/search/movie?api_key={API_KEY}&language=%s&query=%s",
		TMDB_BASE, c.language, url.QueryEscape(name))
	if year != nil {
		u += fmt.Sprintf("&year=%d", *year)
	}
	data, err := c.tmdbGet(ctx, u)
	if err != nil {
		return nil, err
	}
	results, ok := data["results"].([]interface{})
	if !ok || len(results) == 0 {
		return nil, fmt.Errorf("no results")
	}
	first, ok := results[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid result format")
	}
	return first, nil
}

func (c *TmdbClient) SearchTV(ctx context.Context, name string) (map[string]interface{}, error) {
	u := fmt.Sprintf("%s/search/tv?api_key={API_KEY}&language=%s&query=%s",
		TMDB_BASE, c.language, url.QueryEscape(name))
	data, err := c.tmdbGet(ctx, u)
	if err != nil {
		return nil, err
	}
	results, ok := data["results"].([]interface{})
	if !ok || len(results) == 0 {
		return nil, fmt.Errorf("no results")
	}
	first, ok := results[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid result format")
	}
	return first, nil
}

// SearchMovieMulti returns up to 20 TMDB movie search results.
// 带 year 过滤 0 结果时自动 fallback 去掉 year 重试一次 —— 常见场景:
//   - item 之前被错误识别,production_year 被污染,用户自定义搜索时 year 预填错值
//   - 文件名里的年份是再发行年/目录年,不是 TMDB 的首映年
//
// Matcher 的 scoreCandidate 会用 parsed.Year 给候选打分,年份不一致的候选分数低,
// 所以放宽 year 过滤不会降低识别准确度,只会提高召回。
func (c *TmdbClient) SearchMovieMulti(ctx context.Context, name string, year *int32) ([]map[string]interface{}, error) {
	out, err := c.searchMovieOnce(ctx, name, year)
	if err != nil {
		return nil, err
	}
	if len(out) > 0 {
		return out, nil
	}
	if year != nil {
		slog.Debug("[TMDB] search with year returned 0, retrying without year",
			"query", name, "year", *year)
		out, err = c.searchMovieOnce(ctx, name, nil)
		if err != nil {
			return nil, err
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return nil, fmt.Errorf("未找到结果")
}

func (c *TmdbClient) searchMovieOnce(ctx context.Context, name string, year *int32) ([]map[string]interface{}, error) {
	u := fmt.Sprintf("%s/search/movie?api_key={API_KEY}&language=%s&query=%s&include_adult=false",
		TMDB_BASE, c.language, url.QueryEscape(name))
	if year != nil {
		u += fmt.Sprintf("&year=%d", *year)
	}
	data, err := c.tmdbGet(ctx, u)
	if err != nil {
		return nil, err
	}
	results, ok := data["results"].([]interface{})
	if !ok || len(results) == 0 {
		return nil, nil
	}
	var out []map[string]interface{}
	for _, r := range results {
		if m, ok := r.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// SearchTVMulti returns up to 20 TMDB TV search results.
func (c *TmdbClient) SearchTVMulti(ctx context.Context, name string) ([]map[string]interface{}, error) {
	u := fmt.Sprintf("%s/search/tv?api_key={API_KEY}&language=%s&query=%s&include_adult=false",
		TMDB_BASE, c.language, url.QueryEscape(name))
	data, err := c.tmdbGet(ctx, u)
	if err != nil {
		return nil, err
	}
	results, ok := data["results"].([]interface{})
	if !ok || len(results) == 0 {
		return nil, fmt.Errorf("未找到结果")
	}
	var out []map[string]interface{}
	for _, r := range results {
		if m, ok := r.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (c *TmdbClient) GetMovieDetails(ctx context.Context, tmdbID int64) (map[string]interface{}, error) {
	u := fmt.Sprintf("%s/movie/%d?api_key={API_KEY}&language=%s&append_to_response=credits,release_dates",
		TMDB_BASE, tmdbID, c.language)
	return c.tmdbGet(ctx, u)
}

func (c *TmdbClient) GetTVDetails(ctx context.Context, tmdbID int64) (map[string]interface{}, error) {
	u := fmt.Sprintf("%s/tv/%d?api_key={API_KEY}&language=%s&append_to_response=credits,content_ratings",
		TMDB_BASE, tmdbID, c.language)
	return c.tmdbGet(ctx, u)
}

func (c *TmdbClient) GetSeasonImages(ctx context.Context, tmdbID int64, seasonNum int32) *string {
	u := fmt.Sprintf("%s/tv/%d/season/%d?api_key={API_KEY}&language=%s",
		TMDB_BASE, tmdbID, seasonNum, c.language)
	data, err := c.tmdbGet(ctx, u)
	if err != nil {
		return nil
	}
	if pp, ok := data["poster_path"].(string); ok && pp != "" {
		return &pp
	}
	return nil
}

func (c *TmdbClient) DownloadImage(ctx context.Context, imgPath, savePath, size string) bool {
	imgURL := fmt.Sprintf("%s/%s%s", TMDB_IMAGE_BASE, size, imgPath)
	return c.downloadImageURL(ctx, imgURL, savePath)
}

func (c *TmdbClient) downloadImageURL(ctx context.Context, imgURL, savePath string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imgURL, nil)
	if err != nil {
		return false
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	dir := filepath.Dir(savePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false
	}

	return os.WriteFile(savePath, data, 0644) == nil
}

// ScrapeItem scrapes TMDB metadata for a single item, creating its own TmdbClient from config.
func ScrapeItem(ctx context.Context, pool *pgxpool.Pool, itemID string) (map[string]interface{}, error) {
	client := TmdbClientFromConfig(ctx, pool)
	if client == nil {
		return nil, fmt.Errorf("TMDB API key not configured")
	}
	return ScrapeItemWithClient(ctx, pool, itemID, client)
}

type scrapeItemMeta struct {
	ItemType    string
	Name        string
	Year        *int32
	TmdbID      *int32
	ImdbID      *string
	FilePath    *string
	LibraryID   string // 用于 per-library 刮削配置
	ExternalIDs map[string]string
}

func loadScrapeItemMeta(ctx context.Context, pool *pgxpool.Pool, itemID string) (*scrapeItemMeta, error) {
	meta := &scrapeItemMeta{ExternalIDs: map[string]string{}}
	var providerIDsRaw []byte
	err := pool.QueryRow(ctx,
		"SELECT type, name, production_year, tmdb_id, imdb_id, file_path, library_id::text, provider_ids FROM items WHERE id = $1::uuid", itemID,
	).Scan(&meta.ItemType, &meta.Name, &meta.Year, &meta.TmdbID, &meta.ImdbID, &meta.FilePath, &meta.LibraryID, &providerIDsRaw)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("item not found")
	}
	if err != nil {
		return nil, fmt.Errorf("query item: %w", err)
	}
	mergeProviderIDs(meta.ExternalIDs, providerIDsRaw)

	rows, err := pool.Query(ctx,
		"SELECT provider, external_id FROM item_external_ids WHERE item_id = $1::uuid",
		itemID)
	if err != nil {
		return nil, fmt.Errorf("query item external ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var provider, externalID string
		if err := rows.Scan(&provider, &externalID); err != nil {
			return nil, fmt.Errorf("scan item external ids: %w", err)
		}
		provider = strings.ToLower(strings.TrimSpace(provider))
		externalID = strings.TrimSpace(externalID)
		if provider != "" && externalID != "" {
			meta.ExternalIDs[provider] = externalID
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate item external ids: %w", err)
	}
	return meta, nil
}

// tmdbSetIdentifyAttempted 记录"尝试识别过一次"(不区分成功/失败)。
// Phase 5 前这里还会同时设置 identify_cooldown_until 做整块冷却,现在冷却语义
// 由 scrape_queue.next_run_at + 指数退避接管,attempted_at 仅作诊断/审计。
func tmdbSetIdentifyAttempted(ctx context.Context, pool *pgxpool.Pool, itemID string) {
	_, err := pool.Exec(ctx,
		"UPDATE items SET identify_attempted_at = NOW() WHERE id = $1::uuid",
		itemID)
	if err != nil {
		slog.Debug("[TMDB] set identify_attempted_at failed", "item_id", itemID, "error", err)
	}
}

// buildParsedName 从 item meta + 文件路径构造 ParsedName，给 Matcher 使用。
func buildParsedName(meta *scrapeItemMeta) scraper.ParsedName {
	mode := scraper.ModeMovie
	if meta.ItemType == "Series" {
		mode = scraper.ModeSeries
	}

	candidates := collectParsedNameCandidates(meta, mode)
	parsed := pickPrimaryParsedCandidate(candidates, meta.ItemType)
	parsed.SearchSeeds = buildSearchSeeds(candidates, parsed)

	// DB 侧的 year 最可信，覆盖解析结果
	if meta.Year != nil && *meta.Year > 0 {
		parsed.Year = meta.Year
		for i := range parsed.SearchSeeds {
			parsed.SearchSeeds[i].Year = meta.Year
		}
	}
	if parsed.IDs == nil {
		parsed.IDs = map[string]string{}
	}
	for _, cand := range candidates {
		for kind, id := range cand.Parsed.IDs {
			if strings.TrimSpace(parsed.IDs[kind]) == "" && strings.TrimSpace(id) != "" {
				parsed.IDs[kind] = strings.TrimSpace(id)
			}
		}
	}
	if meta.TmdbID != nil && *meta.TmdbID > 0 && parsed.IDs["tmdb"] == "" {
		parsed.IDs["tmdb"] = strconv.Itoa(int(*meta.TmdbID))
	}
	if meta.ImdbID != nil && strings.TrimSpace(*meta.ImdbID) != "" && parsed.IDs["imdb"] == "" {
		parsed.IDs["imdb"] = strings.TrimSpace(*meta.ImdbID)
	}
	for kind, id := range meta.ExternalIDs {
		if strings.TrimSpace(parsed.IDs[kind]) == "" && strings.TrimSpace(id) != "" {
			parsed.IDs[kind] = strings.TrimSpace(id)
		}
	}
	// Title 兜底：若归一化后 Title/OriginalTitle 都为空，用 items.name
	if parsed.Title == "" && parsed.OriginalTitle == "" {
		parsed.Title = meta.Name
	}
	return parsed
}

type parsedNameCandidate struct {
	Source string
	Raw    string
	Parsed scraper.ParsedName
}

func collectParsedNameCandidates(meta *scrapeItemMeta, mode scraper.ParseMode) []parsedNameCandidate {
	candidates := make([]parsedNameCandidate, 0, 4)
	seen := map[string]struct{}{}
	add := func(source, raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		key := strings.ToLower(raw)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		candidates = append(candidates, parsedNameCandidate{
			Source: source,
			Raw:    raw,
			Parsed: scraper.Parse(raw, mode),
		})
	}

	add("item_name", meta.Name)
	if meta.FilePath != nil && strings.TrimSpace(*meta.FilePath) != "" {
		fp := strings.TrimSpace(*meta.FilePath)
		add("file_basename", filepath.Base(fp))
		add("parent_folder", filepath.Base(filepath.Dir(fp)))
		add("grandparent_folder", filepath.Base(filepath.Dir(filepath.Dir(fp))))
	}
	return candidates
}

func pickPrimaryParsedCandidate(candidates []parsedNameCandidate, itemType string) scraper.ParsedName {
	if len(candidates) == 0 {
		return scraper.ParsedName{IDs: make(map[string]string)}
	}
	best := candidates[0]
	bestScore := scoreParsedNameCandidate(best, itemType)
	for _, cand := range candidates[1:] {
		if score := scoreParsedNameCandidate(cand, itemType); score > bestScore {
			best = cand
			bestScore = score
		}
	}
	parsed := best.Parsed
	if parsed.IDs == nil {
		parsed.IDs = make(map[string]string)
	}
	return parsed
}

func scoreParsedNameCandidate(c parsedNameCandidate, itemType string) int {
	score := 0
	if len(c.Parsed.IDs) > 0 {
		score += 100
	}
	if c.Parsed.Year != nil {
		score += 30
	}
	if title := primaryParsedTitle(c.Parsed); title != "" {
		score += 20
		if !scraper.IsWeakTitle(title) {
			score += 25
		} else {
			score -= 40
		}
	}
	if c.Parsed.Title != "" && c.Parsed.OriginalTitle != "" {
		score += 10
	}
	if c.Parsed.Season != nil || c.Parsed.Episode != nil {
		score -= 20
	}
	switch c.Source {
	case "item_name":
		score += 18
	case "file_basename":
		score += 12
	case "parent_folder":
		score += 16
	case "grandparent_folder":
		score += 8
	}
	if itemType == "Movie" && c.Source == "parent_folder" {
		score += 10
	}
	if itemType == "Series" && c.Source == "file_basename" {
		score += 12
	}
	return score
}

func primaryParsedTitle(p scraper.ParsedName) string {
	if s := strings.TrimSpace(p.Title); s != "" {
		return s
	}
	return strings.TrimSpace(p.OriginalTitle)
}

func buildSearchSeeds(candidates []parsedNameCandidate, primary scraper.ParsedName) []scraper.SearchSeed {
	if len(candidates) == 0 {
		return nil
	}
	out := make([]scraper.SearchSeed, 0, len(candidates))
	seen := map[string]struct{}{}
	add := func(c parsedNameCandidate) {
		seed := scraper.SearchSeed{
			Source:        c.Source,
			Title:         strings.TrimSpace(c.Parsed.Title),
			OriginalTitle: strings.TrimSpace(c.Parsed.OriginalTitle),
			Year:          c.Parsed.Year,
		}
		title := primaryParsedTitle(c.Parsed)
		seed.Weak = title == "" || scraper.IsWeakTitle(title)
		key := strings.ToLower(seed.Source + "|" + seed.Title + "|" + seed.OriginalTitle)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, seed)
	}

	primaryKey := strings.ToLower(primary.Title + "|" + primary.OriginalTitle)
	for _, cand := range candidates {
		if strings.ToLower(cand.Parsed.Title+"|"+cand.Parsed.OriginalTitle) == primaryKey {
			add(cand)
			break
		}
	}
	for _, cand := range candidates {
		add(cand)
	}
	return out
}

func mergeProviderIDs(dst map[string]string, raw []byte) {
	if len(raw) == 0 || dst == nil {
		return
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	for key, value := range payload {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		switch v := value.(type) {
		case string:
			if s := strings.TrimSpace(v); s != "" {
				dst[key] = s
			}
		case float64:
			if v > 0 {
				dst[key] = strconv.FormatInt(int64(v), 10)
			}
		}
	}
}

func upsertExternalIDs(ctx context.Context, pool *pgxpool.Pool, itemID string, ids []externalIDRecord) {
	if pool == nil || len(ids) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(ids))
	providerMap := make(map[string]string, len(ids))
	for _, rec := range ids {
		provider := strings.ToLower(strings.TrimSpace(rec.Provider))
		value := strings.TrimSpace(rec.Value)
		if provider == "" || value == "" {
			continue
		}
		if _, ok := seen[provider]; ok {
			continue
		}
		seen[provider] = struct{}{}
		providerMap[provider] = value
		_, err := pool.Exec(ctx,
			`INSERT INTO item_external_ids (item_id, provider, external_id, updated_at)
			 VALUES ($1::uuid, $2, $3, NOW())
			 ON CONFLICT (item_id, provider)
			 DO UPDATE SET external_id = EXCLUDED.external_id,
			               updated_at = EXCLUDED.updated_at`,
			itemID, provider, value)
		if err != nil {
			slog.Warn("[Scraper] upsert item_external_ids failed", "item_id", itemID, "provider", provider, "error", err)
			continue
		}
	}
	if len(providerMap) == 0 {
		return
	}
	if raw, err := json.Marshal(providerMap); err == nil {
		_, err = pool.Exec(ctx,
			"UPDATE items SET provider_ids = $1::jsonb, updated_at = NOW() WHERE id = $2::uuid",
			string(raw), itemID)
		if err != nil {
			slog.Warn("[Scraper] update provider_ids failed", "item_id", itemID, "error", err)
		}
	}
}

func replaceIdentifyCandidates(ctx context.Context, pool *pgxpool.Pool, itemID string, candidates []scraper.ScoredCandidate) error {
	if _, err := pool.Exec(ctx, "DELETE FROM identify_candidates WHERE item_id = $1::uuid", itemID); err != nil {
		return err
	}
	for _, cand := range candidates {
		payload, _ := json.Marshal(map[string]interface{}{
			"provider":       cand.Provider,
			"provider_id":    cand.ProviderID,
			"external_ids":   cand.ExternalIDs,
			"original_title": cand.OriginalTitle,
			"source":         cand.Source,
			"popularity":     cand.Popularity,
			"poster_url":     cand.PosterURL,
			"adult_content":  cand.AdultContent,
			"adult_reasons":  cand.AdultReasons,
			"certifications": cand.Certifications,
		})
		var year interface{}
		if cand.Year != nil {
			year = *cand.Year
		}
		_, err := pool.Exec(ctx,
			`INSERT INTO identify_candidates (item_id, provider, external_id, title, year, poster_url, score, payload)
			 VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
			itemID,
			cand.Provider,
			cand.ProviderID,
			cand.Title,
			year,
			strings.TrimSpace(cand.PosterURL),
			float32(cand.Score),
			string(payload),
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func ListIdentifyCandidates(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]identifyCandidateRecord, error) {
	rows, err := pool.Query(ctx,
		`SELECT id::text, item_id::text, provider, external_id, COALESCE(title, ''), year, COALESCE(poster_url, ''), COALESCE(score, 0), payload, created_at
		   FROM identify_candidates
		  WHERE item_id = $1::uuid
		  ORDER BY score DESC, created_at DESC`,
		itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []identifyCandidateRecord
	for rows.Next() {
		var rec identifyCandidateRecord
		var payload []byte
		if err := rows.Scan(&rec.ID, &rec.ItemID, &rec.Provider, &rec.ExternalID, &rec.Title, &rec.Year, &rec.PosterURL, &rec.Score, &payload, &rec.CreatedAt); err != nil {
			return nil, err
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &rec.Payload)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func fetchTMDBDetailsByID(ctx context.Context, client *TmdbClient, itemType string, tmdbID int64) (map[string]interface{}, error) {
	switch itemType {
	case "Movie":
		return client.GetMovieDetails(ctx, tmdbID)
	case "Series":
		return client.GetTVDetails(ctx, tmdbID)
	default:
		return nil, fmt.Errorf("cannot scrape type: %s", itemType)
	}
}

// =========== scraper.Provider 实现 ===========

func (c *TmdbClient) Name() string { return "tmdb" }

// Priority 数字越小越优先。TMDB 作为基准源置为 1。
func (c *TmdbClient) Priority() int { return 1 }

func (c *TmdbClient) Supports(t scraper.MediaType) bool {
	return t == scraper.MediaMovie || t == scraper.MediaSeries
}

func (c *TmdbClient) Search(ctx context.Context, t scraper.MediaType, q scraper.Query) ([]scraper.Candidate, error) {
	query := q.Title
	if query == "" {
		query = q.OriginalTitle
	}
	if query == "" {
		return nil, nil
	}
	switch t {
	case scraper.MediaMovie:
		results, err := c.SearchMovieMulti(ctx, query, q.Year)
		if err != nil {
			if isNoResultsErr(err) {
				return nil, nil
			}
			return nil, err
		}
		return candidatesFromTMDB(results, "movie"), nil
	case scraper.MediaSeries:
		results, err := c.SearchTVMulti(ctx, query)
		if err != nil {
			if isNoResultsErr(err) {
				return nil, nil
			}
			return nil, err
		}
		return candidatesFromTMDB(results, "tv"), nil
	default:
		return nil, fmt.Errorf("unsupported media type: %s", t)
	}
}

// GetByID 返回统一的 Details 结构，供 Aggregator.Fill（M4）消费。
// 现阶段 applyTMDBDetails 仍直接吃 raw map；Details 路径并行存在，
// 等 M4 字段级合并落地后，raw 路径再逐步切换过去。
func (c *TmdbClient) GetByID(ctx context.Context, t scraper.MediaType, id string) (*scraper.Details, error) {
	tmdbID, err := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
	if err != nil || tmdbID <= 0 {
		return nil, fmt.Errorf("invalid tmdb id: %q", id)
	}
	raw, err := c.fetchRawByID(ctx, t, tmdbID)
	if err != nil {
		return nil, err
	}
	return tmdbDetailsFromRaw(raw, t, tmdbID), nil
}

func (c *TmdbClient) fetchRawByID(ctx context.Context, t scraper.MediaType, id int64) (map[string]interface{}, error) {
	switch t {
	case scraper.MediaMovie:
		return c.GetMovieDetails(ctx, id)
	case scraper.MediaSeries:
		return c.GetTVDetails(ctx, id)
	default:
		return nil, fmt.Errorf("unsupported media type: %s", t)
	}
}

func (c *TmdbClient) FindByExternalID(ctx context.Context, kind, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", nil
	}
	var source string
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "imdb":
		source = "imdb_id"
	case "tmdb":
		return id, nil
	case "tvdb":
		source = "tvdb_id"
	default:
		return "", nil
	}
	u := fmt.Sprintf("%s/find/%s?api_key={API_KEY}&language=%s&external_source=%s",
		TMDB_BASE, url.PathEscape(id), c.language, source)
	data, err := c.tmdbGet(ctx, u)
	if err != nil {
		return "", err
	}
	for _, key := range []string{"movie_results", "tv_results"} {
		arr, ok := data[key].([]interface{})
		if !ok || len(arr) == 0 {
			continue
		}
		if m, ok := arr[0].(map[string]interface{}); ok {
			if id, ok := jsonInt64(m, "id"); ok && id > 0 {
				return strconv.FormatInt(id, 10), nil
			}
		}
	}
	return "", nil
}

func isNoResultsErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "未找到结果") || strings.Contains(s, "no results")
}

// tmdbDetailsFromRaw 把 TMDB 详情 raw map 转为 scraper.Details。
// 提取策略与 applyTMDBDetails 保持一致，便于 M4 切换到 Details 流程时
// 行为不变；actors 上限 20 与现有逻辑一致。
func tmdbDetailsFromRaw(details map[string]interface{}, t scraper.MediaType, id int64) *scraper.Details {
	if details == nil {
		return nil
	}
	titleKey, origKey, dateKey := "title", "original_title", "release_date"
	if t == scraper.MediaSeries {
		titleKey, origKey, dateKey = "name", "original_name", "first_air_date"
	}

	d := &scraper.Details{
		Provider:       "tmdb",
		ProviderID:     strconv.FormatInt(id, 10),
		ExternalIDs:    map[string]string{"tmdb": strconv.FormatInt(id, 10)},
		Certifications: extractTMDBCertifications(details, t),
	}
	if adult, ok := details["adult"].(bool); ok && adult {
		d.AdultContent = true
		d.AdultReasons = []string{"tmdb:adult=true"}
	}
	if s, ok := details[titleKey].(string); ok {
		d.Title = s
	}
	if s, ok := details[origKey].(string); ok {
		d.OriginalTitle = s
	}
	if s, ok := details["overview"].(string); ok {
		d.Overview = s
	}
	if s, ok := details["tagline"].(string); ok {
		d.Tagline = s
	}
	if s, ok := details[dateKey].(string); ok {
		d.Premiered = s
		if len(s) >= 4 {
			if y := parseYearPrefix(s); y > 0 {
				v := int32(y)
				d.Year = &v
			}
		}
	}
	if r := jsonFloat64(details, "vote_average"); r != nil {
		d.Rating = r
	}
	if imdb, ok := details["imdb_id"].(string); ok && imdb != "" {
		d.ExternalIDs["imdb"] = imdb
	}
	if platform := ExtractPlatform(details, map[scraper.MediaType]string{
		scraper.MediaMovie:  "Movie",
		scraper.MediaSeries: "Series",
	}[t]); platform != nil {
		d.Platforms = append(d.Platforms, *platform)
	}

	if arr, ok := details["genres"].([]interface{}); ok {
		for _, g := range arr {
			if gm, ok := g.(map[string]interface{}); ok {
				if n, ok := gm["name"].(string); ok && n != "" {
					d.Genres = append(d.Genres, n)
				}
			}
		}
	}

	// Studios 先不做归一，原样返回；Aggregator 后续调用 ExtractPlatform 统一。
	if arr, ok := details["production_companies"].([]interface{}); ok {
		for _, c := range arr {
			if cm, ok := c.(map[string]interface{}); ok {
				if n, ok := cm["name"].(string); ok && n != "" {
					d.Studios = append(d.Studios, n)
				}
			}
		}
	}

	if credits, ok := details["credits"].(map[string]interface{}); ok {
		if castArr, ok := credits["cast"].([]interface{}); ok {
			limit := min(len(castArr), 20)
			for i, c := range castArr[:limit] {
				cm, ok := c.(map[string]interface{})
				if !ok {
					continue
				}
				name, _ := cm["name"].(string)
				if strings.TrimSpace(name) == "" {
					continue
				}
				role, _ := cm["character"].(string)
				actor := scraper.Actor{Name: name, Role: role, Order: i}
				if aid, ok := jsonInt64(cm, "id"); ok {
					v := int32(aid)
					actor.TmdbID = &v
				}
				if pp, ok := cm["profile_path"].(string); ok && pp != "" {
					u := fmt.Sprintf("%s/w185%s", TMDB_IMAGE_BASE, pp)
					actor.ImageURL = &u
				}
				d.Actors = append(d.Actors, actor)
			}
		}
		if crewArr, ok := credits["crew"].([]interface{}); ok {
			for _, c := range crewArr {
				cm, ok := c.(map[string]interface{})
				if !ok {
					continue
				}
				if job, _ := cm["job"].(string); job == "Director" {
					if dn, ok := cm["name"].(string); ok && dn != "" {
						d.Directors = append(d.Directors, dn)
					}
				}
			}
		}
	}

	if pp, ok := details["poster_path"].(string); ok && pp != "" {
		d.PosterURLs = []string{fmt.Sprintf("%s/w500%s", TMDB_IMAGE_BASE, pp)}
	}
	if bp, ok := details["backdrop_path"].(string); ok && bp != "" {
		d.BackdropURLs = []string{fmt.Sprintf("%s/w1280%s", TMDB_IMAGE_BASE, bp)}
	}

	return d
}

func extractTMDBCertifications(details map[string]interface{}, t scraper.MediaType) []string {
	if details == nil {
		return nil
	}
	var out []string
	switch t {
	case scraper.MediaMovie:
		rd, ok := details["release_dates"].(map[string]interface{})
		if !ok {
			return nil
		}
		results, ok := rd["results"].([]interface{})
		if !ok {
			return nil
		}
		for _, item := range results {
			rm, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			releases, ok := rm["release_dates"].([]interface{})
			if !ok {
				continue
			}
			for _, rel := range releases {
				relMap, ok := rel.(map[string]interface{})
				if !ok {
					continue
				}
				if cert, ok := relMap["certification"].(string); ok {
					if s := strings.TrimSpace(cert); s != "" {
						out = append(out, s)
					}
				}
			}
		}
	case scraper.MediaSeries:
		cr, ok := details["content_ratings"].(map[string]interface{})
		if !ok {
			return nil
		}
		results, ok := cr["results"].([]interface{})
		if !ok {
			return nil
		}
		for _, item := range results {
			rm, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if rating, ok := rm["rating"].(string); ok {
				if s := strings.TrimSpace(rating); s != "" {
					out = append(out, s)
				}
			}
		}
	}
	return dedupeNonEmptyStrings(out)
}

func candidatesFromTMDB(results []map[string]interface{}, kind string) []scraper.Candidate {
	titleKey, origKey, dateKey := "title", "original_title", "release_date"
	if kind == "tv" {
		titleKey, origKey, dateKey = "name", "original_name", "first_air_date"
	}
	out := make([]scraper.Candidate, 0, len(results))
	for _, r := range results {
		cand := scraper.Candidate{}
		if id, ok := jsonInt64(r, "id"); ok && id > 0 {
			cand.ProviderID = strconv.FormatInt(id, 10)
			cand.ExternalIDs = map[string]string{"tmdb": cand.ProviderID}
		}
		if cand.ProviderID == "" {
			continue
		}
		if t, ok := r[titleKey].(string); ok {
			cand.Title = t
		}
		if t, ok := r[origKey].(string); ok {
			cand.OriginalTitle = t
		}
		if d, ok := r[dateKey].(string); ok && len(d) >= 4 {
			if y := parseYearPrefix(d); y > 0 {
				v := int32(y)
				cand.Year = &v
			}
		}
		if pp := jsonFloat64(r, "popularity"); pp != nil {
			cand.Popularity = *pp
		}
		if posterPath, ok := r["poster_path"].(string); ok && strings.TrimSpace(posterPath) != "" {
			cand.PosterURL = fmt.Sprintf("%s/w500%s", TMDB_IMAGE_BASE, posterPath)
		}
		if adult, ok := r["adult"].(bool); ok && adult {
			cand.AdultContent = true
			cand.AdultReasons = []string{"tmdb:adult=true"}
		}
		out = append(out, cand)
	}
	return out
}

func mergedToNfoData(merged *scraper.MergedDetails, tmdbID int64, studio *string) NfoData {
	var title *string
	if s := strings.TrimSpace(merged.Title); s != "" {
		title = &s
	}
	var originalTitle *string
	if s := strings.TrimSpace(merged.OriginalTitle); s != "" {
		originalTitle = &s
	}
	var plot *string
	if s := strings.TrimSpace(merged.Overview); s != "" {
		plot = &s
	}
	var premiered *string
	if s := strings.TrimSpace(merged.Premiered); s != "" {
		premiered = &s
	}
	var tagline *string
	if s := strings.TrimSpace(merged.Tagline); s != "" {
		tagline = &s
	}
	var imdbID *string
	var tvdbID *int32
	if merged.ExternalIDs != nil {
		if s := strings.TrimSpace(merged.ExternalIDs["imdb"]); s != "" {
			imdbID = &s
		}
		if s := strings.TrimSpace(merged.ExternalIDs["tvdb"]); s != "" {
			if v, err := strconv.ParseInt(s, 10, 32); err == nil && v > 0 {
				tv := int32(v)
				tvdbID = &tv
			}
		}
	}
	var tmdbIDi32 *int32
	if tmdbID > 0 {
		v := int32(tmdbID)
		tmdbIDi32 = &v
	}
	var rating *float64
	if merged.Rating != nil {
		v := *merged.Rating
		rating = &v
	}
	actors := make([]NfoActor, 0, len(merged.Actors))
	for _, actor := range merged.Actors {
		actors = append(actors, NfoActor{
			Name:     actor.Name,
			Role:     actor.Role,
			TmdbID:   actor.TmdbID,
			ImageURL: actor.ImageURL,
		})
	}
	return NfoData{
		Title:         title,
		OriginalTitle: originalTitle,
		Plot:          plot,
		Year:          merged.Year,
		Rating:        rating,
		TmdbID:        tmdbIDi32,
		ImdbID:        imdbID,
		TvdbID:        tvdbID,
		Genres:        append([]string(nil), merged.Genres...),
		Actors:        actors,
		Directors:     append([]string(nil), merged.Directors...),
		Premiered:     premiered,
		Tagline:       tagline,
		Studio:        studio,
	}
}

func applyMergedDetails(ctx context.Context, pool *pgxpool.Pool, itemID string, client *TmdbClient, itemType string, tmdbID int64, merged *scraper.MergedDetails, updateTMDBID bool, source models.PlatformScanSource) (map[string]interface{}, error) {
	if merged == nil {
		return nil, fmt.Errorf("merged details is nil")
	}
	saveMode := getScrapeSaveMode(ctx, pool)
	var studio *string
	if len(merged.Platforms) > 0 {
		candidate := strings.TrimSpace(merged.Platforms[0])
		if candidate != "" {
			studio = &candidate
		}
	}
	nfo := mergedToNfoData(merged, tmdbID, studio)
	ApplyNfoDataWithType(ctx, pool, itemID, itemType, &nfo, source)

	var externalIDs []externalIDRecord
	for provider, value := range merged.ExternalIDs {
		externalIDs = append(externalIDs, externalIDRecord{Provider: provider, Value: value})
	}
	if len(externalIDs) == 0 && tmdbID > 0 {
		externalIDs = append(externalIDs, externalIDRecord{Provider: "tmdb", Value: strconv.FormatInt(tmdbID, 10)})
	}
	upsertExternalIDs(ctx, pool, itemID, externalIDs)

	if studio != nil {
		if err := models.MarkPlatformScanMatched(ctx, pool, itemID, *studio, source); err != nil {
			return nil, err
		}
		if itemType == "Series" {
			if err := models.PropagateStudioToChildren(ctx, pool, itemID, *studio); err != nil {
				return nil, err
			}
		}
	} else {
		if err := models.MarkPlatformScanNoMatch(ctx, pool, itemID, source, "no platform matched from merged details"); err != nil {
			return nil, err
		}
	}

	targets := resolveScrapeSaveTargets(ctx, pool, itemID, itemType)
	saveToData := saveMode == "database" || saveMode == "both"
	saveToMedia := saveMode == "media_dir" || saveMode == "both"

	if saveToMedia && targets.NfoPath != "" {
		if ok := writeNfoFile(targets.NfoPath, itemType, &nfo); !ok {
			slog.Warn("[Scraper] Failed to write NFO to media directory", "item_id", itemID, "path", targets.NfoPath)
		}
	}

	if updateTMDBID && tmdbID > 0 {
		_, err := pool.Exec(ctx,
			"UPDATE items SET tmdb_id = $1, imdb_id = COALESCE(NULLIF($2, ''), imdb_id), updated_at = NOW() WHERE id = $3::uuid",
			int32(tmdbID), derefStr(nfo.ImdbID), itemID)
		if err != nil {
			return nil, fmt.Errorf("update ids: %w", err)
		}
	}

	if len(merged.PosterURLs) > 0 {
		posterURL := merged.PosterURLs[0]
		var dbPosterPath string
		var dbPosterTag *string
		mediaSaved := false
		if saveToMedia && targets.PosterPath != "" {
			if client.downloadImageURL(ctx, posterURL, targets.PosterPath) {
				dbPosterPath = targets.PosterPath
				dbPosterTag = GenerateImageTag(targets.PosterPath)
				mediaSaved = true
			}
		}
		if saveToData || (saveToMedia && !mediaSaved) {
			dataPath := fmt.Sprintf("data/metadata/%s/poster.jpg", itemID)
			if client.downloadImageURL(ctx, posterURL, dataPath) && dbPosterPath == "" {
				dbPosterPath = dataPath
				dbPosterTag = GenerateImageTag(dataPath)
			}
		}
		if dbPosterPath != "" {
			_, _ = pool.Exec(ctx,
				"UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid",
				dbPosterPath, dbPosterTag, itemID)
		}
	}

	if len(merged.BackdropURLs) > 0 {
		backdropURL := merged.BackdropURLs[0]
		var dbBackdropPath string
		var dbBackdropTag *string
		mediaSaved := false
		if saveToMedia && targets.BackdropPath != "" {
			if client.downloadImageURL(ctx, backdropURL, targets.BackdropPath) {
				dbBackdropPath = targets.BackdropPath
				dbBackdropTag = GenerateImageTag(targets.BackdropPath)
				mediaSaved = true
			}
		}
		if saveToData || (saveToMedia && !mediaSaved) {
			dataPath := fmt.Sprintf("data/metadata/%s/backdrop.jpg", itemID)
			if client.downloadImageURL(ctx, backdropURL, dataPath) && dbBackdropPath == "" {
				dbBackdropPath = dataPath
				dbBackdropTag = GenerateImageTag(dataPath)
			}
		}
		if dbBackdropPath != "" {
			_, _ = pool.Exec(ctx,
				"UPDATE items SET backdrop_image_path = $1, backdrop_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid",
				dbBackdropPath, dbBackdropTag, itemID)
		}
	}

	if itemType == "Series" && tmdbID > 0 {
		scrapeSeasonPosters(ctx, pool, client, itemID, tmdbID, saveMode)
		scrapeEpisodeMetadata(ctx, pool, client, itemID, tmdbID)
	}

	return map[string]interface{}{
		"success": true,
		"tmdb_id": tmdbID,
		"name":    nfo.Title,
	}, nil
}

func applyTMDBDetails(ctx context.Context, pool *pgxpool.Pool, itemID string, client *TmdbClient, itemType string, itemName string, tmdbID int64, details map[string]interface{}, updateTMDBID bool, source models.PlatformScanSource) (map[string]interface{}, error) {
	saveMode := getScrapeSaveMode(ctx, pool)

	// Extract overview with fallback chain: primary language -> en-US -> Douban
	overview := jsonStringNonEmpty(details, "overview")
	if overview == nil && client.language != "en-US" {
		enClient := client.cloneWithLanguage("en-US")
		var enDetails map[string]interface{}
		if itemType == "Movie" {
			enDetails, _ = enClient.GetMovieDetails(ctx, tmdbID)
		} else {
			enDetails, _ = enClient.GetTVDetails(ctx, tmdbID)
		}
		if enDetails != nil {
			overview = jsonStringNonEmpty(enDetails, "overview")
		}
	}
	if overview == nil {
		fallbackName := itemName
		if title := jsonStringPtr(details, map[string]string{"Movie": "title", "Series": "name"}[itemType]); title != nil && strings.TrimSpace(*title) != "" {
			fallbackName = *title
		}
		overview = fetchDoubanOverview(client.httpClient, fallbackName)
	}

	rating := jsonFloat64(details, "vote_average")

	// Genres
	var genres []string
	if genreArr, ok := details["genres"].([]interface{}); ok {
		for _, g := range genreArr {
			if gm, ok := g.(map[string]interface{}); ok {
				if n, ok := gm["name"].(string); ok && n != "" {
					genres = append(genres, n)
				}
			}
		}
	}

	// Actors (up to 20)
	var actors []NfoActor
	if credits, ok := details["credits"].(map[string]interface{}); ok {
		if castArr, ok := credits["cast"].([]interface{}); ok {
			limit := 20
			if len(castArr) < limit {
				limit = len(castArr)
			}
			for _, c := range castArr[:limit] {
				cm, ok := c.(map[string]interface{})
				if !ok {
					continue
				}
				aName, ok := cm["name"].(string)
				if !ok || aName == "" {
					continue
				}
				role, _ := cm["character"].(string)
				var tmdbActorID *int32
				if aid, ok := jsonInt64(cm, "id"); ok {
					v := int32(aid)
					tmdbActorID = &v
				}
				var imageURL *string
				if pp, ok := cm["profile_path"].(string); ok && pp != "" {
					u := fmt.Sprintf("%s/w185%s", TMDB_IMAGE_BASE, pp)
					imageURL = &u
				}
				actors = append(actors, NfoActor{
					Name:     aName,
					Role:     role,
					TmdbID:   tmdbActorID,
					ImageURL: imageURL,
				})
			}
		}
	}

	// Directors
	var directors []string
	if credits, ok := details["credits"].(map[string]interface{}); ok {
		if crewArr, ok := credits["crew"].([]interface{}); ok {
			for _, c := range crewArr {
				cm, ok := c.(map[string]interface{})
				if !ok {
					continue
				}
				if job, _ := cm["job"].(string); job == "Director" {
					if dn, ok := cm["name"].(string); ok && dn != "" {
						directors = append(directors, dn)
					}
				}
			}
		}
	}

	// Extract platform/studio from networks (TV) or production_companies (Movie)
	studio := ExtractPlatform(details, itemType)

	// Build title key based on type
	titleKey := "title"
	dateKey := "release_date"
	if itemType != "Movie" {
		titleKey = "name"
		dateKey = "first_air_date"
	}

	title := jsonStringPtr(details, titleKey)
	premiered := jsonStringPtr(details, dateKey)

	var nfoYear *int32
	if premiered != nil && len(*premiered) >= 4 {
		if y := parseYearPrefix(*premiered); y > 0 {
			v := int32(y)
			nfoYear = &v
		}
	}

	var tmdbIDi32 *int32
	{
		v := int32(tmdbID)
		tmdbIDi32 = &v
	}

	nfo := NfoData{
		Title:     title,
		Plot:      overview,
		Year:      nfoYear,
		Rating:    rating,
		TmdbID:    tmdbIDi32,
		Genres:    genres,
		Actors:    actors,
		Directors: directors,
		Premiered: premiered,
		Studio:    studio,
	}

	ApplyNfoDataWithPlatformSource(ctx, pool, itemID, &nfo, source)

	var externalIDs []externalIDRecord
	externalIDs = append(externalIDs, externalIDRecord{Provider: "tmdb", Value: strconv.FormatInt(tmdbID, 10)})
	if imdbID := jsonStringNonEmpty(details, "imdb_id"); imdbID != nil {
		externalIDs = append(externalIDs, externalIDRecord{Provider: "imdb", Value: *imdbID})
	}
	upsertExternalIDs(ctx, pool, itemID, externalIDs)

	// Set studio and propagate to children for Series
	if studio != nil {
		if err := models.MarkPlatformScanMatched(ctx, pool, itemID, *studio, source); err != nil {
			return nil, err
		}
		if itemType == "Series" {
			if err := models.PropagateStudioToChildren(ctx, pool, itemID, *studio); err != nil {
				return nil, err
			}
		}
	} else {
		if err := models.MarkPlatformScanNoMatch(ctx, pool, itemID, source, "no platform matched from TMDB details"); err != nil {
			return nil, err
		}
	}

	targets := resolveScrapeSaveTargets(ctx, pool, itemID, itemType)
	saveToData := saveMode == "database" || saveMode == "both"
	saveToMedia := saveMode == "media_dir" || saveMode == "both"

	if saveToMedia && targets.NfoPath != "" {
		if ok := writeNfoFile(targets.NfoPath, itemType, &nfo); !ok {
			slog.Warn("[TMDB] Failed to write NFO to media directory", "item_id", itemID, "path", targets.NfoPath)
		}
	}

	if updateTMDBID {
		_, err := pool.Exec(ctx,
			"UPDATE items SET tmdb_id = $1, updated_at = NOW() WHERE id = $2::uuid",
			int32(tmdbID), itemID)
		if err != nil {
			return nil, fmt.Errorf("update tmdb_id: %w", err)
		}
	}

	// Download poster
	if posterPath, ok := details["poster_path"].(string); ok && posterPath != "" {
		var dbPosterPath string
		var dbPosterTag *string

		mediaSaved := false
		if saveToMedia && targets.PosterPath != "" {
			if client.DownloadImage(ctx, posterPath, targets.PosterPath, "w500") {
				dbPosterPath = targets.PosterPath
				dbPosterTag = GenerateImageTag(targets.PosterPath)
				mediaSaved = true
			} else {
				slog.Warn("[TMDB] Failed to save poster to media directory, falling back to data/metadata/",
					"item_id", itemID, "path", targets.PosterPath)
			}
		}
		if saveToData || (saveToMedia && !mediaSaved) {
			dataPath := fmt.Sprintf("data/metadata/%s/poster.jpg", itemID)
			if client.DownloadImage(ctx, posterPath, dataPath, "w500") && dbPosterPath == "" {
				dbPosterPath = dataPath
				dbPosterTag = GenerateImageTag(dataPath)
			}
		}
		if dbPosterPath != "" {
			_, _ = pool.Exec(ctx,
				"UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid",
				dbPosterPath, dbPosterTag, itemID)
		}
	}

	// Download backdrop
	if backdropPath, ok := details["backdrop_path"].(string); ok && backdropPath != "" {
		var dbBackdropPath string
		var dbBackdropTag *string

		mediaSaved := false
		if saveToMedia && targets.BackdropPath != "" {
			if client.DownloadImage(ctx, backdropPath, targets.BackdropPath, "w1280") {
				dbBackdropPath = targets.BackdropPath
				dbBackdropTag = GenerateImageTag(targets.BackdropPath)
				mediaSaved = true
			} else {
				slog.Warn("[TMDB] Failed to save backdrop to media directory, falling back to data/metadata/",
					"item_id", itemID, "path", targets.BackdropPath)
			}
		}
		if saveToData || (saveToMedia && !mediaSaved) {
			dataPath := fmt.Sprintf("data/metadata/%s/backdrop.jpg", itemID)
			if client.DownloadImage(ctx, backdropPath, dataPath, "w1280") && dbBackdropPath == "" {
				dbBackdropPath = dataPath
				dbBackdropTag = GenerateImageTag(dataPath)
			}
		}
		if dbBackdropPath != "" {
			_, _ = pool.Exec(ctx,
				"UPDATE items SET backdrop_image_path = $1, backdrop_image_tag = $2, updated_at = NOW() WHERE id = $3::uuid",
				dbBackdropPath, dbBackdropTag, itemID)
		}
	}

	// Scrape season posters for Series
	if itemType == "Series" {
		scrapeSeasonPosters(ctx, pool, client, itemID, tmdbID, saveMode)
	}

	return map[string]interface{}{
		"success": true,
		"tmdb_id": tmdbID,
		"name":    nfo.Title,
	}, nil
}

func RefreshItemMetadataByTMDBID(ctx context.Context, pool *pgxpool.Pool, itemID string, client *TmdbClient) (map[string]interface{}, error) {
	meta, err := loadScrapeItemMeta(ctx, pool, itemID)
	if err != nil {
		return nil, err
	}
	if meta.TmdbID == nil || *meta.TmdbID == 0 {
		return nil, fmt.Errorf("no TMDB ID")
	}
	mediaType, ok := mediaTypeFor(meta.ItemType)
	if !ok {
		return nil, fmt.Errorf("cannot scrape type: %s", meta.ItemType)
	}
	tmdbIDStr := strconv.FormatInt(int64(*meta.TmdbID), 10)
	ident := &scraper.Identity{
		Provider:    "tmdb",
		ProviderID:  tmdbIDStr,
		ExternalIDs: map[string]string{"tmdb": tmdbIDStr},
		Score:       1,
		Source:      "tmdb_id_refresh",
	}
	parsed := buildParsedName(meta)
	agg := GetScrapeAggregatorForLibrary(ctx, pool, sharedScrapeCache, client, client.httpClient, meta.LibraryID)
	merged, fillErr := agg.Fill(ctx, ident, parsed, mediaType)
	if fillErr != nil {
		return nil, fmt.Errorf("fill details: %w", fillErr)
	}
	return applyMergedDetails(ctx, pool, itemID, client, meta.ItemType, int64(*meta.TmdbID), merged, false, models.PlatformScanSourceTMDB)
}

func RefreshPlatformOnlyByTMDBID(ctx context.Context, pool *pgxpool.Pool, itemID string, client *TmdbClient) (*string, error) {
	meta, err := loadScrapeItemMeta(ctx, pool, itemID)
	if err != nil {
		return nil, err
	}
	if meta.TmdbID == nil || *meta.TmdbID == 0 {
		return nil, fmt.Errorf("no TMDB ID")
	}
	details, err := fetchTMDBDetailsByID(ctx, client, meta.ItemType, int64(*meta.TmdbID))
	if err != nil {
		return nil, err
	}
	studio := ExtractPlatform(details, meta.ItemType)
	if studio == nil {
		if err := models.MarkPlatformScanNoMatch(ctx, pool, itemID, models.PlatformScanSourceTMDB, "no platform matched from TMDB details"); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err := models.MarkPlatformScanMatched(ctx, pool, itemID, *studio, models.PlatformScanSourceTMDB); err != nil {
		return nil, err
	}
	if meta.ItemType == "Series" {
		if err := models.PropagateStudioToChildren(ctx, pool, itemID, *studio); err != nil {
			return nil, err
		}
	}
	return studio, nil
}

// ScrapeItemWithClient scrapes TMDB metadata for a single item using the provided client.
// ScrapeItemByTMDBID scrapes an item using an explicit TMDB ID (from user selection).
// 保留对外 API,内部转发到 ScrapeItemByProviderID 的 tmdb 路径。
func ScrapeItemByTMDBID(ctx context.Context, pool *pgxpool.Pool, itemID string, tmdbID int64) (map[string]interface{}, error) {
	if tmdbID <= 0 {
		return nil, fmt.Errorf("invalid tmdb id: %d", tmdbID)
	}
	return ScrapeItemByProviderID(ctx, pool, itemID, "tmdb", strconv.FormatInt(tmdbID, 10))
}

// ScrapeItemByProviderID 按 (provider, externalID) 刮削 item,支持任意已注册 provider 作为 primary。
// 流程:
//   - provider=tmdb: 构造 tmdb Identity → agg.Fill 多源合并字段 → applyMergedDetails 写入
//   - provider!=tmdb: 先调 primary.GetByID 拿详情,把 Details.ExternalIDs(通常含 imdb)
//     合并进 Identity,让 Fill.fetchSecondary 能跨源映射回 TMDB;然后走同样 Fill+apply 流程
//   - merged.ExternalIDs.tmdb 有值时反写 items.tmdb_id(Series 可以继续抓 episode)
//   - 无 tmdb_id 也能成功入库(基本字段齐全,Series 暂无分集)
func ScrapeItemByProviderID(ctx context.Context, pool *pgxpool.Pool, itemID, provider, externalID string) (map[string]interface{}, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	externalID = strings.TrimSpace(externalID)
	if provider == "" || externalID == "" {
		return nil, fmt.Errorf("provider / externalID required")
	}
	client := TmdbClientFromConfig(ctx, pool)
	if client == nil {
		return nil, fmt.Errorf("TMDB API 密钥未配置")
	}
	meta, err := loadScrapeItemMeta(ctx, pool, itemID)
	if err != nil {
		return nil, err
	}
	mediaType, ok := mediaTypeFor(meta.ItemType)
	if !ok {
		return nil, fmt.Errorf("cannot scrape type: %s", meta.ItemType)
	}

	agg := GetScrapeAggregatorForLibrary(ctx, pool, sharedScrapeCache, client, client.httpClient, meta.LibraryID)

	ident := &scraper.Identity{
		Provider:    provider,
		ProviderID:  externalID,
		ExternalIDs: map[string]string{provider: externalID},
		Score:       1,
		Source:      "manual_" + provider + "_id",
	}

	// 非 tmdb primary:先拉一次 provider 详情,把 Details.ExternalIDs 合入 Identity。
	// 豆瓣 Candidates 阶段 ExternalIDs 只有 douban id,imdb 要等详情页才有;
	// 合并后 Aggregator.Fill.fetchSecondary 才能通过 imdb 跨源到 TMDB。
	if provider != "tmdb" {
		primary := agg.ProviderByName(provider)
		if primary == nil {
			return nil, fmt.Errorf("provider %s 未启用或未注册", provider)
		}
		primaryDetails, derr := primary.GetByID(ctx, mediaType, externalID)
		if derr != nil {
			return nil, fmt.Errorf("拉取 %s 详情失败: %w", provider, derr)
		}
		if primaryDetails == nil {
			return nil, fmt.Errorf("%s 详情为空(external_id=%s)", provider, externalID)
		}
		for k, v := range primaryDetails.ExternalIDs {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if _, ok := ident.ExternalIDs[k]; !ok {
				ident.ExternalIDs[k] = v
			}
		}
	}

	parsed := buildParsedName(meta)
	merged, fillErr := agg.Fill(ctx, ident, parsed, mediaType)
	if fillErr != nil {
		return nil, fmt.Errorf("fill details: %w", fillErr)
	}

	// merged.ExternalIDs 是 primary + 所有辅源的 union,带 tmdb 则反写 items.tmdb_id
	var tmdbID int64
	if raw := strings.TrimSpace(merged.ExternalIDs["tmdb"]); raw != "" {
		if v, perr := strconv.ParseInt(raw, 10, 64); perr == nil && v > 0 {
			tmdbID = v
		}
	}

	return applyMergedDetails(ctx, pool, itemID, client, meta.ItemType, tmdbID, merged, tmdbID > 0, models.PlatformScanSourceSearch)
}

// SearchTMDBForItem searches TMDB for an item by custom query or explicit TMDB ID.
func SearchTMDBForItem(ctx context.Context, pool *pgxpool.Pool, itemID, query string, year *int32, tmdbID *int64) ([]map[string]interface{}, error) {
	client := TmdbClientFromConfig(ctx, pool)
	if client == nil {
		return nil, fmt.Errorf("TMDB API 密钥未配置")
	}
	meta, err := loadScrapeItemMeta(ctx, pool, itemID)
	if err != nil {
		return nil, err
	}
	switch meta.ItemType {
	case "Movie":
		if tmdbID != nil {
			details, err := client.GetMovieDetails(ctx, *tmdbID)
			if err != nil {
				return nil, fmt.Errorf("未找到电影类型的 TMDB 条目: %w", err)
			}
			if details == nil || details["id"] == nil {
				return nil, fmt.Errorf("未找到电影类型的 TMDB 条目")
			}
			return []map[string]interface{}{tmdbCandidateFromDetails(details, "Movie")}, nil
		}
		return client.SearchMovieMulti(ctx, query, year)
	case "Series":
		if tmdbID != nil {
			details, err := client.GetTVDetails(ctx, *tmdbID)
			if err != nil {
				return nil, fmt.Errorf("未找到剧集类型的 TMDB 条目: %w", err)
			}
			if details == nil || details["id"] == nil {
				return nil, fmt.Errorf("未找到剧集类型的 TMDB 条目")
			}
			return []map[string]interface{}{tmdbCandidateFromDetails(details, "Series")}, nil
		}
		return client.SearchTVMulti(ctx, query)
	default:
		return nil, fmt.Errorf("不支持的类型: %s", meta.ItemType)
	}
}

func tmdbCandidateFromDetails(details map[string]interface{}, itemType string) map[string]interface{} {
	out := map[string]interface{}{
		"id":           details["id"],
		"poster_path":  details["poster_path"],
		"overview":     details["overview"],
		"vote_average": details["vote_average"],
	}
	if itemType == "Movie" {
		out["title"] = details["title"]
		out["release_date"] = details["release_date"]
	} else {
		out["name"] = details["name"]
		out["first_air_date"] = details["first_air_date"]
	}
	return out
}

func ScrapeItemWithClient(ctx context.Context, pool *pgxpool.Pool, itemID string, client *TmdbClient) (map[string]interface{}, error) {
	meta, err := loadScrapeItemMeta(ctx, pool, itemID)
	if err != nil {
		return nil, err
	}

	mediaType, ok := mediaTypeFor(meta.ItemType)
	if !ok {
		return nil, fmt.Errorf("cannot scrape type: %s", meta.ItemType)
	}

	parsed := buildParsedName(meta)
	runtimeCfg := LoadEffectiveScrapeConfig(ctx, pool, meta.LibraryID)
	agg := GetScrapeAggregator(sharedScrapeCache, runtimeCfg, client, client.httpClient)

	slog.Info("[Identify] start",
		"item_id", itemID, "type", meta.ItemType,
		"raw_name", meta.Name,
		"parsed_title", parsed.Title, "parsed_original", parsed.OriginalTitle,
		"parsed_year", formatYear(parsed.Year),
		"parsed_ids", parsed.IDs,
		"providers", agg.Providers(),
		"threshold", runtimeCfg.ConfidenceThreshold)

	// 已经带 TMDB ID 的 item 跳过 Identify 直接 Fill。
	// 注意:不能只喂 tmdb details 给 MergeDetails,那样会绕过辅源。
	// agg.Fill 内部会把 tmdb 作为 primary 拉详情,再并发拉 bangumi/douban/tvdb/fanart
	// 按 FieldPolicy 合字段(rating / poster / overview 等)。
	if meta.TmdbID != nil && *meta.TmdbID > 0 {
		tmdbIDStr := strconv.FormatInt(int64(*meta.TmdbID), 10)
		ident := &scraper.Identity{
			Provider:    "tmdb",
			ProviderID:  tmdbIDStr,
			ExternalIDs: map[string]string{"tmdb": tmdbIDStr},
			Score:       1,
			Source:      "tmdb_id_direct",
		}
		merged, fillErr := agg.Fill(ctx, ident, parsed, mediaType)
		if fillErr != nil {
			var adultErr *scraper.ErrAdultContentFiltered
			if errors.As(fillErr, &adultErr) {
				detail := buildAdultBlockedDetail(
					"fill",
					"fill blocked by adult-content filter",
					parsed,
					runtimeCfg,
					agg.Providers(),
					ident,
					nil,
					adultErr.Blocked,
				)
				DiagFrom(ctx).SetDetail(detail)
				logScrapeFailureDetail(itemID, detail)
				tmdbSetIdentifyAttempted(ctx, pool, itemID)
				if markErr := models.MarkPlatformScanUnidentified(ctx, pool, itemID, models.PlatformScanSourceTMDB, "fill blocked by adult-content filter"); markErr != nil {
					slog.Warn("[TMDB] mark adult-content filtered fill failed", "item_id", itemID, "error", markErr)
				}
				return nil, fillErr
			}
			return nil, fmt.Errorf("fill details: %w", fillErr)
		}
		tmdbSetIdentifyAttempted(ctx, pool, itemID)
		return applyMergedDetails(ctx, pool, itemID, client, meta.ItemType, int64(*meta.TmdbID), merged, false, models.PlatformScanSourceTMDB)
	}

	ident, err := agg.Identify(ctx, parsed, mediaType)
	if err != nil {
		reason := err.Error()
		source := models.PlatformScanSourceSearch
		var adultErr *scraper.ErrAdultContentFiltered
		if errors.As(err, &adultErr) {
			detail := buildAdultBlockedDetail(
				"identify",
				"identify blocked by adult-content filter",
				parsed,
				runtimeCfg,
				agg.Providers(),
				nil,
				nil,
				adultErr.Blocked,
			)
			DiagFrom(ctx).SetDetail(detail)
			logScrapeFailureDetail(itemID, detail)
			tmdbSetIdentifyAttempted(ctx, pool, itemID)
			reason = "identify blocked by adult-content filter"
			if markErr := models.MarkPlatformScanUnidentified(ctx, pool, itemID, source, reason); markErr != nil {
				slog.Warn("[TMDB] mark platform scan unidentified failed", "item_id", itemID, "error", markErr)
			}
			return nil, err
		}
		if errors.Is(err, scraper.ErrNoMatch) {
			// 捞一次候选列表,用于诊断日志 + (可选)人工确认队列。
			// 无论 AutoApply 如何都需要,有 cache 不会多发 TMDB 请求。
			candidates, _ := agg.Candidates(ctx, parsed, mediaType)
			detail := buildIdentifyFailureDetail(parsed, candidates, runtimeCfg.ConfidenceThreshold, agg.Providers(), runtimeCfg.AutoApply, runtimeCfg.AdultContentFilterEnabled)
			DiagFrom(ctx).SetDetail(detail)
			logScrapeFailureDetail(itemID, detail)

			if !runtimeCfg.AutoApply {
				if len(candidates) > 0 {
					_ = replaceIdentifyCandidates(ctx, pool, itemID, candidates)
					tmdbSetIdentifyAttempted(ctx, pool, itemID)
					if markErr := models.MarkPlatformScanUnidentified(ctx, pool, itemID, source, "identify queued for manual confirmation"); markErr != nil {
						slog.Warn("[TMDB] mark manual identify queue failed", "item_id", itemID, "error", markErr)
					}
					return nil, fmt.Errorf("identify queued for manual confirmation")
				}
			}
			tmdbSetIdentifyAttempted(ctx, pool, itemID)
			reason = "identify failed: no confident match"
			if markErr := models.MarkPlatformScanUnidentified(ctx, pool, itemID, source, reason); markErr != nil {
				slog.Warn("[TMDB] mark platform scan unidentified failed", "item_id", itemID, "error", markErr)
			}
			return nil, err
		}
		slog.Warn("[Identify] error (not ErrNoMatch)",
			"item_id", itemID, "parsed_title", parsed.Title, "error", reason)
		if markErr := models.MarkPlatformScanError(ctx, pool, itemID, source, reason); markErr != nil {
			slog.Warn("[TMDB] mark platform scan error failed", "item_id", itemID, "error", markErr)
		}
		return nil, err
	}

	tmdbID := resolveTMDBIDFromIdentity(ident)
	merged, fillErr := agg.Fill(ctx, ident, parsed, mediaType)
	if fillErr != nil {
		var adultErr *scraper.ErrAdultContentFiltered
		if errors.As(fillErr, &adultErr) {
			detail := buildAdultBlockedDetail(
				"fill",
				"fill blocked by adult-content filter",
				parsed,
				runtimeCfg,
				agg.Providers(),
				ident,
				nil,
				adultErr.Blocked,
			)
			DiagFrom(ctx).SetDetail(detail)
			logScrapeFailureDetail(itemID, detail)
			tmdbSetIdentifyAttempted(ctx, pool, itemID)
			if markErr := models.MarkPlatformScanUnidentified(ctx, pool, itemID, models.PlatformScanSourceSearch, "fill blocked by adult-content filter"); markErr != nil {
				slog.Warn("[TMDB] mark adult-content filtered fill failed", "item_id", itemID, "error", markErr)
			}
			return nil, fillErr
		}
		return nil, fmt.Errorf("fill details: %w", fillErr)
	}
	// 非 TMDB primary 时,Fill 内部辅源 TMDB 可能通过 imdb 跨源映射拿到 tmdb_id,
	// 从 merged.ExternalIDs 补解一次,有值就反写 items.tmdb_id 并启用 Series episode 抓取。
	if tmdbID <= 0 {
		if raw := strings.TrimSpace(merged.ExternalIDs["tmdb"]); raw != "" {
			if v, perr := strconv.ParseInt(raw, 10, 64); perr == nil && v > 0 {
				tmdbID = v
			}
		}
	}
	tmdbSetIdentifyAttempted(ctx, pool, itemID)
	slog.Info("[Identify] matched",
		"item_id", itemID,
		"parsed_title", parsed.Title, "parsed_year", formatYear(parsed.Year),
		"provider", ident.Provider, "provider_id", ident.ProviderID,
		"source", ident.Source, "score", fmt.Sprintf("%.3f", ident.Score),
		"tmdb_id", tmdbID)
	return applyMergedDetails(ctx, pool, itemID, client, meta.ItemType, tmdbID, merged, tmdbID > 0, models.PlatformScanSourceSearch)
}

func buildIdentifyFailureDetail(
	parsed scraper.ParsedName,
	candidates []scraper.ScoredCandidate,
	threshold float64,
	providers []string,
	autoApply bool,
	adultFilterEnabled bool,
) identifyFailureDetail {
	attempts := scraper.BuildSearchAttempts(parsed)
	searchAttempts := make([]identifyFailureSearchAttempt, 0, len(attempts))
	for _, a := range attempts {
		if strings.TrimSpace(a.Query) == "" {
			continue
		}
		searchAttempts = append(searchAttempts, identifyFailureSearchAttempt{
			Source: a.Source,
			Query:  a.Query,
			Year:   a.Year,
		})
	}

	detail := identifyFailureDetail{
		Stage:              "identify",
		Threshold:          roundFloat(threshold, 3),
		AutoApply:          autoApply,
		AdultFilterEnabled: adultFilterEnabled,
		Providers:          append([]string(nil), providers...),
		Parsed: identifyFailureParsed{
			Title:         parsed.Title,
			OriginalTitle: parsed.OriginalTitle,
			Year:          parsed.Year,
			IDs:           cloneStringMap(parsed.IDs),
			MediaHint:     parsed.MediaHint,
			Junk:          append([]string(nil), parsed.Junk...),
		},
		SearchAttempts:  searchAttempts,
		CandidatesTotal: len(candidates),
		Candidates:      make([]identifyFailureCandidateRecord, 0, len(candidates)),
	}
	if len(candidates) == 0 {
		detail.Reason = "no candidate returned by providers"
		return detail
	}

	bestScore := roundFloat(candidates[0].Score, 3)
	detail.BestScore = &bestScore
	detail.Reason = fmt.Sprintf("best score %.3f below threshold %.3f", candidates[0].Score, threshold)
	for _, c := range candidates {
		detail.Candidates = append(detail.Candidates, identifyFailureCandidateRecordFromScored(c, false))
	}
	return detail
}

func buildAdultBlockedDetail(
	stage string,
	reason string,
	parsed scraper.ParsedName,
	cfg scraper.RuntimeConfig,
	providers []string,
	ident *scraper.Identity,
	candidates []scraper.ScoredCandidate,
	blocked []scraper.AdultBlockedCandidate,
) identifyFailureDetail {
	searchAttempts := make([]identifyFailureSearchAttempt, 0)
	if strings.EqualFold(strings.TrimSpace(stage), "identify") {
		for _, a := range scraper.BuildSearchAttempts(parsed) {
			if strings.TrimSpace(a.Query) == "" {
				continue
			}
			searchAttempts = append(searchAttempts, identifyFailureSearchAttempt{
				Source: a.Source,
				Query:  a.Query,
				Year:   a.Year,
			})
		}
	}
	detail := identifyFailureDetail{
		Stage:              strings.TrimSpace(stage),
		Reason:             strings.TrimSpace(reason),
		Threshold:          roundFloat(cfg.ConfidenceThreshold, 3),
		AutoApply:          cfg.AutoApply,
		AdultFilterEnabled: cfg.AdultContentFilterEnabled,
		Providers:          append([]string(nil), providers...),
		Parsed: identifyFailureParsed{
			Title:         parsed.Title,
			OriginalTitle: parsed.OriginalTitle,
			Year:          parsed.Year,
			IDs:           cloneStringMap(parsed.IDs),
			MediaHint:     parsed.MediaHint,
			Junk:          append([]string(nil), parsed.Junk...),
		},
		SearchAttempts:         searchAttempts,
		CandidatesTotal:        len(candidates),
		BlockedCandidatesTotal: len(blocked),
		Candidates:             make([]identifyFailureCandidateRecord, 0, len(candidates)),
		BlockedCandidates:      make([]identifyFailureCandidateRecord, 0, len(blocked)),
	}
	if ident != nil {
		detail.Matched = &identifyFailureMatched{
			Provider:    ident.Provider,
			ProviderID:  ident.ProviderID,
			Source:      ident.Source,
			Score:       roundFloat(ident.Score, 3),
			ExternalIDs: cloneStringMap(ident.ExternalIDs),
		}
	}
	for _, cand := range candidates {
		detail.Candidates = append(detail.Candidates, identifyFailureCandidateRecordFromScored(cand, false))
	}
	for _, item := range blocked {
		detail.BlockedCandidates = append(detail.BlockedCandidates, identifyFailureCandidateRecord{
			Provider:       item.Provider,
			ProviderID:     item.ProviderID,
			Title:          item.Title,
			OriginalTitle:  item.OriginalTitle,
			Year:           item.Year,
			Score:          roundFloat(item.Score, 3),
			Popularity:     roundFloat(item.Popularity, 3),
			Source:         item.Source,
			ExternalIDs:    cloneStringMap(item.ExternalIDs),
			PosterURL:      strings.TrimSpace(item.PosterURL),
			Blocked:        true,
			AdultReasons:   append([]string(nil), item.AdultReasons...),
			Certifications: append([]string(nil), item.Certifications...),
		})
	}
	return detail
}

func identifyFailureCandidateRecordFromScored(c scraper.ScoredCandidate, blocked bool) identifyFailureCandidateRecord {
	return identifyFailureCandidateRecord{
		Provider:       c.Provider,
		ProviderID:     c.ProviderID,
		Title:          c.Title,
		OriginalTitle:  c.OriginalTitle,
		Year:           c.Year,
		Score:          roundFloat(c.Score, 3),
		Popularity:     roundFloat(c.Popularity, 3),
		Source:         c.Source,
		ExternalIDs:    cloneStringMap(c.ExternalIDs),
		PosterURL:      strings.TrimSpace(c.PosterURL),
		Blocked:        blocked,
		AdultReasons:   append([]string(nil), c.AdultReasons...),
		Certifications: append([]string(nil), c.Certifications...),
	}
}

// logScrapeFailureDetail 打印识别/填充失败时的完整诊断。
func logScrapeFailureDetail(itemID string, detail identifyFailureDetail) {
	slog.Info("[Scrape] failed",
		"item_id", itemID,
		"stage", detail.Stage,
		"parsed_title", detail.Parsed.Title,
		"parsed_original", detail.Parsed.OriginalTitle,
		"parsed_year", formatYear(detail.Parsed.Year),
		"parsed_ids", detail.Parsed.IDs,
		"providers", detail.Providers,
		"threshold", detail.Threshold,
		"adult_filter_enabled", detail.AdultFilterEnabled,
		"matched", detail.Matched,
		"search_attempts", detail.SearchAttempts,
		"candidates_total", detail.CandidatesTotal,
		"blocked_candidates_total", detail.BlockedCandidatesTotal,
		"top_candidates", detail.Candidates,
		"blocked_candidates", detail.BlockedCandidates,
		"reason", detail.Reason)
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		vv := strings.TrimSpace(v)
		if vv == "" {
			continue
		}
		dst[k] = vv
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}

func formatYear(y *int32) string {
	if y == nil {
		return ""
	}
	return strconv.FormatInt(int64(*y), 10)
}

func roundFloat(v float64, digits int) float64 {
	p := 1.0
	for i := 0; i < digits; i++ {
		p *= 10
	}
	return float64(int64(v*p+0.5)) / p
}

func dedupeNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// resolveTMDBIDFromIdentity 从 Identity 提取 tmdb_id;兼容 Provider=tmdb 的直达
// 与辅源 winner(ExternalIDs["tmdb"]) 两种情况。返回 0 表示无法获取。
func resolveTMDBIDFromIdentity(ident *scraper.Identity) int64 {
	if ident == nil {
		return 0
	}
	if ident.Provider == "tmdb" {
		if id, err := strconv.ParseInt(strings.TrimSpace(ident.ProviderID), 10, 64); err == nil && id > 0 {
			return id
		}
	}
	if v := strings.TrimSpace(ident.ExternalIDs["tmdb"]); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			return id
		}
	}
	return 0
}

func mediaTypeFor(itemType string) (scraper.MediaType, bool) {
	switch itemType {
	case "Movie":
		return scraper.MediaMovie, true
	case "Series":
		return scraper.MediaSeries, true
	default:
		return "", false
	}
}

func scrapeSeasonPosters(ctx context.Context, pool *pgxpool.Pool, client *TmdbClient, seriesID string, tmdbID int64, saveMode string) {
	rows, err := pool.Query(ctx,
		"SELECT id, index_number FROM items WHERE parent_id = $1::uuid AND type = 'Season' ORDER BY index_number",
		seriesID)
	if err != nil {
		return
	}
	defer rows.Close()

	type seasonRow struct {
		id       uuid.UUID
		indexNum *int32
	}
	var seasons []seasonRow
	for rows.Next() {
		var s seasonRow
		if err := rows.Scan(&s.id, &s.indexNum); err != nil {
			continue
		}
		seasons = append(seasons, s)
	}
	rows.Close()

	for _, s := range seasons {
		num := int32(1)
		if s.indexNum != nil {
			num = *s.indexNum
		}

		var existingTag *string
		_ = pool.QueryRow(ctx,
			"SELECT primary_image_tag FROM items WHERE id = $1",
			s.id).Scan(&existingTag)
		if existingTag != nil {
			continue
		}

		posterPath := client.GetSeasonImages(ctx, tmdbID, num)
		if posterPath == nil {
			continue
		}

		sid := s.id.String()
		saveToData := saveMode == "database" || saveMode == "both"
		saveToMedia := saveMode == "media_dir" || saveMode == "both"

		var dbPosterPath string
		var dbPosterTag *string

		mediaSaved := false
		if saveToMedia {
			mediaPath := resolveSeasonPosterMediaPath(ctx, pool, sid)
			if mediaPath != "" {
				if client.DownloadImage(ctx, *posterPath, mediaPath, "w500") {
					dbPosterPath = mediaPath
					dbPosterTag = GenerateImageTag(mediaPath)
					mediaSaved = true
				} else {
					slog.Warn("[TMDB] Failed to save season poster to media directory", "season_id", sid, "path", mediaPath)
				}
			}
		}
		if saveToData || (saveToMedia && !mediaSaved) {
			dataPath := fmt.Sprintf("data/metadata/%s/poster.jpg", sid)
			if client.DownloadImage(ctx, *posterPath, dataPath, "w500") && dbPosterPath == "" {
				dbPosterPath = dataPath
				dbPosterTag = GenerateImageTag(dataPath)
			}
		}

		if dbPosterPath != "" {
			_, _ = pool.Exec(ctx,
				"UPDATE items SET primary_image_path = $1, primary_image_tag = $2, updated_at = NOW() WHERE id = $3",
				dbPosterPath, dbPosterTag, s.id)
		}

		time.Sleep(200 * time.Millisecond)
	}
}

// fetchDoubanOverview attempts to get a short description from Douban as fallback.
func fetchDoubanOverview(client *http.Client, name string) *string {
	suggestURL := fmt.Sprintf("https://movie.douban.com/j/subject_suggest?q=%s", url.QueryEscape(name))

	req, err := http.NewRequest(http.MethodGet, suggestURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Referer", "https://movie.douban.com/")

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var results []map[string]interface{}
	if err := json.Unmarshal(body, &results); err != nil || len(results) == 0 {
		return nil
	}

	subjectID, ok := results[0]["id"].(string)
	if !ok || subjectID == "" {
		return nil
	}

	detailURL := fmt.Sprintf("https://movie.douban.com/j/subject_abstract?subject_id=%s", subjectID)
	req2, err := http.NewRequest(http.MethodGet, detailURL, nil)
	if err != nil {
		return nil
	}
	req2.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req2.Header.Set("Referer", "https://movie.douban.com/")

	resp2, err := client.Do(req2)
	if err != nil {
		return nil
	}
	defer resp2.Body.Close()

	body2, err := io.ReadAll(resp2.Body)
	if err != nil {
		return nil
	}

	var detail map[string]interface{}
	if err := json.Unmarshal(body2, &detail); err != nil {
		return nil
	}

	subject, ok := detail["subject"].(map[string]interface{})
	if !ok {
		return nil
	}
	desc, ok := subject["short_description"].(string)
	if !ok || desc == "" {
		return nil
	}

	slog.Debug("[Douban] Got overview from Douban", "name", name)
	return &desc
}

// ========== Scrape item counters ==========
// 方案 C 后不再有 legacy ScrapeTask / ScrapeProgress:
// 全库刮削在 handler 层退化为一次 EnqueueMissingScrapeIdentify 的入队动作,
// 由 ScrapeWorker 消费 scrape_queue 实际执行。下面两个计数函数仍被
// buildEffectiveScrapeProgress 用来填 missing_count / items_total。

const missingMetadataScrapeWhere = `(overview IS NULL OR overview = '')
    AND type IN ('Movie', 'Series')`

func GetMissingScrapeCount(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM items WHERE "+missingMetadataScrapeWhere,
	).Scan(&count)
	return count, err
}

func GetTopLevelItemCount(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM items WHERE type IN ('Movie', 'Series')").Scan(&count)
	return count, err
}

// ========== JSON helpers ==========

func jsonInt64(m map[string]interface{}, key string) (int64, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	case int64:
		return n, true
	case int:
		return int64(n), true
	}
	return 0, false
}

func jsonFloat64(m map[string]interface{}, key string) *float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch n := v.(type) {
	case float64:
		if n == 0 {
			return nil
		}
		return &n
	case json.Number:
		f, err := n.Float64()
		if err != nil || f == 0 {
			return nil
		}
		return &f
	}
	return nil
}

func jsonStringNonEmpty(m map[string]interface{}, key string) *string {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	return &s
}

func jsonStringPtr(m map[string]interface{}, key string) *string {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return nil
	}
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

func parseYearPrefix(dateStr string) int {
	if len(dateStr) < 4 {
		return 0
	}
	y := 0
	for _, c := range dateStr[:4] {
		if c < '0' || c > '9' {
			return 0
		}
		y = y*10 + int(c-'0')
	}
	return y
}

// platformAliases maps known studio/network names to canonical platform names.
var platformAliases = map[string]string{
	"netflix":            "Netflix",
	"hbo":                "HBO",
	"hbo max":            "HBO",
	"max":                "HBO",
	"disney+":            "Disney+",
	"disney plus":        "Disney+",
	"apple tv+":          "Apple TV+",
	"apple tv":           "Apple TV+",
	"amazon":             "Amazon",
	"amazon studios":     "Amazon",
	"amazon prime video": "Amazon",
	"prime video":        "Amazon",
	"hulu":               "Hulu",
	"paramount+":         "Paramount+",
	"paramount plus":     "Paramount+",
	"peacock":            "Peacock",
	"showtime":           "Showtime",
	"starz":              "Starz",
	"crunchyroll":        "Crunchyroll",
	"fx":                 "FX",
	"fx productions":     "FX",
	"abc":                "ABC",
	"nbc":                "NBC",
	"cbs":                "CBS",
	"the cw":             "The CW",
	"bbc":                "BBC",
	"bbc one":            "BBC",
	"bbc two":            "BBC",
	"itv":                "ITV",
}

func canonicalPlatformAlias(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if canonical, ok := platformAliases[strings.ToLower(name)]; ok {
		return canonical
	}
	return models.CanonicalPlatformName(name)
}

func extractPlatformCandidates(details map[string]interface{}, itemType string) []string {
	var candidates []string
	if itemType == "Series" || itemType == "Episode" || itemType == "Season" {
		if networks, ok := details["networks"].([]interface{}); ok {
			for _, n := range networks {
				nm, ok := n.(map[string]interface{})
				if !ok {
					continue
				}
				name, ok := nm["name"].(string)
				if ok && strings.TrimSpace(name) != "" {
					candidates = append(candidates, name)
				}
			}
		}
	}
	if companies, ok := details["production_companies"].([]interface{}); ok {
		for _, c := range companies {
			cm, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			name, ok := cm["name"].(string)
			if ok && strings.TrimSpace(name) != "" {
				candidates = append(candidates, name)
			}
		}
	}
	return candidates
}

// ExtractPlatform extracts a canonical platform name from TMDB details.
func ExtractPlatform(details map[string]interface{}, itemType string) *string {
	for _, candidate := range extractPlatformCandidates(details, itemType) {
		if canonical := canonicalPlatformAlias(candidate); canonical != "" {
			return &canonical
		}
	}
	return nil
}
