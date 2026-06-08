package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/models"
)

// ============ NFO Parser ============

type nfoTagPair struct {
	cdata *regexp.Regexp
	plain *regexp.Regexp
}

var nfoTagRegexes map[string]nfoTagPair

var (
	nfoGenreRe = regexp.MustCompile(`(?i)<genre>([^<]*)</genre>`)
	nfoTagRe   = regexp.MustCompile(`(?i)<tag>([^<]*)</tag>`)
	nfoActorRe = regexp.MustCompile(`(?is)<actor>([\s\S]*?)</actor>`)
	nfoNameRe  = regexp.MustCompile(`(?i)<name>([^<]*)</name>`)
	nfoRoleRe  = regexp.MustCompile(`(?i)<role>([^<]*)</role>`)
	nfoTypeRe  = regexp.MustCompile(`(?i)<type>([^<]*)</type>`)
	nfoTmdbRe  = regexp.MustCompile(`(?i)<tmdbid>([^<]*)</tmdbid>`)

	// Kodi/Jellyfin/tinyMediaManager 的现代标准:<uniqueid type="xxx">123</uniqueid>
	// default="true" / default="false" 等属性可选。
	nfoUniqueIdTmdbRe = regexp.MustCompile(`(?is)<uniqueid\b[^>]*\btype\s*=\s*"tmdb"[^>]*>([^<]*)</uniqueid>`)
	nfoUniqueIdImdbRe = regexp.MustCompile(`(?is)<uniqueid\b[^>]*\btype\s*=\s*"imdb"[^>]*>([^<]*)</uniqueid>`)
	nfoUniqueIdTvdbRe = regexp.MustCompile(`(?is)<uniqueid\b[^>]*\btype\s*=\s*"tvdb"[^>]*>([^<]*)</uniqueid>`)
)

// stripNfoNestedBlocks 把 actor/director/producer/crew/set 等包含嵌套 <tmdbid>/<imdbid>
// 的块从 xml 里剔除,避免顶层正则误匹配演职员的 ID。Kodi/Jellyfin 风格 NFO 里
// 这是必须的 —— 演员列表里的 <tmdbid> 是人物 ID,跟电影 ID 同名不同义。
func stripNfoNestedBlocks(xml string) string {
	xml = nfoActorRe.ReplaceAllString(xml, "")
	// director 在本 NFO 里是 <director tmdbid="...">name</director> 属性形式,
	// 不会被 <tmdbid> 正则抓到;但有的刮削器会生成 <director><name>...</name><tmdbid>...</tmdbid></director>
	// 这种块状格式,同样剔除。
	dirBlock := regexp.MustCompile(`(?is)<director>[\s\S]*?</director>`)
	xml = dirBlock.ReplaceAllString(xml, "")
	return xml
}

func init() {
	tags := []string{"title", "originaltitle", "plot", "tagline", "year", "rating", "tmdbid", "imdbid", "tvdbid", "premiered", "studio", "runtime", "mpaa", "customrating", "trailer"}
	nfoTagRegexes = make(map[string]nfoTagPair, len(tags))
	for _, name := range tags {
		nfoTagRegexes[name] = nfoTagPair{
			cdata: regexp.MustCompile(`(?is)<` + name + `><!\[CDATA\[([\s\S]*?)\]\]></` + name + `>`),
			plain: regexp.MustCompile(`(?i)<` + name + `>([^<]*)</` + name + `>`),
		}
	}
}

func nfoTag(xml, name string) *string {
	pair, ok := nfoTagRegexes[name]
	if !ok {
		return nil
	}
	if m := pair.cdata.FindStringSubmatch(xml); m != nil {
		s := strings.TrimSpace(m[1])
		return &s
	}
	if m := pair.plain.FindStringSubmatch(xml); m != nil {
		s := strings.TrimSpace(m[1])
		return &s
	}
	return nil
}

