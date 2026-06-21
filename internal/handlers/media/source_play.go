package media

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"fyms/internal/source"
	"fyms/internal/repository"
)

const sourcePlayCacheTTL = 15 * time.Minute

func streamSourcePlay(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
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
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	if err := proxySourceStream(c, result); err != nil {
		state.Cache.Del(ctx, sourcePlayCacheKey(playSource.PublicUUID))
		_ = state.Repo.Source.MarkPlaySourceFailure(ctx, playSource.ID, time.Since(start).Milliseconds())
		if !c.Writer.Written() {
			c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		}
		return
	}
	if !cacheHit {
		_ = state.Repo.Source.MarkPlaySourceSuccess(ctx, playSource.ID, time.Since(start).Milliseconds())
	}
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

func proxySourceStream(c *gin.Context, result *source.PlayResult) error {
	if result == nil || strings.TrimSpace(result.URL) == "" {
		return fmt.Errorf("播放地址为空")
	}
	if err := source.ValidateOutboundURL(c.Request.Context(), result.URL); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, result.URL, nil)
	if err != nil {
		return err
	}
	for key, value := range result.Headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", c.GetHeader("User-Agent"))
	}
	if rangeHeader := c.GetHeader("Range"); rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}

	client := &http.Client{Timeout: 0}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	for _, header := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if value := resp.Header.Get(header); value != "" {
			c.Header(header, value)
		}
	}
	c.Status(resp.StatusCode)
	_, err = io.Copy(c.Writer, resp.Body)
	return err
}

func sourcePlayCacheKey(publicUUID string) string {
	return "sourceplay:" + publicUUID
}
