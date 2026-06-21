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
	clauses := []string{"TRUE"}
	args := []any{}
	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if opts.ConfigID != nil {
		clauses = append(clauses, "sp.config_id = "+addArg(*opts.ConfigID))
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
