package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"fyms/internal/repository"
)

const (
	parserResponseMaxBytes = 2 << 20
	parserMaxRedirects     = 5
)

var parserForwardHeaders = []string{"User-Agent", "Referer", "Origin", "Cookie"}

type ParserResolver struct {
	repo   *repository.SourceRepository
	client *http.Client
	logger *slog.Logger
}

func NewParserResolver(repo *repository.SourceRepository, client *http.Client) *ParserResolver {
	if client == nil {
		client = http.DefaultClient
	}
	return &ParserResolver{
		repo:   repo,
		client: client,
		logger: SourceLogger("resolver"),
	}
}

func (r *ParserResolver) Resolve(ctx context.Context, playSource repository.SourcePlaySource) (*PlayResult, error) {
	start := time.Now()
	if r == nil || r.repo == nil {
		err := fmt.Errorf("parser resolver 缺少 repository")
		logParserResolve(r.logger, start, 0, playSource, err)
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(playSource.ParseMode), "resolver") {
		err := fmt.Errorf("解析器仅支持 parse_mode=resolver: %s", playSource.ParseMode)
		logParserResolve(r.logger, start, 0, playSource, err)
		return nil, err
	}
	parsers, err := r.repo.ListParsers(ctx, repository.SourceParserListOptions{Limit: 50, OnlyEnabled: true})
	if err != nil {
		logParserResolve(r.logger, start, 0, playSource, err)
		return nil, err
	}
	if len(parsers) == 0 {
		err := fmt.Errorf("没有启用的播放解析器")
		logParserResolve(r.logger, start, 0, playSource, err)
		return nil, err
	}
	var lastErr error
	for i := range parsers {
		parser := parsers[i]
		if !parserSupportsPlaySource(parser, playSource) {
			continue
		}
		result, err := r.resolveWithParser(ctx, parser, playSource)
		if err != nil {
			msg := err.Error()
			_, _ = r.repo.UpdateParserCheck(ctx, parser.ID, &msg)
			logParserResolve(r.logger, start, parser.ID, playSource, err)
			lastErr = err
			continue
		}
		_, _ = r.repo.UpdateParserCheck(ctx, parser.ID, nil)
		logParserResolve(r.logger, start, parser.ID, playSource, nil)
		return result, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有匹配该线路 flag 的可用 type=1 播放解析器")
	}
	return nil, lastErr
}

