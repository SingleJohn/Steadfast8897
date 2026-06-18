package repository

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type CompatItemsRepository struct {
	pool *pgxpool.Pool
}

type CompatItemCountOptions struct {
	RestrictLibraries bool
	AllowedLibraryIDs []string
}

type CompatItemsParentMode string

const (
	CompatItemsParentNone              CompatItemsParentMode = ""
	CompatItemsParentPlatformActor     CompatItemsParentMode = "platform_actor"
	CompatItemsParentPlatformNumPrefix CompatItemsParentMode = "platform_num_prefix"
	CompatItemsParentPlatformStudio    CompatItemsParentMode = "platform_studio"
	CompatItemsParentLibrary           CompatItemsParentMode = "library"
	CompatItemsParentLibraryRecursive  CompatItemsParentMode = "library_recursive"
)

type CompatItemsParentFilter struct {
	Mode  CompatItemsParentMode
	Value string
}

type CompatItemsSearchOptions struct {
	AuthUserID          string
	IDs                 string
	UseEmbyID           bool
	SearchTerm          string
	IncludeTypes        []string
	Parent              *CompatItemsParentFilter
	RestrictLibraries   bool
	AllowedLibraryIDs   []string
	HasSubtitles        *bool
	AnyProviderIDEquals string
	Limit               int64
	Offset              int64
}

type CompatItemsSearchResult struct {
	Total int64
	Rows  []map[string]any
}

type CompatSearchHintsOptions struct {
	SearchTerm   string
	IncludeTypes []string
	Limit        int64
	Offset       int64
}

type CompatSearchHintRow struct {
	ID                     string
	Name                   string
	ItemType               string
	ProductionYear         *int32
	PrimaryImageTag        *string
	BackdropImageTag       *string
	SeriesID               *string
	SeriesName             *string
	RuntimeTicks           *int64
	IndexNumber            *int32
	ParentIndexNumber      *int32
	CommunityRating        *float64
	SeriesPrimaryImageTag  *string
	SeriesBackdropImageTag *string
	SeriesFallbackID       *string
}

type CompatSearchHintsResult struct {
	Total int64
	Rows  []CompatSearchHintRow
}

func NewCompatItemsRepository(pool *pgxpool.Pool) *CompatItemsRepository {
	return &CompatItemsRepository{pool: pool}
}

func (r *CompatItemsRepository) CountItemTypes(ctx context.Context, itemTypes []string, opts CompatItemCountOptions) (map[string]int64, error) {
	out := make(map[string]int64, len(itemTypes))
	where := "type = $1"
	forceFalse := opts.RestrictLibraries && len(opts.AllowedLibraryIDs) == 0
	if forceFalse {
		where += " AND FALSE"
	} else if opts.RestrictLibraries {
		where += " AND library_id::text = ANY($2)"
	}
	for _, itemType := range itemTypes {
		var n int64
		var err error
		if opts.RestrictLibraries && !forceFalse {
			err = r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM items WHERE "+where, itemType, opts.AllowedLibraryIDs).Scan(&n)
		} else {
			err = r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM items WHERE "+where, itemType).Scan(&n)
		}
		if err != nil {
			return nil, err
		}
		out[itemType] = n
	}
	return out, nil
}

