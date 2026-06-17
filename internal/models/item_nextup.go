package models

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
	"fyms/internal/repository"
)

func QueryNextUp(ctx context.Context, pool *pgxpool.Pool, userID string, seriesID *string, limit int64) (*ItemQueryResult, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := repository.NewItemReadRepository(pool).NextUp(ctx, userID, seriesID, limit)
	if err != nil {
		return nil, err
	}
	items := make([]dto.ItemRow, 0, len(rows))
	userData := make([]dto.UserDataRow, 0, len(rows))
	for _, row := range rows {
		items = append(items, MapColsToItemRow(row))
		userData = append(userData, MapColsToUserDataRow(row))
	}
	return &ItemQueryResult{
		Items:      items,
		UserData:   userData,
		TotalCount: int64(len(items)),
	}, nil
}
