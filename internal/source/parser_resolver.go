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
	"strings"
	"time"

	"fyms/internal/repository"
)

const (
	parserResponseMaxBytes = 2 << 20
	parserMaxRedirects     = 5
)

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
		lastErr = fmt.Errorf("启用的播放解析器均不可用")
	}
	return nil, lastErr
}

func (r *ParserResolver) resolveWithParser(ctx context.Context, parser repository.SourceParser, playSource repository.SourcePlaySource) (*PlayResult, error) {
	if parser.ParserType == 3 {
		return nil, fmt.Errorf("TVBox type=3 嗅探解析器暂不支持")
	}
	if parser.ParserType != 0 && parser.ParserType != 1 {
		return nil, fmt.Errorf("不支持的解析器类型: %d", parser.ParserType)
	}
	rawURL := strings.TrimSpace(playSource.RawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("播放原始地址为空")
	}
	if err := ValidateOutboundURL(ctx, rawURL); err != nil {
		return nil, err
	}
	requestURL, err := parserRequestURL(parser.URL, rawURL, parser.BaseURL)
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
	result, err := extractParserResult(body)
	if err != nil {
		return nil, err
	}
	if err := ValidateOutboundURL(ctx, result.URL); err != nil {
		return nil, err
	}
	return result, nil
}

func parserRequestURL(parserURL, rawPlayURL string, baseURL *string) (string, error) {
	template := strings.TrimSpace(parserURL)
	if template == "" {
		return "", fmt.Errorf("解析器 URL 为空")
	}
	if baseURL != nil && strings.TrimSpace(*baseURL) != "" {
		if parsed, err := url.Parse(template); err == nil && !parsed.IsAbs() {
			base, baseErr := url.Parse(strings.TrimSpace(*baseURL))
			if baseErr != nil {
				return "", fmt.Errorf("解析 TVBox base URL 失败: %w", baseErr)
			}
			template = base.ResolveReference(parsed).String()
		}
	}
	u, err := url.Parse(template)
	if err != nil {
		return "", fmt.Errorf("解析器 URL 无效: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("解析器 URL 缺少 scheme 或 host")
	}
	if !strings.Contains(template, rawPlayURL) {
		q := u.Query()
		if _, ok := q["url"]; ok || strings.Contains(u.RawQuery, "=") || u.RawQuery == "" {
			q.Set("url", rawPlayURL)
			u.RawQuery = q.Encode()
			return u.String(), nil
		}
		return template + "=" + url.QueryEscape(rawPlayURL), nil
	}
	return template, nil
}

func extractParserResult(body []byte) (*PlayResult, error) {
	body = bytes.TrimSpace(bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf}))
	if len(body) == 0 {
		return nil, fmt.Errorf("解析器响应为空")
	}
	if body[0] != '{' && body[0] != '[' {
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
