package repository

import (
	"context"
	"fmt"
	"strings"
)

func (r *SourceRepository) CreateRuntimeInvocation(ctx context.Context, in SourceRuntimeInvocationCreate) (*SourceRuntimeInvocation, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("source repository 未初始化")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO source_runtime_invocations (
			provider_id, runtime_kind, method, status, error_type, error_message, duration_ms,
			engine_ok, worker_pid, artifact_ids, url_hash, raw
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, COALESCE($10, '{}'::bigint[]), $11, $12::jsonb
		)
		RETURNING id, provider_id, runtime_kind, method, status, error_type, error_message,
		          duration_ms, engine_ok, worker_pid, artifact_ids, url_hash, raw, invoked_at`,
		in.ProviderID, defaultString(in.RuntimeKind, "unknown"), strings.TrimSpace(in.Method),
		defaultString(in.Status, "unknown"), in.ErrorType, in.ErrorMessage, in.DurationMS,
		in.EngineOK, in.WorkerPID, nonNilInt64s(in.ArtifactIDs), in.URLHash, jsonBytesOrObject(in.Raw))
	return scanSourceRuntimeInvocation(row)
}

func (r *SourceRepository) ListRuntimeInvocations(ctx context.Context, opts SourceRuntimeInvocationListOptions) ([]SourceRuntimeInvocation, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("source repository 未初始化")
	}
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
	if opts.ProviderID != nil {
		clauses = append(clauses, "provider_id = "+addArg(*opts.ProviderID))
	}
	if strings.TrimSpace(opts.Method) != "" {
		clauses = append(clauses, "method = "+addArg(strings.TrimSpace(opts.Method)))
	}
	if strings.TrimSpace(opts.Status) != "" {
		clauses = append(clauses, "status = "+addArg(strings.TrimSpace(opts.Status)))
	}
	limitArg := addArg(opts.Limit)
	offsetArg := addArg(opts.Offset)
	rows, err := r.pool.Query(ctx, `
		SELECT id, provider_id, runtime_kind, method, status, error_type, error_message,
		       duration_ms, engine_ok, worker_pid, artifact_ids, url_hash, raw, invoked_at
		  FROM source_runtime_invocations
		 WHERE `+strings.Join(clauses, " AND ")+`
		 ORDER BY invoked_at DESC, id DESC
		 LIMIT `+limitArg+` OFFSET `+offsetArg, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SourceRuntimeInvocation{}
	for rows.Next() {
		item, err := scanSourceRuntimeInvocation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func nonNilInt64s(values []int64) []int64 {
	if values == nil {
		return []int64{}
	}
	return values
}
