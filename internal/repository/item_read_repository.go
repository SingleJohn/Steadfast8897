package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ItemReadRepository struct {
	pool *pgxpool.Pool
}

func NewItemReadRepository(pool *pgxpool.Pool) *ItemReadRepository {
	return &ItemReadRepository{pool: pool}
}

func (r *ItemReadRepository) GetItemByID(ctx context.Context, id string) (map[string]any, error) {
	rows, err := r.pool.Query(ctx, singleItemSelect+" WHERE id = $1::uuid", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return singleMapRow(rows)
}

func (r *ItemReadRepository) GetItemByEmbyID(ctx context.Context, embyID int) (map[string]any, error) {
	rows, err := r.pool.Query(ctx, singleItemSelect+" WHERE emby_id = $1", embyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return singleMapRow(rows)
}

func (r *ItemReadRepository) CountLibraryDisplayItems(ctx context.Context, libraryID string) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx,
		`SELECT `+libraryRepresentativeCountExpr("i")+`
		   FROM items i
		  WHERE i.library_id = $1::uuid
		    AND i.type IN ('Movie', 'Series')`,
		libraryID).Scan(&count)
	return count, err
}

func (r *ItemReadRepository) UnplayedEpisodeCounts(ctx context.Context, userID string, itemIDs []string, itemType string) (map[string]int64, error) {
	counts := make(map[string]int64, len(itemIDs))
	if userID == "" || len(itemIDs) == 0 {
		return counts, nil
	}

	var keyExpr string
	switch itemType {
	case "Series":
		keyExpr = "e.series_id::text"
	case "Season":
		keyExpr = "e.parent_id::text"
	default:
		return counts, nil
	}

	rows, err := r.pool.Query(ctx,
		`SELECT `+keyExpr+`, COUNT(*)
		   FROM items e
		   LEFT JOIN user_item_data uid ON uid.item_id = e.id AND uid.user_id = $1::uuid
		  WHERE e.type = 'Episode'
		    AND `+keyExpr+` = ANY($2::text[])
		    AND (uid.played IS NULL OR uid.played = FALSE)
		  GROUP BY `+keyExpr,
		userID, itemIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var count int64
		if err := rows.Scan(&id, &count); err != nil {
			return nil, err
		}
		counts[id] = count
	}
	return counts, rows.Err()
}

func (r *ItemReadRepository) LatestItems(ctx context.Context, libraryID, libraryType string, limit int64) ([]map[string]any, error) {
	var query string
	switch libraryType {
	case "tvshows":
		query = latestSeriesItemsQuery
	case "mixed":
		query = latestMixedItemsQuery
	default:
		query = latestMovieItemsQuery
	}

	rows, err := r.pool.Query(ctx, query, libraryID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return mapRows(rows)
}

func (r *ItemReadRepository) MediaStreams(ctx context.Context, itemID string) ([]map[string]any, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT * FROM media_streams WHERE item_id = $1::uuid ORDER BY stream_index", itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return mapRows(rows)
}

func (r *ItemReadRepository) NextUp(ctx context.Context, userID string, seriesID *string, limit int64) ([]map[string]any, error) {
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

	rows, err := r.pool.Query(ctx, sql, params...)
	if err != nil {
		return nil, fmt.Errorf("nextup query: %w", err)
	}
	defer rows.Close()
	return mapRows(rows)
}

func singleMapRow(rows pgx.Rows) (map[string]any, error) {
	mapped, err := mapRows(rows)
	if err != nil {
		return nil, err
	}
	if len(mapped) == 0 {
		return nil, nil
	}
	return mapped[0], nil
}

func mapRows(rows pgx.Rows) ([]map[string]any, error) {
	fieldDescs := rows.FieldDescriptions()
	colNames := make([]string, len(fieldDescs))
	for i, fd := range fieldDescs {
		colNames[i] = string(fd.Name)
	}

	var out []map[string]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make(map[string]any, len(colNames))
		for i, name := range colNames {
			row[name] = vals[i]
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func libraryRepresentativeCountExpr(itemAlias string) string {
	return fmt.Sprintf(
		"COUNT(DISTINCT CASE WHEN %s.type = 'Movie' THEN COALESCE(%s.merged_to_id::text, %s.id::text) ELSE %s.id::text END)",
		itemAlias, itemAlias, itemAlias, itemAlias,
	)
}

const singleItemSelect = `SELECT id, name, type, sort_name, NULL::text AS collection_type, overview,
	production_year, premiere_date, community_rating, official_rating,
	runtime_ticks, index_number, parent_index_number, parent_id,
	series_id, series_name, season_id, container, file_path,
	resolved_path, provider_ids, primary_image_path, primary_image_tag, backdrop_image_tag,
	NULL::bigint AS child_count, NULL::bigint AS recursive_item_count
	FROM items`

const latestMovieRepresentativeWhere = `
	i.library_id = $1::uuid
	AND i.type = 'Movie'
	AND NOT EXISTS (
		SELECT 1
		FROM items better
		WHERE better.library_id = i.library_id
			AND better.type = 'Movie'
			AND COALESCE(better.merged_to_id, better.id) = COALESCE(i.merged_to_id, i.id)
			AND (
				CASE WHEN better.merged_to_id IS NULL THEN 0 ELSE 1 END,
				CASE WHEN better.primary_image_tag IS NOT NULL THEN 0 ELSE 1 END,
				CASE WHEN better.primary_image_path IS NOT NULL AND better.primary_image_path <> '' THEN 0 ELSE 1 END,
				CASE WHEN better.overview IS NOT NULL AND better.overview <> '' THEN 0 ELSE 1 END,
				TIMESTAMP '9999-12-31' - better.created_at,
				better.id
			) < (
				CASE WHEN i.merged_to_id IS NULL THEN 0 ELSE 1 END,
				CASE WHEN i.primary_image_tag IS NOT NULL THEN 0 ELSE 1 END,
				CASE WHEN i.primary_image_path IS NOT NULL AND i.primary_image_path <> '' THEN 0 ELSE 1 END,
				CASE WHEN i.overview IS NOT NULL AND i.overview <> '' THEN 0 ELSE 1 END,
				TIMESTAMP '9999-12-31' - i.created_at,
				i.id
			)
	)`

const latestMovieItemsQuery = `
	SELECT i.*
	FROM items i
	WHERE ` + latestMovieRepresentativeWhere + `
	ORDER BY i.created_at DESC
	LIMIT $2::bigint`

const latestSeriesItemsQuery = `
	SELECT i.*
	FROM items i
	WHERE i.library_id = $1::uuid
		AND i.type = 'Series'
	ORDER BY i.created_at DESC
	LIMIT $2::bigint`

const latestMixedItemsQuery = `
	WITH movie_latest AS (
		SELECT i.*
		FROM items i
		WHERE ` + latestMovieRepresentativeWhere + `
		ORDER BY i.created_at DESC
		LIMIT $2::bigint
	), series_latest AS (
		SELECT i.*
		FROM items i
		WHERE i.library_id = $1::uuid
			AND i.type = 'Series'
		ORDER BY i.created_at DESC
		LIMIT $2::bigint
	), latest AS (
		SELECT * FROM movie_latest
		UNION ALL
		SELECT * FROM series_latest
	)
	SELECT *
	FROM latest
	ORDER BY created_at DESC
	LIMIT $2::bigint`
