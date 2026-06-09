package models

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

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
	return options.ParentID != nil || len(options.ParentIDs) > 0 || options.LibraryID != nil
}

func QueryItems(ctx context.Context, pool *pgxpool.Pool, options *ItemQueryOptions) (*ItemQueryResult, error) {
	var conditions []string
	var params []interface{}
	paramIdx := 1
	useRepresentative := shouldUseLibraryRepresentative(options)

	if len(options.ParentIDs) > 0 {
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
		// 除条目名外,也匹配该条目下各 media_version 的文件名(name)。
		// 混合库里被错误坍缩成"多集/多版本"的条目,名字往往是无意义的目录名(如 "96-9"),
		// 但每个真实影片的文件名(如 "叶子姐姐9065")保留在 media_versions 里 —— 搜文件名也能命中。
		// 同一个 %term% 参数两处复用,不额外占位。
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

	// AnyProviderIdEquals 过滤:大小写不敏感匹配 provider key、精确匹配 id 值,
	// 不依赖 provider_ids 里 key 的大小写(Tmdb/tmdb 均可)与 value 类型(字符串/数字均可)。
	// 多个条件之间为 OR(任一命中)。
	if len(options.AnyProviderID) > 0 {
		ors := make([]string, len(options.AnyProviderID))
		for i, p := range options.AnyProviderID {
			ors[i] = fmt.Sprintf(
				"EXISTS (SELECT 1 FROM jsonb_each_text(i.provider_ids) pe WHERE LOWER(pe.key) = $%d AND pe.value = $%d)",
				paramIdx, paramIdx+1)
			params = append(params, p.Provider, p.ID)
			paramIdx += 2
		}
		conditions = append(conditions,
			"i.provider_ids IS NOT NULL AND jsonb_typeof(i.provider_ids) = 'object' AND ("+strings.Join(ors, " OR ")+")")
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

	// Platform virtual libraries show only the global merged primary.
	// Ordinary user libraries use a per-library representative selection later
	// so a title does not disappear just because the global primary lives elsewhere.
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

	orderBy := buildOrderBy(options)
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

	// Random 快速路径：从最近 1000 条中随机选取，跳过昂贵的 COUNT 和全表扫描
	if isRandom && !useRepresentative {
		lim := int64(6)
		if options.Limit != nil {
			lim = *options.Limit
		}
		// 用 pg_class 估算值作为 TotalCount（瞬时返回）
		var totalCount int64
		_ = pool.QueryRow(ctx, "SELECT COALESCE(reltuples, 0)::bigint FROM pg_class WHERE relname = 'items'").Scan(&totalCount)

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

		randomParams := make([]interface{}, len(params))
		copy(randomParams, params)
		randomParams = append(randomParams, lim)

		rows, err := pool.Query(ctx, randomSQL, randomParams...)
		if err != nil {
			return nil, fmt.Errorf("random query: %w", err)
		}
		items, userData, err := scanItemRows(rows)
		if err != nil {
			return nil, err
		}
		return &ItemQueryResult{Items: items, UserData: userData, TotalCount: totalCount}, nil
	}

	// 非 Random 路径：COUNT
	// 当 StartIndex > 0 时，使用 pg_class 估算值（O(1)）避免全表 COUNT
	var totalCount int64
	if options.StartIndex != nil && *options.StartIndex > 0 && genreJoin == "" && !useRepresentative {
		// 快速估算：对简单 type 筛选使用 pg_class 统计信息
		if len(options.IncludeItemTypes) == 1 && options.ParentID == nil && len(options.ParentIDs) == 0 && options.LibraryID == nil && options.SearchTerm == nil && len(options.Studio) == 0 {
			_ = pool.QueryRow(ctx,
				"SELECT COALESCE(n_live_tup, 0) FROM pg_stat_user_tables WHERE relname = 'items'").Scan(&totalCount)
			// 如果估算值明显小于 StartIndex，用精确值
			if totalCount < *options.StartIndex {
				countSQL := fmt.Sprintf("SELECT COUNT(*) FROM items i WHERE i.type = $1")
				_ = pool.QueryRow(ctx, countSQL, options.IncludeItemTypes[0]).Scan(&totalCount)
			}
		} else {
			countTarget := "DISTINCT i.id"
			countSQL := fmt.Sprintf(
				"SELECT COUNT(%s) FROM items i %s %s %s",
				countTarget, genreJoin, userJoin, whereClause)
			countParams := make([]interface{}, len(params))
			copy(countParams, params)
			_ = pool.QueryRow(ctx, countSQL, countParams...).Scan(&totalCount)
		}
	} else {
		countTarget := "DISTINCT i.id"
		if useRepresentative {
			countTarget = "DISTINCT " + mergedRepresentativeExpr("i")
		} else if genreJoin == "" {
			// 无 JOIN 时 id 本身唯一，COUNT(*) 更快
			countTarget = "*"
		}
		countSQL := fmt.Sprintf(
			"SELECT COUNT(%s) FROM items i %s %s %s",
			countTarget, genreJoin, userJoin, whereClause)
		countParams := make([]interface{}, len(params))
		copy(countParams, params)
		err := pool.QueryRow(ctx, countSQL, countParams...).Scan(&totalCount)
		if err != nil {
			return nil, fmt.Errorf("count query: %w", err)
		}
	}

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
	} else if genreJoin != "" {
		itemSQL = fmt.Sprintf(
			"SELECT DISTINCT i.*, %s, %s FROM items i %s %s %s %s ORDER BY %s",
			userColumns, seriesCols, genreJoin, userJoin, seriesJoin, whereClause, orderBy)
	} else {
		itemSQL = fmt.Sprintf(
			"SELECT %s i.*, %s, %s FROM items i %s %s %s %s ORDER BY %s",
			needDistinct, userColumns, seriesCols, genreJoin, userJoin, seriesJoin, whereClause, orderBy)
	}

	itemParams := make([]interface{}, len(params))
	copy(itemParams, params)

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
