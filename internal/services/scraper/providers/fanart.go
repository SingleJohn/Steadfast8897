package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fyms/internal/services/scraper"
)

// FanartProvider 只用于图片字段补充(poster / backdrop / season poster)。
// 它不参与识别:Search 返回空,FindByExternalID 仅对 tmdb/tvdb 生效。
// GetByID 接受带前缀的 id:"tmdb:<id>" 或 "tvdb:<id>"。
type FanartProvider struct {
	httpClient *http.Client
	apiKey     string
	gate       *scraper.Gate
}

const fanartBaseURL = "https://webservice.fanart.tv/v3"

func NewFanartProvider(client *http.Client, apiKey string) *FanartProvider {
	if client == nil {
		client = http.DefaultClient
	}
	return &FanartProvider{
		httpClient: client,
		apiKey:     strings.TrimSpace(apiKey),
		gate: scraper.NewGate(scraper.GateConfig{
			RPS:        5,
			Burst:      10,
			WindowSpan: 5 * time.Minute,
			MinSamples: 10,
			ErrorRate:  0.5,
			Cooldown:   10 * time.Minute,
		}),
	}
}

func (p *FanartProvider) Name() string                      { return "fanart" }
func (p *FanartProvider) Priority() int                     { return 5 }
func (p *FanartProvider) Supports(t scraper.MediaType) bool { return t == scraper.MediaMovie || t == scraper.MediaSeries }

// Search 不用于识别,始终返回空。Fanart 不暴露搜索能力。
func (p *FanartProvider) Search(ctx context.Context, t scraper.MediaType, q scraper.Query) ([]scraper.Candidate, error) {
	return nil, nil
}

// FindByExternalID 把外部 ID 转成带前缀的 Fanart 内部 ID(GetByID 吃这个前缀)。
func (p *FanartProvider) FindByExternalID(ctx context.Context, kind, id string) (string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	id = strings.TrimSpace(id)
	if id == "" {
		return "", nil
	}
	switch kind {
	case "tmdb":
		return "tmdb:" + id, nil
	case "tvdb":
		return "tvdb:" + id, nil
	}
	return "", nil
}

func (p *FanartProvider) GetByID(ctx context.Context, t scraper.MediaType, id string) (*scraper.Details, error) {
	if !p.Supports(t) || p.apiKey == "" {
		return nil, nil
	}
	prefix, extID := splitFanartID(id)
	if extID == "" {
		return nil, nil
	}
	if err := p.gate.Wait(ctx); err != nil {
		return nil, err
	}
	var endpoint string
	switch t {
	case scraper.MediaMovie:
		if prefix != "" && prefix != "tmdb" {
			return nil, nil
		}
		endpoint = fmt.Sprintf("%s/movies/%s?api_key=%s", fanartBaseURL, url.PathEscape(extID), url.QueryEscape(p.apiKey))
		return p.fetchMovie(ctx, endpoint, extID)
	case scraper.MediaSeries:
		if prefix != "" && prefix != "tvdb" {
			return nil, nil
		}
		endpoint = fmt.Sprintf("%s/tv/%s?api_key=%s", fanartBaseURL, url.PathEscape(extID), url.QueryEscape(p.apiKey))
		return p.fetchSeries(ctx, endpoint, extID)
	}
	return nil, nil
}

type fanartArt struct {
	URL    string `json:"url"`
	Lang   string `json:"lang"`
	Season string `json:"season"`
	Likes  string `json:"likes"`
}

func (p *FanartProvider) fetchMovie(ctx context.Context, endpoint, id string) (*scraper.Details, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		p.gate.Observe(true)
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		p.gate.Observe(false)
		return nil, fmt.Errorf("fanart HTTP %d", resp.StatusCode)
	}
	var payload struct {
		MoviePoster     []fanartArt `json:"movieposter"`
		MovieBackground []fanartArt `json:"moviebackground"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	p.gate.Observe(true)
	d := &scraper.Details{
		Provider:    p.Name(),
		ProviderID:  "tmdb:" + id,
		ExternalIDs: map[string]string{"tmdb": id},
	}
	d.PosterURLs = fanartURLs(payload.MoviePoster)
	d.BackdropURLs = fanartURLs(payload.MovieBackground)
	if len(d.PosterURLs) == 0 && len(d.BackdropURLs) == 0 {
		return nil, nil
	}
	return d, nil
}

func (p *FanartProvider) fetchSeries(ctx context.Context, endpoint, id string) (*scraper.Details, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		p.gate.Observe(true)
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		p.gate.Observe(false)
		return nil, fmt.Errorf("fanart HTTP %d", resp.StatusCode)
	}
	var payload struct {
		TVPoster     []fanartArt `json:"tvposter"`
		ShowBackground []fanartArt `json:"showbackground"`
		SeasonPoster []fanartArt `json:"seasonposter"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	p.gate.Observe(true)
	d := &scraper.Details{
		Provider:    p.Name(),
		ProviderID:  "tvdb:" + id,
		ExternalIDs: map[string]string{"tvdb": id},
	}
	d.PosterURLs = fanartURLs(payload.TVPoster)
	d.BackdropURLs = fanartURLs(payload.ShowBackground)
	if len(payload.SeasonPoster) > 0 {
		d.SeasonPosters = make(map[int32]string)
		for _, art := range payload.SeasonPoster {
			u := strings.TrimSpace(art.URL)
			if u == "" {
				continue
			}
			season, err := strconv.Atoi(strings.TrimSpace(art.Season))
			if err != nil || season <= 0 {
				continue
			}
			s := int32(season)
			if _, exists := d.SeasonPosters[s]; !exists {
				d.SeasonPosters[s] = u
			}
		}
		if len(d.SeasonPosters) == 0 {
			d.SeasonPosters = nil
		}
	}
	if len(d.PosterURLs) == 0 && len(d.BackdropURLs) == 0 && len(d.SeasonPosters) == 0 {
		return nil, nil
	}
	return d, nil
}

func splitFanartID(s string) (string, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	if idx := strings.Index(s, ":"); idx > 0 {
		return strings.ToLower(s[:idx]), strings.TrimSpace(s[idx+1:])
	}
	return "", s
}

func fanartURLs(arts []fanartArt) []string {
	out := make([]string, 0, len(arts))
	for _, a := range arts {
		if u := strings.TrimSpace(a.URL); u != "" {
			out = append(out, u)
		}
	}
	return out
}
