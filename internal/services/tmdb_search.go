package services

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

func (c *TmdbClient) SearchMovie(ctx context.Context, name string, year *int32) (map[string]interface{}, error) {
	u := fmt.Sprintf("%s/search/movie?api_key={API_KEY}&language=%s&query=%s",
		TMDB_BASE, c.language, url.QueryEscape(name))
	if year != nil {
		u += fmt.Sprintf("&year=%d", *year)
	}
	data, err := c.tmdbGet(ctx, u)
	if err != nil {
		return nil, err
	}
	results, ok := data["results"].([]interface{})
	if !ok || len(results) == 0 {
		return nil, fmt.Errorf("no results")
	}
	first, ok := results[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid result format")
	}
	return first, nil
}

func (c *TmdbClient) SearchTV(ctx context.Context, name string) (map[string]interface{}, error) {
	u := fmt.Sprintf("%s/search/tv?api_key={API_KEY}&language=%s&query=%s",
		TMDB_BASE, c.language, url.QueryEscape(name))
	data, err := c.tmdbGet(ctx, u)
	if err != nil {
		return nil, err
	}
	results, ok := data["results"].([]interface{})
	if !ok || len(results) == 0 {
		return nil, fmt.Errorf("no results")
	}
	first, ok := results[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid result format")
	}
	return first, nil
}

// SearchMovieMulti returns up to 20 TMDB movie search results.
// 带 year 过滤 0 结果时自动 fallback 去掉 year 重试一次 —— 常见场景:
//   - item 之前被错误识别,production_year 被污染,用户自定义搜索时 year 预填错值
//   - 文件名里的年份是再发行年/目录年,不是 TMDB 的首映年
//
// Matcher 的 scoreCandidate 会用 parsed.Year 给候选打分,年份不一致的候选分数低,
// 所以放宽 year 过滤不会降低识别准确度,只会提高召回。
func (c *TmdbClient) SearchMovieMulti(ctx context.Context, name string, year *int32) ([]map[string]interface{}, error) {
	out, err := c.searchMovieOnce(ctx, name, year)
	if err != nil {
		return nil, err
	}
	if len(out) > 0 {
		return out, nil
	}
	if year != nil {
		slog.Debug("[TMDB] search with year returned 0, retrying without year",
			"query", name, "year", *year)
		out, err = c.searchMovieOnce(ctx, name, nil)
		if err != nil {
			return nil, err
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return nil, fmt.Errorf("未找到结果")
}

func (c *TmdbClient) searchMovieOnce(ctx context.Context, name string, year *int32) ([]map[string]interface{}, error) {
	u := fmt.Sprintf("%s/search/movie?api_key={API_KEY}&language=%s&query=%s&include_adult=false",
		TMDB_BASE, c.language, url.QueryEscape(name))
	if year != nil {
		u += fmt.Sprintf("&year=%d", *year)
	}
	data, err := c.tmdbGet(ctx, u)
	if err != nil {
		return nil, err
	}
	results, ok := data["results"].([]interface{})
	if !ok || len(results) == 0 {
		return nil, nil
	}
	var out []map[string]interface{}
	for _, r := range results {
		if m, ok := r.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

// SearchTVMulti returns up to 20 TMDB TV search results.
func (c *TmdbClient) SearchTVMulti(ctx context.Context, name string) ([]map[string]interface{}, error) {
	u := fmt.Sprintf("%s/search/tv?api_key={API_KEY}&language=%s&query=%s&include_adult=false",
		TMDB_BASE, c.language, url.QueryEscape(name))
	data, err := c.tmdbGet(ctx, u)
	if err != nil {
		return nil, err
	}
	results, ok := data["results"].([]interface{})
	if !ok || len(results) == 0 {
		return nil, fmt.Errorf("未找到结果")
	}
	var out []map[string]interface{}
	for _, r := range results {
		if m, ok := r.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

func (c *TmdbClient) GetMovieDetails(ctx context.Context, tmdbID int64) (map[string]interface{}, error) {
	u := fmt.Sprintf("%s/movie/%d?api_key={API_KEY}&language=%s&append_to_response=credits,release_dates",
		TMDB_BASE, tmdbID, c.language)
	return c.tmdbGet(ctx, u)
}

func (c *TmdbClient) GetTVDetails(ctx context.Context, tmdbID int64) (map[string]interface{}, error) {
	u := fmt.Sprintf("%s/tv/%d?api_key={API_KEY}&language=%s&append_to_response=credits,content_ratings",
		TMDB_BASE, tmdbID, c.language)
	return c.tmdbGet(ctx, u)
}

func (c *TmdbClient) GetSeasonImages(ctx context.Context, tmdbID int64, seasonNum int32) *string {
	u := fmt.Sprintf("%s/tv/%d/season/%d?api_key={API_KEY}&language=%s",
		TMDB_BASE, tmdbID, seasonNum, c.language)
	data, err := c.tmdbGet(ctx, u)
	if err != nil {
		return nil
	}
	if pp, ok := data["poster_path"].(string); ok && pp != "" {
		return &pp
	}
	return nil
}

func (c *TmdbClient) DownloadImage(ctx context.Context, imgPath, savePath, size string) bool {
	imgURL := fmt.Sprintf("%s/%s%s", TMDB_IMAGE_BASE, size, imgPath)
	return c.downloadImageURL(ctx, imgURL, savePath)
}

func (c *TmdbClient) downloadImageURL(ctx context.Context, imgURL, savePath string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imgURL, nil)
	if err != nil {
		return false
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	dir := filepath.Dir(savePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false
	}

	return os.WriteFile(savePath, data, 0644) == nil
}
