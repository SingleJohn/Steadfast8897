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
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/models"
	"fyms/internal/services/scraper"
)

// sharedScrapeCache 在 main.go 启动时通过 SetScrapeCache 注入；
// 用于 Matcher 的搜索结果缓存。未注入时 Matcher 自动退化为无缓存。
var sharedScrapeCache scraper.Cache

func SetScrapeCache(c scraper.Cache) {
	sharedScrapeCache = c
}

// identifyFailureCooldown 是识别失败后重试的冷却时长。
const identifyFailureCooldown = 24 * time.Hour

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

func (c *TmdbClient) tmdbGet(ctx context.Context, urlTemplate string) (map[string]interface{}, error) {
	maxRetries := len(c.apiKeys)
	for attempt := 0; attempt <= maxRetries; attempt++ {
		key := c.nextKey()
		reqURL := strings.ReplaceAll(urlTemplate, "{API_KEY}", key)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			slog.Debug("[TMDB] Request error", "error", err)
			return nil, err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read body: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
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
			slog.Debug("[TMDB] HTTP error", "status", resp.StatusCode)
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("json decode: %w", err)
		}
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
func (c *TmdbClient) SearchMovieMulti(ctx context.Context, name string, year *int32) ([]map[string]interface{}, error) {
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

// SearchTVMulti returns up to 20 TMDB TV search results.
func (c *TmdbClient) SearchTVMulti(ctx context.Context, name string) ([]map[string]interface{}, error) {
	u := fmt.Sprintf("%s/search/tv?api_key={API_KEY}&language=%s&query=%s",
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
	u := fmt.Sprintf("%s/movie/%d?api_key={API_KEY}&language=%s&append_to_response=credits",
		TMDB_BASE, tmdbID, c.language)
	return c.tmdbGet(ctx, u)
}

func (c *TmdbClient) GetTVDetails(ctx context.Context, tmdbID int64) (map[string]interface{}, error) {
	u := fmt.Sprintf("%s/tv/%d?api_key={API_KEY}&language=%s&append_to_response=credits",
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
	ItemType string
	Name     string
	Year     *int32
	TmdbID   *int32
	ImdbID   *string
	FilePath *string
}

func loadScrapeItemMeta(ctx context.Context, pool *pgxpool.Pool, itemID string) (*scrapeItemMeta, error) {
	meta := &scrapeItemMeta{}
	err := pool.QueryRow(ctx,
		"SELECT type, name, production_year, tmdb_id, imdb_id, file_path FROM items WHERE id = $1::uuid", itemID,
	).Scan(&meta.ItemType, &meta.Name, &meta.Year, &meta.TmdbID, &meta.ImdbID, &meta.FilePath)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("item not found")
	}
	if err != nil {
		return nil, fmt.Errorf("query item: %w", err)
	}
	return meta, nil
}

// setIdentifyCooldown 在识别失败后把 item 标记为冷却中，避免短期重试。
func setIdentifyCooldown(ctx context.Context, pool *pgxpool.Pool, itemID string, d time.Duration) {
	interval := fmt.Sprintf("%d seconds", int(d.Seconds()))
	_, err := pool.Exec(ctx,
		"UPDATE items SET identify_attempted_at = NOW(), identify_cooldown_until = NOW() + $1::interval WHERE id = $2::uuid",
		interval, itemID)
	if err != nil {
		slog.Debug("[TMDB] set identify cooldown failed", "item_id", itemID, "error", err)
	}
}

// clearIdentifyCooldown 识别成功后清除冷却。
func clearIdentifyCooldown(ctx context.Context, pool *pgxpool.Pool, itemID string) {
	_, err := pool.Exec(ctx,
		"UPDATE items SET identify_attempted_at = NOW(), identify_cooldown_until = NULL WHERE id = $1::uuid",
		itemID)
	if err != nil {
		slog.Debug("[TMDB] clear identify cooldown failed", "item_id", itemID, "error", err)
	}
}

// buildParsedName 从 item meta + 文件路径构造 ParsedName，给 Matcher 使用。
func buildParsedName(meta *scrapeItemMeta) scraper.ParsedName {
	basis := meta.Name
	if meta.FilePath != nil && *meta.FilePath != "" {
		basis = filepath.Base(*meta.FilePath)
	}
	mode := scraper.ModeMovie
	if meta.ItemType == "Series" {
		mode = scraper.ModeSeries
	}
	parsed := scraper.Parse(basis, mode)

	// DB 侧的 year 最可信，覆盖解析结果
	if meta.Year != nil && *meta.Year > 0 {
		parsed.Year = meta.Year
	}
	if parsed.IDs == nil {
		parsed.IDs = map[string]string{}
	}
	if meta.TmdbID != nil && *meta.TmdbID > 0 && parsed.IDs["tmdb"] == "" {
		parsed.IDs["tmdb"] = strconv.Itoa(int(*meta.TmdbID))
	}
	if meta.ImdbID != nil && strings.TrimSpace(*meta.ImdbID) != "" && parsed.IDs["imdb"] == "" {
		parsed.IDs["imdb"] = strings.TrimSpace(*meta.ImdbID)
	}
	// Title 兜底：若归一化后 Title/OriginalTitle 都为空，用 items.name
	if parsed.Title == "" && parsed.OriginalTitle == "" {
		parsed.Title = meta.Name
	}
	return parsed
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
		Provider:    "tmdb",
		ProviderID:  strconv.FormatInt(id, 10),
		ExternalIDs: map[string]string{"tmdb": strconv.FormatInt(id, 10)},
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
	if merged.ExternalIDs != nil {
		if s := strings.TrimSpace(merged.ExternalIDs["imdb"]); s != "" {
			imdbID = &s
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
	ApplyNfoDataWithPlatformSource(ctx, pool, itemID, &nfo, source)

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
	details, err := fetchTMDBDetailsByID(ctx, client, meta.ItemType, int64(*meta.TmdbID))
	if err != nil {
		return nil, err
	}
	merged := scraper.MergeDetails(&scraper.Identity{
		Provider:    "tmdb",
		ProviderID:  strconv.FormatInt(int64(*meta.TmdbID), 10),
		ExternalIDs: map[string]string{"tmdb": strconv.FormatInt(int64(*meta.TmdbID), 10)},
		Score:       1,
		Source:      "tmdb_id_refresh",
	}, tmdbDetailsFromRaw(details, mediaTypeMust(meta.ItemType), int64(*meta.TmdbID)))
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
func ScrapeItemByTMDBID(ctx context.Context, pool *pgxpool.Pool, itemID string, tmdbID int64) (map[string]interface{}, error) {
	client := TmdbClientFromConfig(ctx, pool)
	if client == nil {
		return nil, fmt.Errorf("TMDB API 密钥未配置")
	}
	meta, err := loadScrapeItemMeta(ctx, pool, itemID)
	if err != nil {
		return nil, err
	}
	details, err := fetchTMDBDetailsByID(ctx, client, meta.ItemType, tmdbID)
	if err != nil {
		return nil, fmt.Errorf("获取 TMDB 详情失败: %w", err)
	}
	merged := scraper.MergeDetails(&scraper.Identity{
		Provider:    "tmdb",
		ProviderID:  strconv.FormatInt(tmdbID, 10),
		ExternalIDs: map[string]string{"tmdb": strconv.FormatInt(tmdbID, 10)},
		Score:       1,
		Source:      "manual_tmdb_id",
	}, tmdbDetailsFromRaw(details, mediaTypeMust(meta.ItemType), tmdbID))
	return applyMergedDetails(ctx, pool, itemID, client, meta.ItemType, tmdbID, merged, true, models.PlatformScanSourceSearch)
}

// SearchTMDBForItem searches TMDB for an item by custom query, returning multiple results.
func SearchTMDBForItem(ctx context.Context, pool *pgxpool.Pool, itemID, query string, year *int32) ([]map[string]interface{}, error) {
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
		return client.SearchMovieMulti(ctx, query, year)
	case "Series":
		return client.SearchTVMulti(ctx, query)
	default:
		return nil, fmt.Errorf("不支持的类型: %s", meta.ItemType)
	}
}

func ScrapeItemWithClient(ctx context.Context, pool *pgxpool.Pool, itemID string, client *TmdbClient) (map[string]interface{}, error) {
	meta, err := loadScrapeItemMeta(ctx, pool, itemID)
	if err != nil {
		return nil, err
	}

	// 已经带 TMDB ID 的 item 走直达，节省搜索阶段
	if meta.TmdbID != nil && *meta.TmdbID > 0 {
		details, ferr := fetchTMDBDetailsByID(ctx, client, meta.ItemType, int64(*meta.TmdbID))
		if ferr != nil {
			return nil, fmt.Errorf("fetch tmdb details: %w", ferr)
		}
		merged := scraper.MergeDetails(&scraper.Identity{
			Provider:    "tmdb",
			ProviderID:  strconv.FormatInt(int64(*meta.TmdbID), 10),
			ExternalIDs: map[string]string{"tmdb": strconv.FormatInt(int64(*meta.TmdbID), 10)},
			Score:       1,
			Source:      "tmdb_id_direct",
		}, tmdbDetailsFromRaw(details, mediaTypeMust(meta.ItemType), int64(*meta.TmdbID)))
		clearIdentifyCooldown(ctx, pool, itemID)
		return applyMergedDetails(ctx, pool, itemID, client, meta.ItemType, int64(*meta.TmdbID), merged, false, models.PlatformScanSourceTMDB)
	}

	mediaType, ok := mediaTypeFor(meta.ItemType)
	if !ok {
		return nil, fmt.Errorf("cannot scrape type: %s", meta.ItemType)
	}

	parsed := buildParsedName(meta)
	runtimeCfg := scraper.LoadRuntimeConfig(ctx, pool)
	agg := BuildScrapeAggregator(sharedScrapeCache, runtimeCfg, client, client.httpClient)

	ident, err := agg.Identify(ctx, parsed, mediaType)
	if err != nil {
		reason := err.Error()
		source := models.PlatformScanSourceSearch
		if errors.Is(err, scraper.ErrNoMatch) {
			if !runtimeCfg.AutoApply {
				candidates, candErr := agg.Candidates(ctx, parsed, mediaType)
				if candErr == nil && len(candidates) > 0 {
					_ = replaceIdentifyCandidates(ctx, pool, itemID, candidates)
					setIdentifyCooldown(ctx, pool, itemID, identifyFailureCooldown)
					if markErr := models.MarkPlatformScanUnidentified(ctx, pool, itemID, source, "identify queued for manual confirmation"); markErr != nil {
						slog.Warn("[TMDB] mark manual identify queue failed", "item_id", itemID, "error", markErr)
					}
					return nil, fmt.Errorf("identify queued for manual confirmation")
				}
			}
			setIdentifyCooldown(ctx, pool, itemID, identifyFailureCooldown)
			reason = "identify failed: no confident match"
			if markErr := models.MarkPlatformScanUnidentified(ctx, pool, itemID, source, reason); markErr != nil {
				slog.Warn("[TMDB] mark platform scan unidentified failed", "item_id", itemID, "error", markErr)
			}
			return nil, err
		}
		if markErr := models.MarkPlatformScanError(ctx, pool, itemID, source, reason); markErr != nil {
			slog.Warn("[TMDB] mark platform scan error failed", "item_id", itemID, "error", markErr)
		}
		return nil, err
	}

	tmdbID := resolveTMDBIDFromIdentity(ident)
	if tmdbID <= 0 {
		// 非 TMDB 源命中且无法映射到 tmdb_id:落人工确认,避免脏写 items.tmdb_id。
		if candidates, candErr := agg.Candidates(ctx, parsed, mediaType); candErr == nil && len(candidates) > 0 {
			_ = replaceIdentifyCandidates(ctx, pool, itemID, candidates)
		}
		setIdentifyCooldown(ctx, pool, itemID, identifyFailureCooldown)
		reason := fmt.Sprintf("identified by %s but no tmdb id", ident.Provider)
		if markErr := models.MarkPlatformScanUnidentified(ctx, pool, itemID, models.PlatformScanSourceSearch, reason); markErr != nil {
			slog.Warn("[TMDB] mark platform scan unidentified failed", "item_id", itemID, "error", markErr)
		}
		return nil, fmt.Errorf(reason)
	}
	merged, fillErr := agg.Fill(ctx, ident, parsed, mediaType)
	if fillErr != nil {
		return nil, fmt.Errorf("fill details: %w", fillErr)
	}
	clearIdentifyCooldown(ctx, pool, itemID)
	slog.Debug("[Matcher] matched",
		"item_id", itemID, "provider", ident.Provider, "provider_id", ident.ProviderID, "source", ident.Source, "score", ident.Score)
	return applyMergedDetails(ctx, pool, itemID, client, meta.ItemType, tmdbID, merged, true, models.PlatformScanSourceSearch)
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

func mediaTypeMust(itemType string) scraper.MediaType {
	t, _ := mediaTypeFor(itemType)
	return t
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

// ========== Scrape Task with Progress ==========

type ScrapeProgress struct {
	Status         string  `json:"status"`
	TotalItems     int64   `json:"total_items"`
	ProcessedItems int64   `json:"processed_items"`
	SuccessItems   int64   `json:"success_items"`
	FailedItems    int64   `json:"failed_items"`
	CurrentItem    *string `json:"current_item,omitempty"`
	LastError      *string `json:"last_error,omitempty"`
	Percentage     int     `json:"percentage"`
	MissingCount   int64   `json:"missing_count"`
	ItemsTotal     int64   `json:"items_total"`
}

type ScrapeTask struct {
	mu       sync.Mutex
	progress ScrapeProgress
	stopFlag atomic.Bool
}

func NewScrapeTask() *ScrapeTask {
	return &ScrapeTask{
		progress: ScrapeProgress{
			Status: "idle",
		},
	}
}

func (t *ScrapeTask) GetProgress() ScrapeProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	p := t.progress
	return p
}

func (t *ScrapeTask) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.progress.Status == "running" {
		t.stopFlag.Store(true)
		t.progress.Status = "stopping"
	}
}

const missingMetadataScrapeWhere = `(overview IS NULL OR overview = '')
    AND type IN ('Movie', 'Series')
    AND (identify_cooldown_until IS NULL OR identify_cooldown_until < NOW())`

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

func loadItemsPendingScrape(ctx context.Context, pool *pgxpool.Pool) ([]itemRow, error) {
	rows, err := pool.Query(ctx,
		"SELECT id, type, name, production_year FROM items WHERE "+missingMetadataScrapeWhere+" ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("query items: %w", err)
	}
	defer rows.Close()

	var items []itemRow
	for rows.Next() {
		var r itemRow
		if err := rows.Scan(&r.id, &r.itemType, &r.name, &r.year); err != nil {
			continue
		}
		items = append(items, r)
	}
	return items, rows.Err()
}

type itemRow struct {
	id       uuid.UUID
	itemType string
	name     string
	year     *int32
}

func (t *ScrapeTask) Start(ctx context.Context, pool *pgxpool.Pool) error {
	t.mu.Lock()
	if t.progress.Status == "running" || t.progress.Status == "stopping" {
		t.mu.Unlock()
		return fmt.Errorf("already running")
	}
	t.mu.Unlock()

	items, err := loadItemsPendingScrape(ctx, pool)
	if err != nil {
		return err
	}

	total := int64(len(items))

	t.mu.Lock()
	t.progress = ScrapeProgress{
		Status:     "running",
		TotalItems: total,
	}
	t.mu.Unlock()
	t.stopFlag.Store(false)

	go func() {
		bgCtx := context.Background()
		client := TmdbClientFromConfig(bgCtx, pool)
		if client == nil {
			t.mu.Lock()
			t.progress.Status = "error"
			errMsg := "TMDB API key not configured"
			t.progress.LastError = &errMsg
			t.mu.Unlock()
			return
		}

		for _, item := range items {
			if t.stopFlag.Load() {
				t.mu.Lock()
				t.progress.Status = "stopped"
				t.progress.CurrentItem = nil
				t.mu.Unlock()
				slog.Info("[Scrape] Stopped by user")
				return
			}

			t.mu.Lock()
			name := item.name
			t.progress.CurrentItem = &name
			t.mu.Unlock()

			_, err := ScrapeItemWithClient(bgCtx, pool, item.id.String(), client)
			t.mu.Lock()
			if err != nil {
				t.progress.FailedItems++
				errMsg := fmt.Sprintf("%s: %s", item.name, err.Error())
				t.progress.LastError = &errMsg
				slog.Debug("[Scrape] Failed", "name", item.name, "error", err)
			} else {
				t.progress.SuccessItems++
			}
			t.progress.ProcessedItems++
			if t.progress.TotalItems > 0 {
				t.progress.Percentage = int(t.progress.ProcessedItems * 100 / t.progress.TotalItems)
			}
			t.mu.Unlock()

			time.Sleep(300 * time.Millisecond)
		}

		t.mu.Lock()
		slog.Info("[Scrape] Done",
			"success", t.progress.SuccessItems,
			"total", t.progress.TotalItems,
			"failed", t.progress.FailedItems)
		t.progress.Status = "completed"
		t.progress.CurrentItem = nil
		t.mu.Unlock()

		merged, merr := models.MergeMultiVersionItems(bgCtx, pool)
		if merr != nil {
			slog.Error("[Scrape] MergeVersions failed", "error", merr)
		} else if merged > 0 {
			slog.Info("[Scrape] MergeVersions completed", "merged", merged)
		}
	}()

	return nil
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
