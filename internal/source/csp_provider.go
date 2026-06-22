package source

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type CSPProvider struct {
	providerID    int64
	siteKey       string
	name          string
	api           string
	spider        string
	configBaseURL string
	ext           string
	runtime       *CSPRuntimeManager
	timeout       time.Duration
}

func NewCSPProvider(providerID int64, siteKey, name, api, spider, configBaseURL string, extRaw json.RawMessage, runtime *CSPRuntimeManager, timeout time.Duration) (*CSPProvider, error) {
	siteKey = strings.TrimSpace(siteKey)
	if siteKey == "" {
		return nil, fmt.Errorf("CSP Provider 缺少 site key")
	}
	if runtime == nil {
		return nil, fmt.Errorf("CSP runtime 未初始化")
	}
	api = strings.TrimSpace(api)
	if !strings.HasPrefix(api, "csp_") {
		return nil, fmt.Errorf("CSP Provider api 非 csp_*: %s", api)
	}
	spider = strings.TrimSpace(spider)
	if spider == "" {
		return nil, fmt.Errorf("CSP Provider 缺少 spider artifact")
	}
	if timeout <= 0 {
		timeout = cspRuntimeDefaultTimeout
	}
	return &CSPProvider{
		providerID:    providerID,
		siteKey:       siteKey,
		name:          strings.TrimSpace(name),
		api:           api,
		spider:        spider,
		configBaseURL: strings.TrimSpace(configBaseURL),
		ext:           cspProviderExt(extRaw),
		runtime:       runtime,
		timeout:       timeout,
	}, nil
}

