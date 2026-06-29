package repository

import "context"

func (r *SourceRepository) UpsertPlaySource(ctx context.Context, in SourcePlaySourceUpsert) (*SourcePlaySource, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO source_play_sources (
			public_uuid, source_item_id, provider_id, line_name, episode_title, episode_key,
			episode_number, raw_url, parse_mode, flag, headers, resolver_payload, sort_order
		) VALUES (
			$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12::jsonb, $13
		)
		ON CONFLICT (public_uuid) DO UPDATE SET
			source_item_id = EXCLUDED.source_item_id,
			provider_id = EXCLUDED.provider_id,
			line_name = EXCLUDED.line_name,
			episode_title = EXCLUDED.episode_title,
			episode_key = EXCLUDED.episode_key,
			episode_number = EXCLUDED.episode_number,
			raw_url = EXCLUDED.raw_url,
			parse_mode = EXCLUDED.parse_mode,
			flag = EXCLUDED.flag,
			headers = EXCLUDED.headers,
			resolver_payload = EXCLUDED.resolver_payload,
			sort_order = EXCLUDED.sort_order,
			updated_at = NOW()
		RETURNING id, public_uuid::text, source_item_id, provider_id, line_name, episode_title, episode_key,
		          episode_number, raw_url, parse_mode, flag, headers, resolver_payload, sort_order,
		          health_status, success_count, failure_count, avg_latency_ms,
		          last_success_at, last_failure_at, created_at, updated_at`,
		in.PublicUUID, in.SourceItemID, in.ProviderID, in.LineName, in.EpisodeTitle, in.EpisodeKey,
		in.EpisodeNumber, in.RawURL, defaultString(in.ParseMode, "unknown"), in.Flag,
		jsonBytesOrObject(in.Headers), jsonBytesOrObject(in.ResolverPayload), in.SortOrder)
	return scanSourcePlaySource(row)
}

func (r *SourceRepository) UpsertUserItemData(ctx context.Context, in SourceUserItemDataUpsert) (*SourceUserItemData, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO source_user_item_data (
			user_id, source_item_id, playback_position_ticks, play_count,
			is_favorite, played, last_played_date
		) VALUES (
			$1::uuid, $2, COALESCE($3, 0), COALESCE($4, 0), COALESCE($5, FALSE), COALESCE($6, FALSE), $7
		)
		ON CONFLICT (user_id, source_item_id) DO UPDATE SET
			playback_position_ticks = COALESCE($3, source_user_item_data.playback_position_ticks),
			play_count = COALESCE($4, source_user_item_data.play_count),
			is_favorite = COALESCE($5, source_user_item_data.is_favorite),
			played = COALESCE($6, source_user_item_data.played),
			last_played_date = COALESCE($7, source_user_item_data.last_played_date),
			updated_at = NOW()
		RETURNING user_id::text, source_item_id, playback_position_ticks, play_count,
		          is_favorite, played, last_played_date, updated_at`,
		in.UserID, in.SourceItemID, in.PlaybackPositionTicks, in.PlayCount,
		in.IsFavorite, in.Played, in.LastPlayedDate)
	return scanSourceUserItemData(row)
}

func (r *SourceRepository) GetPlaySourceByID(ctx context.Context, id int64) (*SourcePlaySource, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, public_uuid::text, source_item_id, provider_id, line_name, episode_title, episode_key,
		       episode_number, raw_url, parse_mode, flag, headers, resolver_payload, sort_order,
		       health_status, success_count, failure_count, avg_latency_ms,
		       last_success_at, last_failure_at, created_at, updated_at
		  FROM source_play_sources
		 WHERE id = $1`, id)
	return scanSourcePlaySource(row)
}

func (r *SourceRepository) GetPlaySourceByPublicUUID(ctx context.Context, publicUUID string) (*SourcePlaySource, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, public_uuid::text, source_item_id, provider_id, line_name, episode_title, episode_key,
		       episode_number, raw_url, parse_mode, flag, headers, resolver_payload, sort_order,
		       health_status, success_count, failure_count, avg_latency_ms,
		       last_success_at, last_failure_at, created_at, updated_at
		  FROM source_play_sources
		 WHERE public_uuid = $1::uuid`, publicUUID)
	return scanSourcePlaySource(row)
}

