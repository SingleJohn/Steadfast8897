package repository

import (
	"context"
	"encoding/json"
	"fmt"
)

func (r *SourceRepository) UpsertRuntimeArtifact(ctx context.Context, in SourceRuntimeArtifactUpsert) (*SourceRuntimeArtifact, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("source repository 未初始化")
	}
	raw := in.Raw
	if len(raw) == 0 || !json.Valid(raw) {
		raw = []byte("{}")
	}
	status := in.Status
	if status == "" {
		status = "active"
	}
	trust := in.TrustStatus
	if trust == "" {
		trust = "unverified"
	}
	sourceType := in.SourceType
	if sourceType == "" {
		sourceType = "tvbox"
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO source_runtime_artifacts (
			provider_id, source_type, artifact_kind, name, source_url, base_url, relative_path,
			local_path, md5, sha256, byte_size, content_type, trust_status, status, last_error, raw
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (artifact_kind, sha256) DO UPDATE SET
			provider_id = COALESCE(EXCLUDED.provider_id, source_runtime_artifacts.provider_id),
			source_type = EXCLUDED.source_type,
			name = EXCLUDED.name,
			source_url = EXCLUDED.source_url,
			base_url = EXCLUDED.base_url,
			relative_path = EXCLUDED.relative_path,
			local_path = EXCLUDED.local_path,
			md5 = EXCLUDED.md5,
			byte_size = EXCLUDED.byte_size,
			content_type = EXCLUDED.content_type,
			trust_status = CASE
				WHEN source_runtime_artifacts.trust_status = 'trusted' THEN source_runtime_artifacts.trust_status
				ELSE EXCLUDED.trust_status
			END,
			status = EXCLUDED.status,
			last_fetched_at = NOW(),
			last_error = EXCLUDED.last_error,
			raw = EXCLUDED.raw,
			updated_at = NOW()
		RETURNING id, provider_id, source_type, artifact_kind, name, source_url, base_url, relative_path,
			local_path, md5, sha256, byte_size, content_type, trust_status, status, last_fetched_at,
			verified_at, last_error, raw, created_at, updated_at
	`, in.ProviderID, sourceType, in.ArtifactKind, in.Name, in.SourceURL, in.BaseURL, in.RelativePath,
		in.LocalPath, in.MD5, in.SHA256, in.ByteSize, in.ContentType, trust, status, in.LastError, raw)
	return scanSourceRuntimeArtifact(row)
}

func (r *SourceRepository) ListRuntimeArtifacts(ctx context.Context, providerID *int64) ([]SourceRuntimeArtifact, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("source repository 未初始化")
	}
	args := []any{}
	where := ""
	if providerID != nil {
		args = append(args, *providerID)
		where = "WHERE provider_id = $1"
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, provider_id, source_type, artifact_kind, name, source_url, base_url, relative_path,
			local_path, md5, sha256, byte_size, content_type, trust_status, status, last_fetched_at,
			verified_at, last_error, raw, created_at, updated_at
		FROM source_runtime_artifacts
		`+where+`
		ORDER BY updated_at DESC, id DESC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SourceRuntimeArtifact{}
	for rows.Next() {
		item, err := scanSourceRuntimeArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

type sourceRuntimeArtifactScanner interface {
	Scan(dest ...any) error
}

func scanSourceRuntimeArtifact(row sourceRuntimeArtifactScanner) (*SourceRuntimeArtifact, error) {
	var out SourceRuntimeArtifact
	if err := row.Scan(
		&out.ID, &out.ProviderID, &out.SourceType, &out.ArtifactKind, &out.Name, &out.SourceURL,
		&out.BaseURL, &out.RelativePath, &out.LocalPath, &out.MD5, &out.SHA256, &out.ByteSize,
		&out.ContentType, &out.TrustStatus, &out.Status, &out.LastFetchedAt, &out.VerifiedAt,
		&out.LastError, &out.Raw, &out.CreatedAt, &out.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &out, nil
}
