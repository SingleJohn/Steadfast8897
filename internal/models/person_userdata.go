package models

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
	"fyms/internal/repository"
)

func GetUserPersonData(ctx context.Context, pool *pgxpool.Pool, userID, personID string) (*dto.UserDataRow, error) {
	return repository.NewItemHelperRepository(pool).GetUserPersonData(ctx, userID, personID)
}

func GetUserPersonFavoriteMap(ctx context.Context, pool *pgxpool.Pool, userID string, personIDs []string) (map[string]bool, error) {
	return repository.NewItemHelperRepository(pool).GetUserPersonFavoriteMap(ctx, userID, personIDs)
}

func UpsertUserPersonFavorite(ctx context.Context, pool *pgxpool.Pool, userID, personID string, favorite bool) error {
	return repository.NewItemHelperRepository(pool).UpsertUserPersonFavorite(ctx, userID, personID, favorite)
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
