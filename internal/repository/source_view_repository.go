package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (r *SourceRepository) UpsertLibraryView(ctx context.Context, in SourceLibraryViewUpsert) (*SourceLibraryView, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO source_library_views (
			public_uuid, name, display_name, dimension, match_value, match_values,
			collection_type, provider_ids, filter, enabled, expose_to_emby, sort_order, config,
			cover_image_path, cover_image_tag
		) VALUES (
			$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10, $11, $12, $13::jsonb, $14, $15
		)
		ON CONFLICT (dimension, match_value) DO UPDATE SET
			public_uuid = EXCLUDED.public_uuid,
			name = EXCLUDED.name,
			display_name = EXCLUDED.display_name,
			match_values = EXCLUDED.match_values,
			collection_type = EXCLUDED.collection_type,
			provider_ids = EXCLUDED.provider_ids,
			filter = EXCLUDED.filter,
			enabled = EXCLUDED.enabled,
			expose_to_emby = EXCLUDED.expose_to_emby,
			sort_order = EXCLUDED.sort_order,
			config = EXCLUDED.config,
			cover_image_path = EXCLUDED.cover_image_path,
			cover_image_tag = EXCLUDED.cover_image_tag,
			updated_at = NOW()
		RETURNING id, public_uuid::text, name, display_name, dimension, match_value, match_values,
		          collection_type, provider_ids, filter, enabled, expose_to_emby, sort_order,
		          config, cover_image_path, cover_image_tag, created_at, updated_at`,
		in.PublicUUID, in.Name, in.DisplayName, in.Dimension, in.MatchValue, in.MatchValues,
		defaultString(in.CollectionType, "mixed"), in.ProviderIDs, jsonBytesOrObject(in.Filter),
		in.Enabled, in.ExposeToEmby, in.SortOrder, jsonBytesOrObject(in.Config), in.CoverImagePath, in.CoverImageTag)
	return scanSourceLibraryView(row)
}

func (r *SourceRepository) GetLibraryViewByID(ctx context.Context, id int64) (*SourceLibraryView, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, public_uuid::text, name, display_name, dimension, match_value, match_values,
		       collection_type, provider_ids, filter, enabled, expose_to_emby, sort_order,
		       config, cover_image_path, cover_image_tag, created_at, updated_at
		  FROM source_library_views
		 WHERE id = $1`, id)
	return scanSourceLibraryView(row)
}

func (r *SourceRepository) ListLibraryViews(ctx context.Context, withCounts bool) ([]SourceLibraryView, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, public_uuid::text, name, display_name, dimension, match_value, match_values,
		       collection_type, provider_ids, filter, enabled, expose_to_emby, sort_order,
		       config, cover_image_path, cover_image_tag, created_at, updated_at
		  FROM source_library_views
		 ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceLibraryView
	for rows.Next() {
		view, err := scanSourceLibraryView(rows)
		if err != nil {
			return nil, err
		}
		if withCounts {
			view.ItemCount, _ = r.CountItemsForLibraryView(ctx, *view)
		}
		out = append(out, *view)
	}
	return out, rows.Err()
}

func (r *SourceRepository) DeleteLibraryView(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM source_library_views WHERE id = $1`, id)
	return err
}

func (r *SourceRepository) RenameLibraryView(ctx context.Context, id int64, name string) (*SourceLibraryView, error) {
	name = strings.TrimSpace(name)
	var displayName any
	if name != "" {
		displayName = name
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE source_library_views
		   SET display_name = $2,
		       updated_at = NOW()
		 WHERE id = $1
		RETURNING id, public_uuid::text, name, display_name, dimension, match_value, match_values,
		          collection_type, provider_ids, filter, enabled, expose_to_emby, sort_order,
		          config, cover_image_path, cover_image_tag, created_at, updated_at`, id, displayName)
	return scanSourceLibraryView(row)
}

