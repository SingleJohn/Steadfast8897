package repository

import (
	"context"
	"fmt"
	"strings"
)

func (r *SourceRepository) UpsertSourceItem(ctx context.Context, in SourceItemUpsert) (*SourceItem, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO source_items (
			public_uuid, provider_id, source_item_id, source_parent_id, item_type, title,
			original_title, sort_title, year, region, area, language, category_name,
			normalized_kind, season_number, episode_number, poster_url, backdrop_url,
			remarks, summary, directors, actors, provider_ids, raw, detail_loaded
		) VALUES (
			$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18, $19, $20, COALESCE($21, '{}'::text[]), COALESCE($22, '{}'::text[]), $23::jsonb, $24::jsonb, $25
		)
		ON CONFLICT (public_uuid) DO UPDATE SET
			provider_id = EXCLUDED.provider_id,
			source_item_id = EXCLUDED.source_item_id,
			source_parent_id = EXCLUDED.source_parent_id,
			item_type = EXCLUDED.item_type,
			title = EXCLUDED.title,
			original_title = EXCLUDED.original_title,
			sort_title = EXCLUDED.sort_title,
			year = EXCLUDED.year,
			region = EXCLUDED.region,
			area = EXCLUDED.area,
			language = EXCLUDED.language,
			category_name = EXCLUDED.category_name,
			normalized_kind = EXCLUDED.normalized_kind,
			season_number = EXCLUDED.season_number,
			episode_number = EXCLUDED.episode_number,
			poster_url = EXCLUDED.poster_url,
			backdrop_url = EXCLUDED.backdrop_url,
			remarks = EXCLUDED.remarks,
			summary = EXCLUDED.summary,
			directors = EXCLUDED.directors,
			actors = EXCLUDED.actors,
			provider_ids = EXCLUDED.provider_ids,
			raw = EXCLUDED.raw,
			detail_loaded = EXCLUDED.detail_loaded,
			last_seen_at = NOW(),
			updated_at = NOW()
		RETURNING id, public_uuid::text, provider_id, source_item_id, source_parent_id, item_type, title,
		          original_title, sort_title, year, region, area, language, category_name, normalized_kind,
		          season_number, episode_number, poster_url, backdrop_url, remarks, summary, directors, actors,
		          provider_ids, raw, detail_loaded, last_seen_at, created_at, updated_at`,
		in.PublicUUID, in.ProviderID, in.SourceItemID, in.SourceParentID, defaultString(in.ItemType, "unknown"),
		in.Title, in.OriginalTitle, in.SortTitle, in.Year, in.Region, in.Area, in.Language, in.CategoryName,
		defaultString(in.NormalizedKind, "unknown"), in.SeasonNumber, in.EpisodeNumber, in.PosterURL,
		in.BackdropURL, in.Remarks, in.Summary, nonNilStrings(in.Directors), nonNilStrings(in.Actors), jsonBytesOrObject(in.ProviderIDs),
		jsonBytesOrObject(in.Raw), in.DetailLoaded)
	return scanSourceItem(row)
}

func (r *SourceRepository) GetSourceItemByID(ctx context.Context, id int64) (*SourceItem, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, public_uuid::text, provider_id, source_item_id, source_parent_id, item_type, title,
		       original_title, sort_title, year, region, area, language, category_name, normalized_kind,
		       season_number, episode_number, poster_url, backdrop_url, remarks, summary, directors, actors,
		       provider_ids, raw, detail_loaded, last_seen_at, created_at, updated_at
		  FROM source_items
		 WHERE id = $1`, id)
	return scanSourceItem(row)
}

func (r *SourceRepository) GetSourceItemByPublicUUID(ctx context.Context, publicUUID string) (*SourceItem, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, public_uuid::text, provider_id, source_item_id, source_parent_id, item_type, title,
		       original_title, sort_title, year, region, area, language, category_name, normalized_kind,
		       season_number, episode_number, poster_url, backdrop_url, remarks, summary, directors, actors,
		       provider_ids, raw, detail_loaded, last_seen_at, created_at, updated_at
		  FROM source_items
		 WHERE public_uuid = $1::uuid`, publicUUID)
	return scanSourceItem(row)
}

func (r *SourceRepository) SearchSourceItems(ctx context.Context, opts SourceItemSearchOptions) ([]SourceItem, int64, error) {
	where, args := sourceItemSearchWhere(opts, 1)
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM source_items si `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	selectWhere, selectArgs := sourceItemSearchWhere(opts, 2)
	args = append([]any{strings.TrimSpace(opts.SearchTerm)}, selectArgs...)
	limitIdx := len(args) + 1
	offsetIdx := len(args) + 2
	args = append(args, opts.Limit, opts.Offset)
	rows, err := r.pool.Query(ctx, `
		SELECT si.id, si.public_uuid::text, si.provider_id, si.source_item_id, si.source_parent_id, si.item_type, si.title,
		       si.original_title, si.sort_title, si.year, si.region, si.area, si.language, si.category_name, si.normalized_kind,
		       si.season_number, si.episode_number, si.poster_url, si.backdrop_url, si.remarks, si.summary, si.directors, si.actors,
		       si.provider_ids, si.raw, si.detail_loaded, si.last_seen_at, si.created_at, si.updated_at
		  FROM source_items si
		`+selectWhere+`
		 ORDER BY CASE
		            WHEN $1 = '' THEN 0
		            WHEN lower(si.title) = lower($1) OR lower(COALESCE(si.original_title, '')) = lower($1) THEN 100
		            WHEN si.title ILIKE $1 || '%' OR COALESCE(si.original_title, '') ILIKE $1 || '%' THEN 80
		            WHEN si.title ILIKE '%' || $1 || '%' OR COALESCE(si.original_title, '') ILIKE '%' || $1 || '%' THEN 60
		            ELSE 20
		          END DESC,
		          si.last_seen_at DESC,
		          si.id DESC
		 LIMIT $`+fmt.Sprint(limitIdx)+` OFFSET $`+fmt.Sprint(offsetIdx), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]SourceItem, 0)
	for rows.Next() {
		item, err := scanSourceItem(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *item)
	}
	return out, total, rows.Err()
}

func sourceItemSearchWhere(opts SourceItemSearchOptions, firstArg int) (string, []any) {
	search := strings.TrimSpace(opts.SearchTerm)
	args := []any{}
	clauses := []string{
		"si.item_type IN ('Movie', 'Series')",
		"btrim(si.title) <> ''",
		`EXISTS (
			SELECT 1
			  FROM source_providers sp
			  LEFT JOIN source_config_imports sci ON sci.id = sp.config_id
			 WHERE sp.id = si.provider_id
			   AND sp.enabled = TRUE
			   AND sp.searchable = TRUE
			   AND sp.provider_kind = 'cms_vod'
			   AND sp.runtime_kind = 'native_cms'
			   AND (sp.config_id IS NULL OR (sci.enabled = TRUE AND sci.import_status = 'active'))
		)`,
	}
	if search != "" {
		args = append(args, "%"+search+"%")
		searchIdx := firstArg + len(args) - 1
		clauses = append(clauses, fmt.Sprintf("(si.title ILIKE $%d OR si.original_title ILIKE $%d OR si.remarks ILIKE $%d)", searchIdx, searchIdx, searchIdx))
	}
	if len(opts.IncludeTypes) > 0 {
		args = append(args, opts.IncludeTypes)
		typeIdx := firstArg + len(args) - 1
		clauses = append(clauses, fmt.Sprintf("si.item_type = ANY($%d::text[])", typeIdx))
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}