func (r *SourceRepository) ListPlayableAlternatives(ctx context.Context, sourceItemID int64, episodeKey string) ([]SourcePlaySource, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, public_uuid::text, source_item_id, provider_id, line_name, episode_title, episode_key,
		       episode_number, raw_url, parse_mode, flag, headers, resolver_payload, sort_order,
		       health_status, success_count, failure_count, avg_latency_ms,
		       last_success_at, last_failure_at, created_at, updated_at
		  FROM source_play_sources
		 WHERE source_item_id = $1
		   AND COALESCE(NULLIF(episode_key, ''), episode_title) = $2
		 ORDER BY CASE health_status
		            WHEN 'ok' THEN 0
		            WHEN 'unknown' THEN 1
		            WHEN 'error' THEN 2
		            WHEN 'unhealthy' THEN 3
		            ELSE 4
		          END,
		          CASE WHEN lower(COALESCE(parse_mode, '')) IN ('', 'unknown', 'direct') THEN 0 ELSE 1 END,
		          (success_count::float / GREATEST(success_count + failure_count, 1)) DESC,
		          avg_latency_ms NULLS LAST,
		          sort_order, line_name, id`, sourceItemID, episodeKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourcePlaySource
	for rows.Next() {
		ps, err := scanSourcePlaySource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ps)
	}
	return out, rows.Err()
}

func (r *SourceRepository) ListPlayableAlternativesForItems(ctx context.Context, sourceItemIDs []int64, episodeKey string) ([]SourcePlaySource, error) {
	if len(sourceItemIDs) == 0 {
		return []SourcePlaySource{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, public_uuid::text, source_item_id, provider_id, line_name, episode_title, episode_key,
		       episode_number, raw_url, parse_mode, flag, headers, resolver_payload, sort_order,
		       health_status, success_count, failure_count, avg_latency_ms,
		       last_success_at, last_failure_at, created_at, updated_at
		  FROM source_play_sources
		 WHERE source_item_id = ANY($1::bigint[])
		   AND COALESCE(NULLIF(episode_key, ''), episode_title) = $2
		 ORDER BY array_position($1::bigint[], source_item_id),
		          CASE health_status
		            WHEN 'ok' THEN 0
		            WHEN 'unknown' THEN 1
		            WHEN 'error' THEN 2
		            WHEN 'unhealthy' THEN 3
		            ELSE 4
		          END,
		          CASE WHEN lower(COALESCE(parse_mode, '')) IN ('', 'unknown', 'direct') THEN 0 ELSE 1 END,
		          (success_count::float / GREATEST(success_count + failure_count, 1)) DESC,
		          avg_latency_ms NULLS LAST,
		          sort_order, line_name, id`, sourceItemIDs, episodeKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourcePlaySource
	for rows.Next() {
		ps, err := scanSourcePlaySource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ps)
	}
	return out, rows.Err()
}