func (p *CSPProvider) Categories(ctx context.Context) ([]ProviderCategory, error) {
	raw, err := p.runData(ctx, CSPRuntimeMethodHome, map[string]any{"filter": true})
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
	if err := decodeCSPRuntimeData(raw, &payload); err != nil {
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

func (p *CSPProvider) Search(ctx context.Context, req SearchRequest) (*ProviderPage, error) {
	keyword := strings.TrimSpace(req.Keyword)
	raw, err := p.runData(ctx, CSPRuntimeMethodSearch, map[string]any{
		"key": keyword,
		"wd":  keyword,
		"pg":  normalizePage(req.Page),
	})
	if err != nil {
		return nil, err
	}
	return p.parsePage(raw, false)
}

func (p *CSPProvider) Category(ctx context.Context, req CategoryRequest) (*ProviderPage, error) {
	categoryID := strings.TrimSpace(req.CategoryID)
	raw, err := p.runData(ctx, CSPRuntimeMethodCategory, map[string]any{
		"tid": categoryID,
		"id":  categoryID,
		"pg":  normalizePage(req.Page),
	})
	if err != nil {
		return nil, err
	}
	return p.parsePage(raw, false)
}

func (p *CSPProvider) Detail(ctx context.Context, sourceItemID string) (*ProviderDetail, error) {
	raw, err := p.runData(ctx, CSPRuntimeMethodDetail, map[string]any{"id": strings.TrimSpace(sourceItemID)})
	if err != nil {
		return nil, err
	}
	page, err := p.parsePage(raw, true)
	if err != nil {
		return nil, err
	}
	if len(page.Items) == 0 {
		return nil, fmt.Errorf("CSP Provider 详情为空: %s", sourceItemID)
	}
	vod, err := firstCSPRuntimeVOD(raw)
	if err != nil {
		return nil, err
	}
	return &ProviderDetail{
		Item:        page.Items[0],
		PlaySources: splitCSPPlaySources(vod.VodPlayFrom, vod.VodPlayURL),
	}, nil
}

func (p *CSPProvider) ResolvePlay(ctx context.Context, play PlaySourceSnapshot) (*PlayResult, error) {
	if strings.EqualFold(strings.TrimSpace(play.ParseMode), "proxy") || isCSPProxyURL(play.RawURL) {
		return p.resolveProxy(ctx, play)
	}
	flag := ""
	if play.Flag != nil {
		flag = strings.TrimSpace(*play.Flag)
	}
	if flag == "" {
		flag = strings.TrimSpace(play.LineName)
	}
	raw, err := p.runData(ctx, CSPRuntimeMethodPlay, map[string]any{
		"flag": flag,
		"from": flag,
		"id":   strings.TrimSpace(play.RawURL),
		"url":  strings.TrimSpace(play.RawURL),
	})
	if err != nil {
		return nil, err
	}
	result, err := parseCSPPlayResult(ctx, raw)
	if err != nil {
		return nil, err
	}
	if isCSPProxyURL(result.URL) {
		proxyPlay := play
		proxyPlay.RawURL = result.URL
		proxyPlay.ParseMode = "proxy"
		return p.resolveProxy(ctx, proxyPlay)
	}
	return result, nil
}

func (p *CSPProvider) resolveProxy(ctx context.Context, play PlaySourceSnapshot) (*PlayResult, error) {
	args := map[string]any{
		"url":  strings.TrimSpace(play.RawURL),
		"id":   strings.TrimSpace(play.RawURL),
		"flag": strings.TrimSpace(play.LineName),
	}
	for key, value := range cspProxyParams(play.RawURL) {
		args[key] = value
	}
	raw, err := p.runData(ctx, CSPRuntimeMethodProxy, args)
	if err != nil {
		return nil, err
	}
	return parseCSPProxyResult(ctx, raw)
}

func (p *CSPProvider) runData(ctx context.Context, method string, args map[string]any) (json.RawMessage, error) {
	if args == nil {
		args = map[string]any{}
	}
	timeoutMS := int(p.timeout / time.Millisecond)
	resp, err := p.runtime.Run(ctx, CSPRuntimeRequest{
		ConfigBaseURL: p.configBaseURL,
		Spider:        p.spider,
		API:           p.api,
		Ext:           p.ext,
		Method:        method,
		Args:          args,
		ProviderID:    &p.providerID,
		ProviderKey:   p.siteKey,
		TimeoutMs:     timeoutMS,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || !resp.OK {
		if resp != nil && resp.Result.Error != "" {
			return nil, fmt.Errorf("CSP runtime %s 失败: %s", method, resp.Result.Error)
		}
		return nil, fmt.Errorf("CSP runtime %s 失败", method)
	}
	return firstCSPRuntimeResultData(resp.Data, method)
}

func (p *CSPProvider) parsePage(raw json.RawMessage, detailLoaded bool) (*ProviderPage, error) {
	var payload cmsResponse
	if err := decodeCSPRuntimeData(raw, &payload); err != nil {
		return nil, err
	}
	for i := range payload.List {
		if payload.List[i].Raw == nil {
			payload.List[i].Raw = map[string]any{}
		}
		payload.List[i].Raw["provider_format"] = "csp_dex"
	}
	return parseCMSPage(p.baseForImages(), payload, detailLoaded), nil
}

func (p *CSPProvider) baseForImages() string {
	if strings.TrimSpace(p.ext) != "" {
		if u, err := url.Parse(p.ext); err == nil && u.IsAbs() {
			return p.ext
		}
	}
	if strings.TrimSpace(p.configBaseURL) != "" {
		return p.configBaseURL
	}
	return defaultDRPYBaseURL
}

func cspProviderExt(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil {
		for _, key := range []string{"extend", "ext", "_raw"} {
			if value, ok := obj[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func decodeCSPRuntimeData(raw json.RawMessage, out any) error {
	raw = normalizeRuntimeJSON(raw)
	if len(raw) == 0 {
		return fmt.Errorf("CSP runtime 数据为空")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("解析 CSP runtime 数据失败: %w", err)
	}
	return nil
}

func firstCSPRuntimeResultData(raw json.RawMessage, method string) (json.RawMessage, error) {
	raw = normalizeRuntimeJSON(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("CSP runtime %s 数据为空", method)
	}
	return raw, nil
}

func firstCSPRuntimeVOD(raw json.RawMessage) (cmsVOD, error) {
	var payload cmsResponse
	if err := decodeCSPRuntimeData(raw, &payload); err != nil {
		return cmsVOD{}, err
	}
	if len(payload.List) == 0 {
		return cmsVOD{}, fmt.Errorf("CSP runtime list 为空")
	}
	return payload.List[0], nil
}

func splitCSPPlaySources(playFrom, playURL string) []PlaySourceSnapshot {
	out := splitCMSPlaySources(playFrom, playURL)
	for i := range out {
		out[i].ParseMode = parseModeForCSPURL(out[i].RawURL)
		if out[i].ResolverPayload == nil {
			out[i].ResolverPayload = map[string]any{}
		}
		out[i].ResolverPayload["runtime_kind"] = CSPRuntimeKindJVM
	}
	return out
}

func parseModeForCSPURL(rawURL string) string {
	lower := strings.ToLower(strings.TrimSpace(rawURL))
	switch {
	case strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://"):
		return "direct"
	case strings.HasPrefix(lower, "magnet:"):
		return "magnet"
	case strings.HasPrefix(lower, "proxy://") || strings.HasPrefix(lower, "fyms-csp-proxy://"):
		return "proxy"
	case strings.HasPrefix(lower, "push:"):
		return "unsupported"
	default:
		return "unsupported"
	}
}

func isCSPProxyURL(rawURL string) bool {
	lower := strings.ToLower(strings.TrimSpace(rawURL))
	if strings.HasPrefix(lower, "proxy://") || strings.HasPrefix(lower, "fyms-csp-proxy://") {
		return true
	}
	if u, err := url.Parse(rawURL); err == nil {
		host := strings.ToLower(u.Hostname())
		return (host == "127.0.0.1" || host == "localhost" || host == "::1") && strings.Contains(strings.ToLower(u.Path), "proxy")
	}
	return false
}

func cspProxyParams(rawURL string) map[string]string {
	out := map[string]string{}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return out
	}
	for key, values := range u.Query() {
		if len(values) > 0 {
			out[key] = values[0]
		}
	}
	return out
}

func parseCSPPlayResult(ctx context.Context, raw json.RawMessage) (*PlayResult, error) {
	raw = normalizeRuntimeJSON(raw)
	var payload struct {
		Parse   int               `json:"parse"`
		URL     string            `json:"url"`
		Header  map[string]string `json:"header"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("解析 CSP 播放结果失败: %w", err)
	}
	playURL := strings.TrimSpace(payload.URL)
	if playURL == "" {
		return nil, fmt.Errorf("CSP 播放地址为空")
	}
	headers := payload.Headers
	if len(headers) == 0 {
		headers = payload.Header
	}
	if payload.Parse == 1 {
		return nil, fmt.Errorf("parse=1 线路需解析器")
	}
	if strings.HasPrefix(strings.ToLower(playURL), "magnet:") {
		return &PlayResult{URL: playURL, Headers: headers}, nil
	}
	if err := ValidateOutboundURL(ctx, playURL); err != nil {
		return nil, err
	}
	return &PlayResult{URL: playURL, Headers: headers}, nil
}

func parseCSPProxyResult(ctx context.Context, raw json.RawMessage) (*PlayResult, error) {
	raw = normalizeRuntimeJSON(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("CSP proxy 返回为空")
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		text = strings.TrimSpace(text)
		if isHTTPURL(text) {
			if err := ValidateOutboundURL(ctx, text); err != nil {
				return nil, err
			}
			return &PlayResult{URL: text}, nil
		}
		return &PlayResult{Body: []byte(text), ContentType: "application/octet-stream", StatusCode: 200}, nil
	}
	var obj struct {
		URL         string            `json:"url"`
		Header      map[string]string `json:"header"`
		Headers     map[string]string `json:"headers"`
		Content     string            `json:"content"`
		Body        string            `json:"body"`
		BodyBase64  string            `json:"bodyBase64"`
		ContentType string            `json:"contentType"`
		Type        string            `json:"type"`
		Status      int               `json:"status"`
		StatusCode  int               `json:"statusCode"`
	}
	if json.Unmarshal(raw, &obj) == nil && (obj.URL != "" || obj.Content != "" || obj.Body != "" || obj.BodyBase64 != "") {
		headers := obj.Headers
		if len(headers) == 0 {
			headers = obj.Header
		}
		if strings.TrimSpace(obj.URL) != "" {
			if err := ValidateOutboundURL(ctx, obj.URL); err != nil {
				return nil, err
			}
			return &PlayResult{URL: obj.URL, Headers: headers}, nil
		}
		status := obj.StatusCode
		if status == 0 {
			status = obj.Status
		}
		if status == 0 {
			status = 200
		}
		contentType := strings.TrimSpace(obj.ContentType)
		if contentType == "" {
			contentType = strings.TrimSpace(obj.Type)
		}
		body := []byte(obj.Content)
		if obj.Body != "" {
			body = []byte(obj.Body)
		}
		if obj.BodyBase64 != "" {
			if decoded, err := base64.StdEncoding.DecodeString(obj.BodyBase64); err == nil {
				body = decoded
			}
		}
		return &PlayResult{Body: body, Headers: headers, ContentType: contentType, StatusCode: status}, nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
		return parseCSPProxyArray(ctx, arr)
	}
	return nil, fmt.Errorf("CSP proxy 返回格式无法识别")
}

func parseCSPProxyArray(ctx context.Context, arr []json.RawMessage) (*PlayResult, error) {
	status := 200
	if len(arr) > 0 {
		var n int
		if json.Unmarshal(arr[0], &n) == nil && n > 0 {
			status = n
		}
	}
	contentType := ""
	if len(arr) > 1 {
		_ = json.Unmarshal(arr[1], &contentType)
	}
	headers := map[string]string{}
	if len(arr) > 2 {
		_ = json.Unmarshal(arr[2], &headers)
	}
	body := []byte{}
	if len(arr) > 3 {
		var text string
		if json.Unmarshal(arr[3], &text) == nil {
			text = strings.TrimSpace(text)
			if isHTTPURL(text) {
				if err := ValidateOutboundURL(ctx, text); err != nil {
					return nil, err
				}
				return &PlayResult{URL: text, Headers: headers}, nil
			}
			body = []byte(text)
		} else {
			var bytes []byte
			if json.Unmarshal(arr[3], &bytes) == nil {
				body = bytes
			}
		}
	}
	return &PlayResult{Body: body, Headers: headers, ContentType: contentType, StatusCode: status}, nil
}

func isHTTPURL(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}