func (r *CompatItemsRepository) SearchItems(ctx context.Context, opts CompatItemsSearchOptions) (CompatItemsSearchResult, error) {
	if opts.Limit <= 0 {
		opts.Limit = 50
	}
	userCols := "NULL::bigint AS playback_position_ticks, 0::int AS play_count, FALSE AS is_favorite, FALSE AS played, NULL::timestamp AS last_played_date"
	userJoin := ""
	var args []any
	idx := 1
	if strings.TrimSpace(opts.AuthUserID) != "" {
		userCols = "uid.playback_position_ticks, uid.play_count, uid.is_favorite, uid.played, uid.last_played_date"
		userJoin = fmt.Sprintf(" LEFT JOIN user_item_data uid ON i.id = uid.item_id AND uid.user_id = $%d::uuid", idx)
		args = append(args, strings.TrimSpace(opts.AuthUserID))
		idx++
	}

	baseCols := `i.id, i.name, i.type, i.sort_name, NULL::text AS collection_type, i.overview,
		i.production_year, i.premiere_date, i.community_rating, i.official_rating,
		i.runtime_ticks, i.index_number, i.parent_index_number, i.parent_id,
		i.series_id, i.series_name, i.season_id, i.container, i.file_path,
		i.resolved_path, i.provider_ids, i.primary_image_tag, i.backdrop_image_tag,
		NULL::bigint AS child_count, NULL::bigint AS recursive_item_count,
		i.tagline, i.studio, i.created_at, i.emby_id,
		i.merged_to_id, i.primary_image_path, i.updated_at`
	seriesCols := `, sf.primary_image_tag AS series_primary_image_tag, sf.backdrop_image_tag AS series_backdrop_image_tag, sf.id AS series_fallback_id`
	seriesJoin := " LEFT JOIN items sf ON sf.id = COALESCE(i.series_id, CASE WHEN i.type = 'Season' THEN i.parent_id END)"
	sql := fmt.Sprintf("SELECT %s%s, %s FROM items i%s%s WHERE 1=1", baseCols, seriesCols, userCols, userJoin, seriesJoin)

	var whereParts []string
	useRepresentative := false
	if strings.TrimSpace(opts.IDs) != "" {
		idList := strings.Split(opts.IDs, ",")
		var placeholders []string
		for _, id := range idList {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if opts.UseEmbyID {
				placeholders = append(placeholders, "$"+strconv.Itoa(idx)+"::int")
			} else {
				placeholders = append(placeholders, "$"+strconv.Itoa(idx)+"::uuid")
			}
			args = append(args, id)
			idx++
		}
		if len(placeholders) > 0 {
			if opts.UseEmbyID {
				whereParts = append(whereParts, "i.emby_id IN ("+strings.Join(placeholders, ",")+")")
			} else {
				whereParts = append(whereParts, "i.id IN ("+strings.Join(placeholders, ",")+")")
			}
		}
	}
	if opts.Parent != nil && strings.TrimSpace(opts.Parent.Value) != "" {
		switch opts.Parent.Mode {
		case CompatItemsParentPlatformActor:
			whereParts = append(whereParts, "EXISTS (SELECT 1 FROM cast_members cm WHERE cm.item_id = i.id AND cm.name = $"+strconv.Itoa(idx)+" AND cm.role = 'Actor')")
			args = append(args, opts.Parent.Value)
			idx++
			whereParts = append(whereParts, "i.merged_to_id IS NULL")
			if len(opts.IncludeTypes) == 0 {
				whereParts = append(whereParts, "i.type IN ('Movie','Series')")
			}
		case CompatItemsParentPlatformNumPrefix:
			whereParts = append(whereParts, "regexp_replace(upper(i.catalog_number), '-[0-9]+$', '') = $"+strconv.Itoa(idx))
			args = append(args, opts.Parent.Value)
			idx++
			whereParts = append(whereParts, "i.merged_to_id IS NULL")
			if len(opts.IncludeTypes) == 0 {
				whereParts = append(whereParts, "i.type IN ('Movie','Series')")
			}
		case CompatItemsParentPlatformStudio:
			whereParts = append(whereParts, "i.studio = $"+strconv.Itoa(idx))
			args = append(args, opts.Parent.Value)
			idx++
			whereParts = append(whereParts, "i.merged_to_id IS NULL")
			if len(opts.IncludeTypes) == 0 {
				whereParts = append(whereParts, "i.type IN ('Movie','Series')")
			}
		case CompatItemsParentLibraryRecursive:
			useRepresentative = true
			whereParts = append(whereParts, "i.library_id = $"+strconv.Itoa(idx)+"::uuid")
			args = append(args, opts.Parent.Value)
			idx++
		case CompatItemsParentLibrary:
			useRepresentative = true
			whereParts = append(whereParts, "i.parent_id = $"+strconv.Itoa(idx)+"::uuid")
			args = append(args, opts.Parent.Value)
			idx++
		}
	}
	if opts.RestrictLibraries {
		if len(opts.AllowedLibraryIDs) == 0 {
			whereParts = append(whereParts, "FALSE")
		} else {
			whereParts = append(whereParts, "i.library_id::text = ANY($"+strconv.Itoa(idx)+")")
			args = append(args, opts.AllowedLibraryIDs)
			idx++
		}
	}
	if len(opts.IncludeTypes) > 0 {
		var placeholders []string
		for _, t := range opts.IncludeTypes {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			placeholders = append(placeholders, "$"+strconv.Itoa(idx))
			args = append(args, t)
			idx++
		}
		if len(placeholders) > 0 {
			whereParts = append(whereParts, "i.type IN ("+strings.Join(placeholders, ",")+")")
		} else {
			whereParts = append(whereParts, "i.type IN ('Movie', 'Series', 'Episode')")
		}
	}
	if strings.TrimSpace(opts.SearchTerm) != "" {
		whereParts = append(whereParts, "i.name ILIKE $"+strconv.Itoa(idx))
		args = append(args, "%"+strings.TrimSpace(opts.SearchTerm)+"%")
		idx++
	}
	if opts.HasSubtitles != nil {
		subtitleExists := `(EXISTS (SELECT 1 FROM media_streams ms WHERE ms.item_id = i.id AND LOWER(ms.type) = 'subtitle')
			OR EXISTS (SELECT 1 FROM external_subtitles es WHERE es.item_id = i.id))`
		if *opts.HasSubtitles {
			whereParts = append(whereParts, subtitleExists)
		} else {
			whereParts = append(whereParts, "NOT ("+subtitleExists+")")
		}
	}
	if strings.TrimSpace(opts.AnyProviderIDEquals) != "" {
		var kv []string
		for _, raw := range strings.FieldsFunc(opts.AnyProviderIDEquals, func(r rune) bool { return r == ';' || r == ',' }) {
			raw = strings.TrimSpace(raw)
			dot := strings.Index(raw, ".")
			if dot <= 0 || dot >= len(raw)-1 {
				continue
			}
			provider := strings.ToLower(strings.TrimSpace(raw[:dot]))
			id := strings.TrimSpace(raw[dot+1:])
			if provider == "" || id == "" {
				continue
			}
			kv = append(kv, provider+"="+id)
		}
		if len(kv) > 0 {
			// 走 idx_items_provider_kv(GIN),等价于原 OR-of-EXISTS,避免整表顺序扫描。
			whereParts = append(whereParts, fmt.Sprintf("item_provider_kv(i.provider_ids) && $%d::text[]", idx))
			args = append(args, kv)
			idx++
		}
	}
	if len(whereParts) > 0 {
		sql += " AND " + strings.Join(whereParts, " AND ")
	}

	countTarget := "COUNT(*)"
	if useRepresentative {
		countTarget = "COUNT(DISTINCT " + compatItemsRepresentativeExpr("i") + ")"
	}
	countSQL := "SELECT " + countTarget + " FROM items i" + userJoin + " WHERE 1=1"
	if len(whereParts) > 0 {
		countSQL += " AND " + strings.Join(whereParts, " AND ")
	}
	countArgs := append([]any{}, args...)
	var totalCount int64
	if err := r.pool.QueryRow(ctx, countSQL, countArgs...).Scan(&totalCount); err != nil {
		return CompatItemsSearchResult{}, err
	}

	if useRepresentative {
		sql = fmt.Sprintf(
			`WITH filtered AS (
				SELECT %s%s, %s, %s AS merge_group_key
				FROM items i%s%s
				WHERE 1=1%s
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
			SELECT * FROM ranked WHERE merge_row_num = 1`,
			baseCols, seriesCols, userCols, compatItemsRepresentativeExpr("i"), userJoin, seriesJoin, compatItemsWhereSuffix(whereParts))
		sql += " ORDER BY ranked.sort_name"
	} else {
		sql += " ORDER BY i.sort_name"
	}
	sql += " LIMIT $" + strconv.Itoa(idx) + "::bigint"
	args = append(args, opts.Limit)
	idx++
	if opts.Offset > 0 {
		sql += " OFFSET $" + strconv.Itoa(idx) + "::bigint"
		args = append(args, opts.Offset)
	}

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return CompatItemsSearchResult{}, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return CompatItemsSearchResult{}, err
		}
		fds := rows.FieldDescriptions()
		m := make(map[string]any, len(fds))
		for i, fd := range fds {
			m[string(fd.Name)] = vals[i]
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return CompatItemsSearchResult{}, err
	}
	return CompatItemsSearchResult{Total: totalCount, Rows: out}, nil
}

