package repository

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/db/gen"
)

type DisplayOrderRepository struct {
	pool    *pgxpool.Pool
	queries *dbgen.Queries
}

func NewDisplayOrderRepository(pool *pgxpool.Pool) *DisplayOrderRepository {
	return &DisplayOrderRepository{
		pool:    pool,
		queries: dbgen.New(pool),
	}
}

func (r *DisplayOrderRepository) GetDisplayOrder(ctx context.Context) (map[string]int, error) {
	rows, err := r.queries.ListDisplayOrder(ctx)
	if err != nil {
		return nil, err
	}
	order := make(map[string]int, len(rows))
	for _, row := range rows {
		order[row.EntryID] = int(row.SortOrder)
	}
	return order, nil
}

func (r *DisplayOrderRepository) SetDisplayOrder(ctx context.Context, entries []DisplayOrderEntry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	q := r.queries.WithTx(tx)
	if err := q.ClearDisplayOrder(ctx); err != nil {
		return err
	}
	for i, e := range entries {
		id := strings.TrimSpace(e.ID)
		if id == "" {
			continue
		}
		kind := strings.TrimSpace(e.Kind)
		if kind == "" {
			kind = "library"
		}
		if err := q.UpsertDisplayOrderEntry(ctx, dbgen.UpsertDisplayOrderEntryParams{
			EntryKind: kind,
			EntryID:   id,
			SortOrder: int32(i),
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
