package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// UnmatchedItem 是未匹配面板返回的 item 视图。
// 带上 top N 候选以便前端无须再次请求 /IdentifyCandidates。
type UnmatchedItem struct {
	ID                string                    `json:"id"`
	Name              string                    `json:"name"`
	Type              string                    `json:"type"`
	ProductionYear    *int32                    `json:"production_year,omitempty"`
	FilePath          *string                   `json:"file_path,omitempty"`
	TmdbID            *int32                    `json:"tmdb_id,omitempty"`
	ScanStatus        string                    `json:"scan_status"`
	ScanError         *string                   `json:"scan_error,omitempty"`
	ScannedAt         *time.Time                `json:"scanned_at,omitempty"`
	IdentifyCooldown  *time.Time                `json:"identify_cooldown_until,omitempty"`
	Candidates        []identifyCandidateRecord `json:"candidates"`
}

// ListUnmatchedItems 返回所有 platform_scan_status='unidentified' 或处于 identify_cooldown_until 未来
// 的 item。按冷却/扫描时间降序。itemTypeFilter 为空则不过滤类型,否则要求大小写完全匹配(Movie/Series)。
func ListUnmatchedItems(ctx context.Context, pool *pgxpool.Pool, itemTypeFilter string, limit int) ([]UnmatchedItem, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}

	var args []any
	where := "WHERE (platform_scan_status = 'unidentified' OR (identify_cooldown_until IS NOT NULL AND identify_cooldown_until > NOW()))"
	if strings.TrimSpace(itemTypeFilter) != "" {
		args = append(args, itemTypeFilter)
		where += fmt.Sprintf(" AND type = $%d", len(args))
	}
	args = append(args, limit)
	limitPlaceholder := fmt.Sprintf("$%d", len(args))

	query := fmt.Sprintf(`
		SELECT id::text, name, type, production_year, file_path, tmdb_id,
		       COALESCE(platform_scan_status, ''), platform_scan_error,
		       platform_scanned_at, identify_cooldown_until
		  FROM items
		  %s
		  ORDER BY COALESCE(identify_cooldown_until, platform_scanned_at) DESC NULLS LAST
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
			&it.ScanStatus, &it.ScanError, &it.ScannedAt, &it.IdentifyCooldown); err != nil {
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
		if imdb := pickPayloadExternalID(item.Payload, "imdb"); imdb != "" {
			if client := TmdbClientFromConfig(ctx, pool); client != nil {
				if pid, ferr := client.FindByExternalID(ctx, "imdb", imdb); ferr == nil {
					if id, perr := parseInt64(pid); perr == nil && id > 0 {
						return id, nil
					}
				}
			}
		}
		return 0, fmt.Errorf("候选未关联 TMDB ID(provider=%s),请使用「搜索 TMDB」手动选择", item.Provider)
	}
	return 0, fmt.Errorf("候选不存在")
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
