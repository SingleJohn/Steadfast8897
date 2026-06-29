package repository

import (
	"context"
	"fmt"
	"strings"
)

func (r *SourceRepository) UpsertProvider(ctx context.Context, in SourceProviderUpsert) (*SourceProvider, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO source_providers (
			config_id, source_key, name, provider_kind, runtime_kind, tvbox_type, api, ext,
			categories, headers, capabilities, timeout_ms, enabled, visible, searchable,
			health_status, last_error, raw_site
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10::jsonb, $11::jsonb,
			$12, $13, $14, $15, $16, $17, $18::jsonb
		)
		ON CONFLICT (config_id, source_key) DO UPDATE SET
			name = EXCLUDED.name,
			provider_kind = EXCLUDED.provider_kind,
			runtime_kind = EXCLUDED.runtime_kind,
			tvbox_type = EXCLUDED.tvbox_type,
			api = EXCLUDED.api,
			ext = EXCLUDED.ext,
			categories = EXCLUDED.categories,
			headers = EXCLUDED.headers,
			capabilities = EXCLUDED.capabilities,
			timeout_ms = EXCLUDED.timeout_ms,
			enabled = EXCLUDED.enabled,
			visible = EXCLUDED.visible,
			searchable = EXCLUDED.searchable,
			health_status = EXCLUDED.health_status,
			last_error = EXCLUDED.last_error,
			raw_site = EXCLUDED.raw_site,
			updated_at = NOW()
		RETURNING id, config_id, source_key, name, provider_kind, runtime_kind, tvbox_type, api,
		          ext, categories, headers, capabilities, timeout_ms, enabled, visible, searchable,
		          health_status, last_check_at, last_error, raw_site, created_at, updated_at`,
		in.ConfigID, in.SourceKey, in.Name, in.ProviderKind, in.RuntimeKind, in.TVBoxType, in.API,
		jsonBytesOrObject(in.Ext), jsonBytesOrArray(in.Categories), jsonBytesOrObject(in.Headers),
		jsonBytesOrObject(in.Capabilities), defaultInt32(in.TimeoutMS, 8000), in.Enabled, in.Visible,
		in.Searchable, defaultString(in.HealthStatus, "unknown"), in.LastError, jsonBytesOrObject(in.RawSite))
	return scanSourceProvider(row)
}

func (r *SourceRepository) UpsertProviderBySourceKey(ctx context.Context, in SourceProviderUpsert) (*SourceProvider, error) {
	if strings.TrimSpace(in.SourceKey) == "" {
		return nil, fmt.Errorf("source_key is required")
	}
	existing, err := r.GetProviderBySourceKey(ctx, in.SourceKey)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return r.UpsertProvider(ctx, in)
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE source_providers
		   SET config_id = $2,
		       name = $3,
		       provider_kind = $4,
		       runtime_kind = $5,
		       tvbox_type = $6,
		       api = $7,
		       ext = $8::jsonb,
		       categories = $9::jsonb,
		       headers = $10::jsonb,
		       capabilities = $11::jsonb,
		       timeout_ms = $12,
		       enabled = $13,
		       visible = $14,
		       searchable = $15,
		       health_status = $16,
		       last_error = $17,
		       raw_site = $18::jsonb,
		       updated_at = NOW()
		 WHERE id = $1
		RETURNING id, config_id, source_key, name, provider_kind, runtime_kind, tvbox_type, api,
		          ext, categories, headers, capabilities, timeout_ms, enabled, visible, searchable,
		          health_status, last_check_at, last_error, raw_site, created_at, updated_at`,
		existing.ID, in.ConfigID, in.Name, in.ProviderKind, in.RuntimeKind, in.TVBoxType, in.API,
		jsonBytesOrObject(in.Ext), jsonBytesOrArray(in.Categories), jsonBytesOrObject(in.Headers),
		jsonBytesOrObject(in.Capabilities), defaultInt32(in.TimeoutMS, 8000), in.Enabled, in.Visible,
		in.Searchable, defaultString(in.HealthStatus, "unknown"), in.LastError, jsonBytesOrObject(in.RawSite))
	return scanSourceProvider(row)
}

func (r *SourceRepository) GetProviderByID(ctx context.Context, id int64) (*SourceProvider, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, config_id, source_key, name, provider_kind, runtime_kind, tvbox_type, api,
		       ext, categories, headers, capabilities, timeout_ms, enabled, visible, searchable,
		       health_status, last_check_at, last_error, raw_site, created_at, updated_at
		  FROM source_providers
		 WHERE id = $1`, id)
	return scanSourceProvider(row)
}

func (r *SourceRepository) GetProviderBySourceKey(ctx context.Context, sourceKey string) (*SourceProvider, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, config_id, source_key, name, provider_kind, runtime_kind, tvbox_type, api,
		       ext, categories, headers, capabilities, timeout_ms, enabled, visible, searchable,
		       health_status, last_check_at, last_error, raw_site, created_at, updated_at
		  FROM source_providers
		 WHERE source_key = $1
		 ORDER BY updated_at DESC, id DESC
		 LIMIT 1`, strings.TrimSpace(sourceKey))
	return scanSourceProvider(row)
}

