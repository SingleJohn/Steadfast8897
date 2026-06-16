package models

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/repository"
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

	// 第三方刮削器(mdc-ng 等)回写并完整保存的扩展资料(迁移 060)。
	PremiereDate        *string           // 出生日期 "YYYY-MM-DD"
	ProductionYear      *int32            // 出生年
	ProductionLocations []string          // 出身地
	Genres              []string          //
	Tags                []string          // 罩杯/身高/三围/年龄/生涯/账号 等
	Taglines            []string          //
	ProviderIDs         map[string]string // 完整外部 id 映射
	BackdropPath        *string           // 背景图(独立于头像 ImagePath)
}

// personColumns 是 GetPersonByName/ByID/ListPersons 共用的列清单与 scanPerson 对应。
// jsonb 列统一 ::text 取出，scanPerson 再 Unmarshal —— 不依赖 pgx 对 jsonb 的扫描细节。
const personColumns = `id::text, name, image_path, image_locked, tmdb_person_id, overview,
	EXTRACT(EPOCH FROM updated_at)::bigint::text,
	premiere_date, production_year,
	production_locations::text, genres::text, tags::text, taglines::text, provider_ids::text, backdrop_path`

const personColumnsP = `p.id::text, p.name, p.image_path, p.image_locked, p.tmdb_person_id, p.overview,
	EXTRACT(EPOCH FROM p.updated_at)::bigint::text,
	p.premiere_date, p.production_year,
	p.production_locations::text, p.genres::text, p.tags::text, p.taglines::text, p.provider_ids::text, p.backdrop_path`

// scanPerson 扫描 personColumns 一行。jsonb 列以 text 入再 Unmarshal(空值容错)。
func scanPerson(row pgx.Row) (*Person, error) {
	var p Person
	var locs, genres, tags, taglines, providerIDs string
	if err := row.Scan(
		&p.ID, &p.Name, &p.ImagePath, &p.ImageLocked, &p.TmdbPersonID, &p.Overview, &p.ImageTag,
		&p.PremiereDate, &p.ProductionYear,
		&locs, &genres, &tags, &taglines, &providerIDs, &p.BackdropPath,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(locs), &p.ProductionLocations)
	_ = json.Unmarshal([]byte(genres), &p.Genres)
	_ = json.Unmarshal([]byte(tags), &p.Tags)
	_ = json.Unmarshal([]byte(taglines), &p.Taglines)
	_ = json.Unmarshal([]byte(providerIDs), &p.ProviderIDs)
	return &p, nil
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
	img, ok, err := repository.NewItemHelperRepository(pool).GetPersonImagePath(ctx, personID)
	if err != nil || !ok {
		return "", false
	}
	return img, true
}

// SetPersonImage 写入(并锁定)person 头像。上传接口用,全库同名条目随之生效。
func SetPersonImage(ctx context.Context, pool *pgxpool.Pool, personID, imagePath string, locked bool) error {
	return repository.NewItemHelperRepository(pool).SetPersonImage(ctx, personID, imagePath, locked)
}

// ClearPersonImage 清除 person 头像并解锁。
func ClearPersonImage(ctx context.Context, pool *pgxpool.Pool, personID string) error {
	return repository.NewItemHelperRepository(pool).ClearPersonImage(ctx, personID)
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
	return repository.NewItemHelperRepository(pool).FillPersonImageIfUnlocked(ctx, personID, imagePath)
}

// ListItemsForActorImageBackfill 返回有 tmdb_id 且仍有演员既无 per-item 头像
// 也无全局头像的 Movie/Series id —— 批量 TMDB 补头像入队用。
func ListItemsForActorImageBackfill(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	return repository.NewItemHelperRepository(pool).ListItemsForActorImageBackfill(ctx)
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
	stats, err := repository.NewItemHelperRepository(pool).GetActorImageStats(ctx)
	if err != nil {
		return s, err
	}
	s.Total = stats.Total
	s.WithImage = stats.WithImage
	s.Locked = stats.Locked
	s.Missing = s.Total - s.WithImage
	return s, nil
}

// PersonExists 判断某 uuid 是否为 person(serveImage 区分 person/item 用)。
func PersonExists(ctx context.Context, pool *pgxpool.Pool, id string) bool {
	exists, err := repository.NewItemHelperRepository(pool).PersonExists(ctx, id)
	if err != nil {
		return false
	}
	return exists
}

