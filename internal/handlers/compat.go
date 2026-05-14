package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"fyms/internal/dto"
	"fyms/internal/middleware"
	"fyms/internal/models"
	"fyms/internal/services"
)

// RegisterCompatRoutes registers Emby-compatible endpoints used by third-party clients and plugins.
func RegisterCompatRoutes(group *gin.RouterGroup, state *AppState, authMW, adminMW, optAuthMW gin.HandlerFunc) {
	_ = optAuthMW

	group.GET("/Sessions", adminMW, func(c *gin.Context) { getSessions(c, state) })
	group.GET("/DisplayPreferences/usersettings", getDisplayPrefs)
	group.POST("/DisplayPreferences/usersettings", postDisplayPrefs)
	group.GET("/DisplayPreferences/:id", getDisplayPrefs)
	group.POST("/DisplayPreferences/:id", postDisplayPrefs)

	group.GET("/Plugins", stubPlugins)
	group.GET("/Channels", stubItemsEmpty)
	group.GET("/Shows/NextUp", authMW, stubItemsEmpty)
	group.GET("/Studios", authMW, stubItemsEmpty)
	group.GET("/Artists", authMW, stubItemsEmpty)

	group.GET("/LiveTv/Info", stubLiveTv)
	group.GET("/LiveTv/Channels", stubItemsEmpty)
	group.GET("/LiveTv/Programs", stubItemsEmpty)

	group.GET("/Notifications", emptyJSONArray)
	group.GET("/Notifications/Types", emptyJSONArray)
	group.GET("/Notifications/:userId/Summary", stubNotifications)

	group.GET("/Shows/:seriesId/Seasons", authMW, func(c *gin.Context) { getSeasons(c, state) })
	group.GET("/Shows/:seriesId/Episodes", authMW, func(c *gin.Context) { getEpisodes(c, state) })

	group.POST("/Auth/Keys", adminMW, func(c *gin.Context) { createApiKey(c, state) })
	group.GET("/Auth/Keys", adminMW, func(c *gin.Context) { listApiKeys(c, state) })
	group.DELETE("/Auth/Keys/:keyId", adminMW, func(c *gin.Context) { deleteApiKey(c, state) })
	group.POST("/ApiKeys", adminMW, func(c *gin.Context) { createApiKey(c, state) })
	group.GET("/ApiKeys", adminMW, func(c *gin.Context) { listApiKeys(c, state) })
	group.DELETE("/ApiKeys/:keyId", adminMW, func(c *gin.Context) { deleteApiKey(c, state) })

	group.GET("/Items/Counts", authMW, func(c *gin.Context) { getItemCounts(c, state) })
	group.GET("/Items", authMW, func(c *gin.Context) { itemsSearch(c, state) })
	group.GET("/Devices", emptyJSONArray)
	group.GET("/Devices/Info", authMW, func(c *gin.Context) { deviceInfo(c, state) })
	group.GET("/Search/Hints", authMW, func(c *gin.Context) { searchHints(c, state) })

	group.POST("/Sessions/:sessionId/Playing/Stop", adminMW, func(c *gin.Context) { sessionStop(c, state) })
	group.POST("/Sessions/:sessionId/Message", authMW, sessionMessage)
	group.POST("/Sessions/Capabilities/Full", emptyOK)

	group.POST("/user_usage_stats/submit_custom_query", adminMW, func(c *gin.Context) { submitCustomQuery(c, state) })
	group.GET("/Persons", authMW, func(c *gin.Context) { getPersons(c, state) })
}

func emptyJSONArray(c *gin.Context) {
	c.JSON(http.StatusOK, []interface{}{})
}

func emptyOK(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

func stubPlugins(c *gin.Context) {
	c.JSON(http.StatusOK, []interface{}{})
}

func stubItemsEmpty(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"Items": []interface{}{}, "TotalRecordCount": 0})
}

func stubLiveTv(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"Services": []interface{}{}, "IsEnabled": false, "EnabledUsers": []interface{}{}})
}

func stubNotifications(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"UnreadCount": 0, "MaxUnreadCount": 0})
}

func getSessions(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	sessions := state.SessionManager.GetActiveSessions()
	out := make([]gin.H, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, formatEmbySessionInfo(ctx, s, state))
	}
	c.JSON(http.StatusOK, out)
}

