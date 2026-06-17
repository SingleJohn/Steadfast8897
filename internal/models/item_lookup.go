package models

import (
	"context"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
	"fyms/internal/repository"
)

func GetItemByID(ctx context.Context, pool *pgxpool.Pool, id string) (*dto.ItemRow, error) {
	row, err := repository.NewItemReadRepository(pool).GetItemByID(ctx, id)
	if err != nil || row == nil {
		return nil, err
	}
	item := MapColsToItemRow(row)
	return &item, nil
}

func GetItemByAnyID(ctx context.Context, pool *pgxpool.Pool, id string) (*dto.ItemRow, error) {
	if _, err := uuid.Parse(id); err == nil {
		return GetItemByID(ctx, pool, id)
	}
	if embyID, err := strconv.Atoi(id); err == nil {
		row, err := repository.NewItemReadRepository(pool).GetItemByEmbyID(ctx, embyID)
		if err != nil || row == nil {
			return nil, err
		}
		item := MapColsToItemRow(row)
		return &item, nil
	}
	return nil, nil
}

func ResolveToUUID(ctx context.Context, pool *pgxpool.Pool, id string) (*string, error) {
	if _, err := uuid.Parse(id); err == nil {
		return &id, nil
	}
	if embyID, err := strconv.Atoi(id); err == nil {
		return repository.NewItemHelperRepository(pool).ResolveItemUUIDByEmbyID(ctx, int32(embyID))
	}
	return nil, nil
}

func GetEmbyID(ctx context.Context, pool *pgxpool.Pool, uuidStr string) *int32 {
	eid, err := repository.NewItemHelperRepository(pool).GetItemEmbyID(ctx, uuidStr)
	if err != nil {
		return nil
	}
	return eid
}
