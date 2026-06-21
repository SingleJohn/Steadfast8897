package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SourceRepository struct {
	pool *pgxpool.Pool
}

func NewSourceRepository(pool *pgxpool.Pool) *SourceRepository {
	return &SourceRepository{pool: pool}
}

func (r *SourceRepository) ResolveSourceItemPublicUUID(ctx context.Context, publicUUID string) (int64, bool, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM source_items WHERE public_uuid = $1::uuid`,
		publicUUID).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func (r *SourceRepository) ResolveSourceLibraryViewPublicUUID(ctx context.Context, publicUUID string) (int64, bool, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM source_library_views WHERE public_uuid = $1::uuid`,
		publicUUID).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}
