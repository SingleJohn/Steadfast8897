package services

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"fyms/internal/repository"
	"fyms/internal/services/scraper"

	"github.com/jackc/pgx/v5/pgxpool"
)

// UnmatchedItem 是未匹配面板返回的 item 视图。
// 带上 top N 候选以便前端无须再次请求 /IdentifyCandidates。
//
// NextRetryAt 由 scrape_queue LEFT JOIN 得出(Phase 5 起替代原来的 identify_cooldown_until):
// 只要 scrape_queue 里有这个 item 的 identify 任务处于 pending/running/failed,就取 next_run_at。
type UnmatchedItem struct {
	ID             string                    `json:"id"`
	Name           string                    `json:"name"`
	Type           string                    `json:"type"`
	ProductionYear *int32                    `json:"production_year,omitempty"`
	FilePath       *string                   `json:"file_path,omitempty"`
	TmdbID         *int32                    `json:"tmdb_id,omitempty"`
	ScanStatus     string                    `json:"scan_status"`
	ScanError      *string                   `json:"scan_error,omitempty"`
	ScannedAt      *time.Time                `json:"scanned_at,omitempty"`
	NextRetryAt    *time.Time                `json:"next_retry_at,omitempty"`
	Candidates     []identifyCandidateRecord `json:"candidates"`
}

// ListUnmatchedItems 返回所有 platform_scan_status='unidentified' 或在 scrape_queue 里
// 有 identify 任务待重试(next_run_at > NOW)的 item。按重试时间/扫描时间降序。
// itemTypeFilter 为空则不过滤类型,否则要求大小写完全匹配(Movie/Series)。
func ListUnmatchedItems(ctx context.Context, pool *pgxpool.Pool, itemTypeFilter string, limit int) ([]UnmatchedItem, error) {
	rows, err := repository.NewBackgroundTaskRepository(pool).ListUnmatchedItems(ctx, itemTypeFilter, limit)
	if err != nil {
		return nil, err
	}

	items := make([]UnmatchedItem, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		items = append(items, UnmatchedItem{
			ID:             row.ID,
			Name:           row.Name,
			Type:           row.Type,
			ProductionYear: row.ProductionYear,
			FilePath:       row.FilePath,
			TmdbID:         row.TmdbID,
			ScanStatus:     row.ScanStatus,
			ScanError:      row.ScanError,
			ScannedAt:      row.ScannedAt,
			NextRetryAt:    row.NextRetryAt,
		})
		ids = append(ids, row.ID)
	}
	if len(items) == 0 {
		return items, nil
	}

	// 批量拉候选,每 item top3。
	candidatesByItem, err := listIdentifyCandidatesBatch(ctx, pool, ids, 3)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Candidates = candidatesByItem[items[i].ID]
	}
	return items, nil
}

// listIdentifyCandidatesBatch 为多个 item 一次查出各自 topN 候选。用窗口函数避免 N+1 查询。
func listIdentifyCandidatesBatch(ctx context.Context, pool *pgxpool.Pool, itemIDs []string, topN int) (map[string][]identifyCandidateRecord, error) {
	return repository.NewBackgroundTaskRepository(pool).ListIdentifyCandidatesBatch(ctx, itemIDs, topN)
}

// ResolveIdentifyCandidate 把一条候选反解为 "采纳时应使用的 (provider, externalID)"。
// 优先返回 ("tmdb", tmdbID):尝试把候选映射到 TMDB,能映射就走 TMDB 路径(Series
// 可继续抓 episode)。依次尝试:
//  1. payload.external_ids.tmdb
//  2. provider=tmdb → external_id
//  3. payload.external_ids.imdb / tvdb → TmdbClient.FindByExternalID
//  4. 非 tmdb 候选:调 provider.GetByID 补 external ids 后再 FindByExternalID(豆瓣/Bangumi
//     详情页通常含 imdb,Candidates 阶段的 payload 里没有)
//  5. 候选 title+year → TMDB Search 兜底
//
// 全部映射失败时回落到候选原 provider,返回 (candidate.Provider, candidate.ExternalID):
// 调用方用 ScrapeItemByProviderID 走非 TMDB 路径入库(Series 无 episode 但其它字段齐全)。
// 候选不存在或无 external_id 才返回 error。
func ResolveIdentifyCandidate(ctx context.Context, pool *pgxpool.Pool, itemID, candidateID string) (provider, externalID string, err error) {
	items, lerr := ListIdentifyCandidates(ctx, pool, itemID)
	if lerr != nil {
		return "", "", lerr
	}
	var candidate *identifyCandidateRecord
	for i := range items {
		if items[i].ID == candidateID {
			candidate = &items[i]
			break
		}
	}
	if candidate == nil {
		return "", "", fmt.Errorf("候选不存在")
	}

	if tmdbID := tryResolveToTMDBID(ctx, pool, itemID, candidate); tmdbID > 0 {
		return "tmdb", strconv.FormatInt(tmdbID, 10), nil
	}

	if strings.TrimSpace(candidate.ExternalID) == "" {
		return "", "", fmt.Errorf("候选 %s 没有 external_id,无法采纳", candidate.Provider)
	}
	return candidate.Provider, candidate.ExternalID, nil
}

