package models

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// junkNameCond 判定“垃圾演员名”：含尖括号 / HTML 实体残留(刮削解析漏过滤的产物)。
// 仅用于过滤与「全选垃圾」辅助；删除一律按显式勾选的 id，绝不按规则盲删。
const junkNameCond = `p.name ~ '[<>]' OR p.name ~ '&(lt|gt|amp|quot|apos|#[0-9]+|#x[0-9a-fA-F]+);'`

// junkNameCondBare 同 junkNameCond 但不带表别名(用于 DELETE FROM persons 无 join 场景)。
const junkNameCondBare = `(name ~ '[<>]' OR name ~ '&(lt|gt|amp|quot|apos|#[0-9]+|#x[0-9a-fA-F]+);')`

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

// actorAdminFrom 是列表/计数共用的 FROM(带 work_count 聚合 join)。
const actorAdminFrom = `FROM persons p
	LEFT JOIN (
		SELECT person_id, COUNT(*) AS cnt
		  FROM cast_members WHERE person_id IS NOT NULL GROUP BY person_id
	) w ON w.person_id = p.id`

// ListActorsAdmin 演员管理列表(服务端分页/过滤/排序)。
func ListActorsAdmin(ctx context.Context, pool *pgxpool.Pool, f ActorAdminFilter) ([]ActorAdminRow, int64, error) {
	args := []any{}
	conds := []string{}
	if s := strings.TrimSpace(f.Search); s != "" {
		args = append(args, "%"+s+"%")
		conds = append(conds, "p.name ILIKE $"+strconv.Itoa(len(args)))
	}
	switch f.Filter {
	case "missing_image":
		conds = append(conds, "(p.image_path IS NULL OR p.image_path = '')")
	case "has_image":
		conds = append(conds, "(p.image_path IS NOT NULL AND p.image_path <> '')")
	case "locked":
		conds = append(conds, "p.image_locked")
	case "with_works":
		conds = append(conds, "COALESCE(w.cnt, 0) > 0")
	case "junk":
		conds = append(conds, "("+junkNameCond+")")
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}

	var total int64
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) "+actorAdminFrom+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listArgs := append([]any{}, args...)
	limClause := ""
	if f.Limit > 0 {
		listArgs = append(listArgs, f.Limit)
		limClause += " LIMIT $" + strconv.Itoa(len(listArgs))
	}
	listArgs = append(listArgs, f.Offset)
	limClause += " OFFSET $" + strconv.Itoa(len(listArgs))

	sql := `SELECT p.id::text, p.name,
	        (p.image_path IS NOT NULL AND p.image_path <> ''),
	        (p.backdrop_path IS NOT NULL AND p.backdrop_path <> ''),
	        p.image_locked,
	        (p.overview IS NOT NULL AND p.overview <> ''),
	        (SELECT COUNT(*) FROM jsonb_object_keys(p.provider_ids))::int,
	        COALESCE(jsonb_array_length(p.tags), 0),
	        COALESCE(w.cnt, 0),
	        (` + junkNameCond + `),
	        EXTRACT(EPOCH FROM p.updated_at)::bigint::text
	        ` + actorAdminFrom + where + actorAdminOrderBy(f.Sort, f.Order) + limClause

	rows, err := pool.Query(ctx, sql, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]ActorAdminRow, 0, 50)
	for rows.Next() {
		var r ActorAdminRow
		if err := rows.Scan(&r.ID, &r.Name, &r.HasImage, &r.HasBackdrop, &r.ImageLocked,
			&r.HasOverview, &r.ProviderCount, &r.TagCount, &r.WorkCount, &r.IsJunk, &r.ImageTag); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// actorAdminOrderBy 白名单化排序，杜绝注入。
func actorAdminOrderBy(sort, order string) string {
	dir := "ASC"
	if strings.EqualFold(order, "desc") {
		dir = "DESC"
	}
	switch sort {
	case "works":
		return " ORDER BY COALESCE(w.cnt, 0) " + dir + ", p.name ASC"
	case "updated":
		return " ORDER BY p.updated_at " + dir + ", p.name ASC"
	default:
		return " ORDER BY p.name " + dir
	}
}

// CountPersonWorks 统计某 person 的作品数(详情抽屉用)。
func CountPersonWorks(ctx context.Context, pool *pgxpool.Pool, personID string) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM cast_members WHERE person_id = $1::uuid`, personID).Scan(&n)
	return n, err
}

// SetPersonImageLocked 设置头像锁定(锁定后刮削不覆盖)。
func SetPersonImageLocked(ctx context.Context, pool *pgxpool.Pool, personID string, locked bool) error {
	_, err := pool.Exec(ctx,
		`UPDATE persons SET image_locked = $2, updated_at = NOW() WHERE id = $1::uuid`,
		personID, locked)
	return err
}

// DeletePersons 删除演员:解除 cast_members 关联(置空 person_id)+ 删 persons 行。
// 返回被删行记录的图片本地路径(image_path / backdrop_path),交调用方清磁盘文件。
func DeletePersons(ctx context.Context, pool *pgxpool.Pool, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var paths []string
	rows, err := tx.Query(ctx,
		`SELECT image_path, backdrop_path FROM persons WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var img, bd *string
		if err := rows.Scan(&img, &bd); err != nil {
			rows.Close()
			return nil, err
		}
		if img != nil && *img != "" {
			paths = append(paths, *img)
		}
		if bd != nil && *bd != "" {
			paths = append(paths, *bd)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE cast_members SET person_id = NULL WHERE person_id = ANY($1::uuid[])`, ids); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM persons WHERE id = ANY($1::uuid[])`, ids); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return paths, nil
}

// DeleteJunkPersons 删除所有“垃圾名”演员(HTML 实体/尖括号残留)。返回图片路径与删除条数。
func DeleteJunkPersons(ctx context.Context, pool *pgxpool.Pool) ([]string, int64, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)

	var paths []string
	rows, err := tx.Query(ctx,
		`SELECT image_path, backdrop_path FROM persons WHERE `+junkNameCondBare)
	if err != nil {
		return nil, 0, err
	}
	for rows.Next() {
		var img, bd *string
		if err := rows.Scan(&img, &bd); err != nil {
			rows.Close()
			return nil, 0, err
		}
		if img != nil && *img != "" {
			paths = append(paths, *img)
		}
		if bd != nil && *bd != "" {
			paths = append(paths, *bd)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE cast_members SET person_id = NULL
		   WHERE person_id IN (SELECT id FROM persons WHERE `+junkNameCondBare+`)`); err != nil {
		return nil, 0, err
	}
	ct, err := tx.Exec(ctx, `DELETE FROM persons WHERE `+junkNameCondBare)
	if err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return paths, ct.RowsAffected(), nil
}
