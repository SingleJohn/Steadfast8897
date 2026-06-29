package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MediaVersionUserDataRepository struct {
	pool *pgxpool.Pool
}

type MediaVersionUserData struct {
	UserID                string
	ItemID                string
	MediaVersionID        string
	PlaybackPositionTicks int64
	PlayCount             int32
	Played                bool
	LastPlayedDate        *time.Time
}

type MediaVersionUserDataUpsert struct {
	UserID         string
	ItemID         string
	MediaVersionID string
	PositionTicks  *int64
	PlayCount      *int32
	Played         *bool
}

func NewMediaVersionUserDataRepository(pool *pgxpool.Pool) *MediaVersionUserDataRepository {
	return &MediaVersionUserDataRepository{pool: pool}
}

func (r *MediaVersionUserDataRepository) Upsert(ctx context.Context, in MediaVersionUserDataUpsert) error {
	userID, mediaVersionID, err := parseTwoUUIDs(in.UserID, in.MediaVersionID)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO user_media_version_data (
			user_id, item_id, media_version_id, playback_position_ticks, play_count, played, last_played_date, updated_at
		)
		SELECT $1, mv.item_id, mv.id, COALESCE($3, 0), COALESCE($4, 0), COALESCE($5, FALSE), NOW(), NOW()
		  FROM media_versions mv
		 WHERE mv.id = $2
		ON CONFLICT (user_id, media_version_id) DO UPDATE SET
			item_id = EXCLUDED.item_id,
			playback_position_ticks = COALESCE($3, user_media_version_data.playback_position_ticks),
			play_count = COALESCE($4, user_media_version_data.play_count),
			played = COALESCE($5, user_media_version_data.played),
			last_played_date = NOW(),
			updated_at = NOW()`,
		toPGUUID(userID), toPGUUID(mediaVersionID),
		optionalInt64(in.PositionTicks), optionalInt32(in.PlayCount), optionalBool(in.Played))
	return err
}

func (r *MediaVersionUserDataRepository) GetForVersion(ctx context.Context, userID, mediaVersionID string) (*MediaVersionUserData, error) {
	uid, mvid, err := parseTwoUUIDs(userID, mediaVersionID)
	if err != nil {
		return nil, err
	}
	row := r.pool.QueryRow(ctx,
		`SELECT user_id::text, item_id::text, media_version_id::text,
			playback_position_ticks, play_count, played, last_played_date
		   FROM user_media_version_data
		  WHERE user_id = $1 AND media_version_id = $2`,
		toPGUUID(uid), toPGUUID(mvid))
	data, err := scanMediaVersionUserData(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return data, err
}

func (r *MediaVersionUserDataRepository) ListForItem(ctx context.Context, userID, itemID string) (map[string]MediaVersionUserData, error) {
	uid, iid, err := parseTwoUUIDs(userID, itemID)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT user_id::text, item_id::text, media_version_id::text,
			playback_position_ticks, play_count, played, last_played_date
		   FROM user_media_version_data
		  WHERE user_id = $1 AND item_id = $2`,
		toPGUUID(uid), toPGUUID(iid))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]MediaVersionUserData)
	for rows.Next() {
		data, err := scanMediaVersionUserData(rows)
		if err != nil {
			return nil, err
		}
		out[data.MediaVersionID] = *data
	}
	return out, rows.Err()
}

func (r *MediaVersionUserDataRepository) GetLatestForItem(ctx context.Context, userID, itemID string) (*MediaVersionUserData, error) {
	uid, iid, err := parseTwoUUIDs(userID, itemID)
	if err != nil {
		return nil, err
	}
	row := r.pool.QueryRow(ctx,
		`SELECT user_id::text, item_id::text, media_version_id::text,
			playback_position_ticks, play_count, played, last_played_date
		   FROM user_media_version_data
		  WHERE user_id = $1 AND item_id = $2
		  ORDER BY last_played_date DESC NULLS LAST, updated_at DESC
		  LIMIT 1`,
		toPGUUID(uid), toPGUUID(iid))
	data, err := scanMediaVersionUserData(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return data, err
}

func (r *MediaVersionUserDataRepository) MarkItemVersions(ctx context.Context, userID, itemID string, position int64, played bool) error {
	uid, iid, err := parseTwoUUIDs(userID, itemID)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO user_media_version_data (
			user_id, item_id, media_version_id, playback_position_ticks, play_count, played, last_played_date, updated_at
		)
		SELECT $1, mv.item_id, mv.id, $3, 0, $4, NOW(), NOW()
		  FROM media_versions mv
		 WHERE mv.item_id = $2
		ON CONFLICT (user_id, media_version_id) DO UPDATE SET
			playback_position_ticks = EXCLUDED.playback_position_ticks,
			played = EXCLUDED.played,
			last_played_date = NOW(),
			updated_at = NOW()`,
		toPGUUID(uid), toPGUUID(iid), position, played)
	return err
}

func scanMediaVersionUserData(row pgx.Row) (*MediaVersionUserData, error) {
	var data MediaVersionUserData
	var lastPlayed *time.Time
	err := row.Scan(
		&data.UserID,
		&data.ItemID,
		&data.MediaVersionID,
		&data.PlaybackPositionTicks,
		&data.PlayCount,
		&data.Played,
		&lastPlayed,
	)
	if err != nil {
		return nil, err
	}
	data.LastPlayedDate = lastPlayed
	return &data, nil
}

func parseThreeUUIDs(a, b, c string) (uuid.UUID, uuid.UUID, uuid.UUID, error) {
	ua, err := uuid.Parse(a)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	ub, err := uuid.Parse(b)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	uc, err := uuid.Parse(c)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	return ua, ub, uc, nil
}
