package repository

import "context"

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
