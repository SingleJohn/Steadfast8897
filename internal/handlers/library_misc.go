package handlers

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"fyms/internal/models"
)

func mergeVersions(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	merged, err := models.MergeMultiVersionItems(ctx, state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	slog.Info("MergeVersions completed", "merged", merged)

	// Gather diagnostic counts. primary 不再要求 tmdb_id IS NOT NULL ——
	// 多源合并后 primary 可能仅有 douban/bangumi 等非 TMDB 外部 ID。
	var totalPrimaries, totalSecondaries int64
	totalPrimaries, _ = state.Repo.ItemHelpers.CountMergedVersionPrimaries(ctx)
	totalSecondaries, _ = state.Repo.ItemHelpers.CountMergedVersionSecondaries(ctx)

	c.JSON(http.StatusOK, gin.H{
		"merged":            merged,
		"total_primaries":   totalPrimaries,
		"total_secondaries": totalSecondaries,
	})
}

type browseBody struct {
	Path string `json:"path"`
}

func browseDir(c *gin.Context) {
	_ = GetState(c)
	var body browseBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	p := filepath.Clean(body.Path)
	if p == "" || p == "." {
		p = "/"
	}

	entries, err := os.ReadDir(p)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	type entry struct {
		Name        string `json:"Name"`
		IsDirectory bool   `json:"IsDirectory"`
		Path        string `json:"Path"`
	}
	out := make([]entry, 0, len(entries))
	for _, e := range entries {
		full := filepath.Join(p, e.Name())
		out = append(out, entry{
			Name:        e.Name(),
			IsDirectory: e.IsDir(),
			Path:        full,
		})
	}
	c.JSON(http.StatusOK, gin.H{"Path": p, "Entries": out})
}

func getGenres(c *gin.Context) {
	state := GetState(c)
	rows, err := models.GetAllGenresWithCounts(c.Request.Context(), state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"Items":            rows,
		"TotalRecordCount": len(rows),
	})
}

func getTags(c *gin.Context) {
	state := GetState(c)
	rows, err := models.GetAllTagsWithCounts(c.Request.Context(), state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"Items":            rows,
		"TotalRecordCount": len(rows),
	})
}

func browseDirGet(c *gin.Context) {
	p := strings.TrimSpace(c.Query("path"))
	if p == "" {
		p = strings.TrimSpace(c.Query("Path"))
	}
	if p == "" {
		p = "/mnt"
	}
	p = filepath.Clean(p)

	entries, err := os.ReadDir(p)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	type dirEntry struct {
		Name string `json:"Name"`
		Path string `json:"Path"`
	}
	dirs := make([]dirEntry, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		full := filepath.Join(p, e.Name())
		dirs = append(dirs, dirEntry{
			Name: e.Name(),
			Path: full,
		})
	}
	c.JSON(http.StatusOK, gin.H{"Path": p, "Directories": dirs})
}
