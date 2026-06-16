package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
)

func TmdbClientFromConfig(ctx context.Context, pool *pgxpool.Pool) *TmdbClient {
	configRepo := repository.NewSystemConfigRepository(pool)
	rawKey, ok, err := configRepo.GetString(ctx, "tmdb_api_key")
	if err != nil || !ok || rawKey == "" {
		return nil
	}

	var apiKeys []string
	for _, k := range strings.Split(rawKey, ",") {
		k = strings.TrimSpace(k)
		if k != "" {
			apiKeys = append(apiKeys, k)
		}
	}
	if len(apiKeys) == 0 {
		return nil
	}

	slog.Info("[TMDB] Loaded API key(s)", "count", len(apiKeys))

	language := "zh-CN"
	if langVal, ok, err := configRepo.GetString(ctx, "tmdb_language"); err == nil && ok && langVal != "" {
		language = langVal
	}

	proxyURL, hasProxy, _ := configRepo.GetString(ctx, "tmdb_proxy")

	transport := &http.Transport{
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	if hasProxy {
		rawProxy := strings.TrimSpace(proxyURL)
		if rawProxy != "" {
			if u, err := url.Parse(rawProxy); err == nil && u.Scheme != "" && u.Host != "" {
				slog.Info("[TMDB] Using proxy", "proxy", redactProxyURL(u))
				transport.Proxy = http.ProxyURL(u)
			} else {
				slog.Warn("[TMDB] Invalid proxy URL, ignoring", "proxy", rawProxy, "error", err)
			}
		} else {
			slog.Info("[TMDB] Proxy not configured")
		}
	} else {
		slog.Info("[TMDB] Proxy not configured")
	}

	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
	}

	return &TmdbClient{
		httpClient: client,
		apiKeys:    apiKeys,
		language:   language,
	}
}

// sanitizeTmdbURL 把 api_key=XXX 替换成 api_key=***,避免日志泄漏。
func sanitizeTmdbURL(u string) string {
	const key = "api_key="
	idx := strings.Index(u, key)
	if idx < 0 {
		return u
	}
	tail := u[idx+len(key):]
	end := strings.IndexAny(tail, "&")
	if end < 0 {
		return u[:idx] + key + "***"
	}
	return u[:idx] + key + "***" + tail[end:]
}

func redactProxyURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	clean := *u
	if clean.User != nil {
		username := clean.User.Username()
		if username != "" {
			clean.User = url.UserPassword(username, "******")
		} else {
			clean.User = url.User("******")
		}
	}
	return clean.String()
}

func (c *TmdbClient) cloneWithLanguage(lang string) *TmdbClient {
	return &TmdbClient{
		httpClient: c.httpClient,
		apiKeys:    c.apiKeys,
		language:   lang,
	}
}

func (c *TmdbClient) nextKey() string {
	idx := c.keyIndex.Add(1) - 1
	return c.apiKeys[idx%uint64(len(c.apiKeys))]
}

// tmdbRequestCount 统计 tmdbGet 的总调用数(Phase 4 metrics 观测用)。
var tmdbRequestCount atomic.Int64

// TmdbRequestCount 返回 tmdbGet 的累计调用次数。
func TmdbRequestCount() int64 { return tmdbRequestCount.Load() }

func (c *TmdbClient) tmdbGet(ctx context.Context, urlTemplate string) (map[string]interface{}, error) {
	tmdbRequestCount.Add(1)
	if sharedTmdbLimiter != nil {
		if err := sharedTmdbLimiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter wait: %w", err)
		}
	}
	maxRetries := len(c.apiKeys)
	for attempt := 0; attempt <= maxRetries; attempt++ {
		key := c.nextKey()
		reqURL := strings.ReplaceAll(urlTemplate, "{API_KEY}", key)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		// 部分代理/CDN WAF 会黑名单默认的 Go-http-client UA,显式带上浏览器 UA 避过
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; fyms/1.0; +https://github.com/ffoocn/fyms)")
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			DiagFrom(ctx).Record(reqURL, 0, nil, false)
			// 完整错误类型 + 字符串,诊断 "Access denied" / proxy / DNS 等异常必备
			slog.Warn("[TMDB] Request error", "error", err, "error_type", fmt.Sprintf("%T", err), "url", sanitizeTmdbURL(reqURL))
			return nil, err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			DiagFrom(ctx).Record(reqURL, resp.StatusCode, nil, false)
			return nil, fmt.Errorf("read body: %w", err)
		}

		diag := DiagFrom(ctx)

		if resp.StatusCode == http.StatusTooManyRequests {
			diag.Record(reqURL, resp.StatusCode, body, false)
			if attempt < maxRetries {
				suffix := key
				if len(suffix) > 6 {
					suffix = suffix[len(suffix)-6:]
				}
				slog.Debug("[TMDB] 429 rate limited, rotating to next key", "key_suffix", suffix)
				time.Sleep(500 * time.Millisecond)
				continue
			}
			slog.Warn("[TMDB] All keys rate limited", "count", len(c.apiKeys))
			return nil, fmt.Errorf("all %d API keys rate limited", len(c.apiKeys))
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			diag.Record(reqURL, resp.StatusCode, body, false)
			slog.Debug("[TMDB] HTTP error", "status", resp.StatusCode)
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err != nil {
			diag.Record(reqURL, resp.StatusCode, body, false)
			return nil, fmt.Errorf("json decode: %w", err)
		}
		diag.Record(reqURL, resp.StatusCode, body, true)
		return result, nil
	}
	return nil, fmt.Errorf("exhausted retries")
}
