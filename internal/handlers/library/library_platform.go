package library

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"fyms/internal/models"
	"fyms/internal/repository"
	"fyms/internal/services"
)

func getPlatforms(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()
	platforms, err := models.GetPlatformLibraries(ctx, state.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	globalEnabled := state.Repo.SystemConfig.GetStringOrDefault(ctx, "platform_libraries_enabled", "")

	items := make([]gin.H, 0, len(platforms))
	for _, p := range platforms {
		entry := gin.H{
			"Id":             p.ID,
			"PlatformName":   p.PlatformName,
			"DisplayName":    p.EffectiveDisplayName(),
			"CustomName":     p.DisplayName,
			"Enabled":        p.Enabled,
			"CollectionType": p.CollectionType,
			"ItemCount":      p.ItemCount,
			"SortOrder":      p.SortOrder,
			"Dimension":      p.Dimension,
			"MatchValue":     p.MatchValue,
			"MatchValues":    p.Values(),
			"HasCover":       p.CoverImagePath != nil && *p.CoverImagePath != "",
			"IsLatest":       p.IsLatest(),
		}
		if p.IsLatest() {
			entry["ItemLimit"] = p.LatestLimit()
		}
		// 封面优先用生成封面(虚拟 ID 出图),否则已知平台用内置 logo
		if p.CoverImagePath != nil && *p.CoverImagePath != "" {
			coverTag := ""
			if p.CoverImageTag != nil {
				coverTag = *p.CoverImageTag
			}
			entry["CoverUrl"] = "/Items/" + models.PlatformVirtualID(p.Dimension, p.MatchValue) + "/Images/Primary?tag=" + url.QueryEscape(coverTag)
		} else if models.HasPlatformLogo(p.PlatformName) {
			entry["LogoUrl"] = "/Library/Platforms/Logo?name=" + url.QueryEscape(p.PlatformName)
		}
		items = append(items, entry)
	}
	c.JSON(http.StatusOK, gin.H{
		"GlobalEnabled": globalEnabled == "true",
		"Platforms":     items,
	})
}

// applyVirtualDimension 把虚拟库的维度+匹配值翻译成 QueryItems 的对应过滤项。
func applyVirtualDimension(opts *models.ItemQueryOptions, p *models.PlatformLibrary) {
	vals := p.Values()
	switch p.Dimension {
	case models.PlatformDimLatest:
		limit := p.LatestLimit()
		opts.LatestItemLimit = &limit
		sortBy := "LatestActivity"
		sortOrder := "Descending"
		opts.SortBy = &sortBy
		opts.SortOrder = &sortOrder
	case models.PlatformDimActor:
		opts.ActorName = vals
	case models.PlatformDimNumPrefix:
		opts.CatalogPrefix = vals
	default:
		opts.Studio = vals
	}
}

// upsertLatestPlatform POST /Library/Platforms/Latest
// 创建或更新唯一的最新媒体虚拟库。
func upsertLatestPlatform(c *gin.Context, state *AppState) {
	var body struct {
		Name    string `json:"Name"`
		Limit   int64  `json:"Limit"`
		Enabled *bool  `json:"Enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	if body.Limit == 0 {
		body.Limit = models.DefaultLatestItemLimit
	}
	if body.Limit < 1 || body.Limit > 2000 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Limit must be between 1 and 2000"})
		return
	}
	if err := models.UpsertLatestPlatformLibrary(c.Request.Context(), state.DB, body.Name, body.Limit, body.Enabled); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

func addPlatform(c *gin.Context, state *AppState) {
	var body struct {
		PlatformName string `json:"PlatformName"`
		Dimension    string `json:"Dimension"`  // 可选,默认 studio
		MatchValue   string `json:"MatchValue"` // 可选,默认 = PlatformName
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.PlatformName) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "PlatformName required"})
		return
	}
	matchValue := strings.TrimSpace(body.MatchValue)
	if matchValue == "" {
		matchValue = strings.TrimSpace(body.PlatformName)
	}
	if _, err := models.AddPlatformLibrary(c.Request.Context(), state.DB,
		body.Dimension, matchValue, strings.TrimSpace(body.PlatformName), false); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

// discoverPlatformDimension GET /Library/Platforms/Discover?dimension=&search=&minCount=
// 扫描本地数据列出某维度的 distinct 值 + 计数,供前端勾选添加。
func discoverPlatformDimension(c *gin.Context, state *AppState) {
	dimension := strings.TrimSpace(c.Query("dimension"))
	if dimension == "" {
		dimension = models.PlatformDimStudio
	}
	search := c.Query("search")
	var minCount int64 = 1
	if v := strings.TrimSpace(c.Query("minCount")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			minCount = n
		}
	}
	values, err := models.DiscoverDimensionValues(c.Request.Context(), state.DB, dimension, search, minCount)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"dimension": dimension, "values": values})
}

// addPlatformsBatch POST /Library/Platforms/Batch  body: {Dimension, Values:[...]}
// 批量把选中的维度值添加为虚拟库(默认 enabled=false)。
func addPlatformsBatch(c *gin.Context, state *AppState) {
	var body struct {
		Dimension string   `json:"Dimension"`
		Values    []string `json:"Values"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.Values) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Dimension and Values required"})
		return
	}
	ctx := c.Request.Context()
	added := 0
	skipped := 0
	var failed []gin.H
	for _, v := range body.Values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		created, err := models.AddPlatformLibrary(ctx, state.DB, body.Dimension, v, v, false)
		if err != nil {
			slog.Warn("[Platform] batch add failed", "dimension", body.Dimension, "value", v, "error", err)
			failed = append(failed, gin.H{"value": v, "message": err.Error()})
			continue
		}
		if created {
			added++
		} else {
			skipped++
		}
	}
	if len(failed) > 0 {
		if added > 0 {
			invalidateViewsCache(c, state)
		}
		status := http.StatusMultiStatus
		if added == 0 {
			status = http.StatusInternalServerError
		}
		c.JSON(status, gin.H{
			"message": "部分虚拟库添加失败",
			"added":   added,
			"skipped": skipped,
			"failed":  failed,
		})
		return
	}
	invalidateViewsCache(c, state)
	c.JSON(http.StatusOK, gin.H{"added": added, "skipped": skipped})
}

