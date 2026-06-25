package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"fyms/internal/repository"
	sourcebridge "fyms/internal/source"
)

type sourceProviderDTO struct {
	ID              int64
	ConfigID        *int64
	SourceKey       string
	Name            string
	ProviderKind    string
	RuntimeKind     string
	TVBoxType       *int32
	API             string
	APIHash         string
	TimeoutMS       int32
	Enabled         bool
	Visible         bool
	Searchable      bool
	HealthStatus    string
	LastCheckAt     *time.Time
	LastError       *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Categories      any
	CategoriesCount int
}

type sourceProviderCategoryDTO struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Count  *int64 `json:"count,omitempty"`
	Source string `json:"source"`
}

type sourceProviderBatchRequest struct {
	ProviderIDs []int64 `json:"provider_ids"`
}

type sourceProviderBatchHealthResult struct {
	ProviderID      int64  `json:"provider_id"`
	ProviderName    string `json:"provider_name"`
	Status          string `json:"status"`
	ErrorType       string `json:"error_type,omitempty"`
	Message         string `json:"message,omitempty"`
	LatencyMS       int64  `json:"latency_ms"`
	CategoriesCount int    `json:"categories_count"`
}

func listSourceProviders(c *gin.Context, state *AppState) {
	configID, ok := queryInt64Ptr(c, "config_id")
	if !ok {
		return
	}
	enabled, ok := queryBoolPtr(c, "enabled")
	if !ok {
		return
	}
	rows, err := state.Repo.Source.ListProviders(c.Request.Context(), repository.SourceProviderListOptions{
		Limit:        int64(queryInt(c, "limit", 100)),
		Offset:       int64(queryInt(c, "offset", 0)),
		ConfigID:     configID,
		Enabled:      enabled,
		HealthStatus: strings.TrimSpace(c.Query("health_status")),
		RuntimeKind:  strings.TrimSpace(c.Query("runtime_kind")),
		ProviderKind: strings.TrimSpace(c.Query("provider_kind")),
		Keyword:      strings.TrimSpace(c.Query("keyword")),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": sourceProviderDTOs(rows)})
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
	c.JSON(http.StatusOK, sourceProviderDTOFromRepository(*item))
}

func batchEnableSourceProviders(c *gin.Context, state *AppState) {
	batchSetSourceProvidersEnabled(c, state, true)
}

func batchDisableSourceProviders(c *gin.Context, state *AppState) {
	batchSetSourceProvidersEnabled(c, state, false)
}

func batchSetSourceProvidersEnabled(c *gin.Context, state *AppState, enabled bool) {
	var req sourceProviderBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	rows, err := state.Repo.Source.SetProvidersEnabled(c.Request.Context(), req.ProviderIDs, enabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items": sourceProviderDTOs(rows),
		"count": len(rows),
	})
}

func healthCheckSourceProvider(c *gin.Context, state *AppState) {
	id, ok := pathInt64(c, "id")
	if !ok {
		return
	}
	manager := newSourceProviderRuntimeManager(state)
	item, err := manager.HealthCheck(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, sourceProviderDTOFromRepository(*item))
}

func batchHealthCheckSourceProviders(c *gin.Context, state *AppState) {
	var req sourceProviderBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ids := compactRequestInt64s(req.ProviderIDs)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "provider_ids required"})
		return
	}

	manager := newSourceProviderRuntimeManager(state)
	sem := make(chan struct{}, 4)
	results := make([]sourceProviderBatchHealthResult, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(index int, providerID int64) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[index] = runSourceProviderHealth(c.Request.Context(), state, manager, providerID)
		}(i, id)
	}
	wg.Wait()
	c.JSON(http.StatusOK, gin.H{"items": results, "count": len(results)})
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
	manager := newSourceProviderRuntimeManager(state)
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
	manager := newSourceProviderRuntimeManager(state)
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
	manager := newSourceProviderRuntimeManager(state)
	items, err := manager.Categories(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items": sourceProviderCategoryDTOs(items),
		"meta": gin.H{
			"source":      "upstream_site",
			"description": "上游站点栏目，可用于站点浏览与在线库组织辅助。",
		},
	})
}

