package models

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
)

func GetItemGenres(ctx context.Context, pool *pgxpool.Pool, itemID string) ([][2]string, error) {
	return repository.NewItemHelperRepository(pool).ListItemGenres(ctx, itemID)
}

// GetItemTags 返回 item 的标签名(与 genres 分离,对齐 Emby Tags)。
func GetItemTags(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]string, error) {
	return repository.NewItemHelperRepository(pool).ListItemTags(ctx, itemID)
}

func GetAllTagsWithCounts(ctx context.Context, pool *pgxpool.Pool) ([]struct {
	ID    int
	Name  string
	Count int64
}, error) {
	rows, err := repository.NewItemHelperRepository(pool).ListAllTagsWithCounts(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]struct {
		ID    int
		Name  string
		Count int64
	}, 0, len(rows))
	for _, row := range rows {
		id, _ := strconv.Atoi(row.ID)
		result = append(result, struct {
			ID    int
			Name  string
			Count int64
		}{ID: id, Name: row.Name, Count: row.Count})
	}
	return result, nil
}

// GetItemExtraBackdrops 返回 item 的额外 Backdrop tag(extrafanart),按 idx 升序。
// 调用方把它们追加到 items.backdrop_image_path(Backdrop/0)之后,组成 BackdropImageTags 数组。
func GetItemExtraBackdrops(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]string, error) {
	return repository.NewItemHelperRepository(pool).ListItemExtraBackdropTags(ctx, itemID)
}

func GetItemCast(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]map[string]interface{}, error) {
	return repository.NewItemHelperRepository(pool).ListItemCast(ctx, itemID)
}

func GetAllGenresWithCounts(ctx context.Context, pool *pgxpool.Pool) ([]struct {
	ID    string
	Name  string
	Count int64
}, error) {
	rows, err := repository.NewItemHelperRepository(pool).ListAllGenresWithCounts(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]struct {
		ID    string
		Name  string
		Count int64
	}, 0, len(rows))
	for _, row := range rows {
		result = append(result, struct {
			ID    string
			Name  string
			Count int64
		}{ID: row.ID, Name: row.Name, Count: row.Count})
	}
	return result, nil
}
