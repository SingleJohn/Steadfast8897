package services

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func tmdbConfigured(ctx context.Context, pool *pgxpool.Pool) bool {
	raw := strings.TrimSpace(readSystemConfigValue(ctx, pool, "tmdb_api_key"))
	if raw == "" {
		return false
	}
	for _, key := range strings.Split(raw, ",") {
		if strings.TrimSpace(key) != "" {
			return true
		}
	}
	return false
}
