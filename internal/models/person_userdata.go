package models

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
)

func GetUserPersonData(ctx context.Context, pool *pgxpool.Pool, userID, personID string) (*dto.UserDataRow, error) {
	row := pool.QueryRow(ctx,
		`SELECT is_favorite FROM user_person_data
		  WHERE user_id = $1::uuid AND person_id = $2::uuid`,
		userID, personID)

	var isFavorite bool
	if err := row.Scan(&isFavorite); err != nil {
		if err == pgx.ErrNoRows {
			return &dto.UserDataRow{}, nil
		}
		return nil, err
	}
	return PersonUserDataRow(isFavorite), nil
}

func GetUserPersonFavoriteMap(ctx context.Context, pool *pgxpool.Pool, userID string, personIDs []string) (map[string]bool, error) {
	out := make(map[string]bool, len(personIDs))
	if userID == "" || len(personIDs) == 0 {
		return out, nil
	}
	rows, err := pool.Query(ctx,
		`SELECT person_id::text, is_favorite
		   FROM user_person_data
		  WHERE user_id = $1::uuid
		    AND person_id = ANY($2::uuid[])`,
		userID, personIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var isFavorite bool
		if err := rows.Scan(&id, &isFavorite); err != nil {
			return nil, err
		}
		out[id] = isFavorite
	}
	return out, rows.Err()
}

func UpsertUserPersonFavorite(ctx context.Context, pool *pgxpool.Pool, userID, personID string, favorite bool) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO user_person_data (user_id, person_id, is_favorite)
		 VALUES ($1::uuid, $2::uuid, $3)
		 ON CONFLICT (user_id, person_id) DO UPDATE SET
		   is_favorite = EXCLUDED.is_favorite,
		   updated_at = NOW()`,
		userID, personID, favorite)
	return err
}

func PersonUserDataRow(isFavorite bool) *dto.UserDataRow {
	pos := int64(0)
	playCount := int32(0)
	played := false
	return &dto.UserDataRow{
		PlaybackPositionTicks: &pos,
		PlayCount:             &playCount,
		IsFavorite:            &isFavorite,
		Played:                &played,
	}
}
