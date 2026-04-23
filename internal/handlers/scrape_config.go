package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"fyms/internal/models"
	"fyms/internal/services"
	"fyms/internal/services/scraper"
)

// RegisterScrapeConfigRoutes 注册刮削配置相关的端点。
// 所有端点 admin-only。
func RegisterScrapeConfigRoutes(group *gin.RouterGroup, adminMW gin.HandlerFunc) {
	group.GET("/System/Config/Scrape/Defaults", adminMW, getScrapeDefaults)
	group.GET("/Library/:id/ScrapeConfig", adminMW, getLibraryScrapeConfig)
	group.PUT("/Library/:id/ScrapeConfig", adminMW, putLibraryScrapeConfig)
}

// scrapeDefaultsResponse 给前端渲染 UI 的元数据:
// 有哪些 provider、哪些字段、每个字段默认的 provider 优先级。
// 字段名与 scraper.FieldPolicy 的字段一一对应,顺序即展示顺序。
type scrapeDefaultsResponse struct {
	Providers     []string            `json:"providers"`
	FieldNames    []string            `json:"field_names"`
	DefaultPolicy map[string][]string `json:"default_policy"`
}

func getScrapeDefaults(c *gin.Context) {
	def := scraper.DefaultFieldPolicy()
	resp := scrapeDefaultsResponse{
		Providers: []string{"tmdb", "tvdb", "bangumi", "douban", "fanart"},
		FieldNames: []string{
			"overview", "title", "original_title", "tagline", "premiered",
			"year", "rating", "actors", "poster", "backdrop", "season_poster",
		},
		DefaultPolicy: map[string][]string{
			"overview":       def.Overview,
			"title":          def.Title,
			"original_title": def.OriginalTitle,
			"tagline":        def.Tagline,
			"premiered":      def.Premiered,
			"year":           def.Year,
			"rating":         def.Rating,
			"actors":         def.Actors,
			"poster":         def.Poster,
			"backdrop":       def.Backdrop,
			"season_poster":  def.SeasonPoster,
		},
	}
	c.JSON(http.StatusOK, resp)
}

// effectiveScrapeConfig 是前端可见的 effective DTO。
// 所有凭据明文从不序列化。字段名保持 PascalCase 以匹配历史前端合约。
type effectiveScrapeConfig struct {
	ProvidersEnabled          []string            `json:"ProvidersEnabled"`
	ProviderPriority          map[string]int      `json:"ProviderPriority"`
	FieldPriority             map[string][]string `json:"FieldPriority"`
	ConfidenceThreshold       float64             `json:"ConfidenceThreshold"`
	AutoApply                 bool                `json:"AutoApply"`
	AdultContentFilterEnabled bool                `json:"AdultContentFilterEnabled"`
}

func effectiveFromRuntime(cfg scraper.RuntimeConfig) effectiveScrapeConfig {
	return effectiveScrapeConfig{
		ProvidersEnabled:          cfg.ProvidersEnabled,
		ProviderPriority:          cfg.ProviderPriority,
		FieldPriority:             cfg.FieldPriority,
		ConfidenceThreshold:       cfg.ConfidenceThreshold,
		AutoApply:                 cfg.AutoApply,
		AdultContentFilterEnabled: cfg.AdultContentFilterEnabled,
	}
}

// libraryScrapeConfigResponse 描述一个库的刮削配置三件套:
//   - inherit: override IS NULL(= 完全继承全局)
//   - override: 库级 JSONB 反序列化,null 表示未设置
//   - effective: 全局 + override 合并后的最终生效值(给前端预览,剥离凭据)
type libraryScrapeConfigResponse struct {
	Inherit   bool                    `json:"inherit"`
	Override  *scraper.ConfigOverride `json:"override"`
	Effective effectiveScrapeConfig   `json:"effective"`
}

func getLibraryScrapeConfig(c *gin.Context) {
	state := GetState(c)
	ctx := c.Request.Context()

	rawID := strings.TrimSpace(c.Param("id"))
	libID, err := uuid.Parse(rawID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid library id"})
		return
	}
	lib, err := models.GetLibraryByID(ctx, state.DB, libID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if lib == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "library not found"})
		return
	}

	override, _ := services.LoadLibraryScrapeOverride(ctx, state.DB, libID.String())
	effective := services.LoadEffectiveScrapeConfig(ctx, state.DB, libID.String())

	c.JSON(http.StatusOK, libraryScrapeConfigResponse{
		Inherit:   override == nil,
		Override:  override,
		Effective: effectiveFromRuntime(effective),
	})
}

type putLibraryScrapeConfigBody struct {
	Inherit  bool                    `json:"inherit"`
	Override *scraper.ConfigOverride `json:"override"`
}

func putLibraryScrapeConfig(c *gin.Context) {
	state := GetState(c)
	ctx := c.Request.Context()

	rawID := strings.TrimSpace(c.Param("id"))
	libID, err := uuid.Parse(rawID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid library id"})
		return
	}

	var body putLibraryScrapeConfigBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	// inherit=true 或 override 全空 → 写 NULL
	if body.Inherit || body.Override == nil || body.Override.IsEmpty() {
		if err := models.UpdateLibraryScrapeConfig(ctx, state.DB, libID, nil); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		services.InvalidateScrapeAggregator()
		c.Status(http.StatusNoContent)
		return
	}

	raw, err := json.Marshal(body.Override)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid override: " + err.Error()})
		return
	}
	rawStr := string(raw)
	if err := models.UpdateLibraryScrapeConfig(ctx, state.DB, libID, &rawStr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	services.InvalidateScrapeAggregator()
	c.Status(http.StatusNoContent)
}
