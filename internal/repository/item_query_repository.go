package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ItemProviderIDMatch struct {
	Provider string
	ID       string
}

type ItemQueryOptions struct {
	ParentID          *string
	ParentIDs         []string
	ParentLibraryID   *string
	RecursiveParentID *string
	IncludeItemTypes  []string
	SortBy            *string
	SortOrder         *string
	Limit             *int64
	StartIndex        *int64
	Recursive         bool
	LibraryID         *string
	SearchTerm        *string
	NameStartsWith    *string
	Filters           []string
	UserID            *string
	GenreIDs          []string
	GenreNames        []string
	TagIDs            []int
	TagNames          []string
	PersonIDs         []string
	PersonNames       []string
	PersonTypes       []string
	Years             []int
	Studio            []string
	ActorName         []string
	CatalogPrefix     []string
	AnyProviderID     []ItemProviderIDMatch
	HasSubtitles      *bool
	AllowedLibraryIDs []string
	LightMode         bool
}

type ItemQueryResult struct {
	Rows       []map[string]any
	TotalCount int64
}

type ItemQueryRepository struct {
	pool *pgxpool.Pool
}

func NewItemQueryRepository(pool *pgxpool.Pool) *ItemQueryRepository {
	return &ItemQueryRepository{pool: pool}
}