// ResolveIdentifyCandidateTMDBID 兼容旧调用点(要求最终必须是 tmdb_id)。
// 非 tmdb 结果返回友好错误,不再向上抛。
func ResolveIdentifyCandidateTMDBID(ctx context.Context, pool *pgxpool.Pool, itemID, candidateID string) (int64, error) {
	provider, externalID, err := ResolveIdentifyCandidate(ctx, pool, itemID, candidateID)
	if err != nil {
		return 0, err
	}
	if provider != "tmdb" {
		return 0, fmt.Errorf("候选未关联 TMDB ID(provider=%s),请使用「搜索 TMDB」手动选择", provider)
	}
	id, perr := parseInt64(externalID)
	if perr != nil || id <= 0 {
		return 0, fmt.Errorf("invalid tmdb id from candidate: %s", externalID)
	}
	return id, nil
}

// tryResolveToTMDBID 尝试把候选映射到 tmdb_id,失败返回 0。
func tryResolveToTMDBID(ctx context.Context, pool *pgxpool.Pool, itemID string, candidate *identifyCandidateRecord) int64 {
	// 1) payload.external_ids.tmdb
	if tmdb := pickPayloadExternalID(candidate.Payload, "tmdb"); tmdb != "" {
		if id, err := parseInt64(tmdb); err == nil && id > 0 {
			return id
		}
	}
	// 2) provider=tmdb
	if candidate.Provider == "tmdb" {
		if id, err := parseInt64(candidate.ExternalID); err == nil && id > 0 {
			return id
		}
	}

	var client *TmdbClient
	ensureClient := func() *TmdbClient {
		if client == nil {
			client = TmdbClientFromConfig(ctx, pool)
		}
		return client
	}
	tryMapToTMDB := func(kind, externalID string) int64 {
		kind = strings.ToLower(strings.TrimSpace(kind))
		externalID = strings.TrimSpace(externalID)
		if kind == "" || externalID == "" {
			return 0
		}
		c := ensureClient()
		if c == nil {
			return 0
		}
		pid, err := c.FindByExternalID(ctx, kind, externalID)
		if err != nil {
			return 0
		}
		if id, perr := parseInt64(pid); perr == nil && id > 0 {
			return id
		}
		return 0
	}

	// 3) payload.external_ids.imdb / tvdb
	for _, kind := range []string{"imdb", "tvdb"} {
		if id := tryMapToTMDB(kind, pickPayloadExternalID(candidate.Payload, kind)); id > 0 {
			return id
		}
	}

	// 4) 非 tmdb 候选:走 provider.GetByID 拉详情,补 external_ids
	if candidate.Provider != "tmdb" && strings.TrimSpace(candidate.ExternalID) != "" {
		if details := fetchCandidateDetails(ctx, pool, candidate); details != nil {
			if tmdb := strings.TrimSpace(details.ExternalIDs["tmdb"]); tmdb != "" {
				if id, err := parseInt64(tmdb); err == nil && id > 0 {
					slog.Info("[identify] resolved via provider GetByID → tmdb",
						"item_id", itemID, "provider", candidate.Provider, "tmdb_id", id)
					return id
				}
			}
			for _, kind := range []string{"imdb", "tvdb"} {
				if id := tryMapToTMDB(kind, details.ExternalIDs[kind]); id > 0 {
					slog.Info("[identify] resolved via provider GetByID → external_id → tmdb",
						"item_id", itemID, "provider", candidate.Provider, "kind", kind, "tmdb_id", id)
					return id
				}
			}
		}
	}

	// 5) title+year → TMDB search 兜底
	if strings.TrimSpace(candidate.Title) != "" {
		c := ensureClient()
		if c != nil {
			if id := searchTMDBByTitle(ctx, pool, c, itemID, candidate.Title, candidate.Year); id > 0 {
				slog.Info("[identify] resolved via TMDB search fallback",
					"item_id", itemID, "provider", candidate.Provider, "title", candidate.Title, "tmdb_id", id)
				return id
			}
		}
	}
	return 0
}

