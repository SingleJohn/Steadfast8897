package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type sourceConfigQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func (r *SourceRepository) UpsertConfigImport(ctx context.Context, in SourceConfigImportUpsert) (*SourceConfigImport, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO source_config_imports (
			source_type, name, source_url, base_url, content_sha256, spider_ref, spider_md5,
			raw_config, import_status, enabled, imported_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11
		)
		ON CONFLICT (source_type, content_sha256) DO UPDATE SET
			name = EXCLUDED.name,
			source_url = EXCLUDED.source_url,
			base_url = EXCLUDED.base_url,
			spider_ref = EXCLUDED.spider_ref,
			spider_md5 = EXCLUDED.spider_md5,
			raw_config = EXCLUDED.raw_config,
			import_status = EXCLUDED.import_status,
			enabled = EXCLUDED.enabled,
			imported_by = EXCLUDED.imported_by,
			updated_at = NOW()
		RETURNING id, source_type, name, source_url, base_url, content_sha256, spider_ref, spider_md5,
		          raw_config, import_status, enabled, imported_by::text, imported_at, updated_at`,
		defaultString(in.SourceType, "tvbox"), in.Name, in.SourceURL, in.BaseURL, in.ContentSHA256,
		in.SpiderRef, in.SpiderMD5, jsonBytesOrObject(in.RawConfig), defaultString(in.ImportStatus, "active"),
		in.Enabled, nullableUUIDText(in.ImportedBy))
	return scanSourceConfigImport(row)
}

func (r *SourceRepository) SupersedeConfigImportsForSourceKeys(ctx context.Context, sourceType string, activeConfigID int64, sourceKeys []string) error {
	keys := compactStrings(sourceKeys)
	if len(keys) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE source_config_imports sci
		   SET import_status = 'superseded',
		       updated_at = NOW()
		  FROM source_providers sp
		 WHERE sp.config_id = sci.id
		   AND sci.source_type = $1
		   AND sci.id <> $2
		   AND sp.source_key = ANY($3::text[])
		   AND sci.import_status <> 'superseded'`,
		defaultString(sourceType, "tvbox"), activeConfigID, keys)
	return err
}

func (r *SourceRepository) ListConfigImports(ctx context.Context, opts SourceConfigListOptions) ([]SourceConfigImport, error) {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, source_type, name, source_url, base_url, content_sha256, spider_ref, spider_md5,
		       raw_config, import_status, enabled, imported_by::text, imported_at, updated_at
		  FROM source_config_imports
		 ORDER BY imported_at DESC, id DESC
		 LIMIT $1 OFFSET $2`, opts.Limit, opts.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceConfigImport
	for rows.Next() {
		item, err := scanSourceConfigImport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (r *SourceRepository) SetConfigEnabled(ctx context.Context, id int64, enabled bool) (*SourceConfigImport, error) {
	status := "disabled"
	if enabled {
		status = "active"
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE source_config_imports
		   SET enabled = $2,
		       import_status = CASE WHEN import_status = 'invalid' THEN import_status ELSE $3 END,
		       updated_at = NOW()
		 WHERE id = $1
		RETURNING id, source_type, name, source_url, base_url, content_sha256, spider_ref, spider_md5,
		          raw_config, import_status, enabled, imported_by::text, imported_at, updated_at`,
		id, enabled, status)
	return scanSourceConfigImport(row)
}

