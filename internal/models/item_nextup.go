package models

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func QueryNextUp(ctx context.Context, pool *pgxpool.Pool, userID string, seriesID *string, limit int64) (*ItemQueryResult, error) {
	if limit <= 0 {
		limit = 20
	}
	params := []interface{}{userID}
	seriesFilter := ""
	if seriesID != nil && *seriesID != "" {
		params = append(params, *seriesID)
		seriesFilter = fmt.Sprintf(" AND e.series_id = $%d::uuid", len(params))
	}
	params = append(params, limit)
	limitIdx := len(params)

	sql := fmt.Sprintf(`
WITH last_watched AS (
	SELECT DISTINCT ON (e.series_id)
		e.series_id AS lw_series_id,
		COALESCE(e.parent_index_number, 0) AS lw_season,
		COALESCE(e.index_number, 0) AS lw_episode,
		uid.last_played_date AS lw_last_played
	FROM items e
	JOIN user_item_data uid ON uid.item_id = e.id AND uid.user_id = $1::uuid
	WHERE e.type = 'Episode' AND e.series_id IS NOT NULL
		AND uid.played = TRUE
		AND COALESCE(e.parent_index_number, 0) > 0%s
	ORDER BY e.series_id,
		COALESCE(e.parent_index_number, 0) DESC,
		COALESCE(e.index_number, 0) DESC
)
SELECT * FROM (
	SELECT DISTINCT ON (i.series_id) i.*,
		uid.playback_position_ticks, uid.play_count, uid.is_favorite, uid.played, uid.last_played_date,
		series_fallback.primary_image_tag AS series_primary_image_tag,
		series_fallback.backdrop_image_tag AS series_backdrop_image_tag,
		series_fallback.id AS series_fallback_id,
		lw.lw_last_played AS nextup_last_played
	FROM last_watched lw
	JOIN items i ON i.series_id = lw.lw_series_id AND i.type = 'Episode'
	LEFT JOIN user_item_data uid ON uid.item_id = i.id AND uid.user_id = $1::uuid
	LEFT JOIN items series_fallback ON series_fallback.id = i.series_id
	WHERE COALESCE(i.parent_index_number, 0) > 0
		AND (uid.played IS NULL OR uid.played = FALSE)
		AND (COALESCE(i.parent_index_number, 0), COALESCE(i.index_number, 0)) > (lw.lw_season, lw.lw_episode)
	ORDER BY i.series_id,
		COALESCE(i.parent_index_number, 0) ASC,
		COALESCE(i.index_number, 0) ASC
) nextup
ORDER BY nextup_last_played DESC NULLS LAST
LIMIT $%d::bigint`, seriesFilter, limitIdx)

	rows, err := pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, fmt.Errorf("nextup query: %w", err)
	}
	items, userData, err := scanItemRows(rows)
	if err != nil {
		return nil, err
	}
	return &ItemQueryResult{
		Items:      items,
		UserData:   userData,
		TotalCount: int64(len(items)),
	}, nil
}
