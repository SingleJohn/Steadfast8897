package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"fyms/internal/models"
)

// RegisterEmbyCompatRoutes registers the /emby/ endpoints used by external STRM tools.
func RegisterEmbyCompatRoutes(group *gin.RouterGroup, state *AppState, adminMW gin.HandlerFunc) {
	e := group.Group("/emby")
	e.GET("/emby_users/:configId", adminMW, func(c *gin.Context) { embyGetUsers(c, state) })
	e.GET("/media_stats", adminMW, func(c *gin.Context) { embyMediaStats(c, state) })
	e.POST("/season_episodes_check", adminMW, func(c *gin.Context) { embySeasonEpisodesCheck(c, state) })
	e.POST("/library_check", adminMW, func(c *gin.Context) { embyLibraryCheck(c, state) })
	e.POST("/gap_scan/start", adminMW, func(c *gin.Context) { embyGapScanStart(c, state) })
	e.GET("/gap_scan/status", adminMW, func(c *gin.Context) { embyGapScanStatus(c, state) })
	e.GET("/gap_scan/result", adminMW, func(c *gin.Context) { embyGapScanResult(c, state) })
}

func embyGetUsers(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	users, err := models.GetAllUsers(ctx, state.DB)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	var data []gin.H
	for _, u := range users {
		data = append(data, gin.H{"id": u.ID, "name": u.Name})
	}
	if data == nil {
		data = []gin.H{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func embyMediaStats(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	pool := state.DB

	var movieCount, tvCount, episodeCount, userCount int64
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM items WHERE type = 'Movie'").Scan(&movieCount)
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM items WHERE type = 'Series'").Scan(&tvCount)
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM items WHERE type = 'Episode'").Scan(&episodeCount)
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&userCount)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"movie_count":   movieCount,
			"tv_count":      tvCount,
			"episode_count": episodeCount,
			"user_count":    userCount,
		},
	})
}

func embySeasonEpisodesCheck(c *gin.Context, state *AppState) {
	var body struct {
		Name   string `json:"name"`
		Year   *int   `json:"year"`
		TmdbID *int64 `json:"tmdb_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Name == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "name is required"})
		return
	}

	ctx := c.Request.Context()
	pool := state.DB

	seriesID := findSeriesID(ctx, pool, body.Name, body.Year, body.TmdbID)
	if seriesID == "" {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{}})
		return
	}

	seasonRows, err := pool.Query(ctx,
		"SELECT id, index_number FROM items WHERE parent_id = $1::uuid AND type = 'Season' ORDER BY index_number", seriesID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{}})
		return
	}
	defer seasonRows.Close()

	result := gin.H{}
	for seasonRows.Next() {
		var seasonID string
		var seasonNum int32
		if err := seasonRows.Scan(&seasonID, &seasonNum); err != nil {
			continue
		}

		epRows, err := pool.Query(ctx,
			"SELECT index_number FROM items WHERE parent_id = $1::uuid AND type = 'Episode' AND index_number IS NOT NULL ORDER BY index_number",
			seasonID)
		if err != nil {
			continue
		}

		var eps []int32
		for epRows.Next() {
			var ep int32
			if epRows.Scan(&ep) == nil && ep > 0 {
				eps = append(eps, ep)
			}
		}
		epRows.Close()

		if len(eps) > 0 {
			result[strconv.Itoa(int(seasonNum))] = eps
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func embyLibraryCheck(c *gin.Context, state *AppState) {
	var body struct {
		Items []struct {
			Name   string `json:"name"`
			Year   *int   `json:"year"`
			Type   string `json:"type"`
			TmdbID *int64 `json:"tmdb_id"`
		} `json:"items"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	ctx := c.Request.Context()
	pool := state.DB
	result := gin.H{}

	for _, item := range body.Items {
		var key string
		if item.TmdbID != nil && *item.TmdbID > 0 {
			key = fmt.Sprintf("tmdb-%d", *item.TmdbID)
		} else if item.Year != nil {
			key = fmt.Sprintf("%s-%d", item.Name, *item.Year)
		} else {
			key = item.Name
		}

		dbType := "Movie"
		if item.Type == "tv" {
			dbType = "Series"
		}

		found := false

		if item.TmdbID != nil && *item.TmdbID > 0 {
			var count int64
			_ = pool.QueryRow(ctx,
				"SELECT COUNT(*) FROM items WHERE type = $1 AND provider_ids->>'Tmdb' = $2",
				dbType, strconv.FormatInt(*item.TmdbID, 10)).Scan(&count)
			found = count > 0
		}

		if !found {
			var count int64
			if item.Year != nil && *item.Year > 0 {
				_ = pool.QueryRow(ctx,
					"SELECT COUNT(*) FROM items WHERE type = $1 AND name ILIKE $2 AND EXTRACT(YEAR FROM premiere_date) = $3",
					dbType, item.Name, *item.Year).Scan(&count)
			} else {
				_ = pool.QueryRow(ctx,
					"SELECT COUNT(*) FROM items WHERE type = $1 AND name ILIKE $2",
					dbType, item.Name).Scan(&count)
			}
			found = count > 0
		}

		if found {
			result[key] = "found"
		} else {
			result[key] = "not-found"
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func embyGapScanStart(c *gin.Context, state *AppState) {
	if err := state.GapScanTask.Start(state.DB); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func embyGapScanStatus(c *gin.Context, state *AppState) {
	p := state.GapScanTask.GetProgress()
	c.JSON(http.StatusOK, gin.H{"success": true, "data": p})
}

func embyGapScanResult(c *gin.Context, state *AppState) {
	r := state.GapScanTask.GetResult()
	if r == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": r})
}

func findSeriesID(ctx context.Context, pool *pgxpool.Pool, name string, year *int, tmdbID *int64) string {
	var id string

	// 1. Try TMDB ID
	if tmdbID != nil && *tmdbID > 0 {
		if pool.QueryRow(ctx,
			"SELECT id FROM items WHERE type = 'Series' AND provider_ids->>'Tmdb' = $1 LIMIT 1",
			strconv.FormatInt(*tmdbID, 10)).Scan(&id) == nil && id != "" {
			return id
		}
	}

	// 2. Try name + year
	if year != nil && *year > 0 {
		if pool.QueryRow(ctx,
			"SELECT id FROM items WHERE type = 'Series' AND name ILIKE $1 AND EXTRACT(YEAR FROM premiere_date) = $2 LIMIT 1",
			name, *year).Scan(&id) == nil && id != "" {
			return id
		}
	}

	// 3. Try name only
	if pool.QueryRow(ctx,
		"SELECT id FROM items WHERE type = 'Series' AND name ILIKE $1 LIMIT 1",
		name).Scan(&id) == nil && id != "" {
		return id
	}

	// 4. Try without year suffix: "三体 (2023)" -> "三体"
	cleanName := strings.TrimSpace(name)
	if idx := strings.LastIndex(cleanName, "("); idx > 0 {
		cleanName = strings.TrimSpace(cleanName[:idx])
		if cleanName != name {
			if pool.QueryRow(ctx,
				"SELECT id FROM items WHERE type = 'Series' AND name ILIKE $1 LIMIT 1",
				cleanName).Scan(&id) == nil && id != "" {
				return id
			}
		}
	}

	return ""
}
