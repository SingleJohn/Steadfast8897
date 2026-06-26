package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type JSProvider struct {
	providerID    int64
	siteKey       string
	name          string
	engine        string
	rule          string
	configBaseURL string
	headers       map[string]string
	runtime       *JSRuntimeManager
	timeout       time.Duration
}

func NewJSProvider(providerID int64, rowProviderKey, name, engine, rule, configBaseURL string, headers map[string]string, runtime *JSRuntimeManager, timeout time.Duration) (*JSProvider, error) {
	siteKey := strings.TrimSpace(rowProviderKey)
	if siteKey == "" {
		return nil, fmt.Errorf("JS Provider 缺少 site key")
	}
	if runtime == nil {
		return nil, fmt.Errorf("JS runtime 未初始化")
	}
	engine = strings.TrimSpace(engine)
	rule = strings.TrimSpace(rule)
	if engine == "" {
		return nil, fmt.Errorf("JS Provider 缺少 drpy engine")
	}
	if rule == "" {
		return nil, fmt.Errorf("JS Provider 缺少规则 ext")
	}
	if timeout <= 0 {
		timeout = jsRuntimeDefaultTimeout
	}
	return &JSProvider{
		providerID:    providerID,
		siteKey:       siteKey,
		name:          strings.TrimSpace(name),
		engine:        engine,
		rule:          rule,
		configBaseURL: strings.TrimSpace(configBaseURL),
		headers:       compactHeaderMap(headers),
		runtime:       runtime,
		timeout:       timeout,
	}, nil
}

func (p *JSProvider) Categories(ctx context.Context) ([]ProviderCategory, error) {
	raw, err := p.runData(ctx, JSRuntimeMethodHome, nil)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Class []struct {
			TypeID   string `json:"type_id"`
			TypeName string `json:"type_name"`
		} `json:"class"`
		Classes []struct {
			TypeID   string `json:"type_id"`
			TypeName string `json:"type_name"`
		} `json:"classes"`
	}
	if err := decodeRuntimeData(raw, &payload); err != nil {
		return nil, err
	}
	rows := payload.Class
	if len(rows) == 0 {
		rows = payload.Classes
	}
	out := make([]ProviderCategory, 0, len(rows))
	for _, row := range rows {
		id := cleanCMSValue(row.TypeID)
		name := cleanCMSValue(row.TypeName)
		if id == "" || name == "" {
			continue
		}
		out = append(out, ProviderCategory{ID: id, Name: name})
	}
	return out, nil
}

func (p *JSProvider) HomeProfile(ctx context.Context) (*ProviderHomeProfile, error) {
	start := time.Now()
	raw, err := p.runData(ctx, JSRuntimeMethodHome, nil)
	if err != nil {
		return &ProviderHomeProfile{
			ProviderID:  p.providerID,
			RuntimeKind: JSRuntimeKindNodeDRPY,
			Sources: ProviderHomeSources{
				HomeContent:      providerRuntimeSliceError(JSRuntimeMethodHome, start, err),
				HomeVideoContent: providerRuntimeSliceUnsupported("homeVideoContent", "JS runtime 当前未提供独立 homeVideoContent，首页列表来自 home。"),
			},
		}, err
	}
	payload, err := decodeProviderHomePayload(raw, "JS")
	if err != nil {
		return nil, err
	}
	categories := providerHomeCategories(payload)
	filters, filtersCount := providerHomeFilters(payload.Filters)
	items := providerHomeItems(p.baseForImages(), payload, "drpy_js")
	return &ProviderHomeProfile{
		ProviderID:     p.providerID,
		RuntimeKind:    JSRuntimeKindNodeDRPY,
		Categories:     categories,
		Filters:        filters,
		FiltersCount:   filtersCount,
		HomeItems:      items,
		HomeItemSource: "homeContent",
		Sources: ProviderHomeSources{
			HomeContent:      newProviderRuntimeSlice(JSRuntimeMethodHome, start, len(categories), filtersCount, len(items)),
			HomeVideoContent: providerRuntimeSliceUnsupported("homeVideoContent", "JS runtime 当前未提供独立 homeVideoContent，首页列表来自 home。"),
		},
	}, nil
}

