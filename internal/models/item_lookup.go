package models

import (
	"context"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
	"fyms/internal/repository"
)

const singleItemSelect = `SELECT id, name, type, sort_name, NULL::text AS collection_type, overview,
	production_year, premiere_date, community_rating, official_rating,
	runtime_ticks, index_number, parent_index_number, parent_id,
	series_id, series_name, season_id, container, file_path,
	resolved_path, provider_ids, primary_image_path, primary_image_tag, backdrop_image_tag,
	NULL::bigint AS child_count, NULL::bigint AS recursive_item_count
	FROM items`

func GetItemByID(ctx context.Context, pool *pgxpool.Pool, id string) (*dto.ItemRow, error) {
	row := pool.QueryRow(ctx, singleItemSelect+" WHERE id = $1::uuid", id)
	return scanItemRow(row)
}

func GetItemByAnyID(ctx context.Context, pool *pgxpool.Pool, id string) (*dto.ItemRow, error) {
	if _, err := uuid.Parse(id); err == nil {
		return GetItemByID(ctx, pool, id)
	}
	if embyID, err := strconv.Atoi(id); err == nil {
		row := pool.QueryRow(ctx, singleItemSelect+" WHERE emby_id = $1", embyID)
		return scanItemRow(row)
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