type NfoData struct {
	Title         *string
	OriginalTitle *string
	Plot          *string
	Year          *int32
	Rating        *float64
	TmdbID        *int32
	ImdbID        *string
	TvdbID        *int32
	Genres         []string
	Tags           []string
	Actors         []NfoActor
	Directors      []string
	Premiered      *string
	Tagline        *string
	Studio         *string
	Runtime        *int32  // 分钟
	OfficialRating *string // mpaa / customrating
	Trailer        *string // 预告片直链(http/https)
}

type NfoActor struct {
	Name     string
	Role     string
	TmdbID   *int32
	ImageURL *string
}

func ParseNfo(nfoPath string) *NfoData {
	data, err := os.ReadFile(nfoPath)
	if err != nil {
		return nil
	}
	xml := string(data)
	if strings.HasPrefix(xml, "\uFEFF") {
		xml = xml[3:]
	}

	// 关键修复:演员块里也带 <tmdbid>/<imdbid>(那是人物 ID),顶层字段提取前
	// 必须先把 <actor>/<director> 块剔除,否则 FindStringSubmatch 会抓到
	// 第一个演员的人物 ID,当成电影 ID 写入 items.tmdb_id。
	xmlTop := stripNfoNestedBlocks(xml)

	result := &NfoData{}

	result.Title = nfoTag(xmlTop, "title")
	result.OriginalTitle = nfoTag(xmlTop, "originaltitle")
	result.Plot = nfoTag(xmlTop, "plot")
	result.Tagline = nfoTag(xmlTop, "tagline")
	if s := nfoTag(xmlTop, "year"); s != nil {
		if v, err := strconv.ParseInt(*s, 10, 32); err == nil {
			i := int32(v)
			result.Year = &i
		}
	}
	if s := nfoTag(xmlTop, "rating"); s != nil {
		if v, err := strconv.ParseFloat(*s, 64); err == nil {
			result.Rating = &v
		}
	}
	// tmdbid:老式 <tmdbid> 优先,fallback 到 <uniqueid type="tmdb">
	if s := nfoTag(xmlTop, "tmdbid"); s != nil {
		if v, err := strconv.ParseInt(*s, 10, 32); err == nil {
			i := int32(v)
			result.TmdbID = &i
		}
	}
	if result.TmdbID == nil {
		if m := nfoUniqueIdTmdbRe.FindStringSubmatch(xmlTop); m != nil {
			if v, err := strconv.ParseInt(strings.TrimSpace(m[1]), 10, 32); err == nil && v > 0 {
				i := int32(v)
				result.TmdbID = &i
			}
		}
	}
	// imdbid:<imdbid> 优先,fallback 到 <uniqueid type="imdb">
	result.ImdbID = nfoTag(xmlTop, "imdbid")
	if result.ImdbID == nil || strings.TrimSpace(*result.ImdbID) == "" {
		if m := nfoUniqueIdImdbRe.FindStringSubmatch(xmlTop); m != nil {
			if s := strings.TrimSpace(m[1]); s != "" {
				result.ImdbID = &s
			}
		}
	}
	if s := nfoTag(xmlTop, "tvdbid"); s != nil {
		if v, err := strconv.ParseInt(*s, 10, 32); err == nil && v > 0 {
			i := int32(v)
			result.TvdbID = &i
		}
	}
	if result.TvdbID == nil {
		if m := nfoUniqueIdTvdbRe.FindStringSubmatch(xmlTop); m != nil {
			if v, err := strconv.ParseInt(strings.TrimSpace(m[1]), 10, 32); err == nil && v > 0 {
				i := int32(v)
				result.TvdbID = &i
			}
		}
	}
	result.Premiered = nfoTag(xmlTop, "premiered")

	// runtime(分钟):仅在解析端取值,落库时只在 runtime_ticks 为空时填(ffprobe 更精确)。
	if s := nfoTag(xmlTop, "runtime"); s != nil {
		if v, err := strconv.ParseInt(strings.TrimSpace(*s), 10, 32); err == nil && v > 0 {
			i := int32(v)
			result.Runtime = &i
		}
	}

	// official_rating:mpaa 优先,回退 customrating。
	if s := nfoTag(xmlTop, "mpaa"); s != nil && strings.TrimSpace(*s) != "" {
		result.OfficialRating = s
	} else if s := nfoTag(xmlTop, "customrating"); s != nil && strings.TrimSpace(*s) != "" {
		result.OfficialRating = s
	}

	// trailer:只保留 http/https 直链,客户端可直接播放。
	if s := nfoTag(xmlTop, "trailer"); s != nil {
		t := strings.TrimSpace(*s)
		if strings.HasPrefix(strings.ToLower(t), "http://") || strings.HasPrefix(strings.ToLower(t), "https://") {
			result.Trailer = &t
		}
	}

	for _, m := range nfoGenreRe.FindAllStringSubmatch(xml, -1) {
		g := strings.TrimSpace(m[1])
		if g != "" {
			result.Genres = append(result.Genres, g)
		}
	}

	// tags:与 genres 分离的标签维度(对齐 Emby Tags)。actor 块里没有 <tag>,用整段 xml 即可。
	seenTag := make(map[string]struct{})
	for _, m := range nfoTagRe.FindAllStringSubmatch(xml, -1) {
		t := strings.TrimSpace(m[1])
		if t == "" {
			continue
		}
		if _, ok := seenTag[t]; ok {
			continue
		}
		seenTag[t] = struct{}{}
		result.Tags = append(result.Tags, t)
	}

	for _, m := range nfoActorRe.FindAllStringSubmatch(xml, -1) {
		block := m[1]
		nameMatch := nfoNameRe.FindStringSubmatch(block)
		if nameMatch == nil {
			continue
		}
		name := strings.TrimSpace(nameMatch[1])

		role := ""
		if rm := nfoRoleRe.FindStringSubmatch(block); rm != nil {
			role = strings.TrimSpace(rm[1])
		}
		atype := "Actor"
		if tm := nfoTypeRe.FindStringSubmatch(block); tm != nil {
			atype = strings.TrimSpace(tm[1])
		}
		var tmdbID *int32
		if tm := nfoTmdbRe.FindStringSubmatch(block); tm != nil {
			if v, err := strconv.ParseInt(strings.TrimSpace(tm[1]), 10, 32); err == nil {
				i := int32(v)
				tmdbID = &i
			}
		}

		if atype == "Director" {
			result.Directors = append(result.Directors, name)
		} else {
			result.Actors = append(result.Actors, NfoActor{Name: name, Role: role, TmdbID: tmdbID})
		}
	}

	dirRe := regexp.MustCompile(`(?i)<director>([^<]*)</director>`)
	for _, m := range dirRe.FindAllStringSubmatch(xml, -1) {
		d := strings.TrimSpace(m[1])
		if d == "" {
			continue
		}
		found := false
		for _, existing := range result.Directors {
			if existing == d {
				found = true
				break
			}
		}
		if !found {
			result.Directors = append(result.Directors, d)
		}
	}

	// Extract first <studio> tag. 命中规范平台名(如 "Youku"->"YOUKU")用规范名,便于进平台虚拟库;
	// 否则保留原值落库(如 "S级素人"),避免外部刮削器给的片商信息丢失。
	if s := nfoTag(xmlTop, "studio"); s != nil {
		raw := strings.TrimSpace(*s)
		if canon := models.CanonicalPlatformName(raw); canon != "" {
			result.Studio = &canon
		} else if raw != "" {
			result.Studio = &raw
		}
	}

	return result
}

