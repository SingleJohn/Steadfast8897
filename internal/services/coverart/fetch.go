package coverart

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PosterCount 是封面生成需要的海报数量。九宫格风格改为 3 列 × 4 行后为 12;
// 风格内部可按需截取或循环。
const PosterCount = 12

// PickPosterPaths 从该媒体库随机抽最多 PosterCount 张已有海报的 item,
// 不足时循环填满;没有任何海报则返回 ErrNoPosters。
//
// 只查 libraries 表中 collection_type=movies/tvshows 的库;平台库走独立表,
// 不走本函数。
func PickPosterPaths(ctx context.Context, pool *pgxpool.Pool, libID uuid.UUID) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT primary_image_path
		  FROM items
		 WHERE library_id = $1
		   AND type IN ('Movie', 'Series')
		   AND primary_image_path IS NOT NULL
		   AND primary_image_path <> ''
		 ORDER BY RANDOM()
		 LIMIT $2
	`, libID, PosterCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var raw []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		raw = append(raw, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	n := len(raw)
	if n == 0 {
		return nil, ErrNoPosters
	}
	out := make([]string, PosterCount)
	for i := 0; i < PosterCount; i++ {
		out[i] = raw[i%n]
	}
	return out, nil
}