// GetPersonByName 按精确姓名取单个 person（对齐 Emby `GET /Persons/{Name}` 的 Items-by-Name
// 详情语义）。未命中返回 (nil, nil)，由调用方决定返回 404。
func GetPersonByName(ctx context.Context, pool *pgxpool.Pool, name string) (*Person, error) {
	p, err := scanPerson(pool.QueryRow(ctx,
		`SELECT `+personColumns+` FROM persons WHERE name = $1 LIMIT 1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// GetPersonByID 按 person id 取单个 person（Emby 里 person 也是 item，
// `GET /Items/{personId}` 复用此与 GetPersonByName 同构的详情）。未命中或 id 非法返回 (nil, nil)。
func GetPersonByID(ctx context.Context, pool *pgxpool.Pool, id string) (*Person, error) {
	p, err := scanPerson(pool.QueryRow(ctx,
		`SELECT `+personColumns+` FROM persons WHERE id = $1::uuid LIMIT 1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// PersonMetadataUpdate 是 POST /Items/{personId} 回写的演员资料。每个字段：指针/切片/映射
// 为 nil 表示“本次未提供，保留原值”；非 nil（含空切片/空串）表示显式覆盖。
type PersonMetadataUpdate struct {
	Overview            *string
	PremiereDate        *string
	ProductionYear      *int32
	ProductionLocations []string
	Genres              []string
	Tags                []string
	Taglines            []string
	ProviderIDs         map[string]string
	TmdbPersonID        *int32
}

// UpdatePersonMetadata 完整持久化第三方刮削器(mdc-ng 等)回写的演员资料。
// COALESCE 保证只覆盖本次显式提供的字段；jsonb 列由 jsonbArg 决定传 nil(保留)或新值。
func UpdatePersonMetadata(ctx context.Context, pool *pgxpool.Pool, id string, u PersonMetadataUpdate) error {
	_, err := pool.Exec(ctx,
		`UPDATE persons
		    SET overview              = COALESCE($2, overview),
		        premiere_date         = COALESCE($3, premiere_date),
		        production_year       = COALESCE($4, production_year),
		        tmdb_person_id        = COALESCE($5, tmdb_person_id),
		        production_locations  = COALESCE($6::jsonb, production_locations),
		        genres                = COALESCE($7::jsonb, genres),
		        tags                  = COALESCE($8::jsonb, tags),
		        taglines              = COALESCE($9::jsonb, taglines),
		        provider_ids          = COALESCE($10::jsonb, provider_ids),
		        updated_at            = NOW()
		  WHERE id = $1::uuid`,
		id, u.Overview, u.PremiereDate, u.ProductionYear, u.TmdbPersonID,
		jsonbArg(u.ProductionLocations), jsonbArg(u.Genres), jsonbArg(u.Tags),
		jsonbArg(u.Taglines), jsonbMapArg(u.ProviderIDs))
	return err
}

// jsonbArg 把切片转成 jsonb 入参的 JSON 文本(配合 SQL 里的 $n::jsonb 转换):
// nil 切片 → nil(COALESCE 保留原值);非 nil(含空切片)→ JSON 文本(显式覆盖，空切片写 '[]')。
// 必须传文本而非 []byte —— pgx 会把 []byte 当 bytea 发送，与 jsonb 列类型不匹配。
func jsonbArg(v []string) any {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func jsonbMapArg(v map[string]string) any {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// GetPersonBackdropPath 取 person 背景图路径(serveImage 处理 Backdrop 用)。
func GetPersonBackdropPath(ctx context.Context, pool *pgxpool.Pool, personID string) (string, bool) {
	img, ok, err := repository.NewItemHelperRepository(pool).GetPersonBackdropPath(ctx, personID)
	if err != nil || !ok {
		return "", false
	}
	return img, true
}

// SetPersonBackdrop 写入 person 背景图路径。
func SetPersonBackdrop(ctx context.Context, pool *pgxpool.Pool, personID, path string) error {
	return repository.NewItemHelperRepository(pool).SetPersonBackdrop(ctx, personID, path)
}

// ClearPersonBackdrop 清除 person 背景图路径。
func ClearPersonBackdrop(ctx context.Context, pool *pgxpool.Pool, personID string) error {
	return repository.NewItemHelperRepository(pool).ClearPersonBackdrop(ctx, personID)
}

type PersonListOptions struct {
	Search         string
	NameStartsWith string
	UserID         string
	Filters        []string
	Limit          int64
	Offset         int64
}

func (o PersonListOptions) favoriteOnly() bool {
	for _, f := range o.Filters {
		if strings.EqualFold(strings.TrimSpace(f), "IsFavorite") {
			return true
		}
	}
	return false
}

// ListPersons 列出人物(供 /Persons)。Search=SearchTerm(包含匹配);
// NameStartsWith=Emby 的 NameStartsWith(前缀匹配,mdc-ng 等按名定位演员用)。两者可叠加。
func ListPersons(ctx context.Context, pool *pgxpool.Pool, opts PersonListOptions) ([]Person, int64, error) {
	var total int64
	args := []any{}
	conds := []string{}
	join := ""

	if opts.favoriteOnly() {
		if strings.TrimSpace(opts.UserID) == "" {
			return []Person{}, 0, nil
		}
		args = append(args, opts.UserID)
		join = ` JOIN user_person_data upd
		           ON upd.person_id = p.id
		          AND upd.user_id = $` + strconv.Itoa(len(args)) + `::uuid
		          AND upd.is_favorite = TRUE`
	}
	if opts.Search != "" {
		args = append(args, "%"+opts.Search+"%")
		conds = append(conds, "p.name ILIKE $"+strconv.Itoa(len(args)))
	}
	if opts.NameStartsWith != "" {
		args = append(args, opts.NameStartsWith+"%")
		conds = append(conds, "p.name ILIKE $"+strconv.Itoa(len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	from := ` FROM persons p` + join
	if err := pool.QueryRow(ctx, `SELECT COUNT(*)`+from+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listSQL := `SELECT ` + personColumnsP + from + where + ` ORDER BY p.name`
	listArgs := append([]any{}, args...)
	// limit <= 0 表示不限量（对齐 Emby /Persons 未传 Limit 的语义，返回全部）。
	if opts.Limit > 0 {
		listSQL += " LIMIT $" + strconv.Itoa(len(listArgs)+1)
		listArgs = append(listArgs, opts.Limit)
	}
	listSQL += " OFFSET $" + strconv.Itoa(len(listArgs)+1)
	listArgs = append(listArgs, opts.Offset)

	rows, err := pool.Query(ctx, listSQL, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var result []Person
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, *p)
	}
	return result, total, rows.Err()
}
