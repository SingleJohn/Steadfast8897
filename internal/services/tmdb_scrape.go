package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/models"
	"fyms/internal/services/scraper"
)

func ScrapeItem(ctx context.Context, pool *pgxpool.Pool, itemID string) (map[string]interface{}, error) {
	client := TmdbClientFromConfig(ctx, pool)
	if client == nil {
		return nil, fmt.Errorf("TMDB API key not configured")
	}
	return ScrapeItemWithClient(ctx, pool, itemID, client)
}

func RefreshItemMetadataByTMDBID(ctx context.Context, pool *pgxpool.Pool, itemID string, client *TmdbClient) (map[string]interface{}, error) {
	meta, err := loadScrapeItemMeta(ctx, pool, itemID)
	if err != nil {
		return nil, err
	}
	if meta.TmdbID == nil || *meta.TmdbID == 0 {
		return nil, fmt.Errorf("no TMDB ID")
	}
	mediaType, ok := mediaTypeFor(meta.ItemType)
	if !ok {
		return nil, fmt.Errorf("cannot scrape type: %s", meta.ItemType)
	}
	tmdbIDStr := strconv.FormatInt(int64(*meta.TmdbID), 10)
	ident := &scraper.Identity{
		Provider:    "tmdb",
		ProviderID:  tmdbIDStr,
		ExternalIDs: map[string]string{"tmdb": tmdbIDStr},
		Score:       1,
		Source:      "tmdb_id_refresh",
	}
	parsed := buildParsedName(meta)
	agg := GetScrapeAggregatorForLibrary(ctx, pool, sharedScrapeCache, client, client.httpClient, meta.LibraryID)
	merged, fillErr := agg.Fill(ctx, ident, parsed, mediaType)
	if fillErr != nil {
		return nil, fmt.Errorf("fill details: %w", fillErr)
	}
	return applyMergedDetails(ctx, pool, itemID, client, meta.ItemType, int64(*meta.TmdbID), merged, false, models.PlatformScanSourceTMDB)
}

