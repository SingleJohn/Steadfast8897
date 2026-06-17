package models

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
)

// ActorAdminRow 是演员管理列表的一行(轻量,带统计/状态)。
type ActorAdminRow struct {
	ID            string `json:"Id"`
	Name          string `json:"Name"`
	HasImage      bool   `json:"HasImage"`
	HasBackdrop   bool   `json:"HasBackdrop"`
	ImageLocked   bool   `json:"ImageLocked"`
	HasOverview   bool   `json:"HasOverview"`
	ProviderCount int    `json:"ProviderCount"`
	TagCount      int    `json:"TagCount"`
	WorkCount     int64  `json:"WorkCount"`
	IsJunk        bool   `json:"IsJunk"`
	ImageTag      string `json:"ImageTag"` // updated_at epoch，给头像缩略图做缓存失效
}

// ActorAdminFilter 是列表查询条件。
type ActorAdminFilter struct {
	Search string // 按名包含匹配
	Filter string // all|missing_image|has_image|locked|with_works|junk
	Sort   string // name|works|updated
	Order  string // asc|desc
	Limit  int64
	Offset int64
}

// ListActorsAdmin 演员管理列表(服务端分页/过滤/排序)。
func ListActorsAdmin(ctx context.Context, pool *pgxpool.Pool, f ActorAdminFilter) ([]ActorAdminRow, int64, error) {
	rows, total, err := repository.NewPersonRepository(pool).ListActorsAdmin(ctx, repository.ActorAdminFilter{
		Search: f.Search,
		Filter: f.Filter,
		Sort:   f.Sort,
		Order:  f.Order,
		Limit:  f.Limit,
		Offset: f.Offset,
	})
	if err != nil {
		return nil, 0, err
	}
	out := make([]ActorAdminRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, ActorAdminRow{
			ID:            row.ID,
			Name:          row.Name,
			HasImage:      row.HasImage,
			HasBackdrop:   row.HasBackdrop,
			ImageLocked:   row.ImageLocked,
			HasOverview:   row.HasOverview,
			ProviderCount: row.ProviderCount,
			TagCount:      row.TagCount,
			WorkCount:     row.WorkCount,
			IsJunk:        row.IsJunk,
			ImageTag:      row.ImageTag,
		})
	}
	return out, total, nil
}

// CountPersonWorks 统计某 person 的作品数(详情抽屉用)。
func CountPersonWorks(ctx context.Context, pool *pgxpool.Pool, personID string) (int64, error) {
	return repository.NewPersonRepository(pool).CountWorks(ctx, personID)
}

// SetPersonImageLocked 设置头像锁定(锁定后刮削不覆盖)。
func SetPersonImageLocked(ctx context.Context, pool *pgxpool.Pool, personID string, locked bool) error {
	return repository.NewPersonRepository(pool).SetImageLocked(ctx, personID, locked)
}

// DeletePersons 删除演员:解除 cast_members 关联(置空 person_id)+ 删 persons 行。
// 返回被删行记录的图片本地路径(image_path / backdrop_path),交调用方清磁盘文件。
func DeletePersons(ctx context.Context, pool *pgxpool.Pool, ids []string) ([]string, error) {
	return repository.NewPersonRepository(pool).DeletePersons(ctx, ids)
}

// DeleteJunkPersons 删除所有“垃圾名”演员(HTML 实体/尖括号残留)。返回图片路径与删除条数。
func DeleteJunkPersons(ctx context.Context, pool *pgxpool.Pool) ([]string, int64, error) {
	return repository.NewPersonRepository(pool).DeleteJunkPersons(ctx)
}
