package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"fyms/internal/dto"
	"fyms/internal/middleware"
	"fyms/internal/models"
	"fyms/internal/services"
)

func getItemCounts(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	var movie, series, episodes int64
	where := "type = $1"
	var allowed []string
	if auth := middleware.GetAuthUser(c); auth != nil && !auth.IsAdmin && !strings.HasPrefix(auth.ID, "api-key-") {
		scope, err := loadUserLibraryScope(ctx, state, auth.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		if !scope.AllowAll {
			allowed = scope.IDs
			if len(allowed) == 0 {
				where += " AND FALSE"
			} else {
				where += " AND library_id::text = ANY($2)"
			}
		}
	}
	countType := func(itemType string) int64 {
		var n int64
		if allowed != nil && len(allowed) > 0 {
			_ = state.DB.QueryRow(ctx, "SELECT COUNT(*) FROM items WHERE "+where, itemType, allowed).Scan(&n)
		} else {
			_ = state.DB.QueryRow(ctx, "SELECT COUNT(*) FROM items WHERE "+where, itemType).Scan(&n)
		}
		return n
	}
	movie = countType("Movie")
	series = countType("Series")
	episodes = countType("Episode")
	c.JSON(http.StatusOK, gin.H{
		"MovieCount":      movie,
		"SeriesCount":     series,
		"EpisodeCount":    episodes,
		"ArtistCount":     0,
		"ProgramCount":    0,
		"TrailerCount":    0,
		"SongCount":       0,
		"AlbumCount":      0,
		"MusicVideoCount": 0,
		"BoxSetCount":     0,
		"BookCount":       0,
		"ItemCount":       movie + series + episodes,
	})
}

func getPersons(c *gin.Context, state *AppState) {
	start := int64(0)
	limit := int64(50)
	if v := c.Query("StartIndex"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			start = n
		}
	}
	if v := c.Query("Limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			limit = n
		}
	}

	search := strings.TrimSpace(c.Query("SearchTerm"))
	persons, total, err := models.ListPersons(c.Request.Context(), state.DB, search, limit, start)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	items := make([]gin.H, 0, len(persons))
	for i := range persons {
		p := persons[i]
		hasImage := (p.ImagePath != nil && *p.ImagePath != "")
		item := gin.H{
			"Name":      p.Name,
			"Id":        p.ID,
			"Type":      "Person",
			"ServerId":  state.Config.ServerID,
			"ImageTags": gin.H{},
			"IsFolder":  false,
		}
		if hasImage {
			tag := p.ImageTag
			if tag == "" {
				tag = p.ID
			}
			item["ImageTags"] = gin.H{"Primary": tag}
			item["PrimaryImageTag"] = tag
		}
		if p.Overview != nil && *p.Overview != "" {
			item["Overview"] = *p.Overview
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"Items": items, "TotalRecordCount": total})
}

func deviceInfo(c *gin.Context, state *AppState) {
	deviceID := c.Query("Id")
	sessions := state.SessionManager.GetActiveSessions()
	var found *services.ActiveSession
	for i := range sessions {
		if sessions[i].DeviceID == deviceID {
			found = &sessions[i]
			break
		}
	}
	name := "Unknown"
	appName := "Unknown"
	userName := ""
	userID := ""
	lastActivity := ""
	if found != nil {
		name = found.DeviceName
		appName = found.AppName
		userName = found.UserName
		userID = found.UserID
		lastActivity = found.LastActivity.UTC().Format("2006-01-02T15:04:05.0000000Z")
	}
	c.JSON(http.StatusOK, gin.H{
		"Id":               deviceID,
		"Name":             name,
		"AppName":          appName,
		"LastUserName":     userName,
		"LastUserId":       userID,
		"DateLastActivity": lastActivity,
	})
}

func compatQueryAny(c *gin.Context, keys ...string) string {
	for _, k := range keys {
		if v := c.Query(k); v != "" {
			return v
		}
	}
	return ""
}

