package services

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"fyms/internal/services/scraper"
)

func fetchTMDBDetailsByID(ctx context.Context, client *TmdbClient, itemType string, tmdbID int64) (map[string]interface{}, error) {
	switch itemType {
	case "Movie":
		return client.GetMovieDetails(ctx, tmdbID)
	case "Series":
		return client.GetTVDetails(ctx, tmdbID)
	default:
		return nil, fmt.Errorf("cannot scrape type: %s", itemType)
	}
}

// =========== scraper.Provider 实现 ===========

func (c *TmdbClient) Name() string { return "tmdb" }

// Priority 数字越小越优先。TMDB 作为基准源置为 1。
func (c *TmdbClient) Priority() int { return 1 }

func (c *TmdbClient) Supports(t scraper.MediaType) bool {
	return t == scraper.MediaMovie || t == scraper.MediaSeries
}

func (c *TmdbClient) Search(ctx context.Context, t scraper.MediaType, q scraper.Query) ([]scraper.Candidate, error) {
	query := q.Title
	if query == "" {
		query = q.OriginalTitle
	}
	if query == "" {
		return nil, nil
	}
	switch t {
	case scraper.MediaMovie:
		results, err := c.SearchMovieMulti(ctx, query, q.Year)
		if err != nil {
			if isNoResultsErr(err) {
				return nil, nil
			}
			return nil, err
		}
		return candidatesFromTMDB(results, "movie"), nil
	case scraper.MediaSeries:
		results, err := c.SearchTVMulti(ctx, query)
		if err != nil {
			if isNoResultsErr(err) {
				return nil, nil
			}
			return nil, err
		}
		return candidatesFromTMDB(results, "tv"), nil
	default:
		return nil, fmt.Errorf("unsupported media type: %s", t)
	}
}

// GetByID 返回统一的 Details 结构，供 Aggregator.Fill（M4）消费。
// 现阶段 applyTMDBDetails 仍直接吃 raw map；Details 路径并行存在，
// 等 M4 字段级合并落地后，raw 路径再逐步切换过去。
func (c *TmdbClient) GetByID(ctx context.Context, t scraper.MediaType, id string) (*scraper.Details, error) {
	tmdbID, err := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
	if err != nil || tmdbID <= 0 {
		return nil, fmt.Errorf("invalid tmdb id: %q", id)
	}
	raw, err := c.fetchRawByID(ctx, t, tmdbID)
	if err != nil {
		return nil, err
	}
	return tmdbDetailsFromRaw(raw, t, tmdbID), nil
}

func (c *TmdbClient) fetchRawByID(ctx context.Context, t scraper.MediaType, id int64) (map[string]interface{}, error) {
	switch t {
	case scraper.MediaMovie:
		return c.GetMovieDetails(ctx, id)
	case scraper.MediaSeries:
		return c.GetTVDetails(ctx, id)
	default:
		return nil, fmt.Errorf("unsupported media type: %s", t)
	}
}

func (c *TmdbClient) FindByExternalID(ctx context.Context, kind, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", nil
	}
	var source string
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "imdb":
		source = "imdb_id"
	case "tmdb":
		return id, nil
	case "tvdb":
		source = "tvdb_id"
	default:
		return "", nil
	}
	u := fmt.Sprintf("%s/find/%s?api_key={API_KEY}&language=%s&external_source=%s",
		TMDB_BASE, url.PathEscape(id), c.language, source)
	data, err := c.tmdbGet(ctx, u)
	if err != nil {
		return "", err
	}
	for _, key := range []string{"movie_results", "tv_results"} {
		arr, ok := data[key].([]interface{})
		if !ok || len(arr) == 0 {
			continue
		}
		if m, ok := arr[0].(map[string]interface{}); ok {
			if id, ok := jsonInt64(m, "id"); ok && id > 0 {
				return strconv.FormatInt(id, 10), nil
			}
		}
	}
	return "", nil
}

func isNoResultsErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "未找到结果") || strings.Contains(s, "no results")
}