func formatEmbySessionInfo(ctx context.Context, s services.ActiveSession, state *AppState) gin.H {
	h := gin.H{
		"Id":                 s.UserID + "_" + s.DeviceID,
		"UserId":             s.UserID,
		"UserName":           s.UserName,
		"Client":             s.AppName,
		"DeviceId":           s.DeviceID,
		"DeviceName":         s.DeviceName,
		"ApplicationVersion": s.AppVersion,
		"ServerId":           state.Config.ServerID,
		"RemoteEndPoint":     s.ClientIP,
		"LastActivityDate":   s.LastActivity.UTC().Format("2006-01-02T15:04:05.0000000Z"),
	}
	if s.NowPlaying != nil {
		np := s.NowPlaying
		item := gin.H{
			"Id":           np.ItemID,
			"Name":         np.ItemName,
			"Type":         np.ItemType,
			"ServerId":     state.Config.ServerID,
			"RunTimeTicks": np.RuntimeTicks,
		}
		if np.SeriesName != nil {
			item["SeriesName"] = *np.SeriesName
		}
		if np.SeasonIndex != nil {
			item["ParentIndexNumber"] = *np.SeasonIndex
		}
		if np.EpisodeIndex != nil {
			item["IndexNumber"] = *np.EpisodeIndex
		}
		if np.PrimaryImageItemID != nil {
			item["PrimaryImageItemId"] = *np.PrimaryImageItemID
		}

		streams, err := models.GetMediaStreams(ctx, state.DB, np.ItemID)
		if err == nil && len(streams) > 0 {
			ms := make([]gin.H, 0, len(streams))
			for i := range streams {
				s := &streams[i]
				entry := gin.H{
					"Type":         s.StreamType,
					"Codec":        ptrOrEmpty(s.Codec),
					"DisplayTitle": ptrOrEmpty(s.DisplayTitle),
					"IsDefault":    s.IsDefault != nil && *s.IsDefault,
				}
				if s.Width != nil {
					entry["Width"] = *s.Width
				}
				if s.Height != nil {
					entry["Height"] = *s.Height
				}
				if s.BitRate != nil {
					entry["BitRate"] = *s.BitRate
				}
				if s.Channels != nil {
					entry["Channels"] = *s.Channels
				}
				ms = append(ms, entry)
			}
			item["MediaStreams"] = ms
		}

		var container string
		var bitrate *int32
		err = state.DB.QueryRow(ctx,
			"SELECT container, bitrate FROM media_versions WHERE item_id = $1::uuid AND is_primary = true LIMIT 1",
			np.ItemID).Scan(&container, &bitrate)
		if err == nil {
			item["Container"] = container
			item["Bitrate"] = bitrate
		}

		h["NowPlayingItem"] = item
		h["PlayState"] = gin.H{
			"IsPaused":      np.IsPaused,
			"PositionTicks": np.PositionTicks,
			"CanSeek":       true,
			"PlayMethod":    "DirectPlay",
		}
	} else {
		h["PlayState"] = gin.H{
			"IsPaused":      false,
			"PositionTicks": int64(0),
			"CanSeek":       true,
		}
	}
	return h
}

func ptrOrEmpty(p *string) string {
	if p != nil {
		return *p
	}
	return ""
}

func getDisplayPrefs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"Id":               "usersettings",
		"SortBy":           "SortName",
		"SortOrder":        "Ascending",
		"RememberIndexing": false,
		"RememberSorting":  false,
		"CustomPrefs":      gin.H{},
	})
}

