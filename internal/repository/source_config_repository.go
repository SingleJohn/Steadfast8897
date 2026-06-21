package repository

import "context"

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
