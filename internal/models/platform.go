package models

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PlatformLibrary struct {
	ID             string
	PlatformName   string
	Enabled        bool
	CollectionType string
	IconURL        *string
	CreatedAt      time.Time
	ItemCount      int64
	SortOrder      int
	Dimension      string   // 'studio' | 'num_prefix' | 'actor'
	MatchValue     string   // 主匹配值(唯一键 + VirtualID 用),studio 维度 = platform_name
	MatchValues    []string // 实际聚合的全部匹配值(含主值);空则退化为 [MatchValue]
	CoverImagePath *string
	CoverImageTag  *string
	DisplayName    *string // 用户自定义显示名,非空时优先于 PlatformDisplayName
}

// Values 返回虚拟库实际聚合的匹配值集合;为空时退化为主匹配值。
func (p *PlatformLibrary) Values() []string {
	if len(p.MatchValues) > 0 {
		return p.MatchValues
	}
	return []string{p.MatchValue}
}

// EffectiveDisplayName 返回虚拟库最终展示名:
// 用户自定义 display_name 优先,否则回退内置本地化映射(PlatformDisplayName)。
func (p *PlatformLibrary) EffectiveDisplayName() string {
	if p.DisplayName != nil && strings.TrimSpace(*p.DisplayName) != "" {
		return strings.TrimSpace(*p.DisplayName)
	}
	return PlatformDisplayName(p.PlatformName)
}

// 虚拟库支持的维度常量。
const (
	PlatformDimStudio    = "studio"
	PlatformDimNumPrefix = "num_prefix"
	PlatformDimActor     = "actor"
)

// catalogPrefixExpr 番号前缀(label)派生表达式:去掉结尾的 "-数字",保留前面的整段 label。
// 例:300MIUM-1328 → 300MIUM,IPZZ-857 → IPZZ,326IAV-002 → 326IAV。与 049 迁移的函数索引一致。
const catalogPrefixExpr = "regexp_replace(upper(catalog_number), '-[0-9]+$', '')"

type PlatformScanStatus string

const (
	PlatformScanPending      PlatformScanStatus = "pending"
	PlatformScanMatched      PlatformScanStatus = "matched"
	PlatformScanNoMatch      PlatformScanStatus = "no_match"
	PlatformScanUnidentified PlatformScanStatus = "unidentified"
	PlatformScanError        PlatformScanStatus = "error"
)

type PlatformScanSource string

const (
	PlatformScanSourceTMDB   PlatformScanSource = "tmdb"
	PlatformScanSourceSearch PlatformScanSource = "tmdb_search"
	PlatformScanSourceFile   PlatformScanSource = "filename"
	PlatformScanSourceNFO    PlatformScanSource = "nfo"
	PlatformScanSourceManual PlatformScanSource = "manual"
	PlatformScanSourceLegacy PlatformScanSource = "legacy"
)

type PlatformScanItem struct {
	ID                 string
	ItemType           string
	Name               string
	ProductionYear     *int32
	TmdbID             *int32
	FilePath           *string
	Studio             *string
	PlatformScanStatus PlatformScanStatus
	PlatformScanSource *string
}

func GetPlatformLibraries(ctx context.Context, pool *pgxpool.Pool) ([]PlatformLibrary, error) {
	return listPlatforms(ctx, pool, false)
}

func GetEnabledPlatforms(ctx context.Context, pool *pgxpool.Pool) ([]PlatformLibrary, error) {
	return listPlatforms(ctx, pool, true)
}

func GetEnabledPlatformsLite(ctx context.Context, pool *pgxpool.Pool) ([]PlatformLibrary, error) {
	return listPlatformsLite(ctx, pool, true)
}