func (p *JSProvider) Search(ctx context.Context, req SearchRequest) (*ProviderPage, error) {
	raw, err := p.runData(ctx, JSRuntimeMethodSearch, map[string]any{
		"keyword": strings.TrimSpace(req.Keyword),
		"wd":      strings.TrimSpace(req.Keyword),
		"pg":      normalizePage(req.Page),
	})
	if err != nil {
		return nil, err
	}
	return p.parsePage(raw, false)
}

func (p *JSProvider) Category(ctx context.Context, req CategoryRequest) (*ProviderPage, error) {
	raw, err := p.runData(ctx, JSRuntimeMethodCategory, map[string]any{
		"tid": strings.TrimSpace(req.CategoryID),
		"id":  strings.TrimSpace(req.CategoryID),
		"pg":  normalizePage(req.Page),
	})
	if err != nil {
		return nil, err
	}
	return p.parsePage(raw, false)
}

func (p *JSProvider) Detail(ctx context.Context, sourceItemID string) (*ProviderDetail, error) {
	raw, err := p.runData(ctx, JSRuntimeMethodDetail, map[string]any{"id": strings.TrimSpace(sourceItemID)})
	if err != nil {
		return nil, err
	}
	page, err := p.parsePage(raw, true)
	if err != nil {
		return nil, err
	}
	if len(page.Items) == 0 {
		return nil, fmt.Errorf("JS Provider 详情为空: %s", sourceItemID)
	}
	vod, err := firstRuntimeVOD(raw)
	if err != nil {
		return nil, err
	}
	return &ProviderDetail{
		Item:        page.Items[0],
		PlaySources: splitJSPlaySources(vod.VodPlayFrom, vod.VodPlayURL),
	}, nil
}

func (p *JSProvider) ResolvePlay(ctx context.Context, play PlaySourceSnapshot) (*PlayResult, error) {
	switch strings.ToLower(strings.TrimSpace(play.ParseMode)) {
	case "", "unknown", "direct":
		if err := ValidateOutboundURL(ctx, play.RawURL); err != nil {
			return nil, err
		}
		headers := map[string]string{}
		for key, value := range play.Headers {
			if s, ok := value.(string); ok && strings.TrimSpace(key) != "" && strings.TrimSpace(s) != "" {
				headers[strings.TrimSpace(key)] = strings.TrimSpace(s)
			}
		}
		return &PlayResult{URL: play.RawURL, Headers: headers}, nil
	case "resolver":
		return nil, fmt.Errorf("parse=1 线路需解析器")
	default:
		return nil, fmt.Errorf("需 runtime，暂不支持: %s", play.ParseMode)
	}
}

