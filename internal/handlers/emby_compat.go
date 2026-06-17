package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
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
	users, err := state.Repo.Users.ListUsers(ctx)
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

	var movieCount, tvCount, episodeCount, userCount int64
	movieCount, _ = state.Repo.ItemHelpers.CountItemsByType(ctx, "Movie")
	tvCount, _ = state.Repo.ItemHelpers.CountItemsByType(ctx, "Series")
	episodeCount, _ = state.Repo.ItemHelpers.CountItemsByType(ctx, "Episode")
	userCount, _ = state.Repo.Users.CountUsers(ctx)

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

	seriesID := findSeriesID(ctx, state, body.Name, body.Year, body.TmdbID)
	if seriesID == "" {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{}})
		return
	}

	seasons, err := state.Repo.ItemHelpers.ListSeasonsForSeries(ctx, seriesID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{}})
		return
	}

	result := gin.H{}
	for _, season := range seasons {
		if season.IndexNumber == nil {
			continue
		}

		eps, err := state.Repo.ItemHelpers.ListEpisodeIndexesForSeason(ctx, season.ID)
		if err != nil {
			continue
		}

		if len(eps) > 0 {
			result[strconv.Itoa(int(*season.IndexNumber))] = eps
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
			count, _ = state.Repo.ItemHelpers.CountItemsByTypeAndTmdbProvider(ctx, dbType, strconv.FormatInt(*item.TmdbID, 10))
			found = count > 0
		}

		if !found {
			var count int64
			if item.Year != nil && *item.Year > 0 {
				count, _ = state.Repo.ItemHelpers.CountItemsByTypeNameYear(ctx, dbType, item.Name, *item.Year)
			} else {
				count, _ = state.Repo.ItemHelpers.CountItemsByTypeName(ctx, dbType, item.Name)
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

func findSeriesID(ctx context.Context, state *AppState, name string, year *int, tmdbID *int64) string {
	// 1. Try TMDB ID
	if tmdbID != nil && *tmdbID > 0 {
		if id, err := state.Repo.ItemHelpers.FindSeriesIDByTmdbProvider(ctx, strconv.FormatInt(*tmdbID, 10)); err == nil && id != nil && *id != "" {
			return *id
		}
	}

	// 2. Try name + year
	if year != nil && *year > 0 {
		if id, err := state.Repo.ItemHelpers.FindSeriesIDByNameYear(ctx, name, *year); err == nil && id != nil && *id != "" {
			return *id
		}
	}

	// 3. Try name only
	if id, err := state.Repo.ItemHelpers.FindSeriesIDByName(ctx, name); err == nil && id != nil && *id != "" {
		return *id
	}

	// 4. Try without year suffix: "三体 (2023)" -> "三体"
	cleanName := strings.TrimSpace(name)
	if idx := strings.LastIndex(cleanName, "("); idx > 0 {
		cleanName = strings.TrimSpace(cleanName[:idx])
		if cleanName != name {
			if id, err := state.Repo.ItemHelpers.FindSeriesIDByName(ctx, cleanName); err == nil && id != nil && *id != "" {
				return *id
			}
		}
	}

	return ""
}