func itemsSearch(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()

	ids := compatQueryAny(c, "Ids", "ids")
	searchTerm := compatQueryAny(c, "SearchTerm", "searchTerm", "searchterm")
	includeTypes := compatQueryAny(c, "IncludeItemTypes", "includeItemTypes", "includeitemtypes")
	fields := compatQueryAny(c, "Fields", "fields")
	parentID := compatQueryAny(c, "ParentId", "parentId", "parentid")
	recStr := compatQueryAny(c, "Recursive", "recursive")
	recursive := strings.EqualFold(recStr, "true") || recStr == "1"
	limitStr := compatQueryAny(c, "Limit", "limit")
	limitVal := int64(50)
	if limitStr != "" {
		if n, err := strconv.ParseInt(limitStr, 10, 64); err == nil && n > 0 {
			limitVal = n
		}
	}
	startIndex := int64(0)
	if v := compatQueryAny(c, "StartIndex", "startIndex", "startindex"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			startIndex = n
		}
	}

	useEmbyID := false
	if ids != "" {
		parts := strings.Split(ids, ",")
		allInt := true
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, err := strconv.ParseInt(p, 10, 64); err != nil {
				allInt = false
				break
			}
		}
		useEmbyID = allInt
	}

	auth := middleware.GetAuthUser(c)
	var authUserID string
	if auth != nil && !strings.HasPrefix(auth.ID, "api-key-") {
		authUserID = auth.ID
	}
	var scope *userLibraryScope
	if authUserID != "" {
		var err error
		scope, err = loadUserLibraryScope(ctx, state, authUserID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}

	// Build query with LEFT JOIN user_item_data to avoid N+1
	userCols := "NULL::bigint AS playback_position_ticks, 0::int AS play_count, FALSE AS is_favorite, FALSE AS played, NULL::timestamp AS last_played_date"
	userJoin := ""
	var args []interface{}
	idx := 1
	if authUserID != "" {
		userCols = "uid.playback_position_ticks, uid.play_count, uid.is_favorite, uid.played, uid.last_played_date"
		userJoin = fmt.Sprintf(" LEFT JOIN user_item_data uid ON i.id = uid.item_id AND uid.user_id = $%d::uuid", idx)
		args = append(args, authUserID)
		idx++
	}

	baseCols := `i.id, i.name, i.type, i.sort_name, NULL::text AS collection_type, i.overview,
		i.production_year, i.premiere_date, i.community_rating, i.official_rating,
		i.runtime_ticks, i.index_number, i.parent_index_number, i.parent_id,
		i.series_id, i.series_name, i.season_id, i.container, i.file_path,
		i.resolved_path, i.provider_ids, i.primary_image_tag, i.backdrop_image_tag,
		NULL::bigint AS child_count, NULL::bigint AS recursive_item_count,
		i.tagline, i.studio, i.created_at, i.emby_id,
		i.merged_to_id, i.primary_image_path, i.updated_at`

	seriesCols := `, sf.primary_image_tag AS series_primary_image_tag, sf.backdrop_image_tag AS series_backdrop_image_tag, sf.id AS series_fallback_id`
	seriesJoin := " LEFT JOIN items sf ON sf.id = COALESCE(i.series_id, CASE WHEN i.type = 'Season' THEN i.parent_id END)"

	// Start with no merge filter; platform queries use global primaries while
	// ordinary user-library queries use a per-library representative selection.
	sql := fmt.Sprintf("SELECT %s%s, %s FROM items i%s%s WHERE 1=1", baseCols, seriesCols, userCols, userJoin, seriesJoin)

	var whereParts []string
	useRepresentative := false

	if ids != "" {
		idList := strings.Split(ids, ",")
		var placeholders []string
		for _, id := range idList {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if useEmbyID {
				placeholders = append(placeholders, "$"+strconv.Itoa(idx)+"::int")
			} else {
				placeholders = append(placeholders, "$"+strconv.Itoa(idx)+"::uuid")
			}
			args = append(args, id)
			idx++
		}
		if useEmbyID {
			whereParts = append(whereParts, "i.emby_id IN ("+strings.Join(placeholders, ",")+")")
		} else {
			whereParts = append(whereParts, "i.id IN ("+strings.Join(placeholders, ",")+")")
		}
	}
	if parentID != "" {
		if p, ok := models.ResolvePlatformVirtualID(ctx, state.DB, parentID); ok {
			if scope != nil && !scope.AllowAll {
				c.JSON(http.StatusOK, gin.H{"Items": []interface{}{}, "TotalRecordCount": 0})
				return
			}
			switch p.Dimension {
			case models.PlatformDimActor:
				whereParts = append(whereParts, "EXISTS (SELECT 1 FROM cast_members cm WHERE cm.item_id = i.id AND cm.name = $"+strconv.Itoa(idx)+" AND cm.role = 'Actor')")
			case models.PlatformDimNumPrefix:
				whereParts = append(whereParts, "regexp_replace(upper(i.catalog_number), '-[0-9]+$', '') = $"+strconv.Itoa(idx))
			default:
				whereParts = append(whereParts, "i.studio = $"+strconv.Itoa(idx))
			}
			args = append(args, p.MatchValue)
			idx++
			// Only filter merged items in platform library queries
			whereParts = append(whereParts, "i.merged_to_id IS NULL")
			if includeTypes == "" {
				whereParts = append(whereParts, "i.type IN ('Movie','Series')")
			}
		} else {
			pid, _ := models.ResolveToUUID(ctx, state.DB, parentID)
			if pid != nil {
				useRepresentative = true
				if recursive {
					whereParts = append(whereParts, "i.library_id = $"+strconv.Itoa(idx)+"::uuid")
				} else {
					whereParts = append(whereParts, "i.parent_id = $"+strconv.Itoa(idx)+"::uuid")
				}
				args = append(args, *pid)
				idx++
			}
		}
	}
	if scope != nil && !scope.AllowAll {
		if len(scope.IDs) == 0 {
			whereParts = append(whereParts, "FALSE")
		} else {
			whereParts = append(whereParts, "i.library_id::text = ANY($"+strconv.Itoa(idx)+")")
			args = append(args, scope.IDs)
			idx++
		}
	}
	if includeTypes != "" {
		validTypes := map[string]bool{"Movie": true, "Series": true, "Episode": true, "Season": true}
		typeMap := map[string]string{"Video": "Movie", "Folder": "CollectionFolder"}
		typeList := strings.Split(includeTypes, ",")
		seen := map[string]bool{}
		var placeholders []string
		for _, t := range typeList {
			// 先按 itemTypeCanonical 规范化大小写,Lenna 等客户端会传 "movie" 小写,
			// 直接精确匹配 i.type='movie' 会查不到记录。
			t = normalizeItemType(strings.TrimSpace(t))
			if t == "" {
				continue
			}
			if mapped, ok := typeMap[t]; ok {
				t = mapped
			}
			if t == "Person" || t == "CollectionFolder" {
				continue
			}
			if !validTypes[t] || seen[t] {
				continue
			}
			seen[t] = true
			placeholders = append(placeholders, "$"+strconv.Itoa(idx))
			args = append(args, t)
			idx++
		}
		if len(placeholders) > 0 {
			whereParts = append(whereParts, "i.type IN ("+strings.Join(placeholders, ",")+")")
		} else {
			whereParts = append(whereParts, "i.type IN ('Movie', 'Series', 'Episode')")
		}
	}
	if searchTerm != "" {
		whereParts = append(whereParts, "i.name ILIKE $"+strconv.Itoa(idx))
		args = append(args, "%"+searchTerm+"%")
		idx++
	}

	// AnyProviderIdEquals=tmdb.755898 —— 聚合类客户端按外部站点 ID 跨源匹配。
	// 大小写不敏感匹配 provider key、精确匹配 id 值;多个之间 OR。whereParts 会同时
	// 作用于主查询 / count / representative CTE,这里只需追加一项。
	if s := strings.TrimSpace(compatQueryAny(c, "AnyProviderIdEquals", "anyProviderIdEquals", "anyprovideridequals")); s != "" {
		var ors []string
		for _, raw := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ',' }) {
			raw = strings.TrimSpace(raw)
			dot := strings.Index(raw, ".")
			if dot <= 0 || dot >= len(raw)-1 {
				continue
			}
			provider := strings.ToLower(strings.TrimSpace(raw[:dot]))
			id := strings.TrimSpace(raw[dot+1:])
			if provider == "" || id == "" {
				continue
			}
			ors = append(ors, fmt.Sprintf(
				"EXISTS (SELECT 1 FROM jsonb_each_text(i.provider_ids) pe WHERE LOWER(pe.key) = $%d AND pe.value = $%d)",
				idx, idx+1))
			args = append(args, provider, id)
			idx += 2
		}
		if len(ors) > 0 {
			whereParts = append(whereParts, "i.provider_ids IS NOT NULL AND jsonb_typeof(i.provider_ids) = 'object' AND ("+strings.Join(ors, " OR ")+")")
		}
	}

	if len(whereParts) > 0 {
		sql += " AND " + strings.Join(whereParts, " AND ")
	}

	countTarget := "COUNT(*)"
	if useRepresentative {
		countTarget = "COUNT(DISTINCT " + modelsMergedRepresentativeExpr("i") + ")"
	}
	countSQL := "SELECT " + countTarget + " FROM items i" + userJoin + " WHERE 1=1"
	if len(whereParts) > 0 {
		countSQL += " AND " + strings.Join(whereParts, " AND ")
	}
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	var totalCount int64
	_ = state.DB.QueryRow(ctx, countSQL, countArgs...).Scan(&totalCount)

	if useRepresentative {
		sql = fmt.Sprintf(
			`WITH filtered AS (
				SELECT %s%s, %s, %s AS merge_group_key
				FROM items i%s%s
				WHERE 1=1%s
			), ranked AS (
				SELECT filtered.*,
					ROW_NUMBER() OVER (
						PARTITION BY merge_group_key
						ORDER BY
							CASE WHEN filtered.merged_to_id IS NULL THEN 0 ELSE 1 END,
							CASE WHEN filtered.primary_image_tag IS NOT NULL THEN 0 ELSE 1 END,
							CASE WHEN filtered.primary_image_path IS NOT NULL AND filtered.primary_image_path <> '' THEN 0 ELSE 1 END,
							CASE WHEN filtered.overview IS NOT NULL AND filtered.overview <> '' THEN 0 ELSE 1 END,
							filtered.updated_at DESC,
							filtered.id
					) AS merge_row_num
				FROM filtered
			)
			SELECT * FROM ranked WHERE merge_row_num = 1`,
			baseCols, seriesCols, userCols, modelsMergedRepresentativeExpr("i"), userJoin, seriesJoin, whereSuffix(whereParts))
		sql += " ORDER BY ranked.sort_name"
	} else {
		sql += " ORDER BY i.sort_name"
	}
	sql += " LIMIT $" + strconv.Itoa(idx) + "::bigint"
	args = append(args, limitVal)
	idx++
	if startIndex > 0 {
		sql += " OFFSET $" + strconv.Itoa(idx) + "::bigint"
		args = append(args, startIndex)
		idx++
	}

	rows, err := state.DB.Query(ctx, sql, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	needMediaSources := strings.Contains(fields, "MediaSources") || strings.Contains(fields, "Path")
	needGenres := strings.Contains(fields, "Genres")
	needPeople := strings.Contains(fields, "People")

	var items []gin.H
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			continue
		}
		fds := rows.FieldDescriptions()
		m := make(map[string]interface{})
		for i, fd := range fds {
			m[string(fd.Name)] = vals[i]
		}

		row := models.MapColsToItemRow(m)
		itemID := row.ID
		if itemID == "" {
			continue
		}

		ud := models.MapColsToUserDataRow(m)
		var udPtr *dto.UserDataRow
		if ud.PlaybackPositionTicks != nil || ud.IsFavorite != nil {
			udPtr = &ud
		}

		d := dto.FormatItemDtoList(&row, state.Config.ServerID, udPtr)
		result := dtoToMap(d)

		if embyID, ok := m["emby_id"]; ok && embyID != nil {
			result["EmbyId"] = embyID
			if useEmbyID {
				result["Id"] = fmt.Sprintf("%v", embyID)
			}
		}

		if row.ItemType == "Movie" || row.ItemType == "Episode" {
			if needMediaSources {
				sources := buildItemMediaSources(ctx, state, itemID, &row)
				if len(sources) > 0 {
					hideMediaSourceSizeForInfuse(c, sources)
					result["MediaSources"] = sources
					result["MediaStreams"] = sources[0].MediaStreams
					if strings.TrimSpace(sources[0].Path) != "" {
						result["Path"] = sources[0].Path
					}
					if strings.TrimSpace(sources[0].Container) != "" {
						result["Container"] = sources[0].Container
					}
				}
			}
			// Emby standard: MediaSourceCount tells clients how many versions exist.
			// Only set when > 1 (matches Jellyfin DtoService behavior).
			msc := models.GetMediaSourceCount(ctx, state.DB, itemID)
			if msc > 1 {
				result["MediaSourceCount"] = msc
			}
		}

		if needGenres {
			genres, _ := models.GetItemGenres(ctx, state.DB, itemID)
			genreNames := make([]string, 0, len(genres))
			for _, g := range genres {
				genreNames = append(genreNames, g[1])
			}
			result["Genres"] = genreNames
		}
		if needPeople {
			cast, _ := models.GetItemCast(ctx, state.DB, itemID)
			if cast != nil {
				result["People"] = cast
			} else {
				result["People"] = []interface{}{}
			}
		}

		items = append(items, result)
	}
	if items == nil {
		items = []gin.H{}
	}
	c.JSON(http.StatusOK, gin.H{"Items": items, "TotalRecordCount": totalCount})
}

