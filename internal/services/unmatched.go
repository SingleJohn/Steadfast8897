package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

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
	if limit <= 0 || limit > 500 {
		limit = 200
	}

	var args []any
	where := `WHERE (i.platform_scan_status = 'unidentified' OR (sq.next_run_at IS NOT NULL AND sq.next_run_at > NOW()))`
	if strings.TrimSpace(itemTypeFilter) != "" {
		args = append(args, itemTypeFilter)
		where += fmt.Sprintf(" AND i.type = $%d", len(args))
	}
	args = append(args, limit)
	limitPlaceholder := fmt.Sprintf("$%d", len(args))

	query := fmt.Sprintf(`
		SELECT i.id::text, i.name, i.type, i.production_year, i.file_path, i.tmdb_id,
		       COALESCE(i.platform_scan_status, ''), i.platform_scan_error,
		       i.platform_scanned_at, sq.next_run_at
		  FROM items i
		  LEFT JOIN scrape_queue sq
		    ON sq.item_id = i.id
		   AND sq.task_type = 'identify'
		   AND sq.status IN ('pending', 'running', 'failed')
		  %s
		  ORDER BY COALESCE(sq.next_run_at, i.platform_scanned_at) DESC NULLS LAST
		  LIMIT %s`, where, limitPlaceholder)

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []UnmatchedItem
	ids := make([]string, 0)
	for rows.Next() {
		var it UnmatchedItem
		if err := rows.Scan(&it.ID, &it.Name, &it.Type, &it.ProductionYear, &it.FilePath, &it.TmdbID,
			&it.ScanStatus, &it.ScanError, &it.ScannedAt, &it.NextRetryAt); err != nil {
			return nil, err
		}
		items = append(items, it)
		ids = append(ids, it.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
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
	if len(itemIDs) == 0 || topN <= 0 {
		return map[string][]identifyCandidateRecord{}, nil
	}
	rows, err := pool.Query(ctx, `
		SELECT id::text, item_id::text, provider, external_id, COALESCE(title, ''),
		       year, COALESCE(poster_url, ''), COALESCE(score, 0), payload, created_at
		  FROM (
		      SELECT *,
		             ROW_NUMBER() OVER (PARTITION BY item_id ORDER BY score DESC, created_at DESC) AS rn
		        FROM identify_candidates
		       WHERE item_id = ANY($1::uuid[])
		  ) t
		 WHERE rn <= $2`, itemIDs, topN)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]identifyCandidateRecord, len(itemIDs))
	for rows.Next() {
		var rec identifyCandidateRecord
		var payload []byte
		if err := rows.Scan(&rec.ID, &rec.ItemID, &rec.Provider, &rec.ExternalID, &rec.Title,
			&rec.Year, &rec.PosterURL, &rec.Score, &payload, &rec.CreatedAt); err != nil {
			return nil, err
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &rec.Payload)
		}
		out[rec.ItemID] = append(out[rec.ItemID], rec)
	}
	return out, rows.Err()
}

// ResolveIdentifyCandidateTMDBID 从一个候选记录反查实际可用的 tmdb_id。
// 批量采纳的公共路径,保持与 applyIdentifyCandidate 单条采纳相同的语义:
// 1) provider=tmdb → 用 external_id
// 2) payload.external_ids.tmdb
// 3) payload.external_ids.imdb → TmdbClient.FindByExternalID 映射
// 4) 候选 title+year → TMDB Search 兜底(Movie/Series 按 item.type 分流)
// 都没有返回友好错误,避免把非 TMDB 候选(如豆瓣)的 external_id 误当 TMDB ID 发请求。
func ResolveIdentifyCandidateTMDBID(ctx context.Context, pool *pgxpool.Pool, itemID, candidateID string) (int64, error) {
	items, err := ListIdentifyCandidates(ctx, pool, itemID)
	if err != nil {
		return 0, err
	}
	for _, item := range items {
		if item.ID != candidateID {
			continue
		}
		if tmdb := pickPayloadExternalID(item.Payload, "tmdb"); tmdb != "" {
			if id, err := parseInt64(tmdb); err == nil && id > 0 {
				return id, nil
			}
		}
		if item.Provider == "tmdb" {
			if id, err := parseInt64(item.ExternalID); err == nil && id > 0 {
				return id, nil
			}
		}
		var client *TmdbClient
		if imdb := pickPayloadExternalID(item.Payload, "imdb"); imdb != "" {
			client = TmdbClientFromConfig(ctx, pool)
			if client != nil {
				if pid, ferr := client.FindByExternalID(ctx, "imdb", imdb); ferr == nil {
					if id, perr := parseInt64(pid); perr == nil && id > 0 {
						return id, nil
					}
				}
			}
		}
		if strings.TrimSpace(item.Title) != "" {
			if client == nil {
				client = TmdbClientFromConfig(ctx, pool)
			}
			if client != nil {
				if id := searchTMDBByTitle(ctx, pool, client, itemID, item.Title, item.Year); id > 0 {
					slog.Info("[identify] resolved via TMDB search fallback",
						"item_id", itemID, "provider", item.Provider, "title", item.Title, "tmdb_id", id)
					return id, nil
				}
			}
		}
		return 0, fmt.Errorf("候选未关联 TMDB ID(provider=%s),请使用「搜索 TMDB」手动选择", item.Provider)
	}
	return 0, fmt.Errorf("候选不存在")
}

// searchTMDBByTitle 最后一级兜底:用候选 title+year 调 TMDB 搜索,取第一条命中。
// 按 item.type 分流到 SearchMovieMulti/SearchTVMulti;命中失败返回 0。
// 只在 imdb 映射失败后调用,避免无谓消耗 TMDB 配额。
func searchTMDBByTitle(ctx context.Context, pool *pgxpool.Pool, client *TmdbClient, itemID, title string, year *int32) int64 {
	var itemType string
	if err := pool.QueryRow(ctx, "SELECT type FROM items WHERE id = $1::uuid", itemID).Scan(&itemType); err != nil {
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