func postDisplayPrefs(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

func getSeasons(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	seriesID := c.Param("seriesId")
	suid, err := models.ResolveToUUID(ctx, state.DB, seriesID)
	if err != nil || suid == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid series id"})
		return
	}

	seriesRow, _ := models.GetItemByID(ctx, state.DB, *suid)
	var seriesImageTag, seriesBackdropTag, seriesNameVal *string
	if seriesRow != nil {
		seriesImageTag = seriesRow.PrimaryImageTag
		seriesBackdropTag = seriesRow.BackdropImageTag
		seriesNameVal = &seriesRow.Name
	}

	rows, err := state.DB.Query(ctx,
		`SELECT id FROM items WHERE parent_id = $1::uuid AND type = 'Season' ORDER BY index_number NULLS LAST, sort_name ASC`,
		*suid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	var items []dto.BaseItemDto
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		row, err := models.GetItemByID(ctx, state.DB, id)
		if err != nil || row == nil {
			continue
		}
		d := dto.FormatItemDto(row, state.Config.ServerID, nil)
		d.SeriesID = suid
		if d.SeriesName == nil {
			d.SeriesName = seriesNameVal
		}
		childCount, _ := models.GetChildCount(ctx, state.DB, id)
		d.ChildCount = &childCount

		if len(d.ImageTags) == 0 && seriesImageTag != nil {
			d.SeriesPrimaryImageTag = seriesImageTag
			d.SeriesPrimaryImageItemID = suid
			d.ParentPrimaryImageItemID = suid
			d.ParentPrimaryImageTag = seriesImageTag
			d.ParentThumbItemID = suid
			d.ParentThumbImageTag = seriesImageTag
		}
		if len(d.BackdropImageTags) == 0 && seriesBackdropTag != nil {
			d.ParentBackdropItemID = suid
			d.ParentBackdropImageTags = []string{*seriesBackdropTag}
		}
		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Items": items, "TotalRecordCount": len(items)})
}

func getEpisodes(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	seriesID := c.Param("seriesId")
	seasonID := c.Query("SeasonId")
	if seasonID == "" {
		seasonID = c.Query("seasonId")
	}

	suid, err := models.ResolveToUUID(ctx, state.DB, seriesID)
	if err != nil || suid == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid series id"})
		return
	}

	auth := middleware.GetAuthUser(c)
	userID := ""
	if quid := c.Query("UserId"); quid != "" {
		userID = quid
	} else if quid := c.Query("userId"); quid != "" {
		userID = quid
	} else if auth != nil && !strings.HasPrefix(auth.ID, "api-key-") {
		userID = auth.ID
	}
	if userID != "" {
		if _, err := uuid.Parse(userID); err != nil {
			userID = ""
		}
	}

	limit := int64(0)
	if v := c.Query("Limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			limit = n
		}
	}
	startIndex := int64(0)
	if v := c.Query("StartIndex"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			startIndex = n
		}
	}

	var countSQL, itemSQL string
	var bindID string
	if seasonID != "" {
		sid, rerr := models.ResolveToUUID(ctx, state.DB, seasonID)
		if rerr != nil || sid == nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Invalid SeasonId"})
			return
		}
		bindID = *sid
		countSQL = "SELECT COUNT(*) FROM items WHERE parent_id = $1::uuid AND type = 'Episode'"
		itemSQL = `SELECT i.id FROM items i WHERE i.parent_id = $1::uuid AND i.type = 'Episode' ORDER BY i.index_number NULLS LAST, i.sort_name ASC, i.id ASC`
	} else {
		bindID = *suid
		countSQL = "SELECT COUNT(*) FROM items WHERE series_id = $1::uuid AND type = 'Episode'"
		itemSQL = `SELECT i.id FROM items i WHERE i.series_id = $1::uuid AND i.type = 'Episode' ORDER BY i.parent_index_number NULLS LAST, i.index_number NULLS LAST, i.id ASC`
	}

	var totalCount int64
	_ = state.DB.QueryRow(ctx, countSQL, bindID).Scan(&totalCount)

	if limit > 0 {
		itemSQL += " LIMIT " + strconv.FormatInt(limit, 10)
	}
	if startIndex > 0 {
		itemSQL += " OFFSET " + strconv.FormatInt(startIndex, 10)
	}

	rows, qerr := state.DB.Query(ctx, itemSQL, bindID)
	if qerr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": qerr.Error()})
		return
	}
	defer rows.Close()

	seriesRow, _ := models.GetItemByID(ctx, state.DB, *suid)
	var seriesImageTag, seriesBackdropTag, seriesNameVal *string
	if seriesRow != nil {
		seriesImageTag = seriesRow.PrimaryImageTag
		seriesBackdropTag = seriesRow.BackdropImageTag
		seriesNameVal = &seriesRow.Name
	}

	var items []dto.BaseItemDto
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		row, err := models.GetItemByID(ctx, state.DB, id)
		if err != nil || row == nil {
			continue
		}

		var ud *dto.UserDataRow
		if userID != "" {
			u, err := models.GetUserItemData(ctx, state.DB, userID, id)
			if err == nil && u != nil {
				ud = u
			}
		}
		d := dto.FormatItemDto(row, state.Config.ServerID, ud)
		d.SeriesID = suid
		if d.SeriesName == nil {
			d.SeriesName = seriesNameVal
		}

		if len(d.ImageTags) == 0 && seriesImageTag != nil {
			d.SeriesPrimaryImageTag = seriesImageTag
			d.SeriesPrimaryImageItemID = suid
			d.ParentPrimaryImageItemID = suid
			d.ParentPrimaryImageTag = seriesImageTag
			d.ParentThumbItemID = suid
			d.ParentThumbImageTag = seriesImageTag
		}
		if len(d.BackdropImageTags) == 0 && seriesBackdropTag != nil {
			d.ParentBackdropItemID = suid
			d.ParentBackdropImageTags = []string{*seriesBackdropTag}
		}

		if row.FilePath != nil && *row.FilePath != "" {
			sources := buildItemMediaSources(ctx, state, id, row)
			if len(sources) > 0 {
				d.MediaSources = sources
				d.MediaStreams = sources[0].MediaStreams
			}
		}

		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"Items": items, "TotalRecordCount": totalCount})
}

type apiKeyCreateBody struct {
	Name string `json:"Name"`
}