func (r *CompatItemsRepository) SearchHints(ctx context.Context, opts CompatSearchHintsOptions) (CompatSearchHintsResult, error) {
	if strings.TrimSpace(opts.SearchTerm) == "" {
		return CompatSearchHintsResult{}, nil
	}
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	args := []any{"%" + strings.TrimSpace(opts.SearchTerm) + "%"}
	idx := 2
	whereExtra := ""
	if len(opts.IncludeTypes) > 0 {
		var placeholders []string
		for _, t := range opts.IncludeTypes {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			placeholders = append(placeholders, "$"+strconv.Itoa(idx))
			args = append(args, t)
			idx++
		}
		if len(placeholders) > 0 {
			whereExtra = " AND i.type IN (" + strings.Join(placeholders, ",") + ")"
		}
	} else {
		whereExtra = " AND i.type IN ('Movie', 'Series', 'Episode')"
	}
	countSQL := "SELECT COUNT(*) FROM items i WHERE i.name ILIKE $1" + whereExtra
	var totalCount int64
	if err := r.pool.QueryRow(ctx, countSQL, args...).Scan(&totalCount); err != nil {
		return CompatSearchHintsResult{}, err
	}
	sql := `SELECT i.id, i.name, i.type, i.production_year,
		i.primary_image_tag, i.backdrop_image_tag,
		i.series_id, i.series_name, i.runtime_ticks,
		i.index_number, i.parent_index_number, i.community_rating,
		sf.primary_image_tag AS series_primary_image_tag,
		sf.backdrop_image_tag AS series_backdrop_image_tag,
		sf.id AS series_fallback_id
		FROM items i
		LEFT JOIN items sf ON sf.id = COALESCE(i.series_id, CASE WHEN i.type = 'Season' THEN i.parent_id END)
		WHERE i.name ILIKE $1` + whereExtra
	sql += " ORDER BY CASE WHEN i.name ILIKE $" + strconv.Itoa(idx) + " THEN 0 ELSE 1 END, i.type, i.sort_name"
	args = append(args, strings.TrimSpace(opts.SearchTerm))
	idx++
	sql += " LIMIT $" + strconv.Itoa(idx) + "::bigint"
	args = append(args, opts.Limit)
	idx++
	if opts.Offset > 0 {
		sql += " OFFSET $" + strconv.Itoa(idx) + "::bigint"
		args = append(args, opts.Offset)
	}
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return CompatSearchHintsResult{}, err
	}
	defer rows.Close()
	out := []CompatSearchHintRow{}
	for rows.Next() {
		var row CompatSearchHintRow
		if err := rows.Scan(
			&row.ID, &row.Name, &row.ItemType, &row.ProductionYear, &row.PrimaryImageTag, &row.BackdropImageTag,
			&row.SeriesID, &row.SeriesName, &row.RuntimeTicks, &row.IndexNumber, &row.ParentIndexNumber,
			&row.CommunityRating, &row.SeriesPrimaryImageTag, &row.SeriesBackdropImageTag, &row.SeriesFallbackID,
		); err != nil {
			return CompatSearchHintsResult{}, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return CompatSearchHintsResult{}, err
	}
	return CompatSearchHintsResult{Total: totalCount, Rows: out}, nil
}

func compatItemsRepresentativeExpr(itemAlias string) string {
	return fmt.Sprintf(
		"CASE WHEN %s.type = 'Movie' THEN COALESCE(%s.merged_to_id::text, %s.id::text) ELSE %s.id::text END",
		itemAlias, itemAlias, itemAlias, itemAlias,
	)
}

func compatItemsWhereSuffix(whereParts []string) string {
	if len(whereParts) == 0 {
		return ""
	}
	return " AND " + strings.Join(whereParts, " AND ")
}
