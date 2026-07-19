package models

import (
	"github.com/jackc/pgx/v5"

	"fyms/internal/dto"
)

type ProviderIDMatch struct {
	Provider string // 已小写化的 provider 名,如 "tmdb" / "imdb" / "tvdb"
	ID       string // 该 provider 下的 id 值,如 "755898"
}

type ItemQueryOptions struct {
	ParentID          *string
	ParentIDs         []string // 多库聚合;非空时覆盖 ParentID 单值
	ParentLibraryID   *string  // ParentId 指向物理库根;非递归查顶层,递归查全库
	RecursiveParentID *string  // ParentId 指向普通 Folder;递归查该 Folder 子树
	IncludeItemTypes  []string
	SortBy            *string
	SortOrder         *string
	Limit             *int64
	StartIndex        *int64
	Recursive         bool
	LibraryID         *string
	SearchTerm        *string
	NameStartsWith    *string
	Filters           []string
	UserID            *string
	GenreIDs          []string
	GenreNames        []string
	TagIDs            []int
	TagNames          []string
	PersonIDs         []string
	PersonNames       []string
	PersonTypes       []string
	Years             []int
	Studio            []string          // 片商维度虚拟库:命中任一值(= ANY)
	ActorName         []string          // 演员维度虚拟库:含任一演员(role='Actor')的影片
	CatalogPrefix     []string          // 番号前缀维度虚拟库:命中任一番号字母前缀
	LatestItemLimit   *int64            // 最新影片虚拟库:固定取 created_at 倒序的前 N 部
	AnyProviderID     []ProviderIDMatch // 任一匹配即命中(OR);空则不过滤
	HasSubtitles      *bool             // Emby/Jellyfin 查询参数 HasSubtitles
	AllowedLibraryIDs []string          // 用户可访问的物理库;nil 表示不限制,空切片表示无可访问库
	LightMode         bool              // 跳过 series_fallback JOIN，用于大批量列表
}

type QueryResult struct {
	Items      []pgx.Rows
	TotalCount int64
	Rows       []map[string]interface{}
}

type ItemQueryResult struct {
	Items      []dto.ItemRow
	UserData   []dto.UserDataRow
	TotalCount int64
}