func (r *SourceRepository) ListEpisodesForSeries(ctx context.Context, sourceItemID int64) ([]SourceEpisode, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT si.id, si.public_uuid::text, si.provider_id, si.title, si.summary, si.poster_url, si.backdrop_url,
		       COALESCE(NULLIF(sps.episode_key, ''), sps.episode_title, sps.id::text) AS episode_key,
		       COALESCE(NULLIF(sps.episode_title, ''), NULLIF(sps.episode_key, ''), sps.line_name) AS episode_title,
		       MIN(sps.episode_number) AS episode_number,
		       COUNT(*) AS line_count,
		       MIN(sps.created_at) AS first_seen_at
		  FROM source_items si
		  JOIN source_play_sources sps ON sps.source_item_id = si.id
		 WHERE si.id = $1
		 GROUP BY si.id, si.public_uuid, si.provider_id, si.title, si.summary, si.poster_url, si.backdrop_url,
		          COALESCE(NULLIF(sps.episode_key, ''), sps.episode_title, sps.id::text),
		          COALESCE(NULLIF(sps.episode_title, ''), NULLIF(sps.episode_key, ''), sps.line_name)
		 ORDER BY MIN(sps.sort_order), MIN(sps.episode_number) NULLS LAST, episode_key`, sourceItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceEpisode
	for rows.Next() {
		var ep SourceEpisode
		if err := rows.Scan(&ep.SourceItemID, &ep.SourceItemUUID, &ep.ProviderID, &ep.SeriesTitle,
			&ep.SeriesSummary, &ep.PosterURL, &ep.BackdropURL, &ep.EpisodeKey, &ep.EpisodeTitle,
			&ep.EpisodeNumber, &ep.LineCount, &ep.FirstSeenAt); err != nil {
			return nil, err
		}
		out = append(out, ep)
	}
	return out, rows.Err()
}

func (r *SourceRepository) GetEpisodeForSeries(ctx context.Context, sourceItemID int64, episodeKey string) (*SourceEpisode, error) {
	episodes, err := r.ListEpisodesForSeries(ctx, sourceItemID)
	if err != nil {
		return nil, err
	}
	for i := range episodes {
		if episodes[i].EpisodeKey == episodeKey {
			return &episodes[i], nil
		}
	}
	return nil, nil
}

func (r *SourceRepository) GetUserItemData(ctx context.Context, userID string, sourceItemID int64) (*SourceUserItemData, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT user_id::text, source_item_id, playback_position_ticks, play_count,
		       is_favorite, played, last_played_date, updated_at
		  FROM source_user_item_data
		 WHERE user_id = $1::uuid AND source_item_id = $2`, userID, sourceItemID)
	return scanSourceUserItemData(row)
}

func (r *SourceRepository) ListPlaySourcesForItem(ctx context.Context, sourceItemID int64) ([]SourcePlaySource, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, public_uuid::text, source_item_id, provider_id, line_name, episode_title, episode_key,
		       episode_number, raw_url, parse_mode, flag, headers, resolver_payload, sort_order,
		       health_status, success_count, failure_count, avg_latency_ms,
		       last_success_at, last_failure_at, created_at, updated_at
		  FROM source_play_sources
		 WHERE source_item_id = $1
		 ORDER BY CASE health_status
		            WHEN 'ok' THEN 0
		            WHEN 'unknown' THEN 1
		            WHEN 'error' THEN 2
		            WHEN 'unhealthy' THEN 3
		            ELSE 4
		          END,
		          CASE WHEN lower(COALESCE(parse_mode, '')) IN ('', 'unknown', 'direct') THEN 0 ELSE 1 END,
		          (success_count::float / GREATEST(success_count + failure_count, 1)) DESC,
		          avg_latency_ms NULLS LAST,
		          sort_order, line_name, episode_number NULLS LAST, episode_key`, sourceItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourcePlaySource
	for rows.Next() {
		ps, err := scanSourcePlaySource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ps)
	}
	return out, rows.Err()
}

func (r *SourceRepository) MarkPlaySourceSuccess(ctx context.Context, id int64, latencyMS int64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE source_play_sources
		   SET health_status = 'ok',
		       success_count = success_count + 1,
		       avg_latency_ms = CASE
		         WHEN avg_latency_ms IS NULL THEN $2
		         ELSE ((avg_latency_ms + $2) / 2)::integer
		       END,
		       last_success_at = NOW(),
		       updated_at = NOW()
		 WHERE id = $1`, id, latencyMS)
	return err
}

func (r *SourceRepository) MarkPlaySourceFailure(ctx context.Context, id int64, latencyMS int64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE source_play_sources
		   SET health_status = CASE WHEN failure_count + 1 >= 3 THEN 'unhealthy' ELSE 'error' END,
		       failure_count = failure_count + 1,
		       avg_latency_ms = CASE
		         WHEN avg_latency_ms IS NULL THEN $2
		         ELSE ((avg_latency_ms + $2) / 2)::integer
		       END,
		       last_failure_at = NOW(),
		       updated_at = NOW()
		 WHERE id = $1`, id, latencyMS)
	return err
}