func setPlatformEnabled(c *gin.Context, state *AppState, enabled bool) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "id required"})
		return
	}
	if err := models.SetPlatformEnabledByID(c.Request.Context(), state.DB, id, enabled); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

// renamePlatform 设置虚拟库自定义显示名。POST /Library/Platforms/:id/Rename  body: {Name}
// Name 为空串则清除自定义名,回退默认本地化名。
func renamePlatform(c *gin.Context, state *AppState) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "id required"})
		return
	}
	var body struct {
		Name string `json:"Name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid body"})
		return
	}
	if err := models.RenamePlatform(c.Request.Context(), state.DB, id, body.Name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

// deletePlatformCover 清除虚拟库生成的封面,回退内置 logo / 默认渐变。
// DELETE /Library/Platforms/:id/Image
func deletePlatformCover(c *gin.Context, state *AppState) {
	id := c.Param("id")
	oldPath, err := models.ClearPlatformCover(c.Request.Context(), state.DB, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if oldPath != "" {
		_ = os.Remove(oldPath)
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

// addPlatformValues 把若干匹配值合并进某虚拟库(多维聚合,解决簡繁/译名拆库)。
// POST /Library/Platforms/:id/Values  body: {Values:[...]}
func addPlatformValues(c *gin.Context, state *AppState) {
	id := c.Param("id")
	var body struct {
		Values []string `json:"Values"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.Values) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Values required"})
		return
	}
	if err := models.AddPlatformValues(c.Request.Context(), state.DB, id, body.Values); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

// removePlatformValue 从某虚拟库移除一个匹配值(主匹配值不可移除)。
// DELETE /Library/Platforms/:id/Values?value=
func removePlatformValue(c *gin.Context, state *AppState) {
	id := c.Param("id")
	value := strings.TrimSpace(c.Query("value"))
	if value == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "value required"})
		return
	}
	if err := models.RemovePlatformValue(c.Request.Context(), state.DB, id, value); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