func RefreshPlatformOnlyByTMDBID(ctx context.Context, pool *pgxpool.Pool, itemID string, client *TmdbClient) (*string, error) {
	meta, err := loadScrapeItemMeta(ctx, pool, itemID)
	if err != nil {
		return nil, err
	}
	if meta.TmdbID == nil || *meta.TmdbID == 0 {
		return nil, fmt.Errorf("no TMDB ID")
	}
	details, err := fetchTMDBDetailsByID(ctx, client, meta.ItemType, int64(*meta.TmdbID))
	if err != nil {
		return nil, err
	}
	studio := ExtractPlatform(details, meta.ItemType)
	if studio == nil {
		if err := models.MarkPlatformScanNoMatch(ctx, pool, itemID, models.PlatformScanSourceTMDB, "no platform matched from TMDB details"); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err := models.MarkPlatformScanMatched(ctx, pool, itemID, *studio, models.PlatformScanSourceTMDB); err != nil {
		return nil, err
	}
	if meta.ItemType == "Series" {
		if err := models.PropagateStudioToChildren(ctx, pool, itemID, *studio); err != nil {
			return nil, err
		}
	}
	return studio, nil
}

// ScrapeItemWithClient scrapes TMDB metadata for a single item using the provided client.
// ScrapeItemByTMDBID scrapes an item using an explicit TMDB ID (from user selection).
// 保留对外 API,内部转发到 ScrapeItemByProviderID 的 tmdb 路径。
func ScrapeItemByTMDBID(ctx context.Context, pool *pgxpool.Pool, itemID string, tmdbID int64) (map[string]interface{}, error) {
	if tmdbID <= 0 {
		return nil, fmt.Errorf("invalid tmdb id: %d", tmdbID)
	}
	return ScrapeItemByProviderID(ctx, pool, itemID, "tmdb", strconv.FormatInt(tmdbID, 10))
}

// ScrapeItemByProviderID 按 (provider, externalID) 刮削 item,支持任意已注册 provider 作为 primary。
// 流程:
//   - provider=tmdb: 构造 tmdb Identity → agg.Fill 多源合并字段 → applyMergedDetails 写入
//   - provider!=tmdb: 先调 primary.GetByID 拿详情,把 Details.ExternalIDs(通常含 imdb)
//     合并进 Identity,让 Fill.fetchSecondary 能跨源映射回 TMDB;然后走同样 Fill+apply 流程
//   - merged.ExternalIDs.tmdb 有值时反写 items.tmdb_id(Series 可以继续抓 episode)
//   - 无 tmdb_id 也能成功入库(基本字段齐全,Series 暂无分集)
func ScrapeItemByProviderID(ctx context.Context, pool *pgxpool.Pool, itemID, provider, externalID string) (map[string]interface{}, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	externalID = strings.TrimSpace(externalID)
	if provider == "" || externalID == "" {
		return nil, fmt.Errorf("provider / externalID required")
	}
	client := TmdbClientFromConfig(ctx, pool)
	if client == nil {
		return nil, fmt.Errorf("TMDB API 密钥未配置")
	}
	meta, err := loadScrapeItemMeta(ctx, pool, itemID)
	if err != nil {
		return nil, err
	}
	mediaType, ok := mediaTypeFor(meta.ItemType)
	if !ok {
		return nil, fmt.Errorf("cannot scrape type: %s", meta.ItemType)
	}

	agg := GetScrapeAggregatorForLibrary(ctx, pool, sharedScrapeCache, client, client.httpClient, meta.LibraryID)

	ident := &scraper.Identity{
		Provider:    provider,
		ProviderID:  externalID,
		ExternalIDs: map[string]string{provider: externalID},
		Score:       1,
		Source:      "manual_" + provider + "_id",
	}

	// 非 tmdb primary:先拉一次 provider 详情,把 Details.ExternalIDs 合入 Identity。
	// 豆瓣 Candidates 阶段 ExternalIDs 只有 douban id,imdb 要等详情页才有;
	// 合并后 Aggregator.Fill.fetchSecondary 才能通过 imdb 跨源到 TMDB。
	if provider != "tmdb" {
		primary := agg.ProviderByName(provider)
		if primary == nil {
			return nil, fmt.Errorf("provider %s 未启用或未注册", provider)
		}
		primaryDetails, derr := primary.GetByID(ctx, mediaType, externalID)
		if derr != nil {
			return nil, fmt.Errorf("拉取 %s 详情失败: %w", provider, derr)
		}
		if primaryDetails == nil {
			return nil, fmt.Errorf("%s 详情为空(external_id=%s)", provider, externalID)
		}
		for k, v := range primaryDetails.ExternalIDs {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if _, ok := ident.ExternalIDs[k]; !ok {
				ident.ExternalIDs[k] = v
			}
		}
	}

	parsed := buildParsedName(meta)
	merged, fillErr := agg.Fill(ctx, ident, parsed, mediaType)
	if fillErr != nil {
		return nil, fmt.Errorf("fill details: %w", fillErr)
	}

	// merged.ExternalIDs 是 primary + 所有辅源的 union,带 tmdb 则反写 items.tmdb_id
	var tmdbID int64
	if raw := strings.TrimSpace(merged.ExternalIDs["tmdb"]); raw != "" {
		if v, perr := strconv.ParseInt(raw, 10, 64); perr == nil && v > 0 {
			tmdbID = v
		}
	}

	return applyMergedDetails(ctx, pool, itemID, client, meta.ItemType, tmdbID, merged, tmdbID > 0, models.PlatformScanSourceSearch)
}

// SearchTMDBForItem searches TMDB for an item by custom query or explicit TMDB ID.
func SearchTMDBForItem(ctx context.Context, pool *pgxpool.Pool, itemID, query string, year *int32, tmdbID *int64) ([]map[string]interface{}, error) {
	client := TmdbClientFromConfig(ctx, pool)
	if client == nil {
		return nil, fmt.Errorf("TMDB API 密钥未配置")
	}
	meta, err := loadScrapeItemMeta(ctx, pool, itemID)
	if err != nil {
		return nil, err
	}
	switch meta.ItemType {
	case "Movie":
		if tmdbID != nil {
			details, err := client.GetMovieDetails(ctx, *tmdbID)
			if err != nil {
				return nil, fmt.Errorf("未找到电影类型的 TMDB 条目: %w", err)
			}
			if details == nil || details["id"] == nil {
				return nil, fmt.Errorf("未找到电影类型的 TMDB 条目")
			}
			return []map[string]interface{}{tmdbCandidateFromDetails(details, "Movie")}, nil
		}
		return client.SearchMovieMulti(ctx, query, year)
	case "Series":
		if tmdbID != nil {
			details, err := client.GetTVDetails(ctx, *tmdbID)
			if err != nil {
				return nil, fmt.Errorf("未找到剧集类型的 TMDB 条目: %w", err)
			}
			if details == nil || details["id"] == nil {
				return nil, fmt.Errorf("未找到剧集类型的 TMDB 条目")
			}
			return []map[string]interface{}{tmdbCandidateFromDetails(details, "Series")}, nil
		}
		return client.SearchTVMulti(ctx, query)
	default:
		return nil, fmt.Errorf("不支持的类型: %s", meta.ItemType)
	}
}

func tmdbCandidateFromDetails(details map[string]interface{}, itemType string) map[string]interface{} {
	out := map[string]interface{}{
		"id":           details["id"],
		"poster_path":  details["poster_path"],
		"overview":     details["overview"],
		"vote_average": details["vote_average"],
	}
	if itemType == "Movie" {
		out["title"] = details["title"]
		out["release_date"] = details["release_date"]
	} else {
		out["name"] = details["name"]
		out["first_air_date"] = details["first_air_date"]
	}
	return out
}

func ScrapeItemWithClient(ctx context.Context, pool *pgxpool.Pool, itemID string, client *TmdbClient) (map[string]interface{}, error) {
	meta, err := loadScrapeItemMeta(ctx, pool, itemID)
	if err != nil {
		return nil, err
	}

	mediaType, ok := mediaTypeFor(meta.ItemType)
	if !ok {
		return nil, fmt.Errorf("cannot scrape type: %s", meta.ItemType)
	}

	parsed := buildParsedName(meta)
	runtimeCfg := LoadEffectiveScrapeConfig(ctx, pool, meta.LibraryID)
	agg := GetScrapeAggregator(sharedScrapeCache, runtimeCfg, client, client.httpClient)

	slog.Info("[Identify] start",
		"item_id", itemID, "type", meta.ItemType,
		"raw_name", meta.Name,
		"parsed_title", parsed.Title, "parsed_original", parsed.OriginalTitle,
		"parsed_year", formatYear(parsed.Year),
		"parsed_ids", parsed.IDs,
		"providers", agg.Providers(),
		"threshold", runtimeCfg.ConfidenceThreshold)

	// 已经带 TMDB ID 的 item 跳过 Identify 直接 Fill。
	// 注意:不能只喂 tmdb details 给 MergeDetails,那样会绕过辅源。
	// agg.Fill 内部会把 tmdb 作为 primary 拉详情,再并发拉 bangumi/douban/tvdb/fanart
	// 按 FieldPolicy 合字段(rating / poster / overview 等)。
	if meta.TmdbID != nil && *meta.TmdbID > 0 {
		tmdbIDStr := strconv.FormatInt(int64(*meta.TmdbID), 10)
		ident := &scraper.Identity{
			Provider:    "tmdb",
			ProviderID:  tmdbIDStr,
			ExternalIDs: map[string]string{"tmdb": tmdbIDStr},
			Score:       1,
			Source:      "tmdb_id_direct",
		}
		merged, fillErr := agg.Fill(ctx, ident, parsed, mediaType)
		if fillErr != nil {
			var adultErr *scraper.ErrAdultContentFiltered
			if errors.As(fillErr, &adultErr) {
				detail := buildAdultBlockedDetail(
					"fill",
					"fill blocked by adult-content filter",
					parsed,
					runtimeCfg,
					agg.Providers(),
					ident,
					nil,
					adultErr.Blocked,
				)
				DiagFrom(ctx).SetDetail(detail)
				logScrapeFailureDetail(itemID, detail)
				tmdbSetIdentifyAttempted(ctx, pool, itemID)
				if markErr := models.MarkPlatformScanUnidentified(ctx, pool, itemID, models.PlatformScanSourceTMDB, "fill blocked by adult-content filter"); markErr != nil {
					slog.Warn("[TMDB] mark adult-content filtered fill failed", "item_id", itemID, "error", markErr)
				}
				return nil, fillErr
			}
			return nil, fmt.Errorf("fill details: %w", fillErr)
		}
		tmdbSetIdentifyAttempted(ctx, pool, itemID)
		return applyMergedDetails(ctx, pool, itemID, client, meta.ItemType, int64(*meta.TmdbID), merged, false, models.PlatformScanSourceTMDB)
	}

	ident, err := agg.Identify(ctx, parsed, mediaType)
	if err != nil {
		reason := err.Error()
		source := models.PlatformScanSourceSearch
		var adultErr *scraper.ErrAdultContentFiltered
		if errors.As(err, &adultErr) {
			detail := buildAdultBlockedDetail(
				"identify",
				"identify blocked by adult-content filter",
				parsed,
				runtimeCfg,
				agg.Providers(),
				nil,
				nil,
				adultErr.Blocked,
			)
			DiagFrom(ctx).SetDetail(detail)
			logScrapeFailureDetail(itemID, detail)
			tmdbSetIdentifyAttempted(ctx, pool, itemID)
			reason = "identify blocked by adult-content filter"
			if markErr := models.MarkPlatformScanUnidentified(ctx, pool, itemID, source, reason); markErr != nil {
				slog.Warn("[TMDB] mark platform scan unidentified failed", "item_id", itemID, "error", markErr)
			}
			return nil, err
		}
		if errors.Is(err, scraper.ErrNoMatch) {
			// 捞一次候选列表,用于诊断日志 + (可选)人工确认队列。
			// 无论 AutoApply 如何都需要,有 cache 不会多发 TMDB 请求。
			candidates, _ := agg.Candidates(ctx, parsed, mediaType)
			detail := buildIdentifyFailureDetail(parsed, candidates, runtimeCfg.ConfidenceThreshold, agg.Providers(), runtimeCfg.AutoApply, runtimeCfg.AdultContentFilterEnabled)
			DiagFrom(ctx).SetDetail(detail)
			logScrapeFailureDetail(itemID, detail)

			if !runtimeCfg.AutoApply {
				if len(candidates) > 0 {
					_ = replaceIdentifyCandidates(ctx, pool, itemID, candidates)
					tmdbSetIdentifyAttempted(ctx, pool, itemID)
					if markErr := models.MarkPlatformScanUnidentified(ctx, pool, itemID, source, "identify queued for manual confirmation"); markErr != nil {
						slog.Warn("[TMDB] mark manual identify queue failed", "item_id", itemID, "error", markErr)
					}
					return nil, fmt.Errorf("identify queued for manual confirmation")
				}
			}
			tmdbSetIdentifyAttempted(ctx, pool, itemID)
			reason = "identify failed: no confident match"
			if markErr := models.MarkPlatformScanUnidentified(ctx, pool, itemID, source, reason); markErr != nil {
				slog.Warn("[TMDB] mark platform scan unidentified failed", "item_id", itemID, "error", markErr)
			}
			return nil, err
		}
		slog.Warn("[Identify] error (not ErrNoMatch)",
			"item_id", itemID, "parsed_title", parsed.Title, "error", reason)
		if markErr := models.MarkPlatformScanError(ctx, pool, itemID, source, reason); markErr != nil {
			slog.Warn("[TMDB] mark platform scan error failed", "item_id", itemID, "error", markErr)
		}
		return nil, err
	}

	tmdbID := resolveTMDBIDFromIdentity(ident)
	merged, fillErr := agg.Fill(ctx, ident, parsed, mediaType)
	if fillErr != nil {
		var adultErr *scraper.ErrAdultContentFiltered
		if errors.As(fillErr, &adultErr) {
			detail := buildAdultBlockedDetail(
				"fill",
				"fill blocked by adult-content filter",
				parsed,
				runtimeCfg,
				agg.Providers(),
				ident,
				nil,
				adultErr.Blocked,
			)
			DiagFrom(ctx).SetDetail(detail)
			logScrapeFailureDetail(itemID, detail)
			tmdbSetIdentifyAttempted(ctx, pool, itemID)
			if markErr := models.MarkPlatformScanUnidentified(ctx, pool, itemID, models.PlatformScanSourceSearch, "fill blocked by adult-content filter"); markErr != nil {
				slog.Warn("[TMDB] mark adult-content filtered fill failed", "item_id", itemID, "error", markErr)
			}
			return nil, fillErr
		}
		return nil, fmt.Errorf("fill details: %w", fillErr)
	}
	// 非 TMDB primary 时,Fill 内部辅源 TMDB 可能通过 imdb 跨源映射拿到 tmdb_id,
	// 从 merged.ExternalIDs 补解一次,有值就反写 items.tmdb_id 并启用 Series episode 抓取。
	if tmdbID <= 0 {
		if raw := strings.TrimSpace(merged.ExternalIDs["tmdb"]); raw != "" {
			if v, perr := strconv.ParseInt(raw, 10, 64); perr == nil && v > 0 {
				tmdbID = v
			}
		}
	}
	tmdbSetIdentifyAttempted(ctx, pool, itemID)
	slog.Info("[Identify] matched",
		"item_id", itemID,
		"parsed_title", parsed.Title, "parsed_year", formatYear(parsed.Year),
		"provider", ident.Provider, "provider_id", ident.ProviderID,
		"source", ident.Source, "score", fmt.Sprintf("%.3f", ident.Score),
		"tmdb_id", tmdbID)
	return applyMergedDetails(ctx, pool, itemID, client, meta.ItemType, tmdbID, merged, tmdbID > 0, models.PlatformScanSourceSearch)
}
