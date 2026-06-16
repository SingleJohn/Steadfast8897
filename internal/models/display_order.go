package models

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
)

// DisplayOrderEntry 是一个统一展示条目:库或虚拟库。
type DisplayOrderEntry struct {
	Kind string `json:"Kind"` // "library" | "platform"
	ID   string `json:"Id"`
}

// GetDisplayOrder 返回 entry_id -> sort_order 映射。
// entry_id 在两类(库 uuid / 虚拟库派生 uuid)间不会冲突,故直接以 id 为键供合并排序查找。
func GetDisplayOrder(ctx context.Context, pool *pgxpool.Pool) (map[string]int, error) {
	return repository.NewDisplayOrderRepository(pool).GetDisplayOrder(ctx)
}

// SetDisplayOrder 用给定的有序列表整体重写展示顺序(按下标赋 sort_order)。
func SetDisplayOrder(ctx context.Context, pool *pgxpool.Pool, entries []DisplayOrderEntry) error {
	repoEntries := make([]repository.DisplayOrderEntry, 0, len(entries))
	for _, e := range entries {
		repoEntries = append(repoEntries, repository.DisplayOrderEntry{
			Kind: strings.TrimSpace(e.Kind),
			ID:   strings.TrimSpace(e.ID),
		})
	}
	return repository.NewDisplayOrderRepository(pool).SetDisplayOrder(ctx, repoEntries)
}