func (r *SourceRepository) ListProviders(ctx context.Context, opts SourceProviderListOptions) ([]SourceProvider, error) {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	if opts.Limit > 500 {
		opts.Limit = 500
	}
	clauses := []string{"TRUE"}
	args := []any{}
	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if opts.ConfigID != nil {
		clauses = append(clauses, "sp.config_id = "+addArg(*opts.ConfigID))
	}
	if opts.Enabled != nil {
		clauses = append(clauses, "sp.enabled = "+addArg(*opts.Enabled))
	}
	if opts.Visible != nil {
		clauses = append(clauses, "sp.visible = "+addArg(*opts.Visible))
	}
	if value := strings.TrimSpace(opts.HealthStatus); value != "" {
		clauses = append(clauses, "sp.health_status = "+addArg(value))
	}
	if value := strings.TrimSpace(opts.RuntimeStatus); value != "" {
		clauses = append(clauses, "sp.capabilities #>> '{health,runtime_status}' = "+addArg(value))
	}
	if value := strings.TrimSpace(opts.HomeStatus); value != "" {
		clauses = append(clauses, "sp.capabilities #>> '{health,home_status}' = "+addArg(value))
	}
	if value := strings.TrimSpace(opts.CategoryStatus); value != "" {
		clauses = append(clauses, "sp.capabilities #>> '{health,category_status}' = "+addArg(value))
	}
	if value := strings.TrimSpace(opts.RuntimeKind); value != "" {
		clauses = append(clauses, "sp.runtime_kind = "+addArg(value))
	}
	if value := strings.TrimSpace(opts.ProviderKind); value != "" {
		clauses = append(clauses, "sp.provider_kind = "+addArg(value))
	}
	if value := strings.TrimSpace(opts.Keyword); value != "" {
		keywordArg := addArg("%" + value + "%")
		clauses = append(clauses, "(sp.name ILIKE "+keywordArg+" OR sp.source_key ILIKE "+keywordArg+" OR sp.api ILIKE "+keywordArg+")")
	}
	if opts.OnlyUsable {
		clauses = append(clauses, "sp.enabled = TRUE", "COALESCE(sci.enabled, TRUE) = TRUE", "COALESCE(sci.import_status, 'active') = 'active'")
	}
	limitArg := addArg(opts.Limit)
	offsetArg := addArg(opts.Offset)
	rows, err := r.pool.Query(ctx, `
		SELECT sp.id, sp.config_id, sp.source_key, sp.name, sp.provider_kind, sp.runtime_kind, sp.tvbox_type, sp.api,
		       sp.ext, sp.categories, sp.headers, sp.capabilities, sp.timeout_ms, sp.enabled, sp.visible, sp.searchable,
		       sp.health_status, sp.last_check_at, sp.last_error, sp.raw_site, sp.created_at, sp.updated_at
		  FROM source_providers sp
		  LEFT JOIN source_config_imports sci ON sci.id = sp.config_id
		 WHERE `+strings.Join(clauses, " AND ")+`
		 ORDER BY sp.updated_at DESC, sp.id DESC
		 LIMIT `+limitArg+` OFFSET `+offsetArg, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceProvider
	for rows.Next() {
		provider, err := scanSourceProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *provider)
	}
	return out, rows.Err()
}

