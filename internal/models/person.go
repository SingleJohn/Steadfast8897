package models

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// personDBTX 同时被 *pgxpool.Pool 与 pgx.Tx 满足,使 link/propagate
// 既能在 ApplyNfo 的事务里调用,也能在独立连接上调用。
type personDBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Person 是全局人物实体(按 name 归一)。
type Person struct {
	ID           string
	Name         string
	ImagePath    *string
	ImageLocked  bool
	TmdbPersonID *int32
	Overview     *string
	ImageTag     string // 随 updated_at 变化,用于客户端缓存失效
}

// EnsurePersonsForItem 为某 item 下还没有 person_id 的 cast_members 建立/关联 persons。
// 幂等:persons.name 唯一,重复姓名只会命中已有行。在 ApplyNfo 事务内调用。
func EnsurePersonsForItem(ctx context.Context, db personDBTX, itemID string) error {
	if _, err := db.Exec(ctx,
		`INSERT INTO persons (name)
		   SELECT DISTINCT name FROM cast_members
		    WHERE item_id = $1::uuid AND person_id IS NULL
		      AND name IS NOT NULL AND name <> ''
		 ON CONFLICT (name) DO NOTHING`,
		itemID); err != nil {
		return err
	}
	_, err := db.Exec(ctx,
		`UPDATE cast_members cm
		    SET person_id = p.id
		   FROM persons p
		  WHERE p.name = cm.name
		    AND cm.item_id = $1::uuid
		    AND cm.person_id IS NULL`,
		itemID)
	return err
}

// PropagateCastImagesToPersons 把某 item 下 cast_members.image_url 提升为
// persons.image_path 的初始值 —— 仅当 person 未锁定且还没有头像时。
// 用于 NFO thumb / 本地 .actors 扫描:写完 cast_members 后让全局头像跟上。
func PropagateCastImagesToPersons(ctx context.Context, db personDBTX, itemID string) error {
	_, err := db.Exec(ctx,
		`UPDATE persons p
		    SET image_path = sub.image_url,
		        updated_at = NOW()
		   FROM (
		     SELECT DISTINCT ON (person_id) person_id, image_url
		       FROM cast_members
		      WHERE item_id = $1::uuid
		        AND person_id IS NOT NULL
		        AND image_url IS NOT NULL AND image_url <> ''
		      ORDER BY person_id, order_index
		   ) sub
		  WHERE p.id = sub.person_id
		    AND p.image_locked = false
		    AND (p.image_path IS NULL OR p.image_path = '')`,
		itemID)
	return err
}

// GetPersonImagePath 按 person id 取头像路径(image_path 优先;为空时回退到
// 该 person 任一 cast_members.image_url)。serveImage 用它解析 /Items/{personId}/Images。
func GetPersonImagePath(ctx context.Context, pool *pgxpool.Pool, personID string) (string, bool) {
	var img *string
	err := pool.QueryRow(ctx,
		`SELECT COALESCE(NULLIF(p.image_path, ''),
		        (SELECT image_url FROM cast_members
		          WHERE person_id = p.id AND image_url IS NOT NULL AND image_url <> ''
		          LIMIT 1))
		   FROM persons p WHERE p.id = $1::uuid`,
		personID).Scan(&img)
	if err != nil || img == nil || *img == "" {
		return "", false
	}
	return *img, true
}

// SetPersonImage 写入(并锁定)person 头像。上传接口用,全库同名条目随之生效。
func SetPersonImage(ctx context.Context, pool *pgxpool.Pool, personID, imagePath string, locked bool) error {
	_, err := pool.Exec(ctx,
		`UPDATE persons
		    SET image_path = $1, image_locked = $2, updated_at = NOW()
		  WHERE id = $3::uuid`,
		imagePath, locked, personID)
	return err
}

// ClearPersonImage 清除 person 头像并解锁。
func ClearPersonImage(ctx context.Context, pool *pgxpool.Pool, personID string) error {
	_, err := pool.Exec(ctx,
		`UPDATE persons
		    SET image_path = NULL, image_locked = false, updated_at = NOW()
		  WHERE id = $1::uuid`,
		personID)
	return err
}

