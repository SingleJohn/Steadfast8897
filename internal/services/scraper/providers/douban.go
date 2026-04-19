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

type DoubanProvider struct {
	httpClient *http.Client
	gate       *scraper.Gate
}

func NewDoubanProvider(client *http.Client) *DoubanProvider {
	if client == nil {
		client = http.DefaultClient
	}
	return &DoubanProvider{
		httpClient: client,
		gate: scraper.NewGate(scraper.GateConfig{
			RPS:        1,
			Burst:      2,
			MinGap:     500 * time.Millisecond,
			WindowSpan: 5 * time.Minute,
			MinSamples: 10,
			ErrorRate:  0.5,
			Cooldown:   10 * time.Minute,
		}),
	}
}

func (p *DoubanProvider) Name() string                      { return "douban" }
func (p *DoubanProvider) Priority() int                     { return 4 }
func (p *DoubanProvider) Supports(t scraper.MediaType) bool { return t == scraper.MediaMovie || t == scraper.MediaSeries }

func (p *DoubanProvider) Search(ctx context.Context, t scraper.MediaType, q scraper.Query) ([]scraper.Candidate, error) {
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
	endpoint := fmt.Sprintf("https://movie.douban.com/j/subject_suggest?q=%s", url.QueryEscape(keyword))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://movie.douban.com/")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		p.gate.Observe(false)
		return nil, fmt.Errorf("douban search HTTP %d", resp.StatusCode)
	}
	var payload []struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Year     string `json:"year"`
		Img      string `json:"img"`
		SubTitle string `json:"sub_title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	p.gate.Observe(true)
	out := make([]scraper.Candidate, 0, len(payload))
	for _, item := range payload {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		cand := scraper.Candidate{
			ProviderID:    strings.TrimSpace(item.ID),
			Title:         strings.TrimSpace(item.Title),
			OriginalTitle: strings.TrimSpace(item.SubTitle),
			PosterURL:     strings.TrimSpace(item.Img),
			ExternalIDs:   map[string]string{"douban": strings.TrimSpace(item.ID)},
		}
		if len(item.Year) >= 4 {
			if y, err := strconv.Atoi(item.Year[:4]); err == nil {
				v := int32(y)
				cand.Year = &v
			}
		}
		out = append(out, cand)
	}
	return out, nil
}

func (p *DoubanProvider) FindByExternalID(ctx context.Context, kind, id string) (string, error) {
	if strings.ToLower(strings.TrimSpace(kind)) == "douban" {
		return strings.TrimSpace(id), nil
	}
	return "", nil
}

func (p *DoubanProvider) GetByID(ctx context.Context, t scraper.MediaType, id string) (*scraper.Details, error) {
	if strings.TrimSpace(id) == "" {
		return nil, nil
	}
	if err := p.gate.Wait(ctx); err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("https://movie.douban.com/j/subject_abstract?subject_id=%s", url.QueryEscape(strings.TrimSpace(id)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://movie.douban.com/")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		p.gate.Observe(false)
		return nil, fmt.Errorf("douban detail HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Subject struct {
			ID               string `json:"id"`
			Title            string `json:"title"`
			CardSubtitle     string `json:"card_subtitle"`
			ShortDescription string `json:"short_description"`
			Pic              struct {
				Large string `json:"large"`
			} `json:"pic"`
		} `json:"subject"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	p.gate.Observe(true)
	d := &scraper.Details{
		Provider:    p.Name(),
		ProviderID:  strings.TrimSpace(id),
		ExternalIDs: map[string]string{"douban": strings.TrimSpace(id)},
		Title:       strings.TrimSpace(payload.Subject.Title),
		Overview:    strings.TrimSpace(payload.Subject.ShortDescription),
	}
	if strings.TrimSpace(payload.Subject.Pic.Large) != "" {
		d.PosterURLs = []string{strings.TrimSpace(payload.Subject.Pic.Large)}
	}
	return d, nil
}
