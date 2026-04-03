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
		itemSQL = `SELECT i.id FROM items i WHERE i.parent_id = $1::uuid AND i.type = 'Episode' ORDER BY i.index_number NULLS LAST`
	} else {
		bindID = *suid
		countSQL = "SELECT COUNT(*) FROM items WHERE series_id = $1::uuid AND type = 'Episode'"
		itemSQL = `SELECT i.id FROM items i WHERE i.series_id = $1::uuid AND i.type = 'Episode' ORDER BY i.parent_index_number NULLS LAST, i.index_number NULLS LAST`
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
		if strings.Contains(trimmed, kw) {
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

	sql = strings.ReplaceAll(sql, "PlaybackActivity", `"PlaybackActivity"`)
	sql = rewriteRowID.ReplaceAllString(sql, "id")
	sql = rewriteStrftimeYMD.ReplaceAllString(sql, "TO_CHAR($1, 'YYYY-MM-DD')")
	sql = rewriteStrftimeH.ReplaceAllString(sql, "TO_CHAR($1, 'HH24')")
	sql = rewriteStrftimeW.ReplaceAllString(sql, "EXTRACT(DOW FROM $1)::text")
	sql = rewriteDatetimeDays.ReplaceAllString(sql, "(NOW() - INTERVAL '$1 days')")
	sql = rewriteDatetimeNow.ReplaceAllString(sql, "NOW()")

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
)

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

func itemsSearch(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()

	ids := c.Query("Ids")
	if ids == "" {
		ids = c.Query("ids")
	}
	searchTerm := c.Query("SearchTerm")
	if searchTerm == "" {
		searchTerm = c.Query("searchTerm")
	}
	includeTypes := c.Query("IncludeItemTypes")
	if includeTypes == "" {
		includeTypes = c.Query("includeItemTypes")
	}
	fields := c.Query("Fields")
	if fields == "" {
		fields = c.Query("fields")
	}
	limitStr := c.Query("Limit")
	if limitStr == "" {
		limitStr = c.Query("limit")
	}
	limitVal := int64(50)
	if limitStr != "" {
		if n, err := strconv.ParseInt(limitStr, 10, 64); err == nil && n > 0 {
			limitVal = n
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

	sql := "SELECT * FROM items WHERE 1=1"
	var args []interface{}
	idx := 1

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
			sql += " AND emby_id IN (" + strings.Join(placeholders, ",") + ")"
		} else {
			sql += " AND id IN (" + strings.Join(placeholders, ",") + ")"
		}
	}
	if includeTypes != "" {
		typeList := strings.Split(includeTypes, ",")
		var placeholders []string
		for _, t := range typeList {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			placeholders = append(placeholders, "$"+strconv.Itoa(idx))
			args = append(args, t)
			idx++
		}
		if len(placeholders) > 0 {
			sql += " AND type IN (" + strings.Join(placeholders, ",") + ")"
		}
	}
	if searchTerm != "" {
		sql += " AND name ILIKE $" + strconv.Itoa(idx)
		args = append(args, "%"+searchTerm+"%")
		idx++
	}
	sql += " ORDER BY sort_name LIMIT $" + strconv.Itoa(idx) + "::bigint"
	args = append(args, limitVal)

	rows, err := state.DB.Query(ctx, sql, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	needMediaSources := strings.Contains(fields, "MediaSources") || strings.Contains(fields, "Path")

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
		itemID := ""
		if v, ok := m["id"]; ok {
			itemID = uuidToString(v)
		}
		if itemID == "" {
			continue
		}
		row, err := models.GetItemByID(ctx, state.DB, itemID)
		if err != nil || row == nil {
			continue
		}
		d := dto.FormatItemDto(row, state.Config.ServerID, nil)
		result := dtoToMap(d)

		if embyID, ok := m["emby_id"]; ok && embyID != nil {
			result["EmbyId"] = embyID
			if useEmbyID {
				result["Id"] = fmt.Sprintf("%v", embyID)
			}
		}

		if needMediaSources && (row.ItemType == "Movie" || row.ItemType == "Episode") {
			sources := buildItemMediaSources(ctx, state, itemID, row)
			if len(sources) > 0 {
				result["MediaSources"] = sources
				result["MediaStreams"] = sources[0].MediaStreams
			}
		}

		items = append(items, result)
	}
	if items == nil {
		items = []gin.H{}
	}
	c.JSON(http.StatusOK, gin.H{"Items": items, "TotalRecordCount": len(items)})
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

func buildItemMediaSources(ctx context.Context, state *AppState, itemID string, item *dto.ItemRow) []dto.MediaSourceInfo {
	rows, err := state.DB.Query(ctx,
		`SELECT id, name, file_path, container, is_primary, runtime_ticks, bitrate, size, mediainfo
		 FROM media_versions WHERE item_id = $1::uuid
		 ORDER BY is_primary DESC, created_at ASC`, itemID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var versions []mediaVersionRow
	for rows.Next() {
		var v mediaVersionRow
		if err := rows.Scan(&v.ID, &v.Name, &v.FilePath, &v.Container, &v.IsPrimary, &v.RuntimeTicks, &v.Bitrate, &v.Size, &v.MediaInfo); err != nil {
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
	var baseStreams []dto.MediaStreamInfo
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
			SupportsTranscoding:  true,
			MediaStreams:          versionStreams,
			DirectStreamURL:      fmt.Sprintf("/Videos/%s/stream.%s?MediaSourceId=%s&Static=true", itemID, actualContainer, msid),
			ETag:                 msid,
			Size:                 mv.Size,
		}
		if mv.Bitrate != nil {
			b := int64(*mv.Bitrate)
			src.Bitrate = &b
		}
		sources = append(sources, src)
	}
	return sources
}