func createApiKey(c *gin.Context, state *AppState) {
	var body apiKeyCreateBody
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Name required"})
		return
	}
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	key := hex.EncodeToString(keyBytes)

	auth := middleware.GetAuthUser(c)
	var createdBy interface{}
	if auth != nil && !strings.HasPrefix(auth.ID, "api-key-") {
		if uid, err := uuid.Parse(auth.ID); err == nil {
			createdBy = uid
		}
	}

	var newID uuid.UUID
	var createdAt time.Time
	err := state.DB.QueryRow(c.Request.Context(),
		`INSERT INTO api_keys (name, key, created_by) VALUES ($1, $2, $3) RETURNING id, created_at`,
		body.Name, key, createdBy).Scan(&newID, &createdAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"Id":        newID.String(),
		"Name":      body.Name,
		"Key":       key,
		"CreatedAt": createdAt.UTC().Format(time.RFC3339),
	})
}

func listApiKeys(c *gin.Context, state *AppState) {
	rows, err := state.DB.Query(c.Request.Context(),
		`SELECT ak.id, ak.name, ak.key, ak.created_at, ak.last_used_at, COALESCE(u.name, 'Unknown') as created_by_name
		 FROM api_keys ak LEFT JOIN users u ON ak.created_by = u.id ORDER BY ak.created_at DESC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	var out []gin.H
	for rows.Next() {
		var id uuid.UUID
		var name, key, createdByName string
		var createdAt interface{}
		var lastUsed interface{}
		if err := rows.Scan(&id, &name, &key, &createdAt, &lastUsed, &createdByName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		out = append(out, gin.H{
			"Id":        id.String(),
			"Name":      name,
			"Key":       key,
			"CreatedAt": createdAt,
			"LastUsedAt": lastUsed,
			"CreatedBy": createdByName,
		})
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if out == nil {
		out = []gin.H{}
	}
	c.JSON(http.StatusOK, gin.H{"Items": out})
}

func deleteApiKey(c *gin.Context, state *AppState) {
	keyID := c.Param("keyId")
	ct, err := state.DB.Exec(c.Request.Context(), `DELETE FROM api_keys WHERE id = $1::uuid`, keyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if ct.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"message": "Not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

func getItemCounts(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	var movie, series, episodes int64
	_ = state.DB.QueryRow(ctx, "SELECT COUNT(*) FROM items WHERE type = 'Movie'").Scan(&movie)
	_ = state.DB.QueryRow(ctx, "SELECT COUNT(*) FROM items WHERE type = 'Series'").Scan(&series)
	_ = state.DB.QueryRow(ctx, "SELECT COUNT(*) FROM items WHERE type = 'Episode'").Scan(&episodes)
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

func sessionStop(c *gin.Context, state *AppState) {
	sessionID := c.Param("sessionId")
	if idx := strings.Index(sessionID, "_"); idx > 0 {
		userID := sessionID[:idx]
		deviceID := sessionID[idx+1:]
		state.SessionManager.ClearNowPlaying(userID, deviceID)
	}
	c.Status(http.StatusNoContent)
}

func sessionMessage(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

func customSqlReport(c *gin.Context, state *AppState) {
	q := strings.TrimSpace(c.Query("Query"))
	if q == "" {
		q = `SELECT * FROM "PlaybackActivity" ORDER BY "DateCreated" DESC LIMIT 500`
	}
	if !isSafePlaybackActivityQuery(q) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Query not allowed"})
		return
	}

	rows, err := state.DB.Query(c.Request.Context(), q)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	data, err := rowsToMaps(rows)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}

type customQueryBody struct {
	CustomQueryString string `json:"CustomQueryString"`
}

func submitCustomQuery(c *gin.Context, state *AppState) {
	var body customQueryBody
	if err := c.ShouldBindJSON(&body); err != nil || body.CustomQueryString == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "CustomQueryString required"})
		return
	}
	sql := body.CustomQueryString
	trimmed := strings.ToUpper(strings.TrimSpace(sql))
	if !strings.HasPrefix(trimmed, "SELECT") {
		c.JSON(http.StatusForbidden, gin.H{"message": "Only SELECT queries allowed"})
		return
	}
	forbidden := []string{"INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE", "TRUNCATE",
		"GRANT", "REVOKE", "COPY", "EXECUTE", "DO ", "CALL", "SET ",
		"PG_READ_FILE", "PG_WRITE_FILE", "PG_SLEEP", "LO_IMPORT", "LO_EXPORT"}
	for _, kw := range forbidden {
		// Use word-boundary matching to avoid false positives (e.g. DateCreated containing CREATE)
		kwTrimmed := strings.TrimSpace(kw)
		pattern := `(?i)\b` + regexp.QuoteMeta(kwTrimmed) + `\b`
		if matched, _ := regexp.MatchString(pattern, trimmed); matched {
			c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden keyword: " + kw})
			return
		}
	}
	allowedTables := []string{"PlaybackActivity", "playback_activity", "items", "users",
		"media_versions", "media_streams", "user_item_data", "genres", "item_genres"}
	hasAllowed := false
	sqlLower := strings.ToLower(sql)
	for _, t := range allowedTables {
		if strings.Contains(sqlLower, strings.ToLower(t)) {
			hasAllowed = true
			break
		}
	}
	if !hasAllowed {
		c.JSON(http.StatusForbidden, gin.H{"message": "Query must reference a known table"})
		return
	}

	// Step 1: SQLite function rewrites (before column name mapping)
	sql = rewriteSubstrInstr.ReplaceAllString(sql, "split_part($1, '$2', 1)")
	sql = rewriteInstr.ReplaceAllString(sql, "POSITION($2 IN $1)")
	sql = rewriteStrftimeYMD.ReplaceAllString(sql, "TO_CHAR($1, 'YYYY-MM-DD')")
	sql = rewriteStrftimeH.ReplaceAllString(sql, "TO_CHAR($1, 'HH24')")
	sql = rewriteStrftimeW.ReplaceAllString(sql, "EXTRACT(DOW FROM $1)::text")
	sql = rewriteDatetimeDays.ReplaceAllString(sql, "(NOW() - INTERVAL '$1 days')")
	sql = rewriteDatetimeNow.ReplaceAllString(sql, "NOW()")
	sql = rewriteRowID.ReplaceAllString(sql, "id")
	sql = rewriteUserList.ReplaceAllString(sql, "SELECT id::text FROM users WHERE is_admin = true")

	// Step 2: Fix GROUP BY before column quoting (aliases like "name" are unquoted)
	sql = fixLooseGroupBy(sql)

	// Step 3: Table + column name mapping for PG case sensitivity
	sql = strings.ReplaceAll(sql, "PlaybackActivity", `"PlaybackActivity"`)
	embyColumns := map[string]string{
		"UserId": `"UserId"`, "DateCreated": `"DateCreated"`, "ItemId": `"ItemId"`,
		"ItemType": `"ItemType"`, "ItemName": `"ItemName"`, "PlayDuration": `"PlayDuration"`,
		"PauseDuration": `"PauseDuration"`, "ClientName": `"ClientName"`, "DeviceName": `"DeviceName"`,
		"RemoteAddress": `"ClientIp"`, "ClientIp": `"ClientIp"`, "PlaybackMethod": `"PlaybackMethod"`,
		"SeriesName": `"SeriesName"`,
	}
	for embyCol, pgCol := range embyColumns {
		re := regexp.MustCompile(`\b` + embyCol + `\b`)
		sql = re.ReplaceAllString(sql, pgCol)
	}
	sql = strings.ReplaceAll(sql, `"PauseDuration"`, "0")

	rows, err := state.DB.Query(c.Request.Context(), sql)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	fds := rows.FieldDescriptions()
	columns := make([]string, len(fds))
	for i, fd := range fds {
		columns[i] = string(fd.Name)
	}

	var results [][]interface{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		results = append(results, vals)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if results == nil {
		results = [][]interface{}{}
	}
	c.JSON(http.StatusOK, gin.H{"colums": columns, "results": results})
}

var (
	rewriteRowID        = regexp.MustCompile(`(?i)\browid\b`)
	rewriteStrftimeYMD  = regexp.MustCompile(`(?i)strftime\s*\(\s*'%Y-%m-%d'\s*,\s*(\w+)\s*\)`)
	rewriteStrftimeH    = regexp.MustCompile(`(?i)strftime\s*\(\s*'%H'\s*,\s*(\w+)\s*\)`)
	rewriteStrftimeW    = regexp.MustCompile(`(?i)strftime\s*\(\s*'%w'\s*,\s*(\w+)\s*\)`)
	rewriteDatetimeDays = regexp.MustCompile(`(?i)datetime\s*\(\s*'now'\s*,\s*'-(\d+)\s+days?'\s*\)`)
	rewriteDatetimeNow  = regexp.MustCompile(`(?i)datetime\s*\(\s*'now'\s*\)`)
	rewriteSubstrInstr  = regexp.MustCompile(`(?i)substr\s*\(\s*(\w+)\s*,\s*0\s*,\s*instr\s*\(\s*\w+\s*,\s*'([^']+)'\s*\)\s*\)`)
	rewriteInstr        = regexp.MustCompile(`(?i)instr\s*\(\s*(\w+)\s*,\s*'([^']+)'\s*\)`)
	rewriteUserList     = regexp.MustCompile(`(?i)select\s+UserId\s+from\s+UserList`)
)

// fixLooseGroupBy detects SELECT columns not in GROUP BY and not in aggregate functions,
// then wraps them with MIN() to satisfy PostgreSQL strict GROUP BY rules.
func fixLooseGroupBy(sql string) string {
	upper := strings.ToUpper(sql)
	groupByIdx := strings.LastIndex(upper, "GROUP BY")
	if groupByIdx < 0 {
		return sql
	}

	// Extract GROUP BY columns
	afterGroupBy := sql[groupByIdx+8:]
	// Cut at ORDER BY / LIMIT / HAVING if present
	for _, kw := range []string{"ORDER BY", "LIMIT", "HAVING"} {
		if idx := strings.Index(strings.ToUpper(afterGroupBy), kw); idx >= 0 {
			afterGroupBy = afterGroupBy[:idx]
		}
	}
	groupCols := make(map[string]bool)
	for _, col := range strings.Split(afterGroupBy, ",") {
		col = strings.TrimSpace(col)
		col = strings.Trim(col, `"`)
		if col != "" {
			groupCols[strings.ToLower(col)] = true
		}
	}

	// Extract SELECT columns (between SELECT and FROM)
	selectIdx := strings.Index(upper, "SELECT")
	fromIdx := strings.Index(upper, "FROM")
	if selectIdx < 0 || fromIdx < 0 || fromIdx <= selectIdx+6 {
		return sql
	}
	selectPart := sql[selectIdx+6 : fromIdx]

	// Parse SELECT columns, wrap non-grouped non-aggregate ones
	var newCols []string
	for _, col := range splitSelectColumns(selectPart) {
		trimmed := strings.TrimSpace(col)
		if trimmed == "" {
			continue
		}

		upperCol := strings.ToUpper(trimmed)
		// Skip if already an aggregate
		isAgg := false
		for _, fn := range []string{"SUM(", "COUNT(", "MAX(", "MIN(", "AVG("} {
			if strings.Contains(upperCol, fn) {
				isAgg = true
				break
			}
		}
		if isAgg {
			newCols = append(newCols, trimmed)
			continue
		}

		// Extract the bare column name and alias (handle "X AS alias")
		bareName := trimmed
		alias := ""
		aliasName := ""
		if asIdx := strings.LastIndex(strings.ToUpper(trimmed), " AS "); asIdx >= 0 {
			bareName = strings.TrimSpace(trimmed[:asIdx])
			alias = strings.TrimSpace(trimmed[asIdx:])
			aliasName = strings.TrimSpace(trimmed[asIdx+4:])
		}
		bareNameClean := strings.ToLower(strings.Trim(bareName, `"`))

		// Check if bare name or alias is in GROUP BY
		if groupCols[bareNameClean] || (aliasName != "" && groupCols[strings.ToLower(aliasName)]) {
			newCols = append(newCols, trimmed)
		} else {
			// Wrap with MIN()
			newCols = append(newCols, "MIN("+bareName+")"+alias)
		}
	}

	return sql[:selectIdx+6] + " " + strings.Join(newCols, ", ") + " " + sql[fromIdx:]
}

// splitSelectColumns splits SELECT column list respecting parentheses.
func splitSelectColumns(s string) []string {
	var result []string
	depth := 0
	start := 0
	for i, c := range s {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				result = append(result, s[start:i])
				start = i + 1
			}
		}
	}
	result = append(result, s[start:])
	return result
}