func (r *SourceRepository) SetProvidersEnabled(ctx context.Context, ids []int64, enabled bool) ([]SourceProvider, error) {
	ids = compactInt64s(ids)
	if len(ids) == 0 {
		return []SourceProvider{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		UPDATE source_providers
		   SET enabled = $2,
		       updated_at = NOW()
		 WHERE id = ANY($1::bigint[])
		RETURNING id, config_id, source_key, name, provider_kind, runtime_kind, tvbox_type, api,
		          ext, categories, headers, capabilities, timeout_ms, enabled, visible, searchable,
		          health_status, last_check_at, last_error, raw_site, created_at, updated_at`, ids, enabled)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SourceProvider{}
	for rows.Next() {
		provider, err := scanSourceProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *provider)
	}
	return out, rows.Err()
}

func (r *SourceRepository) SetProviderEnabled(ctx context.Context, id int64, enabled bool) (*SourceProvider, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE source_providers
		   SET enabled = $2,
		       updated_at = NOW()
		 WHERE id = $1
		RETURNING id, config_id, source_key, name, provider_kind, runtime_kind, tvbox_type, api,
		          ext, categories, headers, capabilities, timeout_ms, enabled, visible, searchable,
		          health_status, last_check_at, last_error, raw_site, created_at, updated_at`, id, enabled)
	return scanSourceProvider(row)
}

func (r *SourceRepository) RecordProviderAutoDisableFailure(ctx context.Context, id int64, scope, errorType, message string, threshold int) (*SourceProvider, int, bool, error) {
	if threshold <= 0 {
		threshold = 3
	}
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "unknown"
	}
	errorType = strings.TrimSpace(errorType)
	message = strings.TrimSpace(message)
	row := r.pool.QueryRow(ctx, `
		WITH current AS (
			SELECT COALESCE(capabilities, '{}'::jsonb) AS capabilities
			  FROM source_providers
			 WHERE id = $1
			 FOR UPDATE
		),
		next AS (
			SELECT capabilities,
			       COALESCE((capabilities #>> ARRAY['auto_disable', $2, 'failure_count'])::int, 0) + 1 AS failure_count
			  FROM current
		),
		updated AS (
			UPDATE source_providers sp
			   SET enabled = CASE WHEN next.failure_count >= $5 THEN FALSE ELSE sp.enabled END,
			       health_status = CASE WHEN next.failure_count >= $5 THEN 'unhealthy' ELSE sp.health_status END,
			       last_error = CASE WHEN next.failure_count >= $5 THEN $4 ELSE sp.last_error END,
			       capabilities = jsonb_set(
			         jsonb_set(
			           jsonb_set(
			             jsonb_set(COALESCE(sp.capabilities, '{}'::jsonb), ARRAY['auto_disable', $2, 'failure_count'], to_jsonb(next.failure_count), true),
			             ARRAY['auto_disable', $2, 'last_error_type'], to_jsonb($3::text), true
			           ),
			           ARRAY['auto_disable', $2, 'last_error'], to_jsonb($4::text), true
			         ),
			         ARRAY['auto_disable', $2, 'last_failure_at'], to_jsonb(NOW()::text), true
			       ),
			       updated_at = NOW()
			  FROM next
			 WHERE sp.id = $1
			RETURNING sp.id, sp.config_id, sp.source_key, sp.name, sp.provider_kind, sp.runtime_kind, sp.tvbox_type, sp.api,
			          sp.ext, sp.categories, sp.headers, sp.capabilities, sp.timeout_ms, sp.enabled, sp.visible, sp.searchable,
			          sp.health_status, sp.last_check_at, sp.last_error, sp.raw_site, sp.created_at, sp.updated_at,
			          next.failure_count,
			          next.failure_count >= $5 AS disabled
		)
		SELECT * FROM updated`, id, scope, errorType, message, threshold)
	provider, count, disabled, err := scanSourceProviderAutoDisable(row)
	return provider, count, disabled, err
}

