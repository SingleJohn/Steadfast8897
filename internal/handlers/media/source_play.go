package media

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"fyms/internal/repository"
	"fyms/internal/source"
)

const sourcePlayCacheTTL = 15 * time.Minute
const sourcePlayMaxRedirects = 5

func streamSourcePlay(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	logger := source.SourceLogger("resolver")
	publicUUID := strings.TrimSpace(c.Param("playSourceUUID"))
	if publicUUID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid play source id"})
		return
	}
	playSource, err := state.Repo.Source.GetPlaySourceByPublicUUID(ctx, publicUUID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if playSource == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "Play source not found"})
		return
	}

	candidates, err := sourcePlayCandidates(ctx, state, *playSource)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	var lastErr error
	for i := range candidates {
		current := candidates[i]
		start := time.Now()
		result, cacheHit, err := resolveCachedPlay(ctx, state, current)
		if err != nil {
			_ = state.Repo.Source.MarkPlaySourceFailure(ctx, current.ID, time.Since(start).Milliseconds())
			logSourceProxy(logger, start, current, "", cacheHit, 0, err)
			lastErr = err
			continue
		}
		statusCode, err := proxySourceStream(c, result)
		if err != nil {
			state.Cache.Del(ctx, sourcePlayCacheKey(current.PublicUUID))
			_ = state.Repo.Source.MarkPlaySourceFailure(ctx, current.ID, time.Since(start).Milliseconds())
			logSourceProxy(logger, start, current, result.URL, cacheHit, statusCode, err)
			lastErr = err
			if c.Writer.Written() {
				return
			}
			continue
		}
		if !cacheHit {
			_ = state.Repo.Source.MarkPlaySourceSuccess(ctx, current.ID, time.Since(start).Milliseconds())
		}
		logSourceProxy(logger, start, current, result.URL, cacheHit, statusCode, nil)
		return
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有可用播放线路")
	}
	c.JSON(http.StatusBadGateway, gin.H{"message": lastErr.Error()})
}

func sourcePlayCandidates(ctx context.Context, state *AppState, primary repository.SourcePlaySource) ([]repository.SourcePlaySource, error) {
	episodeKey := strings.TrimSpace(primary.EpisodeKey)
	if episodeKey == "" {
		episodeKey = strings.TrimSpace(primary.EpisodeTitle)
	}
	if episodeKey == "" || primary.SourceItemID <= 0 {
		return []repository.SourcePlaySource{primary}, nil
	}
	all, err := state.Repo.Source.ListPlayableAlternatives(ctx, primary.SourceItemID, episodeKey)
	if err != nil {
		return nil, err
	}
	out := make([]repository.SourcePlaySource, 0, len(all)+1)
	out = append(out, primary)
	for i := range all {
		if all[i].ID == primary.ID {
			continue
		}
		out = append(out, all[i])
	}
	return out, nil
}

func resolveCachedPlay(ctx context.Context, state *AppState, playSource repository.SourcePlaySource) (*source.PlayResult, bool, error) {
	key := sourcePlayCacheKey(playSource.PublicUUID)
	var cached source.PlayResult
	if state.Cache.GetJSON(ctx, key, &cached) && strings.TrimSpace(cached.URL) != "" {
		if err := source.ValidateOutboundURL(ctx, cached.URL); err == nil {
			return &cached, true, nil
		}
		state.Cache.Del(ctx, key)
	}
	manager := source.NewProviderRuntimeManager(state.Repo.Source, state.HTTPClient).WithJSRuntime(state.JSRuntime).WithCSPRuntime(state.CSPRuntime)
	result, err := manager.ResolvePlay(ctx, playSource)
	if err != nil && !source.IsProviderDisabledError(err) {
		if strings.EqualFold(strings.TrimSpace(playSource.ParseMode), "resolver") {
			result, err = source.NewParserResolver(state.Repo.Source, state.HTTPClient).Resolve(ctx, playSource)
		} else {
			result, err = source.ResolvePlay(ctx, playSource)
		}
	}
	if err != nil {
		state.Cache.Del(ctx, key)
		return nil, false, err
	}
	if cacheableSourcePlayResult(ctx, result) {
		state.Cache.SetJSON(ctx, key, result, sourcePlayCacheTTL)
	}
	return result, false, nil
}

