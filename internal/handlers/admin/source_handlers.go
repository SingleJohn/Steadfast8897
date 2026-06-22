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

func listSourceProviders(c *gin.Context, state *AppState) {
	var configID *int64
	if raw := strings.TrimSpace(c.Query("config_id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "invalid config_id"})
			return
		}
		configID = &id
	}
	rows, err := state.Repo.Source.ListProviders(c.Request.Context(), repository.SourceProviderListOptions{
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

func setSourceProviderEnabled(c *gin.Context, state *AppState, enabled bool) {
	id, ok := pathInt64(c, "id")
	if !ok {
		return
	}
	item, err := state.Repo.Source.SetProviderEnabled(c.Request.Context(), id, enabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "source provider not found"})
		return
	}
	c.JSON(http.StatusOK, item)
}

func healthCheckSourceProvider(c *gin.Context, state *AppState) {
	id, ok := pathInt64(c, "id")
	if !ok {
		return
	}
	manager := sourcebridge.NewProviderRuntimeManager(state.Repo.Source, state.HTTPClient).WithJSRuntime(state.JSRuntime)
	item, err := manager.HealthCheck(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
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
}

func federatedSourceSearch(c *gin.Context, state *AppState) {
	var req federatedSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	manager := sourcebridge.NewProviderRuntimeManager(state.Repo.Source, state.HTTPClient).WithJSRuntime(state.JSRuntime)
	result, err := manager.FederatedSearch(c.Request.Context(), sourcebridge.FederatedSearchRequest{
		Keyword: req.Keyword,
		Limit:   req.Limit,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func searchSourceProvider(c *gin.Context, state *AppState) {
	id, ok := pathInt64(c, "id")
	if !ok {
		return
	}
	var req providerSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	manager := sourcebridge.NewProviderRuntimeManager(state.Repo.Source, state.HTTPClient).WithJSRuntime(state.JSRuntime)
	page, items, err := manager.Search(c.Request.Context(), id, sourcebridge.SearchRequest{
		Keyword: req.Keyword,
		Page:    req.Page,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"page": page, "items": items})
}

type providerDetailRequest struct {
	SourceItemID string `json:"source_item_id"`
	ID           string `json:"id"`
}

func detailSourceProvider(c *gin.Context, state *AppState) {
	id, ok := pathInt64(c, "id")
	if !ok {
		return
	}
	var req providerDetailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	sourceItemID := strings.TrimSpace(req.SourceItemID)
	if sourceItemID == "" {
		sourceItemID = strings.TrimSpace(req.ID)
	}
	if sourceItemID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "source_item_id required"})
		return
	}
	manager := sourcebridge.NewProviderRuntimeManager(state.Repo.Source, state.HTTPClient).WithJSRuntime(state.JSRuntime)
	detail, item, playSources, err := manager.Detail(c.Request.Context(), id, sourceItemID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"detail": detail, "item": item, "play_sources": playSources})
}

func listSourceProviderCategories(c *gin.Context, state *AppState) {
	id, ok := pathInt64(c, "id")
	if !ok {
		return
	}
	manager := sourcebridge.NewProviderRuntimeManager(state.Repo.Source, state.HTTPClient).WithJSRuntime(state.JSRuntime)
	items, err := manager.Categories(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
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
