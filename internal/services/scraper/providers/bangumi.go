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

const bangumiBaseURL = "https://api.bgm.tv"

type BangumiProvider struct {
	httpClient *http.Client
	userAgent  string
	gate       *scraper.Gate
}

func NewBangumiProvider(client *http.Client, ua string) *BangumiProvider {
	if client == nil {
		client = http.DefaultClient
	}
	return &BangumiProvider{
		httpClient: client,
		userAgent:  strings.TrimSpace(ua),
		gate: scraper.NewGate(scraper.GateConfig{
			RPS:        2,
			Burst:      4,
			MinGap:     200 * time.Millisecond,
			WindowSpan: 5 * time.Minute,
			MinSamples: 10,
			ErrorRate:  0.5,
			Cooldown:   10 * time.Minute,
		}),
	}
}

func (p *BangumiProvider) Name() string                       { return "bangumi" }
func (p *BangumiProvider) Priority() int                      { return 3 }
func (p *BangumiProvider) Supports(t scraper.MediaType) bool  { return t == scraper.MediaSeries }

func (p *BangumiProvider) Search(ctx context.Context, t scraper.MediaType, q scraper.Query) ([]scraper.Candidate, error) {
	if !p.Supports(t) {
		return nil, nil
	}
	keyword := strings.TrimSpace(q.Title)
	if keyword == "" {
		keyword = strings.TrimSpace(q.OriginalTitle)
	}
	if keyword == "" {
		return nil, nil
	}
	if err := p.gate.Wait(ctx); err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/search/subject/%s?type=2", bangumiBaseURL, url.PathEscape(keyword))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	if p.userAgent != "" {
		req.Header.Set("User-Agent", p.userAgent)
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		p.gate.Observe(false)
		return nil, fmt.Errorf("bangumi search HTTP %d", resp.StatusCode)
	}
	var payload struct {
		List []struct {
			ID     int64   `json:"id"`
			Name   string  `json:"name"`
			NameCN string  `json:"name_cn"`
			Date   string  `json:"date"`
			Score  float64 `json:"score"`
			Images struct {
				Large  string `json:"large"`
				Common string `json:"common"`
			} `json:"images"`
		} `json:"list"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	p.gate.Observe(true)
	out := make([]scraper.Candidate, 0, len(payload.List))
	for _, item := range payload.List {
		if item.ID <= 0 {
			continue
		}
		cand := scraper.Candidate{
			ProviderID:    strconv.FormatInt(item.ID, 10),
			Title:         strings.TrimSpace(item.NameCN),
			OriginalTitle: strings.TrimSpace(item.Name),
			Popularity:    item.Score,
			ExternalIDs:   map[string]string{"bangumi": strconv.FormatInt(item.ID, 10)},
		}
		if strings.TrimSpace(item.Images.Large) != "" {
			cand.PosterURL = strings.TrimSpace(item.Images.Large)
		} else if strings.TrimSpace(item.Images.Common) != "" {
			cand.PosterURL = strings.TrimSpace(item.Images.Common)
		}
		if cand.Title == "" {
			cand.Title = cand.OriginalTitle
		}
		if len(item.Date) >= 4 {
			if y, err := strconv.Atoi(item.Date[:4]); err == nil {
				v := int32(y)
				cand.Year = &v
			}
		}
		out = append(out, cand)
	}
	return out, nil
}

func (p *BangumiProvider) FindByExternalID(ctx context.Context, kind, id string) (string, error) {
	if strings.ToLower(strings.TrimSpace(kind)) == "bangumi" {
		return strings.TrimSpace(id), nil
	}
	return "", nil
}

func (p *BangumiProvider) GetByID(ctx context.Context, t scraper.MediaType, id string) (*scraper.Details, error) {
	if !p.Supports(t) || strings.TrimSpace(id) == "" {
		return nil, nil
	}
	if err := p.gate.Wait(ctx); err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/v0/subjects/%s", bangumiBaseURL, url.PathEscape(strings.TrimSpace(id)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	if p.userAgent != "" {
		req.Header.Set("User-Agent", p.userAgent)
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		p.gate.Observe(false)
		return nil, fmt.Errorf("bangumi detail HTTP %d", resp.StatusCode)
	}
	var payload struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		NameCN  string `json:"name_cn"`
		Summary string `json:"summary"`
		Date    string `json:"date"`
		Rating  struct {
			Score float64 `json:"score"`
		} `json:"rating"`
		Images struct {
			Large  string `json:"large"`
			Common string `json:"common"`
		} `json:"images"`
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	p.gate.Observe(true)
	d := &scraper.Details{
		Provider:      p.Name(),
		ProviderID:    strings.TrimSpace(id),
		ExternalIDs:   map[string]string{"bangumi": strings.TrimSpace(id)},
		Title:         strings.TrimSpace(payload.NameCN),
		OriginalTitle: strings.TrimSpace(payload.Name),
		Overview:      strings.TrimSpace(payload.Summary),
	}
	if d.Title == "" {
		d.Title = d.OriginalTitle
	}
	if payload.Rating.Score > 0 {
		v := payload.Rating.Score
		d.Rating = &v
	}
	if len(payload.Date) >= 4 {
		if y, err := strconv.Atoi(payload.Date[:4]); err == nil {
			v := int32(y)
			d.Year = &v
		}
		d.Premiered = payload.Date
	}
	if strings.TrimSpace(payload.Images.Large) != "" {
		d.PosterURLs = []string{strings.TrimSpace(payload.Images.Large)}
	} else if strings.TrimSpace(payload.Images.Common) != "" {
		d.PosterURLs = []string{strings.TrimSpace(payload.Images.Common)}
	}
	for _, tag := range payload.Tags {
		if name := strings.TrimSpace(tag.Name); name != "" {
			d.Genres = append(d.Genres, name)
		}
	}
	return d, nil
}