func (r *SourceRepository) SetLibraryViewCover(ctx context.Context, id int64, path, tag string) (*SourceLibraryView, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE source_library_views
		   SET cover_image_path = $2,
		       cover_image_tag = $3,
		       updated_at = NOW()
		 WHERE id = $1
		RETURNING id, public_uuid::text, name, display_name, dimension, match_value, match_values,
		          collection_type, provider_ids, filter, enabled, expose_to_emby, sort_order,
		          config, cover_image_path, cover_image_tag, created_at, updated_at`, id, path, tag)
	return scanSourceLibraryView(row)
}

func (r *SourceRepository) ClearLibraryViewCover(ctx context.Context, id int64) (string, error) {
	var oldPath *string
	_ = r.pool.QueryRow(ctx, `SELECT cover_image_path FROM source_library_views WHERE id = $1`, id).Scan(&oldPath)
	_, err := r.pool.Exec(ctx, `
		UPDATE source_library_views
		   SET cover_image_path = NULL,
		       cover_image_tag = NULL,
		       updated_at = NOW()
		 WHERE id = $1`, id)
	if oldPath != nil {
		return *oldPath, err
	}
	return "", err
}

func (r *SourceRepository) UpdateLibraryViewSortOrder(ctx context.Context, orderedIDs []int64) error {
	for i, id := range orderedIDs {
		if _, err := r.pool.Exec(ctx, `UPDATE source_library_views SET sort_order = $1, updated_at = NOW() WHERE id = $2`, i, id); err != nil {
			return err
		}
	}
	return nil
}

func (r *SourceRepository) DiscoverLibraryViewValues(ctx context.Context, dimension, search string, minCount int64) ([]SourceDimensionValue, error) {
	dimension = strings.TrimSpace(dimension)
	search = strings.TrimSpace(search)
	if minCount <= 0 {
		minCount = 1
	}
	groupExpr, whereClause, err := sourceDimensionDiscoverSQL(dimension)
	if err != nil {
		return nil, err
	}
	args := []any{}
	if search != "" {
		args = append(args, "%"+search+"%")
		whereClause += fmt.Sprintf(" AND %s ILIKE $%d", groupExpr, len(args))
	}
	args = append(args, minCount)
	minArg := len(args)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT %s AS value, COUNT(*) AS count
		  FROM source_items si
		 WHERE %s
		 GROUP BY %s
		HAVING COUNT(*) >= $%d
		 ORDER BY count DESC, value ASC
		 LIMIT 2000`, groupExpr, whereClause, groupExpr, minArg), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceDimensionValue
	for rows.Next() {
		var value *string
		var count int64
		if err := rows.Scan(&value, &count); err != nil {
			return nil, err
		}
		if value == nil || strings.TrimSpace(*value) == "" {
			continue
		}
		out = append(out, SourceDimensionValue{Value: *value, Count: count})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	added, _ := r.addedSourceViewValues(ctx, dimension)
	for i := range out {
		if _, ok := added[out[i].Value]; ok {
			out[i].AlreadyAdded = true
		}
	}
	return out, nil
}

func (r *SourceRepository) ListExposedLibraryViews(ctx context.Context) ([]SourceLibraryView, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, public_uuid::text, name, display_name, dimension, match_value, match_values,
		       collection_type, provider_ids, filter, enabled, expose_to_emby, sort_order,
		       config, cover_image_path, cover_image_tag, created_at, updated_at
		  FROM source_library_views
		 WHERE enabled = TRUE AND expose_to_emby = TRUE
		 ORDER BY sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceLibraryView
	for rows.Next() {
		view, err := scanSourceLibraryView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *view)
	}
	return out, rows.Err()
}

