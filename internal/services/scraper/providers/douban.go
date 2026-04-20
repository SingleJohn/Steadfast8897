package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"fyms/internal/services/scraper"
)

const (
	doubanUAFallback = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

type DoubanProvider struct {
	httpClient *http.Client
	userAgent  string
	cookie     string
	gate       *scraper.Gate
}

func NewDoubanProvider(client *http.Client, ua, cookie string) *DoubanProvider {
	if client == nil {
		client = http.DefaultClient
	}
	ua = strings.TrimSpace(ua)
	if ua == "" {
		ua = doubanUAFallback
	}
	return &DoubanProvider{
		httpClient: client,
		userAgent:  ua,
		cookie:     strings.TrimSpace(cookie),
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

func (p *DoubanProvider) Name() string  { return "douban" }
func (p *DoubanProvider) Priority() int { return 4 }
func (p *DoubanProvider) Supports(t scraper.MediaType) bool {
	return t == scraper.MediaMovie || t == scraper.MediaSeries
}

// ---------- Search ----------

func (p *DoubanProvider) Search(ctx context.Context, t scraper.MediaType, q scraper.Query) ([]scraper.Candidate, error) {
	keyword := strings.TrimSpace(q.Title)
	if keyword == "" {
		keyword = strings.TrimSpace(q.OriginalTitle)
	}
	if keyword == "" {
		return nil, nil
	}
	endpoint := fmt.Sprintf("https://movie.douban.com/j/subject_suggest?q=%s", url.QueryEscape(keyword))
	resp, err := p.doRequest(ctx, endpoint)
	if err != nil {
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
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		cand := scraper.Candidate{
			ProviderID:    id,
			Title:         strings.TrimSpace(item.Title),
			OriginalTitle: strings.TrimSpace(item.SubTitle),
			PosterURL:     strings.TrimSpace(item.Img),
			ExternalIDs:   map[string]string{"douban": id},
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

// ---------- FindByExternalID ----------

func (p *DoubanProvider) FindByExternalID(ctx context.Context, kind, id string) (string, error) {
	if strings.ToLower(strings.TrimSpace(kind)) == "douban" {
		return strings.TrimSpace(id), nil
	}
	return "", nil
}

// ---------- GetByID ----------

func (p *DoubanProvider) GetByID(ctx context.Context, t scraper.MediaType, id string) (*scraper.Details, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	d, err := p.fetchDetailHTML(ctx, id)
	if err != nil || d == nil || strings.TrimSpace(d.Title) == "" {
		// HTML 被反爬/解析失败 → 降级 JSON 接口,只能拿基础字段
		jd, jerr := p.fetchDetailJSON(ctx, id)
		if jerr == nil && jd != nil {
			return jd, nil
		}
		if err != nil {
			return nil, err
		}
		return jd, jerr
	}
	return d, nil
}

// ---------- HTTP 请求包装 ----------

func (p *DoubanProvider) doRequest(ctx context.Context, endpoint string) (*http.Response, error) {
	if err := p.gate.Wait(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	req.Header.Set("User-Agent", p.userAgent)
	req.Header.Set("Referer", "https://movie.douban.com/")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	if p.cookie != "" {
		req.Header.Set("Cookie", p.cookie)
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	return resp, nil
}

// ---------- HTML 详情 ----------

func (p *DoubanProvider) fetchDetailHTML(ctx context.Context, id string) (*scraper.Details, error) {
	endpoint := fmt.Sprintf("https://movie.douban.com/subject/%s/", url.PathEscape(id))
	resp, err := p.doRequest(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		p.gate.Observe(false)
		return nil, fmt.Errorf("douban detail HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		p.gate.Observe(false)
		return nil, err
	}
	p.gate.Observe(true)

	ld := extractLDJSON(body)
	d := &scraper.Details{
		Provider:    "douban",
		ProviderID:  id,
		ExternalIDs: map[string]string{"douban": id},
	}
	if ld != nil {
		d.Title = strings.TrimSpace(ld.Name)
		d.Overview = cleanText(ld.Description)
		if raw := strings.TrimSpace(ld.DatePublished); raw != "" {
			d.Premiered = raw
			if len(raw) >= 4 {
				if y, err := strconv.Atoi(raw[:4]); err == nil {
					v := int32(y)
					d.Year = &v
				}
			}
		}
		if len(ld.Image) > 0 {
			for _, img := range ld.Image {
				if s := strings.TrimSpace(img); s != "" {
					d.PosterURLs = append(d.PosterURLs, s)
				}
			}
		}
		for _, g := range ld.Genre {
			if s := strings.TrimSpace(g); s != "" {
				d.Genres = append(d.Genres, s)
			}
		}
		for _, person := range ld.Director {
			if name := strings.TrimSpace(person.Name); name != "" {
				d.Directors = append(d.Directors, name)
			}
		}
		for i, person := range ld.Actor {
			name := strings.TrimSpace(person.Name)
			if name == "" {
				continue
			}
			d.Actors = append(d.Actors, scraper.Actor{Name: name, Order: i})
		}
		if rating := parseRating(ld.AggregateRating.RatingValue); rating != nil {
			d.Rating = rating
		}
	}

	// HTML 内文字段兜底/补全
	if imdb := extractIMDb(body); imdb != "" {
		d.ExternalIDs["imdb"] = imdb
	}
	if alt := extractAlternateTitle(body); alt != "" && d.OriginalTitle == "" {
		d.OriginalTitle = alt
	}
	// JSON-LD 里 name 有时含年份 / 季信息,取不到时从 HTML 补
	if d.Title == "" {
		d.Title = extractTitleFromHTML(body)
	}

	return d, nil
}

// ---------- JSON 接口 fallback ----------

func (p *DoubanProvider) fetchDetailJSON(ctx context.Context, id string) (*scraper.Details, error) {
	endpoint := fmt.Sprintf("https://movie.douban.com/j/subject_abstract?subject_id=%s", url.QueryEscape(id))
	resp, err := p.doRequest(ctx, endpoint)
	if err != nil {
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
		Provider:    "douban",
		ProviderID:  id,
		ExternalIDs: map[string]string{"douban": id},
		Title:       strings.TrimSpace(payload.Subject.Title),
		Overview:    strings.TrimSpace(payload.Subject.ShortDescription),
	}
	if s := strings.TrimSpace(payload.Subject.Pic.Large); s != "" {
		d.PosterURLs = []string{s}
	}
	return d, nil
}

// ---------- JSON-LD 解析 ----------

type ldPerson struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type ldMovie struct {
	Type            string     `json:"@type"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	DatePublished   string     `json:"datePublished"`
	Genre           []string   `json:"genre"`
	Image           []string   `json:"image"`
	Director        []ldPerson `json:"director"`
	Actor           []ldPerson `json:"actor"`
	AggregateRating struct {
		RatingValue string `json:"ratingValue"`
	} `json:"aggregateRating"`
}

// extractLDJSON 扫 HTML 找第一个 <script type="application/ld+json"> 块并反序列化。
// 豆瓣偶尔把 image 字段写成单个字符串而非数组,用一个宽松 struct 兜住。
func extractLDJSON(body []byte) *ldMovie {
	z := html.NewTokenizer(strings.NewReader(string(body)))
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return nil
		case html.StartTagToken:
			name, hasAttr := z.TagName()
			if string(name) != "script" || !hasAttr {
				continue
			}
			isLD := false
			for {
				k, v, more := z.TagAttr()
				if string(k) == "type" && string(v) == "application/ld+json" {
					isLD = true
				}
				if !more {
					break
				}
			}
			if !isLD {
				continue
			}
			if z.Next() != html.TextToken {
				continue
			}
			raw := string(z.Text())
			// 豆瓣 JSON-LD 里偶尔有未转义 \n,容错处理
			raw = strings.ReplaceAll(raw, "\n", " ")
			// image 字段在豆瓣是字符串,用中间结构容错
			var loose struct {
				Type            string          `json:"@type"`
				Name            string          `json:"name"`
				Description     string          `json:"description"`
				DatePublished   string          `json:"datePublished"`
				Genre           []string        `json:"genre"`
				Image           json.RawMessage `json:"image"`
				Director        []ldPerson      `json:"director"`
				Actor           []ldPerson      `json:"actor"`
				AggregateRating struct {
					RatingValue string `json:"ratingValue"`
				} `json:"aggregateRating"`
			}
			if err := json.Unmarshal([]byte(raw), &loose); err != nil {
				return nil
			}
			out := &ldMovie{
				Type:          loose.Type,
				Name:          loose.Name,
				Description:   loose.Description,
				DatePublished: loose.DatePublished,
				Genre:         loose.Genre,
				Director:      loose.Director,
				Actor:         loose.Actor,
			}
			out.AggregateRating.RatingValue = loose.AggregateRating.RatingValue
			if len(loose.Image) > 0 {
				var single string
				if err := json.Unmarshal(loose.Image, &single); err == nil {
					if single != "" {
						out.Image = []string{single}
					}
				} else {
					var multi []string
					_ = json.Unmarshal(loose.Image, &multi)
					out.Image = multi
				}
			}
			return out
		}
	}
}

// ---------- 正则抽取 ----------

var (
	reIMDb     = regexp.MustCompile(`IMDb:\s*(tt\d{5,10})`)
	reAltTitle = regexp.MustCompile(`又名:</span>\s*([^<\n]+)`)
	reTitleH1  = regexp.MustCompile(`<span property="v:itemreviewed">([^<]+)</span>`)
)

func extractIMDb(body []byte) string {
	m := reIMDb.FindSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(string(m[1]))
}

func extractAlternateTitle(body []byte) string {
	m := reAltTitle.FindSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	// 豆瓣又名以 " / " 分隔,取第一个
	alt := strings.TrimSpace(string(m[1]))
	if idx := strings.Index(alt, " / "); idx > 0 {
		alt = strings.TrimSpace(alt[:idx])
	}
	return alt
}

func extractTitleFromHTML(body []byte) string {
	m := reTitleH1.FindSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(string(m[1]))
}

// ---------- 工具 ----------

func cleanText(s string) string {
	s = strings.TrimSpace(s)
	// 豆瓣简介偶尔带前导空格 /
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return s
}

func parseRating(raw string) *float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v <= 0 {
		return nil
	}
	return &v
}