// listPlatformsLite 只取虚拟库行,不算 ItemCount(供高频路径如出图解析用)。
func listPlatformsLite(ctx context.Context, pool *pgxpool.Pool, onlyEnabled bool) ([]PlatformLibrary, error) {
	sql := `SELECT id::text, platform_name, enabled, collection_type, icon_url, created_at, sort_order,
	               dimension, COALESCE(match_value, platform_name), match_values, cover_image_path, cover_image_tag, display_name
	          FROM platform_libraries`
	if onlyEnabled {
		sql += ` WHERE enabled = true`
	}
	sql += ` ORDER BY sort_order, platform_name`

	rows, err := pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PlatformLibrary
	for rows.Next() {
		var p PlatformLibrary
		if err := rows.Scan(&p.ID, &p.PlatformName, &p.Enabled, &p.CollectionType, &p.IconURL,
			&p.CreatedAt, &p.SortOrder, &p.Dimension, &p.MatchValue, &p.MatchValues, &p.CoverImagePath, &p.CoverImageTag, &p.DisplayName); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// listPlatforms 取虚拟库行后,按各自维度逐行计数。用于 Views/管理列表(低频)。
func listPlatforms(ctx context.Context, pool *pgxpool.Pool, onlyEnabled bool) ([]PlatformLibrary, error) {
	result, err := listPlatformsLite(ctx, pool, onlyEnabled)
	if err != nil {
		return nil, err
	}
	for i := range result {
		result[i].ItemCount, _ = CountItemsForVirtual(ctx, pool, result[i].Dimension, result[i].Values())
	}
	return result, nil
}

// virtualDimensionCondition 返回某维度的匹配 SQL 片段(占位符 $1 = text[] 多值)与是否合法。
// 用 = ANY($1) 聚合多个值, 解决簡繁/译名拆库。
// 所有路径统一带 type IN ('Movie','Series') AND merged_to_id IS NULL 由调用方拼。
func virtualDimensionCondition(dimension string) (string, bool) {
	switch dimension {
	case PlatformDimStudio:
		return "studio = ANY($1)", true
	case PlatformDimNumPrefix:
		return catalogPrefixExpr + " = ANY($1)", true
	case PlatformDimActor:
		return "EXISTS (SELECT 1 FROM cast_members cm WHERE cm.item_id = items.id AND cm.name = ANY($1) AND cm.role = 'Actor')", true
	default:
		return "", false
	}
}

// VirtualDimensionCondition 暴露维度匹配条件(占位符 $1=text[] 多值)给其他包(如 coverart 取素材)。
func VirtualDimensionCondition(dimension string) (string, bool) {
	return virtualDimensionCondition(dimension)
}

// CountItemsForVirtual 按维度 + 多个匹配值统计顶层影片数。
func CountItemsForVirtual(ctx context.Context, pool *pgxpool.Pool, dimension string, values []string) (int64, error) {
	cond, ok := virtualDimensionCondition(dimension)
	if !ok || len(values) == 0 {
		return 0, nil
	}
	var count int64
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM items WHERE `+cond+` AND type IN ('Movie','Series') AND merged_to_id IS NULL`,
		values).Scan(&count)
	return count, err
}

// DiscoveredValue 是维度发现结果的一项。
type DiscoveredValue struct {
	Value        string `json:"Value"`
	Count        int64  `json:"Count"`
	AlreadyAdded bool   `json:"AlreadyAdded"`
}

// DiscoverDimensionValues 扫描本地数据,列出某维度的 distinct 值 + 计数(供用户勾选添加)。
// search 用 ILIKE 过滤,minCount 过滤低频项,按计数倒序。标注是否已加入虚拟库。
func DiscoverDimensionValues(ctx context.Context, pool *pgxpool.Pool, dimension, search string, minCount int64) ([]DiscoveredValue, error) {
	var groupExpr, fromWhere string
	switch dimension {
	case PlatformDimStudio:
		groupExpr = "studio"
		fromWhere = `FROM items WHERE studio IS NOT NULL AND studio != '' AND type IN ('Movie','Series') AND merged_to_id IS NULL`
	case PlatformDimNumPrefix:
		groupExpr = catalogPrefixExpr
		fromWhere = `FROM items WHERE catalog_number IS NOT NULL AND type IN ('Movie','Series') AND merged_to_id IS NULL`
	case PlatformDimActor:
		groupExpr = "cm.name"
		fromWhere = `FROM cast_members cm JOIN items i ON i.id = cm.item_id
		             WHERE cm.role = 'Actor' AND cm.name != '' AND i.type IN ('Movie','Series') AND i.merged_to_id IS NULL`
	default:
		return nil, fmt.Errorf("unknown dimension: %s", dimension)
	}

	countExpr := "COUNT(*)"
	if dimension == PlatformDimActor {
		countExpr = "COUNT(DISTINCT cm.item_id)"
	}

	args := []interface{}{}
	idx := 1
	having := ""
	searchClause := ""
	search = strings.TrimSpace(search)
	if search != "" {
		searchClause = fmt.Sprintf(" AND %s ILIKE $%d", groupExpr, idx)
		args = append(args, "%"+search+"%")
		idx++
	}
	if minCount > 1 {
		having = fmt.Sprintf(" HAVING %s >= $%d", countExpr, idx)
		args = append(args, minCount)
		idx++
	}

	sql := fmt.Sprintf(
		`SELECT %s AS v, %s AS cnt %s%s GROUP BY %s%s ORDER BY cnt DESC, v ASC LIMIT 2000`,
		groupExpr, countExpr, fromWhere, searchClause, groupExpr, having)

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []DiscoveredValue
	for rows.Next() {
		var v *string
		var cnt int64
		if err := rows.Scan(&v, &cnt); err != nil {
			return nil, err
		}
		if v == nil || strings.TrimSpace(*v) == "" {
			continue
		}
		result = append(result, DiscoveredValue{Value: *v, Count: cnt})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 标注已加入(同维度同值)。
	added, _ := getAddedMatchValues(ctx, pool, dimension)
	for i := range result {
		if _, ok := added[result[i].Value]; ok {
			result[i].AlreadyAdded = true
		}
	}
	return result, nil
}

func getAddedMatchValues(ctx context.Context, pool *pgxpool.Pool, dimension string) (map[string]struct{}, error) {
	// 多值聚合后,某 discover 值只要是任一虚拟库的别名(在 match_values 里)就算"已加"。
	rows, err := pool.Query(ctx,
		`SELECT unnest(COALESCE(match_values, ARRAY[COALESCE(match_value, platform_name)]))
		   FROM platform_libraries WHERE dimension = $1`, dimension)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]struct{})
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err == nil {
			m[v] = struct{}{}
		}
	}
	return m, rows.Err()
}

func SetPlatformEnabled(ctx context.Context, pool *pgxpool.Pool, platformName string, enabled bool) error {
	_, err := pool.Exec(ctx,
		`UPDATE platform_libraries SET enabled = $1 WHERE platform_name = $2`,
		enabled, platformName)
	return err
}

// SetPlatformEnabledByID 按 id 启用/停用(多维度下显示名可能重复,优先用 id)。
func SetPlatformEnabledByID(ctx context.Context, pool *pgxpool.Pool, id string, enabled bool) error {
	_, err := pool.Exec(ctx,
		`UPDATE platform_libraries SET enabled = $1 WHERE id = $2::uuid`,
		enabled, id)
	return err
}

// UpdatePlatformSortOrder assigns sort_order by the position of each id in orderedIDs.
func UpdatePlatformSortOrder(ctx context.Context, pool *pgxpool.Pool, orderedIDs []string) error {
	for i, id := range orderedIDs {
		if _, err := pool.Exec(ctx,
			`UPDATE platform_libraries SET sort_order = $1 WHERE id = $2::uuid`,
			i, id); err != nil {
			return err
		}
	}
	return nil
}

// AddPlatformLibrary 新增一个虚拟库。dimension 为空时默认 studio;displayName 为空时取 matchValue。
func AddPlatformLibrary(ctx context.Context, pool *pgxpool.Pool, dimension, matchValue, displayName string, enabled bool) error {
	dimension = strings.TrimSpace(dimension)
	if dimension == "" {
		dimension = PlatformDimStudio
	}
	matchValue = strings.TrimSpace(matchValue)
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = matchValue
	}
	_, err := pool.Exec(ctx,
		`INSERT INTO platform_libraries (platform_name, dimension, match_value, match_values, enabled)
		 VALUES ($1, $2, $3, ARRAY[$3], $4)
		 ON CONFLICT (dimension, match_value) DO NOTHING`,
		displayName, dimension, matchValue, enabled)
	return err
}

// GetPlatformByID 取单个虚拟库(含维度/匹配值/封面),不填 ItemCount。
func GetPlatformByID(ctx context.Context, pool *pgxpool.Pool, id string) (*PlatformLibrary, error) {
	var p PlatformLibrary
	err := pool.QueryRow(ctx,
		`SELECT id::text, platform_name, enabled, collection_type, icon_url, created_at, sort_order,
		        dimension, COALESCE(match_value, platform_name), match_values, cover_image_path, cover_image_tag, display_name
		   FROM platform_libraries WHERE id = $1::uuid`, id).
		Scan(&p.ID, &p.PlatformName, &p.Enabled, &p.CollectionType, &p.IconURL,
			&p.CreatedAt, &p.SortOrder, &p.Dimension, &p.MatchValue, &p.MatchValues, &p.CoverImagePath, &p.CoverImageTag, &p.DisplayName)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SetPlatformCover 记录某虚拟库生成的封面路径 + tag。
func SetPlatformCover(ctx context.Context, pool *pgxpool.Pool, id, path, tag string) error {
	_, err := pool.Exec(ctx,
		`UPDATE platform_libraries SET cover_image_path = $1, cover_image_tag = $2 WHERE id = $3::uuid`,
		path, tag, id)
	return err
}

// ClearPlatformCover 清除虚拟库生成的封面(回退内置 logo / 默认渐变),
// 返回被清除的封面文件路径(若有)供调用方删除磁盘文件。
func ClearPlatformCover(ctx context.Context, pool *pgxpool.Pool, id string) (string, error) {
	var oldPath *string
	_ = pool.QueryRow(ctx,
		`SELECT cover_image_path FROM platform_libraries WHERE id = $1::uuid`, id).Scan(&oldPath)
	_, err := pool.Exec(ctx,
		`UPDATE platform_libraries SET cover_image_path = NULL, cover_image_tag = NULL WHERE id = $1::uuid`, id)
	if oldPath != nil {
		return *oldPath, err
	}
	return "", err
}

// RenamePlatform 设置虚拟库的自定义显示名;name 为空串则清除,回退默认本地化名。
func RenamePlatform(ctx context.Context, pool *pgxpool.Pool, id, name string) error {
	name = strings.TrimSpace(name)
	var val interface{}
	if name != "" {
		val = name
	}
	_, err := pool.Exec(ctx,
		`UPDATE platform_libraries SET display_name = $1 WHERE id = $2::uuid`, val, id)
	return err
}

// AddPlatformValues 把若干匹配值合并进某虚拟库(去重),实现"多个簡繁/译名聚合到一个库"。
func AddPlatformValues(ctx context.Context, pool *pgxpool.Pool, id string, values []string) error {
	clean := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			clean = append(clean, v)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	_, err := pool.Exec(ctx,
		`UPDATE platform_libraries
		    SET match_values = ARRAY(
		      SELECT DISTINCT v
		        FROM unnest(COALESCE(match_values, ARRAY[match_value]) || $2::text[]) AS v
		       WHERE v IS NOT NULL AND v <> ''
		    )
		  WHERE id = $1::uuid`,
		id, clean)
	return err
}

// RemovePlatformValue 从某虚拟库移除一个匹配值(不会移除主匹配值 match_value)。
func RemovePlatformValue(ctx context.Context, pool *pgxpool.Pool, id, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	_, err := pool.Exec(ctx,
		`UPDATE platform_libraries
		    SET match_values = array_remove(COALESCE(match_values, ARRAY[match_value]), $2)
		  WHERE id = $1::uuid AND match_value <> $2`,
		id, value)
	return err
}

func DeletePlatformLibrary(ctx context.Context, pool *pgxpool.Pool, id string) error {
	_, err := pool.Exec(ctx, `DELETE FROM platform_libraries WHERE id = $1::uuid`, id)
	return err
}

// IsPlatformLibrariesEnabled checks the global toggle.
func IsPlatformLibrariesEnabled(ctx context.Context, pool *pgxpool.Pool) bool {
	var val *string
	_ = pool.QueryRow(ctx, "SELECT value FROM system_config WHERE key = 'platform_libraries_enabled'").Scan(&val)
	return val != nil && *val == "true"
}

// platformNamespace is a fixed UUID namespace for generating deterministic platform virtual IDs.
var platformNamespace = uuid.MustParse("a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d")

// PlatformVirtualID returns a deterministic UUID for a (dimension, matchValue) pair,
// compatible with Emby clients that require valid UUIDs. 加入 dimension 避免不同维度同名值撞 ID。
func PlatformVirtualID(dimension, matchValue string) string {
	return uuid.NewSHA1(platformNamespace, []byte(dimension+"\x00"+matchValue)).String()
}

// ResolvePlatformVirtualID 检查某 ID 是否属于已启用的虚拟库,命中返回整行(供出图取 cover/显示名)。
func ResolvePlatformVirtualID(ctx context.Context, pool *pgxpool.Pool, id string) (*PlatformLibrary, bool) {
	if !IsPlatformLibrariesEnabled(ctx, pool) {
		return nil, false
	}
	// 高频路径(出图等),用 lite 版避免逐行 count。
	platforms, err := listPlatformsLite(ctx, pool, true)
	if err != nil {
		return nil, false
	}
	for i := range platforms {
		if PlatformVirtualID(platforms[i].Dimension, platforms[i].MatchValue) == id {
			return &platforms[i], true
		}
	}
	return nil, false
}

// PlatformCollectionType returns the appropriate collection type based on item distribution.
// 同时包含电影和剧集时返回空串(Emby 混合内容库语义),调用方据此省略 CollectionType,
// 否则客户端会把整库当成电影库, 只显示电影而隐藏剧集。
func PlatformCollectionType(ctx context.Context, pool *pgxpool.Pool, dimension string, values []string) string {
	cond, ok := virtualDimensionCondition(dimension)
	if !ok || len(values) == 0 {
		return ""
	}
	var movieCount, seriesCount int64
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM items WHERE `+cond+` AND type = 'Movie' AND merged_to_id IS NULL`, values).Scan(&movieCount)
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM items WHERE `+cond+` AND type = 'Series' AND merged_to_id IS NULL`, values).Scan(&seriesCount)
	switch {
	case seriesCount > 0 && movieCount == 0:
		return "tvshows"
	case movieCount > 0 && seriesCount == 0:
		return "movies"
	default:
		return ""
	}
}

// PlatformVirtualIDHash returns a deterministic emby-compatible numeric hash for a platform.
func PlatformVirtualIDHash(platformName string) string {
	h := sha256.Sum256([]byte("platform:" + platformName))
	return fmt.Sprintf("%x", h[:16])
}

// GetItemCountByStudio returns number of top-level items (Movie/Series) for a studio.
func GetItemCountByStudio(ctx context.Context, pool *pgxpool.Pool, studio string) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM items WHERE studio = $1 AND type IN ('Movie', 'Series') AND merged_to_id IS NULL`,
		studio).Scan(&count)
	return count, err
}