// tmdbDetailsFromRaw 把 TMDB 详情 raw map 转为 scraper.Details。
// 提取策略与 applyTMDBDetails 保持一致，便于 M4 切换到 Details 流程时
// 行为不变；actors 上限 20 与现有逻辑一致。
func tmdbDetailsFromRaw(details map[string]interface{}, t scraper.MediaType, id int64) *scraper.Details {
	if details == nil {
		return nil
	}
	titleKey, origKey, dateKey := "title", "original_title", "release_date"
	if t == scraper.MediaSeries {
		titleKey, origKey, dateKey = "name", "original_name", "first_air_date"
	}

	d := &scraper.Details{
		Provider:       "tmdb",
		ProviderID:     strconv.FormatInt(id, 10),
		ExternalIDs:    map[string]string{"tmdb": strconv.FormatInt(id, 10)},
		Certifications: extractTMDBCertifications(details, t),
	}
	if adult, ok := details["adult"].(bool); ok && adult {
		d.AdultContent = true
		d.AdultReasons = []string{"tmdb:adult=true"}
	}
	if s, ok := details[titleKey].(string); ok {
		d.Title = s
	}
	if s, ok := details[origKey].(string); ok {
		d.OriginalTitle = s
	}
	if s, ok := details["overview"].(string); ok {
		d.Overview = s
	}
	if s, ok := details["tagline"].(string); ok {
		d.Tagline = s
	}
	if s, ok := details[dateKey].(string); ok {
		d.Premiered = s
		if len(s) >= 4 {
			if y := parseYearPrefix(s); y > 0 {
				v := int32(y)
				d.Year = &v
			}
		}
	}
	if r := jsonFloat64(details, "vote_average"); r != nil {
		d.Rating = r
	}
	if imdb, ok := details["imdb_id"].(string); ok && imdb != "" {
		d.ExternalIDs["imdb"] = imdb
	}
	if platform := ExtractPlatform(details, map[scraper.MediaType]string{
		scraper.MediaMovie:  "Movie",
		scraper.MediaSeries: "Series",
	}[t]); platform != nil {
		d.Platforms = append(d.Platforms, *platform)
	}

	if arr, ok := details["genres"].([]interface{}); ok {
		for _, g := range arr {
			if gm, ok := g.(map[string]interface{}); ok {
				if n, ok := gm["name"].(string); ok && n != "" {
					d.Genres = append(d.Genres, n)
				}
			}
		}
	}

	// Studios 先不做归一，原样返回；Aggregator 后续调用 ExtractPlatform 统一。
	if arr, ok := details["production_companies"].([]interface{}); ok {
		for _, c := range arr {
			if cm, ok := c.(map[string]interface{}); ok {
				if n, ok := cm["name"].(string); ok && n != "" {
					d.Studios = append(d.Studios, n)
				}
			}
		}
	}

	if credits, ok := details["credits"].(map[string]interface{}); ok {
		if castArr, ok := credits["cast"].([]interface{}); ok {
			limit := min(len(castArr), 20)
			for i, c := range castArr[:limit] {
				cm, ok := c.(map[string]interface{})
				if !ok {
					continue
				}
				name, _ := cm["name"].(string)
				if strings.TrimSpace(name) == "" {
					continue
				}
				role, _ := cm["character"].(string)
				actor := scraper.Actor{Name: name, Role: role, Order: i}
				if aid, ok := jsonInt64(cm, "id"); ok {
					v := int32(aid)
					actor.TmdbID = &v
				}
				if pp, ok := cm["profile_path"].(string); ok && pp != "" {
					u := fmt.Sprintf("%s/w185%s", TMDB_IMAGE_BASE, pp)
					actor.ImageURL = &u
				}
				d.Actors = append(d.Actors, actor)
			}
		}
		if crewArr, ok := credits["crew"].([]interface{}); ok {
			for _, c := range crewArr {
				cm, ok := c.(map[string]interface{})
				if !ok {
					continue
				}
				if job, _ := cm["job"].(string); job == "Director" {
					if dn, ok := cm["name"].(string); ok && dn != "" {
						d.Directors = append(d.Directors, dn)
					}
				}
			}
		}
	}

	if pp, ok := details["poster_path"].(string); ok && pp != "" {
		d.PosterURLs = []string{fmt.Sprintf("%s/w500%s", TMDB_IMAGE_BASE, pp)}
	}
	if bp, ok := details["backdrop_path"].(string); ok && bp != "" {
		d.BackdropURLs = []string{fmt.Sprintf("%s/w1280%s", TMDB_IMAGE_BASE, bp)}
	}

	return d
}

func extractTMDBCertifications(details map[string]interface{}, t scraper.MediaType) []string {
	if details == nil {
		return nil
	}
	var out []string
	switch t {
	case scraper.MediaMovie:
		rd, ok := details["release_dates"].(map[string]interface{})
		if !ok {
			return nil
		}
		results, ok := rd["results"].([]interface{})
		if !ok {
			return nil
		}
		for _, item := range results {
			rm, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			releases, ok := rm["release_dates"].([]interface{})
			if !ok {
				continue
			}
			for _, rel := range releases {
				relMap, ok := rel.(map[string]interface{})
				if !ok {
					continue
				}
				if cert, ok := relMap["certification"].(string); ok {
					if s := strings.TrimSpace(cert); s != "" {
						out = append(out, s)
					}
				}
			}
		}
	case scraper.MediaSeries:
		cr, ok := details["content_ratings"].(map[string]interface{})
		if !ok {
			return nil
		}
		results, ok := cr["results"].([]interface{})
		if !ok {
			return nil
		}
		for _, item := range results {
			rm, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if rating, ok := rm["rating"].(string); ok {
				if s := strings.TrimSpace(rating); s != "" {
					out = append(out, s)
				}
			}
		}
	}
	return dedupeNonEmptyStrings(out)
}

func candidatesFromTMDB(results []map[string]interface{}, kind string) []scraper.Candidate {
	titleKey, origKey, dateKey := "title", "original_title", "release_date"
	if kind == "tv" {
		titleKey, origKey, dateKey = "name", "original_name", "first_air_date"
	}
	out := make([]scraper.Candidate, 0, len(results))
	for _, r := range results {
		cand := scraper.Candidate{}
		if id, ok := jsonInt64(r, "id"); ok && id > 0 {
			cand.ProviderID = strconv.FormatInt(id, 10)
			cand.ExternalIDs = map[string]string{"tmdb": cand.ProviderID}
		}
		if cand.ProviderID == "" {
			continue
		}
		if t, ok := r[titleKey].(string); ok {
			cand.Title = t
		}
		if t, ok := r[origKey].(string); ok {
			cand.OriginalTitle = t
		}
		if d, ok := r[dateKey].(string); ok && len(d) >= 4 {
			if y := parseYearPrefix(d); y > 0 {
				v := int32(y)
				cand.Year = &v
			}
		}
		if pp := jsonFloat64(r, "popularity"); pp != nil {
			cand.Popularity = *pp
		}
		if posterPath, ok := r["poster_path"].(string); ok && strings.TrimSpace(posterPath) != "" {
			cand.PosterURL = fmt.Sprintf("%s/w500%s", TMDB_IMAGE_BASE, posterPath)
		}
		if adult, ok := r["adult"].(bool); ok && adult {
			cand.AdultContent = true
			cand.AdultReasons = []string{"tmdb:adult=true"}
		}
		out = append(out, cand)
	}
	return out
}
