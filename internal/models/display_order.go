package models

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DisplayOrderEntry 是一个统一展示条目:库或虚拟库。
type DisplayOrderEntry struct {
	Kind string `json:"Kind"` // "library" | "platform"
	ID   string `json:"Id"`
}

// GetDisplayOrder 返回 entry_id -> sort_order 映射。
// entry_id 在两类(库 uuid / 虚拟库派生 uuid)间不会冲突,故直接以 id 为键供合并排序查找。
func GetDisplayOrder(ctx context.Context, pool *pgxpool.Pool) (map[string]int, error) {
	rows, err := pool.Query(ctx, `SELECT entry_id, sort_order FROM library_display_order`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]int)
	for rows.Next() {
		var id string
		var so int
		if err := rows.Scan(&id, &so); err != nil {
			return nil, err
		}
		m[id] = so
	}
	return m, rows.Err()
}

// SetDisplayOrder 用给定的有序列表整体重写展示顺序(按下标赋 sort_order)。
func SetDisplayOrder(ctx context.Context, pool *pgxpool.Pool, entries []DisplayOrderEntry) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM library_display_order`); err != nil {
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
		if _, err := tx.Exec(ctx,
			`INSERT INTO library_display_order (entry_kind, entry_id, sort_order)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (entry_kind, entry_id) DO UPDATE SET sort_order = EXCLUDED.sort_order`,
			kind, id, i); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