// GradientColor defines a 3-stop gradient: [0%, 40%, 100%] as RGB.
type GradientColor [3][3]uint8

// PlatformLogoEntry maps keyword patterns to embedded logo filenames and gradient colors.
type PlatformLogoEntry struct {
	Keywords []string
	File     string
	Gradient GradientColor
}

var defaultGradient = GradientColor{{0x1a, 0x1a, 0x2e}, {0x1e, 0x29, 0x3b}, {0x33, 0x41, 0x55}}

// PlatformLogoMap is the list of all known platform → logo mappings.
var PlatformLogoMap = []PlatformLogoEntry{
	{[]string{"netflix"}, "logo/Netflix-iOS-1024x1024.png", GradientColor{{0x1a, 0x00, 0x00}, {0x3d, 0x00, 0x00}, {0x8b, 0x00, 0x00}}},
	{[]string{"disney"}, "logo/Disney+-iOS-1024x1024.png", GradientColor{{0x02, 0x00, 0x24}, {0x04, 0x0e, 0x50}, {0x0d, 0x1b, 0x63}}},
	{[]string{"amazon", "prime video"}, "logo/Amazon Prime Video-iOS-1024x1024.png", GradientColor{{0x00, 0x1a, 0x2e}, {0x00, 0x30, 0x50}, {0x00, 0x66, 0x8a}}},
	{[]string{"apple tv", "apple"}, "logo/Apple TV-iOS-1024x1024.png", GradientColor{{0x0a, 0x0a, 0x0a}, {0x1a, 0x1a, 0x1a}, {0x1d, 0x1d, 0x1f}}},
	{[]string{"hulu"}, "logo/Hulu_ Stream TV shows & movies-iOS-1024x1024.png", GradientColor{{0x00, 0x1a, 0x0a}, {0x00, 0x3c, 0x15}, {0x0a, 0x5c, 0x25}}},
	{[]string{"paramount"}, "logo/Paramount+-iOS-1024x1024.png", GradientColor{{0x00, 0x0a, 0x2e}, {0x00, 0x1b, 0x5e}, {0x00, 0x40, 0xb0}}},
	{[]string{"peacock"}, "logo/Peacock TV_ Stream TV & Movies-iOS-1024x1024.png", GradientColor{{0x0a, 0x0a, 0x0a}, {0x1a, 0x10, 0x20}, {0x2a, 0x1a, 0x3a}}},
	{[]string{"hbo"}, "logo/HBO Max_ Stream Movies & TV-iOS-1024x1024.png", GradientColor{{0x0a, 0x0a, 0x0a}, {0x1a, 0x1a, 0x2e}, {0x2d, 0x2d, 0x3f}}},
	{[]string{"bilibili", "哔哩哔哩"}, "logo/bilibili - All Your Fav Videos-iOS-1024x1024.png", GradientColor{{0x2e, 0x05, 0x10}, {0x50, 0x10, 0x20}, {0x8a, 0x20, 0x3a}}},
	{[]string{"iqiyi", "爱奇艺"}, "logo/iQIYI - Dramas, Anime, Shows-iOS-1024x1024.png", GradientColor{{0x00, 0x1a, 0x08}, {0x00, 0x40, 0x18}, {0x00, 0x80, 0x30}}},
	{[]string{"tencent", "腾讯"}, "logo/Tencent Video-iOS-1024x1024.png", GradientColor{{0x10, 0x18, 0x00}, {0x28, 0x38, 0x00}, {0x50, 0x68, 0x10}}},
	{[]string{"youku", "优酷"}, "logo/YOUKU-Drama, Film, Show, Anime-iOS-1024x1024.png", GradientColor{{0x10, 0x15, 0x2e}, {0x1a, 0x30, 0x50}, {0x30, 0x58, 0x80}}},
	{[]string{"mango", "芒果"}, "logo/Mango TV.png", GradientColor{{0x2e, 0x16, 0x00}, {0x5a, 0x30, 0x00}, {0x9a, 0x5a, 0x00}}},
	{[]string{"tvn"}, "logo/tvN.png", GradientColor{{0x2a, 0x00, 0x10}, {0x52, 0x00, 0x22}, {0x8a, 0x12, 0x3c}}},
	{[]string{"hunan", "湖南"}, "logo/Hunan Television.png", GradientColor{{0x2e, 0x1e, 0x00}, {0x55, 0x3c, 0x00}, {0x8a, 0x66, 0x10}}},
	{[]string{"cctv"}, "logo/CCTV-8.png", GradientColor{{0x2a, 0x00, 0x00}, {0x52, 0x06, 0x06}, {0x8a, 0x12, 0x12}}},
	{[]string{"tvb"}, "logo/TVB Jade.png", GradientColor{{0x00, 0x1a, 0x1c}, {0x00, 0x32, 0x38}, {0x00, 0x56, 0x60}}},
	{[]string{"tokyo mx", "tokyo metropolitan"}, "logo/Tokyo MX.png", GradientColor{{0x00, 0x1c, 0x12}, {0x00, 0x3a, 0x28}, {0x06, 0x62, 0x46}}},
	{[]string{"tv tokyo"}, "logo/TV Tokyo.png", GradientColor{{0x06, 0x0c, 0x22}, {0x12, 0x16, 0x40}, {0x24, 0x20, 0x58}}},
	{[]string{"sbs"}, "logo/SBS.png", GradientColor{{0x00, 0x10, 0x2e}, {0x00, 0x22, 0x56}, {0x00, 0x3e, 0x8c}}},
	{[]string{"fuji"}, "logo/Fuji TV.png", GradientColor{{0x00, 0x14, 0x26}, {0x00, 0x2c, 0x4c}, {0x00, 0x4c, 0x7c}}},
}