func (r *SourceRepository) CountItemsForLibraryView(ctx context.Context, view SourceLibraryView) (int64, error) {
	where, args, err := sourceViewWhere(view, nil)
	if err != nil {
		return 0, err
	}
	var count int64
	err = r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM source_items si WHERE `+where, args...).Scan(&count)
	return count, err
}

func (r *SourceRepository) ListPosterURLsForLibraryView(ctx context.Context, view SourceLibraryView, limit int64) ([]string, error) {
	where, args, err := sourceViewWhere(view, nil)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 36
	}
	limitIdx := len(args) + 1
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, `
		SELECT poster_url
		  FROM source_items si
		 WHERE `+where+`
		   AND poster_url IS NOT NULL
		   AND poster_url <> ''
		 ORDER BY last_seen_at DESC, id DESC
		 LIMIT $`+fmt.Sprint(limitIdx), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var poster string
		if err := rows.Scan(&poster); err != nil {
			return nil, err
		}
		out = append(out, poster)
	}
	return out, rows.Err()
}

func (r *SourceRepository) ListItemsForLibraryView(ctx context.Context, view SourceLibraryView, opts SourceItemListOptions) ([]SourceItem, int64, error) {
	where, args, err := sourceViewWhere(view, &opts)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM source_items si WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	limitIdx := len(args) + 1
	offsetIdx := len(args) + 2
	args = append(args, opts.Limit, opts.Offset)
	rows, err := r.pool.Query(ctx, `
		SELECT id, public_uuid::text, provider_id, source_item_id, source_parent_id, item_type, title,
		       original_title, sort_title, year, region, area, language, category_name, normalized_kind,
		       season_number, episode_number, poster_url, backdrop_url, remarks, summary, directors, actors,
		       provider_ids, raw, detail_loaded, last_seen_at, created_at, updated_at
		  FROM source_items si
		 WHERE `+where+`
		 ORDER BY COALESCE(sort_title, title), id
		 LIMIT $`+fmt.Sprint(limitIdx)+` OFFSET $`+fmt.Sprint(offsetIdx), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []SourceItem
	for rows.Next() {
		item, err := scanSourceItem(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *item)
	}
	return out, total, rows.Err()
}

func sourceViewWhere(view SourceLibraryView, opts *SourceItemListOptions) (string, []any, error) {
	clauses := []string{"si.item_type IN ('Movie', 'Series')"}
	args := []any{}
	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	switch view.Dimension {
	case "normalized_kind":
		clauses = append(clauses, "si.normalized_kind = "+addArg(view.MatchValue))
	case "region":
		clauses = append(clauses, sourceRegionCondition("si.region", view.MatchValue, addArg))
	case "kind_region":
		kind, region, ok := strings.Cut(view.MatchValue, "/")
		if !ok || strings.TrimSpace(kind) == "" || strings.TrimSpace(region) == "" {
			return "", nil, fmt.Errorf("invalid kind_region match_value: %s", view.MatchValue)
		}
		clauses = append(clauses, "si.normalized_kind = "+addArg(strings.TrimSpace(kind)))
		clauses = append(clauses, sourceRegionCondition("si.region", strings.TrimSpace(region), addArg))
	case "provider":
		clauses = append(clauses, "si.provider_id = "+addArg(view.MatchValue))
	case "custom":
		values := view.MatchValues
		if len(values) == 0 {
			values = []string{view.MatchValue}
		}
		clauses = append(clauses, "(si.normalized_kind = ANY("+addArg(values)+"::text[]) OR si.region = ANY("+fmt.Sprintf("$%d", len(args))+"::text[]))")
	default:
		return "", nil, fmt.Errorf("unknown source view dimension: %s", view.Dimension)
	}

	if len(view.ProviderIDs) > 0 {
		clauses = append(clauses, "si.provider_id = ANY("+addArg(view.ProviderIDs)+"::bigint[])")
	}
	if len(view.Filter) > 0 && json.Valid(view.Filter) {
		var filter struct {
			Regions         []string `json:"regions"`
			NormalizedKinds []string `json:"normalized_kinds"`
			Years           []int32  `json:"years"`
			ProviderIDs     []int64  `json:"provider_ids"`
		}
		if err := json.Unmarshal(view.Filter, &filter); err == nil {
			if len(filter.Regions) > 0 {
				clauses = append(clauses, "si.region = ANY("+addArg(filter.Regions)+"::text[])")
			}
			if len(filter.NormalizedKinds) > 0 {
				clauses = append(clauses, "si.normalized_kind = ANY("+addArg(filter.NormalizedKinds)+"::text[])")
			}
			if len(filter.Years) > 0 {
				clauses = append(clauses, "si.year = ANY("+addArg(filter.Years)+"::integer[])")
			}
			if len(filter.ProviderIDs) > 0 {
				clauses = append(clauses, "si.provider_id = ANY("+addArg(filter.ProviderIDs)+"::bigint[])")
			}
		}
	}
	if opts != nil {
		if len(opts.IncludeTypes) > 0 {
			clauses = append(clauses, "si.item_type = ANY("+addArg(opts.IncludeTypes)+"::text[])")
		}
		if strings.TrimSpace(opts.SearchTerm) != "" {
			clauses = append(clauses, "si.title ILIKE "+addArg("%"+strings.TrimSpace(opts.SearchTerm)+"%"))
		}
	}
	return strings.Join(clauses, " AND "), args, nil
}

func sourceDimensionDiscoverSQL(dimension string) (string, string, error) {
	base := "si.item_type IN ('Movie', 'Series')"
	switch dimension {
	case "normalized_kind":
		return "si.normalized_kind", base + " AND si.normalized_kind IS NOT NULL AND si.normalized_kind <> ''", nil
	case "region":
		return "si.region", base + " AND si.region IS NOT NULL AND si.region <> ''", nil
	case "provider":
		return "si.provider_id::text", base, nil
	case "kind_region":
		return "si.normalized_kind || '/' || COALESCE(si.region, 'Foreign')", base + " AND si.normalized_kind IS NOT NULL AND si.normalized_kind <> ''", nil
	case "custom":
		return "COALESCE(si.normalized_kind, si.region)", base, nil
	default:
		return "", "", fmt.Errorf("unknown source view dimension: %s", dimension)
	}
}

func (r *SourceRepository) addedSourceViewValues(ctx context.Context, dimension string) (map[string]struct{}, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT unnest(CASE WHEN cardinality(match_values) > 0 THEN match_values ELSE ARRAY[match_value] END)
		   FROM source_library_views WHERE dimension = $1`, dimension)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err == nil {
			out[value] = struct{}{}
		}
	}
	return out, rows.Err()
}

func sourceRegionCondition(column, matchValue string, addArg func(any) string) string {
	if strings.EqualFold(matchValue, "Foreign") {
		return "(" + column + " IS NULL OR " + column + " <> 'CN')"
	}
	return column + " = " + addArg(matchValue)
}
