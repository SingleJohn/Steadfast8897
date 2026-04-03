package models

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/dto"
)

type ItemQueryOptions struct {
	ParentID         *string
	IncludeItemTypes []string
	SortBy           *string
	SortOrder        *string
	Limit            *int64
	StartIndex       *int64
	Recursive        bool
	LibraryID        *string
	SearchTerm       *string
	Filters          []string
	UserID           *string
	GenreIDs         []string
	Years            []int
}

type QueryResult struct {
	Items      []pgx.Rows
	TotalCount int64
	Rows       []map[string]interface{}
}

type ItemQueryResult struct {
	Items      []dto.ItemRow
	UserData   []dto.UserDataRow
	TotalCount int64
}

const singleItemSelect = `SELECT id, name, type, sort_name, NULL::text AS collection_type, overview,
	production_year, premiere_date, community_rating, official_rating,
	runtime_ticks, index_number, parent_index_number, parent_id,
	series_id, series_name, season_id, container, file_path,
	resolved_path, provider_ids, primary_image_tag, backdrop_image_tag,
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
		var uid uuid.UUID
		err := pool.QueryRow(ctx, "SELECT id FROM items WHERE emby_id = $1", embyID).Scan(&uid)
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		s := uid.String()
		return &s, nil
	}
	return nil, nil
}

func GetEmbyID(ctx context.Context, pool *pgxpool.Pool, uuidStr string) *int32 {
	var eid int32
	err := pool.QueryRow(ctx, "SELECT emby_id FROM items WHERE id = $1::uuid", uuidStr).Scan(&eid)
	if err != nil {
		return nil
	}
	return &eid
}

func QueryItems(ctx context.Context, pool *pgxpool.Pool, options *ItemQueryOptions) (*ItemQueryResult, error) {
	var conditions []string
	var params []interface{}
	paramIdx := 1

	if options.ParentID != nil {
		if options.Recursive {
			conditions = append(conditions, fmt.Sprintf("i.library_id = $%d::uuid", paramIdx))
		} else {
			conditions = append(conditions, fmt.Sprintf("i.parent_id = $%d::uuid", paramIdx))
		}
		params = append(params, *options.ParentID)
		paramIdx++
	}

	if options.LibraryID != nil {
		conditions = append(conditions, fmt.Sprintf("i.library_id = $%d::uuid", paramIdx))
		params = append(params, *options.LibraryID)
		paramIdx++
	}

	if len(options.IncludeItemTypes) > 0 {
		placeholders := make([]string, len(options.IncludeItemTypes))
		for i, t := range options.IncludeItemTypes {
			placeholders[i] = fmt.Sprintf("$%d", paramIdx)
			params = append(params, t)
			paramIdx++
		}
		conditions = append(conditions, fmt.Sprintf("i.type IN (%s)", strings.Join(placeholders, ",")))
	}

	if options.SearchTerm != nil {
		conditions = append(conditions, fmt.Sprintf("i.name ILIKE $%d", paramIdx))
		params = append(params, "%"+*options.SearchTerm+"%")
		paramIdx++
	}

	genreJoin := ""
	if len(options.GenreIDs) > 0 {
		genreJoin = "JOIN item_genres ig_filter ON i.id = ig_filter.item_id"
		placeholders := make([]string, len(options.GenreIDs))
		for i, gid := range options.GenreIDs {
			placeholders[i] = fmt.Sprintf("$%d", paramIdx)
			params = append(params, gid)
			paramIdx++
		}
		conditions = append(conditions, fmt.Sprintf("ig_filter.genre_id IN (%s)", strings.Join(placeholders, ",")))
	}

	if len(options.Years) > 0 {
		placeholders := make([]string, len(options.Years))
		for i, y := range options.Years {
			placeholders[i] = fmt.Sprintf("$%d::int", paramIdx)
			params = append(params, y)
			paramIdx++
		}
		conditions = append(conditions, fmt.Sprintf("i.production_year IN (%s)", strings.Join(placeholders, ",")))
	}

	userJoin := ""
	if options.UserID != nil {
		userJoin = fmt.Sprintf(
			"LEFT JOIN user_item_data uid ON i.id = uid.item_id AND uid.user_id = $%d::uuid", paramIdx)
		params = append(params, *options.UserID)
		paramIdx++
	}

	for _, f := range options.Filters {
		if options.UserID == nil {
			continue
		}
		switch f {
		case "IsResumable":
			conditions = append(conditions, "uid.playback_position_ticks > 0")
		case "IsFavorite":
			conditions = append(conditions, "uid.is_favorite = TRUE")
		case "IsUnplayed":
			conditions = append(conditions, "(uid.played IS NULL OR uid.played = FALSE)")
		case "IsPlayed":
			conditions = append(conditions, "uid.played = TRUE")
		}
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	countSQL := fmt.Sprintf(
		"SELECT COUNT(DISTINCT i.id) FROM items i %s %s %s",
		genreJoin, userJoin, whereClause)

	var totalCount int64
	countParams := make([]interface{}, len(params))
	copy(countParams, params)
	err := pool.QueryRow(ctx, countSQL, countParams...).Scan(&totalCount)
	if err != nil {
		return nil, fmt.Errorf("count query: %w", err)
	}

	orderBy := buildOrderBy(options)

	userColumns := "NULL::bigint as playback_position_ticks, 0::int as play_count, FALSE as is_favorite, FALSE as played, NULL::timestamp as last_played_date"
	if options.UserID != nil {
		userColumns = "uid.playback_position_ticks, uid.play_count, uid.is_favorite, uid.played, uid.last_played_date"
	}

	seriesJoin := "LEFT JOIN items series_fallback ON series_fallback.id = COALESCE(i.series_id, CASE WHEN i.type = 'Season' THEN i.parent_id END)"
	seriesCols := "series_fallback.primary_image_tag as series_primary_image_tag, series_fallback.backdrop_image_tag as series_backdrop_image_tag, series_fallback.id as series_fallback_id"

	needDistinct := ""
	if genreJoin != "" {
		needDistinct = "DISTINCT"
	}
	isRandom := strings.Contains(orderBy, "RANDOM()")

	var itemSQL string
	if genreJoin != "" && isRandom {
		itemSQL = fmt.Sprintf(
			"SELECT * FROM (SELECT DISTINCT i.*, %s, %s FROM items i %s %s %s %s) sub",
			userColumns, seriesCols, genreJoin, userJoin, seriesJoin, whereClause)
	} else {
		actualOrder := orderBy
		if isRandom {
			actualOrder = "i.id"
		}
		itemSQL = fmt.Sprintf(
			"SELECT %s i.*, %s, %s FROM items i %s %s %s %s ORDER BY %s",
			needDistinct, userColumns, seriesCols, genreJoin, userJoin, seriesJoin, whereClause, actualOrder)
	}

	itemParams := make([]interface{}, len(params))
	copy(itemParams, params)

	if isRandom && totalCount > 0 {
		lim := int64(1)
		if options.Limit != nil {
			lim = *options.Limit
		}
		maxOffset := totalCount - lim
		if maxOffset < 0 {
			maxOffset = 0
		}
		randomOffset := int64(0)
		if maxOffset > 0 {
			randomOffset = int64(rand.IntN(int(maxOffset)))
		}
		itemSQL += fmt.Sprintf(" LIMIT $%d::bigint", paramIdx)
		itemParams = append(itemParams, lim)
		paramIdx++
		itemSQL += fmt.Sprintf(" OFFSET $%d::bigint", paramIdx)
		itemParams = append(itemParams, randomOffset)
		paramIdx++
	} else {
		if options.Limit != nil {
			itemSQL += fmt.Sprintf(" LIMIT $%d::bigint", paramIdx)
			itemParams = append(itemParams, *options.Limit)
			paramIdx++
		}
		if options.StartIndex != nil {
			itemSQL += fmt.Sprintf(" OFFSET $%d::bigint", paramIdx)
			itemParams = append(itemParams, *options.StartIndex)
			paramIdx++
		}
	}

	rows, err := pool.Query(ctx, itemSQL, itemParams...)
	if err != nil {
		return nil, fmt.Errorf("item query: %w", err)
	}

	items, userData, err := scanItemRows(rows)
	if err != nil {
		return nil, err
	}

	return &ItemQueryResult{
		Items:      items,
		UserData:   userData,
		TotalCount: totalCount,
	}, nil
}

func buildOrderBy(options *ItemQueryOptions) string {
	if options.SortBy != nil {
		sortDir := "ASC"
		if options.SortOrder != nil && *options.SortOrder == "Descending" {
			sortDir = "DESC"
		}
		fields := strings.Split(*options.SortBy, ",")
		var mapped []string
		for _, f := range fields {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			var col string
			switch f {
			case "SortName":
				col = "i.sort_name"
			case "DateCreated":
				col = "i.created_at"
			case "PremiereDate":
				col = "i.premiere_date"
			case "ProductionYear":
				col = "i.production_year"
			case "CommunityRating":
				col = "i.community_rating"
			case "Runtime":
				col = "i.runtime_ticks"
			case "Random":
				col = "RANDOM()"
			case "DatePlayed":
				col = "uid.last_played_date"
			case "IndexNumber":
				col = "i.parent_index_number NULLS LAST, i.index_number"
			case "ParentIndexNumber":
				col = "i.parent_index_number"
			default:
				continue
			}
			mapped = append(mapped, col+" "+sortDir)
		}
		if len(mapped) > 0 {
			return strings.Join(mapped, ", ")
		}
	}
	return "i.sort_name ASC"
}

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

		item := mapColsToItemRow(colMap)
		ud := mapColsToUserData(colMap)
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
	item := mapColsToItemRow(m)
	return &item
}

func mapColsToItemRow(m map[string]interface{}) dto.ItemRow {
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
	item.PrimaryImageTag = getStringPtr(m, "primary_image_tag")
	item.BackdropImageTag = getStringPtr(m, "backdrop_image_tag")
	item.SeriesPrimaryImageTag = getStringPtr(m, "series_primary_image_tag")
	item.SeriesBackdropImageTag = getStringPtr(m, "series_backdrop_image_tag")
	item.SeriesFallbackID = getUUIDStrPtr(m, "series_fallback_id")
	item.ChildCount = getInt64Ptr(m, "child_count")
	item.RecursiveItemCount = getInt64Ptr(m, "recursive_item_count")
	return item
}

func mapColsToUserData(m map[string]interface{}) dto.UserDataRow {
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

func GetChildCount(ctx context.Context, pool *pgxpool.Pool, parentID string) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM items WHERE parent_id = $1::uuid", parentID).Scan(&count)
	return count, err
}

func GetRecursiveItemCount(ctx context.Context, pool *pgxpool.Pool, parentID string) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx,
		`WITH RECURSIVE children AS (
			SELECT id FROM items WHERE parent_id = $1::uuid
			UNION ALL
			SELECT i.id FROM items i JOIN children c ON i.parent_id = c.id
		) SELECT COUNT(*) FROM children`, parentID).Scan(&count)
	return count, err
}

func GetLatestItems(ctx context.Context, pool *pgxpool.Pool, libraryID string, limit int64) ([]dto.ItemRow, error) {
	var libType string
	err := pool.QueryRow(ctx,
		"SELECT collection_type FROM libraries WHERE id = $1::uuid", libraryID).Scan(&libType)
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}

	itemType := "Movie"
	if libType == "tvshows" {
		itemType = "Series"
	}

	rows, err := pool.Query(ctx,
		"SELECT * FROM items WHERE library_id = $1::uuid AND type = $2 ORDER BY updated_at DESC LIMIT $3::bigint",
		libraryID, itemType, limit)
	if err != nil {
		return nil, err
	}

	items, _, err := scanItemRows(rows)
	return items, err
}

func GetMediaStreams(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]dto.StreamRow, error) {
	rows, err := pool.Query(ctx,
		"SELECT * FROM media_streams WHERE item_id = $1::uuid ORDER BY stream_index", itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fieldDescs := rows.FieldDescriptions()
	colNames := make([]string, len(fieldDescs))
	for i, fd := range fieldDescs {
		colNames[i] = string(fd.Name)
	}

	var streams []dto.StreamRow
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		m := make(map[string]interface{})
		for i, name := range colNames {
			m[name] = vals[i]
		}
		streams = append(streams, dto.StreamRow{
			Codec:        getStringPtr(m, "codec"),
			StreamType:   getString(m, "stream_type"),
			StreamIndex:  int32(getInt32OrZero(m, "stream_index")),
			Language:     getStringPtr(m, "language"),
			Title:        getStringPtr(m, "title"),
			IsDefault:    getBoolPtr(m, "is_default"),
			IsForced:     getBoolPtr(m, "is_forced"),
			Width:        getInt32Ptr(m, "width"),
			Height:       getInt32Ptr(m, "height"),
			BitRate:      getInt64Ptr(m, "bit_rate"),
			Channels:     getInt32Ptr(m, "channels"),
			SampleRate:   getInt32Ptr(m, "sample_rate"),
			BitDepth:     getInt32Ptr(m, "bit_depth"),
			PixelFormat:  getStringPtr(m, "pixel_format"),
			DisplayTitle: getStringPtr(m, "display_title"),
		})
	}
	return streams, rows.Err()
}

func getInt32OrZero(m map[string]interface{}, key string) int32 {
	if p := getInt32Ptr(m, key); p != nil {
		return *p
	}
	return 0
}

func GetUserItemData(ctx context.Context, pool *pgxpool.Pool, userID, itemID string) (*dto.UserDataRow, error) {
	row := pool.QueryRow(ctx,
		"SELECT playback_position_ticks, play_count, is_favorite, played, last_played_date FROM user_item_data WHERE user_id = $1::uuid AND item_id = $2::uuid",
		userID, itemID)

	var ud dto.UserDataRow
	err := row.Scan(&ud.PlaybackPositionTicks, &ud.PlayCount, &ud.IsFavorite, &ud.Played, &ud.LastPlayedDate)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ud, nil
}

func UpsertUserItemData(ctx context.Context, pool *pgxpool.Pool, userID, itemID string, position *int64, playCount *int32, isFavorite *bool, played *bool) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO user_item_data (user_id, item_id, playback_position_ticks, play_count, is_favorite, played, last_played_date)
		 VALUES ($1::uuid, $2::uuid, COALESCE($3, 0), COALESCE($4, 0), COALESCE($5, false), COALESCE($6, false), NOW())
		 ON CONFLICT (user_id, item_id) DO UPDATE SET
		   playback_position_ticks = COALESCE($3, user_item_data.playback_position_ticks),
		   play_count = COALESCE($4, user_item_data.play_count),
		   is_favorite = COALESCE($5, user_item_data.is_favorite),
		   played = COALESCE($6, user_item_data.played),
		   last_played_date = NOW()`,
		userID, itemID, position, playCount, isFavorite, played)
	return err
}

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