// PlatformLogoEntry lookup by name.
func findPlatformEntry(platformName string) *PlatformLogoEntry {
	lower := strings.ToLower(platformName)
	for i := range PlatformLogoMap {
		for _, kw := range PlatformLogoMap[i].Keywords {
			if strings.Contains(lower, kw) {
				return &PlatformLogoMap[i]
			}
		}
	}
	return nil
}

// PlatformLogoFile returns the embedded file path for a platform, or "" if none found.
func PlatformLogoFile(platformName string) string {
	if e := findPlatformEntry(platformName); e != nil {
		return e.File
	}
	return ""
}

// HasPlatformLogo returns true if a logo exists for the given platform name.
func HasPlatformLogo(platformName string) bool {
	return findPlatformEntry(platformName) != nil
}

// PlatformGradient returns the gradient colors for a platform.
func PlatformGradient(platformName string) GradientColor {
	if e := findPlatformEntry(platformName); e != nil {
		return e.Gradient
	}
	return defaultGradient
}

func normalizePlatformName(name string) string {
	if e := findPlatformEntry(name); e != nil && len(e.Keywords) > 0 {
		switch e.Keywords[0] {
		case "netflix":
			return "Netflix"
		case "disney":
			return "Disney+"
		case "amazon", "prime video":
			return "Amazon"
		case "apple tv", "apple":
			return "Apple TV+"
		case "hulu":
			return "Hulu"
		case "paramount":
			return "Paramount+"
		case "peacock":
			return "Peacock"
		case "hbo":
			return "HBO"
		case "bilibili", "哔哩哔哩":
			return "bilibili"
		case "iqiyi", "爱奇艺":
			return "iQIYI"
		case "tencent", "腾讯":
			return "Tencent Video"
		case "youku", "优酷":
			return "YOUKU"
		case "mango", "芒果":
			return "Mango TV"
		}
	}
	return strings.TrimSpace(name)
}