func runSourceProviderHealth(ctx context.Context, state *AppState, manager *sourcebridge.ProviderRuntimeManager, providerID int64) sourceProviderBatchHealthResult {
	start := time.Now()
	result := sourceProviderBatchHealthResult{
		ProviderID: providerID,
		Status:     "error",
	}
	if row, err := state.Repo.Source.GetProviderByID(ctx, providerID); err == nil && row != nil {
		result.ProviderName = row.Name
	}
	item, err := manager.HealthCheck(ctx, providerID)
	result.LatencyMS = time.Since(start).Milliseconds()
	if item != nil {
		result.ProviderName = item.Name
		result.Status = item.HealthStatus
		result.Message = stringValue(item.LastError)
		result.CategoriesCount = categoriesCount(item.Categories)
	}
	if err != nil {
		result.ErrorType = sourcebridge.ErrorType(err)
		result.Message = err.Error()
		if result.Status == "" {
			result.Status = "error"
		}
	}
	if result.ErrorType == "" && result.Status != "ok" && result.Message != "" {
		result.ErrorType = sourcebridge.ErrorType(errors.New(result.Message))
	}
	return result
}

func newSourceProviderRuntimeManager(state *AppState) *sourcebridge.ProviderRuntimeManager {
	return sourcebridge.NewProviderRuntimeManager(state.Repo.Source, state.HTTPClient).WithJSRuntime(state.JSRuntime).WithCSPRuntime(state.CSPRuntime)
}

func sourceProviderDTOs(rows []repository.SourceProvider) []sourceProviderDTO {
	out := make([]sourceProviderDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, sourceProviderDTOFromRepository(row))
	}
	return out
}

func sourceProviderDTOFromRepository(row repository.SourceProvider) sourceProviderDTO {
	var categories any
	if len(row.Categories) > 0 {
		categories = row.Categories
	}
	return sourceProviderDTO{
		ID:              row.ID,
		ConfigID:        row.ConfigID,
		SourceKey:       row.SourceKey,
		Name:            row.Name,
		ProviderKind:    row.ProviderKind,
		RuntimeKind:     row.RuntimeKind,
		TVBoxType:       row.TVBoxType,
		API:             redactedSourceProviderAPI(row.API),
		APIHash:         sourcebridge.URLHash(row.API),
		TimeoutMS:       row.TimeoutMS,
		Enabled:         row.Enabled,
		Visible:         row.Visible,
		Searchable:      row.Searchable,
		HealthStatus:    row.HealthStatus,
		LastCheckAt:     row.LastCheckAt,
		LastError:       row.LastError,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
		Categories:      categories,
		CategoriesCount: categoriesCount(row.Categories),
	}
}

func sourceProviderCategoryDTOs(items []sourcebridge.ProviderCategory) []sourceProviderCategoryDTO {
	out := make([]sourceProviderCategoryDTO, 0, len(items))
	for _, item := range items {
		out = append(out, sourceProviderCategoryDTO{
			ID:     item.ID,
			Name:   item.Name,
			Source: "upstream_site",
		})
	}
	return out
}

func categoriesCount(raw []byte) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var rows []sourcebridge.ProviderCategory
	if err := json.Unmarshal(raw, &rows); err == nil {
		return len(rows)
	}
	return 0
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func redactedSourceProviderAPI(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		if idx := strings.Index(raw, "?"); idx >= 0 {
			return raw[:idx] + "?..."
		}
		return raw
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func compactRequestInt64s(values []int64) []int64 {
	out := make([]int64, 0, len(values))
	seen := map[int64]struct{}{}
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func queryInt64Ptr(c *gin.Context, name string) (*int64, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid " + name})
		return nil, false
	}
	return &value, true
}

func queryBoolPtr(c *gin.Context, name string) (*bool, bool) {
	raw := strings.TrimSpace(strings.ToLower(c.Query(name)))
	if raw == "" {
		return nil, true
	}
	switch raw {
	case "1", "true", "yes", "on":
		value := true
		return &value, true
	case "0", "false", "no", "off":
		value := false
		return &value, true
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid " + name})
		return nil, false
	}
}