func (p *JSProvider) runData(ctx context.Context, method string, args map[string]any) (json.RawMessage, error) {
	if args == nil {
		args = map[string]any{}
	}
	timeoutMS := int(p.timeout / time.Millisecond)
	resp, err := p.runtime.Run(ctx, JSRuntimeRequest{
		ConfigBaseURL: p.configBaseURL,
		Engine:        p.engine,
		Rule:          p.rule,
		Method:        method,
		Headers:       p.headers,
		Args:          args,
		ProviderID:    &p.providerID,
		ProviderKey:   p.siteKey,
		TimeoutMs:     timeoutMS,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || len(resp.Results) == 0 {
		return nil, fmt.Errorf("JS runtime 无返回结果")
	}
	result := resp.Results[0]
	if !result.OK {
		if result.Error != "" {
			return nil, fmt.Errorf("JS runtime %s 失败: %s", method, result.Error)
		}
		return nil, fmt.Errorf("JS runtime %s 失败", method)
	}
	return firstRuntimeResultData(result.Data, method)
}

func (p *JSProvider) parsePage(raw json.RawMessage, detailLoaded bool) (*ProviderPage, error) {
	var payload cmsResponse
	if err := decodeRuntimeData(raw, &payload); err != nil {
		return nil, err
	}
	for i := range payload.List {
		if payload.List[i].Raw == nil {
			payload.List[i].Raw = map[string]any{}
		}
		payload.List[i].Raw["provider_format"] = "drpy_js"
	}
	return parseCMSPage(p.baseForImages(), payload, detailLoaded), nil
}

func (p *JSProvider) baseForImages() string {
	if strings.TrimSpace(p.configBaseURL) != "" {
		return p.configBaseURL
	}
	if u, err := url.Parse(p.engine); err == nil && u.IsAbs() {
		return p.engine
	}
	return defaultDRPYBaseURL
}

func firstRuntimeResultData(raw json.RawMessage, method string) (json.RawMessage, error) {
	raw = normalizeRuntimeJSON(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("JS runtime %s 数据为空", method)
	}
	var rows []struct {
		OK     bool            `json:"ok"`
		Method string          `json:"method"`
		Data   json.RawMessage `json:"data"`
		Error  string          `json:"error"`
	}
	if json.Unmarshal(raw, &rows) == nil && len(rows) > 0 {
		for _, row := range rows {
			if strings.EqualFold(row.Method, method) {
				if !row.OK {
					return nil, fmt.Errorf("JS runtime %s 失败: %s", method, row.Error)
				}
				return normalizeRuntimeJSON(row.Data), nil
			}
		}
		if !rows[0].OK {
			return nil, fmt.Errorf("JS runtime %s 失败: %s", rows[0].Method, rows[0].Error)
		}
		return normalizeRuntimeJSON(rows[0].Data), nil
	}
	return raw, nil
}

func decodeRuntimeData(raw json.RawMessage, out any) error {
	raw = normalizeRuntimeJSON(raw)
	if len(raw) == 0 {
		return fmt.Errorf("JS runtime 数据为空")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("解析 JS runtime 数据失败: %w", err)
	}
	return nil
}

func normalizeRuntimeJSON(raw json.RawMessage) json.RawMessage {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil
		}
		return json.RawMessage(text)
	}
	return raw
}

func firstRuntimeVOD(raw json.RawMessage) (cmsVOD, error) {
	var payload cmsResponse
	if err := decodeRuntimeData(raw, &payload); err != nil {
		return cmsVOD{}, err
	}
	if len(payload.List) == 0 {
		return cmsVOD{}, fmt.Errorf("JS runtime list 为空")
	}
	return payload.List[0], nil
}

func splitJSPlaySources(playFrom, playURL string) []PlaySourceSnapshot {
	if strings.TrimSpace(playFrom) == "" && strings.TrimSpace(playURL) != "" {
		playFrom = "默认线路"
	}
	out := splitCMSPlaySources(playFrom, playURL)
	for i := range out {
		out[i].ParseMode = parseModeForJSURL(out[i].RawURL)
		if out[i].ResolverPayload == nil {
			out[i].ResolverPayload = map[string]any{}
		}
		out[i].ResolverPayload["runtime_kind"] = JSRuntimeKindNodeDRPY
	}
	return out
}

func parseModeForJSURL(rawURL string) string {
	lower := strings.ToLower(strings.TrimSpace(rawURL))
	switch {
	case strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://"):
		if strings.Contains(lower, ".m3u8") || strings.Contains(lower, ".mp4") || strings.Contains(lower, ".flv") {
			return "direct"
		}
		return "resolver"
	case strings.HasPrefix(lower, "magnet:"):
		return "magnet"
	case strings.HasPrefix(lower, "push:"):
		return "unsupported"
	default:
		return "unsupported"
	}
}