// PlatformDisplayName returns a localized display name for known platforms,
// keeping the canonical (matching) studio name unchanged. Falls back to the
// canonical name when no localized name is defined.
func PlatformDisplayName(canonical string) string {
	switch canonical {
	case "Tencent Video":
		return "腾讯视频"
	case "iQIYI":
		return "爱奇艺"
	case "YOUKU":
		return "优酷"
	case "bilibili":
		return "哔哩哔哩"
	case "Mango TV":
		return "芒果TV"
	}
	return canonical
}

func CanonicalPlatformName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	lower := strings.ToLower(name)
	for _, entry := range PlatformLogoMap {
		for _, kw := range entry.Keywords {
			if strings.Contains(lower, kw) {
				return normalizePlatformName(kw)
			}
		}
	}
	return name
}

// UpdateItemStudio sets the studio field and unified platform scan state for an item.
func UpdateItemStudio(ctx context.Context, pool *pgxpool.Pool, itemID, studio string) error {
	studio = CanonicalPlatformName(studio)
	if studio == "" {
		return MarkPlatformScanNoMatch(ctx, pool, itemID, PlatformScanSourceTMDB, "")
	}
	return MarkPlatformScanMatched(ctx, pool, itemID, studio, PlatformScanSourceTMDB)
}

// PropagateStudioToChildren sets studio/state on child items (Season/Episode) of a Series.
func PropagateStudioToChildren(ctx context.Context, pool *pgxpool.Pool, seriesID, studio string) error {
	studio = CanonicalPlatformName(studio)
	_, err := pool.Exec(ctx,
		`UPDATE items
		   SET studio = $1,
		       platform_scan_status = 'matched',
		       platform_scan_source = COALESCE(platform_scan_source, 'tmdb'),
		       platform_scan_error = NULL,
		       platform_scanned_at = NOW()
		 WHERE (series_id = $2::uuid OR parent_id = $2::uuid)
		   AND type IN ('Season', 'Episode')
		   AND (platform_scan_status <> 'matched' OR studio IS DISTINCT FROM $1)`,
		studio, seriesID)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`UPDATE items
		   SET studio = $1,
		       platform_scan_status = 'matched',
		       platform_scan_source = COALESCE(platform_scan_source, 'tmdb'),
		       platform_scan_error = NULL,
		       platform_scanned_at = NOW()
		 WHERE parent_id IN (
		   SELECT id FROM items WHERE parent_id = $2::uuid AND type = 'Season'
		 ) AND type = 'Episode'
		   AND (platform_scan_status <> 'matched' OR studio IS DISTINCT FROM $1)`,
		studio, seriesID)
	return err
}

