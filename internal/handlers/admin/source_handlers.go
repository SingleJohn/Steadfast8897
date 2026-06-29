package admin

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"fyms/internal/repository"
	sourcebridge "fyms/internal/source"
)

type importTVBoxRequest struct {
	Name      string          `json:"name"`
	SourceURL string          `json:"source_url"`
	RawJSON   string          `json:"raw_json"`
	JSON      json.RawMessage `json:"json"`
}

type importCMSListRequest struct {
	Name           string          `json:"name"`
	SourceURL      string          `json:"source_url"`
	RawText        string          `json:"raw_text"`
	Format         string          `json:"format"`
	DefaultEnabled bool            `json:"default_enabled"`
	JSON           json.RawMessage `json:"json"`
}

func importTVBoxConfig(c *gin.Context, state *AppState) {
	var req importTVBoxRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	raw := []byte(req.JSON)
	if len(raw) == 0 && strings.TrimSpace(req.RawJSON) != "" {
		raw = []byte(strings.TrimSpace(req.RawJSON))
		if decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.RawJSON)); err == nil && len(decoded) > 0 && strings.HasPrefix(strings.TrimSpace(string(decoded)), "{") {
			raw = decoded
		}
	}
	importer := sourcebridge.NewTVBoxImporter(state.Repo.Source)
	result, err := importer.Import(c.Request.Context(), sourcebridge.ImportTVBoxInput{
		Name:      req.Name,
		SourceURL: req.SourceURL,
		RawJSON:   raw,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func importCMSListConfig(c *gin.Context, state *AppState) {
	var req importCMSListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	raw := []byte(req.JSON)
	if len(raw) == 0 && strings.TrimSpace(req.RawText) != "" {
		raw = []byte(strings.TrimSpace(req.RawText))
		if decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.RawText)); err == nil && len(decoded) > 0 {
			trimmed := strings.TrimSpace(string(decoded))
			if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(strings.ToLower(trimmed), "type:") {
				raw = decoded
			}
		}
	}
	importer := sourcebridge.NewCMSListImporter(state.Repo.Source)
	result, err := importer.Import(c.Request.Context(), sourcebridge.ImportCMSListInput{
		Name:           req.Name,
		SourceURL:      req.SourceURL,
		RawText:        raw,
		Format:         req.Format,
		DefaultEnabled: req.DefaultEnabled,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// refreshSourceConfig 用已存配置的 source_url 重新拉取并重导，原地更新 Provider 并保留启停状态。
// TVBox 在无 URL 时回退到已存原始 JSON；CMS 源清单仅保存解析后的元数据，无 URL 时无法自动更新。
func refreshSourceConfig(c *gin.Context, state *AppState) {
	id, ok := pathInt64(c, "id")
	if !ok {
		return
	}
	config, err := state.Repo.Source.GetConfigImportByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if config == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "source config not found"})
		return
	}
	sourceURL := ""
	if config.SourceURL != nil {
		sourceURL = strings.TrimSpace(*config.SourceURL)
	}
	switch config.SourceType {
	case "tvbox":
		var raw []byte
		if sourceURL == "" {
			raw = []byte(config.RawConfig)
		}
		if sourceURL == "" && len(raw) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"message": "该 TVBox 配置无来源 URL 且无原始内容，无法自动更新，请重新导入"})
			return
		}
		importer := sourcebridge.NewTVBoxImporter(state.Repo.Source)
		result, err := importer.Import(c.Request.Context(), sourcebridge.ImportTVBoxInput{
			Name:                  config.Name,
			SourceURL:             sourceURL,
			RawJSON:               raw,
			PreserveProviderState: true,
		})
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, result)
	case sourcebridge.CMSListSourceType:
		if sourceURL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "该 CMS 源清单由粘贴内容导入，无来源 URL，无法自动更新，请重新粘贴导入"})
			return
		}
		importer := sourcebridge.NewCMSListImporter(state.Repo.Source)
		result, err := importer.Import(c.Request.Context(), sourcebridge.ImportCMSListInput{
			Name:                  config.Name,
			SourceURL:             sourceURL,
			Format:                sourcebridge.CMSListFormatAuto,
			PreserveProviderState: true,
		})
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, result)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": "不支持的配置类型: " + config.SourceType})
	}
}

func listSourceConfigs(c *gin.Context, state *AppState) {
	rows, err := state.Repo.Source.ListConfigImports(c.Request.Context(), repository.SourceConfigListOptions{
		Limit:  int64(queryInt(c, "limit", 100)),
		Offset: int64(queryInt(c, "offset", 0)),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": rows})
}

func setSourceConfigEnabled(c *gin.Context, state *AppState, enabled bool) {
	id, ok := pathInt64(c, "id")
	if !ok {
		return
	}
	item, err := state.Repo.Source.SetConfigEnabled(c.Request.Context(), id, enabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "source config not found"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func listSourceParsers(c *gin.Context, state *AppState) {
	var configID *int64
	if raw := strings.TrimSpace(c.Query("config_id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "invalid config_id"})
			return
		}
		configID = &id
	}
	rows, err := state.Repo.Source.ListParsers(c.Request.Context(), repository.SourceParserListOptions{
		Limit:    int64(queryInt(c, "limit", 100)),
		Offset:   int64(queryInt(c, "offset", 0)),
		ConfigID: configID,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": rows})
}

func setSourceParserEnabled(c *gin.Context, state *AppState, enabled bool) {
	id, ok := pathInt64(c, "id")
	if !ok {
		return
	}
	item, err := state.Repo.Source.SetParserEnabled(c.Request.Context(), id, enabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "source parser not found"})
		return
	}
	c.JSON(http.StatusOK, item)
}

type providerSearchRequest struct {
	Keyword string `json:"keyword"`
	Page    int    `json:"page"`
}

type federatedSearchRequest struct {
	Keyword string `json:"keyword"`
	Limit   int    `json:"limit"`
	DryRun  bool   `json:"dry_run"`
}

func federatedSourceSearch(c *gin.Context, state *AppState) {
	var req federatedSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	manager := sourcebridge.NewProviderRuntimeManager(state.Repo.Source, state.HTTPClient).WithJSRuntime(state.JSRuntime).WithCSPRuntime(state.CSPRuntime)
	result, err := manager.FederatedSearch(c.Request.Context(), sourcebridge.FederatedSearchRequest{
		Keyword: req.Keyword,
		Limit:   req.Limit,
		DryRun:  req.DryRun,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

type providerDetailRequest struct {
	SourceItemID string `json:"source_item_id"`
	ID           string `json:"id"`
}

func pathInt64(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param(name)), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid " + name})
		return 0, false
	}
	return id, true
}

func queryInt(c *gin.Context, name string, fallback int) int {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