func cacheableSourcePlayResult(ctx context.Context, result *source.PlayResult) bool {
	if result == nil || len(result.Body) > 0 {
		return false
	}
	playURL := strings.TrimSpace(result.URL)
	if playURL == "" {
		return false
	}
	return source.ValidateOutboundURL(ctx, playURL) == nil
}

func proxySourceStream(c *gin.Context, result *source.PlayResult) (int, error) {
	if result == nil {
		return 0, fmt.Errorf("播放地址为空")
	}
	if len(result.Body) > 0 || strings.TrimSpace(result.URL) == "" {
		status := result.StatusCode
		if status == 0 {
			status = http.StatusOK
		}
		for key, value := range result.Headers {
			if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
				c.Header(key, value)
			}
		}
		if strings.TrimSpace(result.ContentType) != "" {
			c.Header("Content-Type", result.ContentType)
		}
		c.Status(status)
		_, err := c.Writer.Write(result.Body)
		return status, err
	}
	if err := source.ValidateOutboundURL(c.Request.Context(), result.URL); err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, result.URL, nil)
	if err != nil {
		return 0, err
	}
	for key, value := range result.Headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", c.GetHeader("User-Agent"))
	}
	rangeHeader := strings.TrimSpace(c.GetHeader("Range"))
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}

	client := &http.Client{
		Transport: &http.Transport{
			ResponseHeaderTimeout: 30 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= sourcePlayMaxRedirects {
				return fmt.Errorf("播放地址重定向次数过多")
			}
			return source.ValidateOutboundURL(req.Context(), req.URL.String())
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("上游播放地址返回异常状态: %d", resp.StatusCode)
	}

	copySourceStreamHeaders(c, resp, rangeHeader)
	c.Status(resp.StatusCode)
	_, err = io.Copy(c.Writer, resp.Body)
	return resp.StatusCode, err
}

func copySourceStreamHeaders(c *gin.Context, resp *http.Response, rangeHeader string) {
	if contentType := resp.Header.Get("Content-Type"); contentType != "" {
		c.Header("Content-Type", contentType)
	}
	if acceptRanges := resp.Header.Get("Accept-Ranges"); acceptRanges != "" {
		c.Header("Accept-Ranges", acceptRanges)
	} else {
		c.Header("Accept-Ranges", "bytes")
	}
	if contentLength := resp.Header.Get("Content-Length"); contentLength != "" {
		c.Header("Content-Length", contentLength)
	}
	if rangeHeader == "" || resp.StatusCode == http.StatusPartialContent {
		if contentRange := resp.Header.Get("Content-Range"); contentRange != "" {
			c.Header("Content-Range", contentRange)
		}
	}
}

func sourcePlayCacheKey(publicUUID string) string {
	return "sourceplay:" + publicUUID
}

func logSourceProxy(logger *slog.Logger, start time.Time, playSource repository.SourcePlaySource, rawURL string, cacheHit bool, upstreamStatus int, err error) {
	status := "ok"
	level := slog.LevelInfo
	attrs := []any{
		"provider_id", playSource.ProviderID,
		"action", "proxy_stream",
		"status", status,
		"play_source_id", playSource.ID,
		"cache_hit", cacheHit,
		"upstream_status", upstreamStatus,
		"url_hash", source.URLHash(rawURL),
	}
	if err != nil {
		status = "error"
		level = slog.LevelWarn
		attrs[5] = status
		attrs = append(attrs, "error_type", source.ErrorType(err), "error", err)
	}
	source.LogSourceAction(logger, start, level, "[Resolver] proxy_stream", attrs...)
}
