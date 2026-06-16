package models

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
	"fyms/internal/repository"
)

func GetChildCount(ctx context.Context, pool *pgxpool.Pool, parentID string) (int64, error) {
	return repository.NewItemHelperRepository(pool).GetChildCount(ctx, parentID)
}

func GetLibraryDisplayItemCount(ctx context.Context, pool *pgxpool.Pool, libraryID string) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx,
		`SELECT `+libraryRepresentativeCountExpr("i")+`
		   FROM items i
		  WHERE i.library_id = $1::uuid
		    AND i.type IN ('Movie', 'Series')`,
		libraryID).Scan(&count)
	return count, err
}

func GetRecursiveItemCount(ctx context.Context, pool *pgxpool.Pool, parentID string) (int64, error) {
	return repository.NewItemHelperRepository(pool).GetRecursiveItemCount(ctx, parentID)
}

func GetUnplayedEpisodeCounts(ctx context.Context, pool *pgxpool.Pool, userID string, itemIDs []string, itemType string) (map[string]int64, error) {
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

	rows, err := pool.Query(ctx,
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

func GetLatestItems(ctx context.Context, pool *pgxpool.Pool, libraryID string, limit int64) ([]dto.ItemRow, error) {
	libType, err := repository.NewItemHelperRepository(pool).GetCollectionTypeByLibraryID(ctx, libraryID)
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}

	var query string
	switch libType {
	case "tvshows":
		query = latestSeriesItemsQuery
	case "mixed":
		query = latestMixedItemsQuery
	default:
		query = latestMovieItemsQuery
	}

	rows, err := pool.Query(ctx, query, libraryID, limit)
	if err != nil {
		return nil, err
	}

	items, _, err := scanItemRows(rows)
	return items, err
}

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

func GetMediaStreams(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]dto.StreamRow, error) {
	rows, err := pool.Query(ctx,
		"SELECT * FROM media_streams WHERE item_id = $1::uuid ORDER BY stream_index", itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fieldDescs := rows.FieldDescriptions()
	colNames := make([]string, len(fieldDescs))
	for i, fd := range fieldDescs {
		colNames[i] = string(fd.Name)
	}

	var streams []dto.StreamRow
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		m := make(map[string]interface{})
		for i, name := range colNames {
			m[name] = vals[i]
		}
		streams = append(streams, dto.StreamRow{
			Codec:        getStringPtr(m, "codec"),
			StreamType:   getString(m, "stream_type"),
			StreamIndex:  int32(getInt32OrZero(m, "stream_index")),
			Language:     getStringPtr(m, "language"),
			Title:        getStringPtr(m, "title"),
			IsDefault:    getBoolPtr(m, "is_default"),
			IsForced:     getBoolPtr(m, "is_forced"),
			Width:        getInt32Ptr(m, "width"),
			Height:       getInt32Ptr(m, "height"),
			BitRate:      getInt64Ptr(m, "bit_rate"),
			Channels:     getInt32Ptr(m, "channels"),
			SampleRate:   getInt32Ptr(m, "sample_rate"),
			BitDepth:     getInt32Ptr(m, "bit_depth"),
			PixelFormat:  getStringPtr(m, "pixel_format"),
			DisplayTitle: getStringPtr(m, "display_title"),
		})
	}
	return streams, rows.Err()
}

func getInt32OrZero(m map[string]interface{}, key string) int32 {
	if p := getInt32Ptr(m, key); p != nil {
		return *p
	}
	return 0
}