func MarkPlatformScanPending(ctx context.Context, pool *pgxpool.Pool, itemID string) error {
	_, err := pool.Exec(ctx,
		`UPDATE items
		    SET platform_scan_status = 'pending',
		        platform_scan_error = NULL
		  WHERE id = $1::uuid`,
		itemID)
	return err
}

func MarkPlatformScanMatched(ctx context.Context, pool *pgxpool.Pool, itemID, studio string, source PlatformScanSource) error {
	studio = CanonicalPlatformName(studio)
	_, err := pool.Exec(ctx,
		`UPDATE items
		    SET studio = $1,
		        platform_scan_status = 'matched',
		        platform_scan_source = $2,
		        platform_scan_error = NULL,
		        platform_scanned_at = NOW()
		  WHERE id = $3::uuid`,
		studio, string(source), itemID)
	return err
}

func MarkPlatformScanNoMatch(ctx context.Context, pool *pgxpool.Pool, itemID string, source PlatformScanSource, errMsg string) error {
	var errorVal interface{}
	if strings.TrimSpace(errMsg) != "" {
		errorVal = strings.TrimSpace(errMsg)
	}
	_, err := pool.Exec(ctx,
		`UPDATE items
		    SET studio = NULL,
		        platform_scan_status = 'no_match',
		        platform_scan_source = $1,
		        platform_scan_error = $2,
		        platform_scanned_at = NOW()
		  WHERE id = $3::uuid`,
		string(source), errorVal, itemID)
	return err
}

