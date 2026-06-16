package models

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
)

type Library struct {
	ID               uuid.UUID `json:"Id"`
	Name             string    `json:"Name"`
	CollectionType   string    `json:"CollectionType"`
	Paths            []string  `json:"Paths"`
	CreatedAt        time.Time `json:"CreatedAt"`
	PrimaryImagePath *string   `json:"PrimaryImagePath,omitempty"`
	PrimaryImageTag  *string   `json:"PrimaryImageTag,omitempty"`
	SortOrder        int       `json:"SortOrder"`
	ScrapeConfig     *string   `json:"ScrapeConfig,omitempty"` // 原始 JSONB 文本,nil = 继承全局
}

func GetAllLibraries(ctx context.Context, pool *pgxpool.Pool) ([]Library, error) {
	libs, err := repository.NewLibraryRepository(pool).ListLibraries(ctx)
	return modelLibrariesFromRepo(libs), err
}

func GetLibraryByID(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (*Library, error) {
	lib, err := repository.NewLibraryRepository(pool).GetLibraryByID(ctx, id)
	return modelLibraryFromRepo(lib), err
}

func CreateLibrary(ctx context.Context, pool *pgxpool.Pool, name, collectionType string, paths []string) (*Library, error) {
	lib, err := repository.NewLibraryRepository(pool).CreateLibrary(ctx, name, collectionType, paths)
	return modelLibraryFromRepo(lib), err
}

func UpdateLibrary(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, name *string) (*Library, error) {
	lib, err := repository.NewLibraryRepository(pool).UpdateLibrary(ctx, id, name)
	return modelLibraryFromRepo(lib), err
}

func UpdateLibrarySortOrder(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, sortOrder int) error {
	return repository.NewLibraryRepository(pool).UpdateLibrarySortOrder(ctx, id, sortOrder)
}

// UpdateLibraryScrapeConfig 写入库级刮削配置。
// rawJSON 为 nil → 清空(scrape_config = NULL = 继承全局);
// 非 nil → 写入 JSONB(上层负责保证是合法 JSON object)。
// 保存后调用方应触发 services.InvalidateScrapeAggregator 让 aggregator 缓存失效。
func UpdateLibraryScrapeConfig(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, rawJSON *string) error {
	return repository.NewLibraryRepository(pool).UpdateLibraryScrapeConfig(ctx, id, rawJSON)
}

type LibrarySortItem struct {
	ID        string `json:"Id"`
	SortOrder int    `json:"SortOrder"`
}

func BatchUpdateLibrarySortOrder(ctx context.Context, pool *pgxpool.Pool, orders []LibrarySortItem) error {
	repo := repository.NewLibraryRepository(pool)
	for _, o := range orders {
		uid, err := uuid.Parse(o.ID)
		if err != nil {
			continue
		}
		if err := repo.UpdateLibrarySortOrder(ctx, uid, o.SortOrder); err != nil {
			return err
		}
	}
	return nil
}

// MarkLibraryDeleted 软删除：设置 deleted_at，后续读查询都会过滤掉这一行。
// 真正的 items/libraries 物理删除由后台 cleanup goroutine 分批完成。
// 对已标记过的库重复调用是 no-op（WHERE 过滤保证幂等）。
func MarkLibraryDeleted(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (bool, error) {
	return repository.NewLibraryRepository(pool).MarkLibraryDeleted(ctx, id)
}

// CountLibraryItems 统计某个库下剩余未删除的 items，清理 worker 用它计算 total。
func CountLibraryItems(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (int64, error) {
	return repository.NewLibraryRepository(pool).CountLibraryItems(ctx, id)
}

// DeleteLibraryItemsBatch 分批删除指定库的 items。
// 每次最多删 limit 行，返回本批实际删除数。0 表示已清空。
// 采用 CTE + LIMIT 避免一条 DELETE 锁住整表 + 触发整条 CASCADE 链同步处理几十万行。
func DeleteLibraryItemsBatch(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, limit int) (int64, error) {
	if limit <= 0 {
		limit = 500
	}
	ct, err := pool.Exec(ctx, `
		WITH victims AS (
			SELECT id FROM items WHERE library_id = $1 LIMIT $2
		)
		DELETE FROM items WHERE id IN (SELECT id FROM victims)
	`, id, limit)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

// FinalizeLibraryDeletion 在 items 全部清理完后，把 libraries 行本身删掉。
func FinalizeLibraryDeletion(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	return repository.NewLibraryRepository(pool).FinalizeLibraryDeletion(ctx, id)
}

// ListDeletedLibraryIDs 返回所有已标记删除但 items 行或 libraries 行仍在的库 id。
// 服务启动时用它接管上次进程中途退出遗留的待清理任务。
func ListDeletedLibraryIDs(ctx context.Context, pool *pgxpool.Pool) ([]uuid.UUID, error) {
	return repository.NewLibraryRepository(pool).ListDeletedLibraryIDs(ctx)
}

// GetLibraryNameIncludingDeleted 获取库名（即使已软删除），给 cleanup snapshot 展示用。
func GetLibraryNameIncludingDeleted(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (string, error) {
	return repository.NewLibraryRepository(pool).GetLibraryNameIncludingDeleted(ctx, id)
}

func AddLibraryPath(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, path string) error {
	return repository.NewLibraryRepository(pool).AddLibraryPath(ctx, id, path)
}

func UpdateLibraryImage(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, imagePath, imageTag string) error {
	return repository.NewLibraryRepository(pool).UpdateLibraryImage(ctx, id, imagePath, imageTag)
}

func DeleteLibraryImage(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	return repository.NewLibraryRepository(pool).DeleteLibraryImage(ctx, id)
}

func RemoveLibraryPath(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, path string) error {
	return repository.NewLibraryRepository(pool).RemoveLibraryPath(ctx, id, path)
}

func modelLibrariesFromRepo(libs []repository.Library) []Library {
	result := make([]Library, 0, len(libs))
	for _, lib := range libs {
		result = append(result, *modelLibraryFromRepo(&lib))
	}
	return result
}

func modelLibraryFromRepo(lib *repository.Library) *Library {
	if lib == nil {
		return nil
	}
	return &Library{
		ID:               lib.ID,
		Name:             lib.Name,
		CollectionType:   lib.CollectionType,
		Paths:            lib.Paths,
		CreatedAt:        lib.CreatedAt,
		PrimaryImagePath: lib.PrimaryImagePath,
		PrimaryImageTag:  lib.PrimaryImageTag,
		SortOrder:        lib.SortOrder,
		ScrapeConfig:     lib.ScrapeConfig,
	}
}
