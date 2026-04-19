package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyms/internal/services/scraper"
)

const tvdbBaseURL = "https://api4.thetvdb.com/v4"

// TVDBProvider 对接 TVDB v4 API。需要 tvdb_api_key,否则 BuildScrapeAggregator
// 不会注册本 provider。认证走 /login → Bearer token,token 缓存 1 小时。
type TVDBProvider struct {
	httpClient *http.Client
	apiKey     string
	pin        string
	gate       *scraper.Gate

	mu        sync.Mutex
	token     string
	tokenExpAt time.Time
}

func NewTVDBProvider(client *http.Client, apiKey, pin string) *TVDBProvider {
	if client == nil {
		client = http.DefaultClient
	}
	return &TVDBProvider{
		httpClient: client,
		apiKey:     strings.TrimSpace(apiKey),
		pin:        strings.TrimSpace(pin),
		gate: scraper.NewGate(scraper.GateConfig{
			RPS:        10,
			Burst:      20,
			WindowSpan: 5 * time.Minute,
			MinSamples: 15,
			ErrorRate:  0.5,
			Cooldown:   10 * time.Minute,
		}),
	}
}

func (p *TVDBProvider) Name() string                      { return "tvdb" }
func (p *TVDBProvider) Priority() int                     { return 2 }
func (p *TVDBProvider) Supports(t scraper.MediaType) bool { return t == scraper.MediaSeries }

func (p *TVDBProvider) Search(ctx context.Context, t scraper.MediaType, q scraper.Query) ([]scraper.Candidate, error) {
	if !p.Supports(t) {
		return nil, nil
	}
	keyword := firstNonBlank(q.OriginalTitle, q.Title)
	if keyword == "" {
		return nil, nil
	}
	if err := p.gate.Wait(ctx); err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/search?query=%s&type=series&limit=10", tvdbBaseURL, url.QueryEscape(keyword))
	if q.Year != nil {
		endpoint += fmt.Sprintf("&year=%d", *q.Year)
	}
	var payload struct {
		Data []struct {
			TVDBID       string   `json:"tvdb_id"`
			Name         string   `json:"name"`
			Translations map[string]string `json:"translations"`
			Overview     string   `json:"overview"`
			Year         string   `json:"year"`
			ImageURL     string   `json:"image_url"`
			RemoteIDs    []struct {
				ID         string `json:"id"`
				SourceName string `json:"sourceName"`
				Type       int    `json:"type"`
			} `json:"remote_ids"`
		} `json:"data"`
	}
	if err := p.doGet(ctx, endpoint, &payload); err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	p.gate.Observe(true)
	out := make([]scraper.Candidate, 0, len(payload.Data))
	for _, d := range payload.Data {
		id := strings.TrimSpace(d.TVDBID)
		if id == "" || id == "0" {
			continue
		}
		cand := scraper.Candidate{
			ProviderID:    id,
			Title:         pickTranslatedName(d.Translations, d.Name),
			OriginalTitle: strings.TrimSpace(d.Name),
			PosterURL:     strings.TrimSpace(d.ImageURL),
			ExternalIDs:   map[string]string{"tvdb": id},
		}
		if y, err := strconv.Atoi(strings.TrimSpace(d.Year)); err == nil && y > 0 {
			v := int32(y)
			cand.Year = &v
		}
		for _, rid := range d.RemoteIDs {
			switch strings.ToLower(rid.SourceName) {
			case "imdb":
				cand.ExternalIDs["imdb"] = strings.TrimSpace(rid.ID)
			case "themoviedb", "tmdb":
				cand.ExternalIDs["tmdb"] = strings.TrimSpace(rid.ID)
			}
		}
		out = append(out, cand)
	}
	return out, nil
}

func (p *TVDBProvider) FindByExternalID(ctx context.Context, kind, id string) (string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	id = strings.TrimSpace(id)
	if kind == "tvdb" {
		return id, nil
	}
	if id == "" {
		return "", nil
	}
	// TVDB v4 提供 /search/remoteid/{id} 端点
	if kind != "imdb" && kind != "tmdb" && kind != "themoviedb" {
		return "", nil
	}
	if err := p.gate.Wait(ctx); err != nil {
		return "", err
	}
	endpoint := fmt.Sprintf("%s/search/remoteid/%s", tvdbBaseURL, url.PathEscape(id))
	var payload struct {
		Data []struct {
			Series struct {
				ID int64 `json:"id"`
			} `json:"series"`
			Movie struct {
				ID int64 `json:"id"`
			} `json:"movie"`
		} `json:"data"`
	}
	if err := p.doGet(ctx, endpoint, &payload); err != nil {
		p.gate.Observe(false)
		return "", err
	}
	p.gate.Observe(true)
	for _, d := range payload.Data {
		if d.Series.ID > 0 {
			return strconv.FormatInt(d.Series.ID, 10), nil
		}
	}
	return "", nil
}

