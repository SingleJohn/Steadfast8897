package repository

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/db/gen"
)

type SystemConfigRepository struct {
	queries *dbgen.Queries
}

func NewSystemConfigRepository(pool *pgxpool.Pool) *SystemConfigRepository {
	return &SystemConfigRepository{
		queries: dbgen.New(pool),
	}
}

func (r *SystemConfigRepository) GetString(ctx context.Context, key string) (string, bool, error) {
	value, err := r.queries.GetSystemConfigValue(ctx, key)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	if !value.Valid {
		return "", false, nil
	}
	return value.String, true, nil
}

func (r *SystemConfigRepository) GetStringOrDefault(ctx context.Context, key, def string) string {
	value, ok, err := r.GetString(ctx, key)
	if err != nil || !ok {
		return def
	}
	return value
}

func (r *SystemConfigRepository) GetBoolOrDefault(ctx context.Context, key string, def bool) bool {
	raw, ok, err := r.GetString(ctx, key)
	if err != nil || !ok {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func (r *SystemConfigRepository) GetIntOrDefault(ctx context.Context, key string, def int) int {
	raw, ok, err := r.GetString(ctx, key)
	if err != nil || !ok {
		return def
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return v
}

func (r *SystemConfigRepository) SetString(ctx context.Context, key, value string) error {
	return r.queries.UpsertSystemConfigValue(ctx, dbgen.UpsertSystemConfigValueParams{
		Key: key,
		Value: pgtype.Text{
			String: value,
			Valid:  true,
		},
	})
}

func (r *SystemConfigRepository) SetBool(ctx context.Context, key string, value bool) error {
	if value {
		return r.SetString(ctx, key, "true")
	}
	return r.SetString(ctx, key, "false")
}