func (r *ParserResolver) resolveWithParser(ctx context.Context, parser repository.SourceParser, playSource repository.SourcePlaySource) (*PlayResult, error) {
	if parser.ParserType != 1 {
		return nil, fmt.Errorf(parserUnsupportedReason(parser.ParserType))
	}
	rawURL := strings.TrimSpace(playSource.RawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("播放原始地址为空")
	}
	if err := ValidateOutboundURL(ctx, rawURL); err != nil {
		return nil, err
	}
	requestURL, requireJSON, err := parserRequestURL(parser.URL, rawURL, parser.BaseURL)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(parser.TimeoutMS) * time.Millisecond
	if timeout <= 0 || timeout > 15*time.Second {
		timeout = 8 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := ValidateOutboundURL(reqCtx, requestURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建解析器请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/json, text/plain;q=0.9, */*;q=0.8")
	req.Header.Set("User-Agent", "FYMS SourceParser/1.0")
	for key, value := range parserRequestHeaders(playSource.Headers) {
		req.Header.Set(key, value)
	}
	client := *r.client
	client.Timeout = timeout
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= parserMaxRedirects {
			return fmt.Errorf("解析器重定向次数过多")
		}
		return ValidateOutboundURL(req.Context(), req.URL.String())
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求解析器失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("解析器返回异常状态: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, parserResponseMaxBytes))
	if err != nil {
		return nil, fmt.Errorf("读取解析器响应失败: %w", err)
	}
	result, err := extractParserResult(body, requireJSON)
	if err != nil {
		return nil, err
	}
	if err := ValidateOutboundURL(ctx, result.URL); err != nil {
		return nil, err
	}
	return result, nil
}

func parserRequestURL(parserURL, rawPlayURL string, baseURL *string) (string, bool, error) {
	template := strings.TrimSpace(parserURL)
	if template == "" {
		return "", false, fmt.Errorf("解析器 URL 为空")
	}
	requireJSON := false
	if rest, ok := strings.CutPrefix(strings.ToLower(template), "json:"); ok {
		requireJSON = true
		template = strings.TrimSpace(template[len(template)-len(rest):])
	} else if rest, ok := strings.CutPrefix(strings.ToLower(template), "parse:"); ok {
		template = strings.TrimSpace(template[len(template)-len(rest):])
	}
	if baseURL != nil && strings.TrimSpace(*baseURL) != "" {
		if parsed, err := url.Parse(template); err == nil && !parsed.IsAbs() {
			base, baseErr := url.Parse(strings.TrimSpace(*baseURL))
			if baseErr != nil {
				return "", requireJSON, fmt.Errorf("解析 TVBox base URL 失败: %w", baseErr)
			}
			template = base.ResolveReference(parsed).String()
		}
	}
	if replaced := parserTemplateURL(template, rawPlayURL); replaced != template {
		return replaced, requireJSON, nil
	}
	u, err := url.Parse(template)
	if err != nil {
		return "", requireJSON, fmt.Errorf("解析器 URL 无效: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", requireJSON, fmt.Errorf("解析器 URL 缺少 scheme 或 host")
	}
	if !strings.Contains(template, rawPlayURL) {
		q := u.Query()
		q.Set("url", rawPlayURL)
		u.RawQuery = q.Encode()
		return u.String(), requireJSON, nil
	}
	return template, requireJSON, nil
}

func parserTemplateURL(template, rawPlayURL string) string {
	escaped := url.QueryEscape(rawPlayURL)
	replacer := strings.NewReplacer(
		"{url}", escaped,
		"{{url}}", escaped,
		"{playUrl}", escaped,
		"{{playUrl}}", escaped,
	)
	return replacer.Replace(template)
}

func parserSupportsPlaySource(parser repository.SourceParser, playSource repository.SourcePlaySource) bool {
	if parser.ParserType != 1 {
		return false
	}
	flags := parserFlags(parser.Raw)
	if len(flags) == 0 {
		return true
	}
	flag := playSourceFlag(playSource)
	return flag != "" && slices.Contains(flags, flag)
}

func parserFlags(raw []byte) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	value := obj["flags"]
	if value == nil {
		value = obj["flag"]
	}
	out := []string{}
	appendFlag := func(raw string) {
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == '，' || r == '/' || r == '|' || r == ';'
		}) {
			if key := parserFlagKey(part); key != "" && !slices.Contains(out, key) {
				out = append(out, key)
			}
		}
	}
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case string:
			appendFlag(v)
		case []any:
			for _, item := range v {
				walk(item)
			}
		case map[string]any:
			for _, key := range []string{"flag", "name", "value"} {
				if s, ok := v[key].(string); ok {
					appendFlag(s)
				}
			}
		}
	}
	walk(value)
	return out
}

func playSourceFlag(playSource repository.SourcePlaySource) string {
	if playSource.Flag != nil && strings.TrimSpace(*playSource.Flag) != "" {
		return parserFlagKey(*playSource.Flag)
	}
	return parserFlagKey(playSource.LineName)
}

func parserFlagKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.ToLower(value)
}

func parserRequestHeaders(raw []byte) map[string]string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	out := map[string]string{}
	for key, value := range values {
		canonical := canonicalParserHeader(key)
		if canonical == "" {
			continue
		}
		if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
			out[canonical] = strings.TrimSpace(s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func canonicalParserHeader(key string) string {
	key = strings.TrimSpace(key)
	for _, allowed := range parserForwardHeaders {
		if strings.EqualFold(key, allowed) {
			return allowed
		}
	}
	return ""
}

func parserUnsupportedReason(parserType int32) string {
	switch parserType {
	case 0:
		return "TVBox type=0 WebView/嗅探解析器依赖客户端宿主，FYMS 服务端不支持"
	case 2:
		return "TVBox type=2 按直连/免解析口径处理，不进入全局 ParserResolver"
	case 3:
		return "TVBox type=3 mix/sniffer 解析器依赖 WebView 嗅探，FYMS 服务端不支持"
	case 4:
		return "TVBox type=4 super parse 依赖壳私有能力，FYMS 服务端不支持"
	default:
		return fmt.Sprintf("不支持的解析器类型: %d", parserType)
	}
}

func extractParserResult(body []byte, requireJSON bool) (*PlayResult, error) {
	body = bytes.TrimSpace(bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf}))
	if len(body) == 0 {
		return nil, fmt.Errorf("解析器响应为空")
	}
	if body[0] != '{' && body[0] != '[' {
		if requireJSON {
			return nil, fmt.Errorf("解析器响应不是 JSON")
		}
		text := strings.Trim(strings.TrimSpace(string(body)), "\"'")
		if strings.HasPrefix(strings.ToLower(text), "http://") || strings.HasPrefix(strings.ToLower(text), "https://") {
			return &PlayResult{URL: text, Headers: map[string]string{}}, nil
		}
		return nil, fmt.Errorf("解析器响应未包含直链")
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析解析器 JSON 失败: %w", err)
	}
	urlValue := findStringByKeys(payload, "url", "playUrl", "play_url")
	if urlValue == "" {
		return nil, fmt.Errorf("解析器 JSON 未包含 url")
	}
	return &PlayResult{URL: urlValue, Headers: findHeaderMap(payload)}, nil
}

func findStringByKeys(value any, keys ...string) string {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if raw, ok := v[key]; ok {
				if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
		}
		for _, raw := range v {
			if s := findStringByKeys(raw, keys...); s != "" {
				return s
			}
		}
	case []any:
		for _, raw := range v {
			if s := findStringByKeys(raw, keys...); s != "" {
				return s
			}
		}
	}
	return ""
}

func findHeaderMap(value any) map[string]string {
	out := map[string]string{}
	raw := findValueByKeys(value, "header", "headers")
	if raw == nil {
		return out
	}
	switch v := raw.(type) {
	case map[string]any:
		for key, value := range v {
			if s, ok := value.(string); ok && strings.TrimSpace(key) != "" && strings.TrimSpace(s) != "" {
				out[strings.TrimSpace(key)] = strings.TrimSpace(s)
			}
		}
	case string:
		pairs := strings.Split(v, "&")
		for _, pair := range pairs {
			parts := strings.SplitN(pair, "=", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != "" {
				out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}
	return out
}

func findValueByKeys(value any, keys ...string) any {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if raw, ok := v[key]; ok {
				return raw
			}
		}
		for _, raw := range v {
			if found := findValueByKeys(raw, keys...); found != nil {
				return found
			}
		}
	case []any:
		for _, raw := range v {
			if found := findValueByKeys(raw, keys...); found != nil {
				return found
			}
		}
	}
	return nil
}

func logParserResolve(logger *slog.Logger, start time.Time, parserID int64, playSource repository.SourcePlaySource, err error) {
	status := "ok"
	level := slog.LevelInfo
	attrs := []any{
		"provider_id", playSource.ProviderID,
		"parser_id", parserID,
		"action", "parser_resolve",
		"status", status,
		"play_source_id", playSource.ID,
		"parse_mode", playSource.ParseMode,
		"url_hash", URLHash(playSource.RawURL),
	}
	if err != nil {
		status = "error"
		level = slog.LevelWarn
		attrs[7] = status
		attrs = append(attrs, "error_type", ErrorType(err), "error", err)
	}
	LogSourceAction(logger, start, level, "[Resolver] parser_resolve", attrs...)
}