func deletePlatform(c *gin.Context, state *AppState) {
	id := c.Param("id")
	if err := models.DeletePlatformLibrary(c.Request.Context(), state.DB, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

func updatePlatformSortOrder(c *gin.Context, state *AppState) {
	var body struct {
		OrderedIds []string `json:"OrderedIds"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || len(body.OrderedIds) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "OrderedIds required"})
		return
	}
	if err := models.UpdatePlatformSortOrder(c.Request.Context(), state.DB, body.OrderedIds); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	invalidateViewsCache(c, state)
	c.Status(http.StatusNoContent)
}

func scanPlatformStudios(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()

	platformScanState.mu.Lock()
	if platformScanState.running {
		platformScanState.mu.Unlock()
		c.JSON(http.StatusOK, gin.H{"message": "already running"})
		return
	}
	platformScanState.running = true
	platformScanState.mu.Unlock()
	defer func() {
		if c.Writer.Written() && c.Writer.Status() >= 400 {
			platformScanState.mu.Lock()
			platformScanState.running = false
			platformScanState.mu.Unlock()
		}
	}()

	rescan := strings.EqualFold(c.Query("rescan"), "true")
	items, err := models.GetItemsPendingPlatformScan(ctx, state.DB, 50000, true, rescan)
	if err != nil {
		platformScanState.mu.Lock()
		platformScanState.running = false
		platformScanState.mu.Unlock()
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}

	if len(items) == 0 {
		platformScanState.mu.Lock()
		platformScanState.running = false
		platformScanState.mu.Unlock()
		noTmdbCount, _ := models.CountItemsPendingPlatformMetadataScrape(ctx, state.DB)
		c.JSON(http.StatusOK, gin.H{
			"message":          "no_items",
			"total":            0,
			"needs_scrape":     noTmdbCount,
			"needs_scrape_msg": fmt.Sprintf("有 %d 个项目尚未刮削 TMDB，需先执行全量刮削才能获取平台信息", noTmdbCount),
		})
		return
	}

	go func() {
		defer func() {
			platformScanState.mu.Lock()
			platformScanState.running = false
			platformScanState.mu.Unlock()
		}()
		bgCtx := context.Background()
		client := services.TmdbClientFromConfig(bgCtx, state.DB)
		if client == nil {
			slog.Error("[PlatformScan] Failed to create TMDB client, check API key config")
			return
		}

		type result struct {
			id       string
			itemType string
			studio   *string
			failed   bool
			errMsg   string
		}

		sem := make(chan struct{}, 5)
		results := make(chan result, len(items))

		for _, item := range items {
			sem <- struct{}{}
			go func(it models.PlatformScanItem) {
				defer func() { <-sem }()
				studio, fetchErr := services.RefreshPlatformOnlyByTMDBID(bgCtx, state.DB, it.ID, client)
				if fetchErr != nil {
					_ = models.MarkPlatformScanError(bgCtx, state.DB, it.ID, models.PlatformScanSourceTMDB, fetchErr.Error())
					results <- result{id: it.ID, itemType: it.ItemType, studio: nil, failed: true, errMsg: fetchErr.Error()}
					return
				}
				results <- result{id: it.ID, itemType: it.ItemType, studio: studio}
			}(item)
		}

		matched, noMatch, fetchErrors := 0, 0, 0
		for i := 0; i < len(items); i++ {
			r := <-results
			if r.failed {
				fetchErrors++
				continue
			}
			if r.studio == nil {
				noMatch++
			} else {
				matched++
			}
		}

		slog.Info("[PlatformScan] Done", "total", len(items), "matched", matched, "no_platform", noMatch, "fetch_errors", fetchErrors)
	}()

	c.JSON(http.StatusOK, gin.H{"message": "scanning", "total": len(items)})
}

// scanPlatformByFilename fills studio from filename patterns for items still missing studio.
func scanPlatformByFilename(c *gin.Context, state *AppState) {
	ctx := c.Request.Context()

	patterns := []repository.PlatformFilenamePattern{
		{Platform: "Netflix", SQL: "file_path ILIKE '%%.NF.%%' OR file_path ILIKE '%%Netflix%%'"},
		{Platform: "Disney+", SQL: "file_path ILIKE '%%.DSNP.%%' OR file_path ILIKE '%%Disney+%%'"},
		{Platform: "Apple TV+", SQL: "file_path ILIKE '%%.ATVP.%%' OR file_path ILIKE '%%Apple TV%%'"},
		{Platform: "Amazon", SQL: "file_path ILIKE '%%.AMZN.%%' OR file_path ILIKE '%%Amazon%%'"},
		{Platform: "HBO", SQL: "file_path ILIKE '%%.HMAX.%%' OR file_path ILIKE '%%.HBO.%%'"},
		{Platform: "Hulu", SQL: "file_path ILIKE '%%.HULU.%%'"},
		{Platform: "Paramount+", SQL: "file_path ILIKE '%%.PMTP.%%' OR file_path ILIKE '%%Paramount+%%'"},
		{Platform: "Peacock", SQL: "file_path ILIKE '%%.PCOK.%%'"},
		{Platform: "Crunchyroll", SQL: "file_path ILIKE '%%.CR.%%' OR file_path ILIKE '%%Crunchyroll%%'"},
	}

	total, err := repository.NewPlatformRepository(state.DB).ScanByFilename(ctx, patterns, models.CanonicalPlatformName)
	if err != nil {
		slog.Warn("[PlatformFilename] scan failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error(), "updated": total})
		return
	}

	invalidateViewsCache(c, state)
	slog.Info("[PlatformFilename] Done", "updated", total)
	c.JSON(http.StatusOK, gin.H{"message": "done", "updated": total})
}