// ListPersonsMissingImage 返回还没有头像且未锁定的 person(批量按名补头像用)。
func ListPersonsMissingImage(ctx context.Context, pool *pgxpool.Pool, limit int) ([]Person, error) {
	sql := `SELECT id::text, name FROM persons
	         WHERE image_locked = false
	           AND (image_path IS NULL OR image_path = '')
	           AND name IS NOT NULL AND name <> ''
	         ORDER BY name`
	args := []any{}
	if limit > 0 {
		sql += " LIMIT $1"
		args = append(args, limit)
	}
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Person
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// FillPersonImageIfUnlocked 给未锁定且当前无头像的 person 写 image_path(批量补,不锁定)。
// 返回是否实际写入。
func FillPersonImageIfUnlocked(ctx context.Context, pool *pgxpool.Pool, personID, imagePath string) (bool, error) {
	tag, err := pool.Exec(ctx,
		`UPDATE persons
		    SET image_path = $1, updated_at = NOW()
		  WHERE id = $2::uuid
		    AND image_locked = false
		    AND (image_path IS NULL OR image_path = '')`,
		imagePath, personID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ListItemsForActorImageBackfill 返回有 tmdb_id 且仍有演员既无 per-item 头像
// 也无全局头像的 Movie/Series id —— 批量 TMDB 补头像入队用。
func ListItemsForActorImageBackfill(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT DISTINCT i.id::text
		   FROM items i
		   JOIN cast_members cm ON cm.item_id = i.id
		   LEFT JOIN persons p ON p.id = cm.person_id
		  WHERE i.type IN ('Movie','Series')
		    AND i.tmdb_id IS NOT NULL AND i.tmdb_id > 0
		    AND COALESCE(NULLIF(p.image_path,''), NULLIF(cm.image_url,'')) IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ActorImageStats 给前端展示头像覆盖情况。
type ActorImageStats struct {
	Total     int64 `json:"total"`
	WithImage int64 `json:"with_image"`
	Missing   int64 `json:"missing"`
	Locked    int64 `json:"locked"`
}

// GetActorImageStats 统计 persons 头像覆盖。
func GetActorImageStats(ctx context.Context, pool *pgxpool.Pool) (ActorImageStats, error) {
	var s ActorImageStats
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*),
		        COUNT(*) FILTER (WHERE image_path IS NOT NULL AND image_path <> ''),
		        COUNT(*) FILTER (WHERE image_locked)
		   FROM persons`).Scan(&s.Total, &s.WithImage, &s.Locked)
	if err != nil {
		return s, err
	}
	s.Missing = s.Total - s.WithImage
	return s, nil
}

// PersonExists 判断某 uuid 是否为 person(serveImage 区分 person/item 用)。
func PersonExists(ctx context.Context, pool *pgxpool.Pool, id string) bool {
	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM persons WHERE id = $1::uuid)`, id).Scan(&exists); err != nil {
		return false
	}
	return exists
}

// ListPersons 列出人物(供 /Persons 端点)。search 非空时按姓名前缀/包含过滤。
// GetPersonByName 按精确姓名取单个 person（对齐 Emby `GET /Persons/{Name}` 的 Items-by-Name
// 详情语义）。未命中返回 (nil, nil)，由调用方决定返回 404。
func GetPersonByName(ctx context.Context, pool *pgxpool.Pool, name string) (*Person, error) {
	var p Person
	err := pool.QueryRow(ctx,
		`SELECT id::text, name, image_path, image_locked, tmdb_person_id, overview,
		        EXTRACT(EPOCH FROM updated_at)::bigint::text
		   FROM persons WHERE name = $1 LIMIT 1`, name).Scan(
		&p.ID, &p.Name, &p.ImagePath, &p.ImageLocked, &p.TmdbPersonID, &p.Overview, &p.ImageTag)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func ListPersons(ctx context.Context, pool *pgxpool.Pool, search string, limit, offset int64) ([]Person, int64, error) {
	var total int64
	countSQL := `SELECT COUNT(*) FROM persons`
	args := []any{}
	where := ""
	if search != "" {
		where = ` WHERE name ILIKE $1`
		args = append(args, "%"+search+"%")
	}
	if err := pool.QueryRow(ctx, countSQL+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listSQL := `SELECT id::text, name, image_path, image_locked, tmdb_person_id, overview,
	                   EXTRACT(EPOCH FROM updated_at)::bigint::text
	              FROM persons` + where + ` ORDER BY name`
	listArgs := append([]any{}, args...)
	// limit <= 0 表示不限量（对齐 Emby /Persons 未传 Limit 的语义，返回全部）。
	if limit > 0 {
		listSQL += " LIMIT $" + strconv.Itoa(len(listArgs)+1)
		listArgs = append(listArgs, limit)
	}
	listSQL += " OFFSET $" + strconv.Itoa(len(listArgs)+1)
	listArgs = append(listArgs, offset)

	rows, err := pool.Query(ctx, listSQL, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var result []Person
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.ID, &p.Name, &p.ImagePath, &p.ImageLocked, &p.TmdbPersonID, &p.Overview, &p.ImageTag); err != nil {
			return nil, 0, err
		}
		result = append(result, p)
	}
	return result, total, rows.Err()
}
