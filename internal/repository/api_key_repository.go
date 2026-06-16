package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	dbgen "fyms/internal/db/gen"
)

type APIKey struct {
	ID            uuid.UUID
	Name          string
	Key           string
	CreatedAt     time.Time
	LastUsedAt    *time.Time
	CreatedByName string
}

type APIKeyRepository struct {
	queries *dbgen.Queries
}

func NewAPIKeyRepository(pool *pgxpool.Pool) *APIKeyRepository {
	return &APIKeyRepository{queries: dbgen.New(pool)}
}

func (r *APIKeyRepository) Create(ctx context.Context, name, key string, createdBy *uuid.UUID) (APIKey, error) {
	row, err := r.queries.CreateApiKey(ctx, dbgen.CreateApiKeyParams{
		Name:      name,
		Key:       key,
		CreatedBy: nullableUUID(createdBy),
	})
	if err != nil {
		return APIKey{}, err
	}
	return APIKey{
		ID:         fromPGUUID(row.ID),
		Name:       row.Name,
		Key:        row.Key,
		CreatedAt:  row.CreatedAt.Time,
		LastUsedAt: ptrTime(row.LastUsedAt),
	}, nil
}

func (r *APIKeyRepository) List(ctx context.Context) ([]APIKey, error) {
	rows, err := r.queries.ListApiKeys(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]APIKey, 0, len(rows))
	for _, row := range rows {
		out = append(out, APIKey{
			ID:            fromPGUUID(row.ID),
			Name:          row.Name,
			Key:           row.Key,
			CreatedAt:     row.CreatedAt.Time,
			LastUsedAt:    ptrTime(row.LastUsedAt),
			CreatedByName: row.CreatedByName,
		})
	}
	return out, nil
}

func (r *APIKeyRepository) GetIDByKey(ctx context.Context, key string) (*uuid.UUID, error) {
	id, err := r.queries.GetApiKeyIDByKey(ctx, key)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	uid := fromPGUUID(id)
	return &uid, nil
}

func (r *APIKeyRepository) TouchLastUsed(ctx context.Context, id uuid.UUID) error {
	return r.queries.TouchApiKeyLastUsed(ctx, toPGUUID(id))
}

func (r *APIKeyRepository) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	affected, err := r.queries.DeleteApiKey(ctx, toPGUUID(id))
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func nullableUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return toPGUUID(*id)
}