// fetchCandidateDetails 调对应 provider 的 GetByID 拿详情。用于把非 TMDB 候选
// 展开以补 external_ids(典型场景:豆瓣/Bangumi 详情页才会出现 imdb_id)。
// provider 未启用 / GetByID 失败 / item type 不是 Movie/Series → 返回 nil,调用方降级。
func fetchCandidateDetails(ctx context.Context, pool *pgxpool.Pool, cand *identifyCandidateRecord) *scraper.Details {
	mediaType, err := loadItemMediaType(ctx, pool, cand.ItemID)
	if err != nil || mediaType == "" {
		return nil
	}
	client := TmdbClientFromConfig(ctx, pool)
	if client == nil {
		return nil
	}
	cfg := scraper.LoadRuntimeConfig(ctx, pool)
	agg := GetScrapeAggregator(sharedScrapeCache, cfg, client, client.httpClient)
	provider := agg.ProviderByName(cand.Provider)
	if provider == nil {
		return nil
	}
	details, err := provider.GetByID(ctx, mediaType, cand.ExternalID)
	if err != nil {
		slog.Debug("[identify] provider GetByID failed",
			"item_id", cand.ItemID, "provider", cand.Provider, "external_id", cand.ExternalID, "error", err)
		return nil
	}
	return details
}

func loadItemMediaType(ctx context.Context, pool *pgxpool.Pool, itemID string) (scraper.MediaType, error) {
	itemType, err := repository.NewBackgroundTaskRepository(pool).GetItemMediaType(ctx, itemID)
	if err != nil {
		return "", err
	}
	switch itemType {
	case "Movie":
		return scraper.MediaMovie, nil
	case "Series":
		return scraper.MediaSeries, nil
	default:
		return "", nil
	}
}

// searchTMDBByTitle 最后一级兜底:用候选 title+year 调 TMDB 搜索,取第一条命中。
// 按 item.type 分流到 SearchMovieMulti/SearchTVMulti;命中失败返回 0。
// 只在 imdb 映射失败后调用,避免无谓消耗 TMDB 配额。
func searchTMDBByTitle(ctx context.Context, pool *pgxpool.Pool, client *TmdbClient, itemID, title string, year *int32) int64 {
	itemType, itemErr := repository.NewBackgroundTaskRepository(pool).GetItemMediaType(ctx, itemID)
	if itemErr != nil {
		return 0
	}
	var results []map[string]any
	var err error
	switch itemType {
	case "Movie":
		results, err = client.SearchMovieMulti(ctx, title, year)
	case "Series":
		results, err = client.SearchTVMulti(ctx, title)
	default:
		return 0
	}
	if err != nil || len(results) == 0 {
		return 0
	}
	if id, ok := jsonInt64(results[0], "id"); ok && id > 0 {
		return id
	}
	return 0
}

// pickPayloadExternalID 从 candidate.payload.external_ids 取指定 kind 的值。
// payload 反序列化后有 map[string]any / map[string]string 两种形态,都要兼容。
func pickPayloadExternalID(payload map[string]any, kind string) string {
	if payload == nil {
		return ""
	}
	raw, ok := payload["external_ids"]
	if !ok {
		return ""
	}
	switch m := raw.(type) {
	case map[string]any:
		if v, ok := m[kind].(string); ok {
			return strings.TrimSpace(v)
		}
	case map[string]string:
		return strings.TrimSpace(m[kind])
	}
	return ""
}

func parseInt64(s string) (int64, error) {
	var v int64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &v)
	return v, err
}
