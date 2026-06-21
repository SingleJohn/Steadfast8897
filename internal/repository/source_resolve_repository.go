package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func (r *SourceRepository) ResolveSourceItemPublicUUID(ctx context.Context, publicUUID string) (int64, bool, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM source_items WHERE public_uuid = $1::uuid`,
		publicUUID).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func (r *SourceRepository) ResolveSourceLibraryViewPublicUUID(ctx context.Context, publicUUID string) (int64, bool, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM source_library_views WHERE public_uuid = $1::uuid`,
		publicUUID).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func (r *SourceRepository) ResolveEpisodePublicUUID(ctx context.Context, publicUUID string, makeEpisodeUUID func(string, string) string) (int64, string, bool, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT si.id, si.public_uuid::text,
		       COALESCE(NULLIF(sps.episode_key, ''), sps.episode_title, sps.id::text) AS episode_key
		  FROM source_items si
		  JOIN source_play_sources sps ON sps.source_item_id = si.id
		 WHERE si.item_type = 'Series'
		 GROUP BY si.id, si.public_uuid,
		          COALESCE(NULLIF(sps.episode_key, ''), sps.episode_title, sps.id::text)`)
	if err != nil {
		return 0, "", false, err
	}
	defer rows.Close()
	for rows.Next() {
		var sourceItemID int64
		var sourceItemUUID, episodeKey string
		if err := rows.Scan(&sourceItemID, &sourceItemUUID, &episodeKey); err != nil {
			return 0, "", false, err
		}
		if makeEpisodeUUID(sourceItemUUID, episodeKey) == publicUUID {
			return sourceItemID, episodeKey, true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return 0, "", false, err
	}
	return 0, "", false, nil
}