func MarkPlatformScanUnidentified(ctx context.Context, pool *pgxpool.Pool, itemID string, source PlatformScanSource, errMsg string) error {
	var errorVal interface{}
	if strings.TrimSpace(errMsg) != "" {
		errorVal = strings.TrimSpace(errMsg)
	}
	_, err := pool.Exec(ctx,
		`UPDATE items
		    SET studio = NULL,
		        platform_scan_status = 'unidentified',
		        platform_scan_source = $1,
		        platform_scan_error = $2,
		        platform_scanned_at = NOW()
		  WHERE id = $3::uuid`,
		string(source), errorVal, itemID)
	return err
}

func MarkPlatformScanError(ctx context.Context, pool *pgxpool.Pool, itemID string, source PlatformScanSource, errMsg string) error {
	_, err := pool.Exec(ctx,
		`UPDATE items
		    SET platform_scan_status = 'error',
		        platform_scan_source = $1,
		        platform_scan_error = $2,
		        platform_scanned_at = NOW()
		  WHERE id = $3::uuid`,
		string(source), strings.TrimSpace(errMsg), itemID)
	return err
}

func GetItemsPendingPlatformScan(ctx context.Context, pool *pgxpool.Pool, limit int, requireTMDB bool, includeNoMatch bool) ([]PlatformScanItem, error) {
	statuses := []string{string(PlatformScanPending), string(PlatformScanUnidentified), string(PlatformScanError)}
	if includeNoMatch {
		statuses = append(statuses, string(PlatformScanNoMatch))
	}

	sql := `SELECT id::text, type, name, production_year, tmdb_id, file_path, studio, platform_scan_status, platform_scan_source
	          FROM items
	         WHERE type IN ('Movie', 'Series')
	           AND platform_scan_status = ANY($1)`
	args := []interface{}{statuses}
	if requireTMDB {
		sql += ` AND tmdb_id IS NOT NULL`
	}
	sql += ` ORDER BY platform_scanned_at NULLS FIRST, updated_at DESC NULLS LAST`
	if limit > 0 {
		sql += ` LIMIT $2`
		args = append(args, limit)
	}

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PlatformScanItem
	for rows.Next() {
		var item PlatformScanItem
		if err := rows.Scan(
			&item.ID,
			&item.ItemType,
			&item.Name,
			&item.ProductionYear,
			&item.TmdbID,
			&item.FilePath,
			&item.Studio,
			&item.PlatformScanStatus,
			&item.PlatformScanSource,
		); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func CountItemsPendingPlatformScan(ctx context.Context, pool *pgxpool.Pool, requireTMDB bool, includeNoMatch bool) (int64, error) {
	statuses := []string{string(PlatformScanPending), string(PlatformScanUnidentified), string(PlatformScanError)}
	if includeNoMatch {
		statuses = append(statuses, string(PlatformScanNoMatch))
	}
	sql := `SELECT COUNT(*) FROM items WHERE type IN ('Movie', 'Series') AND platform_scan_status = ANY($1)`
	args := []interface{}{statuses}
	if requireTMDB {
		sql += ` AND tmdb_id IS NOT NULL`
	}
	var count int64
	err := pool.QueryRow(ctx, sql, args...).Scan(&count)
	return count, err
}

func CountItemsPendingPlatformMetadataScrape(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var count int64
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM items
		  WHERE type IN ('Movie', 'Series')
		    AND tmdb_id IS NULL
		    AND platform_scan_status IN ('pending', 'unidentified', 'error')`).Scan(&count)
	return count, err
}