// ============ Apply NFO data to DB ============

func ApplyNfoData(ctx context.Context, pool *pgxpool.Pool, itemID string, nfo *NfoData) {
	ApplyNfoDataWithType(ctx, pool, itemID, "", nfo, "")
}

func ApplyNfoDataWithPlatformSource(ctx context.Context, pool *pgxpool.Pool, itemID string, nfo *NfoData, source models.PlatformScanSource) {
	ApplyNfoDataWithType(ctx, pool, itemID, "", nfo, source)
}

// ApplyNfoDataWithType 单 item 元数据落库。整个 Apply 包一个事务,
// 避免原先 20~40 次独立 pool.Exec 带来的 round-trip + WAL sync 风暴。
// itemType 为空时内部会 fallback 查一次 items.type(仅影响 sort_name 是否写入);
// 调用方已知 itemType(比如 applyMergedDetails)应直接传入,省掉这次往返。
func ApplyNfoDataWithType(ctx context.Context, pool *pgxpool.Pool, itemID string, itemType string, nfo *NfoData, source models.PlatformScanSource) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		slog.Warn("[ApplyNfo] begin tx failed", "item_id", itemID, "error", err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	setClauses := make([]string, 0, 10)
	args := make([]any, 0, 10)
	argIdx := 1

	addClause := func(column, castSuffix string, value any) {
		if castSuffix != "" {
			setClauses = append(setClauses, fmt.Sprintf("%s = $%d%s", column, argIdx, castSuffix))
		} else {
			setClauses = append(setClauses, fmt.Sprintf("%s = $%d", column, argIdx))
		}
		args = append(args, value)
		argIdx++
	}

	if nfo.Plot != nil {
		addClause("overview", "", *nfo.Plot)
	}
	if nfo.Rating != nil && *nfo.Rating > 1.0 {
		addClause("community_rating", "", float32(*nfo.Rating))
	}
	if nfo.TmdbID != nil {
		addClause("tmdb_id", "", *nfo.TmdbID)
	}
	if nfo.ImdbID != nil {
		addClause("imdb_id", "", *nfo.ImdbID)
	}
	if nfo.Premiered != nil {
		if premiered := strings.TrimSpace(*nfo.Premiered); premiered != "" {
			addClause("premiere_date", "::date", premiered)
		}
	}
	if nfo.Year != nil {
		addClause("production_year", "", *nfo.Year)
	}
	if nfo.Title != nil {
		addClause("name", "", *nfo.Title)
		effType := itemType
		if effType == "" {
			_ = tx.QueryRow(ctx, "SELECT type FROM items WHERE id = $1::uuid", itemID).Scan(&effType)
		}
		if effType != "Episode" {
			addClause("sort_name", "", strings.ToLower(*nfo.Title))
		}
	}
	if nfo.Tagline != nil {
		addClause("tagline", "", *nfo.Tagline)
	}
	if nfo.OriginalTitle != nil && strings.TrimSpace(*nfo.OriginalTitle) != "" {
		addClause("original_title", "", strings.TrimSpace(*nfo.OriginalTitle))
	}
	if nfo.OfficialRating != nil && strings.TrimSpace(*nfo.OfficialRating) != "" {
		addClause("official_rating", "", strings.TrimSpace(*nfo.OfficialRating))
	}
	if nfo.Trailer != nil && strings.TrimSpace(*nfo.Trailer) != "" {
		addClause("trailer_url", "", strings.TrimSpace(*nfo.Trailer))
	}
	// runtime:仅在 runtime_ticks 为空时用 NFO 填,ffprobe(更精确)之后仍可覆盖。
	if nfo.Runtime != nil && *nfo.Runtime > 0 {
		ticks := int64(*nfo.Runtime) * 60 * 10_000_000
		setClauses = append(setClauses, fmt.Sprintf("runtime_ticks = COALESCE(runtime_ticks, $%d)", argIdx))
		args = append(args, ticks)
		argIdx++
	}
	if nfo.Studio != nil {
		studio := strings.TrimSpace(*nfo.Studio)
		if studio != "" {
			addClause("studio", "", studio)
		}
	}
	// nfo 托管标记与 studio 解耦:只要本次是 NFO 来源(source != "")就标记为已匹配/NFO 托管,
	// 即便片商名是非平台名(studio 仍会落库,只是不进平台虚拟库)。
	if source != "" {
		addClause("platform_scan_status", "", string(models.PlatformScanMatched))
		addClause("platform_scan_source", "", string(source))
		addClause("platform_scan_error", "", nil)
		setClauses = append(setClauses, "platform_scanned_at = NOW()")
	}

	if len(setClauses) > 0 {
		setClauses = append(setClauses, "updated_at = NOW()")
		query := fmt.Sprintf("UPDATE items SET %s WHERE id = $%d::uuid",
			strings.Join(setClauses, ", "), argIdx)
		args = append(args, itemID)
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				// 唯一约束冲突(同库重名等):回滚主事务,用独立连接把 item
				// 打成 error 状态,让未匹配/异常面板可见。
				_ = tx.Rollback(ctx)
				committed = true
				_, markErr := pool.Exec(ctx,
					`UPDATE items
					    SET platform_scan_status = 'error',
					        platform_scan_error  = $1,
					        platform_scanned_at  = NOW(),
					        updated_at           = NOW()
					  WHERE id = $2::uuid`,
					fmt.Sprintf("元数据写入冲突: %s", pgErr.Detail), itemID)
				if markErr != nil {
					slog.Warn("[ApplyNfo] mark error status failed", "item_id", itemID, "error", markErr)
				}
				slog.Warn("[ApplyNfo] unique constraint conflict",
					"item_id", itemID, "constraint", pgErr.ConstraintName, "detail", pgErr.Detail)
				return
			}
			slog.Warn("[ApplyNfo] update items failed", "item_id", itemID, "error", err)
			return
		}
	}

	if len(nfo.Genres) > 0 {
		if _, err := tx.Exec(ctx, "DELETE FROM item_genres WHERE item_id = $1::uuid", itemID); err != nil {
			slog.Warn("[ApplyNfo] delete item_genres failed", "item_id", itemID, "error", err)
			return
		}
		if _, err := tx.Exec(ctx,
			"INSERT INTO genres (name) SELECT unnest($1::text[]) ON CONFLICT (name) DO NOTHING",
			nfo.Genres); err != nil {
			slog.Warn("[ApplyNfo] upsert genres failed", "item_id", itemID, "error", err)
			return
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO item_genres (item_id, genre_id)
			   SELECT $1::uuid, id FROM genres WHERE name = ANY($2::text[])
			 ON CONFLICT DO NOTHING`,
			itemID, nfo.Genres); err != nil {
			slog.Warn("[ApplyNfo] link item_genres failed", "item_id", itemID, "error", err)
			return
		}
	}

	if len(nfo.Tags) > 0 {
		if _, err := tx.Exec(ctx, "DELETE FROM item_tags WHERE item_id = $1::uuid", itemID); err != nil {
			slog.Warn("[ApplyNfo] delete item_tags failed", "item_id", itemID, "error", err)
			return
		}
		if _, err := tx.Exec(ctx,
			"INSERT INTO tags (name) SELECT unnest($1::text[]) ON CONFLICT (name) DO NOTHING",
			nfo.Tags); err != nil {
			slog.Warn("[ApplyNfo] upsert tags failed", "item_id", itemID, "error", err)
			return
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO item_tags (item_id, tag_id)
			   SELECT $1::uuid, id FROM tags WHERE name = ANY($2::text[])
			 ON CONFLICT DO NOTHING`,
			itemID, nfo.Tags); err != nil {
			slog.Warn("[ApplyNfo] link item_tags failed", "item_id", itemID, "error", err)
			return
		}
	}

	if len(nfo.Actors) > 0 || len(nfo.Directors) > 0 {
		existingImages := make(map[string]string)
		rows, qerr := tx.Query(ctx,
			"SELECT name, role, image_url FROM cast_members WHERE item_id = $1::uuid AND image_url IS NOT NULL AND image_url <> ''",
			itemID)
		if qerr == nil {
			for rows.Next() {
				var name, role, imageURL string
				if rows.Scan(&name, &role, &imageURL) == nil {
					existingImages[name+"|"+role] = imageURL
				}
			}
			rows.Close()
		}

		if _, err := tx.Exec(ctx, "DELETE FROM cast_members WHERE item_id = $1::uuid", itemID); err != nil {
			slog.Warn("[ApplyNfo] delete cast_members failed", "item_id", itemID, "error", err)
			return
		}

		itemUUID, perr := uuid.Parse(itemID)
		if perr != nil {
			slog.Warn("[ApplyNfo] parse item uuid failed", "item_id", itemID, "error", perr)
			return
		}

		type castRow struct {
			name, character, role string
			orderIndex            int32
			tmdbID                *int32
			imageURL              *string
		}
		actorLimit := len(nfo.Actors)
		if actorLimit > 20 {
			actorLimit = 20
		}
		castRows := make([]castRow, 0, len(nfo.Directors)+actorLimit)
		for _, dir := range nfo.Directors {
			castRows = append(castRows, castRow{name: dir, role: "Director"})
		}
		for i := 0; i < actorLimit; i++ {
			a := nfo.Actors[i]
			imageURL := a.ImageURL
			if imageURL == nil || *imageURL == "" {
				if existing := existingImages[a.Name+"|Actor"]; existing != "" {
					imageURL = &existing
				}
			}
			castRows = append(castRows, castRow{
				name: a.Name, character: a.Role, role: "Actor",
				orderIndex: int32(i), tmdbID: a.TmdbID, imageURL: imageURL,
			})
		}

		if len(castRows) > 0 {
			if _, err := tx.CopyFrom(ctx,
				pgx.Identifier{"cast_members"},
				[]string{"item_id", "name", "character", "role", "order_index", "tmdb_id", "image_url"},
				pgx.CopyFromSlice(len(castRows), func(i int) ([]any, error) {
					r := castRows[i]
					return []any{itemUUID, r.name, r.character, r.role, r.orderIndex, r.tmdbID, r.imageURL}, nil
				}),
			); err != nil {
				slog.Warn("[ApplyNfo] copy cast_members failed", "item_id", itemID, "error", err)
				return
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		slog.Warn("[ApplyNfo] commit failed", "item_id", itemID, "error", err)
		return
	}
	committed = true
	syncNfoProviderHints(ctx, pool, itemID, nfo)
}

func syncNfoProviderHints(ctx context.Context, pool *pgxpool.Pool, itemID string, nfo *NfoData) {
	if pool == nil || nfo == nil {
		return
	}
	updates := map[string]string{}
	if nfo.TmdbID != nil && *nfo.TmdbID > 0 {
		updates["tmdb"] = strconv.FormatInt(int64(*nfo.TmdbID), 10)
	}
	if nfo.ImdbID != nil {
		if imdb := strings.TrimSpace(*nfo.ImdbID); imdb != "" {
			updates["imdb"] = imdb
		}
	}
	if nfo.TvdbID != nil && *nfo.TvdbID > 0 {
		updates["tvdb"] = strconv.FormatInt(int64(*nfo.TvdbID), 10)
	}
	if len(updates) == 0 {
		return
	}

	var raw []byte
	if err := pool.QueryRow(ctx, "SELECT provider_ids FROM items WHERE id = $1::uuid", itemID).Scan(&raw); err != nil {
		slog.Warn("[ApplyNfo] load provider_ids failed", "item_id", itemID, "error", err)
		return
	}
	merged := map[string]string{}
	mergeProviderIDs(merged, raw)
	for provider, value := range updates {
		merged[provider] = value
	}
	payload, err := json.Marshal(merged)
	if err != nil {
		slog.Warn("[ApplyNfo] marshal provider_ids failed", "item_id", itemID, "error", err)
		return
	}
	if _, err := pool.Exec(ctx,
		"UPDATE items SET provider_ids = $1::jsonb, updated_at = NOW() WHERE id = $2::uuid",
		string(payload), itemID); err != nil {
		slog.Warn("[ApplyNfo] update provider_ids failed", "item_id", itemID, "error", err)
	}
}
