package models

import (
	"context"
	"encoding/json"

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

func personFromRepo(row repository.PersonRow) Person {
	var p Person
	p.ID = row.ID
	p.Name = row.Name
	p.ImagePath = row.ImagePath
	p.ImageLocked = row.ImageLocked
	p.TmdbPersonID = row.TmdbPersonID
	p.Overview = row.Overview
	p.ImageTag = row.ImageTag
	p.PremiereDate = row.PremiereDate
	p.ProductionYear = row.ProductionYear
	p.BackdropPath = row.BackdropPath
	_ = json.Unmarshal([]byte(row.ProductionLocations), &p.ProductionLocations)
	_ = json.Unmarshal([]byte(row.Genres), &p.Genres)
	_ = json.Unmarshal([]byte(row.Tags), &p.Tags)
	_ = json.Unmarshal([]byte(row.Taglines), &p.Taglines)
	_ = json.Unmarshal([]byte(row.ProviderIDs), &p.ProviderIDs)
	return p
}

func personFromRepoPtr(row *repository.PersonRow, err error) (*Person, error) {
	if err != nil || row == nil {
		return nil, err
	}
	p := personFromRepo(*row)
	return &p, nil
}

func personsFromRepo(rows []repository.PersonRow) []Person {
	out := make([]Person, 0, len(rows))
	for _, row := range rows {
		out = append(out, personFromRepo(row))
	}
	return out
}

// EnsurePersonsForItem 为某 item 下还没有 person_id 的 cast_members 建立/关联 persons。
// 幂等:persons.name 唯一,重复姓名只会命中已有行。在 ApplyNfo 事务内调用。
func EnsurePersonsForItem(ctx context.Context, db personDBTX, itemID string) error {
	return repository.EnsurePersonsForItem(ctx, db, itemID)
}

// PropagateCastImagesToPersons 把某 item 下 cast_members.image_url 提升为
// persons.image_path 的初始值 —— 仅当 person 未锁定且还没有头像时。
// 用于 NFO thumb / 本地 .actors 扫描:写完 cast_members 后让全局头像跟上。
func PropagateCastImagesToPersons(ctx context.Context, db personDBTX, itemID string) error {
	return repository.PropagateCastImagesToPersons(ctx, db, itemID)
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
	rows, err := repository.NewPersonRepository(pool).ListMissingImage(ctx, limit)
	if err != nil {
		return nil, err
	}
	return personsFromRepo(rows), nil
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
	return personFromRepoPtr(repository.NewPersonRepository(pool).GetByName(ctx, name))
}

// GetPersonByID 按 person id 取单个 person（Emby 里 person 也是 item，
// `GET /Items/{personId}` 复用此与 GetPersonByName 同构的详情）。未命中或 id 非法返回 (nil, nil)。
func GetPersonByID(ctx context.Context, pool *pgxpool.Pool, id string) (*Person, error) {
	return personFromRepoPtr(repository.NewPersonRepository(pool).GetByID(ctx, id))
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
	return repository.NewPersonRepository(pool).UpdateMetadata(ctx, id, repository.PersonMetadataUpdate{
		Overview:            u.Overview,
		PremiereDate:        u.PremiereDate,
		ProductionYear:      u.ProductionYear,
		TmdbPersonID:        u.TmdbPersonID,
		ProductionLocations: jsonbArg(u.ProductionLocations),
		Genres:              jsonbArg(u.Genres),
		Tags:                jsonbArg(u.Tags),
		Taglines:            jsonbArg(u.Taglines),
		ProviderIDs:         jsonbMapArg(u.ProviderIDs),
	})
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

// ListPersons 列出人物(供 /Persons)。Search=SearchTerm(包含匹配);
// NameStartsWith=Emby 的 NameStartsWith(前缀匹配,mdc-ng 等按名定位演员用)。两者可叠加。
func ListPersons(ctx context.Context, pool *pgxpool.Pool, opts PersonListOptions) ([]Person, int64, error) {
	rows, total, err := repository.NewPersonRepository(pool).List(ctx, repository.PersonListOptions{
		Search:         opts.Search,
		NameStartsWith: opts.NameStartsWith,
		UserID:         opts.UserID,
		Filters:        opts.Filters,
		Limit:          opts.Limit,
		Offset:         opts.Offset,
	})
	if err != nil {
		return nil, 0, err
	}
	return personsFromRepo(rows), total, nil
}