func (p *TVDBProvider) GetByID(ctx context.Context, t scraper.MediaType, id string) (*scraper.Details, error) {
	if !p.Supports(t) || strings.TrimSpace(id) == "" {
		return nil, nil
	}
	if err := p.gate.Wait(ctx); err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/series/%s/extended?meta=translations&short=true", tvdbBaseURL, url.PathEscape(strings.TrimSpace(id)))
	var payload struct {
		Data struct {
			ID           int64  `json:"id"`
			Name         string `json:"name"`
			Slug         string `json:"slug"`
			Overview     string `json:"overview"`
			FirstAired   string `json:"firstAired"`
			Image        string `json:"image"`
			Artworks     []struct {
				ID       int64  `json:"id"`
				Image    string `json:"image"`
				Type     int    `json:"type"`
				Language string `json:"language"`
				Season   *int32 `json:"seasonId"`
			} `json:"artworks"`
			Seasons []struct {
				ID     int64  `json:"id"`
				Number int32  `json:"number"`
				Image  string `json:"image"`
			} `json:"seasons"`
			Genres []struct {
				Name string `json:"name"`
			} `json:"genres"`
			Networks []struct {
				Name string `json:"name"`
			} `json:"networks"`
			RemoteIDs []struct {
				ID         string `json:"id"`
				SourceName string `json:"sourceName"`
			} `json:"remoteIds"`
			Translations map[string]struct {
				Name     string `json:"name"`
				Overview string `json:"overview"`
				Language string `json:"language"`
			} `json:"translations"`
		} `json:"data"`
	}
	if err := p.doGet(ctx, endpoint, &payload); err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	p.gate.Observe(true)
	d := &scraper.Details{
		Provider:    p.Name(),
		ProviderID:  strings.TrimSpace(id),
		ExternalIDs: map[string]string{"tvdb": strings.TrimSpace(id)},
	}
	d.OriginalTitle = strings.TrimSpace(payload.Data.Name)
	d.Title = d.OriginalTitle
	// zh/zho 中文翻译优先
	for _, lang := range []string{"zho", "zh", "zhs", "cmn"} {
		if tr, ok := payload.Data.Translations[lang]; ok {
			if name := strings.TrimSpace(tr.Name); name != "" {
				d.Title = name
			}
			if ov := strings.TrimSpace(tr.Overview); ov != "" && d.Overview == "" {
				d.Overview = ov
			}
			break
		}
	}
	if d.Overview == "" {
		d.Overview = strings.TrimSpace(payload.Data.Overview)
	}
	if s := strings.TrimSpace(payload.Data.FirstAired); s != "" {
		d.Premiered = s
		if len(s) >= 4 {
			if y, err := strconv.Atoi(s[:4]); err == nil && y > 0 {
				v := int32(y)
				d.Year = &v
			}
		}
	}
	if img := strings.TrimSpace(payload.Data.Image); img != "" {
		d.PosterURLs = append(d.PosterURLs, img)
	}
	// Artwork type: 2=series poster, 3=series backdrop, 7=season poster, 22=series clearlogo (v4)
	d.SeasonPosters = make(map[int32]string)
	for _, a := range payload.Data.Artworks {
		url := strings.TrimSpace(a.Image)
		if url == "" {
			continue
		}
		switch a.Type {
		case 2:
			d.PosterURLs = append(d.PosterURLs, url)
		case 3:
			d.BackdropURLs = append(d.BackdropURLs, url)
		}
	}
	for _, s := range payload.Data.Seasons {
		img := strings.TrimSpace(s.Image)
		if img == "" || s.Number <= 0 {
			continue
		}
		if _, exists := d.SeasonPosters[s.Number]; !exists {
			d.SeasonPosters[s.Number] = img
		}
	}
	for _, g := range payload.Data.Genres {
		if name := strings.TrimSpace(g.Name); name != "" {
			d.Genres = append(d.Genres, name)
		}
	}
	for _, n := range payload.Data.Networks {
		if name := strings.TrimSpace(n.Name); name != "" {
			d.Platforms = append(d.Platforms, name)
		}
	}
	for _, rid := range payload.Data.RemoteIDs {
		src := strings.ToLower(rid.SourceName)
		if src == "imdb" {
			d.ExternalIDs["imdb"] = strings.TrimSpace(rid.ID)
		}
		if src == "themoviedb" || src == "tmdb" {
			d.ExternalIDs["tmdb"] = strings.TrimSpace(rid.ID)
		}
	}
	if len(d.SeasonPosters) == 0 {
		d.SeasonPosters = nil
	}
	return d, nil
}

// doGet 统一处理 Bearer 认证 + JSON 解码。401 时强制重刷 token 再试一次。
func (p *TVDBProvider) doGet(ctx context.Context, endpoint string, dest any) error {
	token, err := p.ensureToken(ctx)
	if err != nil {
		return err
	}
	resp, err := p.callOnce(ctx, endpoint, token)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		p.invalidateToken()
		token, err = p.ensureToken(ctx)
		if err != nil {
			return err
		}
		resp, err = p.callOnce(ctx, endpoint, token)
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tvdb HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

func (p *TVDBProvider) callOnce(ctx context.Context, endpoint, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	return p.httpClient.Do(req)
}

func (p *TVDBProvider) ensureToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.token != "" && time.Now().Before(p.tokenExpAt) {
		return p.token, nil
	}
	body := map[string]string{"apikey": p.apiKey}
	if p.pin != "" {
		body["pin"] = p.pin
	}
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tvdbBaseURL+"/login", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("tvdb login HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	tok := strings.TrimSpace(payload.Data.Token)
	if tok == "" {
		return "", fmt.Errorf("tvdb login empty token")
	}
	p.token = tok
	p.tokenExpAt = time.Now().Add(60 * time.Minute)
	return tok, nil
}

func (p *TVDBProvider) invalidateToken() {
	p.mu.Lock()
	p.token = ""
	p.tokenExpAt = time.Time{}
	p.mu.Unlock()
}

func pickTranslatedName(translations map[string]string, fallback string) string {
	for _, lang := range []string{"zho", "zh", "zhs", "cmn"} {
		if name := strings.TrimSpace(translations[lang]); name != "" {
			return name
		}
	}
	return strings.TrimSpace(fallback)
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
