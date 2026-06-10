package models

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
)

func GetExternalSubtitlesForMediaVersion(ctx context.Context, pool *pgxpool.Pool, mediaVersionID string) ([]dto.ExternalSubtitleRow, error) {
	rows, err := pool.Query(ctx,
		`SELECT id::text, item_id::text, media_version_id::text, file_path, codec, language, title, is_default, is_forced
		   FROM external_subtitles
		  WHERE media_version_id = $1::uuid
		  ORDER BY language NULLS LAST, title NULLS LAST, file_path`,
		mediaVersionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []dto.ExternalSubtitleRow
	for rows.Next() {
		var r dto.ExternalSubtitleRow
		if err := rows.Scan(&r.ID, &r.ItemID, &r.MediaVersionID, &r.FilePath, &r.Codec, &r.Language, &r.Title, &r.IsDefault, &r.IsForced); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
