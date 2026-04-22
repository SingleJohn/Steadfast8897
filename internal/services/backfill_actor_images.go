package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// processBackfillActorImagesTask 给 Series / Movie 补演员头像 URL。
//
// 背景:NFO 扫描写 cast_members 时只有 name / role / tmdb_id(人物 ID),没有 image_url
// —— 因为标准 NFO 格式不携带头像 URL。TMDB credits 接口能按 person_id 拿到 profile_path,
// 拼 w185 URL 存进 cast_members.image_url 后,serveImage 会按需 materialize 缓存。
//
// 匹配规则按优先级:
//  1. cast_members.tmdb_id = cast.id(稳定、唯一)
//  2. 同名 + 同 character(退化场景,处理旧 NFO 未带 tmdbid 的人物)
//
// 只更新 image_url IS NULL 或空串的行,不覆盖用户已手动设置的头像。
// TMDB 接口单次调用即可(已带 append_to_response=credits),无 N+1 请求。
func processBackfillActorImagesTask(ctx context.Context, pool *pgxpool.Pool, client *TmdbClient, itemID string) error {
	var itemType string
	var tmdbID *int64
	err := pool.QueryRow(ctx,
		"SELECT type, tmdb_id FROM items WHERE id = $1::uuid",
		itemID,
	).Scan(&itemType, &tmdbID)
	if err != nil {
		return err
	}
	if tmdbID == nil || *tmdbID <= 0 {
		// 没有 tmdb_id 就没地方补,静默完成
		return nil
	}

	// 先看有没有需要补的 —— 全表都填齐了就直接跳过,省一次 TMDB 调用。
	var missing int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM cast_members
		  WHERE item_id = $1::uuid
		    AND (image_url IS NULL OR image_url = '')`,
		itemID,
	).Scan(&missing); err != nil {
		return err
	}
	if missing == 0 {
		slog.Debug("[Backfill-ActorImg] all cast have image_url, skip",
			"item_id", itemID, "type", itemType)
		return nil
	}

	var details map[string]interface{}
	switch itemType {
	case "Movie":
		details, err = client.GetMovieDetails(ctx, *tmdbID)
	case "Series":
		details, err = client.GetTVDetails(ctx, *tmdbID)
	default:
		return fmt.Errorf("cannot backfill actors for type: %s", itemType)
	}
	if err != nil {
		return err
	}
	if details == nil {
		return fmt.Errorf("tmdb details empty for %s tmdb_id=%d", itemType, *tmdbID)
	}

	credits, _ := details["credits"].(map[string]interface{})
	castArr, _ := credits["cast"].([]interface{})
	if len(castArr) == 0 {
		return nil
	}

	// 建立两套索引:按 tmdb_id(int32)、按 name(小写)。name 冲突时保留第一个,后面用 name 匹配的风险小。
	type castEntry struct {
		imageURL string
	}
	byID := make(map[int32]castEntry, len(castArr))
	byName := make(map[string]castEntry, len(castArr))
	for _, c := range castArr {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		pp, _ := cm["profile_path"].(string)
		pp = strings.TrimSpace(pp)
		if pp == "" {
			continue
		}
		url := fmt.Sprintf("%s/w185%s", TMDB_IMAGE_BASE, pp)
		entry := castEntry{imageURL: url}

		if pid, ok := jsonInt64(cm, "id"); ok && pid > 0 {
			byID[int32(pid)] = entry
		}
		if n, ok := cm["name"].(string); ok {
			name := strings.ToLower(strings.TrimSpace(n))
			if name != "" {
				if _, exists := byName[name]; !exists {
					byName[name] = entry
				}
			}
		}
	}

	rows, err := pool.Query(ctx,
		`SELECT id::text, name, tmdb_id
		   FROM cast_members
		  WHERE item_id = $1::uuid
		    AND (image_url IS NULL OR image_url = '')`,
		itemID)
	if err != nil {
		return err
	}
	type castRow struct {
		id     string
		name   string
		tmdbID *int32
	}
	var targets []castRow
	for rows.Next() {
		var r castRow
		if err := rows.Scan(&r.id, &r.name, &r.tmdbID); err == nil {
			targets = append(targets, r)
		}
	}
	rows.Close()

	var updated, unmatched int
	for _, r := range targets {
		var url string
		if r.tmdbID != nil {
			if e, ok := byID[*r.tmdbID]; ok {
				url = e.imageURL
			}
		}
		if url == "" {
			if e, ok := byName[strings.ToLower(strings.TrimSpace(r.name))]; ok {
				url = e.imageURL
			}
		}
		if url == "" {
			unmatched++
			continue
		}
		if _, err := pool.Exec(ctx,
			`UPDATE cast_members SET image_url = $1
			  WHERE id = $2::uuid AND (image_url IS NULL OR image_url = '')`,
			url, r.id); err != nil {
			slog.Warn("[Backfill-ActorImg] update cast_member failed",
				"cast_id", r.id, "error", err)
			continue
		}
		updated++
	}
	slog.Info("[Backfill-ActorImg] done",
		"item_id", itemID, "type", itemType, "tmdb_id", *tmdbID,
		"targets", len(targets), "updated", updated, "unmatched", unmatched,
		"tmdb_cast_with_profile", len(byID))
	return nil
}
