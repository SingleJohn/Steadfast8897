package coverart

import (
	"context"
	"strings"

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
	materials, err := PickMaterials(ctx, pool, libID)
	if err != nil {
		return nil, err
	}
	return PosterPathsFromMaterials(materials), nil
}

// PickMaterials 从该媒体库抽取生成封面所需的素材。
// 有海报的条目才会作为卡片素材;BackdropPath 有则交给风格用作横版背景。
func PickMaterials(ctx context.Context, pool *pgxpool.Pool, libID uuid.UUID) ([]Material, error) {
	rows, err := pool.Query(ctx, `
		SELECT name, primary_image_path, COALESCE(backdrop_image_path, '')
		  FROM items
		 WHERE library_id = $1
		   AND type IN ('Movie', 'Series')
		   AND primary_image_path IS NOT NULL
		   AND primary_image_path <> ''
		 ORDER BY (backdrop_image_path IS NULL OR backdrop_image_path = ''), RANDOM()
		 LIMIT $2
	`, libID, PosterCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var raw []Material
	for rows.Next() {
		var m Material
		if err := rows.Scan(&m.Title, &m.PosterPath, &m.BackdropPath); err != nil {
			return nil, err
		}
		m.Title = strings.TrimSpace(m.Title)
		raw = append(raw, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	n := len(raw)
	if n == 0 {
		return nil, ErrNoPosters
	}
	out := make([]Material, PosterCount)
	for i := 0; i < PosterCount; i++ {
		out[i] = raw[i%n]
	}
	return out, nil
}

// PosterPathsFromMaterials 保持旧风格对 PosterPaths 的依赖。
func PosterPathsFromMaterials(materials []Material) []string {
	out := make([]string, 0, len(materials))
	for _, m := range materials {
		if strings.TrimSpace(m.PosterPath) == "" {
			continue
		}
		out = append(out, m.PosterPath)
	}
	return out
}
