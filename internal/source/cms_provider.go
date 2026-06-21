package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultCMSTimeout = 8 * time.Second

type CMSProviderOption func(*CMSProvider)

type CMSProvider struct {
	siteKey string
	api     string
	client  *http.Client
	headers map[string]string
	timeout time.Duration
}

func NewCMSProvider(siteKey, api string, opts ...CMSProviderOption) (*CMSProvider, error) {
	siteKey = strings.TrimSpace(siteKey)
	api = strings.TrimSpace(api)
	if siteKey == "" {
		return nil, fmt.Errorf("CMS Provider 缺少 site key")
	}
	if api == "" {
		return nil, fmt.Errorf("CMS Provider 缺少 api")
	}
	if _, err := url.ParseRequestURI(api); err != nil {
		return nil, fmt.Errorf("CMS Provider api 无效: %w", err)
	}
	p := &CMSProvider{
		siteKey: siteKey,
		api:     api,
		client:  http.DefaultClient,
		headers: map[string]string{},
		timeout: defaultCMSTimeout,
	}
	for _, opt := range opts {
		opt(p)
	}
	if p.client == nil {
		p.client = http.DefaultClient
	}
	if p.timeout <= 0 {
		p.timeout = defaultCMSTimeout
	}
	return p, nil
}

func WithCMSHTTPClient(client *http.Client) CMSProviderOption {
	return func(p *CMSProvider) {
		if client != nil {
			p.client = client
		}
	}
}

func WithCMSTimeout(timeout time.Duration) CMSProviderOption {
	return func(p *CMSProvider) {
		if timeout > 0 {
			p.timeout = timeout
		}
	}
}

func WithCMSHeaders(headers map[string]string) CMSProviderOption {
	return func(p *CMSProvider) {
		for key, value := range headers {
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key != "" && value != "" {
				p.headers[key] = value
			}
		}
	}
}

func (p *CMSProvider) Categories(ctx context.Context) ([]ProviderCategory, error) {
	var payload cmsResponse
	if err := p.getJSON(ctx, nil, &payload); err != nil {
		return nil, err
	}
	categories := make([]ProviderCategory, 0, len(payload.Class))
	for _, item := range payload.Class {
		id := cleanCMSValue(item.TypeID.String())
		name := cleanCMSValue(item.TypeName)
		if id == "" || name == "" {
			continue
		}
		categories = append(categories, ProviderCategory{ID: id, Name: name})
	}
	return categories, nil
}

func (p *CMSProvider) Search(ctx context.Context, req SearchRequest) (*ProviderPage, error) {
	var payload cmsResponse
	params := map[string]string{
		"ac": "list",
		"wd": strings.TrimSpace(req.Keyword),
		"pg": strconv.Itoa(normalizePage(req.Page)),
	}
	if err := p.getJSON(ctx, params, &payload); err != nil {
		return nil, err
	}
	return parseCMSPage(p.api, payload, false), nil
}

func (p *CMSProvider) Category(ctx context.Context, req CategoryRequest) (*ProviderPage, error) {
	var payload cmsResponse
	params := map[string]string{
		"ac": "list",
		"t":  strings.TrimSpace(req.CategoryID),
		"pg": strconv.Itoa(normalizePage(req.Page)),
	}
	if err := p.getJSON(ctx, params, &payload); err != nil {
		return nil, err
	}
	return parseCMSPage(p.api, payload, false), nil
}

func (p *CMSProvider) Detail(ctx context.Context, sourceItemID string) (*ProviderDetail, error) {
	sourceItemID = strings.TrimSpace(sourceItemID)
	if sourceItemID == "" {
		return nil, fmt.Errorf("CMS 详情缺少 source item id")
	}
	var payload cmsResponse
	params := map[string]string{
		"ac":  "detail",
		"ids": sourceItemID,
	}
	if err := p.getJSON(ctx, params, &payload); err != nil {
		return nil, err
	}
	if len(payload.List) == 0 {
		return nil, fmt.Errorf("CMS 详情为空: %s", sourceItemID)
	}
	item := parseCMSItem(p.api, payload.List[0], true)
	return &ProviderDetail{
		Item:        item,
		PlaySources: splitCMSPlaySources(payload.List[0].VodPlayFrom, payload.List[0].VodPlayURL),
	}, nil
}

func (p *CMSProvider) ResolvePlay(ctx context.Context, play PlaySourceSnapshot) (*PlayResult, error) {
	if !strings.EqualFold(strings.TrimSpace(play.ParseMode), "direct") {
		return nil, fmt.Errorf("需 runtime，暂不支持: %s", play.ParseMode)
	}
	if err := ValidateOutboundURL(ctx, play.RawURL); err != nil {
		return nil, err
	}
	headers := make(map[string]string, len(play.Headers))
	for key, value := range play.Headers {
		if s, ok := value.(string); ok && strings.TrimSpace(key) != "" && strings.TrimSpace(s) != "" {
			headers[strings.TrimSpace(key)] = strings.TrimSpace(s)
		}
	}
	return &PlayResult{URL: play.RawURL, Headers: headers}, nil
}

func (p *CMSProvider) getJSON(ctx context.Context, params map[string]string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	requestURL, err := mergeCMSQuery(p.api, params)
	if err != nil {
		return err
	}
	if err := ValidateOutboundURL(ctx, requestURL); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("创建 CMS 请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	for key, value := range p.headers {
		req.Header.Set(key, value)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("请求 CMS 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("CMS 返回异常状态: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("读取 CMS 响应失败: %w", err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("解析 CMS JSON 失败: %w", err)
	}
	return nil
}

func mergeCMSQuery(rawURL string, params map[string]string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("解析 CMS api 失败: %w", err)
	}
	query := u.Query()
	for key, value := range params {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		query.Set(key, strings.TrimSpace(value))
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func normalizePage(page int) int {
	if page <= 0 {
		return 1
	}
	return page
}
