package models

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
)

func GetChildCount(ctx context.Context, pool *pgxpool.Pool, parentID string) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM items WHERE parent_id = $1::uuid", parentID).Scan(&count)
	return count, err
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
	var count int64
	err := pool.QueryRow(ctx,
		`WITH RECURSIVE children AS (
			SELECT id FROM items WHERE parent_id = $1::uuid
			UNION ALL
			SELECT i.id FROM items i JOIN children c ON i.parent_id = c.id
		) SELECT COUNT(*) FROM children`, parentID).Scan(&count)
	return count, err
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
	var libType string
	err := pool.QueryRow(ctx,
		"SELECT collection_type FROM libraries WHERE id = $1::uuid AND deleted_at IS NULL", libraryID).Scan(&libType)
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}

	itemTypes := []string{"Movie"}
	if libType == "tvshows" {
		itemTypes = []string{"Series"}
	} else if libType == "mixed" {
		itemTypes = []string{"Movie", "Series"}
	}

	rows, err := pool.Query(ctx,
		`WITH filtered AS (
			SELECT *, CASE WHEN type = 'Movie' THEN COALESCE(merged_to_id::text, id::text) ELSE id::text END AS merge_group_key
			FROM items
			WHERE library_id = $1::uuid AND type = ANY($2::text[])
		), ranked AS (
			SELECT filtered.*,
				ROW_NUMBER() OVER (
					PARTITION BY merge_group_key
					ORDER BY
						CASE WHEN filtered.merged_to_id IS NULL THEN 0 ELSE 1 END,
						CASE WHEN filtered.primary_image_tag IS NOT NULL THEN 0 ELSE 1 END,
						CASE WHEN filtered.primary_image_path IS NOT NULL AND filtered.primary_image_path <> '' THEN 0 ELSE 1 END,
						CASE WHEN filtered.overview IS NOT NULL AND filtered.overview <> '' THEN 0 ELSE 1 END,
						filtered.created_at DESC,
						filtered.id
				) AS merge_row_num
			FROM filtered
		)
		SELECT * FROM ranked
		WHERE merge_row_num = 1
		ORDER BY created_at DESC
		LIMIT $3::bigint`,
		libraryID, itemTypes, limit)
	if err != nil {
		return nil, err
	}

	items, _, err := scanItemRows(rows)
	return items, err
}

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
