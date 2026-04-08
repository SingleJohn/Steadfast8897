package models

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	Studio           *string
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

func mergedRepresentativeExpr(itemAlias string) string {
	return fmt.Sprintf(
		"CASE WHEN %s.type = 'Movie' THEN COALESCE(%s.merged_to_id::text, %s.id::text) ELSE %s.id::text END",
		itemAlias, itemAlias, itemAlias, itemAlias,
	)
}

func libraryRepresentativeCountExpr(itemAlias string) string {
	return fmt.Sprintf(
		"COUNT(DISTINCT CASE WHEN %s.type = 'Movie' THEN COALESCE(%s.merged_to_id::text, %s.id::text) ELSE %s.id::text END)",
		itemAlias, itemAlias, itemAlias, itemAlias,
	)
}

func shouldUseLibraryRepresentative(options *ItemQueryOptions) bool {
	if options == nil || options.Studio != nil {
		return false
	}
	return options.ParentID != nil || options.LibraryID != nil
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

	if options.Studio != nil {
		conditions = append(conditions, fmt.Sprintf("i.studio = $%d", paramIdx))
		params = append(params, *options.Studio)
		paramIdx++
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

	// Platform virtual libraries show only the global merged primary.
	// Ordinary user libraries use a per-library representative selection later
	// so a title does not disappear just because the global primary lives elsewhere.
	if options.Studio != nil {
		conditions = append(conditions, "i.merged_to_id IS NULL")
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	useRepresentative := shouldUseLibraryRepresentative(options)
	countTarget := "DISTINCT i.id"
	if useRepresentative {
		countTarget = "DISTINCT " + mergedRepresentativeExpr("i")
	}
	countSQL := fmt.Sprintf(
		"SELECT COUNT(%s) FROM items i %s %s %s",
		countTarget, genreJoin, userJoin, whereClause)

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
	if useRepresentative {
		outerOrder := buildOrderByForAlias(options, "ranked", "ranked")
		itemSQL = fmt.Sprintf(
			`WITH filtered AS (
				SELECT %s i.*, %s, %s, %s AS merge_group_key
				FROM items i %s %s %s %s
			), ranked AS (
				SELECT filtered.*,
					ROW_NUMBER() OVER (
						PARTITION BY merge_group_key
						ORDER BY
							CASE WHEN filtered.merged_to_id IS NULL THEN 0 ELSE 1 END,
							CASE WHEN filtered.primary_image_tag IS NOT NULL THEN 0 ELSE 1 END,
							CASE WHEN filtered.primary_image_path IS NOT NULL AND filtered.primary_image_path <> '' THEN 0 ELSE 1 END,
							CASE WHEN filtered.overview IS NOT NULL AND filtered.overview <> '' THEN 0 ELSE 1 END,
							filtered.updated_at DESC,
							filtered.id
					) AS merge_row_num
				FROM filtered
			)
			SELECT * FROM ranked WHERE merge_row_num = 1 ORDER BY %s`,
			needDistinct, userColumns, seriesCols, mergedRepresentativeExpr("i"),
			genreJoin, userJoin, seriesJoin, whereClause, outerOrder)
	} else if genreJoin != "" && isRandom {
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

func buildOrderByForAlias(options *ItemQueryOptions, itemAlias, userAlias string) string {
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
				col = itemAlias + ".sort_name"
			case "DateCreated":
				col = itemAlias + ".created_at"
			case "PremiereDate":
				col = itemAlias + ".premiere_date"
			case "ProductionYear":
				col = itemAlias + ".production_year"
			case "CommunityRating":
				col = itemAlias + ".community_rating"
			case "Runtime":
				col = itemAlias + ".runtime_ticks"
			case "Random":
				col = "RANDOM()"
			case "DatePlayed":
				col = userAlias + ".last_played_date"
			case "IndexNumber":
				col = itemAlias + ".parent_index_number NULLS LAST, " + itemAlias + ".index_number"
			case "ParentIndexNumber":
				col = itemAlias + ".parent_index_number"
			default:
				continue
			}
			mapped = append(mapped, col+" "+sortDir)
		}
		if len(mapped) > 0 {
			return strings.Join(mapped, ", ")
		}
	}
	return itemAlias + ".sort_name ASC"
}

func buildOrderBy(options *ItemQueryOptions) string {
	return buildOrderByForAlias(options, "i", "uid")
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

func GetChildCount(ctx context.Context, pool *pgxpool.Pool, parentID string) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM items WHERE parent_id = $1::uuid", parentID).Scan(&count)
	return count, err
}

func GetLibraryDisplayItemCount(ctx context.Context, pool *pgxpool.Pool, libraryID string) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx,
		`SELECT `+libraryRepresentativeCountExpr("i")+`
		   FROM items i
		  WHERE i.library_id = $1::uuid
		    AND i.type IN ('Movie', 'Series')`,
		libraryID).Scan(&count)
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
		`WITH filtered AS (
			SELECT *, CASE WHEN type = 'Movie' THEN COALESCE(merged_to_id::text, id::text) ELSE id::text END AS merge_group_key
			FROM items
			WHERE library_id = $1::uuid AND type = $2
		), ranked AS (
			SELECT filtered.*,
				ROW_NUMBER() OVER (
					PARTITION BY merge_group_key
					ORDER BY
						CASE WHEN filtered.merged_to_id IS NULL THEN 0 ELSE 1 END,
						CASE WHEN filtered.primary_image_tag IS NOT NULL THEN 0 ELSE 1 END,
						CASE WHEN filtered.primary_image_path IS NOT NULL AND filtered.primary_image_path <> '' THEN 0 ELSE 1 END,
						CASE WHEN filtered.overview IS NOT NULL AND filtered.overview <> '' THEN 0 ELSE 1 END,
						filtered.created_at DESC,
						filtered.id
				) AS merge_row_num
			FROM filtered
		)
		SELECT * FROM ranked
		WHERE merge_row_num = 1
		ORDER BY created_at DESC
		LIMIT $3::bigint`,
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

