package models

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

// libraryColumns 列出所有 Library 结构体字段对应的列。
// 不再用 SELECT *，避免后续新增列(如 deleted_at)破坏 Scan 行为。
// scrape_config::text 统一以字符串形式返回,反序列化交给上层。
const libraryColumns = `id, name, collection_type, paths, created_at, primary_image_path, primary_image_tag, sort_order, scrape_config::text`

func scanLibrary(row pgx.Row) (*Library, error) {
	var l Library
	err := row.Scan(&l.ID, &l.Name, &l.CollectionType, &l.Paths, &l.CreatedAt,
		&l.PrimaryImagePath, &l.PrimaryImageTag, &l.SortOrder, &l.ScrapeConfig)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func GetAllLibraries(ctx context.Context, pool *pgxpool.Pool) ([]Library, error) {
	rows, err := pool.Query(ctx,
		"SELECT "+libraryColumns+" FROM libraries WHERE deleted_at IS NULL ORDER BY sort_order ASC, name ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var libs []Library
	for rows.Next() {
		var l Library
		if err := rows.Scan(&l.ID, &l.Name, &l.CollectionType, &l.Paths, &l.CreatedAt,
			&l.PrimaryImagePath, &l.PrimaryImageTag, &l.SortOrder, &l.ScrapeConfig); err != nil {
			return nil, err
		}
		libs = append(libs, l)
	}
	return libs, rows.Err()
}

func GetLibraryByID(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (*Library, error) {
	row := pool.QueryRow(ctx,
		"SELECT "+libraryColumns+" FROM libraries WHERE id = $1 AND deleted_at IS NULL", id)
	l, err := scanLibrary(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return l, err
}

func CreateLibrary(ctx context.Context, pool *pgxpool.Pool, name, collectionType string, paths []string) (*Library, error) {
	row := pool.QueryRow(ctx,
		"INSERT INTO libraries (name, collection_type, paths) VALUES ($1, $2, $3) RETURNING "+libraryColumns,
		name, collectionType, paths)
	return scanLibrary(row)
}

func UpdateLibrary(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, name *string) (*Library, error) {
	if name != nil {
		_, err := pool.Exec(ctx, "UPDATE libraries SET name = $1 WHERE id = $2", *name, id)
		if err != nil {
			return nil, err
		}
	}
	return GetLibraryByID(ctx, pool, id)
}

func UpdateLibrarySortOrder(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, sortOrder int) error {
	_, err := pool.Exec(ctx, "UPDATE libraries SET sort_order = $1 WHERE id = $2", sortOrder, id)
	return err
}

// UpdateLibraryScrapeConfig 写入库级刮削配置。
// rawJSON 为 nil → 清空(scrape_config = NULL = 继承全局);
// 非 nil → 写入 JSONB(上层负责保证是合法 JSON object)。
// 保存后调用方应触发 services.InvalidateScrapeAggregator 让 aggregator 缓存失效。
func UpdateLibraryScrapeConfig(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, rawJSON *string) error {
	if rawJSON == nil {
		_, err := pool.Exec(ctx,
			"UPDATE libraries SET scrape_config = NULL WHERE id = $1", id)
		return err
	}
	_, err := pool.Exec(ctx,
		"UPDATE libraries SET scrape_config = $1::jsonb WHERE id = $2", *rawJSON, id)
	return err
}

type LibrarySortItem struct {
	ID        string `json:"Id"`
	SortOrder int    `json:"SortOrder"`
}

func BatchUpdateLibrarySortOrder(ctx context.Context, pool *pgxpool.Pool, orders []LibrarySortItem) error {
	for _, o := range orders {
		uid, err := uuid.Parse(o.ID)
		if err != nil {
			continue
		}
		if _, err := pool.Exec(ctx, "UPDATE libraries SET sort_order = $1 WHERE id = $2", o.SortOrder, uid); err != nil {
			return err
		}
	}
	return nil
}

// MarkLibraryDeleted 软删除：设置 deleted_at，后续读查询都会过滤掉这一行。
// 真正的 items/libraries 物理删除由后台 cleanup goroutine 分批完成。
// 对已标记过的库重复调用是 no-op（WHERE 过滤保证幂等）。
func MarkLibraryDeleted(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (bool, error) {
	ct, err := pool.Exec(ctx,
		"UPDATE libraries SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL", id)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

// CountLibraryItems 统计某个库下剩余未删除的 items，清理 worker 用它计算 total。
func CountLibraryItems(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM items WHERE library_id = $1", id).Scan(&n)
	return n, err
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
	_, err := pool.Exec(ctx, "DELETE FROM libraries WHERE id = $1 AND deleted_at IS NOT NULL", id)
	return err
}

// ListDeletedLibraryIDs 返回所有已标记删除但 items 行或 libraries 行仍在的库 id。
// 服务启动时用它接管上次进程中途退出遗留的待清理任务。
func ListDeletedLibraryIDs(ctx context.Context, pool *pgxpool.Pool) ([]uuid.UUID, error) {
	rows, err := pool.Query(ctx, "SELECT id FROM libraries WHERE deleted_at IS NOT NULL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetLibraryNameIncludingDeleted 获取库名（即使已软删除），给 cleanup snapshot 展示用。
func GetLibraryNameIncludingDeleted(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (string, error) {
	var name string
	err := pool.QueryRow(ctx, "SELECT name FROM libraries WHERE id = $1", id).Scan(&name)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return name, err
}

func AddLibraryPath(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, path string) error {
	_, err := pool.Exec(ctx,
		"UPDATE libraries SET paths = array_append(paths, $1) WHERE id = $2", path, id)
	return err
}

func UpdateLibraryImage(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, imagePath, imageTag string) error {
	_, err := pool.Exec(ctx,
		"UPDATE libraries SET primary_image_path = $1, primary_image_tag = $2 WHERE id = $3",
		imagePath, imageTag, id)
	return err
}

func DeleteLibraryImage(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	_, err := pool.Exec(ctx,
		"UPDATE libraries SET primary_image_path = NULL, primary_image_tag = NULL WHERE id = $1", id)
	return err
}

func RemoveLibraryPath(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, path string) error {
	_, err := pool.Exec(ctx,
		"UPDATE libraries SET paths = array_remove(paths, $1) WHERE id = $2", path, id)
	return err
}
