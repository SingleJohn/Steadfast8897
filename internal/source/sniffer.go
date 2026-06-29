package source

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"fyms/internal/repository"
)

const (
	snifferMaxBytes         = 3 << 20
	snifferTimeout          = 12 * time.Second
	defaultSnifferUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
)

// snifferStreamPattern 从网页正文中提取可能的直链流地址(m3u8/mp4 等)。
var snifferStreamPattern = regexp.MustCompile(`(?i)https?://[^\s"'<>()\\]+?\.(?:m3u8|mp4|flv|m4v|mpd|ts)(?:[^\s"'<>()\\]*)`)

var snifferForwardHeaders = []string{"User-Agent", "Referer", "Origin", "Cookie"}

// SniffPlayURL 拉取网页型播放页正文,从中嗅探真实视频流地址。
// 仅做轻量正则提取,不执行 JS;命中后透传原站 headers(含 Referer)给服务端转发使用。
func SniffPlayURL(ctx context.Context, client *http.Client, playSource repository.SourcePlaySource) (*PlayResult, error) {
	rawURL := strings.TrimSpace(playSource.RawURL)
	if !isHTTPURL(rawURL) {
		return nil, fmt.Errorf("嗅探仅支持 http(s) 页面地址")
	}
	if err := ValidateOutboundURL(ctx, rawURL); err != nil {
		return nil, err
	}
	if client == nil {
		client = http.DefaultClient
	}
	headers, _ := decodeHeaderMap(playSource.Headers)
	if headers == nil {
		headers = map[string]string{}
	}

	reqCtx, cancel := context.WithTimeout(ctx, snifferTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建嗅探请求失败: %w", err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json,*/*;q=0.8")
	if strings.TrimSpace(headers["User-Agent"]) == "" {
		req.Header.Set("User-Agent", defaultSnifferUserAgent)
	}
	for key, value := range headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("拉取播放页失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("播放页返回异常状态: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, snifferMaxBytes))
	if err != nil {
		return nil, fmt.Errorf("读取播放页失败: %w", err)
	}
	candidate := extractSniffedStream(string(body))
	if candidate == "" {
		return nil, fmt.Errorf("未嗅探到可播放流")
	}
	if err := ValidateOutboundURL(ctx, candidate); err != nil {
		return nil, err
	}

	out := map[string]string{}
	for _, key := range snifferForwardHeaders {
		if value := strings.TrimSpace(headers[key]); value != "" {
			out[key] = value
		}
	}
	if out["Referer"] == "" {
		out["Referer"] = rawURL
	}
	return &PlayResult{URL: candidate, Headers: out}, nil
}

// extractSniffedStream 先把 JSON 中常见的转义斜杠 \/ 还原,再正则提取首个流地址。
func extractSniffedStream(body string) string {
	normalized := strings.ReplaceAll(body, `\/`, "/")
	match := snifferStreamPattern.FindString(normalized)
	if match == "" {
		return ""
	}
	return strings.TrimRight(match, `\"') `)
}
