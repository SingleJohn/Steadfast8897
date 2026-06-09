package models

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func GetItemGenres(ctx context.Context, pool *pgxpool.Pool, itemID string) ([][2]string, error) {
	rows, err := pool.Query(ctx,
		"SELECT g.id, g.name FROM genres g JOIN item_genres ig ON g.id = ig.genre_id WHERE ig.item_id = $1::uuid ORDER BY g.name",
		itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result [][2]string
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		result = append(result, [2]string{id.String(), name})
	}
	return result, rows.Err()
}

// GetItemTags 返回 item 的标签名(与 genres 分离,对齐 Emby Tags)。
func GetItemTags(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]string, error) {
	rows, err := pool.Query(ctx,
		"SELECT t.name FROM tags t JOIN item_tags it ON t.id = it.tag_id WHERE it.item_id = $1::uuid ORDER BY t.name",
		itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		result = append(result, name)
	}
	return result, rows.Err()
}

// GetItemExtraBackdrops 返回 item 的额外 Backdrop tag(extrafanart),按 idx 升序。
// 调用方把它们追加到 items.backdrop_image_path(Backdrop/0)之后,组成 BackdropImageTags 数组。
func GetItemExtraBackdrops(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]string, error) {
	rows, err := pool.Query(ctx,
		"SELECT tag FROM item_images WHERE item_id = $1::uuid AND image_type = 'Backdrop' ORDER BY idx",
		itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		result = append(result, tag)
	}
	return result, rows.Err()
}

func GetItemCast(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]map[string]interface{}, error) {
	rows, err := pool.Query(ctx,
		"SELECT * FROM cast_members WHERE item_id = $1::uuid ORDER BY role, order_index", itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fieldDescs := rows.FieldDescriptions()
	colNames := make([]string, len(fieldDescs))
	for i, fd := range fieldDescs {
		colNames[i] = string(fd.Name)
	}

	var result []map[string]interface{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		m := make(map[string]interface{})
		for i, name := range colNames {
			m[name] = vals[i]
		}

		name := getString(m, "name")
		character := getString(m, "character")
		role := getString(m, "role")
		idStr := getUUIDStr(m, "id")
		imageURL := getString(m, "image_url")

		val := map[string]interface{}{
			"Name": name,
			"Role": character,
			"Type": role,
			"Id":   idStr,
		}
		if imageURL != "" {
			val["PrimaryImageTag"] = idStr
			val["HasPrimaryImage"] = true
			val["ImageUrl"] = imageURL
		}
		if oi := getInt32Ptr(m, "order_index"); oi != nil {
			val["OrderIndex"] = *oi
		}
		result = append(result, val)
	}
	return result, rows.Err()
}

func GetAllGenresWithCounts(ctx context.Context, pool *pgxpool.Pool) ([]struct {
	ID    string
	Name  string
	Count int64
}, error) {
	rows, err := pool.Query(ctx,
		`SELECT g.id, g.name, COUNT(ig.item_id) as item_count
		 FROM genres g LEFT JOIN item_genres ig ON g.id = ig.genre_id
		 GROUP BY g.id, g.name ORDER BY g.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []struct {
		ID    string
		Name  string
		Count int64
	}
	for rows.Next() {
		var id uuid.UUID
		var name string
		var count int64
		if err := rows.Scan(&id, &name, &count); err != nil {
			return nil, err
		}
		result = append(result, struct {
			ID    string
			Name  string
			Count int64
		}{id.String(), name, count})
	}
	return result, rows.Err()
}
