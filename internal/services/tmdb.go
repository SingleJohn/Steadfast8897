package services

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/models"
)

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
}

func loadScrapeItemMeta(ctx context.Context, pool *pgxpool.Pool, itemID string) (*scrapeItemMeta, error) {
	meta := &scrapeItemMeta{}
	err := pool.QueryRow(ctx,
		"SELECT type, name, production_year, tmdb_id FROM items WHERE id = $1::uuid", itemID,
	).Scan(&meta.ItemType, &meta.Name, &meta.Year, &meta.TmdbID)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("item not found")
	}
	if err != nil {
		return nil, fmt.Errorf("query item: %w", err)
	}
	return meta, nil
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

func searchTMDBDetails(ctx context.Context, client *TmdbClient, itemType, name string, year *int32) (int64, map[string]interface{}, error) {
	switch itemType {
	case "Movie":
		search, err := client.SearchMovie(ctx, name, year)
		if err != nil {
			return 0, nil, fmt.Errorf("movie not found on TMDB: %w", err)
		}
		tid, ok := jsonInt64(search, "id")
		if !ok {
			return 0, nil, fmt.Errorf("no TMDB ID")
		}
		details, err := client.GetMovieDetails(ctx, tid)
		if err != nil {
			return 0, nil, fmt.Errorf("failed to get movie details: %w", err)
		}
		return tid, details, nil
	case "Series":
		search, err := client.SearchTV(ctx, name)
		if err != nil {
			return 0, nil, fmt.Errorf("TV show not found on TMDB: %w", err)
		}
		tid, ok := jsonInt64(search, "id")
		if !ok {
			return 0, nil, fmt.Errorf("no TMDB ID")
		}
		details, err := client.GetTVDetails(ctx, tid)
		if err != nil {
			return 0, nil, fmt.Errorf("failed to get TV details: %w", err)
		}
		return tid, details, nil
	default:
		return 0, nil, fmt.Errorf("cannot scrape type: %s", itemType)
	}
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
	return applyTMDBDetails(ctx, pool, itemID, client, meta.ItemType, meta.Name, int64(*meta.TmdbID), details, false, models.PlatformScanSourceTMDB)
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
	return applyTMDBDetails(ctx, pool, itemID, client, meta.ItemType, meta.Name, tmdbID, details, true, models.PlatformScanSourceSearch)
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
	tmdbID, details, err := searchTMDBDetails(ctx, client, meta.ItemType, meta.Name, meta.Year)
	if err != nil {
		if markErr := models.MarkPlatformScanError(ctx, pool, itemID, models.PlatformScanSourceSearch, err.Error()); markErr != nil {
			slog.Warn("[TMDB] mark platform scan error failed", "item_id", itemID, "error", markErr)
		}
		return nil, err
	}
	return applyTMDBDetails(ctx, pool, itemID, client, meta.ItemType, meta.Name, tmdbID, details, true, models.PlatformScanSourceSearch)
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

const missingMetadataScrapeWhere = `(overview IS NULL OR overview = '') AND type IN ('Movie', 'Series')`

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