func (r *ItemQueryRepository) QueryItems(ctx context.Context, options *ItemQueryOptions) (*ItemQueryResult, error) {
	if options == nil {
		options = &ItemQueryOptions{}
	}
	var conditions []string
	var params []any
	paramIdx := 1
	useRepresentative := itemShouldUseLibraryRepresentative(options)

	if options.ParentLibraryID != nil {
		conditions = append(conditions, fmt.Sprintf("i.library_id = $%d::uuid", paramIdx))
		params = append(params, *options.ParentLibraryID)
		paramIdx++
		if !options.Recursive {
			conditions = append(conditions, "i.parent_id IS NULL")
		}
	} else if options.RecursiveParentID != nil {
		conditions = append(conditions, fmt.Sprintf(
			`i.id IN (
				WITH RECURSIVE descendants AS (
					SELECT id FROM items WHERE parent_id = $%d::uuid
					UNION ALL
					SELECT child.id FROM items child JOIN descendants d ON child.parent_id = d.id
				)
				SELECT id FROM descendants
			)`, paramIdx))
		params = append(params, *options.RecursiveParentID)
		paramIdx++
	} else if len(options.ParentIDs) > 0 {
		col := "i.parent_id"
		if options.Recursive {
			col = "i.library_id"
		}
		conditions = append(conditions, fmt.Sprintf("%s = ANY($%d::uuid[])", col, paramIdx))
		params = append(params, options.ParentIDs)
		paramIdx++
	} else if options.ParentID != nil {
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
	if options.AllowedLibraryIDs != nil {
		if len(options.AllowedLibraryIDs) == 0 {
			conditions = append(conditions, "FALSE")
		} else {
			conditions = append(conditions, fmt.Sprintf("i.library_id::text = ANY($%d)", paramIdx))
			params = append(params, options.AllowedLibraryIDs)
			paramIdx++
		}
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
		conditions = append(conditions, fmt.Sprintf(
			"(i.name ILIKE $%d OR EXISTS (SELECT 1 FROM media_versions mv WHERE mv.item_id = i.id AND mv.name ILIKE $%d))",
			paramIdx, paramIdx))
		params = append(params, "%"+*options.SearchTerm+"%")
		paramIdx++
	}

	if options.NameStartsWith != nil {
		conditions = append(conditions, fmt.Sprintf("i.name ILIKE $%d", paramIdx))
		params = append(params, *options.NameStartsWith+"%")
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
	if len(options.GenreNames) > 0 {
		conditions = append(conditions, fmt.Sprintf(
			`EXISTS (
				SELECT 1 FROM item_genres ign_filter
				JOIN genres gn_filter ON gn_filter.id = ign_filter.genre_id
				WHERE ign_filter.item_id = i.id AND gn_filter.name = ANY($%d)
			)`, paramIdx))
		params = append(params, options.GenreNames)
		paramIdx++
	}

	if len(options.TagIDs) > 0 {
		conditions = append(conditions, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM item_tags it_filter WHERE it_filter.item_id = i.id AND it_filter.tag_id = ANY($%d::int[]))", paramIdx))
		params = append(params, options.TagIDs)
		paramIdx++
	}
	if len(options.TagNames) > 0 {
		conditions = append(conditions, fmt.Sprintf(
			`EXISTS (
				SELECT 1 FROM item_tags itn_filter
				JOIN tags tn_filter ON tn_filter.id = itn_filter.tag_id
				WHERE itn_filter.item_id = i.id AND tn_filter.name = ANY($%d)
			)`, paramIdx))
		params = append(params, options.TagNames)
		paramIdx++
	}

	personTypeClause := ""
	hasPersonFilter := len(options.PersonIDs) > 0 || len(options.PersonNames) > 0
	if hasPersonFilter && len(options.PersonTypes) > 0 {
		personTypeClause = fmt.Sprintf(" AND cm.role = ANY($%d)", paramIdx)
		params = append(params, options.PersonTypes)
		paramIdx++
	}
	if len(options.PersonIDs) > 0 {
		conditions = append(conditions, fmt.Sprintf(
			`EXISTS (
				SELECT 1 FROM cast_members cm
				WHERE cm.item_id = i.id
				  AND (cm.person_id = ANY($%d::uuid[]) OR cm.id = ANY($%d::uuid[]))
				  %s
			)`, paramIdx, paramIdx, personTypeClause))
		params = append(params, options.PersonIDs)
		paramIdx++
	}
	if len(options.PersonNames) > 0 {
		conditions = append(conditions, fmt.Sprintf(
			`EXISTS (
				SELECT 1 FROM cast_members cm
				WHERE cm.item_id = i.id
				  AND cm.name = ANY($%d)
				  %s
			)`, paramIdx, personTypeClause))
		params = append(params, options.PersonNames)
		paramIdx++
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

	if len(options.Studio) > 0 {
		conditions = append(conditions, fmt.Sprintf("i.studio = ANY($%d)", paramIdx))
		params = append(params, options.Studio)
		paramIdx++
	}

	if len(options.ActorName) > 0 {
		conditions = append(conditions, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM cast_members cm WHERE cm.item_id = i.id AND cm.name = ANY($%d) AND cm.role = 'Actor')", paramIdx))
		params = append(params, options.ActorName)
		paramIdx++
	}

	if len(options.CatalogPrefix) > 0 {
		conditions = append(conditions, fmt.Sprintf("regexp_replace(upper(i.catalog_number), '-[0-9]+$', '') = ANY($%d)", paramIdx))
		params = append(params, options.CatalogPrefix)
		paramIdx++
	}

	if len(options.AnyProviderID) > 0 {
		// 走 idx_items_provider_kv(GIN):规范化成 lower(key)=value 数组后做重叠匹配,
		// 等价于原 OR-of-EXISTS(任一命中),避免整表 jsonb_each_text 顺序扫描。
		kv := make([]string, len(options.AnyProviderID))
		for i, p := range options.AnyProviderID {
			kv[i] = p.Provider + "=" + p.ID
		}
		conditions = append(conditions, fmt.Sprintf("item_provider_kv(i.provider_ids) && $%d::text[]", paramIdx))
		params = append(params, kv)
		paramIdx++
	}

	if options.HasSubtitles != nil {
		subtitleExists := `(EXISTS (SELECT 1 FROM media_streams ms WHERE ms.item_id = i.id AND LOWER(ms.type) = 'subtitle')
			OR EXISTS (SELECT 1 FROM external_subtitles es WHERE es.item_id = i.id))`
		if *options.HasSubtitles {
			conditions = append(conditions, subtitleExists)
		} else {
			conditions = append(conditions, "NOT ("+subtitleExists+")")
		}
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
			conditions = append(conditions, "uid.playback_position_ticks > 0 AND uid.is_hidden_from_resume = FALSE AND (uid.played IS NULL OR uid.played = FALSE)")
		case "IsFavorite":
			conditions = append(conditions, "uid.is_favorite = TRUE")
		case "IsUnplayed":
			conditions = append(conditions, "(uid.played IS NULL OR uid.played = FALSE)")
		case "IsPlayed":
			conditions = append(conditions, "uid.played = TRUE")
		}
	}

	if len(options.Studio) > 0 || len(options.ActorName) > 0 || len(options.CatalogPrefix) > 0 ||
		(!useRepresentative && (len(options.GenreIDs) > 0 || len(options.GenreNames) > 0 ||
			len(options.TagIDs) > 0 || len(options.TagNames) > 0 ||
			len(options.PersonIDs) > 0 || len(options.PersonNames) > 0)) {
		conditions = append(conditions, "i.merged_to_id IS NULL")
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	orderBy := itemBuildOrderBy(options)
	isRandom := strings.Contains(orderBy, "RANDOM()")

	userColumns := "NULL::bigint as playback_position_ticks, 0::int as play_count, FALSE as is_favorite, FALSE as played, NULL::timestamp as last_played_date"
	if options.UserID != nil {
		userColumns = "uid.playback_position_ticks, uid.play_count, uid.is_favorite, uid.played, uid.last_played_date"
	}

	var seriesJoin, seriesCols string
	if options.LightMode {
		seriesJoin = ""
		seriesCols = "NULL::text as series_primary_image_tag, NULL::text as series_backdrop_image_tag, NULL::uuid as series_fallback_id"
	} else {
		seriesJoin = "LEFT JOIN items series_fallback ON series_fallback.id = COALESCE(i.series_id, CASE WHEN i.type = 'Season' THEN i.parent_id END)"
		seriesCols = "series_fallback.primary_image_tag as series_primary_image_tag, series_fallback.backdrop_image_tag as series_backdrop_image_tag, series_fallback.id as series_fallback_id"
	}

	needDistinct := ""
	if genreJoin != "" {
		needDistinct = "DISTINCT"
	}

	if isRandom && !useRepresentative {
		lim := int64(6)
		if options.Limit != nil {
			lim = *options.Limit
		}
		var totalCount int64
		_ = r.pool.QueryRow(ctx, "SELECT COALESCE(reltuples, 0)::bigint FROM pg_class WHERE relname = 'items'").Scan(&totalCount)

		randomSQL := fmt.Sprintf(
			`WITH recent_ids AS (
				SELECT i.id FROM items i %s %s %s ORDER BY i.created_at DESC LIMIT 1000
			)
			SELECT %s i.*, %s, %s FROM items i %s %s
			WHERE i.id IN (SELECT id FROM recent_ids)
			ORDER BY RANDOM() LIMIT $%d::bigint`,
			genreJoin, userJoin, whereClause,
			needDistinct, userColumns, seriesCols, userJoin, seriesJoin,
			paramIdx)

		randomParams := append([]any{}, params...)
		randomParams = append(randomParams, lim)
		rows, err := r.pool.Query(ctx, randomSQL, randomParams...)
		if err != nil {
			return nil, fmt.Errorf("random query: %w", err)
		}
		rowMaps, err := scanItemQueryRows(rows)
		if err != nil {
			return nil, err
		}
		return &ItemQueryResult{Rows: rowMaps, TotalCount: totalCount}, nil
	}

	// useRepresentative 的数据查询本就必须扫全库 + 窗口排序(无法借索引提前终止),
	// 所以总数随数据查询用 COUNT(*) OVER() 一并带出,省掉这里再单独 COUNT 全扫一次。
	// 仅当数据查询返回 0 行(OFFSET 越界 / 空集)拿不到窗口值时,用此闭包回填精确总数。
	representativeCount := func() (int64, error) {
		countSQL := fmt.Sprintf(
			"SELECT COUNT(DISTINCT %s) FROM items i %s %s %s",
			itemMergedRepresentativeExpr("i"), genreJoin, userJoin, whereClause)
		countParams := append([]any{}, params...)
		var n int64
		err := r.pool.QueryRow(ctx, countSQL, countParams...).Scan(&n)
		return n, err
	}

	var totalCount int64
	totalCountFromRows := useRepresentative
	if useRepresentative {
		// 推迟到数据查询(见下方 COUNT(*) OVER())
	} else if options.StartIndex != nil && *options.StartIndex > 0 && genreJoin == "" {
		if len(options.IncludeItemTypes) == 1 && options.ParentID == nil && len(options.ParentIDs) == 0 && options.LibraryID == nil && options.SearchTerm == nil && len(options.Studio) == 0 {
			_ = r.pool.QueryRow(ctx,
				"SELECT COALESCE(n_live_tup, 0) FROM pg_stat_user_tables WHERE relname = 'items'").Scan(&totalCount)
			if totalCount < *options.StartIndex {
				countSQL := "SELECT COUNT(*) FROM items i WHERE i.type = $1"
				_ = r.pool.QueryRow(ctx, countSQL, options.IncludeItemTypes[0]).Scan(&totalCount)
			}
		} else {
			countSQL := fmt.Sprintf(
				"SELECT COUNT(%s) FROM items i %s %s %s",
				"DISTINCT i.id", genreJoin, userJoin, whereClause)
			countParams := append([]any{}, params...)
			_ = r.pool.QueryRow(ctx, countSQL, countParams...).Scan(&totalCount)
		}
	} else {
		countTarget := "DISTINCT i.id"
		if genreJoin == "" {
			countTarget = "*"
		}
		countSQL := fmt.Sprintf(
			"SELECT COUNT(%s) FROM items i %s %s %s",
			countTarget, genreJoin, userJoin, whereClause)
		countParams := append([]any{}, params...)
		err := r.pool.QueryRow(ctx, countSQL, countParams...).Scan(&totalCount)
		if err != nil {
			return nil, fmt.Errorf("count query: %w", err)
		}
	}

	var itemSQL string
	if useRepresentative {
		outerOrder := itemBuildOrderByForAlias(options, "ranked", "ranked")
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
			SELECT *, COUNT(*) OVER() AS __total_count FROM ranked WHERE merge_row_num = 1 ORDER BY %s`,
			needDistinct, userColumns, seriesCols, itemMergedRepresentativeExpr("i"),
			genreJoin, userJoin, seriesJoin, whereClause, outerOrder)
	} else if genreJoin != "" {
		itemSQL = fmt.Sprintf(
			"SELECT DISTINCT i.*, %s, %s FROM items i %s %s %s %s ORDER BY %s",
			userColumns, seriesCols, genreJoin, userJoin, seriesJoin, whereClause, orderBy)
	} else {
		itemSQL = fmt.Sprintf(
			"SELECT %s i.*, %s, %s FROM items i %s %s %s %s ORDER BY %s",
			needDistinct, userColumns, seriesCols, genreJoin, userJoin, seriesJoin, whereClause, orderBy)
	}

	itemParams := append([]any{}, params...)
	if options.Limit != nil {
		itemSQL += fmt.Sprintf(" LIMIT $%d::bigint", paramIdx)
		itemParams = append(itemParams, *options.Limit)
		paramIdx++
	}
	if options.StartIndex != nil {
		itemSQL += fmt.Sprintf(" OFFSET $%d::bigint", paramIdx)
		itemParams = append(itemParams, *options.StartIndex)
	}

	rows, err := r.pool.Query(ctx, itemSQL, itemParams...)
	if err != nil {
		return nil, fmt.Errorf("item query: %w", err)
	}
	rowMaps, err := scanItemQueryRows(rows)
	if err != nil {
		return nil, err
	}
	if totalCountFromRows {
		if len(rowMaps) > 0 {
			if v, ok := rowMaps[0]["__total_count"].(int64); ok {
				totalCount = v
			}
			for _, m := range rowMaps {
				delete(m, "__total_count")
			}
		} else if n, err := representativeCount(); err == nil {
			// 0 行:OFFSET 越界或空集,无窗口值可取,回填精确总数。
			totalCount = n
		}
	}
	return &ItemQueryResult{Rows: rowMaps, TotalCount: totalCount}, nil
}

func itemShouldUseLibraryRepresentative(options *ItemQueryOptions) bool {
	if options == nil || options.Studio != nil {
		return false
	}
	return options.ParentID != nil || len(options.ParentIDs) > 0 ||
		options.ParentLibraryID != nil || options.RecursiveParentID != nil || options.LibraryID != nil
}

func itemMergedRepresentativeExpr(itemAlias string) string {
	return fmt.Sprintf(
		"CASE WHEN %s.type = 'Movie' THEN COALESCE(%s.merged_to_id::text, %s.id::text) ELSE %s.id::text END",
		itemAlias, itemAlias, itemAlias, itemAlias,
	)
}

func itemBuildOrderByForAlias(options *ItemQueryOptions, itemAlias, userAlias string) string {
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
			col, ok := itemSortExpression(f, itemAlias, userAlias)
			if !ok {
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

func itemBuildOrderBy(options *ItemQueryOptions) string {
	return itemBuildOrderByForAlias(options, "i", "uid")
}

func itemSortExpression(field, itemAlias, userAlias string) (string, bool) {
	switch field {
	case "IsFolder":
		return "CASE WHEN " + itemAlias + ".type IN ('Folder','Series','Season','CollectionFolder') THEN 0 ELSE 1 END", true
	case "Filename":
		return itemAlias + ".file_path", true
	case "SortName":
		return itemAlias + ".sort_name", true
	case "DateCreated":
		return itemAlias + ".created_at", true
	case "PremiereDate":
		return itemAlias + ".premiere_date", true
	case "ProductionYear":
		return itemAlias + ".production_year", true
	case "CommunityRating":
		return itemAlias + ".community_rating", true
	case "Runtime":
		return itemAlias + ".runtime_ticks", true
	case "Random":
		return "RANDOM()", true
	case "DatePlayed":
		return userAlias + ".last_played_date", true
	case "IndexNumber":
		return itemAlias + ".parent_index_number NULLS LAST, " + itemAlias + ".index_number", true
	case "ParentIndexNumber":
		return itemAlias + ".parent_index_number", true
	default:
		return "", false
	}
}

func scanItemQueryRows(rows pgx.Rows) ([]map[string]any, error) {
	defer rows.Close()
	fieldDescs := rows.FieldDescriptions()
	colNames := make([]string, len(fieldDescs))
	for i, fd := range fieldDescs {
		colNames[i] = string(fd.Name)
	}
	var out []map[string]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		m := make(map[string]any, len(colNames))
		for i, name := range colNames {
			m[name] = vals[i]
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