func (r *SourceRepository) GetConfigImportByID(ctx context.Context, id int64) (*SourceConfigImport, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, source_type, name, source_url, base_url, content_sha256, spider_ref, spider_md5,
		       raw_config, import_status, enabled, imported_by::text, imported_at, updated_at
		  FROM source_config_imports
		 WHERE id = $1`, id)
	return scanSourceConfigImport(row)
}

func (r *SourceRepository) GetConfigDetail(ctx context.Context, id int64) (*SourceConfigDetail, error) {
	config, err := r.GetConfigImportByID(ctx, id)
	if err != nil || config == nil {
		return nil, err
	}
	impact, err := r.GetConfigImpact(ctx, id)
	if err != nil {
		return nil, err
	}
	return &SourceConfigDetail{
		Config:        *config,
		ProviderCount: impact.ProviderCount,
		ParserCount:   impact.ParserCount,
		Stats: SourceConfigStats{
			SourceItemCount:          impact.SourceItemCount,
			PlaySourceCount:          impact.PlaySourceCount,
			RuntimeArtifactCount:     impact.RuntimeArtifactCount,
			RuntimeInvocationCount:   impact.RuntimeInvocationCount,
			AffectedLibraryViewCount: impact.AffectedLibraryViewCount,
		},
	}, nil
}

func (r *SourceRepository) GetConfigImpact(ctx context.Context, id int64) (*SourceConfigImpact, error) {
	config, err := r.GetConfigImportByID(ctx, id)
	if err != nil || config == nil {
		return nil, err
	}
	providerIDs, err := listConfigProviderIDs(ctx, r.pool, id)
	if err != nil {
		return nil, err
	}
	return buildSourceConfigImpact(ctx, r.pool, id, providerIDs)
}

func (r *SourceRepository) DeleteConfigCascade(ctx context.Context, id int64) (*SourceConfigDeleteResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	config, err := lockSourceConfig(ctx, tx, id)
	if err != nil || config == nil {
		return nil, err
	}
	providerIDs, err := listConfigProviderIDs(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	impact, err := buildSourceConfigImpact(ctx, tx, id, providerIDs)
	if err != nil {
		return nil, err
	}
	if len(providerIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE source_library_views
			   SET provider_ids = ARRAY(
			         SELECT pid
			           FROM unnest(provider_ids) AS pid
			          WHERE NOT (pid = ANY($1::bigint[]))
			       ),
			       updated_at = NOW()
			 WHERE provider_ids && $1::bigint[]`, providerIDs); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM source_providers WHERE id = ANY($1::bigint[])`, providerIDs); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM source_parsers WHERE config_id = $1`, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM source_config_imports WHERE id = $1`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &SourceConfigDeleteResult{Config: *config, Impact: *impact}, nil
}

func lockSourceConfig(ctx context.Context, q sourceConfigQuerier, id int64) (*SourceConfigImport, error) {
	row := q.QueryRow(ctx, `
		SELECT id, source_type, name, source_url, base_url, content_sha256, spider_ref, spider_md5,
		       raw_config, import_status, enabled, imported_by::text, imported_at, updated_at
		  FROM source_config_imports
		 WHERE id = $1
		 FOR UPDATE`, id)
	return scanSourceConfigImport(row)
}

func listConfigProviderIDs(ctx context.Context, q sourceConfigQuerier, configID int64) ([]int64, error) {
	rows, err := q.Query(ctx, `SELECT id FROM source_providers WHERE config_id = $1 ORDER BY id`, configID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func buildSourceConfigImpact(ctx context.Context, q sourceConfigQuerier, configID int64, providerIDs []int64) (*SourceConfigImpact, error) {
	impact := &SourceConfigImpact{
		ConfigID:                   configID,
		ProviderIDs:                providerIDs,
		RuntimeInvocationsRetained: true,
	}
	var err error
	if err = q.QueryRow(ctx, `SELECT COUNT(*) FROM source_providers WHERE config_id = $1`, configID).Scan(&impact.ProviderCount); err != nil {
		return nil, err
	}
	if err = q.QueryRow(ctx, `SELECT COUNT(*) FROM source_parsers WHERE config_id = $1`, configID).Scan(&impact.ParserCount); err != nil {
		return nil, err
	}
	if len(providerIDs) == 0 {
		return impact, nil
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

func listConfigImpactLibraryViews(ctx context.Context, q sourceConfigQuerier, providerIDs []int64) ([]SourceConfigImpactLibraryView, error) {
	rows, err := q.Query(ctx, `
		SELECT id, name, display_name, provider_ids,
		       ARRAY(
		         SELECT pid
		           FROM unnest(provider_ids) AS pid
		          WHERE pid = ANY($1::bigint[])
		       ) AS removed_provider_ids
		  FROM source_library_views
		 WHERE provider_ids && $1::bigint[]
		 ORDER BY sort_order, name`, providerIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SourceConfigImpactLibraryView{}
	for rows.Next() {
		var item SourceConfigImpactLibraryView
		if err := rows.Scan(&item.ID, &item.Name, &item.DisplayName, &item.ProviderIDs, &item.RemovedProviderIDs); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