func uuidToString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case [16]byte:
		u, err := uuid.FromBytes(t[:])
		if err != nil {
			return ""
		}
		return u.String()
	default:
		if s, ok := v.(interface{ String() string }); ok {
			return s.String()
		}
		return ""
	}
}

func modelsMergedRepresentativeExpr(itemAlias string) string {
	return fmt.Sprintf(
		"CASE WHEN %s.type = 'Movie' THEN COALESCE(%s.merged_to_id::text, %s.id::text) ELSE %s.id::text END",
		itemAlias, itemAlias, itemAlias, itemAlias,
	)
}

func whereSuffix(whereParts []string) string {
	if len(whereParts) == 0 {
		return ""
	}
	return " AND " + strings.Join(whereParts, " AND ")
}

func dtoToMap(d dto.BaseItemDto) gin.H {
	b, err := json.Marshal(d)
	if err != nil {
		return gin.H{"Id": d.ID, "Name": d.Name, "Type": d.Type}
	}
	var m gin.H
	if err := json.Unmarshal(b, &m); err != nil {
		return gin.H{"Id": d.ID, "Name": d.Name, "Type": d.Type}
	}
	return m
}

func searchHints(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()

	searchTerm := compatQueryAny(c, "SearchTerm", "searchTerm", "searchterm")
	if searchTerm == "" {
		c.JSON(http.StatusOK, gin.H{"SearchHints": []interface{}{}, "TotalRecordCount": 0})
		return
	}

	limitVal := int64(20)
	if v := compatQueryAny(c, "Limit", "limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			limitVal = n
		}
	}
	startIndex := int64(0)
	if v := compatQueryAny(c, "StartIndex", "startIndex", "startindex"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			startIndex = n
		}
	}

	includeTypes := compatQueryAny(c, "IncludeItemTypes", "includeItemTypes", "includeitemtypes")

	args := []interface{}{"%" + searchTerm + "%"}
	idx := 2

	whereExtra := ""
	if includeTypes != "" {
		typeList := strings.Split(includeTypes, ",")
		var placeholders []string
		for _, t := range typeList {
			// 规范化大小写, 与 parseItemQueryOptions 行为一致, Lenna 等客户端
			// 传 "movie" 小写时仍能命中 SQL 精确匹配 i.type='Movie'.
			t = normalizeItemType(strings.TrimSpace(t))
			if t == "" {
				continue
			}
			placeholders = append(placeholders, "$"+strconv.Itoa(idx))
			args = append(args, t)
			idx++
		}
		if len(placeholders) > 0 {
			whereExtra = " AND i.type IN (" + strings.Join(placeholders, ",") + ")"
		}
	} else {
		whereExtra = " AND i.type IN ('Movie', 'Series', 'Episode')"
	}

	countSQL := "SELECT COUNT(*) FROM items i WHERE i.name ILIKE $1" + whereExtra
	var totalCount int64
	_ = state.DB.QueryRow(ctx, countSQL, args...).Scan(&totalCount)

	sql := `SELECT i.id, i.name, i.type, i.production_year,
		i.primary_image_tag, i.backdrop_image_tag,
		i.series_id, i.series_name, i.runtime_ticks,
		i.index_number, i.parent_index_number, i.community_rating,
		sf.primary_image_tag AS series_primary_image_tag,
		sf.backdrop_image_tag AS series_backdrop_image_tag,
		sf.id AS series_fallback_id
		FROM items i
		LEFT JOIN items sf ON sf.id = COALESCE(i.series_id, CASE WHEN i.type = 'Season' THEN i.parent_id END)
		WHERE i.name ILIKE $1` + whereExtra

	sql += " ORDER BY CASE WHEN i.name ILIKE $" + strconv.Itoa(idx) + " THEN 0 ELSE 1 END, i.type, i.sort_name"
	args = append(args, searchTerm)
	idx++
	sql += " LIMIT $" + strconv.Itoa(idx) + "::bigint"
	args = append(args, limitVal)
	idx++
	if startIndex > 0 {
		sql += " OFFSET $" + strconv.Itoa(idx) + "::bigint"
		args = append(args, startIndex)
		idx++
	}

	rows, err := state.DB.Query(ctx, sql, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	var hints []gin.H
	for rows.Next() {
		var id, name, itemType string
		var prodYear *int32
		var primaryTag, backdropTag, seriesID, seriesName *string
		var runtimeTicks *int64
		var indexNum, parentIndexNum *int32
		var rating *float64
		var seriesPrimaryTag, seriesBackdropTag, seriesFallbackID *string
		if err := rows.Scan(&id, &name, &itemType, &prodYear, &primaryTag, &backdropTag, &seriesID, &seriesName, &runtimeTicks, &indexNum, &parentIndexNum, &rating, &seriesPrimaryTag, &seriesBackdropTag, &seriesFallbackID); err != nil {
			continue
		}

		mediaType := "Video"
		hint := gin.H{
			"Id":        id,
			"ItemId":    id,
			"Name":      name,
			"Type":      itemType,
			"MediaType": mediaType,
			"ServerId":  state.Config.ServerID,
		}
		if prodYear != nil {
			hint["ProductionYear"] = *prodYear
		}
		if runtimeTicks != nil {
			hint["RunTimeTicks"] = *runtimeTicks
		}
		if primaryTag != nil {
			hint["PrimaryImageTag"] = *primaryTag
			hint["ThumbImageTag"] = *primaryTag
		} else if (itemType == "Episode" || itemType == "Season") && seriesPrimaryTag != nil {
			hint["PrimaryImageTag"] = *seriesPrimaryTag
			hint["ThumbImageTag"] = *seriesPrimaryTag
			if seriesFallbackID != nil {
				hint["PrimaryImageItemId"] = *seriesFallbackID
				hint["ThumbImageItemId"] = *seriesFallbackID
			}
		}
		if backdropTag != nil {
			hint["BackdropImageTag"] = *backdropTag
		} else if (itemType == "Episode" || itemType == "Season") && seriesBackdropTag != nil {
			hint["BackdropImageTag"] = *seriesBackdropTag
			if seriesFallbackID != nil {
				hint["BackdropImageItemId"] = *seriesFallbackID
			}
		}
		if seriesName != nil {
			hint["Series"] = *seriesName
		}
		if indexNum != nil {
			hint["IndexNumber"] = *indexNum
		}
		if parentIndexNum != nil {
			hint["ParentIndexNumber"] = *parentIndexNum
		}
		if rating != nil {
			hint["CommunityRating"] = *rating
		}

		isFolder := itemType == "Series" || itemType == "Season" || itemType == "CollectionFolder"
		hint["IsFolder"] = isFolder

		hints = append(hints, hint)
	}
	if hints == nil {
		hints = []gin.H{}
	}
	c.JSON(http.StatusOK, gin.H{"SearchHints": hints, "TotalRecordCount": totalCount})
}

// hideMediaSourceSizeForInfuse 暂不隐藏 MediaSource.Size,用于验证 Infuse 在
