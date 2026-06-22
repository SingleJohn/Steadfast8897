package repository

import (
	"context"
	"fmt"
	"strings"
)

func (r *SourceRepository) UpsertParser(ctx context.Context, in SourceParserUpsert) (*SourceParser, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO source_parsers (
			config_id, source_type, name, parser_type, url, base_url, timeout_ms, enabled,
			trust_status, status, last_error, raw
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb
		)
		ON CONFLICT (config_id, name, url) DO UPDATE SET
			source_type = EXCLUDED.source_type,
			parser_type = EXCLUDED.parser_type,
			base_url = EXCLUDED.base_url,
			timeout_ms = EXCLUDED.timeout_ms,
			trust_status = EXCLUDED.trust_status,
			status = EXCLUDED.status,
			last_error = EXCLUDED.last_error,
			raw = EXCLUDED.raw,
			updated_at = NOW()
		RETURNING id, config_id, source_type, name, parser_type, url, base_url, timeout_ms,
		          enabled, trust_status, status, last_check_at, last_error, raw, created_at, updated_at`,
		in.ConfigID, defaultString(in.SourceType, "tvbox"), strings.TrimSpace(in.Name), in.ParserType,
		strings.TrimSpace(in.URL), in.BaseURL, defaultInt32(in.TimeoutMS, 8000), in.Enabled,
		defaultString(in.TrustStatus, "unverified"), defaultString(in.Status, "active"), in.LastError,
		jsonBytesOrObject(in.Raw))
	return scanSourceParser(row)
}

func (r *SourceRepository) ListParsers(ctx context.Context, opts SourceParserListOptions) ([]SourceParser, error) {
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
	if opts.OnlyEnabled {
		clauses = append(clauses, "sp.enabled = TRUE", "sp.status = 'active'", "COALESCE(sci.enabled, TRUE) = TRUE", "COALESCE(sci.import_status, 'active') = 'active'")
	}
	limitArg := addArg(opts.Limit)
	offsetArg := addArg(opts.Offset)
	rows, err := r.pool.Query(ctx, `
		SELECT sp.id, sp.config_id, sp.source_type, sp.name, sp.parser_type, sp.url, sp.base_url,
		       sp.timeout_ms, sp.enabled, sp.trust_status, sp.status, sp.last_check_at,
		       sp.last_error, sp.raw, sp.created_at, sp.updated_at
		  FROM source_parsers sp
		  LEFT JOIN source_config_imports sci ON sci.id = sp.config_id
		 WHERE `+strings.Join(clauses, " AND ")+`
		 ORDER BY sp.enabled DESC, sp.updated_at DESC, sp.id DESC
		 LIMIT `+limitArg+` OFFSET `+offsetArg, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceParser
	for rows.Next() {
		parser, err := scanSourceParser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *parser)
	}
	return out, rows.Err()
}

func (r *SourceRepository) SetParserEnabled(ctx context.Context, id int64, enabled bool) (*SourceParser, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE source_parsers
		   SET enabled = $2,
		       updated_at = NOW()
		 WHERE id = $1
		RETURNING id, config_id, source_type, name, parser_type, url, base_url, timeout_ms,
		          enabled, trust_status, status, last_check_at, last_error, raw, created_at, updated_at`, id, enabled)
	return scanSourceParser(row)
}

func (r *SourceRepository) UpdateParserCheck(ctx context.Context, id int64, lastError *string) (*SourceParser, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE source_parsers
		   SET last_check_at = NOW(),
		       last_error = $2,
		       updated_at = NOW()
		 WHERE id = $1
		RETURNING id, config_id, source_type, name, parser_type, url, base_url, timeout_ms,
		          enabled, trust_status, status, last_check_at, last_error, raw, created_at, updated_at`, id, lastError)
	return scanSourceParser(row)
}
