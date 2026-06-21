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

	start := time.Now()
	result, cacheHit, err := resolveCachedPlay(ctx, state, *playSource)
	if err != nil {
		_ = state.Repo.Source.MarkPlaySourceFailure(ctx, playSource.ID, time.Since(start).Milliseconds())
		logSourceProxy(logger, start, *playSource, "", cacheHit, 0, err)
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	statusCode, err := proxySourceStream(c, result)
	if err != nil {
		state.Cache.Del(ctx, sourcePlayCacheKey(playSource.PublicUUID))
		_ = state.Repo.Source.MarkPlaySourceFailure(ctx, playSource.ID, time.Since(start).Milliseconds())
		logSourceProxy(logger, start, *playSource, result.URL, cacheHit, statusCode, err)
		if !c.Writer.Written() {
			c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		}
		return
	}
	if !cacheHit {
		_ = state.Repo.Source.MarkPlaySourceSuccess(ctx, playSource.ID, time.Since(start).Milliseconds())
	}
	logSourceProxy(logger, start, *playSource, result.URL, cacheHit, statusCode, nil)
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
	result, err := source.ResolvePlay(ctx, playSource)
	if err != nil {
		state.Cache.Del(ctx, key)
		return nil, false, err
	}
	state.Cache.SetJSON(ctx, key, result, sourcePlayCacheTTL)
	return result, false, nil
}

func proxySourceStream(c *gin.Context, result *source.PlayResult) (int, error) {
	if result == nil || strings.TrimSpace(result.URL) == "" {
		return 0, fmt.Errorf("播放地址为空")
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

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

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