func (r *SourceRepository) ResetProviderAutoDisableFailure(ctx context.Context, id int64, scope string) error {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return nil
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE source_providers
		   SET capabilities = jsonb_set(
		         COALESCE(capabilities, '{}'::jsonb),
		         ARRAY['auto_disable', $2, 'failure_count'],
		         '0'::jsonb,
		         true
		       ),
		       updated_at = NOW()
		 WHERE id = $1`, id, scope)
	return err
}

func (r *SourceRepository) DeleteProvidersCascade(ctx context.Context, ids []int64) (*SourceProviderDeleteResult, error) {
	ids = compactInt64s(ids)
	if len(ids) == 0 {
		return &SourceProviderDeleteResult{
			Providers: []SourceProvider{},
			Impact: SourceProviderDeleteImpact{
				ProviderIDs:                []int64{},
				RuntimeInvocationsRetained: true,
			},
		}, nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	providers, err := lockSourceProviders(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	actualIDs := make([]int64, 0, len(providers))
	for _, provider := range providers {
		actualIDs = append(actualIDs, provider.ID)
	}
	impact, err := buildSourceProviderDeleteImpact(ctx, tx, actualIDs)
	if err != nil {
		return nil, err
	}
	if len(actualIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE source_library_views
			   SET provider_ids = ARRAY(
			         SELECT pid
			           FROM unnest(provider_ids) AS pid
			          WHERE NOT (pid = ANY($1::bigint[]))
			       ),
			       updated_at = NOW()
			 WHERE provider_ids && $1::bigint[]`, actualIDs); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM source_providers WHERE id = ANY($1::bigint[])`, actualIDs); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &SourceProviderDeleteResult{Providers: providers, Impact: *impact}, nil
}

func lockSourceProviders(ctx context.Context, q sourceConfigQuerier, ids []int64) ([]SourceProvider, error) {
	rows, err := q.Query(ctx, `
		SELECT id, config_id, source_key, name, provider_kind, runtime_kind, tvbox_type, api,
		       ext, categories, headers, capabilities, timeout_ms, enabled, visible, searchable,
		       health_status, last_check_at, last_error, raw_site, created_at, updated_at
		  FROM source_providers
		 WHERE id = ANY($1::bigint[])
		 ORDER BY updated_at DESC, id DESC
		 FOR UPDATE`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SourceProvider{}
	for rows.Next() {
		provider, err := scanSourceProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *provider)
	}
	return out, rows.Err()
}

func buildSourceProviderDeleteImpact(ctx context.Context, q sourceConfigQuerier, providerIDs []int64) (*SourceProviderDeleteImpact, error) {
	impact := &SourceProviderDeleteImpact{
		ProviderIDs:                providerIDs,
		RuntimeInvocationsRetained: true,
	}
	if len(providerIDs) == 0 {
		return impact, nil
	}
	var err error
	if err = q.QueryRow(ctx, `SELECT COUNT(*) FROM source_providers WHERE id = ANY($1::bigint[])`, providerIDs).Scan(&impact.ProviderCount); err != nil {
		return nil, err
	}
	if err = q.QueryRow(ctx, `SELECT COUNT(*) FROM source_items WHERE provider_id = ANY($1::bigint[])`, providerIDs).Scan(&impact.SourceItemCount); err != nil {
		return nil, err
	}
	if err = q.QueryRow(ctx, `SELECT COUNT(*) FROM source_play_sources WHERE provider_id = ANY($1::bigint[])`, providerIDs).Scan(&impact.PlaySourceCount); err != nil {
		return nil, err
	}
	if err = q.QueryRow(ctx, `SELECT COUNT(*) FROM source_runtime_artifacts WHERE provider_id = ANY($1::bigint[])`, providerIDs).Scan(&impact.RuntimeArtifactCount); err != nil {
		return nil, err
	}
	if err = q.QueryRow(ctx, `SELECT COUNT(*) FROM source_runtime_invocations WHERE provider_id = ANY($1::bigint[])`, providerIDs).Scan(&impact.RuntimeInvocationCount); err != nil {
		return nil, err
	}
	views, err := listConfigImpactLibraryViews(ctx, q, providerIDs)
	if err != nil {
		return nil, err
	}
	impact.AffectedLibraryViews = views
	impact.AffectedLibraryViewCount = int64(len(views))
	return impact, nil
}

func (r *SourceRepository) UpdateProviderHealth(ctx context.Context, id int64, status string, lastError *string, categories []byte) (*SourceProvider, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE source_providers
		   SET health_status = $2,
		       last_check_at = NOW(),
		       last_error = $3,
		       categories = CASE WHEN $4::jsonb = 'null'::jsonb THEN categories ELSE $4::jsonb END,
		       updated_at = NOW()
		 WHERE id = $1
		RETURNING id, config_id, source_key, name, provider_kind, runtime_kind, tvbox_type, api,
		          ext, categories, headers, capabilities, timeout_ms, enabled, visible, searchable,
		          health_status, last_check_at, last_error, raw_site, created_at, updated_at`,
		id, defaultString(status, "unknown"), lastError, jsonBytesOrNull(categories))
	return scanSourceProvider(row)
}

func (r *SourceRepository) UpdateProviderHealthSummary(ctx context.Context, id int64, status string, lastError *string, categories []byte, summary []byte) (*SourceProvider, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE source_providers
		   SET health_status = $2,
		       last_check_at = NOW(),
		       last_error = $3,
		       categories = CASE WHEN $4::jsonb = 'null'::jsonb THEN categories ELSE $4::jsonb END,
		       capabilities = CASE
		         WHEN $5::jsonb = 'null'::jsonb THEN capabilities
		         ELSE jsonb_set(COALESCE(capabilities, '{}'::jsonb), '{health}', $5::jsonb, true)
		       END,
		       updated_at = NOW()
		 WHERE id = $1
		RETURNING id, config_id, source_key, name, provider_kind, runtime_kind, tvbox_type, api,
		          ext, categories, headers, capabilities, timeout_ms, enabled, visible, searchable,
		          health_status, last_check_at, last_error, raw_site, created_at, updated_at`,
		id, defaultString(status, "unknown"), lastError, jsonBytesOrNull(categories), jsonBytesOrNull(summary))
	return scanSourceProvider(row)
}
