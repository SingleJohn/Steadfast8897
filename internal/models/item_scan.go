package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"fyms/internal/dto"
)

func scanItemRow(row pgx.Row) (*dto.ItemRow, error) {
	cols := itemColumns()
	vals := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := row.Scan(ptrs...); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return mapToItemRow(cols, vals), nil
}

func scanItemRows(rows pgx.Rows) ([]dto.ItemRow, []dto.UserDataRow, error) {
	defer rows.Close()
	fieldDescs := rows.FieldDescriptions()
	colNames := make([]string, len(fieldDescs))
	for i, fd := range fieldDescs {
		colNames[i] = string(fd.Name)
	}

	var items []dto.ItemRow
	var userData []dto.UserDataRow

	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, nil, err
		}

		colMap := make(map[string]interface{})
		for i, name := range colNames {
			colMap[name] = vals[i]
		}

		item := MapColsToItemRow(colMap)
		ud := MapColsToUserDataRow(colMap)
		items = append(items, item)
		userData = append(userData, ud)
	}
	return items, userData, rows.Err()
}

func itemColumns() []string {
	return []string{
		"id", "name", "type", "sort_name", "collection_type", "overview",
		"production_year", "premiere_date", "community_rating", "official_rating",
		"runtime_ticks", "index_number", "parent_index_number", "parent_id",
		"series_id", "series_name", "season_id", "container", "file_path",
		"resolved_path", "provider_ids", "primary_image_tag", "backdrop_image_tag",
		"child_count", "recursive_item_count",
	}
}

func mapToItemRow(cols []string, vals []interface{}) *dto.ItemRow {
	m := make(map[string]interface{})
	for i, c := range cols {
		m[c] = vals[i]
	}
	item := MapColsToItemRow(m)
	return &item
}

func MapColsToItemRow(m map[string]interface{}) dto.ItemRow {
	item := dto.ItemRow{}
	item.ID = getUUIDStr(m, "id")
	item.Name = getString(m, "name")
	item.ItemType = getString(m, "type")
	item.SortName = getStringPtr(m, "sort_name")
	item.CollectionType = getStringPtr(m, "collection_type")
	item.Overview = getStringPtr(m, "overview")
	item.ProductionYear = getInt32Ptr(m, "production_year")
	item.PremiereDate = getTimePtr(m, "premiere_date")
	if v, ok := m["community_rating"]; ok && v != nil {
		switch f := v.(type) {
		case float32:
			val := float64(f)
			item.CommunityRating = &val
		case float64:
			item.CommunityRating = &f
		}
	}
	item.OfficialRating = getStringPtr(m, "official_rating")
	item.RuntimeTicks = getInt64Ptr(m, "runtime_ticks")
	item.IndexNumber = getInt32Ptr(m, "index_number")
	item.ParentIndexNumber = getInt32Ptr(m, "parent_index_number")
	item.ParentID = getUUIDStrPtr(m, "parent_id")
	item.SeriesID = getUUIDStrPtr(m, "series_id")
	item.SeriesName = getStringPtr(m, "series_name")
	item.SeasonID = getUUIDStrPtr(m, "season_id")
	item.Container = getStringPtr(m, "container")
	item.FilePath = getStringPtr(m, "file_path")
	item.ResolvedPath = getStringPtr(m, "resolved_path")
	if v, ok := m["provider_ids"]; ok && v != nil {
		switch pv := v.(type) {
		case map[string]interface{}:
			b, _ := json.Marshal(pv)
			item.ProviderIDs = (*json.RawMessage)(&b)
		case json.RawMessage:
			item.ProviderIDs = &pv
		case []byte:
			rm := json.RawMessage(pv)
			item.ProviderIDs = &rm
		}
	}
	item.PrimaryImagePath = getStringPtr(m, "primary_image_path")
	item.PrimaryImageTag = getStringPtr(m, "primary_image_tag")
	item.BackdropImageTag = getStringPtr(m, "backdrop_image_tag")
	item.SeriesPrimaryImageTag = getStringPtr(m, "series_primary_image_tag")
	item.SeriesBackdropImageTag = getStringPtr(m, "series_backdrop_image_tag")
	item.SeriesFallbackID = getUUIDStrPtr(m, "series_fallback_id")
	item.ChildCount = getInt64Ptr(m, "child_count")
	item.RecursiveItemCount = getInt64Ptr(m, "recursive_item_count")
	item.Tagline = getStringPtr(m, "tagline")
	item.Studio = getStringPtr(m, "studio")
	item.CreatedAt = getTimePtr(m, "created_at")
	return item
}

func MapColsToUserDataRow(m map[string]interface{}) dto.UserDataRow {
	return dto.UserDataRow{
		PlaybackPositionTicks: getInt64Ptr(m, "playback_position_ticks"),
		PlayCount:             getInt32Ptr(m, "play_count"),
		IsFavorite:            getBoolPtr(m, "is_favorite"),
		Played:                getBoolPtr(m, "played"),
		LastPlayedDate:        getTimePtr(m, "last_played_date"),
	}
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getStringPtr(m map[string]interface{}, key string) *string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return &s
		}
	}
	return nil
}

func getInt32Ptr(m map[string]interface{}, key string) *int32 {
	if v, ok := m[key]; ok && v != nil {
		switch n := v.(type) {
		case int32:
			return &n
		case int64:
			i := int32(n)
			return &i
		case int:
			i := int32(n)
			return &i
		}
	}
	return nil
}

func getInt64Ptr(m map[string]interface{}, key string) *int64 {
	if v, ok := m[key]; ok && v != nil {
		switch n := v.(type) {
		case int64:
			return &n
		case int32:
			i := int64(n)
			return &i
		case int:
			i := int64(n)
			return &i
		}
	}
	return nil
}

func getUUIDStr(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch id := v.(type) {
	case [16]byte:
		return uuid.UUID(id).String()
	case uuid.UUID:
		return id.String()
	case string:
		return id
	case []byte:
		if len(id) == 16 {
			return uuid.UUID([16]byte(id)).String()
		}
		return string(id)
	}
	return fmt.Sprintf("%v", v)
}

func getUUIDStrPtr(m map[string]interface{}, key string) *string {
	s := getUUIDStr(m, key)
	if s == "" {
		return nil
	}
	return &s
}

func getBoolPtr(m map[string]interface{}, key string) *bool {
	if v, ok := m[key]; ok && v != nil {
		if b, ok := v.(bool); ok {
			return &b
		}
	}
	return nil
}

func getTimePtr(m map[string]interface{}, key string) *time.Time {
	if v, ok := m[key]; ok && v != nil {
		if t, ok := v.(time.Time); ok {
			return &t
		}
	}
	return nil
}
