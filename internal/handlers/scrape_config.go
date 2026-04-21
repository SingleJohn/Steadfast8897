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
// 有哪些 provider、哪些字段、策略有哪些候选值。
// 字段名与 scraper.FieldPolicy 的字段一一对应,顺序即展示顺序。
type scrapeDefaultsResponse struct {
	Providers       []string            `json:"providers"`
	FieldNames      []string            `json:"field_names"`
	StrategyOptions []string            `json:"strategy_options"`
	DefaultPolicy   map[string][]string `json:"default_policy"`
}

func getScrapeDefaults(c *gin.Context) {
	def := scraper.DefaultFieldPolicy()
	resp := scrapeDefaultsResponse{
		Providers: []string{"tmdb", "tvdb", "bangumi", "douban", "fanart"},
		FieldNames: []string{
			"overview", "title", "original_title", "tagline", "premiered",
			"year", "rating", "actors", "poster", "backdrop", "season_poster",
		},
		StrategyOptions: []string{
			string(scraper.StrategyAggregated),
			string(scraper.StrategySequential),
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

// libraryScrapeConfigResponse 描述一个库的刮削配置三件套:
//   - inherit: override IS NULL(= 完全继承全局)
//   - override: 库级 JSONB 反序列化,null 表示未设置
//   - effective: 全局 + override 合并后的最终生效值(给前端预览)
type libraryScrapeConfigResponse struct {
	Inherit   bool                     `json:"inherit"`
	Override  *scraper.ConfigOverride  `json:"override"`
	Effective scraper.RuntimeConfig    `json:"effective"`
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

	// Effective 里的凭据字段剥离,避免泄漏给前端
	effective = sanitizeEffectiveForClient(effective)

	c.JSON(http.StatusOK, libraryScrapeConfigResponse{
		Inherit:   override == nil,
		Override:  override,
		Effective: effective,
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

// sanitizeEffectiveForClient 清掉 RuntimeConfig 里的凭据明文,只保留行为字段。
// 前端只需要知道"启用哪些 provider / 字段顺序 / 策略 / 阈值",不需要看 api key。
func sanitizeEffectiveForClient(cfg scraper.RuntimeConfig) scraper.RuntimeConfig {
	cfg.TVDBAPIKey = ""
	cfg.TVDBPin = ""
	cfg.FanartAPIKey = ""
	cfg.DoubanCookie = ""
	cfg.DoubanUA = ""
	cfg.BangumiUA = ""
	return cfg
}