func isSafePlaybackActivityQuery(q string) bool {
	low := strings.ToLower(strings.TrimSpace(q))
	if !strings.HasPrefix(low, "select") {
		return false
	}
	if strings.Contains(low, ";") {
		return false
	}
	banned := []string{
		"insert ", "update ", "delete ", "drop ", "truncate ", "alter ", "create ",
		"grant ", "revoke ", "pg_", "information_schema", "into ", "copy ",
	}
	for _, b := range banned {
		if strings.Contains(low, b) {
			return false
		}
	}
	return strings.Contains(low, "playbackactivity")
}

func rowsToMaps(rows pgx.Rows) ([]map[string]interface{}, error) {
	fds := rows.FieldDescriptions()
	var out []map[string]interface{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		m := make(map[string]interface{}, len(fds))
		for i, fd := range fds {
			m[string(fd.Name)] = vals[i]
		}
		out = append(out, m)
	}
	return out, rows.Err()
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

	opts := &models.ItemQueryOptions{
		IncludeItemTypes: []string{"Person"},
		Limit:            &limit,
		StartIndex:       &start,
	}
	if term := c.Query("SearchTerm"); term != "" {
		opts.SearchTerm = &term
	}

	res, err := models.QueryItems(c.Request.Context(), state.DB, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	auth := middleware.GetAuthUser(c)
	var userID *string
	if auth != nil && !strings.HasPrefix(auth.ID, "api-key-") {
		userID = &auth.ID
	}

	items := make([]dto.BaseItemDto, 0, len(res.Items))
	for i := range res.Items {
		var ud *dto.UserDataRow
		if userID != nil {
			u, err := models.GetUserItemData(c.Request.Context(), state.DB, *userID, res.Items[i].ID)
			if err == nil && u != nil {
				ud = u
			}
		}
		items = append(items, dto.FormatItemDto(&res.Items[i], state.Config.ServerID, ud))
	}
	c.JSON(http.StatusOK, gin.H{"Items": items, "TotalRecordCount": res.TotalCount})
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
		i.tagline, i.studio, i.created_at, i.emby_id`

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
		if platformName, ok := models.IsPlatformVirtualID(ctx, state.DB, parentID); ok {
			whereParts = append(whereParts, "i.studio = $"+strconv.Itoa(idx))
			args = append(args, platformName)
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
					result["MediaSources"] = sources
					result["MediaStreams"] = sources[0].MediaStreams
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

func buildItemMediaSources(ctx context.Context, state *AppState, itemID string, item *dto.ItemRow) []dto.MediaSourceInfo {
	rows, err := state.DB.Query(ctx,
		`SELECT id, name, file_path, container, is_primary, runtime_ticks, bitrate, size, mediainfo,
		        resolution, hdr_format, video_codec, audio_codec, source, quality_label
		 FROM media_versions WHERE item_id = $1::uuid
		 ORDER BY is_primary DESC, created_at ASC`, itemID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var versions []mediaVersionRow
	for rows.Next() {
		var v mediaVersionRow
		if err := rows.Scan(&v.ID, &v.Name, &v.FilePath, &v.Container, &v.IsPrimary, &v.RuntimeTicks, &v.Bitrate, &v.Size, &v.MediaInfo,
			&v.Resolution, &v.HDRFormat, &v.VideoCodec, &v.AudioCodec, &v.Source, &v.QualityLabel); err != nil {
			continue
		}
		versions = append(versions, v)
	}

	if len(versions) == 0 && item.FilePath != nil && *item.FilePath != "" {
		versions = append(versions, mediaVersionRow{
			ID:           uuid.Nil,
			Name:         "Default",
			FilePath:     *item.FilePath,
			Container:    item.Container,
			IsPrimary:    true,
			RuntimeTicks: item.RuntimeTicks,
		})
	}

	streamRows, _ := models.GetMediaStreams(ctx, state.DB, itemID)
	baseStreams := make([]dto.MediaStreamInfo, 0, len(streamRows))
	for i := range streamRows {
		baseStreams = append(baseStreams, dto.FormatMediaStreamDto(&streamRows[i]))
	}

	var sources []dto.MediaSourceInfo
	for idx, mv := range versions {
		msid := mv.ID.String()
		if mv.ID == uuid.Nil {
			msid = itemID
		}

		actualPath := mv.FilePath
		actualContainer := ""
		if mv.Container != nil {
			actualContainer = *mv.Container
		}
		protocol := "File"
		isRemote := false

		if strings.HasSuffix(strings.ToLower(mv.FilePath), ".strm") {
			if rp := resolveStrmPath(mv.FilePath); rp != nil {
				actualPath = rp.filePath
				actualContainer = rp.container
				isRemote = rp.isRemote
				if isRemote {
					protocol = "Http"
				}
			}
		} else if strings.HasPrefix(strings.ToLower(actualPath), "http://") || strings.HasPrefix(strings.ToLower(actualPath), "https://") {
			protocol = "Http"
			isRemote = true
		}

		if actualContainer == "" && item.Container != nil {
			actualContainer = *item.Container
		}

		versionStreams := baseStreams
		if len(mv.MediaInfo) > 0 {
			var mi map[string]json.RawMessage
			if json.Unmarshal(mv.MediaInfo, &mi) == nil {
				if msRaw, ok := mi["MediaStreams"]; ok {
					var miStreams []dto.MediaStreamInfo
					if json.Unmarshal(msRaw, &miStreams) == nil && len(miStreams) > 0 {
						versionStreams = miStreams
					}
				}
			}
		}
		if len(versionStreams) == 0 && idx == 0 {
			versionStreams = baseStreams
		}

		src := dto.MediaSourceInfo{
			ID:                   msid,
			Path:                 actualPath,
			Protocol:             protocol,
			Type:                 "Default",
			Container:            actualContainer,
			Name:                 mv.Name,
			IsRemote:             isRemote,
			RunTimeTicks:         mv.RuntimeTicks,
			SupportsDirectPlay:   true,
			SupportsDirectStream: true,
			SupportsTranscoding:  false,
			MediaStreams:         versionStreams,
			DirectStreamURL:      fmt.Sprintf("/Videos/%s/stream.%s?MediaSourceId=%s&Static=true", itemID, actualContainer, msid),
			ETag:                 msid,
			Size:                 mv.Size,
			Formats:              []string{},
			FymsResolution:       mv.Resolution,
			FymsHdrFormat:        mv.HDRFormat,
			FymsVideoCodec:       mv.VideoCodec,
			FymsAudioCodec:       mv.AudioCodec,
			FymsSource:           mv.Source,
			FymsQualityLabel:     mv.QualityLabel,
		}
		if mv.Bitrate != nil {
			b := int64(*mv.Bitrate)
			src.Bitrate = &b
		}
		sources = append(sources, src)
	}

	mergedSources := collectMergedVersionSources(ctx, state, itemID, baseStreams)
	if len(mergedSources) > 0 {
		sources = append(sources, mergedSources...)
	}

	return sources
}

// collectMergedVersionSources finds items merged into itemID (via merged_to_id)
// and returns their media_versions as additional MediaSourceInfo entries.
func collectMergedVersionSources(ctx context.Context, state *AppState, itemID string, fallbackStreams []dto.MediaStreamInfo) []dto.MediaSourceInfo {
	sibRows, err := state.DB.Query(ctx,
		`SELECT s.id::text, l.name AS lib_name
		 FROM items s JOIN libraries l ON s.library_id = l.id
		 WHERE s.merged_to_id = $1::uuid AND l.deleted_at IS NULL`, itemID)
	if err != nil {
		return nil
	}
	defer sibRows.Close()

	type sibInfo struct{ ID, LibName string }
	var siblings []sibInfo
	for sibRows.Next() {
		var si sibInfo
		if err := sibRows.Scan(&si.ID, &si.LibName); err != nil {
			continue
		}
		siblings = append(siblings, si)
	}
	if len(siblings) == 0 {
		return nil
	}

	var merged []dto.MediaSourceInfo
	for _, sib := range siblings {
		mvRows, err := state.DB.Query(ctx,
			`SELECT id, name, file_path, container, is_primary, runtime_ticks, bitrate, size, mediainfo,
			        resolution, hdr_format, video_codec, audio_codec, source, quality_label
			 FROM media_versions WHERE item_id = $1::uuid ORDER BY is_primary DESC, created_at ASC`, sib.ID)
		if err != nil {
			continue
		}
		for mvRows.Next() {
			var mv mediaVersionRow
			if err := mvRows.Scan(&mv.ID, &mv.Name, &mv.FilePath, &mv.Container, &mv.IsPrimary, &mv.RuntimeTicks, &mv.Bitrate, &mv.Size, &mv.MediaInfo,
				&mv.Resolution, &mv.HDRFormat, &mv.VideoCodec, &mv.AudioCodec, &mv.Source, &mv.QualityLabel); err != nil {
				continue
			}
			msid := mv.ID.String()
			actualPath := mv.FilePath
			actualContainer := ""
			if mv.Container != nil {
				actualContainer = *mv.Container
			}
			protocol := "File"
			isRemote := false
			if strings.HasSuffix(strings.ToLower(mv.FilePath), ".strm") {
				if rp := resolveStrmPath(mv.FilePath); rp != nil {
					actualPath = rp.filePath
					actualContainer = rp.container
					isRemote = rp.isRemote
					if isRemote {
						protocol = "Http"
					}
				}
			} else if strings.HasPrefix(strings.ToLower(actualPath), "http://") || strings.HasPrefix(strings.ToLower(actualPath), "https://") {
				protocol = "Http"
				isRemote = true
			}

			versionStreams := fallbackStreams
			if len(mv.MediaInfo) > 0 {
				var mi map[string]json.RawMessage
				if json.Unmarshal(mv.MediaInfo, &mi) == nil {
					if msRaw, ok := mi["MediaStreams"]; ok {
						var miStreams []dto.MediaStreamInfo
						if json.Unmarshal(msRaw, &miStreams) == nil && len(miStreams) > 0 {
							versionStreams = miStreams
						}
					}
				}
			}

			srcName := sib.LibName + " - " + mv.Name
			src := dto.MediaSourceInfo{
				ID:                   msid,
				Path:                 actualPath,
				Protocol:             protocol,
				Type:                 "Default",
				Container:            actualContainer,
				Name:                 srcName,
				IsRemote:             isRemote,
				RunTimeTicks:         mv.RuntimeTicks,
				SupportsDirectPlay:   true,
				SupportsDirectStream: true,
				SupportsTranscoding:  false,
				MediaStreams:         versionStreams,
				DirectStreamURL:      fmt.Sprintf("/Videos/%s/stream.%s?MediaSourceId=%s&Static=true", itemID, actualContainer, msid),
				ETag:                 msid,
				Size:                 mv.Size,
				Formats:              []string{},
				FymsResolution:       mv.Resolution,
				FymsHdrFormat:        mv.HDRFormat,
				FymsVideoCodec:       mv.VideoCodec,
				FymsAudioCodec:       mv.AudioCodec,
				FymsSource:           mv.Source,
				FymsQualityLabel:     mv.QualityLabel,
			}
			if mv.Bitrate != nil {
				b := int64(*mv.Bitrate)
				src.Bitrate = &b
			}
			merged = append(merged, src)
		}
		mvRows.Close()
	}
	return merged
}