// MergeMultiVersionItems merges duplicate items WITHIN the same platform
// (studio) so that each platform virtual library shows only one entry per
// logical movie, with all physical versions aggregated as MediaSources.
//
// Key design decisions (learned from Jellyfin source):
//   - Group by tmdb_id + type + studio — never merge across different studios
//   - Only merge Movies (Series require episode re-parenting which is complex)
//   - Reset all previous merges first to ensure idempotent results
//   - The merged_to_id filter is only applied in platform library queries,
//     so regular library browsing remains unaffected
func MergeMultiVersionItems(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	// Full reset: undo all previous merges so we can re-compute cleanly.
	// This ensures idempotent behavior and fixes any stale/incorrect merges.
	resetTag, _ := pool.Exec(ctx, `UPDATE items SET merged_to_id = NULL WHERE merged_to_id IS NOT NULL`)
	if resetTag.RowsAffected() > 0 {
		slog.Info("[Merge] Reset previous merges", "reset_count", resetTag.RowsAffected())
	}

	// Find duplicate groups: same tmdb_id + type + studio within platform items.
	// Only merge Movies; Series merging would orphan their child seasons/episodes.
	rows, err := pool.Query(ctx,
		`SELECT tmdb_id, studio
		 FROM items
		 WHERE tmdb_id IS NOT NULL
		   AND type = 'Movie'
		   AND studio IS NOT NULL AND studio <> ''
		   AND merged_to_id IS NULL
		 GROUP BY tmdb_id, studio
		 HAVING COUNT(*) > 1`)
	if err != nil {
		return 0, fmt.Errorf("find merge groups: %w", err)
	}
	defer rows.Close()

	type mergeGroup struct {
		TmdbID int32
		Studio string
	}
	var groups []mergeGroup
	for rows.Next() {
		var g mergeGroup
		if err := rows.Scan(&g.TmdbID, &g.Studio); err != nil {
			return 0, err
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	slog.Info("[Merge] Found duplicate groups", "count", len(groups))

	merged := 0
	for _, g := range groups {
		// Pick best primary: image > overview > most recent
		var primaryID string
		err := pool.QueryRow(ctx,
			`SELECT id::text FROM items
			 WHERE tmdb_id = $1 AND type = 'Movie' AND studio = $2 AND merged_to_id IS NULL
			 ORDER BY
			   (CASE WHEN primary_image_tag IS NOT NULL THEN 0 ELSE 1 END),
			   (CASE WHEN primary_image_path IS NOT NULL AND primary_image_path <> '' THEN 0 ELSE 1 END),
			   (CASE WHEN overview IS NOT NULL AND overview <> '' THEN 0 ELSE 1 END),
			   updated_at DESC
			 LIMIT 1`, g.TmdbID, g.Studio).Scan(&primaryID)
		if err != nil {
			slog.Warn("[Merge] Failed to pick primary", "tmdb_id", g.TmdbID, "studio", g.Studio, "error", err)
			continue
		}

		tag, err := pool.Exec(ctx,
			`UPDATE items SET merged_to_id = $1::uuid
			 WHERE tmdb_id = $2 AND type = 'Movie' AND studio = $3 AND id <> $1::uuid AND merged_to_id IS NULL`,
			primaryID, g.TmdbID, g.Studio)
		if err != nil {
			slog.Warn("[Merge] Failed to set merged_to_id", "primary", primaryID, "error", err)
			continue
		}
		affected := int(tag.RowsAffected())
		merged += affected

		syncBestMetadataToPrimary(ctx, pool, primaryID, g.TmdbID, g.Studio)
	}

	// Re-sync metadata for already-merged groups where primary still lacks data
	syncExistingMergedGroups(ctx, pool)

	return merged, nil
}

// syncBestMetadataToPrimary fills NULL/empty metadata fields on the primary
// using the best available value from any group member (same tmdb_id + studio).
func syncBestMetadataToPrimary(ctx context.Context, pool *pgxpool.Pool, primaryID string, tmdbID int32, studio string) {
	_, err := pool.Exec(ctx, `
		UPDATE items p SET
			primary_image_path  = COALESCE(NULLIF(p.primary_image_path, ''),  best.img_path),
			primary_image_tag   = COALESCE(NULLIF(p.primary_image_tag, ''),   best.img_tag),
			backdrop_image_path = COALESCE(NULLIF(p.backdrop_image_path, ''), best.bd_path),
			backdrop_image_tag  = COALESCE(NULLIF(p.backdrop_image_tag, ''),  best.bd_tag),
			overview            = COALESCE(NULLIF(p.overview, ''),            best.overview),
			community_rating    = COALESCE(p.community_rating,               best.rating),
			official_rating     = COALESCE(NULLIF(p.official_rating, ''),     best.official)
		FROM (
			SELECT
				(SELECT primary_image_path  FROM items WHERE tmdb_id=$2 AND studio=$3 AND primary_image_path  IS NOT NULL AND primary_image_path  <> '' LIMIT 1) AS img_path,
				(SELECT primary_image_tag   FROM items WHERE tmdb_id=$2 AND studio=$3 AND primary_image_tag   IS NOT NULL AND primary_image_tag   <> '' LIMIT 1) AS img_tag,
				(SELECT backdrop_image_path FROM items WHERE tmdb_id=$2 AND studio=$3 AND backdrop_image_path IS NOT NULL AND backdrop_image_path <> '' LIMIT 1) AS bd_path,
				(SELECT backdrop_image_tag  FROM items WHERE tmdb_id=$2 AND studio=$3 AND backdrop_image_tag  IS NOT NULL AND backdrop_image_tag  <> '' LIMIT 1) AS bd_tag,
				(SELECT overview            FROM items WHERE tmdb_id=$2 AND studio=$3 AND overview IS NOT NULL AND overview <> '' LIMIT 1) AS overview,
				(SELECT community_rating    FROM items WHERE tmdb_id=$2 AND studio=$3 AND community_rating    IS NOT NULL LIMIT 1) AS rating,
				(SELECT official_rating     FROM items WHERE tmdb_id=$2 AND studio=$3 AND official_rating     IS NOT NULL AND official_rating <> '' LIMIT 1) AS official
		) best
		WHERE p.id = $1::uuid`,
		primaryID, tmdbID, studio)
	if err != nil {
		slog.Warn("[Merge] syncBestMetadata failed", "primary", primaryID, "error", err)
	}
}

// syncExistingMergedGroups re-syncs metadata for primaries that already
// have secondaries but still lack some metadata fields.
func syncExistingMergedGroups(ctx context.Context, pool *pgxpool.Pool) {
	rows, err := pool.Query(ctx,
		`SELECT DISTINCT p.id::text, p.tmdb_id, p.studio
		 FROM items p
		 WHERE p.merged_to_id IS NULL
		   AND p.tmdb_id IS NOT NULL
		   AND p.studio IS NOT NULL AND p.studio <> ''
		   AND EXISTS (SELECT 1 FROM items s WHERE s.merged_to_id = p.id)
		   AND (p.primary_image_tag IS NULL OR p.primary_image_tag = ''
		     OR p.backdrop_image_tag IS NULL OR p.backdrop_image_tag = ''
		     OR p.overview IS NULL OR p.overview = ''
		     OR p.community_rating IS NULL)`)
	if err != nil {
		return
	}
	defer rows.Close()

	type prim struct {
		ID     string
		TmdbID int32
		Studio string
	}
	var primaries []prim
	for rows.Next() {
		var p prim
		if err := rows.Scan(&p.ID, &p.TmdbID, &p.Studio); err != nil {
			continue
		}
		primaries = append(primaries, p)
	}
	for _, p := range primaries {
		syncBestMetadataToPrimary(ctx, pool, p.ID, p.TmdbID, p.Studio)
	}
}

// GetMediaSourceCount returns the total number of media_versions for an item,
// including versions from all items merged into it (via merged_to_id).
// Mirrors Jellyfin's Video.MediaSourceCount property which counts
// LinkedAlternateVersions + LocalAlternateVersions + 1.
func GetMediaSourceCount(ctx context.Context, pool *pgxpool.Pool, itemID string) int32 {
	var count int32
	pool.QueryRow(ctx,
		`SELECT COALESCE(
			(SELECT COUNT(*) FROM media_versions WHERE item_id = $1::uuid) +
			(SELECT COUNT(*) FROM media_versions mv
			   JOIN items s ON mv.item_id = s.id
			  WHERE s.merged_to_id = $1::uuid),
		0)`, itemID).Scan(&count)
	if count == 0 {
		count = 1
	}
	return count
}

// UnmergeItem resets merged_to_id for a specific item (manual unmerge).
func UnmergeItem(ctx context.Context, pool *pgxpool.Pool, itemID string) error {
	_, err := pool.Exec(ctx,
		`UPDATE items SET merged_to_id = NULL WHERE id = $1::uuid OR merged_to_id = $1::uuid`, itemID)
	return err
}
